package tags_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"pulsar/core/internal/pgtest"
	"pulsar/core/internal/tags"
)

// fakeDanbooru serves the two endpoints Live uses. Tags are keyed by name;
// aliases map antecedent → consequent.
type fakeDanbooru struct {
	tags     map[string]map[string]any
	aliases  map[string]string
	requests atomic.Int64
}

func (f *fakeDanbooru) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tags.json", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		out := []map[string]any{}
		if tag, ok := f.tags[r.URL.Query().Get("search[name]")]; ok {
			out = append(out, tag)
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("GET /tag_aliases.json", func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		out := []map[string]any{}
		if c, ok := f.aliases[r.URL.Query().Get("search[antecedent_name]")]; ok {
			out = append(out, map[string]any{"antecedent_name": r.URL.Query().Get("search[antecedent_name]"), "consequent_name": c, "status": "active"})
		}
		json.NewEncoder(w).Encode(out)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fakeTag(name string, count int, category int, deprecated bool, created time.Time) map[string]any {
	return map[string]any{
		"name": name, "post_count": count, "category": category,
		"is_deprecated": deprecated, "created_at": created.Format(time.RFC3339),
	}
}

func TestLiveValidateFindsAndWritesThrough(t *testing.T) {
	pool := pgtest.Pool(t, "live_hit")
	created := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	fake := &fakeDanbooru{tags: map[string]map[string]any{
		"yuming_(nte)": fakeTag("yuming_(nte)", 210, 4, false, created),
	}}
	live := tags.NewLive(pool, fake.server(t).URL, 50, "")

	res, err := live.Validate(context.Background(), "Yuming (NTE)")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Canonical != "yuming_(nte)" || res.Stage != "live" || res.Category != "character" {
		t.Fatalf("res = %+v", res)
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("created_at = %v", res.CreatedAt)
	}

	// write-through: now an exact DB hit, no second Danbooru call
	before := fake.requests.Load()
	res2, err := live.Validate(context.Background(), "yuming_(nte)")
	if err != nil {
		t.Fatal(err)
	}
	if res2 == nil || res2.Stage != "exact" || res2.CreatedAt == nil {
		t.Fatalf("after write-through: %+v", res2)
	}
	if fake.requests.Load() != before {
		t.Error("DB hit still queried Danbooru")
	}
}

func TestLiveBelowFloorReportedNotStored(t *testing.T) {
	pool := pgtest.Pool(t, "live_floor")
	fake := &fakeDanbooru{tags: map[string]map[string]any{
		"brand_new_tag": fakeTag("brand_new_tag", 12, 0, false, time.Now()),
	}}
	live := tags.NewLive(pool, fake.server(t).URL, 50, "")

	res, err := live.Validate(context.Background(), "brand_new_tag")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Count != 12 || res.Stage != "live" {
		t.Fatalf("res = %+v", res) // the user asked; honesty wins
	}
	// but grounding refuses it, and the DB never saw it
	if g, _ := live.Ground(context.Background(), "brand_new_tag"); g != nil {
		t.Error("below-floor tag must not ground")
	}
	if r, _ := tags.NewStore(pool).Resolve(context.Background(), "brand_new_tag", false); r != nil {
		t.Error("below-floor tag leaked into the DB")
	}
}

func TestLiveResolvesThroughAlias(t *testing.T) {
	pool := pgtest.Pool(t, "live_alias")
	fake := &fakeDanbooru{
		tags: map[string]map[string]any{
			"grey_hair": fakeTag("grey_hair", 900000, 0, false, time.Date(2013, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		aliases: map[string]string{"silver_hair": "grey_hair"},
	}
	live := tags.NewLive(pool, fake.server(t).URL, 50, "")

	res, err := live.Validate(context.Background(), "silver hair")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Canonical != "grey_hair" {
		t.Fatalf("res = %+v", res)
	}
	// the alias row came along: next time it's a DB alias hit
	r, err := tags.NewStore(pool).Resolve(context.Background(), "silver hair", false)
	if err != nil || r == nil || r.Stage != "alias" || r.Canonical != "grey_hair" {
		t.Fatalf("alias not written through: %+v, %v", r, err)
	}
}

func TestLiveMissesAreCachedAndDeprecatedRejected(t *testing.T) {
	pool := pgtest.Pool(t, "live_miss")
	fake := &fakeDanbooru{tags: map[string]map[string]any{
		"old_junk": fakeTag("old_junk", 5000, 0, true, time.Now()),
	}}
	live := tags.NewLive(pool, fake.server(t).URL, 50, "")

	if res, err := live.Validate(context.Background(), "old_junk"); err != nil || res != nil {
		t.Fatalf("deprecated tag must not validate: %+v, %v", res, err)
	}
	if res, err := live.Validate(context.Background(), "no_such_tag"); err != nil || res != nil {
		t.Fatalf("miss: %+v, %v", res, err)
	}
	before := fake.requests.Load()
	live.Validate(context.Background(), "no_such_tag")
	if fake.requests.Load() != before {
		t.Error("repeated miss was not served from the negative cache")
	}
}
