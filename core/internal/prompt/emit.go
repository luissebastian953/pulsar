package prompt

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// MaxEmitted bounds any weight the emitter will write (config ceiling).
const MaxEmitted = MaxConfigWeight

// FormatWeight writes an emitted weight for ComfyUI: four decimals with
// trailing zeros and dot trimmed. Never an exponent, NaN, Inf or -0.
func FormatWeight(w float64) string {
	s := strconv.FormatFloat(w, 'f', 4, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

// NAITolerance is FormatNAI's precision: weights are written to 6 dp.
const NAITolerance = 1e-6

// naiNum writes a NovelAI-unit weight: shortest round-tripping decimal.
func naiNum(w float64) string {
	s := strconv.FormatFloat(w, 'f', -1, 64)
	if s == "-0" {
		s = "0"
	}
	return s
}

var parenEscaper = strings.NewReplacer("(", `\(`, ")", `\)`)

// EmitComfy writes tags as one ComfyUI CLIPTextEncode string (S5): tags
// joined by ", "; a run whose weight formats as "1" is bare text; any other
// run is one flat `(text:w)` group — never nested, never spanning a comma;
// every literal paren escaped, so `castorice (honkai: star rail)` reaches
// CLIP as written instead of as a ×1.1 group with its parens dropped.
func EmitComfy(tags []Tag) (string, error) {
	var b strings.Builder
	last := byte(0)
	write := func(s string) {
		if s != "" {
			b.WriteString(s)
			last = s[len(s)-1]
		}
	}
	for i, t := range tags {
		if i > 0 {
			write(", ")
		}
		for _, r := range t.Runs {
			if math.IsNaN(r.Emitted) || math.IsInf(r.Emitted, 0) || r.Emitted <= 0 || r.Emitted > MaxEmitted {
				return "", fmt.Errorf("prompt: tag %q has emitted weight %v outside (0, %v]", t.Text, r.Emitted, MaxEmitted)
			}
			ws := FormatWeight(r.Emitted)
			lead, core, trail := splitSpace(r.Text)
			if ws == "1" || core == "" {
				write(parenEscaper.Replace(r.Text))
				continue
			}
			if ws == "0" {
				return "", fmt.Errorf("prompt: tag %q rounds to weight 0", t.Text)
			}
			write(lead)
			if last == '\\' {
				write(" ") // a trailing backslash must never escape the opener
			}
			write("(" + parenEscaper.Replace(core) + ":" + ws + ")")
			write(trail)
		}
	}
	return b.String(), nil
}

func splitSpace(s string) (lead, core, trail string) {
	core = strings.TrimLeftFunc(s, unicode.IsSpace)
	lead = s[:len(s)-len(core)]
	trimmed := strings.TrimRightFunc(core, unicode.IsSpace)
	trail = core[len(trimmed):]
	return lead, trimmed, trail
}

// FormatNAI is the reverse emitter for orbit and /translate: NovelAI
// syntax, `w::text ::` with the space guard before the closing `::` (so a
// tag ending in digits never becomes a weight), bare text at weight 1.
// Parse(FormatNAI(t)) ≡ t.
func FormatNAI(tags []Tag) string {
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		parts = append(parts, FormatTagNAI(t))
	}
	return strings.Join(parts, ", ")
}

// FormatTagNAI formats one tag. Literal parens are written bare when that
// re-parses identically (the readable Danbooru form), escaped otherwise.
func FormatTagNAI(t Tag) string {
	s := formatRuns(t.Runs, false)
	if roundTrips(s, t) {
		return s
	}
	return formatRuns(t.Runs, true)
}

func formatRuns(runs []Run, escape bool) string {
	var b strings.Builder
	endsSoft := func() bool {
		if b.Len() == 0 {
			return true
		}
		c := b.String()[b.Len()-1]
		return c == ' '
	}
	for i, r := range runs {
		text := r.Text
		if escape {
			text = parenEscaper.Replace(text)
		}
		if r.Weight == 1 {
			b.WriteString(text)
			continue
		}
		lead, core, trail := splitSpace(text)
		b.WriteString(lead)
		if !endsSoft() {
			b.WriteByte(' ') // `abc2` + `1.5::` would glue into weight 21.5
		}
		b.WriteString(naiNum(round6(r.Weight)))
		b.WriteString("::")
		if strings.HasPrefix(core, ":") {
			b.WriteByte(' ')
		}
		b.WriteString(core)
		b.WriteString(" ::")
		if i < len(runs)-1 && trail == "" {
			b.WriteByte(' ')
		}
		b.WriteString(trail)
	}
	return b.String()
}

func roundTrips(s string, t Tag) bool {
	surf, errs := Analyze(s, modeRaw)
	if len(errs) > 0 || len(surf.Tags) != 1 {
		return false
	}
	return equivalent([]Tag{t}, surf.Tags, func(r Run) float64 { return r.Weight }, NAITolerance) == nil
}

// Equivalent compares two tag lists the way ComfyUI sees them: same tag
// boundaries, same non-space runes, emitted weights within tol.
func Equivalent(want, got []Tag, tol float64) error {
	return equivalent(want, got, func(r Run) float64 { return r.Emitted }, tol)
}

// EquivalentNAI compares NovelAI-unit weights (Parse(FormatNAI(t)) ≡ t).
func EquivalentNAI(want, got []Tag, tol float64) error {
	return equivalent(want, got, func(r Run) float64 { return r.Weight }, tol)
}

type wrune struct {
	r rune
	w float64
}

func flatten(t Tag, weight func(Run) float64) []wrune {
	var out []wrune
	for _, r := range t.Runs {
		w := weight(r)
		for _, c := range r.Text {
			if !unicode.IsSpace(c) {
				out = append(out, wrune{c, w})
			}
		}
	}
	return out
}

func equivalent(want, got []Tag, weight func(Run) float64, tol float64) error {
	if len(want) != len(got) {
		return fmt.Errorf("tag count %d ≠ %d", len(want), len(got))
	}
	for i := range want {
		a, b := flatten(want[i], weight), flatten(got[i], weight)
		if len(a) != len(b) {
			return fmt.Errorf("tag %d: %q ≠ %q", i, want[i].Text, got[i].Text)
		}
		for k := range a {
			if a[k].r != b[k].r {
				return fmt.Errorf("tag %d: %q ≠ %q", i, want[i].Text, got[i].Text)
			}
			if math.IsNaN(b[k].w) || math.Abs(a[k].w-b[k].w) > tol {
				return fmt.Errorf("tag %d %q: weight %v ≠ %v", i, want[i].Text, a[k].w, b[k].w)
			}
		}
	}
	return nil
}
