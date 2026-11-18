# Assurance suite

Five capability sweeps that measure pulsar against the "refined NovelAI V5"
target, run through the real API (quality tags, safety negative and the
compliance gate all apply). Each file is one capability; cases share a fixed
seed list so arms compare pairwise.

| File | Capability | Primary metrics |
|---|---|---|
| `01-free-text.yaml` | free-text sentences, mixed with booru tags | tag recall of the grounded prompt; `translate.dropped` surfaced |
| `02-new-characters.yaml` | characters newer than the model (4 tiers, 2 arms) | CCIP identity same-rate, anchor-tag recall |
| `03-quality-accuracy.yaml` | prompt accuracy and quality | attribute recall, person count, quality-tag ablation, rubric |
| `04-styles.yaml` | styles, blends, presets, image references | style-tag probability, identity retention |
| `05-multi-character.yaml` | 2–3 characters, creatures, crowds, interactions | per-region recall, cross-bleed, person count |

```bash
make dev && make run-core && make run-sentinel     # + ComfyUI
./eval/run.sh eval/sweeps/assurance/03-quality-accuracy.yaml
EVAL_SEEDS=2 EVAL_CASES=attr-binding,count-2girls ./eval/run.sh eval/sweeps/assurance/03-quality-accuracy.yaml   # smoke
EVAL_PARAMS='{"steps": 12}' ...                                          # slow dev box only, never for a gated run
./eval/.venv/bin/pip install -r eval/requirements-identity.txt          # CCIP + person counts (optional)
./eval/.venv/bin/python eval/fetch-refs.py "furina_(genshin_impact)"    # 8 solo refs → eval/refs/<tag>/
```

Output: `eval/out/<name>-<stamp>/{sheet.png,results.json,images/}` and a
per-case summary (`recall mean/min`, `bleed`, `count ok`, `ccip same-rate`,
`blocked`, `unmeasurable`). Report every metric as a fraction of seeds with
its n; 16 seeds is the default (standard error ≈ 0.125 on a 50% hit rate),
8 for the character roster because it has 54 cases.

## What the numbers mean

- **Tag recall** — the sentinel's WD EVA02 v3 tagger scores every delivered
  image with the expected tags forced into the report. General tags count at
  ≥ 0.35, character tags at ≥ 0.5. Both thresholds are repo-local choices:
  calibrate them on `02`'s tier-i controls before gating anything.
- **Unmeasurable** — the tagger's vocabulary ends 2024-02 (≥ 600 posts) and
  has no artist tags. Any expected tag outside it (every post-Feb-2024
  character, `ukiyo-e`, `anime_screencap`, `pixel_art`, `game_cg`,
  `official_art`) is listed as unmeasurable and excluded from recall, never
  scored as a miss. This is why `character_hit` cannot be the identity gate
  for capability 2.
- **CCIP** (`dghs-imgutils`, model `ccip-caformer-24-randaug-pruned`,
  threshold 0.178) — zero-shot same-character distance, so it works for
  characters no tagger knows. Per image it is noisy (recall 0.90 / precision
  0.93), so the suite reports the fraction of seeds whose median distance to
  the reference set passes. It assumes one character per image: in
  multi-character cases use the per-region crops and a rubric instead.
  Cross-style comparisons inflate the distance; treat 0.178 as a soft gate
  in `04`.
- **Bleed** — for regional cases, each column crop is tagged with the *other*
  columns' hair/eye tags as probes; bleed is the fraction of probes that
  fire. Column crops mirror `masks.Columns` without the feather.
- **Person count** — `imgutils.detect_person` boxes; `expect_persons` exact,
  `min_persons` lower bound.
- **Compliance** — blocked outputs count toward n and are listed; the gate is
  never relaxed for a benchmark.

## Pass hypotheses (calibrate first, then gate)

| Case family | Hypothesis |
|---|---|
| free-text recall, tags-only parity | recall ≥ 0.80; \|mixed − tags-only\| ≤ 0.10; order delta ≤ 0.10 |
| unmappable concept | concept appears in `translate.dropped`; no tag outside the DB reaches the model |
| new characters, tier i/ii arm `anchors` | CCIP same-rate ≥ 0.80 (otherwise the crop/threshold pipeline is wrong) |
| new characters, tier iii/iv | arm `anchors` near the different-character baseline; arm `autoref` same-rate ≥ 0.60 and ≥ +0.30 over `anchors`; anchor recall ≥ 0.80 |
| attribute binding / camera | recall ≥ 0.85 / ≥ 0.70 |
| counts | exact-rate ≥ 0.90 (one), ≥ 0.75 (two); three reported |
| quality-tag ablation | prompt-tag recall delta ≤ 0.05 |
| style tag swap | style tag ≥ threshold in ≥ 0.80 of seeds; identity retained in ≥ 0.60 |
| two-style blend | Spearman ρ ≥ 0.6 between weight and p(style A) |
| regional multi-character | per-region recall ≥ 0.75, bleed ≤ 0.20, count exact-rate ≥ 0.85 |
| creature / crowd | creature tag ≥ 0.80 with one human; crowd tags present, focus identity retained |
| interactions | correct actor in ≥ 0.60 of seeds (rubric) |

## NovelAI comparison protocol

V5 has no seeds we can pair with. For each case, generate the identical
prompt 4× on V5 (character prompts mapped to NovelAI's character fields;
count tags only in the base prompt, `girl`/`boy` without a number in each
character prompt) and score the downloads with the same tools. Check
journal.novelai.net and the Precise Reference docs before each run: V5
shipped (2026-08-21) with no image references, so capability 4's reference
cases compare against V4.5 Precise Reference / Vibe Transfer, and V5's style
control is prompt-only. V5's 22-character figure is vendor-tested; 6–8 is
what independent users report — use that as the realistic bar.

## Not measurable yet (follow-ups)

- **Newer-character tag detection**: PixAI tagger v1.0 (data cutoff May 2026)
  covers tiers iii/iv; running it as a second sidecar would restore
  `character_hit` for them. CCIP is the primary metric until then.
- **Artist / image-reference style similarity**: CSD is the only open artist
  embedding and its anime validity is unverified (trained on LAION-Styles,
  evaluated on WikiArt); calibrate same-artist vs different-artist cosine on
  real Danbooru pairs before using it. `04`'s artist and `ref-style` cases
  are report-only until then.
- **Aesthetic / quality predictors** (Aesthetic Shadow, ImageReward, PickScore,
  HPSv2): no anime-domain evidence survived review; use only for arm-vs-arm
  deltas after a sanity check on a Danbooru high/low split.
- **Per-character CCIP in multi-character images** needs person-box ∩ region
  crops gated on face-count = 1 (the waifuc pipeline); today those cases use
  per-region tag recall + rubric.
- **Human rubric** (anatomy 0/1/2, overcook, composition copy, correct actor)
  with two raters; agreement reported.

Tag names in `02` were verified against the live Danbooru API on
2026-11-14 (ZZZ uses mixed bare/qualified names — match exactly); per-character
debut dates were not, and ~a third of "new" Danbooru character tags are
renames or costume splits of old characters, so confirm first-post dates
before adding to the roster.
