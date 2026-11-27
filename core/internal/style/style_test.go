package style_test

import (
	"context"
	"errors"
	"testing"

	"pulsar/core/internal/pgtest"
	"pulsar/core/internal/style"
)

// TestSeededPresets reads the migration's seed rows back through the store:
// the measured hsr recipe is noobai-only, tag-only looks exist per lineage,
// and nullable settings project onto assembly's data shape.
func TestSeededPresets(t *testing.T) {
	pool := pgtest.Pool(t, "style_seed")
	st := style.NewStore(pool)
	ctx := context.Background()

	all, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("no seeded presets")
	}
	if all[0].Slug != "hsr" {
		t.Errorf("picker order: first = %s, want hsr (sort_order 10)", all[0].Slug)
	}

	hsr, err := st.Get(ctx, "hsr", "noobai")
	if err != nil {
		t.Fatal(err)
	}
	if hsr.Lora != "honkai star rail_v3.safetensors" || hsr.LoraStrength != 0.8 || hsr.PromptPrefix != "official art, game cg" {
		t.Errorf("hsr = %+v", hsr)
	}
	if hsr.CFGRescale == nil || *hsr.CFGRescale != 0.45 || hsr.Hires == nil || !*hsr.Hires || hsr.HiresScale != 1.5 || hsr.HiresDenoise != 0.55 || hsr.Steps != 28 {
		t.Errorf("hsr settings = %+v", hsr)
	}
	if _, err := st.Get(ctx, "hsr", "illustrious"); !errors.Is(err, style.ErrNotFound) {
		t.Errorf("hsr on illustrious = %v, want ErrNotFound", err)
	}
	if _, err := st.Get(ctx, "nope", "noobai"); !errors.Is(err, style.ErrNotFound) {
		t.Errorf("unknown slug = %v, want ErrNotFound", err)
	}

	chibi, err := st.Get(ctx, "chibi", "illustrious")
	if err != nil {
		t.Fatal(err)
	}
	if chibi.Lora != "" || chibi.CFGRescale != nil || chibi.Hires != nil || chibi.Steps != 0 || chibi.Sampler != "" {
		t.Errorf("tag-only preset must leave settings unset: %+v", chibi)
	}

	a := hsr.Assembly()
	if a.Slug != "hsr" || a.Lora != hsr.Lora || a.CFGRescale == nil || *a.CFGRescale != 0.45 || a.HiresScale != 1.5 {
		t.Errorf("Assembly() = %+v", a)
	}
}
