// Package assembly: request → resolved Spec. Pure — no I/O, no network,
// no GPU (.claude/skills/core-job).
package assembly

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"pulsar/core/internal/prompt"
)

// Request is the raw API input. Prompt, Negative and each character prompt
// are prompt surfaces in the NovelAI weighting dialect (`{x}`, `[x]`,
// `1.3::x ::`; SD `(x:1.3)` auto-converts) — parsed by ParseRequest, never
// passed to ComfyUI verbatim (docs/tagdb.md#weighting).
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
	// Prompt is the user's text on a Request and the emitted ComfyUI
	// string on a Spec.
	Prompt string `json:"prompt"`
	// ReferenceID is an upload id for a per-character Precise Reference;
	// RefStrength is its fidelity (0–1.2, default applied at the API).
	ReferenceID string   `json:"reference_id,omitempty"`
	RefStrength *float64 `json:"ref_strength,omitempty"`
	// Tags is the structure Prompt was emitted from (Spec only; a value on
	// a request is ignored — BuildParsed always rebuilds it).
	Tags []prompt.Tag `json:"tags,omitempty"`
}

// Spec is the resolved parameter set, stored verbatim on the job row.
// Same Seed + same Spec must reproduce the same image. Prompt, Negative
// and Characters[].Prompt are the EMITTED ComfyUI strings the workflow is
// patched with; PromptTags/NegativeTags/Characters[].Tags are the
// structure they were emitted from, which compliance re-checks against a
// fresh ComfyUI-grammar parse of the strings (docs/compliance.md).
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
	// Style is the preset slug that was merged in (docs/tuning.md#style-presets).
	// Its tags/negative/LoRA are already resolved into the fields above, so a
	// later edit to the preset never changes what this row reproduces.
	Style string `json:"style,omitempty"`
	// PromptTags / NegativeTags: every emitted tag with its Source (quality,
	// style, user, anchor; safety first in the negative). History restores
	// editor boxes from the user-sourced ones.
	PromptTags   []prompt.Tag `json:"prompt_tags,omitempty"`
	NegativeTags []prompt.Tag `json:"negative_tags,omitempty"`
	// Calibration is the lineage curve user weights went through, so
	// history shows why a weight came out as it did.
	Calibration *prompt.Calibration `json:"calibration,omitempty"`
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
	// Style is the resolved preset for this request, nil for none. The API
	// looks it up; Build only merges it (docs/tuning.md#style-presets).
	Style *Style
}

// Style is a style preset as data: a deployment's recipe for a look. Prompt
// fields merge unconditionally — carriers lead the prompt, the finish trails
// it, the negative appends after the user's. The LoRA attaches unless the
// request already carries it. Settings fill only what the request left
// unset (request > style > lineage default) and pass the same validation.
// A style never switches hires on: that stays the request's call, so
// HiresScale/HiresDenoise apply only to a request that asked for hires.
type Style struct {
	Slug         string
	PromptPrefix string
	PromptSuffix string
	Negative     string
	Lora         string
	LoraStrength float64 // 0 = the LoRA default (0.8)
	Sampler      string
	Scheduler    string
	CFG          float64
	Steps        int
	CFGRescale   *float64 // nil = lineage default; 0 is a real value (off)
	HiresScale   float64
	HiresDenoise float64
}

// Buckets is the closed set of allowed SDXL resolutions — SDXL only
// behaves on-bucket (docs/tuning.md#resolution-buckets). Ordered: ingest's
// nearest-fit iterates it and ties break toward earlier entries.
var Buckets = [][2]int{{1024, 1024}, {896, 1152}, {832, 1216}, {1216, 832}, {1344, 768}}

// OnBucket reports whether w×h is an allowed generation resolution.
func OnBucket(w, h int) bool {
	for _, b := range Buckets {
		if b[0] == w && b[1] == h {
			return true
		}
	}
	return false
}

// bucketList renders Buckets for error text, so the message can never rot
// out of sync with the real set.
func bucketList() string {
	parts := make([]string, len(Buckets))
	for i, b := range Buckets {
		parts[i] = fmt.Sprintf("%dx%d", b[0], b[1])
	}
	return strings.Join(parts, ", ")
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
	// calibration maps NovelAI-unit prompt weights onto this lineage's
	// usable ComfyUI band (docs/tuning.md#prompt-weights)
	calibration prompt.Calibration
}

// unknown lineages are refused, never guessed (docs/tagdb.md). illustrious
// values match the historical flat defaults; noobai is v-pred (CFG 5, Euler,
// rescale ~0.7 to kill colour burn — docs/research-novelai.md §3).
//
// Calibrations are the spec's starting values (reports/NovelAI prompt
// weighting on SDXL.md S4), NOT measured limits: eval/sweeps/weighting
// refits them. v-pred is more CFG-sensitive, hence noobai's lower ceiling.
var lineageSpecs = map[string]lineageSpec{
	"illustrious": {vpred: false, cfg: 6.0, sampler: "euler_ancestral", scheduler: "normal", steps: 24, cfgRescale: 0,
		calibration: prompt.Calibration{KUp: 1.0, Hi: 1.4, KDown: 1.0, Lo: 0.3}},
	"noobai": {vpred: true, cfg: 5.0, sampler: "euler", scheduler: "normal", steps: 28, cfgRescale: 0.55,
		calibration: prompt.Calibration{KUp: 1.0, Hi: 1.3, KDown: 1.0, Lo: 0.3}},
}

// LineageCalibration is the weight curve for a lineage; ok=false for an
// unknown lineage (refused, never guessed).
func LineageCalibration(lineage string) (prompt.Calibration, bool) {
	ls, ok := lineageSpecs[lineage]
	return ls.calibration.Clamped(), ok
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

// Notice is a parse or assembly notice on one request field ("prompt",
// "negative", "characters.N"); offsets are into that field's text.
type Notice struct {
	Field string `json:"field"`
	prompt.Issue
}

// FieldError is a request prompt that failed to parse — the API's 400,
// located on its field.
type FieldError struct {
	Field string
	Issue prompt.Issue
}

func (e *FieldError) Error() string {
	msg := e.Field + ": " + e.Issue.Message
	if e.Issue.Hint != "" {
		msg += " — " + e.Issue.Hint
	}
	return msg
}

// ConfigError is a deployment prompt (QUALITY_TAGS, DEFAULT_UC,
// FORBIDDEN_TAG_UC, a style row) that failed to parse: an operator
// problem, never the user's — the API answers 500 and logs it.
type ConfigError struct {
	Source string
	Err    error
}

func (e *ConfigError) Error() string { return e.Source + ": " + e.Err.Error() }
func (e *ConfigError) Unwrap() error { return e.Err }

// Parsed is a request's prompt surfaces, parsed. Each surface starts at
// weight 1.0 — nothing carries between them.
type Parsed struct {
	Prompt     prompt.Surface
	Negative   prompt.Surface
	Characters []prompt.Surface
	Notices    []Notice
}

func fieldErr(field string, err error) error {
	var pe *prompt.Error
	if errors.As(err, &pe) {
		return &FieldError{Field: field, Issue: pe.Issue}
	}
	return fmt.Errorf("%s: %w", field, err)
}

func withField(field string, issues []prompt.Issue) []Notice {
	out := make([]Notice, 0, len(issues))
	for _, i := range issues {
		out = append(out, Notice{Field: field, Issue: i})
	}
	return out
}

// CharacterField names a character prompt field in errors and notices.
func CharacterField(i int) string { return fmt.Sprintf("characters.%d", i) }

// ParseRequest parses the user's prompt surfaces (S1 step 2). Errors are
// *FieldError.
func ParseRequest(req Request) (Parsed, error) {
	var p Parsed
	var err error
	if p.Prompt, err = prompt.Parse(req.Prompt, prompt.Positive); err != nil {
		return Parsed{}, fieldErr("prompt", err)
	}
	p.Notices = append(p.Notices, withField("prompt", p.Prompt.Notices)...)
	if p.Negative, err = prompt.Parse(req.Negative, prompt.Negative); err != nil {
		return Parsed{}, fieldErr("negative", err)
	}
	p.Notices = append(p.Notices, withField("negative", p.Negative.Notices)...)
	for i, c := range req.Characters {
		cs, err := prompt.Parse(c.Prompt, prompt.Positive)
		if err != nil {
			return Parsed{}, fieldErr(CharacterField(i), err)
		}
		p.Characters = append(p.Characters, cs)
		p.Notices = append(p.Notices, withField(CharacterField(i), cs.Notices)...)
	}
	return p, nil
}

// CheckConfig parses the deployment prompt config at boot: any error
// refuses boot, the same fail-closed pattern as FORBIDDEN_TAG_UC presence.
// FORBIDDEN_TAG_UC must be plain tags with no operator at all.
func CheckConfig(qualityTags, defaultUC, forbiddenUC string) error {
	if strings.TrimSpace(forbiddenUC) == "" {
		return fmt.Errorf("FORBIDDEN_TAG_UC is empty")
	}
	if _, err := prompt.ParseSafety(forbiddenUC); err != nil {
		return &ConfigError{"FORBIDDEN_TAG_UC", err}
	}
	if _, err := prompt.Parse(qualityTags, prompt.ConfigPositive); err != nil {
		return &ConfigError{"QUALITY_TAGS", err}
	}
	if _, err := prompt.Parse(defaultUC, prompt.ConfigNegative); err != nil {
		return &ConfigError{"DEFAULT_UC", err}
	}
	return nil
}

// Build is ParseRequest + BuildParsed, for callers without anchors.
func Build(req Request, opts Options) (Spec, error) {
	p, err := ParseRequest(req)
	if err != nil {
		return Spec{}, err
	}
	s, _, err := BuildParsed(req, p, opts)
	return s, err
}

func configTags(text string, m prompt.Mode, src prompt.Source, source string) ([]prompt.Tag, error) {
	surf, err := prompt.Parse(text, m)
	if err != nil {
		return nil, &ConfigError{source, err}
	}
	prompt.Identity(surf.Tags)
	for i := range surf.Tags {
		surf.Tags[i].Source = src
	}
	return surf.Tags, nil
}

func userTags(in []prompt.Tag, cal prompt.Calibration, field string) ([]prompt.Tag, []Notice) {
	tags := make([]prompt.Tag, len(in))
	for i, t := range in {
		t.Runs = append([]prompt.Run(nil), t.Runs...)
		if t.Source != prompt.Anchor {
			t.Source = prompt.User
		}
		tags[i] = t
	}
	notes := withField(field, cal.Apply(tags))
	for i := range tags {
		tags[i].NAI = prompt.FormatTagNAI(tags[i])
	}
	return tags, notes
}

func hasForm(set map[string]bool, name string) bool {
	for _, f := range prompt.MatchForms(name) {
		if set[f] || set[strings.ReplaceAll(f, "_", "")] {
			return true
		}
	}
	return false
}

func addForms(set map[string]bool, name string) {
	for _, f := range prompt.MatchForms(name) {
		set[f] = true
		set[strings.ReplaceAll(f, "_", "")] = true
	}
}

func allOne(t prompt.Tag) bool {
	for _, r := range t.Runs {
		if r.Emitted != 1 {
			return false
		}
	}
	return true
}

// BuildParsed assembles a parsed request (S1 step 5): calibrate, relocate,
// dedupe the negative, emit, fill the structured Spec. Errors are
// *FieldError (400), *ConfigError (500) or plain validation errors (400).
func BuildParsed(req Request, p Parsed, opts Options) (Spec, []Notice, error) {
	if len(p.Prompt.Tags) == 0 {
		return Spec{}, nil, fmt.Errorf("prompt is required")
	}
	if opts.Seed < 0 {
		return Spec{}, nil, fmt.Errorf("seed must be non-negative")
	}
	if strings.TrimSpace(opts.ForbiddenTagUC) == "" {
		// fail closed (CLAUDE.md non-negotiables)
		return Spec{}, nil, fmt.Errorf("forbidden-tag negative is not configured (FORBIDDEN_TAG_UC)")
	}
	safety, err := prompt.ParseSafety(opts.ForbiddenTagUC)
	if err != nil {
		return Spec{}, nil, &ConfigError{"FORBIDDEN_TAG_UC", err} // fail closed
	}

	ls, ok := lineageSpecs[opts.Lineage]
	if !ok {
		return Spec{}, nil, fmt.Errorf("unknown lineage %q", opts.Lineage)
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
	if !OnBucket(s.Width, s.Height) {
		return Spec{}, nil, fmt.Errorf("resolution %dx%d is off-bucket; allowed: %s", s.Width, s.Height, bucketList())
	}
	// style settings sit between the request and the lineage defaults; they
	// run through the same gates below as request values
	sty := opts.Style
	if sty == nil {
		sty = &Style{}
	}
	if s.Steps == 0 {
		s.Steps = sty.Steps
	}
	if s.Steps == 0 {
		s.Steps = ls.steps
	}
	if s.Steps < 1 || s.Steps > maxSteps {
		return Spec{}, nil, fmt.Errorf("steps must be 1..%d", maxSteps)
	}
	if s.CFG == 0 {
		s.CFG = sty.CFG
	}
	if s.CFG == 0 {
		s.CFG = ls.cfg
	}
	if s.CFG < 1 || s.CFG > maxCFG {
		return Spec{}, nil, fmt.Errorf("cfg must be 1..%v", maxCFG)
	}
	if s.Sampler == "" {
		s.Sampler = sty.Sampler
	}
	if s.Sampler == "" {
		s.Sampler = ls.sampler
	}
	if !samplers[s.Sampler] {
		return Spec{}, nil, fmt.Errorf("unknown sampler %q", s.Sampler)
	}
	if s.Scheduler == "" {
		s.Scheduler = sty.Scheduler
	}
	if s.Scheduler == "" {
		s.Scheduler = ls.scheduler
	}
	if !schedulers[s.Scheduler] {
		return Spec{}, nil, fmt.Errorf("unknown scheduler %q", s.Scheduler)
	}
	s.CFGRescale = ls.cfgRescale
	if sty.CFGRescale != nil {
		if *sty.CFGRescale < 0 || *sty.CFGRescale > 1 {
			return Spec{}, nil, fmt.Errorf("style %q cfg_rescale must be 0..1", sty.Slug)
		}
		s.CFGRescale = *sty.CFGRescale
	}
	if req.CFGRescale != nil {
		if *req.CFGRescale < 0 || *req.CFGRescale > 1 {
			return Spec{}, nil, fmt.Errorf("cfg_rescale must be 0..1")
		}
		s.CFGRescale = *req.CFGRescale
	}
	if req.Denoise != nil {
		if *req.Denoise < 0.05 || *req.Denoise > 0.95 {
			return Spec{}, nil, fmt.Errorf("denoise must be 0.05..0.95")
		}
		s.Denoise = *req.Denoise
	}
	cal := ls.calibration.Clamped()
	var notices []Notice
	notices = append(notices, p.Notices...)
	// relocated: negative-weight tags lifted out of the positive surfaces,
	// bound for the user segment of the negative (decision 1)
	var relocated []prompt.Tag
	var relocField []string // the field each relocated tag came from
	reloc := func(field string, surf prompt.Surface) {
		tags, notes := userTags(surf.Relocated, cal, field)
		for range tags {
			relocField = append(relocField, field)
		}
		relocated = append(relocated, tags...)
		notices = append(notices, notes...)
	}
	reloc("prompt", p.Prompt)
	if n := len(req.Characters); n != 0 {
		if n < 2 || n > 3 {
			return Spec{}, nil, fmt.Errorf("characters must be 2..3 (one character belongs in the prompt)")
		}
		if len(p.Characters) != n {
			return Spec{}, nil, fmt.Errorf("characters were not parsed")
		}
		for i, c := range req.Characters {
			if len(p.Characters[i].Tags) == 0 {
				return Spec{}, nil, fmt.Errorf("character %d prompt is required", i+1)
			}
			if c.RefStrength != nil && (*c.RefStrength < 0 || *c.RefStrength > 1.2) {
				return Spec{}, nil, fmt.Errorf("character %d ref_strength must be 0..1.2", i+1)
			}
			field := CharacterField(i)
			tags, notes := userTags(p.Characters[i].Tags, cal, field)
			notices = append(notices, notes...)
			emitted, err := prompt.EmitComfy(tags)
			if err != nil {
				return Spec{}, nil, fmt.Errorf("%s: %w", field, err)
			}
			reloc(field, p.Characters[i])
			s.Characters = append(s.Characters, Character{
				Prompt: emitted, ReferenceID: c.ReferenceID, RefStrength: c.RefStrength, Tags: tags,
			})
		}
	}
	if req.RefStrength != nil {
		if *req.RefStrength < 0 || *req.RefStrength > 1.2 {
			return Spec{}, nil, fmt.Errorf("ref_strength must be 0..1.2")
		}
		s.RefStrength = *req.RefStrength
	}
	switch req.RefMode {
	case "", "identity", "character", "style", "character_style":
		s.RefMode = req.RefMode
	default:
		return Spec{}, nil, fmt.Errorf("ref_mode must be character, style, or character_style")
	}
	if req.RefFidelity != nil {
		if *req.RefFidelity < 0 || *req.RefFidelity > 1 {
			return Spec{}, nil, fmt.Errorf("ref_fidelity must be 0..1")
		}
		s.RefFidelity = *req.RefFidelity
	}
	s.Hires = req.Hires
	if req.HiresScale != nil {
		if !req.Hires {
			return Spec{}, nil, fmt.Errorf("hires_scale requires hires")
		}
		if *req.HiresScale < 1.1 || *req.HiresScale > 2.0 {
			return Spec{}, nil, fmt.Errorf("hires_scale must be 1.1..2.0")
		}
		s.HiresScale = *req.HiresScale
	}
	if s.Hires && s.HiresScale == 0 && sty.HiresScale != 0 {
		if sty.HiresScale < 1.1 || sty.HiresScale > 2.0 {
			return Spec{}, nil, fmt.Errorf("style %q hires_scale must be 1.1..2.0", sty.Slug)
		}
		s.HiresScale = sty.HiresScale
	}
	if s.Hires && s.HiresScale == 0 {
		s.HiresScale = 1.5
	}
	if req.HiresDenoise != nil {
		if !req.Hires {
			return Spec{}, nil, fmt.Errorf("hires_denoise requires hires")
		}
		if *req.HiresDenoise < 0.2 || *req.HiresDenoise > 0.7 {
			return Spec{}, nil, fmt.Errorf("hires_denoise must be 0.2..0.7")
		}
		s.HiresDenoise = *req.HiresDenoise
	}
	if s.Hires && s.HiresDenoise == 0 && sty.HiresDenoise != 0 {
		if sty.HiresDenoise < 0.2 || sty.HiresDenoise > 0.7 {
			return Spec{}, nil, fmt.Errorf("style %q hires_denoise must be 0.2..0.7", sty.Slug)
		}
		s.HiresDenoise = sty.HiresDenoise
	}
	if s.Hires && s.HiresDenoise == 0 {
		s.HiresDenoise = 0.4
	}
	// two chained loaders (e.g. character identity + franchise style);
	// deeper stacks dilute both and wait for a real use case
	if len(req.Loras) > 2 {
		return Spec{}, nil, fmt.Errorf("at most two LoRAs are supported")
	}
	for _, l := range req.Loras {
		if strings.TrimSpace(l.Name) == "" {
			return Spec{}, nil, fmt.Errorf("lora name is required")
		}
		st := 0.8
		if l.Strength != nil {
			if *l.Strength < 0 || *l.Strength > 1.5 {
				return Spec{}, nil, fmt.Errorf("lora strength must be 0..1.5")
			}
			st = *l.Strength
		}
		s.Loras = append(s.Loras, Lora{Name: l.Name, Strength: &st})
	}
	// the style's LoRA attaches behind the request's; a request that already
	// names it keeps its own strength
	if sty.Lora != "" {
		carried := false
		for _, l := range s.Loras {
			if l.Name == sty.Lora {
				carried = true
			}
		}
		if !carried {
			if len(s.Loras) >= 2 {
				return Spec{}, nil, fmt.Errorf("style %q adds LoRA %q; at most two LoRAs are supported", sty.Slug, sty.Lora)
			}
			st := 0.8
			if sty.LoraStrength != 0 {
				if sty.LoraStrength < 0 || sty.LoraStrength > 1.5 {
					return Spec{}, nil, fmt.Errorf("style %q lora_strength must be 0..1.5", sty.Slug)
				}
				st = sty.LoraStrength
			}
			s.Loras = append(s.Loras, Lora{Name: sty.Lora, Strength: &st})
		}
	}

	// quality → style carriers → the user's prompt → style finish: earlier
	// weighs more, and the user's tags keep the subject (docs/tagdb.md#tag-ordering).
	// Config surfaces are emitted units with identity calibration (S4).
	styleSrc := "style " + sty.Slug
	var pos []prompt.Tag
	if req.QualityTags == nil || *req.QualityTags {
		q, err := configTags(opts.QualityTags, prompt.ConfigPositive, prompt.Quality, "QUALITY_TAGS")
		if err != nil {
			return Spec{}, nil, err
		}
		pos = append(pos, q...)
	}
	prefix, err := configTags(sty.PromptPrefix, prompt.ConfigPositive, prompt.StylePrefix, styleSrc+" prompt_prefix")
	if err != nil {
		return Spec{}, nil, err
	}
	pos = append(pos, prefix...)
	user, notes := userTags(p.Prompt.Tags, cal, "prompt")
	notices = append(notices, notes...)
	pos = append(pos, user...)
	suffix, err := configTags(sty.PromptSuffix, prompt.ConfigPositive, prompt.StyleSuffix, styleSrc+" prompt_suffix")
	if err != nil {
		return Spec{}, nil, err
	}
	pos = append(pos, suffix...)
	if s.Prompt, err = prompt.EmitComfy(pos); err != nil {
		return Spec{}, nil, fmt.Errorf("prompt: %w", err)
	}
	s.PromptTags = pos

	// safety first, always, at exactly weight 1; then the UC preset, the
	// user's negative (plus relocated tags), the style's
	neg := append([]prompt.Tag(nil), safety...)
	seenSafety := map[string]bool{}
	for _, t := range safety {
		addForms(seenSafety, t.Name)
	}
	seenUC := map[string]bool{}
	if req.UCPreset == nil || *req.UCPreset {
		uc, err := configTags(opts.DefaultUC, prompt.ConfigNegative, prompt.DefaultUC, "DEFAULT_UC")
		if err != nil {
			return Spec{}, nil, err
		}
		for _, t := range uc {
			if !hasForm(seenSafety, t.Name) {
				neg = append(neg, t)
				addForms(seenUC, t.Name)
			}
		}
	}
	userNeg, notes := userTags(p.Negative.Tags, cal, "negative")
	notices = append(notices, notes...)
	styleNeg, err := configTags(sty.Negative, prompt.ConfigNegative, prompt.StyleNegative, styleSrc+" negative")
	if err != nil {
		return Spec{}, nil, err
	}
	// dedupe (S5): a later copy of a mandatory or preset tag adds nothing
	// but a way to re-weight it — and the history round-trip re-sends the
	// whole stored negative as user text. The parsed safety block stays the
	// only copy of the safety terms, at weight 1.
	for gi, group := range [][]prompt.Tag{userNeg, relocated, styleNeg} {
		for ti, t := range group {
			dup := hasForm(seenSafety, t.Name) || (hasForm(seenUC, t.Name) && allOne(t))
			if dup {
				if t.Source == prompt.User {
					field := "negative"
					if gi == 1 {
						field = relocField[ti]
					}
					notices = append(notices, Notice{Field: field, Issue: prompt.Issue{
						Code: prompt.NoticeDuplicateUC, Message: fmt.Sprintf("`%s` is already in the default negative", t.Text),
						Start: t.Span.Start, End: t.Span.End, Tag: t.Span.Index,
					}})
				}
				continue
			}
			neg = append(neg, t)
		}
	}
	if s.Negative, err = prompt.EmitComfy(neg); err != nil {
		return Spec{}, nil, fmt.Errorf("negative: %w", err)
	}
	s.NegativeTags = neg
	s.Calibration = &cal
	s.Style = sty.Slug

	return s, notices, nil
}
