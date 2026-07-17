# CLAUDE.md

Instructions for Claude Code working in this repo.

## What pulsar is

Self-hosted anime image generation (NovelAI-style), built on open weights.
Image generation only — no text/storytelling. Three surfaces: `/create` (art),
`/pose` (artist pose references), `/manga` (phase 7).

## Who you're working with

Backend/infrastructure engineer. Fluent in Go, Postgres, Nuxt, LLM APIs. Be
concrete and technical; skip beginner framing. Prefer committing to one path over
listing alternatives. Lead with the answer.

## Stack — assume this unless told otherwise

| Layer | Choice |
|---|---|
| Generation | ComfyUI in API mode |
| Base model | Illustrious XL (eps) → NoobAI-XL v-pred planned |
| Orchestration | Go |
| Frontend | Nuxt 3 |
| Data | Postgres |
| References | IP-Adapter (not FaceID — see below) |

Don't re-litigate these in every conversation. They're settled.

## Non-negotiable constraints

**Child safety gates launch.** Booru datasets contain sexualized depictions of
apparent minors, and anime models reproduce them readily because they're in the
training distribution — a neutral `1girl` prompt has produced one in this repo.
Filtering this from training data *and* generated outputs is mandatory — legally
and ethically. Never design around it, never treat it as optional polish, never
remove it to "unlock NSFW." Two mechanisms exist today: the `FORBIDDEN_TAG_UC`
age-suppression negative (value in env per the no-hardcode rule; its **presence
is enforced in code** — `cmd/server` refuses to boot without it and
`assembly.Build` refuses per request, pinned by
`TestForbiddenTagUCIsUnconditional`; it is never user-toggleable and holds first
position in every negative) and the compliance hook call sites. The negative is
distribution steering; the phase-4 output classifier is the gate. Any change
touching prompt assembly, training data, or output delivery must preserve both,
and the fail-closed presence checks must never be weakened to warnings. See
`docs/compliance.md`.

**Respect provider terms.** Don't design around a cloud provider's AUP. Pick infra
that permits legal adult content, or self-host.

**Licences.** Verify commercial-use terms before building on any base model.

## The five things that trip people up here

1. **API-format JSON ≠ editor JSON.** ComfyUI's UI export has two formats. We use
   API format only. See `.claude/skills/comfy-workflow/`.
2. **Never rewire graphs in code.** Patch values by node ID against a versioned
   template. Node IDs live in `internal/workflow/nodes.go` and break when workflows
   are edited.
3. **Settings don't transfer between model lineages.** Illustrious (eps) and NoobAI
   (v-pred) need different CFG, samplers, and rescale. See `docs/tuning.md`.
4. **`min_post_count` is the accuracy lever, not CFG.** A tag the model barely saw
   does nothing. Grounding is where adherence lives. See `docs/tagdb.md`.
5. **Don't use IP-Adapter FaceID for anime.** It depends on InsightFace, trained on
   photographic faces. Use IP-Adapter Plus. See `docs/precise-reference.md`.

## Architecture invariants

Preserve these; they're load-bearing.

- **`assembly` is pure.** No I/O, no GPU, no network. It takes a request, returns a
  `PromptSpec` struct — never a prompt string. This is why translation, quality
  tags, and workflow building stay independently testable, and why swapping
  Illustrious→NoobAI doesn't touch prompt logic. Test it exhaustively; it's where
  "why didn't my prompt work" bugs live.
- **`blocked` is a first-class job state.** `queued → running → done | failed |
  blocked`. It exists from the first commit. Retrofitting it after the happy path is
  where corners get cut.
- **Compliance hooks are call sites, not features.** Two exist: pre-enqueue (prompt)
  and post-generation-pre-delivery (output). They may be stubs before phase 4, but
  the call sites stay.
- **The masking subsystem is shared.** Character positions (`regional.json`) and
  per-character references (`ipadapter.json`) derive from the same masks. One
  implementation. Don't build it twice.
- **Image ingest is one path.** img2img, inpaint, and Precise Reference all use it:
  upload → validate/normalize (size, format, strip EXIF) → compliance hook → forward
  to the ComfyUI process that will run the job → patch `LoadImage`.
- **`nltag` grounds against the tag DB, never the LLM's imagination.** The LLM
  decomposes; the tag DB decides vocabulary. Dropping an unmappable concept is
  correct behaviour, not a failure. The cascade's last resort before dropping is
  a live Danbooru check (`tags.Live`) that writes hits through — floor-gated, so
  the DB stays the intersection of "exists" and "the model learned it", and
  never for the pose subset.

## Concurrency model

One ComfyUI process runs one workflow at a time. Real concurrency is one process
per GPU, with core routing to the least-busy queue. Don't assume ComfyUI
parallelises; it doesn't.

## Skills in this repo

Read these before the corresponding work:

| Skill | For |
|---|---|
| `.claude/skills/comfy-workflow/` | Adding or editing ComfyUI workflow templates |
| `.claude/skills/tag-grounding/` | Changing NL→tag, autocomplete, or the tag DB |
| `.claude/skills/core-job/` | Adding an endpoint or job type |

## Frontend

`orbit` is Nuxt 3 + Reka UI (headless) + Tailwind v4 + Lucide + VueUse, linted with
`@nuxt/eslint`. No styled component library — we have our own tokens.

Design tokens live in `@theme` in `app/assets/css/main.css` and are the single source
of truth. **No arbitrary Tailwind values in components** (`bg-[#6AB7FF]`) — if a
colour isn't a token, it isn't in the design. Prefer VueUse over lodash; if lodash is
genuinely needed, import from `lodash-es`, never `lodash` (bundle size — this ships
to mobile). Targets SPA (`ssr:false`) because Electron and
Capacitor shells can't run SSR. Auth is **bearer tokens, not cookie sessions** —
cookies break on `capacitor://` and `file://` origins. This is a `core` constraint,
not just an `orbit` one. See `docs/frontend.md`.

## Conventions

- Go: standard layout, `internal/` for everything not imported externally.
- Tests: `assembly`, `nltag`, and `workflow` are pure — no excuse for thin coverage.
- Workflow JSON: version by content hash, never edit in place without bumping.
- Migrations: forward-only, numbered.
- Don't add dependencies to `assembly`. If it needs I/O, the design is wrong.

## When unsure

The open-model landscape moves fast. For model versions, tooling, GPU pricing, or
NovelAI's current features — search rather than answering from memory. Training-data
knowledge on this topic goes stale in months.
