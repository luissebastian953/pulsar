// Live validation against Danbooru: on a DB miss, ask Danbooru and write
// hits through (.claude/skills/tag-grounding).
package tags

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Live struct {
	store *Store
	base  string
	http  *http.Client
	// floor gates write-through and grounding (docs/tagdb.md)
	floor int

	// serializes lookups: politeness + de facto single-flight
	mu   sync.Mutex
	miss map[string]time.Time // negative cache: name → expiry
}

const missTTL = 10 * time.Minute

func NewLive(pool *pgxpool.Pool, baseURL string, minPostCount int) *Live {
	return &Live{
		store: NewStore(pool),
		base:  baseURL,
		http:  &http.Client{Timeout: 10 * time.Second},
		floor: minPostCount,
		miss:  make(map[string]time.Time),
	}
}

// Validate: DB first, Danbooru on miss; hits ≥ floor write through,
// below-floor hits are returned but never stored. nil = does not exist.
func (l *Live) Validate(ctx context.Context, name string) (*Resolution, error) {
	name = Normalize(name)
	if name == "" {
		return nil, nil
	}
	if r, err := l.store.Resolve(ctx, name, false); err != nil || r != nil {
		return r, err
	}
	return l.lookup(ctx, name)
}

// Ground is the cascade's live stage: Validate, floor-gated.
func (l *Live) Ground(ctx context.Context, name string) (*Resolution, error) {
	r, err := l.Validate(ctx, name)
	if err != nil || r == nil {
		return nil, err
	}
	if r.Count < l.floor {
		return nil, nil // drop — below the accuracy floor
	}
	return r, nil
}

func (l *Live) lookup(ctx context.Context, name string) (*Resolution, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if exp, ok := l.miss[name]; ok && time.Now().Before(exp) {
		return nil, nil
	}

	tag, alias, err := l.fetch(ctx, name)
	if err != nil {
		return nil, err
	}
	if tag == nil {
		if len(l.miss) > 4096 { // bound the negative cache
			l.miss = make(map[string]time.Time)
		}
		l.miss[name] = time.Now().Add(missTTL)
		return nil, nil
	}

	if tag.PostCount >= l.floor {
		cat := danbooruCategories[tag.Category]
		if err := l.store.UpsertTag(ctx, tag.Name, cat, tag.PostCount, tag.CreatedAt, alias); err != nil {
			return nil, err
		}
	}
	return &Resolution{
		Canonical: tag.Name,
		Category:  danbooruCategories[tag.Category],
		Count:     tag.PostCount,
		Stage:     "live",
		CreatedAt: tag.CreatedAt,
	}, nil
}

// fetch: exact tag first, then through an active alias; returns the tag
// and the antecedent name when an alias was followed.
func (l *Live) fetch(ctx context.Context, name string) (*danbooruTag, string, error) {
	tag, err := l.fetchTag(ctx, name)
	if err != nil {
		return nil, "", err
	}
	if tag != nil {
		return tag, "", nil
	}

	var aliases []danbooruAlias
	q := url.Values{}
	q.Set("search[antecedent_name]", name)
	q.Set("search[status]", "active")
	q.Set("limit", "1")
	if err := l.getJSON(ctx, "/tag_aliases.json?"+q.Encode(), &aliases); err != nil {
		return nil, "", err
	}
	if len(aliases) == 0 || aliases[0].Consequent == "" {
		return nil, "", nil
	}
	tag, err = l.fetchTag(ctx, aliases[0].Consequent)
	if err != nil || tag == nil {
		return nil, "", err
	}
	return tag, name, nil
}

func (l *Live) fetchTag(ctx context.Context, name string) (*danbooruTag, error) {
	var tags []danbooruTag
	q := url.Values{}
	q.Set("search[name]", name)
	q.Set("limit", "1")
	if err := l.getJSON(ctx, "/tags.json?"+q.Encode(), &tags); err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, nil
	}
	t := tags[0]
	_, known := danbooruCategories[t.Category]
	if t.IsDeprecated || !known || t.PostCount <= 0 {
		return nil, nil
	}
	return &t, nil
}

func (l *Live) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "pulsar/1.0 (tag validation)")
	resp, err := l.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("danbooru %s: %s", path, resp.Status)
	}
	return json.Unmarshal(body, v)
}
