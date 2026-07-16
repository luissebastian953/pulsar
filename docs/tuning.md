# Tuning

## Settings do not transfer between lineages

This is the #1 source of "why does our output look worse than Civitai's."

| | Illustrious (eps) | NoobAI (v-pred) |
|---|---|---|
| CFG | 5–7 | 3.5–5 |
| Sampler | Euler a / DPM++ 2M Karras | Euler / Euler a |
| Steps | 25–30 (ship 20–24) | 25–30 (ship 20–24) |
| Required node | — | `ModelSamplingDiscrete` (v_prediction, zsnr=true) |
| CFG rescale | 0 | 0.5–0.7 |
| Quality tags | `masterpiece, best quality, very aesthetic, absurdres` | + `very awa` |

**v-pred without `ModelSamplingDiscrete` produces garbage**, not degraded output.
Modern ComfyUI auto-detects v-pred/ZSNR checkpoints; keep the explicit node as a
fallback and don't rely on autodetection silently working.

**CFG rescale exists to kill v-pred's colour burn.** That's what NAI's "Prompt
Guidance Rescale" slider is. Surface it only when the model is v-pred.

## Resolution buckets

SDXL only behaves on-bucket. Illustrious family: 1024×1024, 896×1152, 832×1216,
1216×832, 1344×768. Off-bucket degrades coherence.

This constrains more than it looks — irregular manga panels (phase 7) don't map to
buckets, so generate at the nearest bucket and crop.

## Ancestral vs non-ancestral

Ancestral samplers (Euler a) keep injecting noise and never fully converge → more
variation. Non-ancestral (DPM++ 2M) converge → reproducible.

This matters for the seed round-trip promise: same seed + same resolved params =
same image. Pick deliberately.

## The eval harness

**Build this before the NoobAI swap.** Two afternoons of work in `eval/`:

- fixed set of ~20 prompts × fixed seeds
- sweep CFG × sampler × steps
- render contact sheets

It converts every future tuning question from argument into measurement. You'll need
it the moment you swap lineages, because none of the settings carry over.

It's also how you tune `min_post_count` (see `docs/tagdb.md`) — the highest-leverage
accuracy knob, and one you cannot tune by intuition.

## Quality passes

**FaceDetailer** — detect the face, inpaint at full res, composite back. The single
largest *perceived* quality jump for anime, because faces are small in frame and
that's where people look. Costs a second pass. Default ON.

**Hires fix** — generate at 1024, latent upscale, second KSampler @ denoise ~0.4.
Fixes detail and anatomy. This is the premium high-res tier.

## Accuracy

Most gains are in the backend, not the GPU:

- **Tag correctness** (`min_post_count`, grounding cascade) — lever #1
- **Tag order** — earlier weighs more; `assembly` sorts by `post_count`
- **Prompt weighting** `(tag:1.3)` — escape hatch for real-but-weak tags
- **CFG is the adherence knob but it's a trap** — raising it increases adherence
  *and* burns colour. Use rescale to get adherence without burn rather than cranking
  CFG.
- **Attribute bleed** (blonde's hair colour leaks onto the brunette) is the big
  multi-character failure mode. No setting fixes it; regional conditioning does.

## Performance

Order of payoff — see `docs/architecture.md#performance`.

Sanity check: SDXL 1024×1024, 25 steps on a 4090 ≈ 3–4s.

## Model licences

Verify commercial-use terms **before** building on a base model. Illustrious,
NoobAI, and WAI each carry their own licences (Illustrious License / Fair AI Public
License variants). Building a business on a model whose licence forbids commercial
use is a landmine best defused in phase 1.

ComfyUI itself is GPL-3.0 — relevant if you ever redistribute a packaged product,
not for SaaS.

## Known model weaknesses

Anime models are genuinely bad at unusual poses and foreshortening. Batch-and-pick
is the mitigation. No setting fixes it — don't burn days trying.
