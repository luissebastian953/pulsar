# Pull k solo, rating:general Danbooru posts for a character tag into
# eval/refs/<tag>/ as CCIP identity references (sweeps/assurance/README.md).
# Danbooru etiquette: ~1 request/second sustained (help:api, 2026-08-10).
#
#   ./eval/.venv/bin/python eval/fetch-refs.py "furina_(genshin_impact)" [k=8]
import os
import sys
import time
from pathlib import Path

import httpx

DANBOORU = os.environ.get("DANBOORU_URL", "https://danbooru.donmai.us")
UA = "pulsar-eval/1 (+https://github.com/pulsar; assurance benchmark)"


def main():
    tag = sys.argv[1]
    k = int(sys.argv[2]) if len(sys.argv) > 2 else 8
    out = Path(__file__).parent / "refs" / tag
    out.mkdir(parents=True, exist_ok=True)
    client = httpx.Client(headers={"User-Agent": UA}, timeout=60, follow_redirects=True)
    # general rating + score order: the "consensus" look of the character.
    # Anonymous search allows 2 tags (rating: is free), so `solo` and the
    # static-image check are filtered client-side.
    r = client.get(f"{DANBOORU}/posts.json", params={
        "tags": f"{tag} rating:general order:score", "limit": max(k * 6, 20)})
    posts = r.json()
    if not isinstance(posts, list):
        sys.exit(f"{tag}: Danbooru {r.status_code}: {posts.get('message', posts)}")
    time.sleep(1.1)
    saved = 0
    for p in posts:
        if " solo " not in f" {p.get('tag_string_general', '')} ":
            continue
        url = p.get("large_file_url") or p.get("file_url")
        if not url or not url.lower().endswith((".jpg", ".jpeg", ".png", ".webp")):
            continue
        dest = out / f"{p['id']}{Path(url).suffix.lower()}"
        if not dest.exists():
            dest.write_bytes(client.get(url).content)
            time.sleep(1.1)
        saved += 1
        if saved >= k:
            break
    print(f"{tag}: {saved} references in {out}")
    if saved < k:
        print("  fewer than requested — the tag is sparse or mostly non-general; CCIP same-rate will be noisier")


if __name__ == "__main__":
    main()
