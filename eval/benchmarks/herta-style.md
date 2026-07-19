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
The runner uploads the reference once (blob id cached in `.ref-id`) and saves each
result to `out/<label>.png`.

## Results

| Variant | Reference | Strength | Mode | Hires | Verdict |
|---|---|---|---|---|---|
| recipe | classroom Herta | 1.0 | character_style | no | **Blowout** — abstract color noise, no character. Confirmed **3×** across 3 references (`out/recipe.png`): char_style @ 1.0 destroys generation regardless of reference. |
| cs04 | classroom Herta | 0.4 | character_style | no | **Coherent, on-model** — lavender/purple gradient hair, purple eyes, white dress + caped pauldrons + blue sash + gold clasp. Background still shimmers (IP-Adapter color artifact). |
| char035 | witch art | 0.35 | character | yes | **Reference-only ceiling** — hires smooths linework/shading, single adapter removes character color distortion, identity close to canonical. Background still shimmers; face reads generic-anime not miHoYo-painterly. |

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
