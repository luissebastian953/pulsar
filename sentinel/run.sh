#!/usr/bin/env bash
# Native sentinel runner: venv + pinned model + serve (docs/compliance.md).
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
VENV="$DIR/.venv"

if [ ! -x "$VENV/bin/python" ]; then
  python3 -m venv "$VENV"
  "$VENV/bin/pip" install --quiet --upgrade pip
  "$VENV/bin/pip" install --quiet -r "$DIR/requirements.txt"
fi
"$DIR/fetch-model.sh"
exec "$VENV/bin/python" "$DIR/serve.py"
