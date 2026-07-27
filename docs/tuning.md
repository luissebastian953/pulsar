# Tuning

## Settings do not transfer between lineages

This is the #1 source of "why does our output look worse than Civitai's."

| | Illustrious (eps) | NoobAI (v-pred) |
|---|---|---|
| CFG | 5–7 (ship 6) | 3.5–5 (ship 5) |
| Sampler | Euler a / DPM++ 2M Karras | Euler / Euler a |
| Steps | 25–30 (ship 24) | 28–35 (ship 28) |
| Required node | — | `ModelSamplingDiscrete` (v_prediction, zsnr=true) |
| CFG rescale | 0 | 0.4–0.7 (ship 0.55; i2i-anchored runs tolerate 0.4 for richer color — swept 2026-07-26) |
| Quality tags | `masterpiece, best quality, very aesthetic, absurdres` | + `very awa` |

These are the shipped defaults in `assembly`'s per-lineage table (`lineageSpecs`),
keyed by the request's `lineage` (env `LINEAGE` sets the default; a request may
override it per generation so both lineages A/B on one deployment). Requests
override any individual field.

**v-pred is the `patches/vpred.json` fragment, not a separate base.** It composes
onto *any* built base, inserting `ModelSamplingDiscrete` (v_prediction, zsnr) +
`RescaleCFG` and rewiring the KSampler's model through them. It applies **after**
the model-chain fragments (lora, reference) so it wraps the fully-composed model,
and **before** hires so the wrap carries into the refine pass. `assembly.Vpred(lineage)`
gates it. **Without `ModelSamplingDiscrete` a v-pred checkpoint produces garbage**,
not degraded output — pair the `noobai` lineage with a v-pred checkpoint.

**CFG rescale exists to kill v-pred's colour burn.** That's what NAI's "Prompt
Guidance Rescale" slider is. It's `RescaleCFG`'s `multiplier`, surfaced as a slider
only on v-pred lineages; `cfg_rescale: 0` disables it.

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

Built, in `eval/`: YAML sweep specs (prompts × seeds × CFG/sampler/steps axes)
run through core's real API — quality tags, safety negative, and the
compliance gate all apply — and produce a labelled contact sheet,
`results.json`, and per-image sentinel watch-scores.

```bash
make eval                      # sweeps/smoke.yaml (4 images)
make eval SWEEP=cfg-sampler    # the tuning matrix — run per lineage
make eval SWEEP=thresholds     # classifier calibration distribution
CORE_URL=http://localhost:8081 ./eval/run.sh eval/sweeps/smoke.yaml
```

It converts every future tuning question from argument into measurement:
lineage swaps (none of the settings carry over), `min_post_count`
(docs/tagdb.md — the highest-leverage accuracy knob), and
`SENTINEL_SUM_THRESHOLD` (docs/compliance.md — grow `sweeps/thresholds.yaml`
before trusting the gate for launch). Blocked and rejected cells render in
the sheet; they're data, not failures.

## Quality passes

**FaceDetailer** — detect the face, inpaint at full res, composite back. The single
largest *perceived* quality jump for anime, because faces are small in frame and
that's where people look. Costs a second pass. Default ON.

**Hires fix** — built (`hires: true` on any non-pose kind; `patches/hires.json`).
Generate → **pixel-space** lanczos upscale → re-encode → refine @ denoise 0.4.
`hires_scale` picks the factor (1.1..2.0; default 1.5 = the "Large" size tier,
2.0 = "Ultra"). Size tiers are refine multiples of the native bucket — SDXL
never samples off-bucket. Ultra (2048²-class refine) is memory-heavy on MPS;
treat it as a deploy-GPU tier.
Empirical, from three live iterations: latent-space upscaling is a trap here —
`nearest-exact` adds sharpening halos and `bislerp` produces horizontal
streaking on MPS; pixel-space lanczos is clean. The "official-art finish"
recipe: `hires: true, sampler: dpmpp_2m, scheduler: karras, cfg: 5.5` —
smooth gradients and soft unbroken linework at 1248×1824. The fragment
mirrors the base sampler's model/positive/negative links, so references and
regional conditioning carry into the refine pass.

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
