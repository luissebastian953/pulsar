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
	// Chunk: the tag came out of this chunk reference (start/end are then
	// the reference's)
	Chunk *prompt.ChunkRef `json:"chunk,omitempty"`
}

// chunkView is one resolved `!macro:Name!` in the parsed text: where the
// token sits and what it expands to, so orbit can tint it, show the
// expansion, and bake it into plain text without a grammar of its own.
type chunkView struct {
	Name  string `json:"name"`
	Hash  string `json:"hash"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Tag   int    `json:"tag"`
	Text  string `json:"text"`
}

func viewChunks(refs []prompt.ChunkSpan) []chunkView {
	out := make([]chunkView, 0, len(refs))
	for _, r := range refs {
		out = append(out, chunkView{Name: r.Name, Hash: r.Hash, Start: r.Start, End: r.End, Tag: r.Index, Text: r.Text})
	}
	return out
}

func viewTags(ts []prompt.Tag) []tagView2 {
	out := make([]tagView2, 0, len(ts))
	for _, t := range ts {
		v := tagView2{Text: t.Text, Name: t.Name, NAI: prompt.FormatTagNAI(t), Start: t.Span.Start, End: t.Span.End,
			Tag: t.Span.Index, Relocated: t.Relocated, Chunk: t.Chunk}
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
// exposed here. `chunks` in the request are the prompt chunks the text
// references; the response locates every resolved reference (`chunks`) and
// the typed weighting (`marks`, for the emphasis highlight).
//
// `"clean": true` is the import path (text written for another tool, read
// from an image's metadata): prompt.Clean first drops what pulsar's grammar
// has no place for — `<lora:…>`, `embedding:…`, `|`, `||…||`, control
// characters — each with a notice located in the ORIGINAL text, then the
// normal parser runs on the result. The response carries that result as
// `cleaned`; every other offset (tags, errors, chunks, marks) indexes it.
// Clean only removes: what is left is validated exactly as typed text is,
// and still passes CheckPrompt at /generate like any prompt.
func (s *Server) handlePromptParse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text    string         `json:"text"`
		Surface string         `json:"surface"`
		Lineage string         `json:"lineage"`
		Chunks  []prompt.Chunk `json:"chunks"`
		Clean   bool           `json:"clean"`
	}
	if !decodeJSON(w, r, 512<<10, &req) {
		return
	}
	if err := prompt.CheckChunks(req.Chunks); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
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
	text := req.Text
	var cleanNotes []prompt.Issue
	if req.Clean {
		text, cleanNotes = prompt.Clean(text)
	}
	surf, refs, errs := prompt.AnalyzeWith(text, mode, req.Chunks)
	resp := struct {
		Tags      []tagView2         `json:"tags"`
		Relocated []tagView2         `json:"relocated"`
		Errors    []prompt.Issue     `json:"errors"`
		Notices   []prompt.Issue     `json:"notices"`
		Chunks    []chunkView        `json:"chunks"`
		Marks     []prompt.Mark      `json:"marks"`
		Emitted   string             `json:"emitted"`
		Cal       prompt.Calibration `json:"calibration"`
		// Cleaned is the text that was parsed when the request asked for
		// clean; absent otherwise.
		Cleaned *string `json:"cleaned,omitempty"`
	}{Tags: []tagView2{}, Relocated: []tagView2{}, Errors: []prompt.Issue{}, Notices: []prompt.Issue{},
		Chunks: viewChunks(refs), Marks: []prompt.Mark{}, Cal: cal}
	if req.Clean {
		resp.Cleaned = &text
		resp.Notices = append(resp.Notices, cleanNotes...)
	}
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
	if surf.Marks != nil {
		resp.Marks = surf.Marks
	}
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
