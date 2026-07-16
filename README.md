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

## Quickstart

```bash
make dev          # postgres + comfy + core + orbit
make migrate      # schema
make seed-tags    # Danbooru dump → tag DB (slow, once)
open http://localhost:3000
```

Requires a CUDA GPU (12 GB+) or a `COMFY_URL` pointing at a remote ComfyUI.

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

Pre-MVP. Phases 1–3 (foundation → generation core → application layer) are the
buildable MVP; phase 4 (compliance) gates public launch.

## Licence

Code: TBD. Note ComfyUI is GPL-3.0 and base models (Illustrious, NoobAI) carry
their own licences with commercial-use terms — see `docs/tuning.md`.
