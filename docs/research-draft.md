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
| 🔧 **NoobAI-XL v-pred lineage** | Fully wired: `patches/vpred.json` (ModelSamplingDiscrete + RescaleCFG), per-lineage defaults (CFG 5 / Euler / 28 steps / rescale 0.7), per-request `lineage`, UI picker. | Checkpoint file → `comfy/models/checkpoints/` (7.1 GB, HF link above). **Highest-expected-value untested lever.** |
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

## Next actions (ranked)

1. Download NoobAI-XL v-pred → `comfy/models/checkpoints/` → UI: lineage
   *NoobAI · v-pred* + that checkpoint → rerun the Herta benchmark
   (character @ 0.35, single adapter) → record verdict in the ledger.
2. On the new base: CFG 5.0–5.5, rescale 0.5–0.7 sweep, `very awa` appended
   to quality tags (env `QUALITY_TAGS`).
3. Then, and only then, the LoRA last-mile if the style still misses.
