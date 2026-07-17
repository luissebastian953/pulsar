# Compliance

**Read this before anything ships to real users.**

This is not a policy checkbox bolted onto a generation product. It is the layer that
determines whether pulsar is a lawful product or a criminal liability, and it is the
hardest engineering in the repo.

## Why this is not optional

Booru datasets contain sexualized depictions of characters that read as minors.
Anime models trained on them reproduce that content *readily*, because it is in the
training distribution — not as an edge case, but as something the model will produce
without being asked to.

That content is illegal in essentially every jurisdiction. It is what gets operators
prosecuted and platforms seized, not merely banned.

So the engineering reality is the inverse of the intuitive framing: the hard work in
adult anime generation is not removing safety filters to "unlock NSFW." It's
**adding rigorous apparent-minor exclusion** to both training data and outputs.
Stripping filtering wholesale doesn't produce a permissive adult product; it
produces one that reliably generates illegal content and ends the project.

Every legitimate operator — NovelAI included — runs classifiers for exactly this and
treats it as non-negotiable. So do we.

## Scope

| Surface | Requirement |
|---|---|
| Generated outputs | Classify every image before delivery. No exceptions. |
| User uploads | Classify on ingest (img2img, references, inpaint bases). |
| Training data (phase 5) | Filter the corpus **before** a single training step. |
| Manga (phase 7) | Every panel classified; story LLM needs its own guardrails — narrative context makes prompts less obviously flaggable. |
| Access | Age-gate. Adults-only. |
| Audit | Moderation logging. |

## Hooks

Two call sites exist in `internal/compliance/` from the first commit. They may be
stubs before phase 4 — the call sites are not negotiable.

```
POST /generate
  └─► compliance.CheckPrompt(spec)      ─► reject before enqueue
      ...generation...
  └─► compliance.CheckOutput(image)     ─► state = blocked, never delivered

POST /images
  └─► compliance.CheckUpload(image)     ─► reject on ingest
```

**`blocked` is a first-class job state** — `queued → running → done | failed |
blocked`. It exists in the schema from day one. Retrofitting it after the happy path
is built is exactly where corners get cut, because by then the delivery path assumes
success.

An output that fails classification is never written to public storage and never
returned. Log it, block it, count it.

## Design constraints

- **Fail closed.** Classifier unavailable → block, don't pass. A generation that
  can't be checked is a generation that doesn't ship.
- **Post-generation, pre-delivery.** Not post-delivery, not client-side. The image
  must not leave the server unclassified.
- **The gate is in the main line, not beside it.** Phases 1–3 are low-stakes because
  nothing is public. The moment real users can generate, this must already run.
- **Self-host the classifier.** It sees every image; don't ship them to a third party.

## Current implementation

`sentinel/` is a self-hosted WD-tagger sidecar (wd-eva02-large-tagger-v3,
Danbooru-trained — anime-native apparent-age assessment, sha-pinned, fetched by
`make run-sentinel`). It does pure inference; **policy lives in
`core/internal/compliance`**:

- The `FORBIDDEN_TAG_UC` list drives all three mechanisms: the negative prompt,
  positive-prompt rejection (`CheckPrompt`), and output blocking (`CheckOutput`
  / `CheckUpload`).
- Two rules block an output, both env-tunable and code-clamped (never
  disableable): any forbidden tag ≥ `SENTINEL_BLOCK_THRESHOLD` (default 0.35),
  or the **sum** of forbidden-tag scores ≥ `SENTINEL_SUM_THRESHOLD` (default
  0.05). The sum rule is load-bearing: Danbooru purges minor content, so the
  tagger's age tags score faintly even on clear cases — the first real
  child-like output scored `child 0.054 + loli 0.033 + aged_down 0.006 =
  0.093` while clean adult images sit ≤ 0.002. Explicit-leaning images
  (`explicit + questionable ≥ 0.5`) halve both gates: false negatives are
  catastrophic, false positives merely annoying.
- Sentinel unreachable → checker errors → callers block/reject (fail closed,
  already wired in `worker` and `api`).
- `SENTINEL_URL` unset runs the permissive stub with a loud boot warning — a
  dev convenience that must never survive into public exposure.

Accuracy tuning against real generations belongs in `eval/`; the threshold is
a start, not a conclusion.

## Known risks

1. **Classifier accuracy on anime is the highest-risk unknown in the project**, and
   it's on the critical path to launch. Anime style breaks assumptions baked into
   classifiers trained on photographs — apparent age is stylistic, not
   morphological. Budget iteration time; do not assume an off-the-shelf model works.
2. **False negatives are catastrophic; false positives are merely annoying.** Tune
   accordingly. A frustrated user is a survivable outcome.
3. **`/pose` nudity is an open product decision.** Anatomical figure references are a
   legitimate artist need, but permitting them puts `/pose` inside this gate rather
   than beside it — which determines whether `/pose` can ship before the gate closes.
   Decide deliberately, not by default.

## Phase

Phase 4. Development can overlap phase 3, but it **must fully close before any
public access**. It is the longest-lead risk in the roadmap, which is a second
reason not to leave it for last.
