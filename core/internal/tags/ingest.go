package tags

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Ingest pulls the Danbooru vocabulary into Postgres. Idempotent;
// pose_relevant is set, never cleared (docs/tagdb.md#ingest).
type IngestOptions struct {
	BaseURL string
	// MinPostCount is the ingest floor (docs/tagdb.md#min_post_count).
	MinPostCount int
	PageSize     int
	Throttle     time.Duration // pause between requests; be polite
	UserAgent    string
	Log          func(format string, args ...any)
}

type IngestStats struct {
	Tags        int
	Aliases     int
	Pages       int
	Skipped     int // deprecated or unknown-category
	PoseMarked  int
	PoseMissing []string // seed entries with no tag row — review, don't trust
}

// Danbooru numeric categories; 2 is unused.
var danbooruCategories = map[int]string{
	0: "general",
	1: "artist",
	3: "copyright",
	4: "character",
	5: "meta",
}

type danbooruTag struct {
	Name         string     `json:"name"`
	PostCount    int        `json:"post_count"`
	Category     int        `json:"category"`
	IsDeprecated bool       `json:"is_deprecated"`
	CreatedAt    *time.Time `json:"created_at"`
}

type danbooruAlias struct {
	Antecedent string `json:"antecedent_name"`
	Consequent string `json:"consequent_name"`
	Status     string `json:"status"`
}

func (o *IngestOptions) defaults() {
	if o.BaseURL == "" {
		o.BaseURL = "https://danbooru.donmai.us"
	}
	if o.MinPostCount == 0 {
		o.MinPostCount = 50
	}
	if o.PageSize == 0 {
		o.PageSize = 1000 // Danbooru's max
	}
	if o.Throttle == 0 {
		o.Throttle = 300 * time.Millisecond
	}
	if o.UserAgent == "" {
		o.UserAgent = "pulsar-seed-tags/0.1"
	}
	if o.Log == nil {
		o.Log = log.Printf
	}
}

func Ingest(ctx context.Context, pool *pgxpool.Pool, opts IngestOptions) (IngestStats, error) {
	opts.defaults()
	in := ingester{pool: pool, opts: opts, http: &http.Client{Timeout: 30 * time.Second}}

	if err := in.tags(ctx); err != nil {
		return in.stats, fmt.Errorf("ingest tags: %w", err)
	}
	if err := in.aliases(ctx); err != nil {
		return in.stats, fmt.Errorf("ingest aliases: %w", err)
	}
	if err := in.pose(ctx); err != nil {
		return in.stats, fmt.Errorf("classify pose: %w", err)
	}
	return in.stats, nil
}

// MarkPose re-runs only the pose_relevant classification against the existing
// DB — no Danbooru traffic. Use after extending poseSeed so an already-seeded
// vocabulary picks up the new subset without a full re-ingest.
func MarkPose(ctx context.Context, pool *pgxpool.Pool) (IngestStats, error) {
	in := ingester{pool: pool}
	if err := in.pose(ctx); err != nil {
		return in.stats, fmt.Errorf("classify pose: %w", err)
	}
	return in.stats, nil
}

type ingester struct {
	pool  *pgxpool.Pool
	opts  IngestOptions
	http  *http.Client
	stats IngestStats
}

// tags pages through /tags.json ordered by post count descending and stops
// at the floor — everything below it is vocabulary the model never learned.
func (in *ingester) tags(ctx context.Context) error {
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("limit", fmt.Sprint(in.opts.PageSize))
		q.Set("page", fmt.Sprint(page))
		q.Set("search[order]", "count")

		var batch []danbooruTag
		if err := in.getJSON(ctx, "/tags.json?"+q.Encode(), &batch); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		in.stats.Pages++

		names, cats, counts := make([]string, 0, len(batch)), make([]string, 0, len(batch)), make([]int, 0, len(batch))
		created := make([]*time.Time, 0, len(batch))
		for _, t := range batch {
			cat, known := danbooruCategories[t.Category]
			if t.IsDeprecated || !known || t.Name == "" {
				in.stats.Skipped++
				continue
			}
			if t.PostCount < in.opts.MinPostCount {
				continue
			}
			names = append(names, t.Name)
			cats = append(cats, cat)
			counts = append(counts, t.PostCount)
			created = append(created, t.CreatedAt)
		}
		if len(names) > 0 {
			_, err := in.pool.Exec(ctx, `
				INSERT INTO tags (name, category, post_count, danbooru_created_at)
				SELECT * FROM unnest($1::text[], $2::text[], $3::int[], $4::timestamptz[])
				ON CONFLICT (name) DO UPDATE
				SET category = EXCLUDED.category, post_count = EXCLUDED.post_count,
				    danbooru_created_at = EXCLUDED.danbooru_created_at`,
				names, cats, counts, created)
			if err != nil {
				return err
			}
			in.stats.Tags += len(names)
		}
		in.opts.Log("seed-tags: page %d — %d tags kept (total %d, floor %d)",
			page, len(names), in.stats.Tags, in.opts.MinPostCount)

		// ordered by count desc: once a page ends below the floor, done
		if batch[len(batch)-1].PostCount < in.opts.MinPostCount {
			return nil
		}
		if err := in.pause(ctx); err != nil {
			return err
		}
	}
}

// aliases upserts active aliases whose canonical tag made the cut; the rest
// would dangle and are dropped.
func (in *ingester) aliases(ctx context.Context) error {
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("limit", fmt.Sprint(in.opts.PageSize))
		q.Set("page", fmt.Sprint(page))
		q.Set("search[status]", "active")

		var batch []danbooruAlias
		if err := in.getJSON(ctx, "/tag_aliases.json?"+q.Encode(), &batch); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		in.stats.Pages++

		aliases, canonicals := make([]string, 0, len(batch)), make([]string, 0, len(batch))
		for _, a := range batch {
			if a.Status != "active" || a.Antecedent == "" || a.Consequent == "" {
				continue
			}
			aliases = append(aliases, a.Antecedent)
			canonicals = append(canonicals, a.Consequent)
		}
		if len(aliases) > 0 {
			tag, err := in.pool.Exec(ctx, `
				INSERT INTO tag_aliases (alias, canonical)
				SELECT a, c FROM unnest($1::text[], $2::text[]) AS u(a, c)
				WHERE EXISTS (SELECT 1 FROM tags WHERE name = c)
				ON CONFLICT (alias) DO UPDATE SET canonical = EXCLUDED.canonical`,
				aliases, canonicals)
			if err != nil {
				return err
			}
			in.stats.Aliases += int(tag.RowsAffected())
		}
		in.opts.Log("seed-tags: alias page %d — %d kept (total %d)",
			page, len(aliases), in.stats.Aliases)

		if err := in.pause(ctx); err != nil {
			return err
		}
	}
}

func (in *ingester) pose(ctx context.Context) error {
	// resolve seeds through aliases — Danbooru renames tags
	rows, err := in.pool.Query(ctx, `
		SELECT name, name FROM tags WHERE name = ANY($1)
		UNION
		SELECT a.alias, a.canonical FROM tag_aliases a WHERE a.alias = ANY($1)`,
		poseSeed)
	if err != nil {
		return err
	}
	covered := map[string]bool{}   // seed entries that landed somewhere
	canonical := map[string]bool{} // tag rows to mark
	for rows.Next() {
		var seed, canon string
		if err := rows.Scan(&seed, &canon); err != nil {
			rows.Close()
			return err
		}
		covered[seed] = true
		canonical[canon] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	mark := make([]string, 0, len(canonical))
	for name := range canonical {
		mark = append(mark, name)
	}
	if _, err := in.pool.Exec(ctx,
		`UPDATE tags SET pose_relevant = TRUE WHERE name = ANY($1)`, mark); err != nil {
		return err
	}
	in.stats.PoseMarked = len(mark)
	for _, s := range poseSeed {
		if !covered[s] {
			in.stats.PoseMissing = append(in.stats.PoseMissing, s)
		}
	}
	return nil
}

func (in *ingester) getJSON(ctx context.Context, path string, v any) error {
	var lastErr error
	for attempt := 1; attempt <= 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.opts.BaseURL+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", in.opts.UserAgent)
		resp, err := in.http.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
			resp.Body.Close()
			switch {
			case rerr != nil:
				lastErr = rerr
			case resp.StatusCode == http.StatusOK:
				return json.Unmarshal(body, v)
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				lastErr = fmt.Errorf("GET %s: %s", path, resp.Status)
			default:
				return fmt.Errorf("GET %s: %s: %.200s", path, resp.Status, body)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
	return lastErr
}

func (in *ingester) pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(in.opts.Throttle):
		return nil
	}
}
