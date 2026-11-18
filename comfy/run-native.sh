#!/usr/bin/env bash
# Native ComfyUI for no-NVIDIA dev (see comfy/README.md).
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

# pinned custom nodes (custom-nodes.txt); idempotent
while read -r url sha; do
  [ -z "$url" ] && continue
  case "$url" in '#'*) continue ;; esac
  d="$APP/custom_nodes/$(basename "$url")"
  if [ ! -d "$d" ] || [ "$(git -C "$d" rev-parse HEAD 2>/dev/null)" != "$sha" ]; then
    rm -rf "$d"
    git init -q "$d"
    git -C "$d" fetch -q --depth 1 "$url" "$sha"
    git -C "$d" checkout -q FETCH_HEAD
    if [ -f "$d/requirements.txt" ]; then
      # onnxruntime-gpu has no macOS wheels; failed install retries clean
      if ! sed 's/^onnxruntime-gpu.*/onnxruntime/' "$d/requirements.txt" \
        | "$VENV/bin/pip" install -r /dev/stdin; then
        rm -rf "$d"
        exit 1
      fi
    fi
  fi
done < "$DIR/custom-nodes.txt"

# docker mount layout, absolute host path
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

# --disable-metadata: without it SaveImage writes the whole patched graph
# (safety negative, model files, upload IDs) into every PNG. core strips
# regardless (docs/compliance.md#output-metadata); this is the first layer.
exec "$VENV/bin/python" "$APP/main.py" \
  --listen 127.0.0.1 --port "$PORT" \
  --extra-model-paths-config "$DIR/.extra_model_paths.yaml" \
  --output-directory "$DIR/output" \
  --input-directory "$DIR/input" \
  --disable-metadata
