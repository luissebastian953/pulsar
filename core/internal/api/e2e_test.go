// End-to-end over the real spine: HTTP → queue (Postgres) → worker →
// fake ComfyUI → compliance hook → blob store → SSE. No GPU involved.
package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
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
	"pulsar/core/internal/style"
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
	poseTpl, err := workflow.Load("../../../workflows", workflow.TplPose, workflow.PoseSHA256)
	if err != nil {
		t.Fatal(err)
	}
	i2iTpl, err := workflow.Load("../../../workflows", workflow.TplImg2Img, workflow.Img2ImgSHA256)
	if err != nil {
		t.Fatal(err)
	}
	inpTpl, err := workflow.Load("../../../workflows", workflow.TplInpaint, workflow.InpaintSHA256)
	if err != nil {
		t.Fatal(err)
	}
	ipaFrag, err := workflow.LoadFragment("../../../workflows", workflow.FragIPAdapter, workflow.IPAdapterSHA256)
	if err != nil {
		t.Fatal(err)
	}
	ipaCS, err := workflow.LoadFragment("../../../workflows", workflow.FragIPAdapterCS, workflow.IPAdapterCSSHA256)
	if err != nil {
		t.Fatal(err)
	}
	ipa2, err := workflow.LoadFragment("../../../workflows", workflow.FragIPAdapter2, workflow.IPAdapter2SHA256)
	if err != nil {
		t.Fatal(err)
	}
	reg2, err := workflow.LoadFragment("../../../workflows", workflow.FragRegional2, workflow.Regional2SHA256)
	if err != nil {
		t.Fatal(err)
	}
	reg3, err := workflow.LoadFragment("../../../workflows", workflow.FragRegional3, workflow.Regional3SHA256)
	if err != nil {
		t.Fatal(err)
	}
	hires, err := workflow.LoadFragment("../../../workflows", workflow.FragHires, workflow.HiresSHA256)
	if err != nil {
		t.Fatal(err)
	}
	lora, err := workflow.LoadFragment("../../../workflows", workflow.FragLora, workflow.LoraSHA256)
	if err != nil {
		t.Fatal(err)
	}
	lora2, err := workflow.LoadFragment("../../../workflows", workflow.FragLora2, workflow.Lora2SHA256)
	if err != nil {
		t.Fatal(err)
	}
	vpred, err := workflow.LoadFragment("../../../workflows", workflow.FragVpred, workflow.VpredSHA256)
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
		Tags: tags.NewStore(pool), Styles: style.NewStore(pool), Tpl: tpl, PoseTpl: poseTpl, Img2ImgTpl: i2iTpl, InpaintTpl: inpTpl, IPAdapterFrag: ipaFrag, IPAdapter2Frag: ipa2, IPAdapterCSFrag: ipaCS, Regional2Frag: reg2, Regional3Frag: reg3, HiresFrag: hires, LoraFrag: lora, Lora2Frag: lora2, VpredFrag: vpred,
		Cfg: api.Config{
			Lineage: "illustrious", Checkpoint: "test-ckpt.safetensors",
			QualityTags:    "masterpiece, best quality",
			ForbiddenTagUC: "loli, shota, child", // required: Build refuses without it
		},
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
	ID       string            `json:"id"`
	State    string            `json:"state"`
	Params   json.RawMessage   `json:"params"`
	ImageURL string            `json:"image_url"`
	Images   map[string]string `json:"images"`
	Error    string            `json:"error"`
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
	if resp.StatusCode != http.StatusOK || !bytes.Equal(withoutMark(t, "image", img, "txt2img"), e.fake.PNG) {
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

// TestEndToEndPose drives the pose kind: one generation, three delivered
// images (source + extracted lineart + skeleton), each stored under its own
// blob and each traceable to the right SaveImage node.
func TestEndToEndPose(t *testing.T) {
	e := newEnv(t, "e2e_pose", compliance.Stub{})
	e.fake.TagOutputs = true
	id := e.generate(t, `{"prompt": "1girl, standing", "seed": 7, "kind": "pose"}`)

	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s), want done", v.State, v.Error)
	}
	if v.ImageURL != "/images/"+id {
		t.Errorf("image_url = %q", v.ImageURL)
	}

	// every role delivered, each from its own SaveImage node
	wantNode := map[string]string{
		"source":   workflow.Pose.SaveSource,
		"lineart":  workflow.Pose.SaveLineart,
		"skeleton": workflow.Pose.SaveSkeleton,
	}
	if len(v.Images) != len(wantNode) {
		t.Fatalf("images = %v", v.Images)
	}
	for role, node := range wantNode {
		url, ok := v.Images[role]
		if !ok {
			t.Fatalf("role %s missing from images: %v", role, v.Images)
		}
		resp, err := http.Get(e.ts.URL + url)
		if err != nil {
			t.Fatal(err)
		}
		img, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d", url, resp.StatusCode)
		}
		if !bytes.Equal(withoutMark(t, role, img, "pose"), e.fake.OutputFor("node-"+node+".png")) {
			t.Errorf("role %s did not come from node %s", role, node)
		}
	}

	// the extractors were patched to the render's short side
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	if got := g[workflow.Pose.Lineart].Inputs["resolution"]; got != float64(832) {
		t.Errorf("lineart resolution = %v, want 832", got)
	}
}

// TestEndToEndImg2Img: multipart upload → normalize → CheckUpload → blob,
// then a generation whose input is forwarded to the executing process and
// whose graph loads it by name.
func TestEndToEndImg2Img(t *testing.T) {
	e := newEnv(t, "e2e_i2i", compliance.Stub{})

	// a 100x150 png — should normalize to the 832x1216 bucket
	src := image.NewRGBA(image.Rect(0, 0, 100, 150))
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	fw, _ := mw.CreateFormFile("image", "in.png")
	fw.Write(buf.Bytes())
	mw.Close()
	resp, err := http.Post(e.ts.URL+"/uploads", mw.FormDataContentType(), &form)
	if err != nil {
		t.Fatal(err)
	}
	var up struct {
		ImageID string `json:"image_id"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&up); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || up.ImageID == "" {
		t.Fatalf("upload = %d %+v", resp.StatusCode, up)
	}
	if up.Width != 832 || up.Height != 1216 {
		t.Errorf("normalized to %dx%d, want 832x1216", up.Width, up.Height)
	}

	id := e.generate(t, `{"prompt": "1girl, red dress", "seed": 5, "kind": "img2img", "image_id": "`+up.ImageID+`"}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}

	// the normalized input reached the executing process under its blob name
	fwd, ok := e.fake.Upload(up.ImageID + ".png")
	if !ok {
		t.Fatal("input was not forwarded to comfy")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(fwd))
	if err != nil || cfg.Width != 832 || cfg.Height != 1216 {
		t.Errorf("forwarded image %dx%d, %v", cfg.Width, cfg.Height, err)
	}

	// the graph loads that name and carries the default denoise
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	if got := g[workflow.Img2Img.LoadImage].Inputs["image"]; got != up.ImageID+".png" {
		t.Errorf("LoadImage = %v", got)
	}
	if got := g[workflow.Img2Img.Sampler].Inputs["denoise"]; got != 0.6 {
		t.Errorf("denoise = %v, want 0.6", got)
	}

	// stored params carry the resolved denoise (reproducibility)
	var spec assembly.Spec
	json.Unmarshal(v.Params, &spec)
	if spec.Denoise != 0.6 {
		t.Errorf("stored denoise = %v", spec.Denoise)
	}
}

// uploadPNG posts a wxh image through /uploads and returns its id.
func (e *env) uploadPNG(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	fw, _ := mw.CreateFormFile("image", "in.png")
	fw.Write(buf.Bytes())
	mw.Close()
	resp, err := http.Post(e.ts.URL+"/uploads", mw.FormDataContentType(), &form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var up struct {
		ImageID string `json:"image_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&up); err != nil || up.ImageID == "" {
		t.Fatalf("upload failed: %d %v", resp.StatusCode, err)
	}
	return up.ImageID
}

func TestEndToEndInpaint(t *testing.T) {
	e := newEnv(t, "e2e_inpaint", compliance.Stub{})
	img := e.uploadPNG(t, 100, 150)  // → 832x1216
	mask := e.uploadPNG(t, 200, 300) // same aspect → same bucket

	id := e.generate(t, `{"prompt": "1girl, blonde hair", "seed": 9, "kind": "inpaint", "image_id": "`+img+`", "mask_id": "`+mask+`"}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}

	// both files forwarded to the executing process
	for _, name := range []string{img + ".png", mask + ".png"} {
		if _, ok := e.fake.Upload(name); !ok {
			t.Errorf("%s was not forwarded", name)
		}
	}
	// graph: both LoadImages patched, inpaint default denoise
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	if got := g[workflow.Inpaint.LoadMask].Inputs["image"]; got != mask+".png" {
		t.Errorf("mask load = %v", got)
	}
	if got := g[workflow.Inpaint.Sampler].Inputs["denoise"]; got != 0.85 {
		t.Errorf("denoise = %v, want 0.85", got)
	}
}

func TestInpaintMaskDimsMustMatch(t *testing.T) {
	e := newEnv(t, "e2e_inpaint_dims", compliance.Stub{})
	img := e.uploadPNG(t, 100, 150)  // → 832x1216
	mask := e.uploadPNG(t, 300, 300) // → 1024x1024: mismatch
	resp, err := http.Post(e.ts.URL+"/generate", "application/json",
		strings.NewReader(`{"prompt": "1girl", "kind": "inpaint", "image_id": "`+img+`", "mask_id": "`+mask+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("dims mismatch = %d, want 400", resp.StatusCode)
	}
}

// TestEndToEndReference: the IP-Adapter fragment composes onto txt2img —
// nodes added, KSampler rewired, reference forwarded to the process.
func TestEndToEndReference(t *testing.T) {
	e := newEnv(t, "e2e_ref", compliance.Stub{})
	ref := e.uploadPNG(t, 100, 150)

	id := e.generate(t, `{"prompt": "1girl, cafe", "seed": 3, "reference_id": "`+ref+`", "ref_strength": 0.9}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}
	if _, ok := e.fake.Upload(ref + ".png"); !ok {
		t.Error("reference was not forwarded")
	}
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	if got := g[workflow.IPAdapter.LoadRef].Inputs["image"]; got != ref+".png" {
		t.Errorf("ref load = %v", got)
	}
	if got := g[workflow.IPAdapter.Apply].Inputs["weight"]; got != 0.9 {
		t.Errorf("weight = %v, want 0.9", got)
	}
	// KSampler now takes its model from the adapter chain
	ref2, ok := g[workflow.Txt2Img.Sampler].Inputs["model"].([]any)
	if !ok || ref2[0] != workflow.IPAdapter.Apply {
		t.Errorf("KSampler model = %v, want link to node %s", g[workflow.Txt2Img.Sampler].Inputs["model"], workflow.IPAdapter.Apply)
	}
	// stored params carry the resolved strength
	var spec assembly.Spec
	json.Unmarshal(v.Params, &spec)
	if spec.RefStrength != 0.9 {
		t.Errorf("stored ref_strength = %v", spec.RefStrength)
	}
}

func TestReferenceComposesOnImg2Img(t *testing.T) {
	e := newEnv(t, "e2e_ref_i2i", compliance.Stub{})
	img := e.uploadPNG(t, 100, 150)
	ref := e.uploadPNG(t, 100, 150)
	id := e.generate(t, `{"prompt": "1girl", "seed": 4, "kind": "img2img", "image_id": "`+img+`", "reference_id": "`+ref+`"}`)
	if v := e.waitTerminal(t, id); v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}
	for _, name := range []string{img + ".png", ref + ".png"} {
		if _, ok := e.fake.Upload(name); !ok {
			t.Errorf("%s not forwarded", name)
		}
	}
	var g workflow.Graph
	json.Unmarshal(e.fake.LastGraph(), &g)
	if got := g[workflow.IPAdapter.Apply].Inputs["weight"]; got != 0.8 {
		t.Errorf("default weight = %v, want 0.8", got)
	}
}

// TestEndToEndRegional: two characters, auto column masks generated
// server-side, forwarded, and wired into per-region conditioning.
func TestEndToEndRegional(t *testing.T) {
	e := newEnv(t, "e2e_regional", compliance.Stub{})
	id := e.generate(t, `{"prompt": "2girls, park", "seed": 6, "characters": [{"prompt": "blonde hair, red dress"}, {"prompt": "black hair, blue jacket"}]}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	// per-slot prompts wired, masks forwarded under their patched names
	for i, want := range []string{"blonde hair, red dress", "black hair, blue jacket"} {
		slot := workflow.RegionalSlots[i]
		if got := g[slot.Text].Inputs["text"]; got != want {
			t.Errorf("slot %d text = %v", i, got)
		}
		name, _ := g[slot.LoadMask].Inputs["image"].(string)
		data, ok := e.fake.Upload(name)
		if !ok {
			t.Fatalf("mask %s not forwarded", name)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width != 832 || cfg.Height != 1216 {
			t.Errorf("mask %d dims %dx%d %v", i, cfg.Width, cfg.Height, err)
		}
	}
	// KSampler positive rewired to the combine chain
	ref, ok := g[workflow.Txt2Img.Sampler].Inputs["positive"].([]any)
	if !ok || ref[0] != "39" {
		t.Errorf("positive = %v, want link to 39", g[workflow.Txt2Img.Sampler].Inputs["positive"])
	}
	// resolved params store the characters
	var spec assembly.Spec
	json.Unmarshal(v.Params, &spec)
	if len(spec.Characters) != 2 || spec.Characters[0].Prompt != "blonde hair, red dress" {
		t.Errorf("stored characters = %+v", spec.Characters)
	}
}

// TestEndToEndPerCharacterReferences: the differentiator — each character
// gets its own reference, masked to its own column, no blending.
func TestEndToEndPerCharacterReferences(t *testing.T) {
	e := newEnv(t, "e2e_charrefs", compliance.Stub{})
	refA := e.uploadPNG(t, 100, 150)
	refB := e.uploadPNG(t, 100, 150)

	id := e.generate(t, `{"prompt": "2girls, park", "seed": 8, "characters": [
		{"prompt": "blonde hair", "reference_id": "`+refA+`", "ref_strength": 0.9},
		{"prompt": "black hair", "reference_id": "`+refB+`"}]}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}
	// both refs forwarded alongside the two masks
	for _, name := range []string{refA + ".png", refB + ".png"} {
		if _, ok := e.fake.Upload(name); !ok {
			t.Errorf("%s not forwarded", name)
		}
	}
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	// per-slot refs and weights patched; explicit weight then the default
	if got := g[workflow.IPAdapter2.LoadRef[0]].Inputs["image"]; got != refA+".png" {
		t.Errorf("ref A = %v", got)
	}
	if got := g[workflow.IPAdapter2.Apply[0]].Inputs["weight"]; got != 0.9 {
		t.Errorf("weight A = %v, want 0.9", got)
	}
	if got := g[workflow.IPAdapter2.Apply[1]].Inputs["weight"]; got != 0.8 {
		t.Errorf("weight B = %v, want default 0.8", got)
	}
	// each adapter attends only its own column's mask
	for i, wantMask := range []string{"31", "35"} {
		ref, ok := g[workflow.IPAdapter2.Apply[i]].Inputs["attn_mask"].([]any)
		if !ok || ref[0] != wantMask {
			t.Errorf("adapter %d attn_mask = %v, want link to %s", i, g[workflow.IPAdapter2.Apply[i]].Inputs["attn_mask"], wantMask)
		}
	}
	// KSampler model rides the chained adapters
	ref, ok := g[workflow.Txt2Img.Sampler].Inputs["model"].([]any)
	if !ok || ref[0] != workflow.IPAdapter2.Apply[1] {
		t.Errorf("model = %v", g[workflow.Txt2Img.Sampler].Inputs["model"])
	}
}

// TestCharacterStyleMode: the NAI "Character & Style" chain — identity +
// style-transfer adapters on one reference, fidelity mapped to end_at.
func TestCharacterStyleMode(t *testing.T) {
	e := newEnv(t, "e2e_csmode", compliance.Stub{})
	ref := e.uploadPNG(t, 100, 150)
	id := e.generate(t, `{"prompt": "1girl", "seed": 14, "reference_id": "`+ref+`", "ref_mode": "character_style", "ref_strength": 0.6, "ref_fidelity": 1.0}`)
	if v := e.waitTerminal(t, id); v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, node := range workflow.IPAdapterCS.Apply {
		wt, _ := g[node].Inputs["weight_type"].(string)
		types[wt] = true
		if got := g[node].Inputs["weight"]; got != 0.6 {
			t.Errorf("node %s weight = %v", node, got)
		}
		if got := g[node].Inputs["end_at"]; got != 1.0 {
			t.Errorf("node %s end_at = %v, want 1.0 (fidelity 1)", node, got)
		}
	}
	if !types["linear"] || !types["style transfer"] {
		t.Errorf("chain types = %v, want linear + style transfer", types)
	}
	if m, ok := g[workflow.Txt2Img.Sampler].Inputs["model"].([]any); !ok || m[0] != workflow.IPAdapterCS.Apply[1] {
		t.Errorf("model = %v", g[workflow.Txt2Img.Sampler].Inputs["model"])
	}
}

func TestPerCharacterReferenceRules(t *testing.T) {
	e := newEnv(t, "e2e_charrefs_rules", compliance.Stub{})
	ref := e.uploadPNG(t, 100, 150)
	for name, body := range map[string]string{
		"mixed global and per-char": `{"prompt": "2girls", "reference_id": "` + ref + `", "characters": [{"prompt": "a", "reference_id": "` + ref + `"}, {"prompt": "b", "reference_id": "` + ref + `"}]}`,
		"only one char has ref":     `{"prompt": "2girls", "characters": [{"prompt": "a", "reference_id": "` + ref + `"}, {"prompt": "b"}]}`,
	} {
		resp, err := http.Post(e.ts.URL+"/generate", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, resp.StatusCode)
		}
	}
}

// TestEndToEndHiresAndStyle: hires mirrors the base sampler's links so the
// style-mode reference adapter carries into the refine pass.
func TestEndToEndHiresAndStyle(t *testing.T) {
	e := newEnv(t, "e2e_hires", compliance.Stub{})
	ref := e.uploadPNG(t, 100, 150)
	id := e.generate(t, `{"prompt": "1girl, cafe", "seed": 12, "hires": true, "reference_id": "`+ref+`", "ref_mode": "style", "ref_strength": 0.7}`)
	if v := e.waitTerminal(t, id); v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	if got := g[workflow.IPAdapter.Apply].Inputs["weight_type"]; got != "style transfer" {
		t.Errorf("weight_type = %v", got)
	}
	// default fidelity 0.5 → end_at 0.7
	if got := g[workflow.IPAdapter.Apply].Inputs["end_at"]; got != 0.7 {
		t.Errorf("end_at = %v, want 0.7", got)
	}
	// decode reads the refine sampler, whose model rides the adapter
	if ref2, ok := g["6"].Inputs["samples"].([]any); !ok || ref2[0] != workflow.Hires.Sampler {
		t.Errorf("decode samples = %v", g["6"].Inputs["samples"])
	}
	if m, ok := g[workflow.Hires.Sampler].Inputs["model"].([]any); !ok || m[0] != workflow.IPAdapter.Apply {
		t.Errorf("hires model = %v, want adapter chain", g[workflow.Hires.Sampler].Inputs["model"])
	}
	if got := g[workflow.Hires.Sampler].Inputs["denoise"]; got != 0.4 {
		t.Errorf("hires denoise = %v", got)
	}
}

// TestEndToEndLora: a LoRA composes onto the spine — loader patched, and
// the sampler + text encoders route through it.
func TestEndToEndCheckpointOverride(t *testing.T) {
	e := newEnv(t, "e2e_ckpt", compliance.Stub{})
	e.srv.CheckpointDir = "" // no dir listed → only the default validates
	// unknown checkpoint is rejected
	resp, err := http.Post(e.ts.URL+"/generate", "application/json",
		strings.NewReader(`{"prompt": "1girl", "checkpoint": "nope.safetensors"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown checkpoint = %d, want 400", resp.StatusCode)
	}
	// the configured default is accepted and recorded in params
	id := e.generate(t, `{"prompt": "1girl", "seed": 5, "checkpoint": "test-ckpt.safetensors"}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}
	var g workflow.Graph
	json.Unmarshal(e.fake.LastGraph(), &g)
	if got := g[workflow.Txt2Img.Checkpoint].Inputs["ckpt_name"]; got != "test-ckpt.safetensors" {
		t.Errorf("submitted ckpt = %v", got)
	}
	var spec assembly.Spec
	json.Unmarshal(v.Params, &spec)
	if spec.Checkpoint != "test-ckpt.safetensors" {
		t.Errorf("stored checkpoint = %q", spec.Checkpoint)
	}
}

func TestEndToEndLora(t *testing.T) {
	e := newEnv(t, "e2e_lora", compliance.Stub{})
	id := e.generate(t, `{"prompt": "1girl", "seed": 15, "loras": [{"name": "style.safetensors", "strength": 0.7}]}`)
	if v := e.waitTerminal(t, id); v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	if got := g[workflow.Lora.Loader].Inputs["lora_name"]; got != "style.safetensors" {
		t.Errorf("lora_name = %v", got)
	}
	if got := g[workflow.Lora.Loader].Inputs["strength_model"]; got != 0.7 {
		t.Errorf("strength = %v", got)
	}
	// sampler model and both text encoders route through the loader
	if m, ok := g[workflow.Txt2Img.Sampler].Inputs["model"].([]any); !ok || m[0] != workflow.Lora.Loader {
		t.Errorf("sampler model = %v", g[workflow.Txt2Img.Sampler].Inputs["model"])
	}
	if c, ok := g[workflow.Txt2Img.Positive].Inputs["clip"].([]any); !ok || c[0] != workflow.Lora.Loader {
		t.Errorf("positive clip = %v", g[workflow.Txt2Img.Positive].Inputs["clip"])
	}
}

// TestEndToEndVpredLineage: a per-request noobai lineage swaps in the v-pred
// defaults and composes the ModelSamplingDiscrete + RescaleCFG wrap; the
// default illustrious path never gets it.
func TestEndToEndVpredLineage(t *testing.T) {
	e := newEnv(t, "e2e_vpred", compliance.Stub{})
	id := e.generate(t, `{"prompt": "1girl", "seed": 21, "lineage": "noobai"}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s)", v.State, v.Error)
	}

	// stored params carry the v-pred lineage defaults (reproducibility)
	var spec assembly.Spec
	json.Unmarshal(v.Params, &spec)
	if spec.Lineage != "noobai" || spec.CFG != 5.0 || spec.Sampler != "euler" || spec.CFGRescale != 0.55 {
		t.Errorf("stored spec = lineage %q cfg %v sampler %q rescale %v",
			spec.Lineage, spec.CFG, spec.Sampler, spec.CFGRescale)
	}

	// the submitted graph carries the wrap: MSD → RescaleCFG → KSampler
	var g workflow.Graph
	if err := json.Unmarshal(e.fake.LastGraph(), &g); err != nil {
		t.Fatal(err)
	}
	msd := g[workflow.Vpred.Sampling]
	if msd == nil || msd.Inputs["sampling"] != "v_prediction" || msd.Inputs["zsnr"] != true {
		t.Errorf("ModelSamplingDiscrete missing/misconfigured: %+v", msd)
	}
	if got := g[workflow.Vpred.Rescale].Inputs["multiplier"]; got != 0.55 {
		t.Errorf("rescale multiplier = %v, want the noobai default 0.55 (docs/tuning.md)", got)
	}
	if m, ok := g[workflow.Txt2Img.Sampler].Inputs["model"].([]any); !ok || m[0] != workflow.Vpred.Rescale {
		t.Errorf("KSampler model = %v, want the v-pred wrap", g[workflow.Txt2Img.Sampler].Inputs["model"])
	}

	// default lineage (illustrious) never composes the wrap
	id2 := e.generate(t, `{"prompt": "1girl", "seed": 22}`)
	if v := e.waitTerminal(t, id2); v.State != "done" {
		t.Fatalf("eps state = %s (%s)", v.State, v.Error)
	}
	var g2 workflow.Graph
	json.Unmarshal(e.fake.LastGraph(), &g2)
	if _, exists := g2[workflow.Vpred.Sampling]; exists {
		t.Error("eps lineage must not get the ModelSamplingDiscrete wrap")
	}
	if m, ok := g2[workflow.Txt2Img.Sampler].Inputs["model"].([]any); !ok || m[0] != workflow.Txt2Img.Checkpoint {
		t.Errorf("eps KSampler model = %v, want raw checkpoint", g2[workflow.Txt2Img.Sampler].Inputs["model"])
	}
}

func TestLoraRejectsWithReference(t *testing.T) {
	e := newEnv(t, "e2e_lora_ref", compliance.Stub{})
	ref := e.uploadPNG(t, 100, 150)
	resp, err := http.Post(e.ts.URL+"/generate", "application/json",
		strings.NewReader(`{"prompt": "1girl", "reference_id": "`+ref+`", "loras": [{"name": "x.safetensors"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("lora+reference = %d, want 400", resp.StatusCode)
	}
}

func TestRegionalValidation(t *testing.T) {
	e := newEnv(t, "e2e_regional_val", compliance.Stub{})
	resp, err := http.Post(e.ts.URL+"/generate", "application/json",
		strings.NewReader(`{"prompt": "1girl", "characters": [{"prompt": "solo"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("1 character = %d, want 400", resp.StatusCode)
	}
}

func TestGenerateImg2ImgRequiresUpload(t *testing.T) {
	e := newEnv(t, "e2e_i2i_noupload", compliance.Stub{})
	resp, err := http.Post(e.ts.URL+"/generate", "application/json",
		strings.NewReader(`{"prompt": "1girl", "kind": "img2img"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing image_id = %d, want 400", resp.StatusCode)
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

// TestLiveSentinelVerdicts drives the full spine against a REAL running
// sentinel with real generated images (opt-in: set SENTINEL_LIVE_URL plus
// the two image paths). The known child-like output must reach blocked;
// the known adult output must reach done.
func TestLiveSentinelVerdicts(t *testing.T) {
	url := os.Getenv("SENTINEL_LIVE_URL")
	blockImg := os.Getenv("SENTINEL_LIVE_BLOCK_IMAGE")
	allowImg := os.Getenv("SENTINEL_LIVE_ALLOW_IMAGE")
	if url == "" || blockImg == "" || allowImg == "" {
		t.Skip("SENTINEL_LIVE_URL / _BLOCK_IMAGE / _ALLOW_IMAGE not set")
	}
	checker := compliance.NewSentinel(url, "loli, shota, child, toddler, aged down", "", 0, 0)

	for _, tc := range []struct {
		name, img, want string
	}{
		{"child_like_blocks", blockImg, "blocked"},
		{"adult_passes", allowImg, "done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, "live_sentinel_"+tc.name, checker)
			png, err := os.ReadFile(tc.img)
			if err != nil {
				t.Fatal(err)
			}
			e.fake.PNG = png
			id := e.generate(t, `{"prompt": "1girl, standing", "seed": 1}`)
			if v := e.waitTerminal(t, id); v.State != tc.want {
				t.Errorf("state = %s (%s), want %s", v.State, v.Error, tc.want)
			}
		})
	}
}

// TestEndToEndStylePreset: a style merges its recipe in core — carriers
// lead, finish trails, LoRA and negative attach, settings fill what the
// request left unset — and the resolved values land on the job row.
func TestEndToEndStylePreset(t *testing.T) {
	e := newEnv(t, "e2e_style", compliance.Stub{})
	id := e.generate(t, `{"prompt": "acheron (honkai: star rail), 1girl", "negative": "blurry", "seed": 7, "lineage": "noobai", "style": "hsr", "hires": true}`)
	v := e.waitTerminal(t, id)
	if v.State != "done" {
		t.Fatalf("state = %s (%s), want done", v.State, v.Error)
	}
	var spec assembly.Spec
	if err := json.Unmarshal(v.Params, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Style != "hsr" {
		t.Errorf("style = %q", spec.Style)
	}
	// the series qualifier's parens are escaped: ComfyUI must read them as
	// text, not as a ×1.1 group (reports/NovelAI prompt weighting on SDXL.md S5)
	if want := `masterpiece, best quality, official art, game cg, acheron \(honkai: star rail\), 1girl, painterly`; !strings.HasPrefix(spec.Prompt, want) {
		t.Errorf("prompt = %q, want prefix %q", spec.Prompt, want)
	}
	if !strings.HasPrefix(spec.Negative, "loli, shota, child, blurry, (logo:1.3)") {
		t.Errorf("negative = %q: safety first, then the user's, then the style's", spec.Negative)
	}
	if len(spec.Loras) != 1 || spec.Loras[0].Name != "honkai star rail_v3.safetensors" || *spec.Loras[0].Strength != 0.8 {
		t.Errorf("loras = %+v", spec.Loras)
	}
	if spec.CFGRescale != 0.45 || spec.Steps != 28 || spec.CFG != 5 || spec.Sampler != "euler" || !spec.Hires || spec.HiresScale != 1.5 || spec.HiresDenoise != 0.55 {
		t.Errorf("recipe settings: %+v", spec)
	}

	// the request's explicit values win over the recipe
	id = e.generate(t, `{"prompt": "1girl", "seed": 7, "lineage": "noobai", "style": "hsr", "cfg_rescale": 0.7, "steps": 20}`)
	v = e.waitTerminal(t, id)
	spec = assembly.Spec{} // Unmarshal leaves omitted fields as they were
	if err := json.Unmarshal(v.Params, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.CFGRescale != 0.7 || spec.Steps != 20 || spec.Hires {
		t.Errorf("request must win: rescale %v steps %d hires %v", spec.CFGRescale, spec.Steps, spec.Hires)
	}

	// a recipe is per lineage: hsr exists for noobai only (default lineage here is illustrious)
	resp, err := http.Post(e.ts.URL+"/generate", "application/json", strings.NewReader(`{"prompt": "1girl", "style": "hsr"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "illustrious") {
		t.Errorf("wrong-lineage style = %d %s, want 400 naming the lineage", resp.StatusCode, body)
	}

	// a tag-only style runs on either lineage
	id = e.generate(t, `{"prompt": "1girl", "seed": 7, "style": "chibi"}`)
	v = e.waitTerminal(t, id)
	spec = assembly.Spec{}
	if err := json.Unmarshal(v.Params, &spec); err != nil {
		t.Fatal(err)
	}
	if v.State != "done" || spec.Style != "chibi" || !strings.HasPrefix(spec.Prompt, "masterpiece, best quality, chibi, 1girl") || len(spec.Loras) != 0 {
		t.Errorf("chibi on illustrious: %s %+v", v.State, spec)
	}
}

// TestStylesEndpoint: GET /styles lists the seeded presets for every lineage
// with availability (no LORA_DIR configured → lenient, all available).
func TestStylesEndpoint(t *testing.T) {
	e := newEnv(t, "e2e_styles_list", compliance.Stub{})
	resp, err := http.Get(e.ts.URL + "/styles")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Styles []struct {
			Slug      string   `json:"slug"`
			Lineage   string   `json:"lineage"`
			Family    string   `json:"family"`
			Lora      string   `json:"lora"`
			Available bool     `json:"available"`
			Hires     *bool    `json:"hires"`
			Rescale   *float64 `json:"cfg_rescale"`
		} `json:"styles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, s := range out.Styles {
		seen[s.Slug]++
		if !s.Available {
			t.Errorf("%s/%s unavailable without a LORA_DIR", s.Slug, s.Lineage)
		}
		if s.Slug == "hsr" {
			if s.Lineage != "noobai" || s.Lora == "" || s.Hires == nil || !*s.Hires || s.Rescale == nil || *s.Rescale != 0.45 {
				t.Errorf("hsr row = %+v", s)
			}
		}
	}
	if seen["hsr"] != 1 || seen["game-cg"] != 2 || seen["chibi"] != 2 {
		t.Errorf("seed rows per slug = %v", seen)
	}
}

// 12x8 lossless WebP (left red, right blue) — see ingest_test.go.
const webpFixtureB64 = "UklGRigAAABXRUJQVlA4TBwAAAAvC8ABAA8wHvdQvRXy/EcLBAJJZvuTrRDR/8gB"

// uploadChecker records what CheckUpload saw and can refuse it.
type uploadChecker struct {
	compliance.Stub
	mu     sync.Mutex
	seen   [][]byte
	reject bool
}

func (u *uploadChecker) CheckUpload(_ context.Context, img []byte) (compliance.Decision, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.seen = append(u.seen, append([]byte{}, img...))
	if u.reject {
		return compliance.Decision{Reason: "test"}, nil
	}
	return compliance.Decision{Allowed: true}, nil
}

func (e *env) uploadBytes(t *testing.T, name string, data []byte) (int, string) {
	t.Helper()
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	fw, _ := mw.CreateFormFile("image", name)
	fw.Write(data)
	mw.Close()
	resp, err := http.Post(e.ts.URL+"/uploads", mw.FormDataContentType(), &form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var up struct {
		ImageID string `json:"image_id"`
	}
	json.NewDecoder(resp.Body).Decode(&up)
	return resp.StatusCode, up.ImageID
}

// TestWebPUploadIsOneIngestPath: a WebP goes through POST /uploads like any
// image — normalized to a bucket PNG, then CheckUpload on those normalized
// bytes, fail closed — and works as an img2img base.
func TestWebPUploadIsOneIngestPath(t *testing.T) {
	chk := &uploadChecker{}
	e := newEnv(t, "e2e_webp_upload", chk)
	src, err := base64.StdEncoding.DecodeString(webpFixtureB64)
	if err != nil {
		t.Fatal(err)
	}
	status, id := e.uploadBytes(t, "in.webp", src)
	if status != http.StatusCreated || id == "" {
		t.Fatalf("webp upload = %d", status)
	}
	if len(chk.seen) != 1 {
		t.Fatalf("CheckUpload ran %d times, want 1", len(chk.seen))
	}
	stored, err := os.ReadFile(e.blobs.Root + "/" + id + ".png")
	if err != nil {
		t.Fatal(err)
	}
	// the checker saw the normalized PNG that was stored, not the WebP
	if !bytes.Equal(chk.seen[0], stored) || bytes.Contains(stored, []byte("WEBP")) {
		t.Error("CheckUpload did not see the stored, normalized bytes")
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(stored)); err != nil || format != "png" || cfg.Width != 1216 || cfg.Height != 832 {
		t.Errorf("stored blob = %s %dx%d (%v), want png 1216x832", format, cfg.Width, cfg.Height, err)
	}
	job := e.generate(t, `{"prompt": "1girl", "seed": 5, "kind": "img2img", "width": 1216, "height": 832, "image_id": "`+id+`"}`)
	if v := e.waitTerminal(t, job); v.State != "done" {
		t.Fatalf("img2img on a webp upload: state = %s (%s)", v.State, v.Error)
	}

	// a refused upload stores nothing
	chk.reject = true
	before, _ := os.ReadDir(e.blobs.Root)
	if status, id := e.uploadBytes(t, "in.webp", src); status != http.StatusUnprocessableEntity || id != "" {
		t.Errorf("rejected webp upload = %d %q, want 422", status, id)
	}
	if after, _ := os.ReadDir(e.blobs.Root); len(after) != len(before) {
		t.Error("a rejected upload was stored")
	}
	if len(chk.seen) != 2 {
		t.Errorf("CheckUpload ran %d times, want 2", len(chk.seen))
	}
}
