// Package workflow loads versioned API-format ComfyUI templates and patches
// values by node ID. Graphs are never constructed or rewired in code — the
// JSON in workflows/ is the single source of truth.
package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"pulsar/core/internal/assembly"
)

type Node struct {
	ClassType string         `json:"class_type"`
	Inputs    map[string]any `json:"inputs"`
}

type Graph map[string]*Node

// Load reads a template and verifies its content hash. A mismatch means the
// file was edited without bumping the pinned hash (and likely without
// checking node IDs) — fail fast rather than patch the wrong nodes.
func Load(dir, name, wantSHA256 string) (Graph, error) {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	got := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	if got != wantSHA256 {
		return nil, fmt.Errorf("template %s hash %s does not match pinned %s — if the edit is intentional, bump the constant in internal/workflow/nodes.go and re-check node IDs", name, got, wantSHA256)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	for id, n := range g {
		if n == nil || n.ClassType == "" {
			// editor-format exports have no class_type per node — the most
			// common authoring mistake (.claude/skills/comfy-workflow/)
			return nil, fmt.Errorf("template %s node %s has no class_type — is this an editor-format export instead of API format?", name, id)
		}
	}
	return g, nil
}

func (g Graph) Clone() (Graph, error) {
	raw, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	var out Graph
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (g Graph) Set(nodeID, input string, value any) error {
	n, ok := g[nodeID]
	if !ok {
		return fmt.Errorf("node %s not in graph", nodeID)
	}
	n.Inputs[input] = value
	return nil
}

// BuildTxt2Img patches a txt2img template with a resolved spec and returns
// submit-ready API-format JSON.
func BuildTxt2Img(tpl Graph, spec assembly.Spec, checkpoint string) (json.RawMessage, error) {
	g, err := tpl.Clone()
	if err != nil {
		return nil, err
	}
	n := Txt2Img
	for _, p := range []struct {
		node, input string
		value       any
	}{
		{n.Checkpoint, "ckpt_name", checkpoint},
		{n.Positive, "text", spec.Prompt},
		{n.Negative, "text", spec.Negative},
		{n.Latent, "width", spec.Width},
		{n.Latent, "height", spec.Height},
		{n.Latent, "batch_size", spec.BatchSize},
		{n.Sampler, "seed", spec.Seed},
		{n.Sampler, "steps", spec.Steps},
		{n.Sampler, "cfg", spec.CFG},
		{n.Sampler, "sampler_name", spec.Sampler},
		{n.Sampler, "scheduler", spec.Scheduler},
	} {
		if err := g.Set(p.node, p.input, p.value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(g)
}
