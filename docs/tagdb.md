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

Prompt weighting (`(tag:1.3)`) is the escape hatch when grounding picks a real tag
that's too weak. Expose it in the editor; don't try to infer it.

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
- **Camera:** from above, from below, from side, from behind, dutch angle,
  foreshortening, perspective
- **Framing:** full body, upper body, cowboy shot, portrait

**Verify every one against real `post_count` before trusting it.** The list above is
a seed, not ground truth — the DB is ground truth. Hand-review the top ~300.

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
pick canonical. Gates at the source: rating g/s, png/jpeg only, single
character tag, forbidden tags excluded. The bytes then flow through the
SAME ingest path as user uploads — normalize, then the compliance hook,
fail closed. `auto_reference: false` disables per request. Blob ids cache
per character for the process lifetime.
