package api

import (
	"errors"
	"log"
	"net/http"

	"pulsar/core/internal/assembly"
	"pulsar/core/internal/prompt"
)

// promptErrorBody is the 400 for a prompt that failed to parse. `error`
// stays the human message so clients reading only it keep working; the
// rest locates the problem: field is "prompt", "negative" or
// "characters.N"; start/end are UTF-16 offsets into that field's text;
// tag is the comma/newline piece index (docs/frontend.md#prompt-validation).
type promptErrorBody struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Field   string `json:"field"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Tag     int    `json:"tag"`
}

func writePromptError(w http.ResponseWriter, field string, is prompt.Issue) {
	msg := is.Message
	if is.Hint != "" {
		msg += " — " + is.Hint
	}
	writeJSON(w, http.StatusBadRequest, promptErrorBody{
		Error: field + ": " + msg, Code: is.Code, Message: is.Message, Hint: is.Hint,
		Field: field, Start: is.Start, End: is.End, Tag: is.Tag,
	})
}

// writeAssemblyError maps assembly errors: a located parse error is a 400
// with its span; a deployment prompt (env, style row) that fails to parse
// is the operator's problem — 500, logged, never silently skipped.
func writeAssemblyError(w http.ResponseWriter, err error) {
	var fe *assembly.FieldError
	var ce *assembly.ConfigError
	switch {
	case errors.As(err, &fe):
		writePromptError(w, fe.Field, fe.Issue)
	case errors.As(err, &ce):
		log.Printf("api: prompt config: %v", err)
		httpError(w, http.StatusInternalServerError, "prompt configuration error ("+ce.Source+")")
	default:
		httpError(w, http.StatusBadRequest, err.Error())
	}
}

type runView struct {
	Text    string  `json:"text"`
	Weight  float64 `json:"w"`
	Emitted float64 `json:"emitted"`
}

type tagView2 struct {
	Text      string    `json:"text"`
	Name      string    `json:"name"`
	NAI       string    `json:"nai"`
	Start     int       `json:"start"`
	End       int       `json:"end"`
	Tag       int       `json:"tag"`
	Runs      []runView `json:"runs"`
	Relocated bool      `json:"relocated,omitempty"`
}

func viewTags(ts []prompt.Tag) []tagView2 {
	out := make([]tagView2, 0, len(ts))
	for _, t := range ts {
		v := tagView2{Text: t.Text, Name: t.Name, NAI: prompt.FormatTagNAI(t), Start: t.Span.Start, End: t.Span.End,
			Tag: t.Span.Index, Relocated: t.Relocated}
		for _, r := range t.Runs {
			v.Runs = append(v.Runs, runView{Text: r.Text, Weight: r.Weight, Emitted: r.Emitted})
		}
		out = append(out, v)
	}
	return out
}

// POST /prompt/parse — orbit's live validator: the same parser and lineage
// calibration /generate uses, so orbit needs no second grammar. It checks
// syntax only; the compliance policy (watch/complement lists) is never
// exposed here.
func (s *Server) handlePromptParse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text    string `json:"text"`
		Surface string `json:"surface"`
		Lineage string `json:"lineage"`
	}
	if !decodeJSON(w, r, 64<<10, &req) {
		return
	}
	var mode prompt.Mode
	switch req.Surface {
	case "", "prompt", "character":
		mode = prompt.Positive
	case "negative":
		mode = prompt.Negative
	default:
		httpError(w, http.StatusBadRequest, "surface must be prompt, negative or character")
		return
	}
	lineage := req.Lineage
	if lineage == "" {
		lineage = s.Cfg.Lineage
	}
	cal, ok := assembly.LineageCalibration(lineage)
	if !ok {
		httpError(w, http.StatusBadRequest, "unknown lineage "+lineage)
		return
	}
	surf, errs := prompt.Analyze(req.Text, mode)
	resp := struct {
		Tags      []tagView2         `json:"tags"`
		Relocated []tagView2         `json:"relocated"`
		Errors    []prompt.Issue     `json:"errors"`
		Notices   []prompt.Issue     `json:"notices"`
		Emitted   string             `json:"emitted"`
		Cal       prompt.Calibration `json:"calibration"`
	}{Tags: []tagView2{}, Relocated: []tagView2{}, Errors: []prompt.Issue{}, Notices: []prompt.Issue{}, Cal: cal}
	if len(errs) > 0 {
		resp.Errors = errs
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Notices = append(resp.Notices, surf.Notices...)
	resp.Notices = append(resp.Notices, cal.Apply(surf.Tags)...)
	resp.Notices = append(resp.Notices, cal.Apply(surf.Relocated)...)
	emitted, err := prompt.EmitComfy(surf.Tags)
	if err != nil {
		log.Printf("api: prompt/parse emit: %v", err)
		httpError(w, http.StatusInternalServerError, "emit failed")
		return
	}
	resp.Emitted = emitted
	resp.Tags = viewTags(surf.Tags)
	resp.Relocated = viewTags(surf.Relocated)
	writeJSON(w, http.StatusOK, resp)
}

// fieldErr wraps a prompt parse error as a located assembly.FieldError.
func fieldErr(field string, err error) error {
	var pe *prompt.Error
	if errors.As(err, &pe) {
		return &assembly.FieldError{Field: field, Issue: pe.Issue}
	}
	return err
}
