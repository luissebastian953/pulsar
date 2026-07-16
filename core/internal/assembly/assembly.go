// Package assembly turns a generation request into a resolved Spec.
// It is pure: no I/O, no network, no GPU. Anything nondeterministic (the seed)
// is resolved by the caller and passed in via Options.
package assembly

import (
	"fmt"
	"strings"
)

// Request is the raw API input. Prompt and Negative are comma-separated
// Danbooru tags for now; nltag translation slots in front of this later.
type Request struct {
	Prompt      string  `json:"prompt"`
	Negative    string  `json:"negative,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	Steps       int     `json:"steps,omitempty"`
	CFG         float64 `json:"cfg,omitempty"`
	Sampler     string  `json:"sampler,omitempty"`
	Scheduler   string  `json:"scheduler,omitempty"`
	Seed        *int64  `json:"seed,omitempty"`
	QualityTags *bool   `json:"quality_tags,omitempty"`
	UCPreset    *bool   `json:"uc_preset,omitempty"`
}

// Spec is the resolved parameter set, stored verbatim on the job row.
// Same Seed + same Spec must reproduce the same image.
type Spec struct {
	Prompt    string  `json:"prompt"`
	Negative  string  `json:"negative"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Steps     int     `json:"steps"`
	CFG       float64 `json:"cfg"`
	Sampler   string  `json:"sampler"`
	Scheduler string  `json:"scheduler"`
	Seed      int64   `json:"seed"`
	BatchSize int     `json:"batch_size"`
	Lineage   string  `json:"lineage"`
}

type Options struct {
	Lineage string // quality tags are lineage-specific (docs/tagdb.md)
	Seed    int64
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

var qualityTags = map[string][]string{
	"illustrious": {"masterpiece", "best quality", "very aesthetic", "absurdres"},
	"noobai":      {"masterpiece", "best quality", "very aesthetic", "absurdres", "very awa"},
}

const defaultUC = "lowres, worst quality, bad quality, bad anatomy, bad hands, jpeg artifacts, signature, watermark"

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

	quality, ok := qualityTags[opts.Lineage]
	if !ok {
		// wrong-family quality tags are a silent accuracy killer — refuse
		// rather than guess (docs/tagdb.md#quality-tags-are-lineage-specific)
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

	if req.QualityTags == nil || *req.QualityTags {
		s.Prompt = strings.Join(quality, ", ") + ", " + prompt
	} else {
		s.Prompt = prompt
	}

	negative := strings.TrimSpace(req.Negative)
	if req.UCPreset == nil || *req.UCPreset {
		s.Negative = defaultUC
		if negative != "" {
			s.Negative += ", " + negative
		}
	} else {
		s.Negative = negative
	}

	return s, nil
}
