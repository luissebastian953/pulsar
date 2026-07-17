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
	Seed      *int64  `json:"seed,omitempty"`
	// Denoise applies to image-input kinds; 1.0 ignores the input
	Denoise *float64 `json:"denoise,omitempty"`
	// RefStrength is the Precise Reference fidelity (docs/precise-reference.md)
	RefStrength *float64 `json:"ref_strength,omitempty"`
	QualityTags *bool    `json:"quality_tags,omitempty"`
	UCPreset    *bool    `json:"uc_preset,omitempty"`
}

// Spec is the resolved parameter set, stored verbatim on the job row.
// Same Seed + same Spec must reproduce the same image.
type Spec struct {
	Prompt      string  `json:"prompt"`
	Negative    string  `json:"negative"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Steps       int     `json:"steps"`
	CFG         float64 `json:"cfg"`
	Sampler     string  `json:"sampler"`
	Scheduler   string  `json:"scheduler"`
	Seed        int64   `json:"seed"`
	Denoise     float64 `json:"denoise,omitempty"`
	RefStrength float64 `json:"ref_strength,omitempty"`
	BatchSize   int     `json:"batch_size"`
	Lineage     string  `json:"lineage"`
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

// unknown lineages are refused, never guessed (docs/tagdb.md)
var lineages = map[string]bool{"illustrious": true, "noobai": true}

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
	defaultSteps  = 24
	defaultCFG    = 6.0
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

	if !lineages[opts.Lineage] {
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
		s.Steps = defaultSteps
	}
	if s.Steps < 1 || s.Steps > maxSteps {
		return Spec{}, fmt.Errorf("steps must be 1..%d", maxSteps)
	}
	if s.CFG == 0 {
		s.CFG = defaultCFG
	}
	if s.CFG < 1 || s.CFG > maxCFG {
		return Spec{}, fmt.Errorf("cfg must be 1..%v", maxCFG)
	}
	if s.Sampler == "" {
		s.Sampler = "euler_ancestral"
	}
	if !samplers[s.Sampler] {
		return Spec{}, fmt.Errorf("unknown sampler %q", s.Sampler)
	}
	if s.Scheduler == "" {
		s.Scheduler = "normal"
	}
	if !schedulers[s.Scheduler] {
		return Spec{}, fmt.Errorf("unknown scheduler %q", s.Scheduler)
	}
	if req.Denoise != nil {
		if *req.Denoise < 0.05 || *req.Denoise > 0.95 {
			return Spec{}, fmt.Errorf("denoise must be 0.05..0.95")
		}
		s.Denoise = *req.Denoise
	}
	if req.RefStrength != nil {
		if *req.RefStrength < 0 || *req.RefStrength > 1.2 {
			return Spec{}, fmt.Errorf("ref_strength must be 0..1.2")
		}
		s.RefStrength = *req.RefStrength
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
