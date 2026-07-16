package nltag

import (
	"context"

	"pulsar/core/internal/tags"
)

// Grounder runs the grounding cascade against the tag DB:
//
//	concept → normalise
//	        → exact / alias hit?     → canonical
//	        → fuzzy (ratio >= ~0.88) → canonical   (typos, minor wording)
//	        → nil                    → DROP
//
// The embedding-NN stage (paraphrases, e.g. "silver hair" → grey_hair)
// slots in after fuzzy when it lands. Returning nil is correct behaviour,
// not a failure — force-matching is how you get prompts that quietly
// produce garbage (docs/tagdb.md).
type Grounder struct {
	Store *tags.Store
	// FuzzyRatio is the accept threshold on normalized edit-distance
	// similarity; the Go stand-in for the docs' "WRatio >= ~88".
	FuzzyRatio float64
}

const (
	defaultFuzzyRatio = 0.88
	// Trigram similarity ranks differently than the ratio gate — the true
	// match can sit far down the candidate list behind superficially
	// similar tags — so fetch a wide pool and let the gate filter.
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
	if best == nil {
		return nil, nil // drop — never force-match
	}
	return &GroundedTag{
		Canonical: best.Canonical, Category: best.Category, Count: best.Count,
		Concept: concept, Stage: "fuzzy",
	}, nil
}
