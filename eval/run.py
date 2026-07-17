# eval harness: sweep spec → generations through core → contact sheet +
# per-image sentinel scores (docs/tuning.md#the-eval-harness).
import itertools
import json
import os
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

import httpx
import yaml
from PIL import Image, ImageDraw

CORE = os.environ.get("CORE_URL", "http://localhost:8080")
SENTINEL = os.environ.get("SENTINEL_URL", "http://127.0.0.1:8189")
WATCH = [t.strip().replace(" ", "_") for t in os.environ.get(
    "FORBIDDEN_TAG_UC", "loli, shota, child, toddler, aged down").split(",")]

THUMB = 256
LABEL_H = 34


def cells(spec: dict) -> list[dict]:
    axes = spec.get("axes", {})
    keys = list(axes)
    out = []
    for prompt in spec["prompts"]:
        for seed in spec["seeds"]:
            for combo in itertools.product(*(axes[k] for k in keys)) if keys else [()]:
                req = dict(spec.get("params", {}))
                req.update({"prompt": prompt, "seed": seed})
                req.update(dict(zip(keys, combo)))
                out.append(req)
    return out


def wait(client: httpx.Client, job_id: str) -> dict:
    while True:
        j = client.get(f"{CORE}/jobs/{job_id}").json()
        if j["state"] in ("done", "failed", "blocked"):
            return j
        time.sleep(3)


def score(client: httpx.Client, png: bytes) -> dict:
    r = client.post(f"{SENTINEL}/classify", params={"include": ",".join(WATCH)},
                    content=png, timeout=120)
    r.raise_for_status()
    d = r.json()
    watch = {t: d["tags"].get(t, 0.0) for t in WATCH}
    return {"rating": d["rating"], "watch": watch, "watch_sum": round(sum(watch.values()), 4)}


def sheet(results: list[dict], out: Path, columns: int):
    rows = (len(results) + columns - 1) // columns
    img = Image.new("RGB", (columns * THUMB, rows * (THUMB + LABEL_H)), (16, 16, 18))
    draw = ImageDraw.Draw(img)
    for i, r in enumerate(results):
        x, y = (i % columns) * THUMB, (i // columns) * (THUMB + LABEL_H)
        if r.get("image"):
            cell = Image.open(r["image"])
            cell.thumbnail((THUMB, THUMB))
            img.paste(cell, (x + (THUMB - cell.width) // 2, y))
        else:
            colour = (120, 40, 40) if r["state"] == "blocked" else (60, 60, 66)
            draw.rectangle([x + 4, y + 4, x + THUMB - 4, y + THUMB - 4], outline=colour, width=3)
            draw.text((x + 12, y + THUMB // 2), r["state"].upper(), fill=colour)
        p = r["request"]
        label = f"s{p['seed']} cfg{p.get('cfg', '-')} {p.get('sampler', '')[:12]}"
        if r.get("sentinel"):
            label += f" Σ{r['sentinel']['watch_sum']:.3f}"
        draw.text((x + 6, y + THUMB + 4), label, fill=(200, 200, 205))
        draw.text((x + 6, y + THUMB + 18), p["prompt"][:38], fill=(140, 140, 148))
    img.save(out)


def main():
    spec = yaml.safe_load(Path(sys.argv[1]).read_text())
    stamp = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
    out = Path(__file__).parent / "out" / f"{spec['name']}-{stamp}"
    (out / "images").mkdir(parents=True)

    reqs = cells(spec)
    print(f"eval: {spec['name']} — {len(reqs)} generations via {CORE}")
    client = httpx.Client(timeout=60)

    # submit everything up front; core's queue serializes per GPU. A 4xx is
    # a result, not a crash — the prompt gate rejecting is data.
    jobs = []
    for r in reqs:
        resp = client.post(f"{CORE}/generate", json=r)
        body = resp.json()
        jobs.append((body.get("job_id"), r, body.get("error", "")))

    results = []
    for n, (job_id, req, reject) in enumerate(jobs, 1):
        if job_id is None:
            results.append({"request": req, "state": "rejected", "error": reject})
            print(f"  [{n}/{len(jobs)}] rejected ({reject})")
            continue
        j = wait(client, job_id)
        r = {"request": req, "job_id": job_id, "state": j["state"],
             "error": j.get("error", ""), "params": j.get("params")}
        if j["state"] == "done":
            png = client.get(f"{CORE}{j['image_url']}").content
            path = out / "images" / f"{job_id}.png"
            path.write_bytes(png)
            r["image"] = str(path)
            try:
                r["sentinel"] = score(client, png)
            except httpx.HTTPError as e:
                r["sentinel_error"] = str(e)
        results.append(r)
        print(f"  [{n}/{len(jobs)}] {j['state']}"
              + (f" Σ{r['sentinel']['watch_sum']:.3f}" if r.get("sentinel") else "")
              + (f" ({r['error']})" if r["error"] else ""))

    (out / "results.json").write_text(json.dumps(results, indent=1))
    sheet(results, out / "sheet.png", columns=min(len(reqs), spec.get("columns", 6)))

    sums = sorted(r["sentinel"]["watch_sum"] for r in results if r.get("sentinel"))
    blocked = sum(1 for r in results if r["state"] == "blocked")
    if sums:
        print(f"watch-sum over {len(sums)} delivered images: "
              f"max {sums[-1]:.4f}, p50 {sums[len(sums) // 2]:.4f} — "
              f"{blocked} blocked (gate default 0.05)")
    print(f"→ {out / 'sheet.png'}")


if __name__ == "__main__":
    main()
