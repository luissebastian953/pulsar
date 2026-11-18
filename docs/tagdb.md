# Tag database

The tag DB is pulsar's edge. Everyone building on Illustrious has the same model;
almost nobody has a model-aware tag vocabulary in front of it.

## Why it exists

An anime model is "accurate" when prompted with tags it saw often in training. Tags
aren't magic words — they're the model's actual vocabulary. A tag that's common on
Danbooru but rare in the model's training set produces nothing.

So the tag DB isn't a copy of Danbooru. It's **the intersection of "exists on
Danbooru" and "the model learned it."** That intersection is what both autocomplete
and NL grounding query.

## Schema

```sql
CREATE TABLE tags (
  name          TEXT PRIMARY KEY,      -- canonical, underscored
  category      TEXT NOT NULL,         -- general|character|copyright|artist|meta
  post_count    INT  NOT NULL,         -- Danbooru frequency; proxy for model knowledge
  pose_relevant BOOL NOT NULL DEFAULT FALSE
);

CREATE TABLE tag_aliases (
  alias     TEXT PRIMARY KEY,
  canonical TEXT NOT NULL REFERENCES tags(name)
);

CREATE INDEX tags_trgm ON tags USING GIN (name gin_trgm_ops);
CREATE INDEX tags_pose ON tags (pose_relevant) WHERE pose_relevant;
```

`pose_relevant` is one boolean, not a second table. The pose page's autocomplete is
one `WHERE` clause off the art page's.

## Ingest

Source: Danbooru's `/tags.json` and `/tag_aliases.json`, or the public dumps.

```
dump → filter (post_count >= floor) → normalise → upsert tags
     → resolve aliases → upsert tag_aliases
     → classify pose_relevant
```

Ideally, weight by **your model's** training-tag histogram rather than Danbooru's
raw counts. Until that exists, `post_count` is the proxy.

## `min_post_count` — the accuracy lever

This threshold is the single highest-leverage knob in the product.

- **Raise it** → only strong, well-learned tags survive → high adherence, less
  expressive range
- **Lower it** → rarer tags accepted → more range, weaker adherence, more noise

Tune it against real generations, not intuition. Use the eval harness (`eval/`).
This moves prompt adherence more than any CFG or sampler change.

## Grounding cascade

Used by `nltag` to snap a free-text concept to a canonical tag:

```
concept → normalise
        → exact / alias hit?         → canonical
        → fuzzy (WRatio >= ~88)?     → canonical      (typos, minor wording)
        → embedding NN (>= ~0.55)?   → canonical      (paraphrases)
        → None                       → DROP
```

**Returning `None` is correct behaviour, not a failure.** A concept the model has no
tag for should be dropped, never force-matched into a bad tag. Force-matching is how
you get prompts that quietly produce garbage.

Surface dropped concepts to the user — "couldn't map: *ethereal vibes*". That
transparency is what makes tag translation not feel like a black box, and it's a UX
affordance NovelAI's single text box can't offer.

## Tag ordering

Earlier tags weigh more. `assembly` sorts grounded tags by `post_count` descending —
strong, well-known tags first. Quality tags precede everything.

Prompt weighting (`1.3::tag ::`) is the escape hatch when grounding picks a real tag
that's too weak. Expose it in the editor; don't try to infer it — `/translate`
keeps weights the user typed and never lets the LLM invent one.

## Weighting

Every prompt surface — the prompt, Undesired Content, each character box —
is parsed by `core/internal/prompt` before anything reads it, and each
surface starts at weight 1.0 (nothing carries between boxes). The dialect is
NovelAI's (reports/NovelAI prompt weighting on SDXL.md):

| Syntax | Meaning |
|---|---|
| `{tag}` / `[tag]` | ×1.05 / ÷1.05 per bracket (`{{tag}}` = 1.1025). `[x]` is NovelAI's ÷1.05, **not** A1111's ÷1.1 |
| `1.3::tag ::` | weight 1.3 until the bare `::` (keep the space before the closing `::`, or a tag ending in digits becomes the weight: `year 2000::` is rejected — write `year 2000 ::`) |
| `(tag:1.3)` | SD/ComfyUI syntax, auto-converted to `1.3::tag ::` with a notice. Nested or ambiguous SD groups (`((x):1.2)`) are rejected |
| `(tag)` | **literal** parentheses, as on NovelAI — Danbooru names like `castorice (honkai: star rail)` rely on it. Emitted escaped (`\(`/`\)`), so ComfyUI no longer reads the qualifier as a ×1.1 group |
| `-1::hat ::` | negative weight in a prompt: moved to Undesired Content at weight 1 (notice). In UC itself a negative weight is rejected — it would steer *toward* the tag |
| `0::x ::` | dropped with a notice (SDXL has no meaning for zero) |
| `\|`, `<lora:…>`, `embedding:`, `:::` | rejected (use character boxes / the LoRA picker; embeddings aren't supported) |

Brackets are **scoped** in pulsar: `{`/`[` must close with their own `}`/`]`,
and the closer restores the weight in effect before the opener. Unclosed,
stray and mismatched brackets are errors with an exact location (UTF-16
offsets + tag index — orbit marks the chip/span). `w::` overwrites the weight
inside the current scope and a bare `::` resets it to 1 there; neither closes
a bracket. With balanced input this equals NovelAI's running-weight math
except where a numeric `w::` sits inside brackets: NovelAI's `{1.8::x}, y`
leaks 1.8/1.05 = 1.714 onto `y`; pulsar's `}` restores 1.0. An unclosed `w::`
section at the end of a prompt is legal and runs to the end (notice).

The parsed NovelAI-unit weight is then mapped through the lineage's
calibration curve (docs/tuning.md#prompt-weights) and emitted as one flat
`(text:w)` group per run — never nested, never spanning a comma.
`POST /prompt/parse` exposes the parser to orbit for live validation.

## Quality tags are lineage-specific

| Lineage | Tags |
|---|---|
| Illustrious / NoobAI | `masterpiece, best quality, very aesthetic, absurdres` |
| NoobAI aesthetic | `very awa` |
| Pony | `score_9, score_8_up, …` |

Using the wrong family's quality tags is a common, silent accuracy killer. The
quality-tags toggle must be lineage-aware. (We don't ship Pony — its `score_X`
system doesn't align with a booru-tag autocomplete — but the principle holds.)

## Pose subset

`pose_relevant` marks the closed vocabulary the `/pose` page uses. Roughly:

- **Body:** standing, sitting, kneeling, squatting, crouching, lying, on back,
  on stomach, on side, all fours, jumping, running, walking, leaning forward,
  bent over, arched back, crossed legs, arms up, arms behind head, arms behind back,
  crossed arms, hand on hip, outstretched arm, reaching, looking back, head tilt,
  twisted torso, contrapposto, dynamic pose
- **Head & gaze / arms & hands / legs & feet:** the limb-level detail a full
  line sketch needs — looking directions, hand states (clenched, open, pointing,
  spread fingers), leg positions (leg up, standing on one leg, tiptoes, seiza)
- **Action & airborne:** fighting stance, punching, kicking, midair, falling
- **Holding & weaponry:** holding X (sword, gun, staff, polearm, bow, shield,
  umbrella, book, phone…), the object tags themselves (katana, spear, scythe,
  rifle…), and weapon handling (dual wielding, unsheathing, aiming, sheathed)
- **Camera:** from above, from below, from side, from behind, dutch angle,
  foreshortening, perspective
- **Framing:** full body, upper body, cowboy shot, portrait
- **Backdrop:** simple/white/grey/transparent background, scenery — the /pose
  background toggle grounds through these

**Verify every one against real `post_count` before trusting it.** The list above is
a seed, not ground truth — the DB is ground truth. Hand-review the top ~300.
The full seed lives in `internal/tags/pose.go`; after extending it, re-mark an
already-seeded DB with `seed-tags -pose-only` (DB-only, no Danbooru traffic) and
review the `PoseMissing` log line for seeds that didn't land.

Grounding against the pose subset (~200 candidates vs ~50k) is measurably more
accurate than grounding against everything. That's why `/pose` passes
`subset=pose`.

## UI contract

`GET /tags?q=&subset=` returns `{canonical, category, count}`. Category drives pill
colour in the editor. Rank by `post_count`. Cache hot prefixes in memory.

## Anchors — post-cutoff characters render by description

A character tag whose `danbooru_created_at` post-dates the lineage's knowledge
cutoff names someone the model never saw: autocomplete validates honestly
(`newer_than_model`), the render improvises a stranger. But the model knows
every design *component* — only the name→appearance mapping is missing.

`autoAnchor` (api, generation-time) closes most of that gap automatically: for
each post-cutoff character tag in the prompt it samples the character's solo
posts on Danbooru, aggregates general-tag frequency, keeps design vocabulary
(appearance segments only — never pose/composition/meta) appearing in ≥35% of
posts, and splices up to 12 anchors after the tag. Cached per tag; every
failure degrades to the unmodified prompt.

Anchors capture what's *invariant* about a character — hair and face survive
the frequency floor, outfit variants usually don't. The precision path for
exact outfits stays a character LoRA or a reference image.

Tags seeded before the freshness migration have NULL `danbooru_created_at`;
the anchor path lazily backfills the date through Live (`EnsureCreatedAt`),
which also heals the autocomplete flag — one generation request repairs the
tag for everyone.

### Auto-reference — the escalation above anchors

Anchors carry the palette, not the person (verified across four franchises:
trait-checklist renders still read as the wrong character). When a
post-cutoff character is detected and the request brought no reference or
image of its own, the API escalates: fetch the character's **consensus
exemplar** from Danbooru and attach it as a character-mode Precise Reference
(strength 0.55 — identity assist; the prompt keeps the scene).

Consensus exemplar = the top-scored solo post whose OWN tags agree with the
character's dominant hair-design tags (≥50% of sampled posts). Score alone
picks popular alt-outfit or mistagged art — a pink-haired outlier for a
black-haired character taught us that; requiring design agreement keeps the
pick canonical. Gates at the source: rating g only (in the query and
re-checked), png/jpeg only, single character tag, forbidden tags excluded.
Default outfit beats costume as a tier, not a score penalty: any post
tagged `*_costume`, `cosplay`, `halloween` or `christmas` is a fallback
only. Both rules come from live Castorice: the sample was 32/50
sensitive-rated, and a score-142 sensitive `ghost_costume` post beat
every canonical one. The reference conditions outfit, body and pose, not
just the face, and nobody chose it, so it must be the plainest canonical
image available. The bytes then flow through the
SAME ingest path as user uploads — normalize, then the compliance hook,
fail closed. `auto_reference: false` disables per request. Blob ids cache
per character for the process lifetime.

Artifact note (tuned live, three-variant sweep): the character-mode adapter
bleeds its exemplar's texture into under-specified regions — a sparse scene
("blue sky") renders streaky. **Dense scene vocabulary is the fix** (e.g.
`garden, flower field, trees, fountain, stone path, clouds`): the prompt
outvotes the adapter wherever it has something concrete to say. The hires
refine pass then absorbs the remainder into clean rendering. Auto-reference
defaults (strength 0.55, fidelity 0.5) stay as shipped.
