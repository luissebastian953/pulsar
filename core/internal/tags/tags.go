// Package tags is the tag database layer: autocomplete and canonical
// resolution over the intersection of "exists on Danbooru" and "the model
// learned it" (docs/tagdb.md). nltag grounds against this DB, never against
// the LLM's imagination.
package tags

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Suggestion is the UI contract: {canonical, category, count}
// (docs/tagdb.md#ui-contract). Category drives pill colour in the editor.
type Suggestion struct {
	Canonical string `json:"canonical"`
	Category  string `json:"category"`
	Count     int    `json:"count"`
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

// Autocomplete returns up to limit suggestions for q, prefix matches first,
// then by post_count. Alias hits resolve to their canonical tag. poseOnly
// restricts to the pose vocabulary — grounding against ~200 candidates
// instead of ~50k is measurably more accurate (docs/tagdb.md#pose-subset).
func (s *Store) Autocomplete(ctx context.Context, q string, poseOnly bool, limit int) ([]Suggestion, error) {
	q = Normalize(q)
	if q == "" {
		return nil, nil
	}
	contains := "%" + escapeLike(q) + "%"
	prefix := escapeLike(q) + "%"

	rows, err := s.pool.Query(ctx, `
		WITH hits AS (
			SELECT name, category, post_count, pose_relevant,
			       name LIKE $2 ESCAPE '\' AS pref
			FROM tags
			WHERE name LIKE $1 ESCAPE '\'
			UNION ALL
			SELECT t.name, t.category, t.post_count, t.pose_relevant,
			       a.alias LIKE $2 ESCAPE '\'
			FROM tag_aliases a
			JOIN tags t ON t.name = a.canonical
			WHERE a.alias LIKE $1 ESCAPE '\'
		)
		SELECT name, category, max(post_count) AS count, bool_or(pref) AS pref
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
		if err := rows.Scan(&sg.Canonical, &sg.Category, &sg.Count, &pref); err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

// Resolve snaps a concept to a canonical tag via the exact and alias stages
// of the grounding cascade (fuzzy and embedding stages arrive with nltag).
// ok=false means the model has no tag for it: callers DROP the concept —
// that is correct behaviour, never force-match (docs/tagdb.md).
func (s *Store) Resolve(ctx context.Context, concept string) (canonical string, ok bool, err error) {
	name := Normalize(concept)
	if name == "" {
		return "", false, nil
	}
	err = s.pool.QueryRow(ctx, `
		SELECT name FROM tags WHERE name = $1
		UNION ALL
		SELECT canonical FROM tag_aliases WHERE alias = $1
		LIMIT 1`, name).Scan(&canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return canonical, true, nil
}
