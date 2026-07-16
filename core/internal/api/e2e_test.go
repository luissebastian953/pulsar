// End-to-end over the real spine: HTTP → queue (Postgres) → worker →
// fake ComfyUI → compliance hook → blob store → SSE. No GPU involved.
package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/api"
	"pulsar/core/internal/assembly"
	"pulsar/core/internal/blob"
	"pulsar/core/internal/comfy"
	"pulsar/core/internal/comfy/comfytest"
	"pulsar/core/internal/compliance"
	"pulsar/core/internal/job"
	"pulsar/core/internal/worker"
	"pulsar/core/internal/workflow"
)

func testStore(t *testing.T, schema string) *job.Store {
	t.Helper()
	dsn := os.Getenv("PULSAR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PULSAR_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	for _, sql := range []string{
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema),
		fmt.Sprintf("CREATE SCHEMA %s", schema),
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../../db/migrations/0001_jobs.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	return job.NewStore(pool)
}

type env struct {
	ts    *httptest.Server
	fake  *comfytest.Fake
	blobs *blob.Dir
}

func newEnv(t *testing.T, schema string, checker compliance.Checker) *env {
	t.Helper()
	store := testStore(t, schema)
	fake := comfytest.New(t)

	client, err := comfy.New(fake.URL())
	if err != nil {
		t.Fatal(err)
	}
	router, err := comfy.NewRouter([]*comfy.Client{client})
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := blob.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := workflow.Load("../../../workflows", workflow.TplTxt2Img, workflow.Txt2ImgSHA256)
	if err != nil {
		t.Fatal(err)
	}
	broker := job.NewBroker()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w := &worker.Worker{
		Store: store, Broker: broker, Exec: router, Checker: checker,
		Blobs: blobs, ExecTimeout: 30 * time.Second, Poll: 25 * time.Millisecond,
	}
	go w.Run(ctx)

	srv := &api.Server{
		Store: store, Broker: broker, Checker: checker, Blobs: blobs,
		Tpl: tpl, Cfg: api.Config{Lineage: "illustrious", Checkpoint: "test-ckpt.safetensors"},
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &env{ts: ts, fake: fake, blobs: blobs}
}

func (e *env) generate(t *testing.T, body string) string {
	t.Helper()
	resp, err := http.Post(e.ts.URL+"/generate", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /generate = %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.JobID == "" {
		t.Fatalf("bad response %s", raw)
	}
	return out.JobID
}

type jobView struct {
	ID       string          `json:"id"`
	State    string          `json:"state"`
	Params   json.RawMessage `json:"params"`
	ImageURL string          `json:"image_url"`
	Error    string          `json:"error"`
}

func (e *env) waitTerminal(t *testing.T, id string) jobView {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(e.ts.URL + "/jobs/" + id)
		if err != nil {
			t.Fatal(err)
		}
		var v jobView
		err = json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch v.State {
		case "done", "failed", "blocked":
			return v
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("job never reached a terminal state")
	return jobView{}
}

func TestEndToEndDone(t *testing.T) {
	e := newEnv(t, "e2e_done", compliance.Stub{})
	id := e.generate(t, `{"prompt": "1girl, silver hair", "seed": 42}`)

	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s), want done", v.State, v.Error)
	}
	if v.ImageURL != "/images/"+id {
		t.Errorf("image_url = %q", v.ImageURL)
	}

	// resolved params stored: quality tags applied, seed round-tripped
	var spec assembly.Spec
	if err := json.Unmarshal(v.Params, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Seed != 42 {
		t.Errorf("stored seed = %d, want 42", spec.Seed)
	}
	if !strings.HasPrefix(spec.Prompt, "masterpiece") {
		t.Errorf("stored prompt lost quality tags: %q", spec.Prompt)
	}

	// the patched graph actually reached comfy
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	if got := g[workflow.Txt2Img.Sampler].Inputs["seed"]; got != float64(42) {
		t.Errorf("submitted seed = %v", got)
	}
	if got := g[workflow.Txt2Img.Checkpoint].Inputs["ckpt_name"]; got != "test-ckpt.safetensors" {
		t.Errorf("submitted ckpt = %v", got)
	}

	// image is served and round-trips
	resp, err := http.Get(e.ts.URL + v.ImageURL)
	if err != nil {
		t.Fatal(err)
	}
	img, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(img, e.fake.PNG) {
		t.Errorf("image fetch = %d, %d bytes", resp.StatusCode, len(img))
	}

	// SSE re-attach after completion: snapshot alone must yield the terminal event
	sresp, err := http.Get(e.ts.URL + "/jobs/" + id + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	line, err := bufio.NewReader(sresp.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(line) != "event: done" {
		t.Errorf("first SSE line = %q, want event: done", line)
	}
}

// denyOutput allows prompts through but rejects every generated image —
// the post-generation, pre-delivery gate.
type denyOutput struct{ compliance.Stub }

func (denyOutput) CheckOutput(context.Context, []byte) (compliance.Decision, error) {
	return compliance.Decision{Allowed: false, Reason: "test policy"}, nil
}

func TestEndToEndBlocked(t *testing.T) {
	e := newEnv(t, "e2e_blocked", denyOutput{})
	id := e.generate(t, `{"prompt": "1girl"}`)

	v := e.waitTerminal(t, id)
	if v.State != "blocked" {
		t.Fatalf("state = %s, want blocked", v.State)
	}
	if v.ImageURL != "" {
		t.Error("blocked job exposed an image URL")
	}
	resp, err := http.Get(e.ts.URL + "/images/" + id)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("blocked image fetch = %d, want 404 — the image must never be delivered", resp.StatusCode)
	}
}

// rejectPrompt exercises the pre-enqueue hook.
type rejectPrompt struct{ compliance.Stub }

func (rejectPrompt) CheckPrompt(context.Context, assembly.Spec) (compliance.Decision, error) {
	return compliance.Decision{Allowed: false, Reason: "test policy"}, nil
}

func TestPromptRejectedBeforeEnqueue(t *testing.T) {
	e := newEnv(t, "e2e_reject", rejectPrompt{})
	resp, err := http.Post(e.ts.URL+"/generate", "application/json", strings.NewReader(`{"prompt": "1girl"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestEndToEndFailed(t *testing.T) {
	e := newEnv(t, "e2e_failed", compliance.Stub{})
	e.fake.FailExecution = true
	id := e.generate(t, `{"prompt": "1girl"}`)

	v := e.waitTerminal(t, id)
	if v.State != "failed" {
		t.Fatalf("state = %s, want failed", v.State)
	}
	if !strings.Contains(v.Error, "fake execution failure") {
		t.Errorf("error = %q", v.Error)
	}
}

func TestGenerateValidation(t *testing.T) {
	e := newEnv(t, "e2e_validation", compliance.Stub{})
	for name, body := range map[string]string{
		"empty prompt":  `{"prompt": ""}`,
		"off bucket":    `{"prompt": "1girl", "width": 512, "height": 512}`,
		"bad sampler":   `{"prompt": "1girl", "sampler": "ddim"}`,
		"unknown field": `{"prompt": "1girl", "cfg_scale": 7}`,
	} {
		resp, err := http.Post(e.ts.URL+"/generate", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, resp.StatusCode)
		}
	}
}
