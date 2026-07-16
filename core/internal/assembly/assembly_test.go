package assembly

import (
	"reflect"
	"strings"
	"testing"
)

func opts() Options { return Options{Lineage: "illustrious", Seed: 42} }

func boolPtr(b bool) *bool { return &b }

func TestDefaults(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl, silver hair"}, opts())
	if err != nil {
		t.Fatal(err)
	}
	want := Spec{
		Prompt:   "masterpiece, best quality, very aesthetic, absurdres, 1girl, silver hair",
		Negative: defaultUC,
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

func TestQualityTagsAreLineageSpecific(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl"}, Options{Lineage: "noobai", Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Prompt, "very awa") {
		t.Errorf("noobai lineage missing 'very awa': %q", s.Prompt)
	}
	if _, err := Build(Request{Prompt: "1girl"}, Options{Lineage: "pony", Seed: 1}); err == nil {
		t.Error("unknown lineage must be rejected, not guessed")
	}
}

func TestNegativeHandling(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl", Negative: "extra fingers"}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Negative != defaultUC+", extra fingers" {
		t.Errorf("negative = %q", s.Negative)
	}

	s, err = Build(Request{Prompt: "1girl", Negative: "extra fingers", UCPreset: boolPtr(false)}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if s.Negative != "extra fingers" {
		t.Errorf("uc_preset=false should pass negative through, got %q", s.Negative)
	}
}

func TestSeedPassthrough(t *testing.T) {
	s, err := Build(Request{Prompt: "1girl"}, Options{Lineage: "illustrious", Seed: 123456789})
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
