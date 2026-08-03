package api

import (
	"log"
	"net/http"
	"strings"
)

// POST /translate — NL → grounded tags; drops are surfaced, not errors
// (.claude/skills/tag-grounding).
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

	result, err := s.Translator.Translate(r.Context(), req.Text, poseOnly)
	if err != nil {
		log.Printf("api: translate: %v", err)
		httpError(w, http.StatusBadGateway, "translation failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
