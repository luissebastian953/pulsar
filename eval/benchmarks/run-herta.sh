#!/usr/bin/env bash
# Herta style benchmark runner. Uploads the reference once (caches the blob
# id), then runs a labelled variant from herta-style.json through core and
# saves out/<label>.png. Usage: run-herta.sh <label>  (default: recipe)
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
CORE="${CORE_URL:-http://localhost:8081}"
SPEC="$DIR/herta-style.json"
OUT="$DIR/out"; mkdir -p "$OUT"
LABEL="${1:-recipe}"

# resolve + upload reference once; cache the blob id next to the spec.
# reference_image is spec-relative (vendored in this dir); absolute and
# ~-paths still work.
REFPATH=$(python3 -c "
import json, os
p = os.path.expanduser(json.load(open('$SPEC'))['reference_image'])
print(p if os.path.isabs(p) else os.path.join('$DIR', p))")
CACHE="$DIR/.ref-id"
if [ ! -s "$CACHE" ] || ! curl -sf "$CORE/images/$(cat "$CACHE")" -o /dev/null; then
  curl -s -X POST -F "image=@$REFPATH" "$CORE/uploads" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['image_id'])" > "$CACHE"
  echo "uploaded reference → $(cat "$CACHE")"
fi
REF=$(cat "$CACHE")

# build the request: base + variant overrides (+ optional prompt prepend/append)
REQ=$(python3 - "$SPEC" "$LABEL" "$REF" <<'PY'
import json, sys
spec = json.load(open(sys.argv[1])); label, ref = sys.argv[2], sys.argv[3]
v = next((x for x in spec['variants'] if x['label'] == label), None)
if not v: sys.exit(f"unknown variant {label}")
req = dict(spec['base']); ov = dict(v['overrides'])
pre, post = ov.pop('prompt_prepend', ''), ov.pop('prompt_append', '')
req.update(ov)
# base.character (the subject) leads for tag weight + booru order, then the
# style/quality scaffold; pulsar has no standalone character field for a
# single character, so they merge into one prompt (and 'character' must not
# reach core — it's not a request field).
char = req.pop('character', '').strip()
body = (char + ', ' if char else '') + req['prompt']
req['prompt'] = pre + body + post
# the uploaded blob is the img2img source for kind=img2img variants,
# a Precise Reference otherwise; ref knobs don't apply to plain img2img
if req.get('kind') == 'img2img':
    req['image_id'] = ref
    for k in ('ref_mode', 'ref_strength', 'ref_fidelity'):
        req.pop(k, None)
else:
    req['reference_id'] = ref
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
