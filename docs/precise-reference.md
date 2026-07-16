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
  prompt reclaim the last steps for detail. Under-used; worth exposing internally
  even if not to users.
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
