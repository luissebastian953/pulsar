package tags

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/pgtest"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Silver Hair ":  "silver_hair",
		"  on   back  ": "on_back",
		"1girl":         "1girl",
		"COWBOY shot":   "cowboy_shot",
		"":              "",
		"   ":           "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`a_b%c\`); got != `a\_b\%c\\` {
		t.Errorf("escapeLike = %q", got)
	}
}

func seed(t *testing.T, pool *pgxpool.Pool) *Store {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tags (name, category, post_count, pose_relevant) VALUES
		('1girl',        'general', 8000000, false),
		('long_hair',    'general', 6000000, false),
		('blonde_hair',  'general', 2500000, false),
		('silver_hair',  'general',  500000, false),
		('silver_dress', 'general',    3000, false),
		('sitting',      'general', 2000000, true),
		('standing',     'general', 1500000, true),
		('on_back',      'general',  400000, true),
		('onward',       'general',  100000, false),
		('hatsune_miku', 'character', 900000, false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tag_aliases (alias, canonical) VALUES
		('platinum_hair', 'silver_hair'),
		('miku',          'hatsune_miku')`); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool)
}

func names(s []Suggestion) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.Canonical
	}
	return out
}

func TestAutocomplete(t *testing.T) {
	s := seed(t, pgtest.Pool(t, "tags_ac"))
	ctx := context.Background()

	cases := []struct {
		name     string
		q        string
		poseOnly bool
		want     []string
	}{
		{"prefix rank by count", "silver", false, []string{"silver_hair", "silver_dress"}},
		{"contains rank by count", "hair", false, []string{"long_hair", "blonde_hair", "silver_hair"}},
		{"alias resolves to canonical", "platinum", false, []string{"silver_hair"}},
		{"alias and canonical dedupe", "miku", false, []string{"hatsune_miku"}},
		{"space input normalized", "on back", false, []string{"on_back"}},
		{"underscore is literal not wildcard", "on_", false, []string{"on_back"}},
		{"pose subset filters", "s", true, []string{"sitting", "standing"}},
		{"no match", "zzz", false, nil},
		{"empty query", "  ", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Autocomplete(ctx, tc.q, tc.poseOnly, 20)
			if err != nil {
				t.Fatal(err)
			}
			gotNames := names(got)
			if len(gotNames) != len(tc.want) {
				t.Fatalf("got %v, want %v", gotNames, tc.want)
			}
			for i := range tc.want {
				if gotNames[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", gotNames, tc.want)
				}
			}
		})
	}

	got, err := s.Autocomplete(ctx, "miku", false, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Category != "character" || got[0].Count != 900000 {
		t.Errorf("suggestion payload: %+v", got[0])
	}
}

func TestAutocompleteLimit(t *testing.T) {
	s := seed(t, pgtest.Pool(t, "tags_limit"))
	got, err := s.Autocomplete(context.Background(), "s", false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("limit not applied: %d results", len(got))
	}
}

func TestResolve(t *testing.T) {
	s := seed(t, pgtest.Pool(t, "tags_resolve"))
	ctx := context.Background()

	cases := map[string]struct{ canonical, stage string }{
		"silver_hair":   {"silver_hair", "exact"},
		"Silver Hair":   {"silver_hair", "exact"}, // normalized
		"platinum_hair": {"silver_hair", "alias"},
		"miku":          {"hatsune_miku", "alias"},
		"1 girl":        {"1girl", "exact"}, // squeezed-form fallback
	}
	for in, want := range cases {
		got, err := s.Resolve(ctx, in, false)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || got.Canonical != want.canonical || got.Stage != want.stage {
			t.Errorf("Resolve(%q) = %+v, want %s via %s", in, got, want.canonical, want.stage)
		}
	}

	got, err := s.Resolve(ctx, "miku", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Category != "character" || got.Count != 900000 {
		t.Errorf("resolution payload: %+v", got)
	}

	// the pose subset restricts the whole cascade
	if got, err := s.Resolve(ctx, "silver_hair", true); err != nil || got != nil {
		t.Errorf("poseOnly Resolve(silver_hair) = %+v (%v), want nil", got, err)
	}
	if got, err := s.Resolve(ctx, "sitting", true); err != nil || got == nil {
		t.Errorf("poseOnly Resolve(sitting) = %+v (%v), want hit", got, err)
	}

	// no tag for it → dropped, never force-matched
	if got, err := s.Resolve(ctx, "ethereal vibes", false); err != nil || got != nil {
		t.Errorf("Resolve miss = %+v (%v), want nil", got, err)
	}
	if got, err := s.Resolve(ctx, "   ", false); err != nil || got != nil {
		t.Errorf("Resolve empty = %+v (%v), want nil", got, err)
	}
}

func TestFuzzyCandidates(t *testing.T) {
	s := seed(t, pgtest.Pool(t, "tags_fuzzy"))
	ctx := context.Background()

	cands, err := s.FuzzyCandidates(ctx, "blode hair", false, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) == 0 || cands[0].Canonical != "blonde_hair" {
		t.Fatalf("candidates for 'blode hair' = %+v", cands)
	}
	if cands[0].Similarity <= 0 || cands[0].Count != 2500000 {
		t.Errorf("candidate payload: %+v", cands[0])
	}

	// a similar alias surfaces its canonical tag
	cands, err = s.FuzzyCandidates(ctx, "platnum hair", false, 5)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cands {
		if c.Canonical == "silver_hair" && c.Matched == "platinum_hair" {
			found = true
		}
	}
	if !found {
		t.Errorf("alias candidate missing: %+v", cands)
	}

	// pose subset filters candidates too
	cands, err = s.FuzzyCandidates(ctx, "blode hair", true, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if c.Canonical == "blonde_hair" {
			t.Errorf("poseOnly returned non-pose candidate: %+v", c)
		}
	}
}
