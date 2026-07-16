package tags

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func testPool(t *testing.T, schema string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PULSAR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PULSAR_TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// public stays on the path so pg_trgm's operator classes resolve
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	files, err := filepath.Glob("../../../db/migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
	}
	sort.Strings(files)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// serialize schema setup across parallel test packages — CREATE
	// EXTENSION IF NOT EXISTS races with itself; the lock is session-scoped
	// so setup must run on this one connection
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(727272)"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "SELECT pg_advisory_unlock(727272)")

	stmts := []string{
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema),
		fmt.Sprintf("CREATE SCHEMA %s", schema),
	}
	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		stmts = append(stmts, string(sql))
	}
	for _, sql := range stmts {
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("test schema setup: %v", err)
		}
	}
	return pool
}

func seed(t *testing.T, pool *pgxpool.Pool) *Store {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tags (name, category, post_count, pose_relevant) VALUES
		('long_hair',    'general', 6000000, false),
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
	s := seed(t, testPool(t, "tags_ac"))
	ctx := context.Background()

	cases := []struct {
		name     string
		q        string
		poseOnly bool
		want     []string
	}{
		{"prefix rank by count", "silver", false, []string{"silver_hair", "silver_dress"}},
		{"contains rank by count", "hair", false, []string{"long_hair", "silver_hair"}},
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
	s := seed(t, testPool(t, "tags_limit"))
	got, err := s.Autocomplete(context.Background(), "s", false, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("limit not applied: %d results", len(got))
	}
}

func TestResolve(t *testing.T) {
	s := seed(t, testPool(t, "tags_resolve"))
	ctx := context.Background()

	for in, want := range map[string]string{
		"silver_hair":   "silver_hair",  // exact
		"Silver Hair":   "silver_hair",  // normalized
		"platinum_hair": "silver_hair",  // alias
		"miku":          "hatsune_miku", // alias
	} {
		got, ok, err := s.Resolve(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got != want {
			t.Errorf("Resolve(%q) = %q ok=%v, want %q", in, got, ok, want)
		}
	}

	// no tag for it → dropped, never force-matched
	if _, ok, err := s.Resolve(ctx, "ethereal vibes"); err != nil || ok {
		t.Errorf("Resolve miss: ok=%v err=%v, want ok=false", ok, err)
	}
	if _, ok, err := s.Resolve(ctx, "   "); err != nil || ok {
		t.Errorf("Resolve empty: ok=%v err=%v, want ok=false", ok, err)
	}
}
