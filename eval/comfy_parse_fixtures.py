"""Generate core/internal/prompt/testdata/comfy_parse.json — parity fixtures
for prompt.ComfyParse, the Go re-implementation of ComfyUI's prompt grammar.

The expected outputs come from ComfyUI's OWN functions, extracted with `ast`
from the pinned checkout (comfy/ComfyUI/comfy/sd1_clip.py) and run as-is —
no torch, no ComfyUI import. Only the four pure functions are executed:
escape_important → token_weights(text, 1.0) → unescape_important per
segment, which is exactly the order SDTokenizer.tokenize_with_weights uses.

The fixture records the checkout's commit and the sha256 of sd1_clip.py;
the Go test fails when the checkout moves without a regenerated fixture.

    python3 -I eval/comfy_parse_fixtures.py
"""
import ast
import hashlib
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "comfy" / "ComfyUI" / "comfy" / "sd1_clip.py"
OUT = ROOT / "core" / "internal" / "prompt" / "testdata" / "comfy_parse.json"
FUNCS = ("parse_parentheses", "token_weights", "escape_important", "unescape_important")

CASES = [
    # the ComfyUI analysis (research_notes/.../comfyui_sdxl_weighting.md Q1)
    "castorice (honkai: star rail), 1girl",
    "castorice \\(honkai: star rail\\), 1girl",
    "(castorice \\(honkai: star rail\\):1.2)",
    "(castorice (honkai: star rail):1.2)",
    "hatsune miku (vocaloid)",
    "((blue hair))",
    "(((blue hair)))",
    "((blue hair:1.5))",
    "((blue hair):1.5)",
    "(red, (blue hair:1.5))",
    "(blue hair:-1)",
    "(blue hair:0)",
    "(blue hair: 1.3 )",
    "(blue hair:1e1)",
    "(x:nan)",
    "(x:inf)",
    "(x:-Infinity)",
    "(x:1_0)",
    "(x:1__0)",
    "(x:+1.5)",
    "(x:.5)",
    "(x:5.)",
    "(x:1.5e)",
    "(x:1e-3)",
    "( x : 1.5 )",
    "(a:1.2:1.3)",
    "(:1.5)",
    "[blue hair]",
    "{blue hair}",
    "(unclosed, red",
    "close) (a:1.2)",
    "())",
    "(()",
    "((((((((((x))))))))))",
    "(x:1.2)(y:1.3)",
    "a (b) c (d:1.3) e",
    "(a, b:1.2), (c:0.5)",
    # escapes
    "x \\( y",
    "\\)",
    "(a\\):1.2)",
    "a\\\\(b:1.2)",
    "a\\ (b:1.2)",
    "\\(\\(x\\)\\)",
    # what pulsar's emitter writes (prompt.EmitComfy)
    "(year 2000:1.3393)",
    "(artist:foo:0.8)",
    "(:3:1.1)",
    "blue (hair:1.0497), x",
    "(sleeveless:0.9), (long legs:0.71), (holding hands:1.3856)",
    "castorice \\(honkai: star rail\\), (castorice \\(honkai: star rail\\):1.1848)",
    "loli, shota, child, lowres, (logo:1.3), 1990s \\(style\\)",
    "日本語 (タグ:1.2), 😀 x",
    "",
    "   ",
    "a,,b, ,c",
]


def load_functions():
    tree = ast.parse(SRC.read_text(encoding="utf-8"))
    found = {n.name: n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name in FUNCS}
    missing = set(FUNCS) - set(found)
    if missing:
        raise SystemExit(f"{SRC}: missing {sorted(missing)}")
    module = ast.Module(body=[found[n] for n in FUNCS], type_ignores=[])
    ns = {}
    exec(compile(module, str(SRC), "exec"), ns)  # pure string functions only
    return ns


def comfy_segments(ns, text):
    out = []
    for seg, w in ns["token_weights"](ns["escape_important"](text), 1.0):
        out.append({"text": ns["unescape_important"](seg), "w": repr(float(w))})
    return out


def main():
    ns = load_functions()
    commit = subprocess.run(["git", "-C", str(SRC.parent.parent), "rev-parse", "HEAD"],
                            capture_output=True, text=True).stdout.strip()
    tag = subprocess.run(["git", "-C", str(SRC.parent.parent), "describe", "--tags"],
                         capture_output=True, text=True).stdout.strip()
    fixture = {
        "comfyui": {"tag": tag, "commit": commit,
                    "sd1_clip_sha256": hashlib.sha256(SRC.read_bytes()).hexdigest()},
        "cases": [{"input": c, "segments": comfy_segments(ns, c)} for c in CASES],
    }
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(json.dumps(fixture, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(f"wrote {len(CASES)} cases → {OUT.relative_to(ROOT)} (ComfyUI {tag} {commit[:7]})")


if __name__ == "__main__":
    main()
