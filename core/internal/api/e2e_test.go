// End-to-end over the real spine: HTTP → queue (Postgres) → worker →
// fake ComfyUI → compliance hook → blob store → SSE. No GPU involved.
package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	"pulsar/core/internal/nltag"
	"pulsar/core/internal/pgtest"
	"pulsar/core/internal/tags"
	"pulsar/core/internal/worker"
	"pulsar/core/internal/workflow"
)

type env struct {
	ts    *httptest.Server
	srv   *api.Server
	fake  *comfytest.Fake
	blobs *blob.Dir
	pool  *pgxpool.Pool
}

func newEnv(t *testing.T, schema string, checker compliance.Checker) *env {
	t.Helper()
	pool := pgtest.Pool(t, schema)
	store := job.NewStore(pool)
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
		Tags: tags.NewStore(pool), Tpl: tpl,
		Cfg: api.Config{Lineage: "illustrious", Checkpoint: "test-ckpt.safetensors"},
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &env{ts: ts, srv: srv, fake: fake, blobs: blobs, pool: pool}
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

func TestTagsAutocomplete(t *testing.T) {
	e := newEnv(t, "e2e_tags", compliance.Stub{})
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `
		INSERT INTO tags (name, category, post_count, pose_relevant) VALUES
		('silver_hair',  'general', 500000, false),
		('silver_dress', 'general',   3000, false),
		('sitting',      'general', 2000000, true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO tag_aliases (alias, canonical) VALUES ('platinum_hair', 'silver_hair')`); err != nil {
		t.Fatal(err)
	}

	type response struct {
		Tags []struct {
			Canonical string `json:"canonical"`
			Category  string `json:"category"`
			Count     int    `json:"count"`
		} `json:"tags"`
	}
	query := func(t *testing.T, params string) response {
		t.Helper()
		resp, err := http.Get(e.ts.URL + "/tags?" + params)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /tags?%s = %d", params, resp.StatusCode)
		}
		var out response
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	got := query(t, "q=silver")
	if len(got.Tags) != 2 || got.Tags[0].Canonical != "silver_hair" || got.Tags[0].Count != 500000 {
		t.Errorf("q=silver: %+v", got.Tags)
	}
	if got := query(t, "q=platinum"); len(got.Tags) != 1 || got.Tags[0].Canonical != "silver_hair" {
		t.Errorf("alias hit must resolve to canonical: %+v", got.Tags)
	}
	if got := query(t, "q=si&subset=pose"); len(got.Tags) != 1 || got.Tags[0].Canonical != "sitting" {
		t.Errorf("pose subset: %+v", got.Tags)
	}
	if got := query(t, "q="); got.Tags == nil || len(got.Tags) != 0 {
		t.Errorf("empty q must return empty list, got %+v", got.Tags)
	}
	resp, err := http.Get(e.ts.URL + "/tags?q=x&subset=nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad subset = %d, want 400", resp.StatusCode)
	}
}

type fixedDecomposer struct{ concepts []string }

func (f fixedDecomposer) Decompose(context.Context, string) ([]string, error) {
	return f.concepts, nil
}

func TestTranslateEndpoint(t *testing.T) {
	e := newEnv(t, "e2e_translate", compliance.Stub{})
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `
		INSERT INTO tags (name, category, post_count, pose_relevant) VALUES
		('silver_hair', 'general', 500000, false),
		('sitting',     'general', 2000000, true)`); err != nil {
		t.Fatal(err)
	}
	e.srv.Translator = &nltag.Translator{
		Decomposer: fixedDecomposer{concepts: []string{"silver hair", "sitting", "ethereal vibes"}},
		Grounder:   &nltag.Grounder{Store: tags.NewStore(e.pool)},
	}

	post := func(t *testing.T, body string) (*http.Response, []byte) {
		t.Helper()
		resp, err := http.Post(e.ts.URL+"/translate", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp, raw
	}

	resp, raw := post(t, `{"text": "a girl with silver hair sitting"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /translate = %d: %s", resp.StatusCode, raw)
	}
	var result nltag.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tags) != 2 || result.Tags[0].Canonical != "sitting" || result.Tags[1].Canonical != "silver_hair" {
		t.Errorf("tags = %+v, want sitting then silver_hair (count order)", result.Tags)
	}
	if len(result.Dropped) != 1 || result.Dropped[0] != "ethereal vibes" {
		t.Errorf("dropped = %v", result.Dropped)
	}

	// pose subset grounds against the closed vocabulary
	resp, raw = post(t, `{"text": "sitting girl", "subset": "pose"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pose translate = %d: %s", resp.StatusCode, raw)
	}
	result = nltag.Result{}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Tags) != 1 || result.Tags[0].Canonical != "sitting" {
		t.Errorf("pose tags = %+v", result.Tags)
	}

	if resp, _ := post(t, `{"text": ""}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty text = %d, want 400", resp.StatusCode)
	}
	if resp, _ := post(t, `{"text": "x", "subset": "nope"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad subset = %d, want 400", resp.StatusCode)
	}
}

func TestTranslateUnavailable(t *testing.T) {
	e := newEnv(t, "e2e_translate_off", compliance.Stub{}) // no Translator wired
	resp, err := http.Post(e.ts.URL+"/translate", "application/json",
		strings.NewReader(`{"text": "a girl"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("translate without LLM = %d, want 503", resp.StatusCode)
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
