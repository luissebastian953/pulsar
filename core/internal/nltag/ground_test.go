package nltag

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/pgtest"
	"pulsar/core/internal/tags"
)

func seedVocab(t *testing.T, pool *pgxpool.Pool) *tags.Store {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tags (name, category, post_count, pose_relevant) VALUES
		('1girl',        'general', 8000000, false),
		('long_hair',    'general', 6000000, false),
		('blonde_hair',  'general', 2500000, false),
		('silver_hair',  'general',  500000, false),
		('sitting',      'general', 2000000, true),
		('hatsune_miku', 'character', 900000, false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tag_aliases (alias, canonical) VALUES
		('miku', 'hatsune_miku')`); err != nil {
		t.Fatal(err)
	}
	return tags.NewStore(pool)
}

func TestGroundCascade(t *testing.T) {
	g := &Grounder{Store: seedVocab(t, pgtest.Pool(t, "nltag_ground"))}
	ctx := context.Background()

	cases := []struct {
		name     string
		concept  string
		poseOnly bool
		want     string // canonical; "" = drop
		stage    string
	}{
		{"exact", "blonde hair", false, "blonde_hair", "exact"},
		{"squeezed exact", "1 girl", false, "1girl", "exact"},
		{"alias", "miku", false, "hatsune_miku", "alias"},
		{"fuzzy typo", "blode hair", false, "blonde_hair", "fuzzy"},
		{"fuzzy short typo", "siting", false, "sitting", "fuzzy"},
		{"drop unmappable", "ethereal vibes", false, "", ""},
		// the real-world case that motivates the embedding stage: Danbooru
		// has no silver_hair→grey_hair alias, and 0.60 is far below the
		// fuzzy gate — paraphrases must never be force-matched by fuzzy
		{"drop paraphrase below threshold", "grey hair", false, "", ""},
		{"pose subset drops non-pose exact", "blonde hair", true, "", ""},
		{"pose subset keeps pose fuzzy", "siting", true, "sitting", "fuzzy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.Ground(ctx, tc.concept, tc.poseOnly)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if got != nil {
					t.Fatalf("Ground(%q) = %+v, want drop", tc.concept, got)
				}
				return
			}
			if got == nil || got.Canonical != tc.want || got.Stage != tc.stage {
				t.Fatalf("Ground(%q) = %+v, want %s via %s", tc.concept, got, tc.want, tc.stage)
			}
			if got.Concept != tc.concept {
				t.Errorf("provenance concept = %q, want %q", got.Concept, tc.concept)
			}
		})
	}
}

type stubDecomposer struct {
	concepts []string
	err      error
	called   bool
}

func (s *stubDecomposer) Decompose(context.Context, string) ([]string, error) {
	s.called = true
	return s.concepts, s.err
}

func TestTranslate(t *testing.T) {
	store := seedVocab(t, pgtest.Pool(t, "nltag_translate"))
	dec := &stubDecomposer{concepts: []string{
		"blonde hair", "long hair", "blode hair", "ethereal vibes", "miku", "  ",
	}}
	tr := &Translator{Decomposer: dec, Grounder: &Grounder{Store: store}}

	res, err := tr.Translate(context.Background(), "a blonde girl...", false)
	if err != nil {
		t.Fatal(err)
	}

	// dedupe ("blode hair" re-grounds to blonde_hair) + order by post_count
	want := []string{"long_hair", "blonde_hair", "hatsune_miku"}
	if len(res.Tags) != len(want) {
		t.Fatalf("tags = %+v, want %v", res.Tags, want)
	}
	for i, w := range want {
		if res.Tags[i].Canonical != w {
			t.Fatalf("tags[%d] = %+v, want %s (order by count)", i, res.Tags[i], w)
		}
	}
	if len(res.Dropped) != 1 || res.Dropped[0] != "ethereal vibes" {
		t.Errorf("dropped = %v — drops must be surfaced, never swallowed", res.Dropped)
	}
}

func TestTranslateEmptyText(t *testing.T) {
	dec := &stubDecomposer{err: context.Canceled}
	tr := &Translator{Decomposer: dec, Grounder: &Grounder{}}
	res, err := tr.Translate(context.Background(), "   ", false)
	if err != nil {
		t.Fatal(err)
	}
	if dec.called {
		t.Error("empty text must not spend an LLM call")
	}
	if len(res.Tags) != 0 || len(res.Dropped) != 0 {
		t.Errorf("res = %+v", res)
	}
}
