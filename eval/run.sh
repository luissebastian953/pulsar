#!/usr/bin/env bash
# Eval runner: venv bootstrap + sweep (docs/tuning.md#the-eval-harness).
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
VENV="$DIR/.venv"

if [ ! -x "$VENV/bin/python" ]; then
  python3 -m venv "$VENV"
  "$VENV/bin/pip" install --quiet --upgrade pip
  "$VENV/bin/pip" install --quiet -r "$DIR/requirements.txt"
fi
exec "$VENV/bin/python" "$DIR/run.py" "$@"
