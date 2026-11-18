# miHoYo house-style benchmark (Jean / Acheron)

**Goal:** push the [herta flagship recipe](herta-style.md) past its img2img
crutch. The flagship leaned on an official-art source composition (denoise
0.45) to reach HSR-official finish; this benchmark asks whether the **style
stack alone** — NoobAI v-pred + `official art, game cg` + HSR v3 LoRA —
delivers that finish on **pure txt2img** with tag-carried identity.

Character choice is the point:

- **Jean (Genshin Impact, 2020)** — deep in-model, and a *cross-IP* test: a
  Genshin character rendered through the HSR-trained style LoRA. If the
  house-style generalizes, the LoRA learned the miHoYo hand, not HSR assets.
- **Acheron (HSR 2.1, 2024-03)** — inside NoobAI's 2024-10-23 cutoff (unlike
  The Herta), so tag identity works without any reference. Her night scene is
  where v-pred + Zero-Terminal-SNR should beat eps outright: true darks
  instead of grey wash.

Both scenes are lighting stress tests from opposite ends: hard daylight
sunbeams vs. faint moonlight.

## Run it

```bash
./eval/benchmarks/run-mihoyo.sh jean            # flagship txt2img, LoRA 0.7
./eval/benchmarks/run-mihoyo.sh acheron
./eval/benchmarks/run-mihoyo.sh jean-nolora     # ablation: tags only
./eval/benchmarks/run-mihoyo.sh acheron-nolora
./eval/benchmarks/run-mihoyo.sh jean-w08        # LoRA 0.8 — no i2i anchor, style may need more weight
./eval/benchmarks/run-mihoyo.sh acheron-w08
```

Spec lives in `mihoyo-style.json`. Prompt merge order (runner-enforced):
**style carriers → character → scene → quality scaffold.** No reference
upload, no img2img source; `auto_reference` is pinned off so identity is a
pure tag test.

## Scene brief → booru grounding

The brief is natural language; the prompt is tag vocabulary. Mapping used:

| Brief | Tags |
|---|---|
| tying hair | `tying hair, arms up` |
| sun ray | `sunlight, sunbeam, light rays` |
| grass field | `grass, field` (+ `blue sky, wind`) |
| faint lighting, moon light | `night, moonlight, full moon, dark` |
| traditional umbrella | `oil-paper umbrella, holding umbrella` |
| quarter view / front | `three-quarter view, looking at viewer` — camera tags fight each other, so one committed choice, not `straight-on` stacked on top. Both characters; the original from-side brief renders are kept as `out/jean-fromside.png` / `out/acheron-fromside.png` |
| original costume | base character tag (alt skins have their own tags, e.g. `jean (sea breeze dandelion)`) + signature outfit tags for Jean; Acheron's outfit rides the character tag alone |
| highres, HD, faint glow, detailed, detailed color, reflective eyes, smooth color | **translated, not copied** — see the grounding audit below |

## Grounding audit (2026-08-05) — why round 2 looked soft

Round 2 was judged broken: faces disproportionate, image blurry. Checking the
requested style scaffold against the tag DB (`GET /tags/lookup`) explains most
of it — **seven of the nine descriptive style tags are vocabulary the model
never saw**, and dead tokens don't just do nothing, they dilute attention away
from the tags that work. This is CLAUDE.md #4 in the wild: `min_post_count` is
the accuracy lever, not CFG.

| Requested | DB result | Shipped instead |
|---|---|---|
| `HD` | **MISS** | dropped |
| `detailed` | **MISS** | dropped |
| `detailed color` | **MISS** | dropped |
| `smooth color` | **MISS** | dropped |
| `smooth shading` | **MISS** | dropped |
| `soft shading` | **MISS** | dropped |
| `faint glow` | **MISS** | `glowing` (135k) + `light particles` (79k) + `backlighting` (45k) |
| `reflective eyes` | alias → `eye_reflection`, only **1.6k** posts — far too thin to steer | `sparkling eyes` (18k) + `eyelashes` (255k) |
| `highres` | `highres` 7.8M ✓ | kept |

`masterpiece, best quality, very aesthetic` are also DB-misses but **stay** —
they're model-level aesthetic tokens baked into Illustrious/NoobAI training,
not booru concepts, so the tag DB is the wrong oracle for them. `absurdres`
(2.8M) is both. Added `depth of field` (124k) for painterly subject/background
separation, which also makes the face read crisper by contrast.

**Framing is the other half of the fix.** At `full body` (1.2M) the face
occupies ~90 px natively; no refine pass invents structure that was never
sampled — it only sharpens the mistake, which is exactly the "sharp but wrong"
look round 2 had. Moving to `cowboy shot` (823k, mid-thigh up) roughly triples
face pixels. `kneeling` / `standing` stay in the prompt so the posture the
wider framing used to show is still stated. Short of the unimplemented
FaceDetailer pass, this is the fix.

## Full prompts (as assembled by the runner)

**Jean:**

```
official art, game cg, jean (genshin impact), 1girl, mature female, blonde hair, ponytail, hair between eyes, blue eyes, pale skin, strapless, detached sleeves, white pants, gloves, tying hair, arms up, kneeling, three-quarter view, looking at viewer, outdoors, day, sunlight, sunbeam, dappled sunlight, lens flare, light rays, grass, field, blue sky, wind, cowboy shot, masterpiece, best quality, very aesthetic, absurdres, highres, painterly, gradient hair, gradient eyes, sparkling eyes, eyelashes, shiny skin, glint, sparkle, glowing, depth of field, blurry background
```

**Acheron:**

```
official art, game cg, acheron (honkai: star rail), 1girl, mature female, very long hair, purple hair, streaked hair, multicolored hair, purple eyes, hair over one eye, flower-shaped pupils, holding umbrella, oil-paper umbrella, standing, three-quarter view, looking at viewer, night, moonlight, full moon, dark, outdoors, cowboy shot, purple theme, masterpiece, best quality, very aesthetic, absurdres, highres, painterly, gradient hair, gradient eyes, sparkling eyes, eyelashes, shiny skin, glint, sparkle, glowing, depth of field, blurry background
```

**Negative (both)** — core prepends the forbidden-tag safety negative
unconditionally; the spec adds:

```
(logo:1.3), worst quality, low quality, normal quality, bad quality, displeasing, very displeasing, lowres, blurry, upscaled, jpeg artifacts, artistic error, film grain, scan artifacts, chromatic aberration, halftone, bad anatomy, bad hands, bad proportions, extra arms, extra hands, long neck, anatomical nonsense, alternate costume, official alternate costume, missing fingers, extra digit, fewer digits, extra limbs, deformed, distortion, mismatched pupils, poorly drawn face, soft focus, motion blur, multiple views, multiple girls, 2girls, text, error, signature, watermark, username, cropped, out of frame, realistic, photorealistic
```

`(logo:1.3)` is **mandatory**: the HSR v3 LoRA renders the game logo on
txt2img (herta finding), and every variant here is txt2img. The herta
negative was *not* reused — it carries Herta identity guards (`hair tie`,
`ponytail`-adjacent bans, `shadow`) that would fight Jean's tying-hair pose
and the sunbeam scene.

## Config (all variants)

| Knob | Value | Why |
|---|---|---|
| lineage / checkpoint | `noobai` / `noobai-xl-vpred.safetensors` | flagship base; v-pred+ZTSNR is the finish fix |
| sampler / scheduler | `euler` / `normal` | noobai lineage settings |
| cfg / steps | 5.0 / 28 | noobai lineage settings |
| cfg_rescale | *unset* → lineage default 0.55 | the flagship's 0.4 was tuned against an i2i color anchor that doesn't exist here |
| size / seed | 832×1216 / 88 | benchmark constants |
| hires | `true`, scale **1.5**, denoise **0.55** | load-bearing for eye catchlights; ~2× generation time. Round 2 finding: the 1.5×/0.4 defaults only *sharpen* the malformed native-res face — full-body framing leaves it ~90 px and it reads disproportionate + blurry under zoom. 0.55 makes the refine actually redraw it at no extra memory. Scale stays at 1.5: a 1.8× attempt was in flight when the 17 GB Mac hung on RAM — treat >1.5 on MPS as off-limits, not merely slow. The real fix is FaceDetailer (docs/tuning.md, designed default-ON) which is **not implemented yet** — this benchmark is its motivating case. |
| loras | `honkai star rail_v3.safetensors` @ 0.7 | flagship weight; `-w08` variants probe 0.8, `-nolora` ablates |
| quality_tags / uc_preset / auto_reference | all `false` | spec is fully self-contained and deterministic regardless of env presets |

## Last run (2026-08-05) — this is the config in `mihoyo-style.json`

The spec on disk **is** the round-3 config, verified field-by-field against
what core actually received (jobs `ec8d145b` / `98fbb66b`, both `done`) rather
than assumed from the edit history:

| | |
|---|---|
| Prompt scaffold | `masterpiece, best quality, very aesthetic, absurdres, highres, sparkling eyes, eyelashes, glowing, light particles, backlighting, depth of field` |
| Framing | `cowboy shot` (was `full body`) |
| Sampler / scheduler / CFG | `euler` / `normal` / 5.0 — noobai lineage defaults (the karras finish recipe was tested and **rejected**, see below) |
| CFG rescale | **0.45** — measured winner, not the 0.55 lineage default |
| Steps / size / seed | 28 / 832×1216 / 88 |
| Hires | on, scale 1.5, denoise 0.55 |
| LoRA | `honkai star rail_v3.safetensors` @ **0.8** — measured winner over 0.7 |
| Negative add | blur guards (`soft focus`, `motion blur`), anatomy guards, and alternate-costume suppression |

Reproduce: `CORE_URL=http://localhost:8080 ./eval/benchmarks/run-mihoyo.sh jean`

## Results

| Variant | Verdict |
|---|---|
| jean (round 1 — from side, full body, `out/jean-fromside.png`) | **Strong hit, first try.** Genshin loading-screen finish; cross-IP transfer works — the HSR LoRA carried the miHoYo hand onto a Genshin character. Full brief compliance (tying hair, kneeling, profile, sunbeams). Nits: hand/gauntlet cluster muddled, profile face a touch generic, brooch reads hydro-ish. |
| jean (round 2 — three-quarter, full body, `out/jean-hires04.png`) | **★ Best of round 2.** Camera swap alone lifted the face: multi-tone irises with catchlights, near-canon bodice with gold Favonius-style embroidery, belt chain, diamond pants, gold-trim boots. Pose/scene fully held. |
| acheron (round 1 — from side, full body, `out/acheron-fromside.png`) | **Striking night splash** — v-pred delivers true darks, huge moon + water reflection, red umbrella as sole accent. Unprompted katana handle from the character tag. Misses: bangs shadow BOTH eyes (`reflective eyes`/`flower-shaped pupils` defeated), umbrella grip floaty. |
| acheron (round 2 — three-quarter, full body, `out/acheron-hires04.png`) | **Splash-art grade for round 2.** Umbrella now properly gripped; `hair over one eye` correct (one eye visible); canon thigh tattoo, clawed gauntlet, glowing red flower, chest-strap top all present. Moon + petals + rooftops + moonlit water composition. Nits: visible eye small/downcast so the catchlight is subtle; feet read slightly floaty over the water. |
| **jean (round 3, grounded + cowboy shot, `out/jean.png`)** | **★★ Best of the project.** The face/eye complaint is resolved — both eyes cleanly sampled with catchlights, symmetrical, correct structure, crisp linework. `depth of field` produced foreground-grass and background-hill bokeh (very miHoYo splash); `light particles` gave drifting motes in the sunbeams; the gold Favonius embroidery, gem belt, beaded chain and choker are all legible now. **Cost: `kneeling` is lost** — at mid-thigh framing she reads as standing/leaning. |
| **acheron (round 3, `out/acheron.png`)** | **★★ Face fixed, identity richest yet.** Large proportional face, one visible violet eye with a catchlight (`hair over one eye` correct), plus earring, gem choker, black corset, clawed gauntlet with chain, flame-motif shorts. Dramatic low-angle moon composition with petals. **Two regressions:** (1) a second dark pole crosses the frame diagonally — the katana and the umbrella handle appear to have partly duplicated; (2) the background is hazier/lighter than the round-1 night — `backlighting` + `glowing` + `light particles` lifted the blacks that v-pred's ZTSNR was giving us for free. For the night scene, drop `backlighting` and keep `glowing` only. |
| **jean-a / jean-b (round 4 regen, seeds 1234 / 7777)** | Round-3 config, fresh seeds. Both clean: crisp faces, dappled light, drifting petals, legible gold embroidery. `-b` has the stronger sunbeam wash. |
| **acheron-a / acheron-b (round 4, `backlighting` dropped)** | **Night fix confirmed.** Deep blue-black sky restored, moon actually glows, red umbrella reads as the sole accent. Both pulled canon detail unprompted — mask shoulder-piece, katana, flame thigh marking, and red spider lilies (higanbana) in `-a`. |
| **jean-s1 / acheron-s1 (refined scaffold, euler)** | **★★★ The recipe.** Smooth gradient hair, layered irises with catchlights, specular glints, painterly bokeh. `acheron-s1` is the best image of the project — deep night, purple blossoms, sparkle, blacks fully intact. Promoted to base. |
| **jean-s2 / acheron-s2 (same tags, dpmpp_2m/karras/cfg 5.5)** | **✗ REGRESSION — recipe rejected.** `acheron-s2`'s night sky washed from deep blue-black to lavender haze, moon blown out, umbrella lost its red; `jean-s2` blew highlights and lost background detail. See the sampler finding below. |
| **jean-r1 / jean-r2 (34 steps, LoRA 0.7 vs 0.8)** | Both clean. 0.8 has richer gold embroidery, no grunge flaw → **LoRA weight settled at 0.8**. 34 steps only marginally sharper; reverted to 28. |
| **jean-c45 / acheron-c45 / acheron-c45b (cfg_rescale 0.45)** | **★★★ Current best.** Deep saturated gold, crisp sparkle, full sky on Jean; on Acheron, intact night blacks, glowing moon, blossom bokeh, and the full canon kit — mask shoulder-piece, gauntlet chain, flame thigh marking, gradient violet→lavender hair. |
| **jean-c70 (cfg_rescale 0.70)** | **✗ Softer end loses.** Pastel and washed, gold pales, plus a collar artifact. Confirms lower rescale for this style. |
| acheron-c70 | **never rendered** — died twice to machine hangs. High-rescale end is confirmed on Jean only. |
| **herta-lora** (LoRA 0.8, tags + anchors) | **★★ Best atmosphere.** Warm window light, sparkle, painterly fabric, steaming second teacup on the table. Scene brief fully met. Identity good but slightly loose — outfit reads impressionistic next to `-ref`. |
| **herta-ref** (auto_reference, no LoRA) | **★★ Best identity.** Canon details land sharper: white bow with gold key pendant, blue-purple ribbon, skirt chain, blue armband. Style is flatter / less atmospheric without the LoRA. |
| **himeko-1234** (lean tag identity) | **★★ Keeper.** Gorgeous afternoon light — sunbeams through curtains, sparkle, two teacups, balcony railing, plant. Canon markers present: red hair, yellow eyes, mole under eye, gold rose choker, gold laurel embroidery, red underskirt. |
| **himeko-7777** | **Weaker.** Same light quality, but the costume drifts to a generic white dress — the gold embroidery, rose and jewellery all drop out. Shows the lean-tag approach is seed-fragile at her tag density. |
| jean-nolora | *(pending)* |
| acheron-nolora | *(pending)* |
| jean-w08 | *(pending)* |
| acheron-w08 | *(pending)* |

Three rounds, each a user-directed revision: **round 1** ran the brief
literally (`from side, profile`, `full body`); **round 2** swapped both
cameras to `three-quarter view, looking at viewer` because the side profiles
hid the faces; **round 3** (the current spec) fixed the faces properly via the
grounding audit + `cowboy shot`. Round-1/2 renders are kept under
`out/*-fromside.png` and `out/*-hires04.png` for A/B.

Judging axes: (1) miHoYo finish — painterly fabric, warm splash lighting,
iridescent multi-tone eyes; (2) identity fidelity — on-model original
costume; (3) scene adherence — pose, camera, lighting brief; (4) the v3
grunge flaw at 0.8; (5) logo bleed despite the weighted negative.

## Refinement round (2026-08-09) — smooth colour, lighting, reflection, shading

Second grounding sweep, this time for finish rather than framing. The English
words for "smooth shading" mostly aren't in the model's vocabulary; the booru
equivalents are, and they're what the miHoYo look actually rides on.

| Ask | Shipped (grounded) | Confirmed MISS — stop reaching for these |
|---|---|---|
| smooth coloration | `gradient hair` (216k), `gradient eyes` (12.7k), `purple theme` (13.5k) | `gradient`, `smooth`, `vivid colors`, `cel shading` |
| reflection | `glint` (37k), `shiny skin` (154k) | `specular highlights`, `glossy`, `reflective` |
| illumination | `sparkle` (224k, replaces `light particles` 79k), `glowing` (135k) | `soft lighting`, `dramatic lighting`, `rim light` |
| shades / depth | `painterly` (10k), `blurry background` (181k), `depth of field` (124k) | `ambient occlusion`, `digital painting`, `detailed background` |

### Sampler A/B — the `docs/tuning.md` finish recipe does **not** transfer

Both `-s1` and `-s2` ran the identical refined scaffold at seed 1234; the only
variable was the sampler. `-s1` used the noobai lineage default
(`euler`/`normal`/cfg 5.0); `-s2` used the `docs/tuning.md` "official-art
finish" recipe (`dpmpp_2m`/`karras`/cfg 5.5).

**`-s2` lost, decisively, in exactly the predicted way.** Acheron's night sky
washed from deep blue-black to lavender haze, the full moon blew out, and the
red umbrella desaturated; Jean's highlights clipped and her field lost detail.
The karras sigma schedule clips the terminal sigma that v-pred + Zero-Terminal-SNR
depends on for true blacks — which is precisely the property that makes the
night scene work on this lineage.

That recipe is **eps-lineage guidance**. This is CLAUDE.md #3 measured rather
than assumed: settings don't transfer between lineages. `docs/tuning.md` has
been annotated so the next person doesn't repeat it. Keep `euler`/`normal`/cfg 5.0.

## The Herta (added 2026-08-09) — the post-cutoff case

Jean and Acheron are both inside NoobAI's cutoff, so they isolate *style*.
**The Herta does not**: HSR 3.0, Jan 2025, well past 2024-10-23. Her tag exists
in the DB (3,448 posts) but every one of those posts is newer than the model's
training, so the tag carries no identity. She is the benchmark's *identity*
case, and she runs into a hard product constraint:

> `core` refuses `LoRA + reference` outright (`api.go:380`,
> "LoRA cannot combine with references or characters yet").

Identity wants a reference; style wants the LoRA. You cannot have both today,
so the two variants take opposite sides of that constraint and measure the
cost:

| Variant | Identity from | Style from |
|---|---|---|
| `herta-lora` | character tag + core's live design anchors + explicit design tags | full recipe, HSR v3 LoRA @ 0.8 |
| `herta-ref` | `auto_reference` → consensus Danbooru exemplar | tags only, **no LoRA** |

### The design-anchor correction (worth remembering)

First attempt shipped explicit design tags from memory — `laurel crown`,
`white dress`, `cape`, `gold trim`. Core's anchor path disagreed, injecting
`hat, witch hat, black hat, brown hair, dress` ahead of them, so `brown hair`
outranked my `grey hair` and the prompt was at war with itself.

Checking the vendored reference (`testing_the_herta_3.jpg`) settled it: **the
anchors were right and the memory was wrong.** The Herta's canonical design is
the witch look — wide-brimmed black hat with a purple flower, greyish-brown
hair fading to lavender, purple eyes, black corset dress with a keyhole motif,
white frills, purple ribbons, key necklace. The design tags were rewritten to
match, and core's dedup then stopped re-injecting the conflicting anchors
because they were already present.

The lesson is the repo's own rule (CLAUDE.md: ground against the tag DB, never
the model's imagination) applied to *me*: empirical consensus from 3,448 real
posts beat recall about a character released near the knowledge cutoff. When
the anchor path contradicts your design tags, check a reference before assuming
the anchors are polluted.

Scene note: `afternoon` was dropped — it returns **count 1** via live lookup,
i.e. vocabulary that does not exist. Afternoon light is expressed the way the
model knows it: `day` + `sunlight` through a `window` + `light rays` +
`dappled sunlight`.

## Himeko (added 2026-08-09) — tag density decides how lean you can go

In-model (HSR launch, 2023), and core confirmed it by **not** firing the anchor
path — so no reference, no anchors, full style recipe. Disambiguation matters:
bare `himeko` is a DB **miss** and `murata_himeko` (1,490) is the *Honkai
Impact 3rd* character; only `himeko (honkai: star rail)` targets her.

I deliberately used a **lean** character string — tag, hair, eyes, mole, build,
**no outfit tags** — reasoning from Acheron, whose canon outfit emerged
unprompted, and from The Herta, whose over-specified costume fought core's
anchors. That reasoning was **half right**, and the seed pair exposed where it
breaks:

| Character | Tag posts | Outfit from tag alone? |
|---|---|---|
| `acheron_(honkai:_star_rail)` | 6,683 | **yes** — katana, mask piece, flame marking, all unprompted |
| `jean_(genshin_impact)` | 4,056 | yes, with light reinforcement |
| `the_herta_(honkai:_star_rail)` | 3,448 | n/a — post-cutoff, posts are newer than training |
| `himeko_(honkai:_star_rail)` | **2,019** | **inconsistently** — holds on seed 1234, collapses to a generic white dress on 7777 |

**The rule this gives us:** how lean the character string can be is a function
of *tag density*, not of in-model/post-cutoff status. Around ~2k posts the tag
alone is seed-fragile and needs explicit outfit tags to be reliable; at ~6k it
carries the costume by itself. This is `min_post_count` (CLAUDE.md #4) showing
up as identity stability rather than as prompt adherence.

Note the `alternate costume` / `official alternate costume` negatives did *not*
rescue the 7777 drift — they suppress *named alternate skins*, not generic
non-canon clothing. Fix if her canonical navigator outfit is required: add
explicit outfit tags rather than relying on the tag or the negative.

## Rounds 5–6 (2026-08-09) — character fidelity, LoRA weight, cfg_rescale

**LoRA weight settled (the long-pending `-w08` ablation).** `jean-r1` @ 0.7 vs
`jean-r2` @ 0.8, same seed: **0.8 wins** — richer gold embroidery, no trace of
the v3 grunge flaw the LoRA's own gallery showed. Promoted to base.

**Character-fidelity negatives added.** Grounded anatomy guards
(`bad proportions`, `extra arms`, `extra hands`, `long neck`,
`anatomical nonsense`) plus — the useful one — `alternate costume` (625k) and
`official alternate costume` (544k). "Original costume" was in the brief from
the start but nothing in the negative enforced it; identity rode entirely on
the positive character tag until now.

**Steps 34 reverted to 28.** Only marginally sharper on Jean, and it pushed
render time far enough that it compounded the MPS crawl below.

**`cfg_rescale` 0.45 wins — the 0.55 lineage default was leaving contrast on
the table.** Bracketed 0.45 vs 0.70 at fixed seed: 0.45 gives deeper saturated
gold, crisp sparkle and a fuller sky; 0.70 goes pastel and washed, pales the
gold, and threw a collar artifact on `jean-c70`. This **corroborates the herta
i2i colour sweep** (0.4 winner there), which had been assumed not to transfer
to txt2img. It transfers. Promoted to base.

Caveat on the evidence: **`acheron-c70` was never rendered** — it died twice to
machine hangs — so the high-rescale end is confirmed on Jean only. The render
budget went to a second Acheron keeper at the winning config instead of
re-confirming a setting already rejected.

### The MPS crawl is the real operational limit

Two renders were lost to it. After ~12 renders in one session, a job that
normally takes ~6 min ran past **24 minutes** and blocked the queue — core
gave up at its 20m `EXEC_TIMEOUT` (already raised in the local compose
override) while ComfyUI kept chewing. Restarting ComfyUI is the fix, and it
must be done *proactively* on a long session, not after the first timeout.
Symptom to watch: a job in `running` past ~10 min, or `queue_running 1` with a
core job already marked `failed` — that is an orphan, and it starves
everything behind it. Clear with `POST /interrupt` + `POST /queue {"clear":true}`,
then restart ComfyUI.

Also: start ComfyUI and sentinel with `nohup … & disown`. Launched as ordinary
supervised background jobs they get torn down with the parent shell, which
killed the stack three times in one session (macOS has no `setsid`).

## Iteration levers (next, in order)

1. **Fix the two round-3 regressions on Acheron** — drop `backlighting` for
   dark scenes (it lifts the blacks v-pred gives free) and add
   `holding umbrella` disambiguation / `weapon` to the negative to stop the
   katana+handle duplication.
2. **Restore Jean's `kneeling`** without losing the face. Either a landscape
   bucket (1216×832) where a kneeling figure fills the frame at larger face
   scale, or `squatting`/`sitting` variants that survive a closer crop.
3. **Run the four remaining ablations** (`-nolora`, `-w08` × 2 characters) —
   still the open question of how much the LoRA vs the official-art tags are
   each contributing.
4. **Isolated A/B of the docs/tuning.md finish recipe** (`dpmpp_2m` /
   `karras` / cfg 5.5) against the lineage default, judged on Acheron's night
   blacks specifically — the karras terminal-sigma concern is a hypothesis,
   not a measurement.
5. **cfg_rescale sweep** 0.4–0.7 — Acheron's dark scene is the sensitive one;
   too low burns the moonlight highlights, too high greys the darks.
6. **FaceDetailer** (docs/tuning.md, designed default-ON, not implemented) —
   the principled fix that would let `full body` framing keep a good face and
   make levers 2 unnecessary. This benchmark is its motivating case.

## Ops (learned the hard way)

The stack here is **hybrid**: postgres + core in Docker, ComfyUI and sentinel
native. Start order: postgres (wait `healthy` — core exits if it races it) →
`./comfy/run-native.sh` → `./sentinel/run.sh` → core. Core listens on **8080**
while these runners default to 8081, so prefix `CORE_URL=http://localhost:8080`.

**If sentinel is down, renders complete and are then thrown away**: the
post-generation compliance hook fails closed and the job goes `blocked` with
`compliance check unavailable`. That is correct behaviour, never something to
route around — it just means start sentinel *before* burning GPU time.
`blocked` here means "gate unavailable", not "content refused".

Memory-bound on the 17 GB Mac (MPS): `hires_scale > 1.5` is off-limits — a 1.8×
attempt coincided with a full machine hang that took Docker and ComfyUI with
it. Raise `hires_denoise` instead; it costs no extra memory. ~6 min/image at
1.5×. Restart ComfyUI between long sessions or runs crawl.
