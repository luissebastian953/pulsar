---
name: comfy-workflow
description: How to author, edit, compose, and patch ComfyUI workflow templates in pulsar. Use this skill whenever the work touches anything in workflows/, internal/workflow/, node IDs, API-format JSON, ComfyUI graphs, samplers, KSampler/VAE/CLIP nodes, IP-Adapter or ControlNet nodes, or adding a new generation mode (img2img, inpaint, hires, pose, regional). Also use it when a generation produces wrong or garbage output and the cause might be the graph rather than the prompt — that is a common misdiagnosis here.
---

# ComfyUI workflows in pulsar

Workflows are **data**, not code. They live in `workflows/` as API-format JSON,
versioned by content hash, and are patched by node ID at runtime. The Go builder in
`internal/workflow/` selects a base template and applies feature patches.

## The two formats — get this wrong and nothing works

ComfyUI's UI exports two different JSON formats:

- **Editor format** — what "Save" produces. Contains canvas positions, links, UI
  state. `POST /prompt` rejects it.
- **API format** — what "Save (API format)" produces with Dev mode enabled. A flat
  map of node ID → `{class_type, inputs}`. This is the only format we store.

If a workflow 400s on submit, check the format first. It's the most common cause.

## Patching, not rewiring

Never construct or rewire a graph in Go. Load a template, patch values by node ID:

```go
// Right
wf := workflow.Load("txt2img.json")
wf.Set(nodes.KSampler, "seed", spec.Seed)
wf.Set(nodes.PositiveCLIP, "text", spec.PositivePrompt)

// Wrong — building graphs in code
wf.AddNode("KSampler", ...)
```

Rewiring in code means the graph exists in two places (the JSON and the Go) and they
drift. Patching keeps the JSON the single source of truth, which is what makes a
workflow authored in the ComfyUI UI deployable without translation.

## Node IDs are fragile

Node IDs live in `internal/workflow/nodes.go` as constants. **They change when you
re-export a workflow from the UI.** Editing a workflow in ComfyUI and re-saving will
silently renumber nodes, and the patcher will write to the wrong node or fail.

When you edit a template:
1. Re-export as API format
2. Diff the node IDs against `nodes.go`
3. Update the constants
4. Bump the template's content hash
5. Run the workflow tests

## Templates compose — don't multiply them

A request may be `txt2img_vpred + ipadapter + regional + hires`. Select a base, apply
feature patches. Do **not** create
`txt2img_vpred_ipadapter_regional_hires.json` — the cartesian product is
unmaintainable and it's how this repo would rot.

Bases:
- `txt2img.json` — Illustrious (eps)
- `txt2img_vpred.json` — NoobAI (v-pred; includes `ModelSamplingDiscrete`)
- `img2img.json` — `LoadImage` → `VAEEncode`, `denoise` < 1.0
- `inpaint.json` — + `SetLatentNoiseMask`
- `pose.json` — generate → `lineart_anime` + `openpose` extract

Feature patches: `ipadapter`, `regional`, `hires`.

## The graph diffs worth knowing

**txt2img → img2img** is two nodes:

```
txt2img:  EmptyLatentImage ──────────► KSampler (denoise=1.0)
img2img:  LoadImage → VAEEncode ─────► KSampler (denoise<1.0)
```

Same checkpoint, same CLIP encodes, same sampler. `denoise` is the whole game:
1.0 ignores the input, ~0.5–0.6 restructures while keeping composition, ~0.2–0.3
refines only.

**Hires fix and inpainting are img2img.** Hires = generate → latent upscale → second
KSampler @ denoise ~0.4. Inpaint = img2img + mask. If you understand the img2img
diff, you understand three features.

**v-pred requires `ModelSamplingDiscrete`** (v_prediction, zsnr=true). Without it,
output is garbage — not degraded, garbage. Modern ComfyUI auto-detects v-pred
checkpoints; keep the node explicit anyway rather than trusting silent
autodetection.

## The pose workflow — why it doesn't prompt for lineart

The intuitive approach is prompting `lineart, sketch, monochrome, white background`.
It half-works and it's wrong.

On Danbooru, `sketch`/`lineart` are a small, low-quality subset — mostly unfinished
WIPs. Prompting into that region pushes the model off its strongest distribution and
**anatomy gets worse**, which is the opposite of what a pose tool needs. Anatomy
knowledge lives in the dense region: normal, finished, full-colour illustrations.

So: generate on-distribution, then extract deterministically.

```
KSampler → VAEDecode ─┬─► AnimeLineArtPreprocessor → ImageInvert ─► lineart
 (normal colour)      ├─► OpenposePreprocessor                   ─► skeleton
                      └─► source
```

One extra pass, clean output every time, instead of fighting the sampler for
it. Two lessons from the first real runs (`workflows/pose.json`):

- **Preprocessors emit conditioning, not deliverables.** lineart_anime outputs
  white-on-black (ControlNet input format); artists need black-on-white. The
  `ImageInvert` between extract and save is load-bearing — don't "simplify" it
  away.
- **OpenPose finds nothing on multi-figure or chibi outputs** — it returns an
  empty black canvas, not an error. The fix is prompt-side: `solo` in the
  positive, `multiple views, reference sheet, chibi` in the negative. If
  detection stays flaky on stylized bodies, `DWPreprocessor` is the upgrade
  path (better generalization; needs onnxruntime, already pinned).

## Custom nodes are pinned

`comfy/custom-nodes.txt` pins nodes by commit (`<repo-url> <sha>` per line) and
is consumed by BOTH the Dockerfile and `run-native.sh` — one source, no drift.
A workflow referencing an unpinned node will work locally and fail in the
container. If you add a node dependency, pin it there in the same change.
Two runtime realities: the native runner swaps `onnxruntime-gpu` for plain
`onnxruntime` (the GPU build has no macOS wheels), and controlnet_aux
preprocessors download their annotator weights (~200 MB each) on first use —
the container needs network on the first pose run, or pre-warm after build.

## Before you submit

- API format, not editor format
- Node IDs match `nodes.go`
- Content hash bumped
- Any new custom node pinned in `comfy/`
- Composes as a patch rather than a new cartesian file
- Tested against both lineages if it touches sampling

## Related

- `docs/architecture.md#workflow-templates` — the template table and compose model
- `docs/tuning.md` — per-lineage settings; v-pred requirements
- `docs/precise-reference.md` — the IP-Adapter graph specifically
