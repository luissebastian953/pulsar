# Herta style benchmark

**Goal:** match Honkai: Star Rail official / light-cone art style for **The Herta**.

She's an HSR 3.0 character (Jan 2025), which post-dates Illustrious v0.1's 2023
training cutoff — so the base model has never seen her. Identity must come from a
**Precise Reference** or a **LoRA**, not a character tag. This makes her the ideal
stress test for the reference system.

## Run it

```bash
./eval/benchmarks/run-herta.sh recipe    # user's exact recipe
./eval/benchmarks/run-herta.sh cs04      # coherent strength band
./eval/benchmarks/run-herta.sh char035   # reference-only ceiling (+ hires)
```

Reference image, base recipe, and variant overrides live in `herta-style.json`.
The base prompt is split into `character` (the Herta — identity, hair, eyes,
outfit) and `prompt` (the character-agnostic quality/style/composition scaffold),
so either can be swapped without disturbing the other. The runner merges them
**character-first** (identity leads for tag weight + booru order), then the
scaffold — pulsar has no standalone character field for a single subject, so they
become one prompt. The runner uploads the reference once (blob id cached in
`.ref-id`) and saves each result to `out/<label>.png`.

## Results

| Variant | Base | Strength | Mode | Hires | Verdict |
|---|---|---|---|---|---|
| recipe | eps | 1.0 | character_style | no | **Blowout** — abstract color noise, no character. Confirmed **3×** across 3 references (`out/recipe.png`): char_style @ 1.0 destroys generation regardless of reference. |
| cs04 | eps | 0.4 | character_style | no | **Coherent, on-model** — but background shimmers (IP-Adapter color artifact) and finish is the eps ceiling. |
| char035 | eps | 0.35 | character | yes | **eps reference-only ceiling** — identity close, face generic-anime, background shimmers. |
| vpred-char | v-pred | 0.5 | character | yes | **Best figure rendering of the set** — painterly shading, detailed outfit, 0.5 stays stable (higher coherency ceiling than eps). Hires **amplifies** the reference's bg texture into diagonal streaks; palette drifts lighter than canon. |
| vpred-style | v-pred | 0.6 | style | no | **Style transfer works** — reference palette (dark blue/purple/gold) and clean simple background; style mode doesn't inject the bg artifact. No identity carry (as designed); shading flat/cel-ish. |
| vpred-cs | v-pred | 0.4 | character_style | no | **Coherent but hazy** — softest of the set, washed; 20 steps + dual dilution reads underdone. No blowout at 0.4 on v-pred. Dual adapter remains ~10× slower; keep steps capped. |
| i2i | v-pred | — | img2img @ 0.55 | no | **Repaint path validated** — inherits official composition/values, re-renders in the model's hand; was the best result until the LoRA landed. |
| flagship | v-pred | — | img2img @ 0.45 + **HSR v3 LoRA @ 0.7** | no | **★ THE recipe.** `official art, game cg` tags (no fan-artist tags) + style LoRA + official-composition repaint: iridescent multi-tone eyes, warm splash lighting, painterly fabric — closest to HSR light-cone finish produced. LoRA-weight findings: v3 @ 0.7 has soft lines (gallery "over-crisp" was the poster's setup); Official-Art LoRA @ 0.65 clean but weaker signature; v3 txt2img renders the game logo — use a weighted `(logo:1.3)` negative. |

## Cross-character validation (2026-07-26)

The flagship recipe was run against four more vendored sources
(`testing_the_herta_2.png`, `testing_the_herta_3.jpg`, `testing_ruan_mei.png`,
`testing_silver_wolf.jpg`) — two post-cutoff (Herta), two in-model (Ruan Mei,
Silver Wolf). It generalized: the herta_3 witch-art repaint is the best output
of the project (splash-art grade), and Ruan Mei's canonical design language
survived intact (tag identity + source composition + LoRA stack). One lesson
from Silver Wolf: at denoise 0.45 the source outfit **overrides** tag identity
— for strong-identity characters on off-canon sources, raise denoise (~0.6) or
pick a canonical-outfit source. webp sources must be converted (ingest speaks
JPEG/PNG only).

## Conclusion

`ref_strength` is the whole game with the two-adapter Character & Style mode:
1.0 is a total blowout, ~0.4 is the coherent band. The best reference-only result
(char-only 0.35 + artist tags `ciloranko, ask_(askzy), as109` + hires) is genuinely
usable but **still below true HSR-official finish** — that gap is the base model, not
a tuning miss. IP-Adapter carries *identity*, never *finish*.

**Levers beyond reference tuning (both wired, drop a file + pick it):**

1. **Illustrious *finetune* checkpoint** — raw v0.1 is the plainest base in the
   ecosystem; a finetune (Illustrious v1.1, WAI, Nova Anime) closes much of the
   "unsmooth" gap before any LoRA. Drop into `comfy/models/checkpoints/`, pick in
   the Base-model dropdown. Same lineage — every feature still works.
2. **Style LoRA** — HSR/light-cone style on Illustrious base. Drop into
   `comfy/models/loras/`, pick in the LoRA dropdown. Stacks on the finetune.

Runtime note: these runs are memory-bound on a 17 GB Mac (MPS). Restart ComfyUI
between long sessions to clear accumulated model memory or runs crawl (~4 min →
70 min observed).
