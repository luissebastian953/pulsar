# Precise Reference

Character and style consistency from a reference image. This is the priority
NovelAI feature to match — and the one place we can beat them.

*(NovelAI calls it "Precise Reference." Not "Precise Transfer" — searching the wrong
name finds nothing.)*

## How it works

IP-Adapter uses **decoupled cross-attention**: the reference image goes through a
CLIP-Vision encoder, gets projected into embedding tokens, and those inject into the
UNet's cross-attention via a *separate* set of K/V projections running parallel to
the text ones. Text and image condition the same attention layers independently —
which is exactly why their relative influence is dial-able.

That's structurally what NAI's Fidelity slider does.

## Mapping

| NovelAI control | ComfyUI equivalent |
|---|---|
| Character Reference | IP-Adapter **Plus** (`ip-adapter-plus_sdxl_vit-h`) |
| Style Reference | IP-Adapter, `weight_type: style transfer` |
| Fidelity slider | IP-Adapter `weight` (0.0–1.2; negatives are valid) |
| Blend both | Two chained `IPAdapterAdvanced` nodes |
| *(NAI can't do this)* | `attn_mask` → per-character separate references |

Node suite: **ComfyUI_IPAdapter_plus** (cubiq). Pin the version in `comfy/`.

## Graph

```
LoadImage(ref) ─┐
                ├─► IPAdapterAdvanced ──► MODEL ──► KSampler
CheckpointLoader ┘        ▲
                    IPAdapterUnifiedLoader
                    (CLIP-Vision + adapter)
```

Chain for character + style:
`model → IPAdapter(character, plus) → IPAdapter(style, style transfer) → KSampler`

## Parameters that matter

- **`weight`** → the Fidelity slider. ~0.5 loose, ~0.8 strong, ~1.0+ hard to override
  with prompts — which matches NAI's described high-fidelity behaviour.
- **`weight_type`** → the sleeper feature. `style transfer` and `composition` are
  SDXL-only modes injecting into *different* attention blocks. Style transfer takes
  aesthetic without composition; composition takes layout without style. This gives
  us NAI's character-vs-style split from one adapter, cleaner than what they expose.
- **`start_at` / `end_at`** → step-range scheduling. Ending around 0.7 lets the
  prompt reclaim the last steps for detail. **Confirmed empirically on
  Illustrious**: full-range linear at 0.8 produces severe color burn (pink wash,
  banding) and identity drift; `end_at: 0.7` eliminates the burn outright, so
  the fragment ships it as the default. Working recipe: weight ~0.5 + identity
  tags in the prompt + end_at 0.7 → clean identity carry-over.
- **`attn_mask`** → below.

## Where we beat NovelAI

NAI's current limitation: **multiple character references blend together.** They
can't keep two references separate.

We can. Feed each `IPAdapterAdvanced` an `attn_mask` for that character's region and
each reference conditions only its own area. Combine with regional character prompts
— **the same masks** — and you get per-character reference *and* per-character tags.

This is why the masking subsystem is shared between `regional.json` and
`ipadapter.json`. Build it once. It's also the strongest single differentiator in
the product.

## Style mode — status

`ref_mode: "style"` maps to `weight_type: style transfer` as designed. First
live test at weight 0.7 over-injected: scratchy overdrawn linework and eye
color drift. Treat as experimental until an eval sweep finds the weight band
(likely ≤0.5); for "official-art finish" the hires pass alone
(docs/tuning.md#quality-passes) got there without a style reference.

## Per-character references — status

Built: `patches/ipadapter2.json` chains two masked `IPAdapterAdvanced` nodes,
each `attn_mask` reading its character's regional column mask — requires the
regional path (exactly 2 characters, both with `reference_id`; global and
per-character references are mutually exclusive). Live results on
Illustrious:

- **Identity binding works and never cross-blends** — each column carries
  its own reference's hair/eyes/outfit. The core NAI-can't-do-this claim is
  demonstrated.
- **Composition stability is not yet tuned.** weight 0.6 → refs' plain
  backgrounds swallowed the scene and a third figure grew in the mask seam;
  weight 0.4 → scene recovered, identity held, but one figure rendered
  partially. Different failure each run = sweep territory: weight 0.4–0.6 ×
  seed × framing in `eval/` before exposing per-character fidelity in orbit
  beyond the shared slider.

## Caveats — read before building

**Don't use IP-Adapter FaceID for anime.** FaceID depends on InsightFace for face
embedding, and InsightFace is trained on *photographic* faces — it detects anime
faces unreliably or not at all. Use IP-Adapter Plus, which carries identity through
general image features with no face detection. This is worth knowing before sinking
a day into FaceID.

**The adapters are photoreal-biased.** SDXL IP-Adapters were trained mostly on real
images. On Illustrious/NoobAI they work, but with a pull toward realism and a softer
identity lock than NAI's version. Expect tuning time; don't expect parity.

**NAI's is native, ours is bolt-on.** Precise Reference is trained into V4.5. We're
attaching an adapter to a community model. The gap is real and settings won't close
it.

**LoRA still wins for hero characters.** Zero-shot is the appeal, but a per-character
LoRA beats IP-Adapter on fidelity every time. Offer both: IP-Adapter as the instant
path, LoRA as the premium "pin this character" tier.

## Cost

CLIP-Vision encode is a few hundred ms once, then small per-step cross-attn
overhead. Roughly 10–20% latency — not 2×.

## Integration

Upload plumbing is **identical to img2img**: `POST /upload/image` → patch
`LoadImage`. One ingest path serves img2img, inpaint, and Precise Reference. Same
compliance hook.

## Vibe Transfer

Lower priority, not needed yet. It's a standard IP-Adapter (loose style/mood) — the
same machinery at lower weight without the identity adapter. If Precise Reference
works, Vibe Transfer is nearly free.
