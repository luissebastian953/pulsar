#!/usr/bin/env python3
"""Danbooru → LoRA training set: images + caption .txt per image.

Captions come from Danbooru's human-curated tags (better than any tagger),
pruned to what the LoRA should learn, with the trigger word first.

Compliance (CLAUDE.md non-negotiable): posts are filtered at the source —
rating general/sensitive only, and any post carrying a forbidden tag is
skipped. Training data is gated the same way generation output is.

Usage: fetch-dataset.py "yixuan_(zenless_zone_zero)" [--out DIR] [--limit N]
"""

import argparse
import json
import pathlib
import sys
import time
import urllib.parse
import urllib.request

BASE = "https://danbooru.donmai.us"
UA = {"User-Agent": "pulsar/1.0 (lora dataset prep)"}

# child-safety gate: a post carrying any of these is never fetched
FORBIDDEN = {"loli", "shota", "child", "toddler", "aged_down", "baby"}

# caption noise: meta/composition tags the LoRA must not learn as identity
PRUNE = {
    "highres", "absurdres", "commentary", "commentary_request", "translated",
    "translation_request", "artist_name", "signature", "watermark", "web_address",
    "bad_id", "bad_pixiv_id", "bad_twitter_id", "non-web_source",
}
PRUNE_PREFIXES = ("commentary", "bad_", "spoilers", "official_art")


def get(path: str, params: dict) -> list:
    url = f"{BASE}{path}?{urllib.parse.urlencode(params)}"
    req = urllib.request.Request(url, headers=UA)
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def caption(post: dict, trigger: str) -> str:
    tags = post.get("tag_string_general", "").split()
    tags = [t for t in tags
            if t not in PRUNE and not t.startswith(PRUNE_PREFIXES)]
    # trigger word first (tag order = weight), then curated tags
    words = [trigger] + [t.replace("_", " ") for t in tags]
    return ", ".join(words)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("character")
    ap.add_argument("--out", default=None)
    ap.add_argument("--limit", type=int, default=120)
    ap.add_argument("--trigger", default=None)
    args = ap.parse_args()

    trigger = args.trigger or args.character.replace("_", " ")
    out = pathlib.Path(args.out or f"train/dataset/{args.character}")
    out.mkdir(parents=True, exist_ok=True)

    # anonymous Danbooru allows 2 search tags: character + solo. Rating,
    # compliance, and score ranking happen client-side.
    candidates = []
    for page in range(1, 21):
        posts = get("/posts.json", {
            "tags": f"{args.character} solo", "limit": 100, "page": page,
        })
        if not posts:
            break
        candidates.extend(posts)
        if len(candidates) >= args.limit * 4:
            break
        time.sleep(0.3)

    skipped = 0
    eligible = []
    for p in candidates:
        # g+s only — q/e never enter the set; most ordinary SFW fanart is
        # rated s ("sensitive") by default caution, so g-only starves the set
        if p.get("rating") not in ("g", "s"):
            continue
        if set(p.get("tag_string", "").split()) & FORBIDDEN:
            skipped += 1
            continue
        url = p.get("large_file_url") or p.get("file_url")
        if not url or p.get("file_ext") not in ("jpg", "jpeg", "png", "webp"):
            continue
        if p.get("image_width", 0) < 640 or p.get("image_height", 0) < 640:
            continue
        eligible.append(p)
    eligible.sort(key=lambda p: p.get("score", 0), reverse=True)

    fetched = 0
    for p in eligible[: args.limit]:
        url = p.get("large_file_url") or p.get("file_url")
        name = f"{p['id']}.{url.rsplit('.', 1)[-1]}"
        dest = out / name
        if not dest.exists():
            try:
                req = urllib.request.Request(url, headers=UA)
                with urllib.request.urlopen(req, timeout=60) as r:
                    dest.write_bytes(r.read())
            except Exception as e:  # noqa: BLE001 — skip and continue
                print(f"skip {p['id']}: {e}", file=sys.stderr)
                continue
            time.sleep(0.5)  # politeness
        (out / f"{dest.stem}.txt").write_text(caption(p, trigger))
        fetched += 1

    print(f"dataset: {fetched} images → {out}  (skipped {skipped} by compliance filter)")
    return 0 if fetched else 1


if __name__ == "__main__":
    raise SystemExit(main())
