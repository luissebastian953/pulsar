package api

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strings"

	"pulsar/core/internal/nltag"
	"pulsar/core/internal/prompt"
)

// weightedTag is a grounded tag plus the NovelAI-unit weight its source
// text carried (1 = unweighted; < 0 = relocated to UC by /generate).
type weightedTag struct {
	nltag.GroundedTag
	Weight float64 `json:"weight"`
	// Chunk names the prompt chunk the tag came out of; such a tag never
	// went through the LLM (stage "chunk").
	Chunk string `json:"chunk,omitempty"`
}

// chunkUse is one `!macro:Name!` in the text and the tags it produced.
type chunkUse struct {
	ref         prompt.ChunkRef
	start       int
	tags, reloc []prompt.Tag
}

// splitChunkTags separates the tags that came whole out of a chunk
// reference from the typed ones, grouping the former by reference.
func splitChunkTags(surf prompt.Surface) (typed, typedReloc []prompt.Tag, uses []*chunkUse) {
	at := map[int]*chunkUse{}
	use := func(t prompt.Tag) *chunkUse {
		u, ok := at[t.Span.Start]
		if !ok {
			u = &chunkUse{ref: *t.Chunk, start: t.Span.Start}
			at[t.Span.Start] = u
			uses = append(uses, u)
		}
		return u
	}
	for _, t := range surf.Tags {
		if t.Chunk == nil {
			typed = append(typed, t)
			continue
		}
		u := use(t)
		u.tags = append(u.tags, t)
	}
	for _, t := range surf.Relocated {
		if t.Chunk == nil {
			typedReloc = append(typedReloc, t)
			continue
		}
		u := use(t)
		u.reloc = append(u.reloc, t)
	}
	sort.SliceStable(uses, func(i, j int) bool { return uses[i].start < uses[j].start })
	return typed, typedReloc, uses
}

// render writes a chunk use back in NovelAI syntax. When the tags are
// exactly what the bare reference expands to (no outer weight, nothing
// glued on), that is the reference itself — /generate then expands it again
// and history keeps its provenance. Otherwise the expanded tags are written
// out, weights kept.
func (u *chunkUse) render(chunks []prompt.Chunk) []string {
	token := prompt.ChunkPrefix + u.ref.Name + "!"
	if alone, err := prompt.ParseWith(token, prompt.Positive, chunks); err == nil &&
		prompt.EquivalentNAI(alone.Tags, u.tags, prompt.NAITolerance) == nil &&
		prompt.EquivalentNAI(alone.Relocated, u.reloc, prompt.NAITolerance) == nil {
		return []string{token}
	}
	var parts []string
	for _, t := range u.tags {
		parts = append(parts, prompt.FormatTagNAI(t))
	}
	for _, t := range u.reloc {
		// relocated tags are held at |w|: write the sign back
		t.Runs = append([]prompt.Run(nil), t.Runs...)
		for k := range t.Runs {
			t.Runs[k].Weight = -t.Runs[k].Weight
		}
		parts = append(parts, prompt.FormatTagNAI(t))
	}
	return parts
}

// chunkTag describes a chunk-sourced tag for the resolved strip: looked up
// in the tag DB for its category and count, never rewritten and never
// dropped — chunk text is the user's own tags, as in tags mode.
func (s *Server) chunkTag(ctx context.Context, t prompt.Tag, name string, sign float64, poseOnly bool) weightedTag {
	wt := weightedTag{Weight: sign, Chunk: name,
		GroundedTag: nltag.GroundedTag{Canonical: t.Name, Category: "general", Concept: t.Text, Stage: "chunk"}}
	if len(t.Runs) == 1 {
		wt.Weight = sign * t.Runs[0].Weight
	}
	if s.Translator != nil && s.Translator.Grounder != nil && s.Translator.Grounder.Store != nil {
		if res, err := s.Translator.Grounder.Store.Resolve(ctx, t.Text, poseOnly); err == nil && res != nil && res.Canonical == t.Name {
			wt.Category, wt.Count = res.Category, res.Count
		}
	}
	return wt
}

// translateGroup is one stretch of the input at one weight.
type translateGroup struct {
	text   string
	weight float64
}

// weightGroups splits parsed tags into the weight-1 text and each maximal
// run of equal non-1 weight (consecutive across tag boundaries). Only run
// TEXT goes to the LLM — it never sees an operator, and never invents a
// weight (.claude/skills/tag-grounding: weights are the editor's).
func weightGroups(tags []prompt.Tag, sign float64) []translateGroup {
	var plain []string
	var groups []translateGroup
	var cur []string
	curW := 1.0
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, translateGroup{strings.Join(cur, ", "), curW * sign})
		}
		cur = nil
	}
	for _, t := range tags {
		for _, r := range t.Runs {
			text := strings.TrimSpace(r.Text)
			if text == "" {
				continue
			}
			if r.Weight == 1 && sign > 0 {
				flush()
				plain = append(plain, text)
				continue
			}
			if r.Weight != curW {
				flush()
				curW = r.Weight
			}
			cur = append(cur, text)
		}
	}
	flush()
	if len(plain) > 0 {
		groups = append([]translateGroup{{strings.Join(plain, ", "), 1}}, groups...)
	}
	return groups
}

// POST /translate — NL → grounded tags; drops are surfaced, not errors
// (.claude/skills/tag-grounding). The text is parsed with the prompt
// grammar FIRST: weight-1 text is translated as before, every weighted
// stretch is translated on its own and its grounded tags inherit the
// weight. The response carries the weights plus `prompt`, the tags in
// NovelAI syntax (prompt.FormatNAI), so orbit never builds syntax itself.
//
// Chunk references are expanded first. Text that came out of a chunk is
// tag text, not prose: it skips the LLM and is appended after the grounded
// tags — as the reference itself where that is exact, else expanded.
func (s *Server) handleTranslate(w http.ResponseWriter, r *http.Request) {
	if s.Translator == nil {
		httpError(w, http.StatusServiceUnavailable,
			"translation unavailable: configure TRANSLATE_PROVIDER and its API key to enable natural-language input")
		return
	}

	var req struct {
		Text   string         `json:"text"`
		Subset string         `json:"subset,omitempty"`
		Chunks []prompt.Chunk `json:"chunks,omitempty"`
	}
	if !decodeJSON(w, r, 512<<10, &req) {
		return
	}
	if err := prompt.CheckChunks(req.Chunks); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	poseOnly, ok := parseSubset(req.Subset)
	if !ok {
		httpError(w, http.StatusBadRequest, "subset must be all or pose")
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		httpError(w, http.StatusBadRequest, "text is required")
		return
	}
	surf, err := prompt.ParseWith(req.Text, prompt.Positive, req.Chunks)
	if err != nil {
		writeAssemblyError(w, fieldErr("prompt", err))
		return
	}

	typed, typedReloc, uses := splitChunkTags(surf)
	groups := append(weightGroups(typed, 1), weightGroups(typedReloc, -1)...)
	out := struct {
		Tags    []weightedTag  `json:"tags"`
		Dropped []string       `json:"dropped"`
		Prompt  string         `json:"prompt"`
		Notices []prompt.Issue `json:"notices"`
	}{Tags: []weightedTag{}, Dropped: []string{}, Notices: surf.Notices}
	if out.Notices == nil {
		out.Notices = []prompt.Issue{}
	}
	seen := map[string]int{} // canonical → index in out.Tags
	for _, g := range groups {
		res, err := s.translate(r.Context(), g.text, poseOnly)
		if err != nil {
			log.Printf("api: translate: %v", err)
			httpError(w, http.StatusBadGateway, "translation failed")
			return
		}
		out.Dropped = append(out.Dropped, res.Dropped...)
		for _, gt := range res.Tags {
			wt := weightedTag{GroundedTag: gt, Weight: g.weight}
			if i, dup := seen[gt.Canonical]; dup {
				if g.weight != 1 { // an explicit weight beats the plain mention
					out.Tags[i].Weight = g.weight
				}
				continue
			}
			// a vocabulary entry the NovelAI dialect can't spell (`|`, `::`,
			// braces) can't be sent: surface it as dropped, never mangle it
			if _, err := prompt.Parse(formatOne(wt), prompt.Positive); err != nil {
				out.Dropped = append(out.Dropped, gt.Canonical)
				continue
			}
			seen[gt.Canonical] = len(out.Tags)
			out.Tags = append(out.Tags, wt)
		}
	}
	// earlier tags weigh more: strong, well-known tags first
	sort.SliceStable(out.Tags, func(i, j int) bool { return out.Tags[i].Count > out.Tags[j].Count })
	parts := make([]string, len(out.Tags))
	for i, t := range out.Tags {
		parts[i] = formatOne(t)
	}
	for _, u := range uses {
		parts = append(parts, u.render(req.Chunks)...)
		for _, t := range u.tags {
			out.Tags = append(out.Tags, s.chunkTag(r.Context(), t, u.ref.Name, 1, poseOnly))
		}
		for _, t := range u.reloc {
			out.Tags = append(out.Tags, s.chunkTag(r.Context(), t, u.ref.Name, -1, poseOnly))
		}
	}
	out.Prompt = strings.Join(parts, ", ")
	writeJSON(w, http.StatusOK, out)
}

func formatOne(t weightedTag) string {
	return prompt.FormatTagNAI(prompt.Tag{Text: t.Canonical, Runs: []prompt.Run{{Text: t.Canonical, Weight: t.Weight}}})
}

func (s *Server) translate(ctx context.Context, text string, poseOnly bool) (*nltag.Result, error) {
	return s.Translator.Translate(ctx, text, poseOnly)
}
