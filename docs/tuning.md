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

Two spec shapes. `prompts × seeds × axes` is the tuning matrix. `cases × seeds`
is the assurance shape: each case has a `name`, `prompt`, optional `request`
fields merged into the body (`style`, `lineage`, `characters`, …; a
`reference: path` at top level or per character is uploaded first and becomes
`reference_id`), and an optional `expect` tag list. Every delivered image is
scored for **tag recall** through the sentinel's WD tagger (`include=` forces
the expected tags to report): general tags count at ≥ 0.35, character tags at
≥ 0.5; `expect` defaults to the prompt's own tags minus the quality scaffold.
`results.json` carries `recall` / `missing` / `character_hit` per image and
the run prints a per-case mean — that number is the assurance.

On MPS, set `EXEC_TIMEOUT` (default 5m, a CUDA assumption) to 20m+ before
running anything with hires: a cold checkpoint load plus a 1.5× refine pass
does not fit in five minutes on unified memory, and a timed-out job keeps
rendering on ComfyUI with nobody collecting it.

The **assurance suite** (`eval/sweeps/assurance/`, README there) is five of
these specs — free text + tags, newer characters, accuracy/quality, styles,
multi-character — with per-region bleed scoring, person counts and CCIP
identity (optional `dghs-imgutils`), plus the NovelAI V5 comparison protocol
and the pass thresholds to calibrate on pre-cutoff controls first.

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
smooth gradients and soft unbroken linework at 1248×1824.
**eps lineage only.** Measured against NoobAI v-pred 2026-08-09 and it
regressed: the karras sigma schedule clips the terminal sigma that
v-prediction + Zero-Terminal-SNR relies on, washing night blacks to a
lavender haze and blowing highlights. On v-pred lineages keep the lineage
defaults (`euler`/`normal`/cfg 5.0). Evidence:
`eval/benchmarks/out/acheron-s2.png` vs `acheron-s1.png`. The fragment
mirrors the base sampler's model/positive/negative links, so references and
regional conditioning carry into the refine pass.

## Style presets

A style is a row in `style_presets` (migration 0008), keyed by `(slug,
lineage)` because recipes don't transfer between lineages. `GET /styles`
lists them with `available` (LoRA installed or none needed);
`POST /generate {"style": "hsr"}` merges one in `assembly.Build` via
`Options.Style` — the store only reads rows, assembly stays pure:

- prompt: `QUALITY_TAGS, prompt_prefix, <user prompt>, prompt_suffix` —
  carriers lead, finish trails, the user's tags keep the subject
- negative: `FORBIDDEN_TAG_UC, DEFAULT_UC, <user negative>, negative` — the
  safety negative stays first, unconditionally
- LoRA: chained behind the request's unless the request already names it
  (two-LoRA cap applies; a third via style is a 400 naming the style)
- settings (sampler, scheduler, cfg, steps, cfg_rescale, hires_scale,
  hires_denoise): fill only what the request left unset — request > style >
  lineage default — and pass the same validation as request values
- `hires` on a row is a size-tier hint for the client; core never switches
  hires on from a style, so a user can always pick Normal

Resolved values land on the job row (`params.style` keeps the slug), so
editing a preset never rewrites history. The composer writes the recipe's
settings into its visible Advanced fields on selection and clears them on
deselect; its LoRA takes the second LoRA slot.

`hsr` is the measured miHoYo recipe (`eval/benchmarks/mihoyo-style.md`,
round-6 keeper). The tag-only looks (`game-cg`, `light-novel`, `anime-90s`,
`flat-color`, `watercolor`, `sketch`, `chibi`) ship for both lineages with
no measured settings. Adding a style is an INSERT in a new migration.
Ground new carrier tags with `GET /tags/lookup` first — dead tags dilute
attention (CLAUDE.md #4); the benchmark notes list confirmed misses (`cel
shading`, `soft lighting`, `digital painting`, `vivid colors`, …).
Known limit: a style that carries a LoRA can't combine with references or
multi-character regional prompts (same graph-composition constraint as a
user LoRA, `api.go` "LoRA cannot combine").

## Accuracy

Most gains are in the backend, not the GPU:

- **Tag correctness** (`min_post_count`, grounding cascade) — lever #1
- **Tag order** — earlier weighs more; `assembly` sorts by `post_count`
- **Prompt weighting** `1.3::tag ::` — escape hatch for real-but-weak tags
  (SD `(tag:1.3)` is still accepted and auto-converted; docs/tagdb.md#weighting)
- **CFG is the adherence knob but it's a trap** — raising it increases adherence
  *and* burns colour. Use rescale to get adherence without burn rather than cranking
  CFG.
- **Attribute bleed** (blonde's hair colour leaks onto the brunette) is the big
  multi-character failure mode. No setting fixes it; regional conditioning does.

## Prompt weights

Users type NovelAI units (`{x}` = 1.05, `1.5::x ::`, up to |10|). NovelAI's
numbers are UI units it recalibrated per encoder; SDXL's lerp-toward-empty
weighting has a narrow usable band, so `assembly` maps every user weight
through a per-lineage curve before emitting it (`prompt.Calibration`):
saturating toward `Hi` above 1 (`1 + (Hi−1)·tanh(KUp·(w−1)/(Hi−1))`), linear
down to the floor `Lo` below 1. Bracket weights transfer almost unchanged;
V4.5-style big weights saturate.

| Lineage | KUp | Hi | KDown | Lo |
|---|---|---|---|---|
| `illustrious` (eps) | 1.0 | 1.4 | 1.0 | 0.3 |
| `noobai` (v-pred) | 1.0 | 1.3 | 1.0 | 0.3 |

These are **starting values, not measured limits** — `eval/sweeps/weighting/`
specifies the sweep that refits them (adherence, overcook, stability per
emitted weight). v-pred is more CFG-sensitive, hence noobai's lower ceiling.
Bounds are clamped in code (Hi ∈ [1, 1.6], Lo ∈ [0.1, 1], K ∈ [0, 2]).
Config prompts (`QUALITY_TAGS`, `DEFAULT_UC`, style rows) are written in
emitted units with no curve, bounded (0, 2]; the safety block is always
exactly 1.0. A weight the curve moved by more than 0.05 gets a `calibrated`
notice, and `Spec.calibration` records the curve each job used.

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
