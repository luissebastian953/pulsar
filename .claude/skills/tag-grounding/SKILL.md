---
name: tag-grounding
description: How pulsar's natural-language-to-Danbooru-tag pipeline and tag database work. Use this skill whenever the work touches internal/nltag/, internal/tags/, the tags or tag_aliases tables, autocomplete, prompt assembly, the pose tag subset, or min_post_count. Also use it whenever someone reports weak prompt adherence, a prompt "not taking effect", or wants to improve generation accuracy — the cause is usually grounding, not sampler settings, and reaching for CFG first is the standard misdiagnosis here.
---

# Tag grounding in pulsar

This is the product's edge. Everyone building on Illustrious has the same model.
Almost nobody has a model-aware tag vocabulary in front of it.

## The core idea

An anime model is accurate when prompted with tags it saw **often** in training.
Tags are the model's vocabulary, not magic words. A tag that's common on Danbooru
but rare in the model's training set produces nothing.

So the tag DB is **the intersection of "exists on Danbooru" and "the model learned
it"** — not a copy of Danbooru. Both autocomplete and NL grounding query that
intersection.

## Two stages, and why they're separate

```
sentence ──► [1] DECOMPOSE (LLM)  ──► concepts[]
                                        │
         ──► [2] GROUND (tag DB)  ──► canonical tags[]  +  dropped[]
```

The LLM is good at decomposition and **will happily invent tags that don't exist**.
The tag DB is ground truth that keeps output on-distribution.

Never let the LLM emit tags directly. The LLM decomposes; the tag DB decides
vocabulary. This division holds at every level — including the manga script LLM
(phase 7), which emits natural-language `action` fields, not tags, for exactly this
reason.

## The grounding cascade

```
concept → normalise
        → exact / alias hit?         → canonical
        → fuzzy (WRatio >= ~88)?     → canonical      (typos, wording)
        → embedding NN (>= ~0.55)?   → canonical      (paraphrases — not built yet)
        → live Danbooru hit?         → canonical      (tags newer than the DB)
        → None                       → DROP
```

Two cascade details that live here, not in code comments:

- **Squeezed-form fallback.** "1 girl" normalises to `1_girl` but the canonical
  tag is `1girl` — a pattern (`1girl`, `2girls`, …) trigram ranking is bad at,
  so the exact stage retries with separators squeezed out before falling
  through to fuzzy.
- **Negatives are chunked like prompts.** CLIP truncates past 77 tokens per
  chunk; the front of the first chunk is never truncated, which is why the
  forbidden-tag terms (FORBIDDEN_TAG_UC) hold first position in every
  negative — order is a safety property, not style.

**Returning `None` is correct behaviour, not a failure.** A concept with no
corresponding tag should be dropped, never force-matched. Force-matching is how you
get prompts that quietly produce garbage — the user sees their words accepted and
the output ignore them.

If you find yourself lowering thresholds to "reduce drops," stop. Drops are
information.

## The live stage (`tags.Live`)

The DB is a snapshot; Danbooru is not. On a DB miss, `tags.Live` asks Danbooru
directly (exact name, then through active aliases) and **writes hits through**
— tag, category, count, `danbooru_created_at`, and the alias followed. The
vocabulary self-heals where users push its edges; a character who debuted
yesterday resolves today, no reseed. This is the freshness edge over NovelAI's
stale suggestions.

Three rules keep it honest:

- **Floor-gated.** Below `MIN_POST_COUNT` (the same floor as seeding), a found
  tag is *reported* to an explicit `/tags/lookup` but never stored and never
  grounds. The DB stays the intersection, not a Danbooru mirror.
- **Never for the pose subset.** The closed vocabulary is a feature.
- **`newer_than_model` is the other half of the intersection.** "Exists on
  Danbooru" ≠ "the model learned it": `danbooru_created_at` is compared to
  `assembly.KnowledgeCutoff(lineage)` and surfaced through the API — a 2026
  character validates honestly and renders poorly until a newer lineage. The
  flag overstates newness for renames/splits (a new tag name, old posts);
  treat it as advisory.

Outages degrade to the pre-live behaviour (drop, logged) — translation never
fails because Danbooru is down. Lookups are serialized with a negative cache;
they only fire on misses, so Danbooru sees genuine novelty, not keystrokes.

## Surface the drops

`translate()` returns `(prompt, grounded, dropped)`. The UI shows dropped concepts —
"couldn't map: *ethereal vibes*".

This is not a debug affordance. It's the thing that makes tag translation legible
instead of a black box, and it's a UX advantage NovelAI's single text box structurally
cannot offer. Don't quietly swallow drops.

## `min_post_count` — the accuracy lever

The highest-leverage knob in the product.

- **Raise** → only strong, well-learned tags survive → high adherence, less range
- **Lower** → rarer tags accepted → more range, weaker adherence, more noise

**Tune it against real generations via `eval/`, never by intuition.** It moves
prompt adherence more than any CFG or sampler change.

When someone asks how to improve accuracy, this — not CFG — is the first place to
look.

## Tag ordering

Earlier tags weigh more. `assembly` sorts grounded tags by `post_count` descending.
Quality tags precede everything.

Don't infer prompt weighting `(tag:1.3)` — expose it in the editor as the escape
hatch for real-but-weak tags.

## One service, four boxes, two pages

`nltag` serves base prompt, base UC, character prompt, and character UC — on both
`/create` and `/pose`. Negatives run the identical pipeline, routed to the negative
prompt.

It is a **shared subsystem, not a per-box feature.** If you're adding
box-specific logic inside `nltag`, the design is drifting.

## The pose subset

`/pose` passes `subset=pose`, grounding against `pose_relevant` tags (~200
candidates vs ~50k). Fewer candidates measurably improves accuracy — the closed
vocabulary is a feature.

`pose_relevant` is one boolean on `tags`, not a second table. The pose autocomplete
is one `WHERE` clause off the art one.

**Verify pose tags against real `post_count`.** Curated lists (including any in the
docs) are seeds; the DB is ground truth.

## Quality tags are lineage-specific

Illustrious/NoobAI take `masterpiece, best quality, very aesthetic, absurdres`;
NoobAI adds `very awa`. Pony's `score_X` system is a different family entirely.

Using the wrong family's quality tags is a silent accuracy killer. The toggle must
be lineage-aware.

## Prototype

`nl_to_danbooru.py` (project knowledge) is the reference implementation of the
two-stage pipeline. It's throwaway — the production version is `internal/nltag/`
in Go against Postgres — but the cascade logic and the `dropped` contract carry over
verbatim.

## Related

- `docs/tagdb.md` — schema, ingest, cascade thresholds, pose subset
- `docs/tuning.md` — why accuracy lives here rather than in sampler settings
