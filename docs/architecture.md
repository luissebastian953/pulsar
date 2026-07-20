# Architecture

## Request → image

```
1. validate + bounds-check (steps/res/batch caps)
2. compliance: prompt pre-check ─────────────► may reject
3. translate: NL → grounded tags   (4 boxes: prompt, UC, char prompt, char UC)
4. assemble: quality tags + tags + defaults + settings → PromptSpec
5. build: select template(s) + patch by node ID → API-format JSON
6. enqueue: Postgres FOR UPDATE SKIP LOCKED
7. route: least-busy ComfyUI process (one per GPU)
8. execute: POST /prompt → WS progress → GET /view
9. compliance: output classify ──────────────► may set state=blocked
10. store: object storage + resolved params row
11. stream: SSE to client
```

Steps 3–5 are pure functions. Everything before step 6 is CPU-only and testable
without a GPU — which is most of the interesting logic.

## Endpoints

```
POST   /generate            → validate, enqueue, 202 {job_id}
GET    /jobs/:id            → status + resolved params
GET    /jobs/:id/stream     → SSE: queue pos → step progress → done|blocked
POST   /images              → upload (img2img / references / inpaint base)
GET    /tags?q=&subset=     → autocomplete; subset=all|pose
POST   /translate           → NL → grounded tags (preview before generate)
GET    /history             → user's generations
POST   /pose/use/:id        → skeleton → ControlNet input for /create
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
