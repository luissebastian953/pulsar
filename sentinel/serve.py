# sentinel: WD-tagger inference for the compliance gate (docs/compliance.md).
# Pure scoring — policy lives in core/internal/compliance.
import csv
import io
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

import numpy as np
import onnxruntime as ort
from PIL import Image

MODEL_DIR = os.environ.get("SENTINEL_MODEL_DIR", os.path.join(os.path.dirname(__file__), "models"))
PORT = int(os.environ.get("SENTINEL_PORT", "8189"))
SCORE_FLOOR = 0.05

session = ort.InferenceSession(os.path.join(MODEL_DIR, "model.onnx"))
INPUT = session.get_inputs()[0]
SIZE = INPUT.shape[2]  # NHWC, square

ratings, tags = [], []
with open(os.path.join(MODEL_DIR, "selected_tags.csv"), newline="") as f:
    for i, row in enumerate(csv.DictReader(f)):
        (ratings if row["category"] == "9" else tags).append((i, row["name"], row["category"]))


def preprocess(data: bytes) -> np.ndarray:
    img = Image.open(io.BytesIO(data)).convert("RGBA")
    canvas = Image.new("RGBA", img.size, (255, 255, 255, 255))
    canvas.alpha_composite(img)
    img = canvas.convert("RGB")
    side = max(img.size)
    square = Image.new("RGB", (side, side), (255, 255, 255))
    square.paste(img, ((side - img.width) // 2, (side - img.height) // 2))
    square = square.resize((SIZE, SIZE), Image.BICUBIC)
    arr = np.asarray(square, dtype=np.float32)[:, :, ::-1]  # RGB → BGR
    return arr[np.newaxis, :]


def classify(data: bytes, include: set) -> dict:
    probs = session.run(None, {INPUT.name: preprocess(data)})[0][0]
    out = {"rating": {}, "tags": {}}
    for i, name, _ in ratings:
        out["rating"][name] = round(float(probs[i]), 4)
    for i, name, _ in tags:
        p = float(probs[i])
        # include-listed tags always report, even sub-floor — policy tags
        # (age indicators) are weakly represented and score low
        if p >= SCORE_FLOOR or name in include:
            out["tags"][name] = round(p, 4)
    return out


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _json(self, code: int, body: dict):
        raw = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        if self.path == "/healthz":
            self._json(200, {"status": "ok", "input": SIZE})
        else:
            self._json(404, {"error": "not found"})

    def do_POST(self):
        url = urlparse(self.path)
        if url.path != "/classify":
            self._json(404, {"error": "not found"})
            return
        length = int(self.headers.get("Content-Length", "0"))
        if length <= 0 or length > 64 << 20:
            self._json(400, {"error": "bad content length"})
            return
        include = set((parse_qs(url.query).get("include") or [""])[0].split(","))
        try:
            self._json(200, classify(self.rfile.read(length), include))
        except Exception as e:  # noqa: BLE001 — any failure must surface, caller fails closed
            self._json(422, {"error": str(e)})


if __name__ == "__main__":
    print(f"sentinel listening on :{PORT} (input {SIZE}px, {len(tags)} tags)", file=sys.stderr)
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
