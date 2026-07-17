#!/usr/bin/env bash
# Pinned model weights → comfy/models (see comfy/README.md).
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
MODELS_DIR="${MODELS_DIR:-$DIR/models}"

# Illustrious-XL v0.1, 6.94 GB (licence: docs/tuning.md#licences)
CKPT_URL="https://huggingface.co/OnomaAIResearch/Illustrious-xl-early-release-v0/resolve/main/Illustrious-XL-v0.1.safetensors"
CKPT_SHA256="3e15ba00387db678ab4a099f75771c4f5ac67fda9e7100a01d263eaf30145aa9"
CKPT_DEST="$MODELS_DIR/checkpoints/illustrious-xl.safetensors"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

fetch() {
  local url=$1 sha=$2 dest=$3
  if [ -f "$dest" ]; then
    echo "verifying existing $dest ..."
    if [ "$(sha256 "$dest")" = "$sha" ]; then echo "ok: $dest"; return; fi
    echo "checksum mismatch — re-downloading" >&2
    rm -f "$dest"
  fi
  mkdir -p "$(dirname "$dest")"
  curl -fL --retry 3 --retry-delay 5 -C - -o "$dest.partial" "$url"
  echo "verifying download ..."
  if [ "$(sha256 "$dest.partial")" != "$sha" ]; then
    echo "sha256 mismatch for $url — keeping $dest.partial for inspection" >&2
    exit 1
  fi
  mv "$dest.partial" "$dest"
  echo "fetched: $dest"
}

fetch "$CKPT_URL" "$CKPT_SHA256" "$CKPT_DEST"
