package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/api"
	"pulsar/core/internal/blob"
	"pulsar/core/internal/comfy"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/job"
	"pulsar/core/internal/worker"
	"pulsar/core/internal/workflow"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	var (
		port         = envOr("PORT", "8080")
		databaseURL  = envOr("DATABASE_URL", "postgres://pulsar:pulsar@localhost:5432/pulsar?sslmode=disable")
		comfyURLs    = envOr("COMFY_URL", "http://localhost:8188") // comma-separated, one per GPU
		workflowsDir = envOr("WORKFLOWS_DIR", "../workflows")
		dataDir      = envOr("DATA_DIR", "./data")
		checkpoint   = envOr("CHECKPOINT", "illustrious-xl.safetensors")
		lineage      = envOr("LINEAGE", "illustrious")
		execTimeout  = envOr("EXEC_TIMEOUT", "5m")
	)

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
	broker := job.NewBroker()
	checker := compliance.Stub{} // phase 4 replaces the impl; the call sites stay

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
			Tpl: tpl, Cfg: api.Config{Lineage: lineage, Checkpoint: checkpoint},
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
