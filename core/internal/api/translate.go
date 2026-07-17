package api

import (
	"encoding/json"
	"io"
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
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	var poseOnly bool
	switch req.Subset {
	case "", "all":
	case "pose":
		poseOnly = true
	default:
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
