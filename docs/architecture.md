# Architecture

## Request → image

```
0. (client) translate: NL → grounded tags via POST /translate, weights kept
1. expand the request's prompt chunks (`!macro:Name!`), then parse every
   prompt surface (prompt, UC, each character) ──────────► 400 + span
2. validate + bounds-check (steps/res/batch caps); style lookup
3. autoAnchor on parsed tags (post-cutoff characters)
4. assemble: calibrate weights, relocate negative weights to UC, dedupe the
   negative (safety first), emit flat escaped strings → Spec (+ structured tags)
5. compliance: prompt check on the emitted strings ─────► may reject (422/503)
6. build: select template(s) + patch by node ID → API-format JSON
7. enqueue: Postgres FOR UPDATE SKIP LOCKED
8. route: least-busy ComfyUI process (one per GPU)
9. execute: POST /prompt → WS progress → GET /view
10. strip: pngmeta allow-list on every output ─► malformed PNG = state=failed
11. compliance: output classify (THE gate) ───► may set state=blocked
12. store: object storage + resolved params row (the bytes step 11 classified)
13. stream: SSE to client
14. serve: GET /images/:id strips again on the way out (legacy blobs)
```

### Prompt chunks

A chunk is a named snippet a prompt refers to as `!macro:Name!` (NovelAI's
typeable form; zero tag names contain `!macro`). Core stores none: `/generate`,
`/prompt/parse` and `/translate` take `chunks: [{name, content}]` — the ones
the text references — and `prompt.Expand` splices them in before the scanner
runs, on every user surface (prompt, negative, characters; pose included),
never on a config or safety surface.

- **Origin map.** Every expanded rune remembers the reference it came from,
  so an error, notice or tag span inside a chunk is located on the token the
  user typed (its UTF-16 span and its piece index); the message names the
  chunk. `MaxBytes`/`MaxTags` apply after expansion (`too_long` on the field).
- **Limits** (400 at decode): names trimmed, case-sensitive, unique, ≤ 64
  chars, none of `! , | { } [ ] ::` or a line break; ≤ 64 chunks per request,
  ≤ 4 KiB each. Nesting via `!macro:` inside content, depth ≤ 8.
- **Located 400 codes**: `unknown_chunk`, `chunk_cycle`, `chunk_depth`,
  `chunk_syntax` (a `!macro:` without a name or closing `!`). A token in
  `QUALITY_TAGS`, `DEFAULT_UC`, a style row or `FORBIDDEN_TAG_UC` is
  `chunk_in_config`: boot is refused.
- **Compliance** needs no special case: expanded text becomes ordinary parsed
  tags, and `CheckPrompt` re-parses the emitted strings exactly as for typed
  text (`TestChunkTextInputsAreChecked`, `TestWatchedTagInChunkRejected`).
- **History.** The Spec stores the expanded tags — the authoritative record —
  and each tag that came whole out of a reference carries
  `chunk: {name, hash}` (hash = first 16 hex of the sha256 of the chunk's
  resolved text, nested chunks included). Chunk definitions are never stored.
- **`/prompt/parse`** returns `chunks: [{name, hash, start, end, tag, text}]`
  for every resolved reference (also when the text has errors) and
  `marks: [{start, end, kind: up|down|op, w}]` locating the typed weighting
  in NovelAI units; text that came out of a chunk is never marked.
- **`/translate`** never sends chunk text to the LLM. Chunk tags are appended
  after the grounded tags — as the reference itself where that is exact (so
  `/generate` keeps provenance), written out with weights otherwise.

Steps 1 and 4 are pure functions (`prompt`, `assembly`). Everything before step 7 is CPU-only and testable
without a GPU — which is most of the interesting logic.

## Endpoints

```
POST   /generate            → parse, validate, enqueue, 202 {job_id, notices}; kind=txt2img|img2img|inpaint|pose, style=<slug>;
                               400 {error, code, field, start, end, tag, hint} on a prompt parse error
GET    /jobs/:id            → status + resolved params
GET    /jobs/:id/stream     → SSE: queue pos → step progress → done|blocked
GET    /images/:id[?meta=1] → stored output (…-lineart / …-skeleton for pose roles), stripped, + the XMP AI-generated mark;
                               ?meta=1 (downloads) adds the A1111 `parameters` and `pulsar` JSON chunks (docs/compliance.md#output-metadata)
POST   /uploads             → image ingest (img2img / references / inpaint base + mask); PNG, JPEG or still WebP
GET    /tags?q=&subset=     → autocomplete; subset=all|pose
GET    /tags/lookup?q=      → one tag, DB then live Danbooru
POST   /translate           → NL → grounded tags with weights + `prompt` in NovelAI syntax (parsed first; the LLM never sees operators);
                               {text, subset, chunks}: chunk text skips the LLM
POST   /prompt/parse        → {text, surface, lineage, chunks, clean} → tags/runs/weights, errors[], notices[],
                               chunks[] (resolved references) and marks[] (typed weighting) (live validation; syntax only);
                               clean=true (import): drops <lora:…>, embedding:…, |, ||…|| with notices, returns `cleaned`
GET    /styles              → style presets per lineage (docs/tuning.md#style-presets)
GET    /loras, /checkpoints → installed model files
GET    /history             → user's generations; DELETE /history[/:id]
GET    /healthz
```

## Why async

SDXL generation is 20–40s. It cannot block an HTTP handler, and one GPU pool serves
many users. `POST /generate` returns a `job_id` immediately (202); the client
subscribes to `/jobs/:id/stream`.

Job states: `queued → running → done | failed | blocked`

Reconnection must work — a client can re-attach to an in-flight job. Don't hold
state in the SSE handler.

## Queue

Postgres, `SELECT … FOR UPDATE SKIP LOCKED`. This is enough. It gives durable jobs,
queue position, and history in the database you already have.

Don't reach for Redis/NATS until multi-GPU scheduling gets genuinely complex. The
cost of the upgrade later is lower than the cost of running a broker now.

## Concurrency

**One ComfyUI process executes one workflow at a time.** There is no built-in
concurrency, autoscaling, VRAM pooling, or multi-tenancy. Thirty requests means the
thirtieth waits behind twenty-nine.

The pattern:

```
core ──┬─► comfy:8188  (GPU 0)   queue depth 2
          ├─► comfy:8189  (GPU 1)   queue depth 0  ← route here
          └─► comfy:8190  (GPU 2)   queue depth 5
```

One process per GPU. Core polls `/queue` or watches the WebSocket and routes
each job to the shallowest queue. Implement this from day one even on a single GPU —
scaling later becomes config, not rework.

Operational notes:
- Pre-warm each process on boot (dummy prompt loading checkpoint + VAE + common
  LoRAs) — kills the 6–12s first-request cliff.
- `--highvram` on a dedicated GPU pins the checkpoint so you never pay reload.
- Watch for stuck prompts (no progress in N seconds) and restart hung processes.
  ComfyUI will die. Treat it as unreliable.
- Bind ComfyUI to localhost. Never expose 8188. Core is the only client.
- Whitelist allowed `class_type` values before forwarding to `/prompt` if user input
  can influence workflows.

## Workflow templates

Stored as API-format JSON in `workflows/`, versioned by content hash, patched by
node ID.

| Template / patch | Purpose |
|---|---|
| `txt2img.json` | Art page base |
| `img2img.json` | `LoadImage` → `VAEEncode`, `denoise` |
| `inpaint.json` | + `SetLatentNoiseMask` |
| `patches/vpred.json` | NoobAI v-pred wrap (`ModelSamplingDiscrete` + `RescaleCFG`) |
| `patches/ipadapter.json` | Precise Reference |
| `patches/regional2.json` / `regional3.json` | Multi-character masked conditioning |
| `patches/lora.json` | Style / character LoRA |
| `patches/hires.json` | Pixel-space upscale + 2nd pass @ denoise ~0.4 |
| `pose.json` | Generate → `lineart_anime` + `openpose` extract |

**Templates compose.** A request may be `txt2img + ipadapter + regional + vpred +
hires`. Select a base, apply feature patches. v-pred ships as a *patch* (not a
`txt2img_vpred.json` base) so it composes onto any base and after any model-chain
fragment — see `docs/tuning.md`. Do not maintain the cartesian product as separate
files — that way lies `txt2img_vpred_ipadapter_regional_hires.json`.

See `.claude/skills/comfy-workflow/` before touching these.

## Shared subsystems

- **`assembly`** — pure. Request → `PromptSpec`. No I/O.
- **`nltag`** — two-stage decompose → ground. Serves all four prompt boxes on both
  pages. See `docs/tagdb.md`.
- **Masking** — character positions AND per-character references derive from the same
  masks. Build once, use for `regional.json` and `ipadapter.json`.
- **Image ingest** — one path for img2img, inpaint, and Precise Reference.
  Per-process `input/` dir unless storage is shared: forward the upload to the
  process that will run the job.
- **`compliance`** — two hooks. See `docs/compliance.md`.
- **`pngmeta`** — pure, stdlib-only PNG chunk allow-list. Runs on every output
  before `CheckOutput` and again in `GET /images/:id`; anything pulsar ever
  writes into a PNG goes through it too. See `docs/compliance.md#output-metadata`.

## Storage

Images to object storage (S3-compatible). One DB row per generation with the
**resolved** params (post-translation) — this is what makes "copy prompt" and
reproducibility work. Seed must round-trip: same seed + same resolved params =
same image.

Note ancestral samplers (Euler a) never fully converge and keep injecting noise;
non-ancestral (DPM++ 2M) converge. This matters for the reproducibility promise.

## Performance

Order of payoff:

1. Steps 28→20 — ~30%, barely visible on SDXL
2. SageAttention (`--use-sage-attention`) — ~1.3–2× on attention blocks
3. Resident model (`--highvram`) + pre-warm
4. `torch.compile` — 10–30%, recompiles on shape change (needs pinned buckets)
5. Batching — ~10–20% only; SDXL is compute-bound, not 4× for batch-of-4
6. TensorRT — 40%+, but ~1min cold start and breaks LoRA hot-swap. Last resort.

Sanity check: SDXL 1024×1024, 25 steps on a 4090 ≈ 3–4s. Materially off means
misconfigured, not slow.

**Accuracy lives in the backend, not the GPU.** `min_post_count` and the grounding
cascade move prompt adherence more than any sampler change.
