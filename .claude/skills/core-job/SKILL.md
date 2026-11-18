---
name: core-job
description: How to add or modify an endpoint, job type, or generation mode in pulsar's Go service (core). Use this skill whenever the work touches internal/job/, internal/assembly/, internal/comfy/, the job queue, job states, SSE progress streaming, image upload/ingest, or the request-to-image flow. Also use it before adding any new generation feature end-to-end (img2img, inpaint, hires, manga panels) — there are invariants here about purity, the blocked state, and compliance hooks that are easy to violate and expensive to retrofit.
---

# Adding a job type or endpoint

## The flow every generation follows

```
1. validate + bounds-check (steps/res/batch caps)
2. compliance.CheckPrompt  ─────────────► may reject
3. nltag.Translate         (4 boxes)
4. assembly → PromptSpec
5. workflow: base template + feature patches → API-format JSON
6. enqueue (Postgres FOR UPDATE SKIP LOCKED)
7. route → least-busy ComfyUI process
8. execute: POST /prompt → WS → GET /view
9. pngmeta.Strip (every output)  ───────► malformed PNG = state=failed
10. compliance.CheckOutput  ────────────► may set state=blocked
11. store (object storage + resolved params) — the bytes step 10 saw
12. stream (SSE)
13. GET /images/:id → pngmeta.Strip again, then serve
```

Steps 3–5 are pure. Everything before step 6 is CPU-only and testable without a GPU
— which is most of the interesting logic. Keep it that way.

## Four invariants — violating these is expensive to undo

**1. `assembly` is pure.** No I/O, no network, no GPU. Request in, `PromptSpec` out.

It returns a **struct, not a prompt string.** This is what keeps translation,
quality tags, and workflow building independently testable, and it's why swapping
Illustrious→NoobAI doesn't touch prompt logic. If `assembly` needs a dependency,
the design is wrong — hoist the I/O to the caller.

Test it exhaustively. It's where "why didn't my prompt work" bugs live, and it costs
nothing to test.

**2. `blocked` is a first-class job state.**

```
queued → running → done | failed | blocked
```

Not an error. Not a flag on `failed`. A state, in the schema, from the first commit.
Retrofitting it after the happy path exists is exactly where corners get cut,
because by then the delivery path assumes success.

**3. Compliance hooks are call sites, not features.** Two exist — pre-enqueue and
post-generation-pre-delivery. They may be stubs before phase 4. The call sites stay.
Any new job type wires both. See `docs/compliance.md`.

**4. Store resolved params, not raw input.** The DB row records post-translation
params. That's what makes "copy prompt" and reproducibility work. Seed must
round-trip: same seed + same resolved params = same image.

## Async is not optional

SDXL generation is 20–40s. `POST /generate` returns `job_id` immediately (202); the
client subscribes to `/jobs/:id/stream`.

SSE, not websocket — simpler, and Nuxt handles it natively. **Reconnection must
work**: a client can re-attach to an in-flight job, so don't hold job state in the
SSE handler.

Relay: queue position → step progress (`12/28`) → done + image URL.

## Concurrency

One ComfyUI process runs one workflow at a time. Real concurrency is one process per
GPU with core routing to the shallowest queue (`/queue` or WS). Don't assume
ComfyUI parallelises — it doesn't.

Per-user rate limit and concurrency cap: one user must not hog the pool.

## Composite jobs (manga, phase 7)

A manga is 20 generations over ~7 minutes. That forces:
- **Panel-level sub-jobs** with individual seeds and states → regenerate panel 7, not
  the book
- **Progressive streaming** — emit panels as they finish
- **Two-stage**: cheap `script` job (LLM) → user approval → expensive `render` job

Never spend 20 GPU generations on a story the user hasn't approved.

## Image ingest is one path

img2img, inpaint, and Precise Reference all share it. img2img is a **two-call flow**,
unlike txt2img:

```
1. POST /upload/image (multipart) → ComfyUI writes to input/, returns filename
2. POST /prompt with that filename patched into LoadImage
```

Core responsibilities: accept upload → validate/normalize (size, format, strip
EXIF) → `compliance.CheckUpload` → **forward to the ComfyUI process that will run
the job** (per-process `input/` dir unless storage is shared) → patch `LoadImage`.

That last point bites on multi-GPU. The upload and the job must land on the same
process.

## Multi-output kinds

One generation can deliver several images (pose: source + lineart + skeleton).
The machinery is generic — extend it, don't fork it:

- `workflow.OutputRoles(kind)` maps role → SaveImage node ID. Role `""` is the
  primary: its blob is the bare job ID and it fills `image_path`/`image_url`;
  other roles store as `<jobID>-<role>` and appear in the payload's `images`
  map. `POST /generate` takes `kind` ("txt2img" default).
- The worker loops roles: `comfy.ExecuteAll` returns outputs keyed by node ID,
  **every** image passes `CheckOutput`, and one blocked role blocks the whole
  job — extractions derive from the source, partial delivery makes no sense.
- A new kind = template + `OutputRoles` case + (if multi-output) nothing else:
  worker, blobs, API, and SSE already handle it.

## Output bytes: strip, then check, then store

ComfyUI (and any custom node) writes the patched graph into PNG text chunks —
safety negative, model filenames, upload IDs. Delivered bytes carry only
allow-listed chunks (`internal/pngmeta`, `docs/compliance.md#output-metadata`):

- The worker strips **before** `CheckOutput`, and stores the stripped slice.
  The classifier must see exactly the bytes that get stored — never check one
  slice and `Put` another.
- A strip error fails the job (`MarkFailed`). Fail closed; don't fall back to
  the raw bytes.
- `handleImage` strips again on the way out. Any new route that returns image
  bytes does the same.
- Never embed the job ID, upload IDs or config text in an output; anything
  pulsar adds goes through `pngmeta.Rewrite`.
- Pinned by `TestDeliveredImagesCarryNoGraph`. Fakes that return images must be
  real PNGs (`comfytest.Fake.EmbedGraph` reproduces ComfyUI's chunk).

## Adding a new generation mode — checklist

1. Does it need a new base template or a feature patch? (Patch, usually — see
   `.claude/skills/comfy-workflow/`)
2. Extend `PromptSpec`, keep `assembly` pure
3. Wire both compliance hooks
4. Handle `blocked`
5. Does it need image ingest? Reuse the path; don't build a second one
6. Does it need masks? The masking subsystem is shared between `regional` and
   `ipadapter` — reuse it
7. Store resolved params
8. Test `assembly` and `workflow` without a GPU

## Configuration

All prompt strings (QUALITY_TAGS, DEFAULT_UC, FORBIDDEN_TAG_UC) and deployment
knobs are env vars — `.env.example` is the reference. `internal/dotenv` loads
the repo-root `.env` at boot for native runs; a real environment variable,
even explicitly empty, always wins (compose precedence). FORBIDDEN_TAG_UC is
required: `cmd/server` refuses to boot and `assembly.Build` refuses per
request when it's empty — fail-closed by design (CLAUDE.md non-negotiables),
never weaken these to warnings. Validation whitelists (buckets, samplers,
lineages) and integrity pins (template hashes, node IDs) stay in code on
purpose: env-editing those produces garbage output or graph drift, not
configuration.

## Operational reality

ComfyUI dies. Treat it as unreliable: watch for stuck prompts (no progress in N
seconds), restart hung processes, pre-warm on boot (kills the 6–12s first-request
cliff), bind to localhost only, whitelist `class_type` if user input can influence
workflows.

## Related

- `docs/architecture.md` — full flow, endpoints, queue, concurrency
- `docs/compliance.md` — what the hooks must do
- `.claude/skills/comfy-workflow/` — the template/patch model
