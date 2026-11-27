// Package style: the deployment's style presets — the recipe rows behind
// the /create style picker (docs/tuning.md#style-presets). The store only
// reads rows; assembly.Build merges a preset as data, so this package has
// no prompt logic of its own.
package style

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/core/internal/assembly"
)

var ErrNotFound = errors.New("style not found")

// Preset is one style_presets row, keyed by (Slug, Lineage): recipes don't
// transfer between model lineages, so the same slug may exist per lineage
// with different settings. Nil/zero settings mean "leave it to the request
// or the lineage default".
type Preset struct {
	Slug        string `json:"slug"`
	Lineage     string `json:"lineage"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Family groups the picker: mihoyo | anime | illustration
	Family string `json:"family"`
	// Lora is a filename under LORA_DIR; empty = tags only
	Lora         string  `json:"lora,omitempty"`
	LoraStrength float64 `json:"lora_strength,omitempty"`
	PromptPrefix string  `json:"prompt_prefix"`
	PromptSuffix string  `json:"prompt_suffix"`
	Negative     string  `json:"negative"`
	Sampler      string  `json:"sampler,omitempty"`
	Scheduler    string  `json:"scheduler,omitempty"`
	CFG          float64 `json:"cfg,omitempty"`
	Steps        int     `json:"steps,omitempty"`
	// CFGRescale: nil = lineage default; 0 is a real value (rescale off)
	CFGRescale *float64 `json:"cfg_rescale,omitempty"`
	// Hires is the recommended size tier for the client to preselect; core
	// never switches hires on from a style. HiresScale/HiresDenoise fill a
	// hires request that left them unset.
	Hires        *bool   `json:"hires,omitempty"`
	HiresScale   float64 `json:"hires_scale,omitempty"`
	HiresDenoise float64 `json:"hires_denoise,omitempty"`
	// Available: the preset can run on this deployment — it has no LoRA or
	// its LoRA is installed. Set by the API from LORA_DIR, not stored.
	Available bool `json:"available"`
}

// Assembly projects the preset onto assembly's data shape.
func (p *Preset) Assembly() assembly.Style {
	return assembly.Style{
		Slug:         p.Slug,
		PromptPrefix: p.PromptPrefix,
		PromptSuffix: p.PromptSuffix,
		Negative:     p.Negative,
		Lora:         p.Lora,
		LoraStrength: p.LoraStrength,
		Sampler:      p.Sampler,
		Scheduler:    p.Scheduler,
		CFG:          p.CFG,
		Steps:        p.Steps,
		CFGRescale:   p.CFGRescale,
		HiresScale:   p.HiresScale,
		HiresDenoise: p.HiresDenoise,
	}
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const columns = `slug, lineage, name, description, family, lora, lora_strength,
	prompt_prefix, prompt_suffix, negative, sampler, scheduler, cfg, steps,
	cfg_rescale, hires, hires_scale, hires_denoise`

func scan(row pgx.Row) (*Preset, error) {
	var (
		p                                 Preset
		lora, sampler, scheduler          *string
		loraStrength, cfg, scale, denoise *float64
		steps                             *int
	)
	err := row.Scan(&p.Slug, &p.Lineage, &p.Name, &p.Description, &p.Family, &lora, &loraStrength,
		&p.PromptPrefix, &p.PromptSuffix, &p.Negative, &sampler, &scheduler, &cfg, &steps,
		&p.CFGRescale, &p.Hires, &scale, &denoise)
	if err != nil {
		return nil, err
	}
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	derefF := func(f *float64) float64 {
		if f == nil {
			return 0
		}
		return *f
	}
	p.Lora, p.Sampler, p.Scheduler = deref(lora), deref(sampler), deref(scheduler)
	p.LoraStrength, p.CFG, p.HiresScale, p.HiresDenoise = derefF(loraStrength), derefF(cfg), derefF(scale), derefF(denoise)
	if steps != nil {
		p.Steps = *steps
	}
	return &p, nil
}

// List returns every enabled preset, picker order. Never nil.
func (s *Store) List(ctx context.Context) ([]Preset, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM style_presets WHERE enabled ORDER BY sort_order, name, lineage`)
	if err != nil {
		return nil, fmt.Errorf("list styles: %w", err)
	}
	defer rows.Close()
	out := []Preset{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list styles: %w", err)
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// Get resolves one enabled preset for a lineage; ErrNotFound covers both an
// unknown slug and a slug that exists only for another lineage.
func (s *Store) Get(ctx context.Context, slug, lineage string) (*Preset, error) {
	p, err := scan(s.pool.QueryRow(ctx,
		`SELECT `+columns+` FROM style_presets WHERE slug = $1 AND lineage = $2 AND enabled`, slug, lineage))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get style %q/%q: %w", slug, lineage, err)
	}
	return p, nil
}
