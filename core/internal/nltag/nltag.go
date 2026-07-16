// Package nltag translates natural language into grounded Danbooru tags.
// Two stages, deliberately separate: the LLM DECOMPOSES a sentence into
// concepts — it never emits tags, because it will happily invent ones that
// don't exist — and the tag DB GROUNDS each concept via the cascade.
// Dropping an unmappable concept is correct behaviour, and drops are
// surfaced to the caller, never swallowed (.claude/skills/tag-grounding/).
package nltag

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Decomposer turns free text into short natural-language visual concepts.
type Decomposer interface {
	Decompose(ctx context.Context, text string) ([]string, error)
}

// GroundedTag is one concept snapped to the model's actual vocabulary,
// with provenance for UI transparency.
type GroundedTag struct {
	Canonical string `json:"canonical"`
	Category  string `json:"category"`
	Count     int    `json:"count"`
	Concept   string `json:"concept"`
	Stage     string `json:"stage"` // exact | alias | fuzzy
}

// Result is the translate() contract: (grounded, dropped). Dropped concepts
// are shown to the user — "couldn't map: ethereal vibes" — which is what
// keeps tag translation legible instead of a black box.
type Result struct {
	Tags    []GroundedTag `json:"tags"`
	Dropped []string      `json:"dropped"`
}

type Translator struct {
	Decomposer Decomposer
	Grounder   *Grounder
}

func (t *Translator) Translate(ctx context.Context, text string, poseOnly bool) (*Result, error) {
	res := &Result{Tags: []GroundedTag{}, Dropped: []string{}}
	text = strings.TrimSpace(text)
	if text == "" {
		return res, nil
	}

	concepts, err := t.Decomposer.Decompose(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("decompose: %w", err)
	}

	seen := map[string]bool{}
	for _, concept := range concepts {
		concept = strings.TrimSpace(concept)
		if concept == "" {
			continue
		}
		gt, err := t.Grounder.Ground(ctx, concept, poseOnly)
		if err != nil {
			return nil, fmt.Errorf("ground %q: %w", concept, err)
		}
		if gt == nil {
			res.Dropped = append(res.Dropped, concept)
			continue
		}
		if seen[gt.Canonical] {
			continue
		}
		seen[gt.Canonical] = true
		res.Tags = append(res.Tags, *gt)
	}

	// earlier tags weigh more: strong, well-known tags first
	// (docs/tagdb.md#tag-ordering)
	sort.SliceStable(res.Tags, func(i, j int) bool {
		return res.Tags[i].Count > res.Tags[j].Count
	})
	return res, nil
}
