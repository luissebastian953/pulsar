// Package assembly: request → resolved Spec. Pure — no I/O, no network,
// no GPU (.claude/skills/core-job).
package assembly

import (
	"fmt"
	"strings"
	"time"
)

// Request is the raw API input. Prompt and Negative are comma-separated
// Danbooru tags for now; nltag translation slots in front of this later.
type Request struct {
	Prompt    string  `json:"prompt"`
	Negative  string  `json:"negative,omitempty"`
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	Steps     int     `json:"steps,omitempty"`
	CFG       float64 `json:"cfg,omitempty"`
	Sampler   string  `json:"sampler,omitempty"`
	Scheduler string  `json:"scheduler,omitempty"`
	// CFGRescale tempers v-pred colour burn from high guidance (0..1);
	// unset uses the lineage default, inert on eps lineages (docs/tuning.md).
	CFGRescale *float64 `json:"cfg_rescale,omitempty"`
	Seed       *int64   `json:"seed,omitempty"`
	// Denoise applies to image-input kinds; 1.0 ignores the input
	Denoise *float64 `json:"denoise,omitempty"`
	// RefStrength is the Precise Reference fidelity (docs/precise-reference.md)
	RefStrength *float64 `json:"ref_strength,omitempty"`
	// RefMode: "character" (default; alias "identity"), "style", or
	// "character_style" (docs/precise-reference.md#mapping)
	RefMode string `json:"ref_mode,omitempty"`
	// RefFidelity 0..1 maps to the adapter's step range: how long the
	// reference steers the denoise (NAI's Fidelity slider)
	RefFidelity *float64 `json:"ref_fidelity,omitempty"`
	// Hires: pixel upscale + refine pass (docs/tuning.md#quality-passes)
	Hires bool `json:"hires,omitempty"`
	// HiresScale is the refine upscale factor, 1.1..2.0 (default 1.5 —
	// "large"; 2.0 is the "ultra" tier). Requires Hires.
	HiresScale *float64 `json:"hires_scale,omitempty"`
	// HiresDenoise is the refine pass strength, 0.2..0.7 (default 0.4).
	// Higher dissolves upscale grain/banding but redraws more. Requires Hires.
	HiresDenoise *float64 `json:"hires_denoise,omitempty"`
	QualityTags  *bool    `json:"quality_tags,omitempty"`
	UCPreset     *bool    `json:"uc_preset,omitempty"`
	// Characters get regional conditioning: each prompt applies only to
	// its own column (2–3 characters; the base prompt stays global).
	Characters []Character `json:"characters,omitempty"`
	// Loras apply a style/character LoRA on the spine (one for now).
	Loras []Lora `json:"loras,omitempty"`
}

type Lora struct {
	Name     string   `json:"name"`
	Strength *float64 `json:"strength,omitempty"`
}

type Character struct {
	Prompt string `json:"prompt"`
	// ReferenceID is an upload id for a per-character Precise Reference;
	// RefStrength is its fidelity (0–1.2, default applied at the API).
	ReferenceID string   `json:"reference_id,omitempty"`
	RefStrength *float64 `json:"ref_strength,omitempty"`
}

// Spec is the resolved parameter set, stored verbatim on the job row.
// Same Seed + same Spec must reproduce the same image.
type Spec struct {
	Prompt       string      `json:"prompt"`
	Negative     string      `json:"negative"`
	Width        int         `json:"width"`
	Height       int         `json:"height"`
	Steps        int         `json:"steps"`
	CFG          float64     `json:"cfg"`
	Sampler      string      `json:"sampler"`
	Scheduler    string      `json:"scheduler"`
	CFGRescale   float64     `json:"cfg_rescale,omitempty"`
	Seed         int64       `json:"seed"`
	Denoise      float64     `json:"denoise,omitempty"`
	RefStrength  float64     `json:"ref_strength,omitempty"`
	RefMode      string      `json:"ref_mode,omitempty"`
	RefFidelity  float64     `json:"ref_fidelity,omitempty"`
	Hires        bool        `json:"hires,omitempty"`
	HiresScale   float64     `json:"hires_scale,omitempty"`
	HiresDenoise float64     `json:"hires_denoise,omitempty"`
	Characters   []Character `json:"characters,omitempty"`
	Loras        []Lora      `json:"loras,omitempty"`
	Checkpoint   string      `json:"checkpoint,omitempty"`
	BatchSize    int         `json:"batch_size"`
	Lineage      string      `json:"lineage"`
}

type Options struct {
	Lineage string // quality tags are lineage-specific (docs/tagdb.md)
	Seed    int64
	// QualityTags is the QUALITY_TAGS preset for this lineage; empty = none.
	QualityTags string
	// DefaultUC is the deployment's quality UC preset (env DEFAULT_UC,
	// shipped in .env.example). Empty = no preset.
	DefaultUC string
	// ForbiddenTagUC (env FORBIDDEN_TAG_UC) is required — Build refuses
	// empty (CLAUDE.md non-negotiables).
	ForbiddenTagUC string
}

// SDXL only behaves on-bucket (docs/tuning.md#resolution-buckets).
var buckets = map[[2]int]bool{
	{1024, 1024}: true,
	{896, 1152}:  true,
	{832, 1216}:  true,
	{1216, 832}:  true,
	{1344, 768}:  true,
}

var samplers = map[string]bool{
	"euler":           true,
	"euler_ancestral": true, // ancestral: never converges, more variation
	"dpmpp_2m":        true, // converges: the reproducible choice
}

var schedulers = map[string]bool{"normal": true, "karras": true}

// lineageSpec holds the generation defaults that don't transfer between
// model lineages (docs/tuning.md). Requests override any of these; unset
// fields fall back here. Vpred lineages additionally need the workflow's
// ModelSamplingDiscrete wrap — see workflow.ApplyVpred.
type lineageSpec struct {
	vpred      bool
	cfg        float64
	sampler    string
	scheduler  string
	steps      int
	cfgRescale float64
}

// unknown lineages are refused, never guessed (docs/tagdb.md). illustrious
// values match the historical flat defaults; noobai is v-pred (CFG 5, Euler,
// rescale ~0.7 to kill colour burn — docs/research-novelai.md §3).
var lineageSpecs = map[string]lineageSpec{
	"illustrious": {vpred: false, cfg: 6.0, sampler: "euler_ancestral", scheduler: "normal", steps: 24, cfgRescale: 0},
	"noobai":      {vpred: true, cfg: 5.0, sampler: "euler", scheduler: "normal", steps: 28, cfgRescale: 0.55},
}

// Vpred reports whether a lineage uses v-prediction, so the workflow layer
// knows to apply the ModelSamplingDiscrete wrap. Unknown lineage → false.
func Vpred(lineage string) bool { return lineageSpecs[lineage].vpred }

// CheckpointIsVpred infers a checkpoint's prediction type from its filename,
// so a lineage/checkpoint mismatch can be refused at request time instead of
// rendering garbage (a v-pred lineage wraps the model in ModelSamplingDiscrete;
// against an eps checkpoint the sampler collapses to a flat field — docs/tuning.md).
// Naming-convention only: known is false when the filename carries no marker, so
// a file we can't classify is never rejected. A "vpred" marker wins over a family
// marker (e.g. "noobai-xl-vpred").
func CheckpointIsVpred(name string) (vpred, known bool) {
	n := strings.ToLower(name)
	for _, m := range []string{"vpred", "v-pred", "v_pred", "vprediction", "v-prediction"} {
		if strings.Contains(n, m) {
			return true, true
		}
	}
	// eps markers: an explicit eps tag, or the Illustrious family, which is
	// eps in this repo (its v-pred sibling would carry a vpred marker, caught above).
	for _, m := range []string{"eps", "illustrious"} {
		if strings.Contains(n, m) {
			return false, true
		}
	}
	return false, false
}

// approximate end of each lineage's training data; advisory (docs/tagdb.md)
var knowledgeCutoff = map[string]time.Time{
	"illustrious": time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),   // Danbooru2023 dump
	"noobai":      time.Date(2024, 10, 23, 0, 0, 0, 0, time.UTC), // Danbooru through 2024-10-23
}

// KnowledgeCutoff reports when a lineage's training data ends.
func KnowledgeCutoff(lineage string) (time.Time, bool) {
	t, ok := knowledgeCutoff[lineage]
	return t, ok
}

const (
	defaultWidth  = 832
	defaultHeight = 1216
	maxSteps      = 50
	maxCFG        = 12.0
)

func Build(req Request, opts Options) (Spec, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return Spec{}, fmt.Errorf("prompt is required")
	}
	if opts.Seed < 0 {
		return Spec{}, fmt.Errorf("seed must be non-negative")
	}
	safety := strings.TrimSpace(opts.ForbiddenTagUC)
	if safety == "" {
		// fail closed (CLAUDE.md non-negotiables)
		return Spec{}, fmt.Errorf("forbidden-tag negative is not configured (FORBIDDEN_TAG_UC)")
	}

	ls, ok := lineageSpecs[opts.Lineage]
	if !ok {
		return Spec{}, fmt.Errorf("unknown lineage %q", opts.Lineage)
	}

	s := Spec{
		Width:     req.Width,
		Height:    req.Height,
		Steps:     req.Steps,
		CFG:       req.CFG,
		Sampler:   req.Sampler,
		Scheduler: req.Scheduler,
		Seed:      opts.Seed,
		BatchSize: 1,
		Lineage:   opts.Lineage,
	}
	if s.Width == 0 && s.Height == 0 {
		s.Width, s.Height = defaultWidth, defaultHeight
	}
	if !buckets[[2]int{s.Width, s.Height}] {
		return Spec{}, fmt.Errorf("resolution %dx%d is off-bucket; allowed: 1024x1024, 896x1152, 832x1216, 1216x832, 1344x768", s.Width, s.Height)
	}
	if s.Steps == 0 {
		s.Steps = ls.steps
	}
	if s.Steps < 1 || s.Steps > maxSteps {
		return Spec{}, fmt.Errorf("steps must be 1..%d", maxSteps)
	}
	if s.CFG == 0 {
		s.CFG = ls.cfg
	}
	if s.CFG < 1 || s.CFG > maxCFG {
		return Spec{}, fmt.Errorf("cfg must be 1..%v", maxCFG)
	}
	if s.Sampler == "" {
		s.Sampler = ls.sampler
	}
	if !samplers[s.Sampler] {
		return Spec{}, fmt.Errorf("unknown sampler %q", s.Sampler)
	}
	if s.Scheduler == "" {
		s.Scheduler = ls.scheduler
	}
	if !schedulers[s.Scheduler] {
		return Spec{}, fmt.Errorf("unknown scheduler %q", s.Scheduler)
	}
	s.CFGRescale = ls.cfgRescale
	if req.CFGRescale != nil {
		if *req.CFGRescale < 0 || *req.CFGRescale > 1 {
			return Spec{}, fmt.Errorf("cfg_rescale must be 0..1")
		}
		s.CFGRescale = *req.CFGRescale
	}
	if req.Denoise != nil {
		if *req.Denoise < 0.05 || *req.Denoise > 0.95 {
			return Spec{}, fmt.Errorf("denoise must be 0.05..0.95")
		}
		s.Denoise = *req.Denoise
	}
	if n := len(req.Characters); n != 0 {
		if n < 2 || n > 3 {
			return Spec{}, fmt.Errorf("characters must be 2..3 (one character belongs in the prompt)")
		}
		for i, c := range req.Characters {
			p := strings.TrimSpace(c.Prompt)
			if p == "" {
				return Spec{}, fmt.Errorf("character %d prompt is required", i+1)
			}
			if c.RefStrength != nil && (*c.RefStrength < 0 || *c.RefStrength > 1.2) {
				return Spec{}, fmt.Errorf("character %d ref_strength must be 0..1.2", i+1)
			}
			s.Characters = append(s.Characters, Character{
				Prompt: p, ReferenceID: c.ReferenceID, RefStrength: c.RefStrength,
			})
		}
	}
	if req.RefStrength != nil {
		if *req.RefStrength < 0 || *req.RefStrength > 1.2 {
			return Spec{}, fmt.Errorf("ref_strength must be 0..1.2")
		}
		s.RefStrength = *req.RefStrength
	}
	switch req.RefMode {
	case "", "identity", "character", "style", "character_style":
		s.RefMode = req.RefMode
	default:
		return Spec{}, fmt.Errorf("ref_mode must be character, style, or character_style")
	}
	if req.RefFidelity != nil {
		if *req.RefFidelity < 0 || *req.RefFidelity > 1 {
			return Spec{}, fmt.Errorf("ref_fidelity must be 0..1")
		}
		s.RefFidelity = *req.RefFidelity
	}
	s.Hires = req.Hires
	if req.HiresScale != nil {
		if !req.Hires {
			return Spec{}, fmt.Errorf("hires_scale requires hires")
		}
		if *req.HiresScale < 1.1 || *req.HiresScale > 2.0 {
			return Spec{}, fmt.Errorf("hires_scale must be 1.1..2.0")
		}
		s.HiresScale = *req.HiresScale
	}
	if s.Hires && s.HiresScale == 0 {
		s.HiresScale = 1.5
	}
	if req.HiresDenoise != nil {
		if !req.Hires {
			return Spec{}, fmt.Errorf("hires_denoise requires hires")
		}
		if *req.HiresDenoise < 0.2 || *req.HiresDenoise > 0.7 {
			return Spec{}, fmt.Errorf("hires_denoise must be 0.2..0.7")
		}
		s.HiresDenoise = *req.HiresDenoise
	}
	if s.Hires && s.HiresDenoise == 0 {
		s.HiresDenoise = 0.4
	}
	// two chained loaders (e.g. character identity + franchise style);
	// deeper stacks dilute both and wait for a real use case
	if len(req.Loras) > 2 {
		return Spec{}, fmt.Errorf("at most two LoRAs are supported")
	}
	for _, l := range req.Loras {
		if strings.TrimSpace(l.Name) == "" {
			return Spec{}, fmt.Errorf("lora name is required")
		}
		st := 0.8
		if l.Strength != nil {
			if *l.Strength < 0 || *l.Strength > 1.5 {
				return Spec{}, fmt.Errorf("lora strength must be 0..1.5")
			}
			st = *l.Strength
		}
		s.Loras = append(s.Loras, Lora{Name: l.Name, Strength: &st})
	}

	if quality := strings.TrimSpace(opts.QualityTags); quality != "" && (req.QualityTags == nil || *req.QualityTags) {
		s.Prompt = quality + ", " + prompt
	} else {
		s.Prompt = prompt
	}

	negative := strings.TrimSpace(req.Negative)
	if uc := strings.TrimSpace(opts.DefaultUC); uc != "" && (req.UCPreset == nil || *req.UCPreset) {
		s.Negative = uc
		if negative != "" {
			s.Negative += ", " + negative
		}
	} else {
		s.Negative = negative
	}
	if s.Negative == "" {
		s.Negative = safety
	} else {
		s.Negative = safety + ", " + s.Negative
	}

	return s, nil
}
