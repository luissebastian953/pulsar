package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"pulsar/core/internal/assembly"
)

const workflowsDir = "../../../workflows"

func loadTpl(t *testing.T) Graph {
	t.Helper()
	g, err := Load(workflowsDir, TplTxt2Img, Txt2ImgSHA256)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestLoadVerifiesHash(t *testing.T) {
	if _, err := Load(workflowsDir, TplTxt2Img, "sha256:deadbeef"); err == nil {
		t.Fatal("hash mismatch must fail loudly")
	}
}

func TestLoadRejectsEditorFormat(t *testing.T) {
	// editor exports have top-level metadata, not id → {class_type, inputs}
	raw := []byte(`{"last_node_id": 7, "nodes": [], "links": []}`)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "editor.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	if _, err := Load(dir, "editor.json", hash); err == nil {
		t.Fatal("editor-format JSON must be rejected")
	}
}

func spec(t *testing.T) assembly.Spec {
	t.Helper()
	s, err := assembly.Build(assembly.Request{
		Prompt:   "1girl, silver hair",
		Negative: "extra fingers",
		Width:    1024, Height: 1024,
		Steps: 28, CFG: 5.5,
		Sampler: "dpmpp_2m", Scheduler: "karras",
	}, assembly.Options{Lineage: "illustrious", Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuildTxt2ImgPatchesByNodeID(t *testing.T) {
	raw, err := BuildTxt2Img(loadTpl(t), spec(t), "test-ckpt.safetensors")
	if err != nil {
		t.Fatal(err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		node, input string
		want        any
	}{
		{Txt2Img.Checkpoint, "ckpt_name", "test-ckpt.safetensors"},
		{Txt2Img.Positive, "text", "masterpiece, best quality, very aesthetic, absurdres, 1girl, silver hair"},
		{Txt2Img.Latent, "width", float64(1024)},
		{Txt2Img.Latent, "height", float64(1024)},
		{Txt2Img.Latent, "batch_size", float64(1)},
		{Txt2Img.Sampler, "seed", float64(42)},
		{Txt2Img.Sampler, "steps", float64(28)},
		{Txt2Img.Sampler, "cfg", 5.5},
		{Txt2Img.Sampler, "sampler_name", "dpmpp_2m"},
		{Txt2Img.Sampler, "scheduler", "karras"},
		{Txt2Img.Sampler, "denoise", float64(1)}, // untouched template value survives
	}
	for _, c := range checks {
		got := g[c.node].Inputs[c.input]
		if got != c.want {
			t.Errorf("node %s input %s = %v (%T), want %v", c.node, c.input, got, got, c.want)
		}
	}
	if got := g[Txt2Img.Negative].Inputs["text"]; got == "" {
		t.Error("negative text not patched")
	}
	// graph wiring untouched: KSampler still reads from the latent node
	if link := g[Txt2Img.Sampler].Inputs["latent_image"].([]any); link[0] != Txt2Img.Latent {
		t.Errorf("sampler latent link rewired to %v", link)
	}
}

func TestBuildDoesNotMutateTemplate(t *testing.T) {
	tpl := loadTpl(t)
	if _, err := BuildTxt2Img(tpl, spec(t), "ckpt-a"); err != nil {
		t.Fatal(err)
	}
	if got := tpl[Txt2Img.Positive].Inputs["text"]; got != "" {
		t.Errorf("template mutated by build: positive text = %q", got)
	}
	if got := tpl[Txt2Img.Checkpoint].Inputs["ckpt_name"]; got != "illustrious-xl.safetensors" {
		t.Errorf("template mutated by build: ckpt = %q", got)
	}
}

func TestSetUnknownNode(t *testing.T) {
	if err := loadTpl(t).Set("99", "text", "x"); err == nil {
		t.Fatal("setting a missing node must error, not create it")
	}
}
