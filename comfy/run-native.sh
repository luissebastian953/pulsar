#!/usr/bin/env bash
# Native ComfyUI for dev machines without NVIDIA — Apple Silicon runs on MPS via
# the default torch wheels. The docker image (Dockerfile) is the deploy target;
# this reads the same COMFYUI_REF so the two can't drift.
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
COMFYUI_REF="$(sed -n 's/^ARG COMFYUI_REF=//p' "$DIR/Dockerfile")"
APP="$DIR/ComfyUI"
VENV="$DIR/.venv"
PORT="${COMFY_PORT:-8188}"

if [ ! -d "$APP" ]; then
  git clone --depth 1 --branch "$COMFYUI_REF" https://github.com/Comfy-Org/ComfyUI "$APP"
fi

if [ ! -x "$VENV/bin/python" ]; then
  python3 -m venv "$VENV"
  "$VENV/bin/pip" install --quiet --upgrade pip
  # default-index wheels: MPS on Apple Silicon, CPU elsewhere
  "$VENV/bin/pip" install torch torchvision torchaudio
  "$VENV/bin/pip" install -r "$APP/requirements.txt"
fi

# Same layout as the docker mount, but with an absolute host path.
cat > "$DIR/.extra_model_paths.yaml" <<EOF
pulsar:
  base_path: $DIR/models
  checkpoints: checkpoints
  vae: vae
  loras: loras
  embeddings: embeddings
  controlnet: controlnet
  clip_vision: clip_vision
  ipadapter: ipadapter
  upscale_models: upscale_models
EOF

mkdir -p "$DIR/models/checkpoints" "$DIR/output" "$DIR/input"

exec "$VENV/bin/python" "$APP/main.py" \
  --listen 127.0.0.1 --port "$PORT" \
  --extra-model-paths-config "$DIR/.extra_model_paths.yaml" \
  --output-directory "$DIR/output" \
  --input-directory "$DIR/input"
