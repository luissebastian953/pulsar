# comfy/

The ComfyUI runtime core talks to. Two ways to run it:

- **Deploy (linux + NVIDIA):** `docker compose --profile gpu up` — builds the
  image here. ComfyUI is pinned by `ARG COMFYUI_REF` in the Dockerfile; torch
  comes from cu130 wheels (CUDA bundled, host supplies only the driver).
- **Dev without NVIDIA (Apple Silicon → MPS):** `make run-comfy` — venv +
  checkout under this directory, same ref (read from the Dockerfile, no drift).

Weights are never in git or the image. `make fetch-models` downloads the pinned
checkpoints into `models/` (resumable, sha256-verified); both run modes read it.

Custom nodes: none yet. When a workflow gains one, pin it by commit in the
Dockerfile in the same change — see `.claude/skills/comfy-workflow/`.
