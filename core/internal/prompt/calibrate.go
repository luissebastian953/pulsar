package prompt

import (
	"fmt"
	"math"
)

// Calibration maps NovelAI-unit weights onto one lineage's usable ComfyUI
// band (S4). NovelAI's numbers are UI units it recalibrated per encoder;
// SDXL's lerp band is narrow, so the curve is close to the identity near 1
// (brackets transfer almost unchanged), saturates toward Hi above 1, and is
// linear down to the floor Lo below 1. Values live in assembly's
// lineageSpecs; eval/sweeps/weighting refits them.
type Calibration struct {
	KUp   float64 `json:"k_up"`
	Hi    float64 `json:"hi"`
	KDown float64 `json:"k_down"`
	Lo    float64 `json:"lo"`
}

// Code-clamped bounds, enforced on every use like the sentinel's
// thresholds: config can tune the curve but never leave this box.
const (
	minHi, maxHi = 1.0, 1.6
	minLo, maxLo = 0.1, 1.0
	minK, maxK   = 0.0, 2.0
	// calibratedNotice: N5 fires when the curve moved a weight by more.
	calibratedNotice = 0.05
)

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return math.Min(math.Max(v, lo), hi)
}

// Clamped returns c inside the code bounds.
func (c Calibration) Clamped() Calibration {
	return Calibration{
		KUp: clamp(c.KUp, minK, maxK), Hi: clamp(c.Hi, minHi, maxHi),
		KDown: clamp(c.KDown, minK, maxK), Lo: clamp(c.Lo, minLo, maxLo),
	}
}

// Map is the curve. Invariants (tested): Map(w) ∈ [Lo, Hi] ⊂ (0, 1.6],
// Map(1) = 1, monotone non-decreasing.
func (c Calibration) Map(w float64) float64 {
	c = c.Clamped()
	switch {
	case w > 1:
		if c.Hi <= 1 {
			return 1
		}
		return 1 + (c.Hi-1)*math.Tanh(c.KUp*(w-1)/(c.Hi-1))
	case w < 1:
		return math.Max(c.Lo, 1-c.KDown*(1-w))
	}
	return 1
}

// round4 is the emitter's precision; Emitted is stored already rounded so
// the string ComfyUI parses and the structured weight agree exactly.
func round4(w float64) float64 { return math.Round(w*1e4) / 1e4 }

// Apply sets Emitted on every run and returns N5 notices for tags whose
// weight the curve compressed noticeably.
func (c Calibration) Apply(tags []Tag) []Issue {
	var notes []Issue
	for i := range tags {
		t := &tags[i]
		noted := false
		for k := range t.Runs {
			r := &t.Runs[k]
			m := c.Map(r.Weight)
			r.Emitted = round4(m)
			if !noted && math.Abs(m-r.Weight) > calibratedNotice && !isSpace(r.Text) {
				noted = true
				notes = append(notes, Issue{
					Code:    NoticeCalibrated,
					Message: fmt.Sprintf("`%s`: weight %s is sent as %s for this model", t.Text, naiNum(round6(r.Weight)), FormatWeight(r.Emitted)),
					Hint:    "SDXL's usable weight band is narrower than NovelAI's",
					Start:   t.Span.Start, End: t.Span.End, Tag: t.Span.Index,
				})
			}
		}
	}
	return notes
}

// Identity is the config-surface calibration: config prompts are written
// in emitted units (S4).
func Identity(tags []Tag) {
	for i := range tags {
		for k := range tags[i].Runs {
			tags[i].Runs[k].Emitted = round4(tags[i].Runs[k].Weight)
		}
	}
}

func isSpace(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}
