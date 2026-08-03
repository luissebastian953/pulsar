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

// loadPinned reads a file and verifies its pinned content hash; kind is
// "template" or "fragment" for error text. Both loaders share it so the
// remediation message never diverges again.
func loadPinned(dir, name, wantSHA256, kind string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	got := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	if got != wantSHA256 {
		return nil, fmt.Errorf("%s %s hash %s does not match pinned %s — if the edit is intentional, bump the constant in internal/workflow/nodes.go and re-check node IDs", kind, name, got, wantSHA256)
	}
	return raw, nil
}

// Load reads a template and verifies its pinned content hash.
func Load(dir, name, wantSHA256 string) (Graph, error) {
	raw, err := loadPinned(dir, name, wantSHA256, "template")
	if err != nil {
		return nil, err
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

// Fragment is a hash-pinned feature patch: nodes to add plus input rewires
// on the base — data, like templates, never graphs built in code.
type Fragment struct {
	Add Graph                     `json:"add"`
	Set map[string]map[string]any `json:"set"`
}

// LoadFragment reads a patch fragment and verifies its content hash.
func LoadFragment(dir, name, wantSHA256 string) (*Fragment, error) {
	raw, err := loadPinned(dir, name, wantSHA256, "fragment")
	if err != nil {
		return nil, err
	}
	var f Fragment
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("fragment %s: %w", name, err)
	}
	return &f, nil
}

// merge composes a fragment onto a graph: nodes added collision-safe, then
// the fragment's input rewires applied.
func merge(graph json.RawMessage, frag *Fragment) (Graph, error) {
	var g Graph
	if err := json.Unmarshal(graph, &g); err != nil {
		return nil, err
	}
	for id, node := range frag.Add {
		if _, exists := g[id]; exists {
			return nil, fmt.Errorf("fragment node %s collides with base", id)
		}
		clone := *node
		g[id] = &clone
	}
	for id, inputs := range frag.Set {
		for input, value := range inputs {
			if err := g.Set(id, input, value); err != nil {
				return nil, err
			}
		}
	}
	return g, nil
}

// ApplyIPAdapter composes the reference fragment onto an already-built
// graph, patching the reference filename and weight.
func ApplyIPAdapter(graph json.RawMessage, frag *Fragment, refName string, weight float64, weightType string, endAt float64) (json.RawMessage, error) {
	g, err := merge(graph, frag)
	if err != nil {
		return nil, err
	}
	if err := g.Set(IPAdapter.LoadRef, "image", refName); err != nil {
		return nil, err
	}
	if err := g.Set(IPAdapter.Apply, "weight", weight); err != nil {
		return nil, err
	}
	if weightType != "" {
		if err := g.Set(IPAdapter.Apply, "weight_type", weightType); err != nil {
			return nil, err
		}
	}
	if err := g.Set(IPAdapter.Apply, "end_at", endAt); err != nil {
		return nil, err
	}
	return json.Marshal(g)
}

// ApplyIPAdapterCS: the Character & Style chain — identity and
// style-transfer adapters on the same reference, both at weight/endAt.
func ApplyIPAdapterCS(graph json.RawMessage, frag *Fragment, refName string, weight, endAt float64) (json.RawMessage, error) {
	g, err := merge(graph, frag)
	if err != nil {
		return nil, err
	}
	if err := g.Set(IPAdapterCS.LoadRef, "image", refName); err != nil {
		return nil, err
	}
	for _, node := range IPAdapterCS.Apply {
		if err := g.Set(node, "weight", weight); err != nil {
			return nil, err
		}
		if err := g.Set(node, "end_at", endAt); err != nil {
			return nil, err
		}
	}
	return json.Marshal(g)
}

// ApplyRegional composes a regional fragment, patching each character
// slot's mask filename and prompt. len(maskNames) == len(prompts) must
// match the fragment's slot count (regional2 ↔ 2, regional3 ↔ 3).
func ApplyRegional(graph json.RawMessage, frag *Fragment, maskNames, prompts []string) (json.RawMessage, error) {
	if len(maskNames) != len(prompts) || len(prompts) > len(RegionalSlots) {
		return nil, fmt.Errorf("regional: %d masks / %d prompts", len(maskNames), len(prompts))
	}
	g, err := merge(graph, frag)
	if err != nil {
		return nil, err
	}
	for i := range prompts {
		slot := RegionalSlots[i]
		if err := g.Set(slot.LoadMask, "image", maskNames[i]); err != nil {
			return nil, err
		}
		if err := g.Set(slot.Text, "text", prompts[i]); err != nil {
			return nil, err
		}
	}
	return json.Marshal(g)
}

// ApplyIPAdapter2 composes the per-character reference fragment (two
// masked adapters on the regional masks), patching each slot's reference
// filename and weight. Requires the regional2 fragment already applied.
func ApplyIPAdapter2(graph json.RawMessage, frag *Fragment, refNames [2]string, weights [2]float64) (json.RawMessage, error) {
	g, err := merge(graph, frag)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 2; i++ {
		if err := g.Set(IPAdapter2.LoadRef[i], "image", refNames[i]); err != nil {
			return nil, err
		}
		if err := g.Set(IPAdapter2.Apply[i], "weight", weights[i]); err != nil {
			return nil, err
		}
	}
	return json.Marshal(g)
}

// ApplyLora composes a LoRA onto the spine: one LoraLoader whose model/clip
// outputs replace the checkpoint's for the sampler and text encoders. Must
// compose BEFORE hires (hires mirrors the base sampler's model link, which
// this rewrites). One LoRA for now; chaining is a later fragment.
func ApplyLora(graph json.RawMessage, frag *Fragment, name string, strength float64) (json.RawMessage, error) {
	g, err := merge(graph, frag)
	if err != nil {
		return nil, err
	}
	if err := g.Set(Lora.Loader, "lora_name", name); err != nil {
		return nil, err
	}
	for _, in := range []string{"strength_model", "strength_clip"} {
		if err := g.Set(Lora.Loader, in, strength); err != nil {
			return nil, err
		}
	}
	return json.Marshal(g)
}

// ApplyLora2 chains a second LoRA behind the first (node 71 reads 70's
// outputs; the fragment rewires the sampler and text encoders to 71).
// Compose only after ApplyLora.
func ApplyLora2(graph json.RawMessage, frag *Fragment, name string, strength float64) (json.RawMessage, error) {
	g, err := merge(graph, frag)
	if err != nil {
		return nil, err
	}
	if err := g.Set(Lora2.Loader, "lora_name", name); err != nil {
		return nil, err
	}
	for _, in := range []string{"strength_model", "strength_clip"} {
		if err := g.Set(Lora2.Loader, in, strength); err != nil {
			return nil, err
		}
	}
	return json.Marshal(g)
}

// ApplyVpred wraps the final model in ModelSamplingDiscrete (v_prediction,
// zsnr) + RescaleCFG — required for v-pred lineages (without it, output is
// garbage, not degraded). It reads whatever the base sampler's model link
// currently points to (checkpoint, or a lora/reference node if those
// composed first) and inserts the wrap in front, so it must run AFTER the
// model-chain fragments and BEFORE hires (which then mirrors the wrapped
// model into the refine pass). multiplier 0 = no rescale (docs/tuning.md).
func ApplyVpred(graph json.RawMessage, frag *Fragment, multiplier float64) (json.RawMessage, error) {
	var pre Graph
	if err := json.Unmarshal(graph, &pre); err != nil {
		return nil, err
	}
	base, ok := pre[Txt2Img.Sampler]
	if !ok {
		return nil, fmt.Errorf("vpred: base sampler missing")
	}
	model := base.Inputs["model"] // the composed model tap, pre-wrap

	g, err := merge(graph, frag) // adds 80/81, sets sampler.model = [81,0]
	if err != nil {
		return nil, err
	}
	if err := g.Set(Vpred.Sampling, "model", model); err != nil {
		return nil, err
	}
	if err := g.Set(Vpred.Rescale, "multiplier", multiplier); err != nil {
		return nil, err
	}
	return json.Marshal(g)
}

// ApplyHires composes the refine pass LAST: it mirrors the base sampler's
// model/positive/negative links so whatever fragments rewired them
// (reference adapters, regional conditioning) carry into the second pass.
func ApplyHires(graph json.RawMessage, frag *Fragment, spec assembly.Spec) (json.RawMessage, error) {
	g, err := merge(graph, frag)
	if err != nil {
		return nil, err
	}
	base, ok := g[Txt2Img.Sampler]
	if !ok {
		return nil, fmt.Errorf("hires: base sampler missing")
	}
	scale := spec.HiresScale
	if scale == 0 {
		scale = 1.5 // fragment default; specs from older jobs may omit it
	}
	if err := g.Set(Hires.Upscale, "scale_by", scale); err != nil {
		return nil, err
	}
	denoise := spec.HiresDenoise
	if denoise == 0 {
		denoise = 0.4 // fragment default; specs from older jobs may omit it
	}
	steps := max(12, spec.Steps*3/5)
	for input, value := range map[string]any{
		"model":        base.Inputs["model"],
		"positive":     base.Inputs["positive"],
		"negative":     base.Inputs["negative"],
		"seed":         spec.Seed,
		"steps":        steps,
		"cfg":          spec.CFG,
		"sampler_name": spec.Sampler,
		"scheduler":    spec.Scheduler,
		"denoise":      denoise,
	} {
		if err := g.Set(Hires.Sampler, input, value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(g)
}

// BuildPose: spine patches + extractor resolution at the render's short side.
func BuildPose(tpl Graph, spec assembly.Spec, checkpoint string) (json.RawMessage, error) {
	res := min(spec.Width, spec.Height)
	return apply(tpl, append(spinePatches(spec, checkpoint),
		patch{Pose.Lineart, "resolution", res},
		patch{Pose.Openpose, "resolution", res},
	))
}
