#!/usr/bin/env bash
# miHoYo house-style benchmark runner. Pure txt2img — no reference upload —
# so this is just: build request from spec + variant, submit, poll, save.
# Usage: run-mihoyo.sh <label>  (default: jean)
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
CORE="${CORE_URL:-http://localhost:8081}"
SPEC="$DIR/mihoyo-style.json"
OUT="$DIR/out"; mkdir -p "$OUT"
LABEL="${1:-jean}"

# build the request: base + variant overrides. Prompt merge order is
# style carriers → character → scene → quality scaffold (booru convention:
# subject leads after the style anchor; quality trails). 'style' /
# 'character' / 'scene' are spec-level fields, not core request fields —
# they must never reach the API.
REQ=$(python3 - "$SPEC" "$LABEL" <<'PY'
import json, sys
spec = json.load(open(sys.argv[1])); label = sys.argv[2]
v = next((x for x in spec['variants'] if x['label'] == label), None)
if not v: sys.exit(f"unknown variant {label}")
req = dict(spec['base']); ov = dict(v.get('overrides', {}))
pre, post = ov.pop('prompt_prepend', ''), ov.pop('prompt_append', '')
req.update(ov)
parts = [req.pop('style', ''), v.get('character', ''), v.get('scene', ''), req['prompt']]
req['prompt'] = pre + ', '.join(p.strip() for p in parts if p.strip()) + post
print(json.dumps(req))
PY
)

JOB=$(curl -s -X POST "$CORE/generate" -H 'Content-Type: application/json' -d "$REQ" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['job_id'])")
echo "[$LABEL] job $JOB"
until st=$(curl -s "$CORE/jobs/$JOB" | python3 -c 'import json,sys;print(json.load(sys.stdin)["state"])' 2>/dev/null); \
  [ "$st" = done ] || [ "$st" = failed ] || [ "$st" = blocked ]; do sleep 15; done
err=$(curl -s "$CORE/jobs/$JOB" | python3 -c 'import json,sys;print(json.load(sys.stdin).get("error") or "")')
echo "[$LABEL] $st $err"
[ "$st" = done ] && curl -s -o "$OUT/$LABEL.png" "$CORE/images/$JOB" && echo "[$LABEL] → out/$LABEL.png"
