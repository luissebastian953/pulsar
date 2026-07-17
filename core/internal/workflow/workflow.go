// Package workflow: load hash-pinned API-format templates, patch by node
// ID — never rewire graphs in code (.claude/skills/comfy-workflow).
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

// Load reads a template and verifies its pinned content hash.
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

type patch struct {
	node, input string
	value       any
}

func apply(tpl Graph, patches []patch) (json.RawMessage, error) {
	g, err := tpl.Clone()
	if err != nil {
		return nil, err
	}
	for _, p := range patches {
		if err := g.Set(p.node, p.input, p.value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(g)
}

func spinePatches(spec assembly.Spec, checkpoint string) []patch {
	n := Txt2Img // the pose spine shares these IDs by construction
	return []patch{
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
	}
}

// BuildTxt2Img patches a txt2img template with a resolved spec and returns
// submit-ready API-format JSON.
func BuildTxt2Img(tpl Graph, spec assembly.Spec, checkpoint string) (json.RawMessage, error) {
	return apply(tpl, spinePatches(spec, checkpoint))
}

// BuildImg2Img: prompt/sampler patches + the uploaded input's server-side
// filename and denoise (composition survives below ~0.6).
func BuildImg2Img(tpl Graph, spec assembly.Spec, checkpoint, inputName string) (json.RawMessage, error) {
	n := Img2Img
	denoise := spec.Denoise
	if denoise == 0 {
		denoise = 0.6
	}
	return apply(tpl, []patch{
		{n.Checkpoint, "ckpt_name", checkpoint},
		{n.Positive, "text", spec.Prompt},
		{n.Negative, "text", spec.Negative},
		{n.Sampler, "seed", spec.Seed},
		{n.Sampler, "steps", spec.Steps},
		{n.Sampler, "cfg", spec.CFG},
		{n.Sampler, "sampler_name", spec.Sampler},
		{n.Sampler, "scheduler", spec.Scheduler},
		{n.Sampler, "denoise", denoise},
		{n.LoadImage, "image", inputName},
	})
}

// BuildInpaint: img2img patches + the mask (white = repaint); masked-area
// denoise defaults higher — the point is replacement.
func BuildInpaint(tpl Graph, spec assembly.Spec, checkpoint, inputName, maskName string) (json.RawMessage, error) {
	n := Inpaint
	denoise := spec.Denoise
	if denoise == 0 {
		denoise = 0.85
	}
	return apply(tpl, []patch{
		{n.Checkpoint, "ckpt_name", checkpoint},
		{n.Positive, "text", spec.Prompt},
		{n.Negative, "text", spec.Negative},
		{n.Sampler, "seed", spec.Seed},
		{n.Sampler, "steps", spec.Steps},
		{n.Sampler, "cfg", spec.CFG},
		{n.Sampler, "sampler_name", spec.Sampler},
		{n.Sampler, "scheduler", spec.Scheduler},
		{n.Sampler, "denoise", denoise},
		{n.LoadImage, "image", inputName},
		{n.LoadMask, "image", maskName},
	})
}

// BuildPose: spine patches + extractor resolution at the render's short side.
func BuildPose(tpl Graph, spec assembly.Spec, checkpoint string) (json.RawMessage, error) {
	res := min(spec.Width, spec.Height)
	return apply(tpl, append(spinePatches(spec, checkpoint),
		patch{Pose.Lineart, "resolution", res},
		patch{Pose.Openpose, "resolution", res},
	))
}
