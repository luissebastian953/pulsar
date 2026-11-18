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
func (s *Server) handleTranslate(w http.ResponseWriter, r *http.Request) {
	if s.Translator == nil {
		httpError(w, http.StatusServiceUnavailable,
			"translation unavailable: configure TRANSLATE_PROVIDER and its API key to enable natural-language input")
		return
	}

	var req struct {
		Text   string `json:"text"`
		Subset string `json:"subset,omitempty"`
	}
	if !decodeJSON(w, r, 64<<10, &req) {
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
	surf, err := prompt.Parse(req.Text, prompt.Positive)
	if err != nil {
		writeAssemblyError(w, fieldErr("prompt", err))
		return
	}

	groups := append(weightGroups(surf.Tags, 1), weightGroups(surf.Relocated, -1)...)
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
	out.Prompt = strings.Join(parts, ", ")
	writeJSON(w, http.StatusOK, out)
}

func formatOne(t weightedTag) string {
	return prompt.FormatTagNAI(prompt.Tag{Text: t.Canonical, Runs: []prompt.Run{{Text: t.Canonical, Weight: t.Weight}}})
}

func (s *Server) translate(ctx context.Context, text string, poseOnly bool) (*nltag.Result, error) {
	return s.Translator.Translate(ctx, text, poseOnly)
}
