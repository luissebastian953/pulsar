package tags

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
)

// newFakeDanbooru serves the two endpoints Ingest uses. Page 1 has data,
// page 2+ is empty.
func newFakeDanbooru(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var tagCalls, aliasCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tags.json", func(w http.ResponseWriter, r *http.Request) {
		tagCalls.Add(1)
		if r.URL.Query().Get("page") != "1" {
			w.Write([]byte("[]"))
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"name": "1girl", "post_count": 8000000, "category": 0},
			{"name": "highres", "post_count": 7800000, "category": 5},
			{"name": "old_junk", "post_count": 1000000, "category": 0, "is_deprecated": true},
			{"name": "weird_cat", "post_count": 900000, "category": 2},
			{"name": "sitting", "post_count": 2000000, "category": 0},
			{"name": "hatsune_miku", "post_count": 900000, "category": 4},
			{"name": "squatting", "post_count": 150000, "category": 0},
			{"name": "hand_on_own_hip", "post_count": 300000, "category": 0},
			// below the floor — and last, which stops pagination
			{"name": "one_off_tag", "post_count": 3, "category": 0},
		})
	})
	mux.HandleFunc("GET /tag_aliases.json", func(w http.ResponseWriter, r *http.Request) {
		aliasCalls.Add(1)
		if r.URL.Query().Get("page") != "1" {
			w.Write([]byte("[]"))
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"antecedent_name": "1girls", "consequent_name": "1girl", "status": "active"},
			{"antecedent_name": "miku", "consequent_name": "hatsune_miku", "status": "active"},
			{"antecedent_name": "dangling", "consequent_name": "never_ingested", "status": "active"},
			{"antecedent_name": "dead", "consequent_name": "1girl", "status": "deleted"},
			// renamed pose seeds: resolve through aliases to the canonical
			{"antecedent_name": "crouching", "consequent_name": "squatting", "status": "active"},
			{"antecedent_name": "hand_on_hip", "consequent_name": "hand_on_own_hip", "status": "active"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &tagCalls, &aliasCalls
}

func TestIngest(t *testing.T) {
	pool := testPool(t, "tags_ingest")
	srv, tagCalls, aliasCalls := newFakeDanbooru(t)
	ctx := context.Background()

	opts := IngestOptions{
		BaseURL:      srv.URL,
		MinPostCount: 50,
		Throttle:     1,
		Log:          func(string, ...any) {},
	}
	stats, err := Ingest(ctx, pool, opts)
	if err != nil {
		t.Fatal(err)
	}

	// kept: 1girl, highres, sitting, hatsune_miku, squatting, hand_on_own_hip
	// skipped: deprecated, unknown category; dropped: below floor
	if stats.Tags != 6 {
		t.Errorf("stats.Tags = %d, want 6", stats.Tags)
	}
	if stats.Skipped != 2 {
		t.Errorf("stats.Skipped = %d, want 2", stats.Skipped)
	}
	if stats.Aliases != 4 { // dangling filtered by EXISTS, deleted by status
		t.Errorf("stats.Aliases = %d, want 4", stats.Aliases)
	}
	if stats.PoseMarked != 3 { // sitting, squatting, hand_on_own_hip
		t.Errorf("stats.PoseMarked = %d, want 3", stats.PoseMarked)
	}
	if !slices.Contains(stats.PoseMissing, "standing") {
		t.Errorf("PoseMissing should report unmatched seeds, got %v", stats.PoseMissing)
	}
	for _, resolved := range []string{"crouching", "hand_on_hip"} {
		if slices.Contains(stats.PoseMissing, resolved) {
			t.Errorf("%s resolves through an alias and must not be missing (got %v)",
				resolved, stats.PoseMissing)
		}
	}
	// the floor stop condition means page 2 is never requested for tags
	if tagCalls.Load() != 1 {
		t.Errorf("tags.json fetched %d times, want 1 (stop at floor)", tagCalls.Load())
	}
	if aliasCalls.Load() != 2 { // page 1 + empty page 2
		t.Errorf("tag_aliases.json fetched %d times, want 2", aliasCalls.Load())
	}

	s := NewStore(pool)
	var category string
	if err := pool.QueryRow(ctx, `SELECT category FROM tags WHERE name = 'highres'`).Scan(&category); err != nil || category != "meta" {
		t.Errorf("highres category = %q (%v), want meta", category, err)
	}
	if got, ok, _ := s.Resolve(ctx, "miku"); !ok || got != "hatsune_miku" {
		t.Errorf("alias not resolvable after ingest: %q ok=%v", got, ok)
	}
	if _, ok, _ := s.Resolve(ctx, "dangling"); ok {
		t.Error("alias with missing canonical must not be ingested")
	}
	var pose bool
	if err := pool.QueryRow(ctx, `SELECT pose_relevant FROM tags WHERE name = 'sitting'`).Scan(&pose); err != nil || !pose {
		t.Errorf("sitting pose_relevant = %v (%v), want true", pose, err)
	}
	// the alias-resolved canonical carries the flag, not the stale seed name
	if err := pool.QueryRow(ctx, `SELECT pose_relevant FROM tags WHERE name = 'hand_on_own_hip'`).Scan(&pose); err != nil || !pose {
		t.Errorf("hand_on_own_hip pose_relevant = %v (%v), want true", pose, err)
	}

	// idempotent: re-run refreshes without error and preserves pose flags
	if _, err := Ingest(ctx, pool, opts); err != nil {
		t.Fatalf("re-ingest: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT pose_relevant FROM tags WHERE name = 'sitting'`).Scan(&pose); err != nil || !pose {
		t.Error("re-ingest cleared pose_relevant")
	}
}
