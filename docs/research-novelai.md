# NovelAI image generation — research findings

Synthesized from a multi-source, adversarially-verified research pass (2026-07-19)
plus a full sweep of NovelAI's own image-gen docs (2026-07-20) for pulsar's model
strategy. Confidence is marked per claim:

- **[verified]** — passed 3-vote adversarial verification, primary source.
- **[cited]** — from a primary source (official model card / arxiv) but the
  verification voters were cut off by a rate limit; treat as a strong lead, not
  independently cross-checked.
- **[reasoned]** — our own inference / empirical finding, not from the research.

## 1. Why NovelAI's output looks the way it does

The signature "clean official-splash" finish is **architectural**, not a
prompt/sampler trick:

- **[verified]** NAI Diffusion **V3** = SDXL **uptrained from ε-prediction to
  v-prediction**. Not from scratch — an SDXL derivative, like Illustrious.
  (arxiv 2409.15997)
- **[verified]** V3 was trained on a **Zero-Terminal-SNR (ZTSNR)** noise
  schedule so the model sees **pure noise** during training. This fixes SDXL's
  signal-leakage flaw (training "stops short of pure noise"), which is *the*
  contributor to correct global values — true blacks/whites, clean composition,
  the composed splash look a raw booru model lacks. **This is the single most
  important finding.** (arxiv 2409.15997)
- **[verified]** Recommended **CFG 3.5–5** (vs SDXL's 7.5), attributed to
  cleaner labeling. (arxiv 2409.15997)
- **[verified]** Uses **penultimate CLIP layer** hidden states (not the final
  layer) → better attribute disentanglement (correct colours). (NAI blog)
- **[verified]** Extended prompt length to **225 tokens** (3× SDXL's 77) for
  dense tag stacks. (NAI blog)
- **[verified]** Trained on **~6M crowd-sourced (booru) images**, staged so
  later stages use higher-quality data. (arxiv 2409.15997)
- **[verified]** **V4 / V4.5 are trained fully from scratch**, no public base
  model, described in the docs as "completely original… trained from the ground
  up without relying on a public base model." **This is the part we structurally
  cannot match** by adapting a community checkpoint. (docs.novelai.net)
- **[verified]** **V4 / V4.5 switched the text encoder from CLIP to T5, ~512
  tokens** (V3 and our Illustrious base use CLIP). This is why V4 does natural-
  language understanding and English text rendering — and it means V4's prompt
  behaviour is fundamentally unreachable on an SDXL/CLIP base. The **Curated**
  V4/V4.5 variants are smaller-cleaner-dataset models for "safe streaming."
  (docs.novelai.net/en/image/models)

**Takeaway:** NAI's edge on V3 is v-prediction + ZTSNR on a curated dataset.
On V4/V4.5 it's a bespoke model. The finish is baked into the weights.

## 2. Reference & Director tools

Filled in from NovelAI's own docs (`/image/vibetransfer`, `/image/precisereference`,
`/image/directortools`, `/image/uploadimage`).

**Key correction:** NAI has *two distinct* reference systems, and we conflated
them. **pulsar's 3-mode + Strength + Fidelity UI is a clone of Precise Reference,
not Vibe Transfer.**

### Vibe Transfer — the older embedding-injection path (all V4+)

- **[cited]** **Reference Strength**: "the closer it is to 1, the stronger the AI
  will try to emulate visual cues, such as stylistic aspects, colors etc." — and
  **excessive strength makes it ignore the text prompt.** Same family as our
  IP-Adapter `weight`, and it confirms our empirical finding: strength 1.0 blew
  up because it drowned the prompt. NAI has the same failure mode, just
  better-behaved on a trained-in encoder.
- **[cited]** **Information Extracted**: controls *which* concepts transfer.
  Lowering it first loses **high-frequency info (textures)** → lower = more
  composition, less style. A **second axis we don't expose**; our closest analogs
  are IP-Adapter's `weight_type` and `embeds_scaling`.
- **[cited]** Up to **16 reference images** at once. Encoded vibes are
  **precomputed, cacheable, and embeddable in image metadata** as encoded
  representations (not original pixels) — image-embedding injection, same family
  as IP-Adapter, trained-in on V4+.

### Precise Reference — *this* is what pulsar's reference UI clones (V4.5-only)

- **[cited]** Three modes: **Character Reference** (reproduces appearance, "often
  without requiring detailed tags"), **Style Reference** (overall artistic
  style), **Character & Style**. This is *exactly* pulsar's `ref_mode`
  (`character` / `style` / `character_style`). The user's Facebook screenshot was
  Precise Reference, and we built the right thing.
- **[cited]** **Strength**: toward 1 the AI "increasingly adheres to stylistic
  features, color palettes, and other visual characteristics" — high values
  "risk over-reliance on poses and expressions." → our `ref_strength`.
- **[cited]** **Fidelity**: "At one, the reference is enforced aggressively and
  difficult to override. At zero, the model behaves more flexibly." → our
  `ref_fidelity` (mapped to IP-Adapter `end_at` via `0.4 + 0.6*fidelity`). The
  semantics match: higher fidelity = harder to override.
- **[cited]** Multiple **character** references **blend together** rather than
  making separate characters — matches our single-adapter behaviour and is why
  per-character identity needs the regional/mask path, not stacked refs.
- **[cited]** **V4.5-only**, costs +5 Anlas, **incompatible with Vibe Transfer**,
  works with Inpainting. Best results: full-body standing shot on a plain
  background at large res (1024×1536 / 1472×1472 / 1536×1024) — the same guidance
  we baked into the `char035` benchmark variant.

**Conclusion:** our bolt-on IP-Adapter is the right *mechanism* and our UI is a
faithful Precise-Reference clone. NAI's edge is a trained-in encoder (V4.5) plus
the "information extracted" dial we don't expose. Reference carries identity, not
finish — confirmed in `eval/benchmarks/herta-style`.

### Director Tools — a post-processing suite pulsar can largely already build

All six are img2img/inpaint/preprocessor operations. **pulsar's ingest path,
img2img, inpaint, and pose-lineart extraction are exactly the substrate for
these** — one is already shipped:

| NAI Director tool | Does | pulsar status |
|---|---|---|
| **Line Art** | image → line art | **Already have it** — pose workflow's `AnimeLineArtPreprocessor` |
| **Sketch** | image → rough sketch stage | trivial — same preprocessor family |
| **Declutter** | remove floating text/objects/noise | inpaint (have the path) |
| **Emotion** (`Emotion Level`, `Emotion Prompt`) | change expression | masked inpaint on the face |
| **Colorize** (`Colorize Defry`, `Colorize Prompt`) | colour line art / recolour | img2img (have the path) |
| **Remove Background** (masked / generated / blend) | strip / regen background | new node (bg-removal), then inpaint |

This is a concrete, mostly-cheap feature roadmap that reuses machinery pulsar
already has, and it's a documented NAI-parity checklist.

## 2.5 Prompt & sampler mechanics (primary source)

Direct from the docs — these are concrete parity references for `assembly`,
sampler config, and the regional/multi-character subsystem. All **[cited]** from
docs.novelai.net unless noted.

### Quality tags NAI appends per model (`Add Quality Tags`, on by default)

Appended to the **end** of the prompt (V3+), invisible in the box but consuming
token budget:

| Model | Appended string |
|---|---|
| Anime V3 | `best quality, amazing quality, very aesthetic, absurdres` |
| V4 Full | `no text, best quality, very aesthetic, absurdres` |
| V4 Curated | `rating:general, amazing quality, very aesthetic, absurdres` |
| V4.5 Full | `location, very aesthetic, masterpiece, no text` |
| V4.5 Curated | `location, masterpiece, no text, -0.8::feet::, rating:general` |

pulsar's benchmark prompt already front-loads `masterpiece, best quality, very
aesthetic, absurdres` — same vocabulary, different position (we prepend, NAI V3+
appends). For an eps/Illustrious base, prepend-and-early is correct (position =
weight, see below); NAI's append works because their scheduler weights differently.

### Emphasis syntax — NAI ≠ Stable Diffusion

- **[cited]** Strengthen `{term}` = ×1.05 per level (`{{term}}`=1.1025). Weaken
  `[term]` = ÷1.05 per level. **Inverse of SD's `(term)` = ×1.1.**
- **[cited]** Numeric: `1.5::term::` — but **V4+ only**; **negative weights
  (`-1::hat::`) V4.5+ only.** So NAI-syntax input can't be passed through to our
  SDXL/CLIP base verbatim; pulsar's ComfyUI path uses SD `(term:1.2)` weighting.
  If we ever accept NAI-style prompts we must transpile `{}`/`[]`/`::` → SD.

### Tag ordering (V3 and lower — our regime)

- **[cited]** Recommended order: `1boy, 1girl, characters, series, everything
  else`. **"The earlier a tag appears, the stronger its effect"** on V3-and-lower.
  Confirms pulsar's assembly ordering. Note: V4 (T5 encoder) is far less
  positional — another reason V4 behaviour won't transfer to our base.

### CFG / rescale / decrisper

- **[cited]** Recommended **Prompt Guidance 5–6** for V3+. Lower = "more
  painterly, dreamy, softer"; higher = "more fine detail and sharpness." → the
  user wanted *softer*, so our cfg 6.2 is slightly high; **5.0–5.5 is the softer
  band.**
- **[cited]** **Prompt Guidance Rescale** exists specifically to fix colors that
  go "too strong around the edges / deepfried" at high guidance — this is
  CFG-rescale, exactly the `~0.2` the NoobAI v-pred plan needs.
- **[cited]** **Decrisper** = a toggle that mitigates color/artifact issues at
  high guidance. Same purpose, different knob.

### Samplers

- **[cited]** Eight samplers; **DPM++ 2M and Euler Ancestral are NAI's
  recommended pair.** Our benchmark ran `dpmpp_2m` — aligned. **Caveat:** NoobAI
  v-pred wants **Euler (non-ancestral)**; the recommendation is model-specific.
- **[cited]** **SMEA / SMEA DYN** = NAI-proprietary multi-pass-UNet variants for
  high-res coherency (auto-applied ≥1024²). Not in vanilla ComfyUI (community
  nodes exist). A real NAI-only lever we can't cleanly match.

### Multi-character (V4) — richer than our 2–3 regional columns

- **[cited]** Up to **6 characters**. Position via a **5×5 grid** ("a nudge, not
  a guarantee"); default order top-to-bottom, left-to-right by prompt sequence.
- **[cited]** Anti-bleed structure: **base prompt holds the count** (`2girls,
  1boy`), **character prompts drop numbers** (`girl, purple hair…`). pulsar's
  regional path should follow this split.
- **[cited]** Interaction prefixes **`source#` / `target#` / `mutual#`** (e.g.
  `source#hug` vs `target#hug`) bind an action across two characters — a concrete
  syntax to adopt if we extend regional prompting to interactions.

### Text rendering (V4 T5 only)

- **[cited]** `text, english text` tags + `Text: …` at the **very end** of the
  prompt; ≤120 chars; all-caps helps V4. **Requires the T5 encoder — structurally
  impossible on our CLIP/SDXL base.** Listed only to mark it out of scope.

### img2img Strength & Noise → our denoise

- **[cited]** **Strength** = how much the AI may reinterpret the source (our
  `denoise`). **Noise** = injects *new* detail for elements the source lacks;
  repeated high-noise passes "degrade over iterations." Min both = near-perfect
  copy. We expose one `denoise`; NAI's separate Noise axis is a possible add.

## 3. Replication playbook for open weights

The research pins down the open-weights lineage precisely, and it points at one
answer:

| Model | Base | Objective | Data cutoff | Notes |
|---|---|---|---|---|
| **Illustrious XL v0.1** (what pulsar runs) | SDXL 1.0 | **eps** | Danbooru ~2023 | **[cited]** The plain, untuned root. eps, old data. |
| **NoobAI-XL v-pred 1.0** | Illustrious → SDXL | **v-pred + ZTSNR** | Danbooru ~2024-10-23 + e621 | **[cited]** *Same recipe as NAIv3.* The closest open-weights analog to NovelAI's actual architecture. |

**The decisive finding:** the exact technique that gives NovelAI V3 its look —
**v-prediction + Zero-Terminal-SNR** — is available open-weights as **NoobAI-XL
v-pred**, which descends from the same Illustrious lineage we already use.
Running eps Illustrious v0.1 with an IP-Adapter was the wrong base; that's *why*
today plateaued on "sharp / unsmooth."

### NoobAI-XL v-pred settings (from the model card + Civitai) [cited]

- **CFG 4–5**, **Euler sampler only** (Karras / other schedulers reported to
  fail — matches CLAUDE.md's eps↔v-pred non-transfer invariant),
  **28–35 steps**.
- **CFG Rescale ~0.2**, and the runtime needs `prediction_type=v_prediction` +
  `rescale_betas_zero_snr=True` (this is the `ModelSamplingDiscrete` node the
  comfy-workflow skill already specifies for v-pred).
- Optimal resolution **832×1216** (same bucket pulsar defaults to).
- Quality-tag tiers: `masterpiece` (>95th percentile), `best quality` (>85th),
  down to `worst quality`; plus **`very awa`** for the top ~5% aesthetic
  (waifu-scorer v4-beta). Recommended positive prefix:
  `masterpiece, best quality, newest, absurdres, highres`.

### Illustrious (current) settings for reference [cited]

- eps objective, **Euler A**, **CFG 5–7.5**, >20 steps. Quality via
  percentile rating tags + year modifiers (`newest` ≈ 2023).

### The recommendation

1. **Swap the lineage to NoobAI-XL v-pred.** This is roadmap option #3 and the
   research confirms it's the *architecturally correct* move — it replicates the
   v-pred + ZTSNR recipe behind NAI's finish, and it knows 2024 characters. It's
   real work in pulsar (v-pred template with `ModelSamplingDiscrete`, per-lineage
   CFG/sampler/rescale — exactly what `docs/tuning.md` and the `noobai` lineage
   scaffold anticipate), but it's the highest-ceiling lever.
2. **Then a style LoRA** for the last mile to a *specific* studio look.
3. **Reference/IP-Adapter stays** for identity of characters newer than the base
   (e.g. **The Herta debuted Jan 2025 — after even NoobAI's Oct-2024 cutoff**, so
   she still needs a reference or LoRA regardless of base).

### The honest gap that remains

Even on NoobAI v-pred + LoRA, we're adapting community weights; NAI V4/V4.5 is a
from-scratch model with jointly-trained reference tooling. Expect *close*, not
identical. But the "why doesn't ours look official" gap is mostly the
**eps-v0.1-plus-bolt-on-adapter** choice, and NoobAI v-pred closes most of it.

## Sources

Primary: NAI Diffusion V3 report (arxiv 2409.15997), Illustrious report
(arxiv 2409.19946), NoobAI-XL model cards (HuggingFace Laxhar/noobai-XL-Vpred-1.0,
noobai-XL-1.0), NAI improvements blog. Community: Civitai NoobAI (833294).

NovelAI docs sweep (docs.novelai.net/en/image, 2026-07-20) — pages read:
`models`, `basics`, `tags`, `strengthening-weakening`, `qualitytags`,
`undesiredcontent`, `stepsguidance`, `sampling`, `seed`, `multiplecharacters`,
`textrendering`, `promptmixing`, `promptchunks`, `controltools` (img2img),
`strengthnoise`, `uploadimage`, `vibetransfer`, `precisereference`,
`directortools`. Skipped as UI-only: `history`, `editimagecanvas`, `enhance`,
`upscale`, `promptrandomizer`, `inpaint`.
