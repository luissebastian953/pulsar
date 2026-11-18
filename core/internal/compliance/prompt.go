package compliance

import (
	"context"
	"fmt"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/prompt"
	"pulsar/core/internal/tags"
)

// Prompts is the pre-enqueue prompt policy (reports/NovelAI prompt
// weighting on SDXL.md S7). It reads the EXACT strings patched into the
// CLIPTextEncode nodes with ComfyUI's own grammar (prompt.ComfyParse), so
// no wrapper the encoder strips can launder a tag past it, and it never
// trusts Spec's structured fields alone: they must agree with that parse.
//
// A pass means "no known pre-filter hole", never "safe". The output
// classifier (CheckOutput) remains the gate. No rule here is
// env-toggleable off; config can only ADD watch or complement tags.
type Prompts struct {
	// watch: tags.WatchSet — hard-coded floor ∪ FORBIDDEN_TAG_UC, the same
	// set the reference-fetch gate and Live.Ground use.
	watch map[string]bool
	// safety: FORBIDDEN_TAG_UC in order — the mandatory head of every negative.
	safety []string
	// complement: age-complement (adult-signal) tags, AGE_COMPLEMENT_TAGS.
	// Env only; empty disables the complement rules (decision 5).
	complement map[string]bool
}

// NewPrompts builds the policy from FORBIDDEN_TAG_UC and the optional
// AGE_COMPLEMENT_TAGS value.
func NewPrompts(forbiddenUC, complementTags string) *Prompts {
	comp := map[string]bool{}
	for _, t := range tags.ParseForbidden(complementTags) {
		comp[t] = true
	}
	return &Prompts{
		watch:      tags.WatchSet(forbiddenUC),
		safety:     tags.ParseForbidden(forbiddenUC),
		complement: comp,
	}
}

// structural tolerance for emitted weights: the emitter writes 4 dp and
// stores the rounded value, so agreement is exact up to float noise
const diffTolerance = 5e-5

func reject(format string, args ...any) (Decision, error) {
	return Decision{Allowed: false, Reason: fmt.Sprintf(format, args...)}, nil
}

type surface struct {
	name     string
	emitted  string
	tags     []prompt.Tag
	negative bool
}

// Check applies S7 to every string ComfyUI receives: spec.Prompt (node 2),
// spec.Negative (node 3), each spec.Characters[i].Prompt (nodes 32/36/42);
// hires reuses 2 and 3. Errors fail closed (the API answers 503).
func (p *Prompts) Check(spec assembly.Spec) (Decision, error) {
	if len(p.safety) == 0 {
		return Decision{}, fmt.Errorf("compliance: FORBIDDEN_TAG_UC is empty")
	}
	surfaces := []surface{
		{"prompt", spec.Prompt, spec.PromptTags, false},
		{"negative", spec.Negative, spec.NegativeTags, true},
	}
	for i, c := range spec.Characters {
		surfaces = append(surfaces, surface{assembly.CharacterField(i), c.Prompt, c.Tags, false})
	}
	for _, sf := range surfaces {
		if sf.emitted != "" && sf.tags == nil {
			return Decision{}, fmt.Errorf("compliance: %s has no structured tags", sf.name)
		}
		if prompt.HasEmbedding(sf.emitted) {
			return reject("embedding reference in %s", sf.name)
		}
		segs, err := prompt.ComfyParse(sf.emitted)
		if err != nil {
			return Decision{}, fmt.Errorf("compliance: parse %s: %w", sf.name, err)
		}
		parsed := prompt.ComfyTags(segs)
		// differential: what ComfyUI reads must be exactly the structure
		// assembly emitted — a mismatch is a bug or a bypass, both rejected
		if err := prompt.Equivalent(sf.tags, parsed, diffTolerance); err != nil {
			return reject("%s does not match its structured tags: %v", sf.name, err)
		}
		start := 0
		if sf.negative {
			if len(parsed) < len(p.safety) {
				return reject("negative is missing the safety block")
			}
			for i, want := range p.safety {
				got := parsed[i]
				if got.Name != want {
					return reject("negative does not start with the safety block (position %d)", i)
				}
				for _, r := range got.Runs {
					if r.Emitted != 1 {
						return reject("safety tag %q is not at weight 1", want)
					}
				}
			}
			start = len(p.safety)
		}
		for _, t := range parsed[start:] {
			if tags.Watched(p.watch, t.Name) {
				if sf.negative {
					// dedupe removed every legitimate copy; one left is a bypass or a bug
					return reject("forbidden tag outside the safety block: %s", t.Name)
				}
				// weight never launders a watched tag, in any direction
				return reject("forbidden tag in %s: %s", sf.name, t.Name)
			}
			if len(p.complement) == 0 || !tags.Watched(p.complement, t.Name) {
				continue
			}
			if sf.negative {
				return reject("age-complement tag in the negative: %s", t.Name)
			}
			for _, r := range t.Runs {
				if r.Emitted < 1 {
					return reject("age-complement tag down-weighted in %s: %s", sf.name, t.Name)
				}
			}
		}
	}
	return Decision{Allowed: true}, nil
}

// PromptGuard runs the prompt policy without an output classifier — the
// dev configuration (SENTINEL_URL unset). Outputs and uploads are allowed
// unclassified exactly like Stub, with the same loud boot warning; the
// prompt policy needs no sidecar, so it runs regardless.
type PromptGuard struct {
	Stub
	Prompts *Prompts
}

func (g PromptGuard) CheckPrompt(_ context.Context, spec assembly.Spec) (Decision, error) {
	if g.Prompts == nil {
		return Decision{}, fmt.Errorf("compliance: no prompt policy")
	}
	return g.Prompts.Check(spec)
}
