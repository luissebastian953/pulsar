package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/api"
	"pulsar/core/internal/blob"
	"pulsar/core/internal/comfy"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/dotenv"
	"pulsar/core/internal/job"
	"pulsar/core/internal/nltag"
	"pulsar/core/internal/tags"
	"pulsar/core/internal/worker"
	"pulsar/core/internal/workflow"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func main() {
	dotenv.Load() // native runs read the repo .env; real env always wins
	var (
		port         = envOr("PORT", "8080")
		databaseURL  = envOr("DATABASE_URL", "postgres://pulsar:pulsar@localhost:5432/pulsar?sslmode=disable")
		comfyURLs    = envOr("COMFY_URL", "http://localhost:8188") // comma-separated, one per GPU
		workflowsDir = envOr("WORKFLOWS_DIR", "../workflows")
		dataDir      = envOr("DATA_DIR", "./data")
		checkpoint   = envOr("CHECKPOINT", "illustrious-xl.safetensors")
		lineage      = envOr("LINEAGE", "illustrious")
		execTimeout  = envOr("EXEC_TIMEOUT", "5m")
		corsOrigins  = envOr("CORS_ORIGINS", "*") // comma-separated; empty disables
		forbiddenUC  = strings.TrimSpace(os.Getenv("FORBIDDEN_TAG_UC"))
	)

	// fail closed — a deployment without the forbidden-tag negative does
	// not run (CLAUDE.md non-negotiables)
	if forbiddenUC == "" {
		log.Fatalf("FORBIDDEN_TAG_UC must be set and non-empty — copy the value from .env.example; see docs/compliance.md")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	timeout, err := time.ParseDuration(execTimeout)
	if err != nil {
		log.Fatalf("invalid EXEC_TIMEOUT: %v", err)
	}

	// fail fast on a drifted template before accepting any work
	tpl, err := workflow.Load(workflowsDir, workflow.TplTxt2Img, workflow.Txt2ImgSHA256)
	if err != nil {
		log.Fatalf("load workflow template: %v", err)
	}
	poseTpl, err := workflow.Load(workflowsDir, workflow.TplPose, workflow.PoseSHA256)
	if err != nil {
		log.Fatalf("load workflow template: %v", err)
	}
	img2imgTpl, err := workflow.Load(workflowsDir, workflow.TplImg2Img, workflow.Img2ImgSHA256)
	if err != nil {
		log.Fatalf("load workflow template: %v", err)
	}
	inpaintTpl, err := workflow.Load(workflowsDir, workflow.TplInpaint, workflow.InpaintSHA256)
	if err != nil {
		log.Fatalf("load workflow template: %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping postgres: %v", err)
	}

	var clients []*comfy.Client
	for _, u := range strings.Split(comfyURLs, ",") {
		c, err := comfy.New(strings.TrimSpace(u))
		if err != nil {
			log.Fatalf("comfy client: %v", err)
		}
		clients = append(clients, c)
	}
	router, err := comfy.NewRouter(clients)
	if err != nil {
		log.Fatalf("comfy router: %v", err)
	}

	blobs, err := blob.NewDir(dataDir)
	if err != nil {
		log.Fatalf("blob store: %v", err)
	}

	store := job.NewStore(pool)
	tagStore := tags.NewStore(pool)
	broker := job.NewBroker()

	// the LLM decomposes, the tag DB grounds; without a provider key the
	// endpoint 503s and everything else runs normally
	var decomposer nltag.Decomposer
	switch provider := envOr("TRANSLATE_PROVIDER", "claude"); provider {
	case "claude":
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			decomposer = nltag.NewClaudeDecomposer(key, envOr("ANTHROPIC_MODEL", "claude-opus-4-8"), "")
		} else {
			log.Printf("nltag: ANTHROPIC_API_KEY not set — POST /translate disabled")
		}
	case "deepseek":
		if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
			decomposer = nltag.NewDeepSeekDecomposer(key,
				envOr("DEEPSEEK_MODEL", "deepseek-v4-flash"), os.Getenv("DEEPSEEK_BASE_URL"))
		} else {
			log.Printf("nltag: DEEPSEEK_API_KEY not set — POST /translate disabled")
		}
	default:
		log.Fatalf("invalid TRANSLATE_PROVIDER %q (want claude or deepseek)", provider)
	}
	// live tag validation on DB misses; empty DANBOORU_URL disables
	var live *tags.Live
	if danbooruURL := envOr("DANBOORU_URL", "https://danbooru.donmai.us"); danbooruURL != "" {
		floor, err := strconv.Atoi(envOr("MIN_POST_COUNT", "50"))
		if err != nil {
			log.Fatalf("invalid MIN_POST_COUNT: %v", err)
		}
		live = tags.NewLive(pool, danbooruURL, floor)
	}

	var translator *nltag.Translator
	if decomposer != nil {
		translator = &nltag.Translator{
			Decomposer: decomposer,
			Grounder:   &nltag.Grounder{Store: tagStore, Live: live},
		}
	}
	// output classification (docs/compliance.md); unset SENTINEL_URL = stub
	var checker compliance.Checker = compliance.Stub{}
	if sentinelURL := os.Getenv("SENTINEL_URL"); sentinelURL != "" {
		threshold, sumGate := 0.0, 0.0
		if v := os.Getenv("SENTINEL_BLOCK_THRESHOLD"); v != "" {
			if threshold, err = strconv.ParseFloat(v, 64); err != nil {
				log.Fatalf("invalid SENTINEL_BLOCK_THRESHOLD: %v", err)
			}
		}
		if v := os.Getenv("SENTINEL_SUM_THRESHOLD"); v != "" {
			if sumGate, err = strconv.ParseFloat(v, 64); err != nil {
				log.Fatalf("invalid SENTINEL_SUM_THRESHOLD: %v", err)
			}
		}
		checker = compliance.NewSentinel(sentinelURL, forbiddenUC, threshold, sumGate)
	} else {
		log.Printf("compliance: SENTINEL_URL not set — outputs are UNCLASSIFIED; do not expose publicly (docs/compliance.md)")
	}

	// one worker per ComfyUI process: a process runs one workflow at a time
	for i := 0; i < router.Size(); i++ {
		w := &worker.Worker{
			Store: store, Broker: broker, Exec: router,
			Checker: checker, Blobs: blobs, ExecTimeout: timeout,
		}
		go w.Run(ctx)
	}

	srv := &http.Server{
		Addr: ":" + port,
		Handler: (&api.Server{
			Store: store, Broker: broker, Checker: checker, Blobs: blobs,
			Tags: tagStore, Live: live, Translator: translator, Tpl: tpl, PoseTpl: poseTpl, Img2ImgTpl: img2imgTpl, InpaintTpl: inpaintTpl,
			Cfg: api.Config{
				Lineage: lineage, Checkpoint: checkpoint,
				CORSOrigins:    splitNonEmpty(corsOrigins),
				QualityTags:    os.Getenv("QUALITY_TAGS"),
				DefaultUC:      os.Getenv("DEFAULT_UC"),
				ForbiddenTagUC: forbiddenUC,
			},
		}).Handler(),
	}
	go func() {
		log.Printf("core listening on :%s (comfy: %s)", port, comfyURLs)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
}
