package assembly

import (
	"reflect"
	"strings"
	"testing"
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
	for wh := range buckets {
		if _, err := Build(Request{Prompt: "1girl", Width: wh[0], Height: wh[1]}, opts()); err != nil {
			t.Errorf("bucket %dx%d rejected: %v", wh[0], wh[1], err)
		}
	}
}
