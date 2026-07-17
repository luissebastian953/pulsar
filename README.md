# pulsar

Self-hosted anime image generation. Danbooru-tag precision with natural-language input.

## What this is

A NovelAI-style image generator built on open weights, with two things NovelAI
doesn't have: natural-language prompts that ground to real Danbooru tags, and a
pose-reference tool for artists. Manga generation is planned (see `docs/` and the
product spec).

Surfaces:

| Route | Purpose |
|---|---|
| `/create` | Art generation — tags or plain language, characters, references |
| `/pose` | Pose references — lineart on white + OpenPose skeleton |
| `/manga` | *(phase 7)* Synopsis → script → composited pages |

## Layout

```
pulsar/
├── docker-compose.yml     # postgres + comfy + core + orbit
├── Makefile               # make dev / migrate / seed-tags / eval
│
├── core/                # Go — go.mod HERE, not at root
│   ├── cmd/server/
│   └── internal/
│       ├── job/           # queue, states, sub-jobs
│       ├── assembly/      # PromptSpec — pure logic, heavily tested
│       ├── nltag/         # decompose → ground
│       ├── workflow/      # template patching, node-ID constants
│       ├── comfy/         # client, WS, process router
│       ├── tags/          # autocomplete
│       └── compliance/    # hooks from day one, impl phase 4
│
├── orbit/               # Nuxt 3 — package.json HERE
├── workflows/             # API-format ComfyUI JSON, versioned by hash
├── db/migrations/
├── comfy/                 # Dockerfile, pinned custom nodes, model fetch
└── eval/                  # Python — tuning harness
```

| Directory | Is |
|---|---|
| `core/` | The Go service — API, queue, prompt assembly, tag grounding, compliance. The product. |
| `orbit/` | The Nuxt 3 frontend. |

Each toolchain roots in its own subdirectory. The repo root holds only
orchestration. See `CONTRIBUTING.md` for why it's a monorepo and when to split.

## Running it

Prerequisites: Docker, Go 1.25+, pnpm 10 (`corepack enable`), Python 3.13+
(for native ComfyUI). Everything is configured by env vars — `cp .env.example
.env`; the defaults work for local dev. Native runs and compose read the same
repo-root `.env` (core loads it at boot; real environment variables win).

One-time setup:

```bash
make dev           # infra: postgres (GPU=1 adds containerised ComfyUI — linux + NVIDIA)
make migrate       # apply db/migrations in order
make seed-tags     # Danbooru → tag DB (~4 min; MIN_POST_COUNT tunes the floor)
make fetch-models  # pinned weights → comfy/models (~7 GB, resumable, sha256-verified)
make hooks         # conventional-commit check (brew install commitizen first)
cd orbit && pnpm install
```

Then three terminals:

```bash
make run-comfy     # ComfyUI natively — MPS on Apple Silicon, CUDA elsewhere
make run-core      # Go service on :8080 (PORT=8081 if something owns 8080)
make run-orbit     # Nuxt dev on :3000
```

Open http://localhost:3000, pass the age gate, generate. Without an LLM key
(`ANTHROPIC_API_KEY` / `DEEPSEEK_API_KEY`), plain-language translation 503s
gracefully; tag mode and generation work regardless.

Deploy target is linux + NVIDIA via `make dev GPU=1` — the `comfy/` image pins
ComfyUI and custom nodes by commit. Apple Silicon is a dev environment: the
same pins run natively on MPS (a 24-step 832×1216 SDXL image takes ~4 min).

## Docs

| Doc | Read when |
|---|---|
| `CLAUDE.md` | Working in this repo with Claude Code — read first |
| `docs/architecture.md` | Understanding request → image flow |
| `docs/tagdb.md` | Touching tags, autocomplete, or grounding |
| `docs/precise-reference.md` | Working on character/style references |
| `docs/tuning.md` | Changing models or sampler settings |
| `docs/compliance.md` | **Before anything ships to real users** |

## Status

Working end-to-end on real hardware: txt2img and pose (source + lineart +
skeleton from one generation), 121k-tag DB with NL grounding and live Danbooru
validation (new tags resolve without a reseed, flagged when they post-date the
model's training), full frontend. Phase 4 (compliance classification) gates
public launch — the unconditional safety negative in `assembly` is distribution
steering, not the gate.

## Licence

Code: TBD. Note ComfyUI is GPL-3.0 and base models (Illustrious, NoobAI) carry
their own licences with commercial-use terms — see `docs/tuning.md`.
