# Research draft — HSR-official finish: conclusions, references, experiment ledger

Working companion to `docs/research-novelai.md` (the synthesized report). This
one is the trail: the links behind the conclusions, and an honest ledger of
what has actually been tried in pulsar vs what's still on the table. Update
the ledger as experiments land; graduate anything stable into
`docs/tuning.md` / `docs/research-novelai.md`.

## The conclusion, one paragraph

The "clean official-splash" finish (smooth gradients, soft unbroken linework,
correct global values) that NovelAI V3 produces is **architectural**:
SDXL uptrained to **v-prediction** on a **Zero-Terminal-SNR** noise schedule.
pulsar generates on raw eps Illustrious v0.1, which lacks both — that is the
ceiling every reference/CFG/steps experiment kept hitting ("still not
smooth"). The open-weights model with NAI's recipe is **NoobAI-XL v-pred**,
same Illustrious lineage we already run. The swap is fully wired in pulsar
(v-pred wrap, per-lineage defaults, UI picker) and blocked only on the
checkpoint file. Reference (IP-Adapter) stays regardless of base — it carries
*identity* for post-cutoff characters like The Herta, never *finish*.

## References

### Papers (the architecture argument)

| Ref | What it establishes | Link |
|---|---|---|
| **NovelAI Diffusion V3 report** | V3 = SDXL uptrained ε→v-pred + ZTSNR; CFG 3.5–5; penultimate CLIP; ~6M booru images. The primary source for "the finish is in the weights". | https://arxiv.org/abs/2409.15997 |
| **Illustrious report** (OnomaAI) | What our current base is: SDXL, eps objective, Danbooru-trained. | https://arxiv.org/abs/2409.19946 |
| **Zero-Terminal-SNR** ("Common Diffusion Noise Schedules and Sample Steps are Flawed") | The signal-leak flaw in SDXL's schedule and the ZTSNR fix — why global values/composition come out right. | https://arxiv.org/abs/2305.08891 |
| **v-prediction** (Progressive Distillation, Salimans & Ho) | Origin of the v-pred objective. | https://arxiv.org/abs/2202.00512 |
| **IP-Adapter** | Decoupled cross-attention image prompting — what our reference system bolts on. | https://arxiv.org/abs/2308.06721 |

### Model weights

| Model | Role | Link |
|---|---|---|
| **NoobAI-XL v-pred 1.0** (Laxhar) | The swap target: Illustrious lineage + v-pred + ZTSNR, Danbooru+e621 through ~2024-10. 7.1 GB, public. | https://huggingface.co/Laxhar/noobai-XL-Vpred-1.0 |
| NoobAI-XL 1.0 (eps sibling) | Same data, eps objective — useful A/B against v-pred. | https://huggingface.co/Laxhar/noobai-XL-1.0 |
| Illustrious-XL v0.1 (what we run) | Current base; plainest model in the ecosystem. | https://huggingface.co/OnomaAIResearch/Illustrious-xl-early-release-v0 |
| NoobAI on Civitai | Community settings consensus (CFG 4–5, Euler, rescale) + finetune ecosystem. Auth-walled downloads. | https://civitai.com/models/833294 |

### NovelAI product docs (the parity sweep, 2026-07-20)

Root: https://docs.novelai.net/en/image — pages that mattered most:

| Page | Why it mattered |
|---|---|
| https://docs.novelai.net/en/image/models | V4/V4.5 from scratch, T5 encoder ~512 tokens — the part we structurally can't match. |
| https://docs.novelai.net/en/image/precisereference | **What pulsar's reference UI clones** (3 modes + Strength/Fidelity, V4.5-only). Confirms our param semantics. |
| https://docs.novelai.net/en/image/vibetransfer | The *other* reference system: Reference Strength + Information Extracted (an axis we don't expose), up to 16 vibes. |
| https://docs.novelai.net/en/image/stepsguidance | CFG 5–6 recommended; **lower = softer/painterly** — directly actionable for "not smooth". Rescale/Decrisper exist to fix colour burn. |
| https://docs.novelai.net/en/image/qualitytags | Exact per-model quality-tag strings (V3→V4.5). |
| https://docs.novelai.net/en/image/undesiredcontent | Per-model UC preset strings. |
| https://docs.novelai.net/en/image/sampling | DPM++ 2M + Euler A recommended; SMEA/SMEA DYN are proprietary. |
| https://docs.novelai.net/en/image/multiplecharacters | 6 chars, 5×5 grid, `source#`/`target#`/`mutual#` prefixes. |
| https://docs.novelai.net/en/image/directortools | The 6 post-op tools — pulsar parity roadmap. |

### Tooling

| Tool | Link |
|---|---|
| ComfyUI IPAdapter plus (cubiq) — the adapter nodes we compose | https://github.com/cubiq/ComfyUI_IPAdapter_plus |

## Experiment ledger

Status: ✅ tried (with verdict) · 🔧 built, untested (blocked on a file) · ⬜ not tried.

### ✅ Tried — reference/prompt tuning on eps Illustrious (the exhausted ground)

Full detail in `eval/benchmarks/herta-style.{json,md}`; all seed 88.

| Experiment | Verdict |
|---|---|
| ✅ Character&Style dual adapter @ strength 1.0 | **Total blowout** — abstract colour noise, confirmed 3× across references. |
| ✅ Character&Style @ 0.4 | Coherent, on-model; background shimmer remains. |
| ✅ Character-only @ 0.35 + artist tags + hires | **Reference-only ceiling.** Identity close to canonical; finish still below HSR-official. |
| ✅ Artist tags (`ciloranko, ask_(askzy), as109`) | Real style pull; carries more of the look than the reference does. |
| ✅ Hires pass (pixel-space lanczos 1.5× + refine @ 0.4) | Smooths linework/shading measurably; latent-space upscaling streaks on MPS (documented in `docs/tuning.md`). |
| ✅ Character-first prompt split (identity block leads, scaffold follows) | Adopted in the benchmark; correct booru ordering for a weak-identity subject. |
| ✅ CFG ~6.2 band | Slightly sharp for the goal; NAI docs say lower (5–5.5) = softer — partially explored, worth pairing with the base swap rather than alone. |
| ✅ Dual-adapter on 17 GB MPS | **Operationally unviable**: ~80 s/it (10× single-adapter), memory spiral, one OS crash. Single adapter @ ~8 s/it is the stable envelope. |

**Ledger conclusion:** every knob on the eps base is explored; the gap that
remains is the base model. Do not keep bisecting reference strength.

### 🔧 Built, untested — waiting on files

| Lever | State | Blocker |
|---|---|---|
| ✅ **NoobAI-XL v-pred lineage** — **VALIDATED 2026-07-26.** First A/B vs eps (same prompt/seed/reference, each base at native settings): the finish gap **closed substantially** — soft unbroken linework, controlled palette, correct global values, artist-tag style finally reads. The v-pred+ZTSNR prediction held. Remaining gaps: outfit drifts off-canon (identity, not finish — reference @ 0.35 is a light anchor), residual background texture from the adapter, and final splash polish (hires / `very awa` / LoRA untried on this base). ~10 min incl. 7 GB cold load; sampling pace comparable to eps. | Checkpoint pinned in `comfy/fetch-models.sh`. | — |
| 🔧 Illustrious finetune as base (WAI / Nova / v1.1) | Per-request checkpoint picker shipped. | A finetune file (mostly Civitai, auth-walled — user-sourced). |
| 🔧 Style LoRA (HSR / light-cone) | LoRA fragment + picker shipped. | A LoRA file (Civitai, user-sourced). |

### ⬜ Not tried — identified, unbuilt or unexplored

| Idea | Why it might matter | Source |
|---|---|---|
| ⬜ `very awa` quality tag | NoobAI-specific top-5% aesthetic tag; only meaningful once the noobai lineage is live. | NoobAI model card |
| ⬜ CFG 5.0–5.5 + rescale on v-pred | The documented "softer" band — the run to do *first* on NoobAI. | NAI stepsguidance + NoobAI card |
| ⬜ "Information Extracted" axis (expose `weight_type` / `embeds_scaling`) | NAI's second reference dial (composition-vs-style); we hardcode it. | NAI vibetransfer docs |
| ⬜ Per-character interaction prefixes (`source#`/`target#`/`mutual#`) | Multi-char action binding beyond our regional columns. | NAI multiplecharacters docs |
| ⬜ Director-tool post-ops (Declutter/Emotion/Colorize/RemoveBG) | Cheap parity features on existing img2img/inpaint paths; Line Art already ships in pose. | NAI directortools docs |
| ⬜ SMEA-style high-res sampling | NAI-proprietary; community ComfyUI nodes exist but unvetted. | NAI sampling docs |
| ⬜ FaceDetailer pass | Biggest *perceived* quality jump (faces); planned in `docs/tuning.md`, not built. | tuning.md |

### ✅ Tried — full reference-mode + img2img matrix on v-pred (2026-07-26)

Reference vendored at `eval/benchmarks/testing_the_herta.jpg`; results in
`eval/benchmarks/herta-style.md`.

| Experiment | Verdict |
|---|---|
| ✅ character @ 0.5 + hires (v-pred) | Best figure rendering of the project; 0.5 stable (higher ceiling than eps). **Hires amplifies the adapter's bg texture into streaks** — fix: concrete background tags, or mask the adapter to the subject. |
| ✅ style @ 0.6 (v-pred) | Style/palette transfers cleanly, background stays clean — the bg artifact is specific to *character* embeddings. No identity carry (by design). |
| ✅ character_style @ 0.4, 20 steps (v-pred) | Coherent (no eps-style blowout) but hazy/underdone; dual adapter still ~10× slower — cap steps. |
| ✅ **img2img @ 0.55 denoise on the official art (v-pred)** | **★ Best "official finish" of the entire project.** Inherits composition/values from the source and repaints in the model's hand — real background, near-canonical outfit. For official-look output, i2i-from-official-composition beats every reference mode. |

### ✅ Tried — free levers + HSR style LoRAs (2026-07-26, later)

| Experiment | Verdict |
|---|---|
| ✅ `official art, game cg` tags, fan-artist tags dropped | Real lever: eye rendering and finish move toward game-CG; fan-artist tags actively pull *away* from the miHoYo hand on v-pred. |
| ✅ Concrete background tags (`classroom, chalkboard, indoors`) with character ref | **Fixes the reference background artifact** — no `attn_mask` engineering needed. |
| ✅ HSR v3 LoRA (civitai 1944603) @ 0.7 | **Style winner.** Splash lighting/palette lands; gallery "over-crisp lines" doesn't manifest at 0.7 on our stack. txt2img renders the game logo — weighted `(logo:1.3)` negative. |
| ✅ HSR Official-Art LoRA (civitai 1636274) @ 0.65 | Clean (no gallery "grunge" at reduced weight) but weaker style signature; runner-up. |
| ✅ **FLAGSHIP: v3 LoRA @ 0.7 + i2i 0.45 + official-art tags on v-pred** | Closest to HSR light-cone finish produced. Promoted to `flagship` benchmark variant. |
| ✅ Coloration sweep on the flagship (rescale 0.4/0.55/0.7 × CFG 5.0/5.8 × `very awa`) | **Rescale 0.7 was draining color.** 0.4 = richest grade (i2i anchor prevents burn) → pinned in flagship; 0.55 = new lineage default (safer for unanchored txt2img). CFG 5.8 trades saturation for contrast — taste option. `very awa` goes pastel/airy — wrong direction for richness. |

## Next actions (ranked)

1. ~~NoobAI v-pred~~ **validated.** ~~Style LoRA~~ **validated — v3 @ 0.7 wins; `flagship` variant recorded.**
2. Polish the flagship: denoise sweep 0.35–0.55, hires-on-top @ low denoise,
   `very awa` in quality tags, weighted logo/watermark negatives.
3. txt2img parity: v3 LoRA + character reference together — **blocked by the
   v1 "LoRA cannot combine with references" limit in `api`; lifting it is the
   next engineering task** (LoRA loads before the adapter in the model chain,
   so composition order is already correct).
4. Tier-1 LoRA refinement only if polish plateaus: block-weight merge
   (1636274 output-blocks color × 1944603 structure) or continue-train via
   kohya `--network_weights` on a curated official-art set (~$2 cloud GPU;
   sentinel's WD tagger can auto-caption).
5. Optional identity add-on: all-characters HSR pack (civitai 434763).
