#!/usr/bin/env bash
# Pinned classifier weights → sentinel/models (docs/compliance.md).
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
MODELS_DIR="${SENTINEL_MODEL_DIR:-$DIR/models}"

# SmilingWolf/wd-eva02-large-tagger-v3 (Apache-2.0), 1.26 GB
BASE="https://huggingface.co/SmilingWolf/wd-eva02-large-tagger-v3/resolve/main"
ONNX_SHA256="9e768793060c7939b277ccb382783e8670e8a042d29d77aa736be0c8cc898bfc"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

mkdir -p "$MODELS_DIR"
if [ ! -f "$MODELS_DIR/model.onnx" ] || [ "$(sha256 "$MODELS_DIR/model.onnx")" != "$ONNX_SHA256" ]; then
  curl -fL --retry 3 -C - -o "$MODELS_DIR/model.onnx.partial" "$BASE/model.onnx"
  [ "$(sha256 "$MODELS_DIR/model.onnx.partial")" = "$ONNX_SHA256" ] || { echo "sha256 mismatch" >&2; exit 1; }
  mv "$MODELS_DIR/model.onnx.partial" "$MODELS_DIR/model.onnx"
fi
curl -fsL -o "$MODELS_DIR/selected_tags.csv" "$BASE/selected_tags.csv"
echo "sentinel model ready: $MODELS_DIR"
