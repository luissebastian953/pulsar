// Package prompt is pulsar's one prompt grammar: the NovelAI weighting
// dialect users type (plus SD/ComfyUI `(text:w)` auto-converted), the
// per-lineage calibration, the flat escaped ComfyUI emitter, and a port of
// ComfyUI's own parser so compliance reads the bytes the encoder will read
// (reports/NovelAI prompt weighting on SDXL.md, S1–S6).
//
// Pure and stdlib-only: no I/O, no DB, never imports tags (pgx). assembly,
// compliance and api import it; assembly stays pure.
package prompt

import (
	"fmt"
	"strings"
)

// Source says which segment of an assembled surface a tag came from.
// Serialized as a string so stored Specs and orbit read it directly.
type Source uint8

const (
	User Source = iota
	Safety
	DefaultUC
	Quality
	StylePrefix
	StyleSuffix
	StyleNegative
	Anchor
)

var sourceNames = [...]string{"user", "safety", "default_uc", "quality", "style_prefix", "style_suffix", "style_negative", "anchor"}

func (s Source) String() string {
	if int(s) < len(sourceNames) {
		return sourceNames[s]
	}
	return fmt.Sprintf("source(%d)", s)
}

func (s Source) MarshalText() ([]byte, error) {
	if int(s) >= len(sourceNames) {
		return nil, fmt.Errorf("prompt: unknown source %d", s)
	}
	return []byte(sourceNames[s]), nil
}

func (s *Source) UnmarshalText(b []byte) error {
	for i, n := range sourceNames {
		if n == string(b) {
			*s = Source(i)
			return nil
		}
	}
	return fmt.Errorf("prompt: unknown source %q", b)
}

// Run is a maximal stretch of one tag at one weight.
type Run struct {
	Text    string  `json:"text"`    // unescaped literal text
	Weight  float64 `json:"w"`       // NovelAI units as parsed (|w| for relocated tags)
	Emitted float64 `json:"emitted"` // after calibration — the weight ComfyUI receives
	// Span locates the run in the surface it was parsed from; not stored.
	Span Span `json:"-"`
}

// Tag is one comma/newline-separated piece of a surface. Weight can change
// mid-tag (`blue {hair}`), which is why it holds Runs.
type Tag struct {
	Runs   []Run  `json:"runs"`
	Text   string `json:"text"` // runs concatenated, whitespace-collapsed
	Name   string `json:"name"` // Normalize(Text)
	Source Source `json:"source"`
	// NAI is the tag re-written in the NovelAI dialect (FormatNAI) — what
	// orbit restores into an editor box, so it never builds syntax itself.
	NAI string `json:"nai,omitempty"`
	// Relocated marks a negative-weight tag moved from a positive surface
	// into the user segment of the negative (decision 1).
	Relocated bool `json:"relocated,omitempty"`
	// Chunk is set when every rune of the tag came out of one chunk
	// reference: the chunk's name and the hash of what it expanded to. The
	// tag itself stays the authoritative, compliance-checked record;
	// Equivalent ignores this.
	Chunk *ChunkRef `json:"chunk,omitempty"`
	// Span locates the tag in the surface it was parsed from; not stored.
	Span Span `json:"-"`
}

// Span is a location in a surface string. Start/End are UTF-16 code-unit
// offsets (orbit indexes JS strings); Index is the comma/newline piece
// index, counting empty pieces, so a chip UI joined with ", " maps 1:1.
type Span struct {
	Start, End, Index int
}

// Issue is a parse error or notice, located in the surface.
type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Start   int    `json:"start"` // UTF-16 offset
	End     int    `json:"end"`   // UTF-16 offset, exclusive
	Tag     int    `json:"tag"`   // piece index
}

// Error is a rejected surface; it carries the first Issue.
type Error struct{ Issue }

func (e *Error) Error() string {
	if e.Hint != "" {
		return e.Message + " — " + e.Hint
	}
	return e.Message
}

// Error codes. Unbalanced brackets are errors in pulsar (decision 4), not
// NovelAI-style notices.
const (
	CodeWeightTooHigh     = "weight_too_high"    // P002
	CodeGluedNumber       = "glued_number"       // P003
	CodeTripleColon       = "triple_colon"       // P006
	CodeEmbedding         = "embedding"          // P007
	CodeSDWeightSyntax    = "sd_weight_syntax"   // P008: nested/ambiguous SD groups only
	CodePipe              = "pipe"               // P009
	CodeExtraNetwork      = "extra_network"      // P010
	CodeControlChar       = "control_char"       // P011
	CodeTooLong           = "too_long"           // P012
	CodeUnclosedBracket   = "unclosed_bracket"   // opener never closed
	CodeStrayCloser       = "stray_closer"       // closer without opener
	CodeMismatchedBracket = "mismatched_bracket" // `{]`
	CodeNegativeInUC      = "negative_weight_uc" // w < 0 in a negative surface
	CodeMixedSign         = "mixed_sign"         // one tag partly negative/zero
	CodeWeightNonPositive = "weight_nonpositive" // w ≤ 0 in a config surface
	CodeSafetyOperator    = "safety_operator"    // FORBIDDEN_TAG_UC carries syntax
	CodeEmpty             = "empty"              // a required surface parsed to nothing
)

// Notice codes — the surface is accepted, the user is told.
const (
	NoticeRelocated     = "relocated_to_uc" // decision 1
	NoticeZeroDropped   = "zero_weight_dropped"
	NoticeConvertedSD   = "converted_from_sd" // decision 2
	NoticeLiteralParens = "literal_parens"
	NoticeCalibrated    = "calibrated"    // N5
	NoticeOpenSection   = "open_section"  // `w::` never closed: runs to the end
	NoticeSpacedNumber  = "spaced_number" // N3
	NoticeNumberAfter   = "number_after"  // N4
	NoticeNumberInTag   = "number_in_tag" // `year 5::` reads 5 as a weight
	NoticeDuplicateUC   = "duplicate_dropped"
)

// Normalize maps text to tag-name form: lowercase, trimmed, whitespace
// runs collapsed to single underscores. tags.Normalize delegates here so
// exactly one normalisation exists.
func Normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "_")
}

// matchStrip are the wrapper characters removed for the second match form
// (S7 step 6).
var matchStrip = strings.NewReplacer("(", "", ")", "", "{", "", "}", "", "[", "", "]", "", `\`, "", `"`, "", "'", "")

// MatchForms returns the forms compliance compares a tag name under: the
// name; the name with ( ) { } [ ] \ " ' stripped; that with "_" removed.
// Empty forms are omitted; duplicates collapse.
//
// TODO(S11 #6): the word-boundary "second net" (a watch entry's _-word
// sequence appearing contiguously inside a longer tag) stays OFF until it
// has been run over the whole tags table and the collision list reviewed.
func MatchForms(name string) []string {
	stripped := matchStrip.Replace(name)
	squeezed := strings.ReplaceAll(stripped, "_", "")
	out := make([]string, 0, 3)
	for _, f := range []string{name, stripped, squeezed} {
		if f == "" {
			continue
		}
		dup := false
		for _, o := range out {
			if o == f {
				dup = true
			}
		}
		if !dup {
			out = append(out, f)
		}
	}
	return out
}
