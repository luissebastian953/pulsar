package assembly

import (
	"reflect"
	"strings"
	"testing"

	"pulsar/core/internal/prompt"
)

// testUC / testForbidden stand in for the DEFAULT_UC / FORBIDDEN_TAG_UC env
// values — negative prompts are deployment config, not code, so tests
// supply their own.
const (
	testQuality   = "masterpiece, best quality"
	testUC        = "lowres, bad hands, watermark"
	testForbidden = "loli, shota, child"
)

func opts() Options {
	return Options{Lineage: "illustrious", Seed: 42, QualityTags: testQuality, DefaultUC: testUC, ForbiddenTagUC: testForbidden}
}

func boolPtr(b bool) *bool { return &b }

func fPtr(f float64) *float64 { return &f }

// TestLineageDefaults pins the non-transferable per-lineage generation
// settings (docs/tuning.md): noobai is v-pred with its own CFG/sampler/steps
// and a colour-burn rescale; illustrious keeps the historical flat defaults.
func TestLineageDefaults(t *testing.T) {
	o := opts()
	o.Lineage = "noobai"
	s, err := Build(Request{Prompt: "1girl"}, o)
	if err != nil {
		t.Fatal(err)
	}
	if s.CFG != 5.0 || s.Sampler != "euler" || s.Scheduler != "normal" || s.Steps != 28 || s.CFGRescale != 0.55 {
		t.Errorf("noobai defaults = cfg %v sampler %q sched %q steps %d rescale %v",
			s.CFG, s.Sampler, s.Scheduler, s.Steps, s.CFGRescale)
	}
	if !Vpred("noobai") {
		t.Error("noobai must report v-pred")
	}
	// illustrious: unchanged, no rescale, not v-pred
	s, err = Build(Request{Prompt: "1girl"}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if s.CFG != 6.0 || s.Sampler != "euler_ancestral" || s.Steps != 24 || s.CFGRescale != 0 {
		t.Errorf("illustrious defaults drifted: cfg %v sampler %q steps %d rescale %v",
			s.CFG, s.Sampler, s.Steps, s.CFGRescale)
	}
	if Vpred("illustrious") {
		t.Error("illustrious must not report v-pred")
	}
}

// TestHiresScale: the refine upscale factor defaults to 1.5 (large), caps
// at 2.0 (ultra), and is meaningless without the hires pass.
func TestHiresScale(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl", Hires: true}, opts())
	if err != nil || s.HiresScale != 1.5 {
		t.Errorf("hires default scale = %v (%v), want 1.5", s.HiresScale, err)
	}
	s, err = Build(Request{Prompt: "1girl", Hires: true, HiresScale: fPtr(2.0)}, opts())
	if err != nil || s.HiresScale != 2.0 {
		t.Errorf("ultra scale = %v (%v), want 2.0", s.HiresScale, err)
	}
	if s, err := Build(Request{Prompt: "1girl"}, opts()); err != nil || s.HiresScale != 0 {
		t.Errorf("no hires must mean no scale, got %v (%v)", s.HiresScale, err)
	}
	if _, err := Build(Request{Prompt: "1girl", HiresScale: fPtr(1.5)}, opts()); err == nil {
		t.Error("hires_scale without hires must be rejected")
	}
	for _, bad := range []float64{1.0, 2.1} {
		if _, err := Build(Request{Prompt: "1girl", Hires: true, HiresScale: fPtr(bad)}, opts()); err == nil {
			t.Errorf("hires_scale %v must be rejected", bad)
		}
	}
}

// TestHiresDenoise: refine strength defaults 0.4, bounded 0.2..0.7, and
// requires the hires pass.
func TestHiresDenoise(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl", Hires: true}, opts())
	if err != nil || s.HiresDenoise != 0.4 {
		t.Errorf("default = %v (%v), want 0.4", s.HiresDenoise, err)
	}
	s, err = Build(Request{Prompt: "1girl", Hires: true, HiresDenoise: fPtr(0.5)}, opts())
	if err != nil || s.HiresDenoise != 0.5 {
		t.Errorf("override = %v (%v), want 0.5", s.HiresDenoise, err)
	}
	if _, err := Build(Request{Prompt: "1girl", HiresDenoise: fPtr(0.4)}, opts()); err == nil {
		t.Error("hires_denoise without hires must be rejected")
	}
	for _, bad := range []float64{0.1, 0.8} {
		if _, err := Build(Request{Prompt: "1girl", Hires: true, HiresDenoise: fPtr(bad)}, opts()); err == nil {
			t.Errorf("hires_denoise %v must be rejected", bad)
		}
	}
}

// TestLoraLimits: two LoRAs compose (identity + style), three are refused,
// and each entry keeps its own strength (default 0.8).
func TestLoraLimits(t *testing.T) {
	two := []Lora{{Name: "a.safetensors", Strength: fPtr(0.9)}, {Name: "b.safetensors"}}
	s, err := Build(Request{Prompt: "1girl", Loras: two}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Loras) != 2 || *s.Loras[0].Strength != 0.9 || *s.Loras[1].Strength != 0.8 {
		t.Errorf("loras = %+v", s.Loras)
	}
	three := append(two, Lora{Name: "c.safetensors"})
	if _, err := Build(Request{Prompt: "1girl", Loras: three}, opts()); err == nil {
		t.Error("three LoRAs must be refused")
	}
}

// TestCFGRescaleOverride: a request may tune the rescale (including an
// explicit 0 to disable it on a v-pred lineage); out-of-range is refused.
func TestCFGRescaleOverride(t *testing.T) {
	o := opts()
	o.Lineage = "noobai"
	s, err := Build(Request{Prompt: "1girl", CFGRescale: fPtr(0.2)}, o)
	if err != nil {
		t.Fatal(err)
	}
	if s.CFGRescale != 0.2 {
		t.Errorf("override = %v, want 0.2", s.CFGRescale)
	}
	if s, err := Build(Request{Prompt: "1girl", CFGRescale: fPtr(0)}, o); err != nil || s.CFGRescale != 0 {
		t.Errorf("explicit 0 must disable rescale: %v %v", s.CFGRescale, err)
	}
	for _, bad := range []float64{-0.1, 1.1} {
		if _, err := Build(Request{Prompt: "1girl", CFGRescale: fPtr(bad)}, o); err == nil {
			t.Errorf("cfg_rescale %v must be rejected", bad)
		}
	}
}

func TestDefaults(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl, silver hair"}, opts())
	if err != nil {
		t.Fatal(err)
	}
	// the structured copy is asserted below; the scalar fields must match
	// the historical defaults exactly
	if len(s.PromptTags) != 4 || s.PromptTags[0].Source != prompt.Quality || s.PromptTags[2].Source != prompt.User ||
		len(s.NegativeTags) != 6 || s.NegativeTags[0].Source != prompt.Safety || s.NegativeTags[3].Source != prompt.DefaultUC ||
		s.Calibration == nil || *s.Calibration != lineageSpecs["illustrious"].calibration {
		t.Errorf("structured tags = %+v / %+v / %+v", s.PromptTags, s.NegativeTags, s.Calibration)
	}
	s.PromptTags, s.NegativeTags, s.Calibration = nil, nil, nil
	want := Spec{
		Prompt:   testQuality + ", 1girl, silver hair",
		Negative: testForbidden + ", " + testUC,
		Width:    832, Height: 1216,
		Steps: 24, CFG: 6.0,
		Sampler: "euler_ancestral", Scheduler: "normal",
		Seed: 42, BatchSize: 1, Lineage: "illustrious",
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("got %+v\nwant %+v", s, want)
	}
}

func TestDeterminism(t *testing.T) {
	req := Request{Prompt: "1girl", Width: 1024, Height: 1024, Steps: 30, CFG: 5.5}
	a, err1 := Build(req, opts())
	b, err2 := Build(req, opts())
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("assembly is not deterministic")
	}
}

func TestQualityTagsToggle(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl", QualityTags: boolPtr(false)}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Prompt != "1girl" {
		t.Errorf("quality tags leaked into prompt: %q", s.Prompt)
	}
}

func TestLineageValidation(t *testing.T) {
	// quality tags are env config now, but the lineage whitelist is not:
	// an unknown lineage is refused, never guessed
	if _, err := Build(Request{Prompt: "1girl"}, Options{Lineage: "noobai", Seed: 1, ForbiddenTagUC: testForbidden}); err != nil {
		t.Errorf("noobai must be a valid lineage: %v", err)
	}
	if _, err := Build(Request{Prompt: "1girl"}, Options{Lineage: "pony", Seed: 1, ForbiddenTagUC: testForbidden}); err == nil {
		t.Error("unknown lineage must be rejected, not guessed")
	}
}

func TestNegativeHandling(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl", Negative: "extra fingers"}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Negative != testForbidden+", "+testUC+", extra fingers" {
		t.Errorf("negative = %q", s.Negative)
	}

	s, err = Build(Request{Prompt: "1girl", Negative: "extra fingers", UCPreset: boolPtr(false)}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Negative != testForbidden+", extra fingers" {
		t.Errorf("uc_preset=false should drop the UC preset but never the safety terms, got %q", s.Negative)
	}
}

// TestForbiddenTagUCIsUnconditional pins the child-safety non-negotiable
// (CLAUDE.md): the forbidden-tag value lives in env, its PRESENCE is
// enforced here. Build must refuse an empty ForbiddenTagUC outright, and
// when set, no request shape may produce a negative without the terms in
// first position (never truncated out of the first CLIP chunk).
func TestForbiddenTagUCIsUnconditional(t *testing.T) {
	// fail closed: no safety terms, no Spec — under every request shape
	for _, o := range []Options{
		{Lineage: "illustrious", Seed: 1},
		{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: "   "},
		{Lineage: "illustrious", Seed: 1, DefaultUC: testUC},
	} {
		if _, err := Build(Request{Prompt: "1girl"}, o); err == nil {
			t.Errorf("opts %+v: empty ForbiddenTagUC must refuse to build", o)
		}
	}

	for _, o := range []Options{
		opts(),
		{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: testForbidden, DefaultUC: "blurry"},
		{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: testForbidden},
	} {
		for _, req := range []Request{
			{Prompt: "1girl"},
			{Prompt: "1girl", UCPreset: boolPtr(false)},
			{Prompt: "1girl", UCPreset: boolPtr(false), Negative: ""},
			{Prompt: "1girl", Negative: "blurry"},
		} {
			s, err := Build(req, o)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(s.Negative, testForbidden) {
				t.Errorf("request %+v opts %+v: negative %q lost the safety terms", req, o, s.Negative)
			}
		}
	}
}

func TestNegativeConfig(t *testing.T) {
	// no DEFAULT_UC configured → no preset, safety terms alone remain
	s, err := Build(Request{Prompt: "1girl"}, Options{Lineage: "illustrious", Seed: 1, ForbiddenTagUC: testForbidden})
	if err != nil {
		t.Fatal(err)
	}
	if s.Negative != testForbidden {
		t.Errorf("unset DefaultUC: %q", s.Negative)
	}
}

func TestSeedPassthrough(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl"}, Options{Lineage: "illustrious", Seed: 123456789, ForbiddenTagUC: testForbidden})
	if err != nil {
		t.Fatal(err)
	}
	if s.Seed != 123456789 {
		t.Errorf("seed must round-trip, got %d", s.Seed)
	}
}

func TestValidation(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		opts Options
	}{
		{"empty prompt", Request{}, opts()},
		{"whitespace prompt", Request{Prompt: "   "}, opts()},
		{"off-bucket", Request{Prompt: "1girl", Width: 800, Height: 600}, opts()},
		{"width without height", Request{Prompt: "1girl", Width: 1024}, opts()},
		{"steps over cap", Request{Prompt: "1girl", Steps: 51}, opts()},
		{"negative steps", Request{Prompt: "1girl", Steps: -1}, opts()},
		{"cfg over cap", Request{Prompt: "1girl", CFG: 12.5}, opts()},
		{"cfg under floor", Request{Prompt: "1girl", CFG: 0.5}, opts()},
		{"unknown sampler", Request{Prompt: "1girl", Sampler: "ddim"}, opts()},
		{"unknown scheduler", Request{Prompt: "1girl", Scheduler: "exponential"}, opts()},
		{"negative seed", Request{Prompt: "1girl"}, Options{Lineage: "illustrious", Seed: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Build(tc.req, tc.opts); err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestAllBucketsAccepted(t *testing.T) {
	for _, wh := range Buckets {
		if _, err := Build(Request{Prompt: "1girl", Width: wh[0], Height: wh[1]}, opts()); err != nil {
			t.Errorf("bucket %dx%d rejected: %v", wh[0], wh[1], err)
		}
	}
}

// TestCheckpointIsVpred pins the prediction-type inference used to refuse a
// lineage/checkpoint mismatch at request time (the noobai-lineage +
// illustrious-eps-checkpoint pairing that renders a flat grey field and then
// trips the output classifier as a false compliance block).
func TestCheckpointIsVpred(t *testing.T) {
	cases := []struct {
		name      string
		wantVpred bool
		wantKnown bool
	}{
		{"noobai-xl-vpred.safetensors", true, true},
		{"someModel-v-pred.safetensors", true, true},
		{"illustrious-xl.safetensors", false, true}, // the case that broke
		{"noobai-xl-eps.safetensors", false, true},
		{"waiNSFWIllustrious_v110.safetensors", false, true},
		{"some-random-anime-merge.safetensors", false, false}, // unclassifiable → never rejected
	}
	for _, c := range cases {
		gotVpred, gotKnown := CheckpointIsVpred(c.name)
		if gotVpred != c.wantVpred || gotKnown != c.wantKnown {
			t.Errorf("CheckpointIsVpred(%q) = (%v, %v), want (%v, %v)",
				c.name, gotVpred, gotKnown, c.wantVpred, c.wantKnown)
		}
	}
	// the load-bearing property: a vpred marker must win over an eps/family marker
	if v, known := CheckpointIsVpred("illustrious-vpred-finetune.safetensors"); !v || !known {
		t.Errorf("vpred marker must win over family marker; got (%v, %v)", v, known)
	}
}

func styleOpts(sty *Style) Options {
	o := opts()
	o.Lineage = "noobai"
	o.Style = sty
	return o
}

// TestStyleMerge pins the preset merge (docs/tuning.md#style-presets):
// quality → carriers → user prompt → finish; safety → UC preset → user
// negative → style negative; the style's LoRA attaches; settings fill only
// what the request left unset, and hires stays the request's call.
func TestStyleMerge(t *testing.T) {
	rescale := 0.45
	sty := &Style{
		Slug: "hsr", PromptPrefix: "official art, game cg", PromptSuffix: "painterly, glint",
		Negative: "(logo:1.3)", Lora: "hsr.safetensors", LoraStrength: 0.8,
		Sampler: "euler", Scheduler: "normal", CFG: 5, Steps: 30, CFGRescale: &rescale,
		HiresScale: 1.5, HiresDenoise: 0.55,
	}
	s, err := Build(Request{Prompt: "acheron, 1girl", Negative: "blurry", Hires: true}, styleOpts(sty))
	if err != nil {
		t.Fatal(err)
	}
	if want := testQuality + ", official art, game cg, acheron, 1girl, painterly, glint"; s.Prompt != want {
		t.Errorf("prompt = %q, want %q", s.Prompt, want)
	}
	if want := testForbidden + ", " + testUC + ", blurry, (logo:1.3)"; s.Negative != want {
		t.Errorf("negative = %q, want %q", s.Negative, want)
	}
	if !strings.HasPrefix(s.Negative, testForbidden) {
		t.Error("forbidden-tag negative must stay first with a style negative")
	}
	if s.Style != "hsr" {
		t.Errorf("style = %q", s.Style)
	}
	if len(s.Loras) != 1 || s.Loras[0].Name != "hsr.safetensors" || *s.Loras[0].Strength != 0.8 {
		t.Errorf("loras = %+v", s.Loras)
	}
	if s.Steps != 30 || s.CFG != 5 || s.CFGRescale != 0.45 || s.HiresScale != 1.5 || s.HiresDenoise != 0.55 {
		t.Errorf("settings: steps %d cfg %v rescale %v scale %v denoise %v", s.Steps, s.CFG, s.CFGRescale, s.HiresScale, s.HiresDenoise)
	}

	// request > style > lineage default
	s, err = Build(Request{Prompt: "1girl", Steps: 20, CFGRescale: fPtr(0.7)}, styleOpts(sty))
	if err != nil {
		t.Fatal(err)
	}
	if s.Steps != 20 || s.CFGRescale != 0.7 || s.Sampler != "euler" {
		t.Errorf("request must win: steps %d rescale %v sampler %q", s.Steps, s.CFGRescale, s.Sampler)
	}
	// a style never switches hires on, so its hires fill-ins stay inert
	if s.Hires || s.HiresScale != 0 || s.HiresDenoise != 0 {
		t.Errorf("hires leaked from style: %v %v %v", s.Hires, s.HiresScale, s.HiresDenoise)
	}

	// tag-only style: settings come from the lineage, no LoRA, no suffix
	s, err = Build(Request{Prompt: "1girl"}, styleOpts(&Style{Slug: "chibi", PromptPrefix: "chibi"}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Prompt != testQuality+", chibi, 1girl" || s.Steps != 28 || s.CFGRescale != 0.55 || len(s.Loras) != 0 {
		t.Errorf("tag-only style: prompt %q steps %d rescale %v loras %d", s.Prompt, s.Steps, s.CFGRescale, len(s.Loras))
	}
	if s.Negative != testForbidden+", "+testUC {
		t.Errorf("empty style negative must add nothing: %q", s.Negative)
	}

	// no style: Spec.Style stays empty and nothing else moves
	s, err = Build(Request{Prompt: "1girl"}, styleOpts(nil))
	if err != nil || s.Style != "" || s.Prompt != testQuality+", 1girl" {
		t.Errorf("nil style: %v %+v", err, s)
	}
}

// TestStyleLora: the style's LoRA dedupes against the request's (the
// request's strength wins), defaults to 0.8, and respects the two-LoRA cap.
func TestStyleLora(t *testing.T) {
	sty := &Style{Slug: "hsr", Lora: "hsr.safetensors", LoraStrength: 0.8}
	s, err := Build(Request{Prompt: "1girl", Loras: []Lora{{Name: "hsr.safetensors", Strength: fPtr(0.6)}}}, styleOpts(sty))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Loras) != 1 || *s.Loras[0].Strength != 0.6 {
		t.Errorf("dedupe: %+v", s.Loras)
	}
	s, err = Build(Request{Prompt: "1girl", Loras: []Lora{{Name: "char.safetensors"}}}, styleOpts(sty))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Loras) != 2 || s.Loras[0].Name != "char.safetensors" || s.Loras[1].Name != "hsr.safetensors" {
		t.Errorf("style LoRA must chain behind the request's: %+v", s.Loras)
	}
	if _, err = Build(Request{Prompt: "1girl", Loras: []Lora{{Name: "a"}, {Name: "b"}}}, styleOpts(sty)); err == nil || !strings.Contains(err.Error(), `style "hsr"`) {
		t.Errorf("third LoRA via style must be refused naming the style, got %v", err)
	}
	s, err = Build(Request{Prompt: "1girl"}, styleOpts(&Style{Slug: "x", Lora: "x.safetensors"}))
	if err != nil || *s.Loras[0].Strength != 0.8 {
		t.Errorf("style LoRA default strength: %v %+v", err, s.Loras)
	}
}

// TestStyleValidation: a preset's settings pass the same gates as a
// request's — a bad row is a 400 at request time, never garbage on the GPU.
func TestStyleValidation(t *testing.T) {
	bad := []Style{
		{Slug: "s", Sampler: "ddim"},
		{Slug: "s", Scheduler: "exponential"},
		{Slug: "s", CFG: 40},
		{Slug: "s", Steps: 99},
		{Slug: "s", CFGRescale: fPtr(1.5)},
		{Slug: "s", Lora: "x", LoraStrength: 3},
		{Slug: "s", HiresScale: 3},
		{Slug: "s", HiresDenoise: 0.9},
	}
	for i := range bad {
		if _, err := Build(Request{Prompt: "1girl", Hires: true}, styleOpts(&bad[i])); err == nil {
			t.Errorf("style %+v accepted", bad[i])
		}
	}
}
