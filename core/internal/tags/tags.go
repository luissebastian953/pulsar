// Package tags: the tag DB — autocomplete + canonical resolution
// (docs/tagdb.md, .claude/skills/tag-grounding).
package tags

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Suggestion is the UI contract (docs/tagdb.md#ui-contract); CreatedAt is
// the Danbooru creation date, nil = not yet tracked.
type Suggestion struct {
	Canonical string     `json:"canonical"`
	Category  string     `json:"category"`
	Count     int        `json:"count"`
	CreatedAt *time.Time `json:"-"`
}

// Normalize maps user input to tag-name form: lowercase, trimmed,
// whitespace runs collapsed to single underscores.
func Normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "_")
}

// escapeLike makes s literal inside a LIKE pattern — tag names are full of
// underscores, which LIKE would otherwise treat as wildcards.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Autocomplete: prefix matches first, then post_count; aliases resolve to
// canonical; poseOnly restricts to the pose vocabulary.
func (s *Store) Autocomplete(ctx context.Context, q string, poseOnly bool, limit int) ([]Suggestion, error) {
	q = Normalize(q)
	if q == "" {
		return nil, nil
	}
	contains := "%" + escapeLike(q) + "%"
	prefix := escapeLike(q) + "%"

	rows, err := s.pool.Query(ctx, `
		WITH hits AS (
			SELECT name, category, post_count, pose_relevant, danbooru_created_at,
			       name LIKE $2 ESCAPE '\' AS pref
			FROM tags
			WHERE name LIKE $1 ESCAPE '\'
			UNION ALL
			SELECT t.name, t.category, t.post_count, t.pose_relevant, t.danbooru_created_at,
			       a.alias LIKE $2 ESCAPE '\'
			FROM tag_aliases a
			JOIN tags t ON t.name = a.canonical
			WHERE a.alias LIKE $1 ESCAPE '\'
		)
		SELECT name, category, max(post_count) AS count, max(danbooru_created_at), bool_or(pref) AS pref
		FROM hits
		WHERE NOT $3 OR pose_relevant
		GROUP BY name, category
		ORDER BY pref DESC, count DESC, name
		LIMIT $4`,
		contains, prefix, poseOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Suggestion
	for rows.Next() {
		var sg Suggestion
		var pref bool
		if err := rows.Scan(&sg.Canonical, &sg.Category, &sg.Count, &sg.CreatedAt, &pref); err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

// Resolution is a grounded canonical tag plus the cascade stage that matched.
type Resolution struct {
	Canonical string
	Category  string
	Count     int
	Stage     string // "exact", "alias", or "live" (validated against Danbooru)
	CreatedAt *time.Time
}

// Resolve: exact + alias stages; nil = drop. Misses retry squeezed
// ("1_girl" → "1girl") — see .claude/skills/tag-grounding.
func (s *Store) Resolve(ctx context.Context, concept string, poseOnly bool) (*Resolution, error) {
	name := Normalize(concept)
	if name == "" {
		return nil, nil
	}
	r, err := s.resolveName(ctx, name, poseOnly)
	if err != nil || r != nil {
		return r, err
	}
	if squeezed := strings.ReplaceAll(name, "_", ""); squeezed != name {
		return s.resolveName(ctx, squeezed, poseOnly)
	}
	return nil, nil
}

func (s *Store) resolveName(ctx context.Context, name string, poseOnly bool) (*Resolution, error) {
	r := &Resolution{}
	err := s.pool.QueryRow(ctx, `
		SELECT name, category, post_count, danbooru_created_at, 'exact' AS stage FROM tags
		WHERE name = $1 AND (NOT $2 OR pose_relevant)
		UNION ALL
		SELECT t.name, t.category, t.post_count, t.danbooru_created_at, 'alias' FROM tag_aliases a
		JOIN tags t ON t.name = a.canonical
		WHERE a.alias = $1 AND (NOT $2 OR t.pose_relevant)
		ORDER BY stage DESC
		LIMIT 1`, name, poseOnly).Scan(&r.Canonical, &r.Category, &r.Count, &r.CreatedAt, &r.Stage)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// UpsertTag is the live-validation write-through path.
func (s *Store) UpsertTag(ctx context.Context, name, category string, count int, createdAt *time.Time, alias string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO tags (name, category, post_count, danbooru_created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE
		SET category = EXCLUDED.category, post_count = EXCLUDED.post_count,
		    danbooru_created_at = EXCLUDED.danbooru_created_at`,
		name, category, count, createdAt); err != nil {
		return err
	}
	if alias == "" || alias == name {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tag_aliases (alias, canonical) VALUES ($1, $2)
		ON CONFLICT (alias) DO UPDATE SET canonical = EXCLUDED.canonical`,
		alias, name)
	return err
}

// Candidate is a trigram-similar tag (or alias, resolved to its canonical)
// for the fuzzy stage of the grounding cascade.
type Candidate struct {
	Canonical  string
	Matched    string // the tag or alias name the similarity was computed on
	Category   string
	Count      int
	Similarity float64
}

// FuzzyCandidates prunes the search space; accept/drop is the caller's.
func (s *Store) FuzzyCandidates(ctx context.Context, concept string, poseOnly bool, limit int) ([]Candidate, error) {
	name := Normalize(concept)
	if name == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		WITH cands AS (
			SELECT name AS canonical, name AS matched, category, post_count,
			       similarity(name, $1) AS sim, pose_relevant
			FROM tags WHERE name % $1
			UNION ALL
			SELECT t.name, a.alias, t.category, t.post_count,
			       similarity(a.alias, $1), t.pose_relevant
			FROM tag_aliases a JOIN tags t ON t.name = a.canonical
			WHERE a.alias % $1
		)
		SELECT canonical, matched, category, post_count, sim
		FROM cands
		WHERE NOT $2 OR pose_relevant
		ORDER BY sim DESC, post_count DESC
		LIMIT $3`, name, poseOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.Canonical, &c.Matched, &c.Category, &c.Count, &c.Similarity); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
