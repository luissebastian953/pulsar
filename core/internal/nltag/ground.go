package nltag

import (
	"context"
	"log"

	"pulsar/core/internal/tags"
)

// Grounder runs the cascade: exact/alias → fuzzy → live → drop
// (.claude/skills/tag-grounding).
type Grounder struct {
	Store *tags.Store
	// FuzzyRatio is the accept threshold on normalized edit-distance
	// similarity; the Go stand-in for the docs' "WRatio >= ~88".
	FuzzyRatio float64
	// Live validates DB misses against Danbooru (nil = stage off).
	Live *tags.Live
}

const (
	defaultFuzzyRatio = 0.88
	// wide pool: trigram ranking buries true matches (.claude/skills/tag-grounding)
	fuzzyCandidateLimit = 25
)

func (g *Grounder) Ground(ctx context.Context, concept string, poseOnly bool) (*GroundedTag, error) {
	if res, err := g.Store.Resolve(ctx, concept, poseOnly); err != nil {
		return nil, err
	} else if res != nil {
		return &GroundedTag{
			Canonical: res.Canonical, Category: res.Category, Count: res.Count,
			Concept: concept, Stage: res.Stage,
		}, nil
	}

	cands, err := g.Store.FuzzyCandidates(ctx, concept, poseOnly, fuzzyCandidateLimit)
	if err != nil {
		return nil, err
	}
	threshold := g.FuzzyRatio
	if threshold == 0 {
		threshold = defaultFuzzyRatio
	}
	name := tags.Normalize(concept)
	var best *tags.Candidate
	var bestRatio float64
	for i := range cands {
		r := ratio(name, cands[i].Matched)
		if r < threshold {
			continue
		}
		if best == nil || r > bestRatio || (r == bestRatio && cands[i].Count > best.Count) {
			best, bestRatio = &cands[i], r
		}
	}
	if best != nil {
		return &GroundedTag{
			Canonical: best.Canonical, Category: best.Category, Count: best.Count,
			Concept: concept, Stage: "fuzzy",
		}, nil
	}

	if g.Live != nil && !poseOnly {
		res, err := g.Live.Ground(ctx, concept)
		if err != nil {
			// Danbooru down degrades to a drop, never a failed translation
			log.Printf("nltag: live stage for %q: %v", concept, err)
			return nil, nil
		}
		if res != nil {
			return &GroundedTag{
				Canonical: res.Canonical, Category: res.Category, Count: res.Count,
				Concept: concept, Stage: "live",
			}, nil
		}
	}

	return nil, nil // drop — never force-match
}
