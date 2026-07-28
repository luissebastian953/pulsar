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
	}, assembly.Options{
		Lineage: "illustrious", Seed: 42,
		QualityTags: "masterpiece, best quality", ForbiddenTagUC: "loli, shota, child",
	})
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
		{Txt2Img.Positive, "text", "masterpiece, best quality, 1girl, silver hair"},
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

func TestBuildPosePatchesSpineAndExtractors(t *testing.T) {
	tpl, err := Load(workflowsDir, TplPose, PoseSHA256)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := BuildPose(tpl, spec(t), "test-ckpt.safetensors")
	if err != nil {
		t.Fatal(err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	// spine patched exactly like txt2img
	if got := g[Pose.Sampler].Inputs["seed"]; got != float64(42) {
		t.Errorf("seed = %v", got)
	}
	if got := g[Pose.Checkpoint].Inputs["ckpt_name"]; got != "test-ckpt.safetensors" {
		t.Errorf("ckpt = %v", got)
	}
	// extractors follow the render's short side (1024×1024 → 1024)
	for _, node := range []string{Pose.Lineart, Pose.Openpose} {
		if got := g[node].Inputs["resolution"]; got != float64(1024) {
			t.Errorf("node %s resolution = %v, want 1024", node, got)
		}
	}
	// extraction is deterministic post-processing of the decoded image, not
	// prompted — both preprocessors must read from the VAE decode
	for _, node := range []string{Pose.Lineart, Pose.Openpose} {
		ref, ok := g[node].Inputs["image"].([]any)
		if !ok || len(ref) != 2 || ref[0] != Pose.Decode {
			t.Errorf("node %s image input = %v, want link to node %s", node, g[node].Inputs["image"], Pose.Decode)
		}
	}
	// delivered lineart is inverted to black-on-white — the save must read
	// through the invert, not the raw conditioning output
	if ref, ok := g[Pose.SaveLineart].Inputs["images"].([]any); !ok || ref[0] != Pose.LineartInvert {
		t.Errorf("lineart save reads %v, want the invert node %s", g[Pose.SaveLineart].Inputs["images"], Pose.LineartInvert)
	}
}

func loadFrag(t *testing.T, name, sha string) *Fragment {
	t.Helper()
	f, err := LoadFragment(workflowsDir, name, sha)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func firstLink(t *testing.T, g Graph, node, input string) string {
	t.Helper()
	link, ok := g[node].Inputs[input].([]any)
	if !ok || len(link) == 0 {
		t.Fatalf("node %s input %s is not a link: %v", node, input, g[node].Inputs[input])
	}
	return link[0].(string)
}

// TestApplyVpredWrapsFinalModel: on a bare spine, the v-pred fragment wraps
// the checkpoint's model in ModelSamplingDiscrete → RescaleCFG → KSampler,
// with sampling/zsnr set and the multiplier patched.
func TestApplyVpredWrapsFinalModel(t *testing.T) {
	base, err := BuildTxt2Img(loadTpl(t), spec(t), "ckpt")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ApplyVpred(base, loadFrag(t, FragVpred, VpredSHA256), 0.7)
	if err != nil {
		t.Fatal(err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if got := firstLink(t, g, Vpred.Sampling, "model"); got != Txt2Img.Checkpoint {
		t.Errorf("MSD model tap = %s, want checkpoint %s", got, Txt2Img.Checkpoint)
	}
	if g[Vpred.Sampling].Inputs["sampling"] != "v_prediction" || g[Vpred.Sampling].Inputs["zsnr"] != true {
		t.Errorf("MSD not configured for v-pred: %+v", g[Vpred.Sampling].Inputs)
	}
	if got := firstLink(t, g, Vpred.Rescale, "model"); got != Vpred.Sampling {
		t.Errorf("RescaleCFG must read MSD, got %s", got)
	}
	if g[Vpred.Rescale].Inputs["multiplier"] != 0.7 {
		t.Errorf("multiplier = %v, want 0.7", g[Vpred.Rescale].Inputs["multiplier"])
	}
	if got := firstLink(t, g, Txt2Img.Sampler, "model"); got != Vpred.Rescale {
		t.Errorf("KSampler model = %s, want RescaleCFG %s", got, Vpred.Rescale)
	}
}

// TestApplyVpredWrapsComposedModel: applied after a LoRA, the wrap must sit
// in front of the LoRA output (the composed model), not the raw checkpoint —
// otherwise the LoRA is bypassed.
func TestApplyVpredWrapsComposedModel(t *testing.T) {
	base, err := BuildTxt2Img(loadTpl(t), spec(t), "ckpt")
	if err != nil {
		t.Fatal(err)
	}
	withLora, err := ApplyLora(base, loadFrag(t, FragLora, LoraSHA256), "s.safetensors", 0.8)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ApplyVpred(withLora, loadFrag(t, FragVpred, VpredSHA256), 0.5)
	if err != nil {
		t.Fatal(err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if got := firstLink(t, g, Vpred.Sampling, "model"); got != Lora.Loader {
		t.Errorf("MSD must wrap the LoRA output %s, got %s", Lora.Loader, got)
	}
	if got := firstLink(t, g, Txt2Img.Sampler, "model"); got != Vpred.Rescale {
		t.Errorf("KSampler model = %s, want RescaleCFG", got)
	}
}

// TestApplyLora2Chains: the second LoRA reads the first's outputs, and the
// sampler + text encoders route through the end of the chain.
func TestApplyLora2Chains(t *testing.T) {
	base, err := BuildTxt2Img(loadTpl(t), spec(t), "ckpt")
	if err != nil {
		t.Fatal(err)
	}
	one, err := ApplyLora(base, loadFrag(t, FragLora, LoraSHA256), "identity.safetensors", 0.8)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ApplyLora2(one, loadFrag(t, FragLora2, Lora2SHA256), "style.safetensors", 0.65)
	if err != nil {
		t.Fatal(err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if got := firstLink(t, g, Lora2.Loader, "model"); got != Lora.Loader {
		t.Errorf("second loader model = %s, want chained off %s", got, Lora.Loader)
	}
	if got := g[Lora2.Loader].Inputs["lora_name"]; got != "style.safetensors" {
		t.Errorf("lora2 name = %v", got)
	}
	if got := g[Lora2.Loader].Inputs["strength_model"]; got != 0.65 {
		t.Errorf("lora2 strength = %v", got)
	}
	if got := firstLink(t, g, Txt2Img.Sampler, "model"); got != Lora2.Loader {
		t.Errorf("sampler model = %s, want end of chain %s", got, Lora2.Loader)
	}
	for _, enc := range []string{Txt2Img.Positive, Txt2Img.Negative} {
		if got := firstLink(t, g, enc, "clip"); got != Lora2.Loader {
			t.Errorf("node %s clip = %s, want %s", enc, got, Lora2.Loader)
		}
	}
	// and the first loader still carries its own file/strength
	if got := g[Lora.Loader].Inputs["lora_name"]; got != "identity.safetensors" {
		t.Errorf("lora1 name = %v", got)
	}
}

// TestApplyVpredCarriesIntoHires: hires runs after the wrap and mirrors the
// base sampler's model, so the refine pass samples the v-pred model too.
func TestApplyVpredCarriesIntoHires(t *testing.T) {
	base, err := BuildTxt2Img(loadTpl(t), spec(t), "ckpt")
	if err != nil {
		t.Fatal(err)
	}
	withV, err := ApplyVpred(base, loadFrag(t, FragVpred, VpredSHA256), 0.7)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ApplyHires(withV, loadFrag(t, FragHires, HiresSHA256), spec(t))
	if err != nil {
		t.Fatal(err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if got := firstLink(t, g, Hires.Sampler, "model"); got != Vpred.Rescale {
		t.Errorf("hires model = %s, want the v-pred wrap %s", got, Vpred.Rescale)
	}
}

// TestApplyHiresScale: the refine pass upscales by spec.HiresScale, with
// the 1.5 fragment default when older specs omit it.
func TestApplyHiresScale(t *testing.T) {
	base, err := BuildTxt2Img(loadTpl(t), spec(t), "ckpt")
	if err != nil {
		t.Fatal(err)
	}
	frag := loadFrag(t, FragHires, HiresSHA256)
	s := spec(t)
	s.HiresScale = 2.0
	raw, err := ApplyHires(base, frag, s)
	if err != nil {
		t.Fatal(err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if got := g[Hires.Upscale].Inputs["scale_by"]; got != 2.0 {
		t.Errorf("scale_by = %v, want 2.0", got)
	}
	raw, err = ApplyHires(base, frag, spec(t)) // zero-value HiresScale
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if got := g[Hires.Upscale].Inputs["scale_by"]; got != 1.5 {
		t.Errorf("default scale_by = %v, want 1.5", got)
	}
	if got := g[Hires.Sampler].Inputs["denoise"]; got != 0.4 {
		t.Errorf("default refine denoise = %v, want 0.4", got)
	}
	s.HiresDenoise = 0.5
	raw, err = ApplyHires(base, frag, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	if got := g[Hires.Sampler].Inputs["denoise"]; got != 0.5 {
		t.Errorf("refine denoise = %v, want 0.5", got)
	}
}

func TestOutputRoles(t *testing.T) {
	if roles, ok := OutputRoles("txt2img"); !ok || len(roles) != 1 || roles[""] != Txt2Img.Save {
		t.Errorf("txt2img roles = %v, %v", roles, ok)
	}
	roles, ok := OutputRoles("pose")
	if !ok || len(roles) != 3 || roles[""] != Pose.SaveSource ||
		roles["lineart"] != Pose.SaveLineart || roles["skeleton"] != Pose.SaveSkeleton {
		t.Errorf("pose roles = %v, %v", roles, ok)
	}
	if _, ok := OutputRoles("manga"); ok {
		t.Error("unknown kind must not resolve")
	}
}
