# Port NovelAI's running weight, not its numbers

NovelAI emphasis is not a bracket grammar. It is **one running multiplier**: every `{` or `]` multiplies it by exactly 1.05, every `}` or `[` divides it by 1.05, `N::` overwrites it with N, and a bare `::` resets it to 1.0. NovelAI's docs state this as an operator definition, and its production frontend implements it as a single float with no stack ([NovelAI docs](https://docs.novelai.net/en/image/strengthening-weakening); [NovelAI frontend](https://novelai.net/_next/static/chunks/2pglf7ew9_mj1.js)). That one model explains every edge case. Unclosed brackets run to the end of the prompt, stray closers weaken everything after them, `::` "closes" brackets by resetting the value, and a tag that ends in digits right before `::` turns those digits into a weight. It is also the model that every public converter gets wrong. Numeric emphasis arrived in V4 and negative weights in V4.5. V3, the only SDXL-era NovelAI model, has brackets only. V5 kept the syntax and moved to a Qwen tokenizer. NovelAI's numbers do not carry over to SDXL. NovelAI has recalibrated emphasis strength at least three times on the same syntax, and it calls weights up to 10 "reasonably stable" on V4.5. ComfyUI's SDXL path is different in every respect that matters. It overwrites nested explicit weights instead of multiplying them. It interpolates each token's CLIP-L and CLIP-G hidden state away from the empty prompt with no normalisation, never weights the pooled vector, and accepts negative, zero, NaN and infinite weights without a clamp. For pulsar this is a **safety and correctness problem first and a feature second**. Core parses no prompts today. `CheckPrompt` compares raw comma pieces of the positive and character prompts for exact equality against an env-only watch list and never scans the negative. Every wrapper that ComfyUI strips defeats that check, and Danbooru qualifiers such as `name (series)` are silently weighted ×1.1. The recommendation is one stdlib-only pure package, `core/internal/prompt`, with five jobs:

- Parse NovelAI syntax exactly as the frontend does.
- Reject non-positive weights, weights above 10, `embedding:`, `|` and SD-style `(x:1.2)`.
- Map the running weight through a per-lineage saturating calibration fitted in `eval/`.
- Emit flat, escaped `(text:w)` per tag.
- Give `CheckPrompt` a Go port of ComfyUI's own parser, so the check reads the bytes ComfyUI will encode on every surface.

FORBIDDEN_TAG_UC is asserted first in every negative, and the output classifier remains the gate.

## NovelAI emphasis is one running number that brackets nudge and `::` overwrites

The docs give the semantics in their own words: "every { or ] acts as a x1.05 focus multiplier to everything that's to their right, and every } or [ acts as a ÷1.05 focus divider to everything that's to their right." Their example is deliberately unbalanced (`… a {{{{{cherry blossom tree`) ([NovelAI docs](https://docs.novelai.net/en/image/strengthening-weakening)). The frontend's rule table matches: `{` → `1.05*e`, `}` → `e/1.05`, `[` → `e/1.05`, `]` → `1.05*e`. The loop keeps one value `h=1`, declared **outside the per-paragraph loop**, so emphasis carries across newlines. Nothing pairs brackets ([FE 2pglf7ew9_mj1.js](https://novelai.net/_next/static/chunks/2pglf7ew9_mj1.js)). The weakening factor is exactly ÷1.05 (0.952381), not ×0.95, so `{[x]}` is exactly neutral. **No depth limit and no escape exist.** The client token counters delete every `{ } [ ]` before tokenizing: CLIP replaces them with a space and T5 removes them, with no backslash handling. A literal brace therefore cannot be expressed at all ([FE 0f3vss_nqpcf5.js](https://novelai.net/_next/static/chunks/0f3vss_nqpcf5.js)). Round brackets have no meaning on NovelAI ([nishio](https://scrapbox.io/nishio-en/Using_round_brackets_in_NovelAI_is_pointless.)).

Numeric emphasis (V4+) **replaces rather than multiplies**. The frontend's `::` rule is `(e,t)=>void 0===t||Number.isNaN(t)?1:t`. The number is whatever `/-?\d*\.?\d*$/` matches in the text immediately before the `::`, read with `parseFloat`. An empty match resets the weight to 1. The bare strings `-`, `-.` and `.` become **0**. There is no `+` sign, no exponent, and no whitespace allowed between the number and the `::` ([FE 2pglf7ew9_mj1.js](https://novelai.net/_next/static/chunks/2pglf7ew9_mj1.js)). The docs' advice that `::` "also serves to close any open brackets" (`{{{{{rain ::`) is that reset, not a pop ([NovelAI docs](https://docs.novelai.net/en/image/strengthening-weakening)). The frontend has three error hints. Two catch a space between the number and `::` and a number placed after `::`. The third fires when `|w| > 70` and asks whether the user meant to put a space before `::` ([FE 0duset3_dg_wg.js](https://novelai.net/_next/static/chunks/0duset3_dg_wg.js)). That third hint exists because of the **digits-before-`::` trap**. The trap has been confirmed in production: users of one converter saw `{{{year 2025}}}, tag` reach NovelAI as `1.16::year 2025::`, which weighted the rest of the prompt by 2025 ([MooshieUI PR #937](https://github.com/Mooshieblob1/MooshieUI/pull/937)). NoobAI's year tags (`year 2021`) walk straight into it ([NoobAI-XL card](https://huggingface.co/Laxhar/noobai-XL-Vpred-1.0)). I ran a faithful port of the frontend loop. It reproduces the published behaviour and shows how strongly the scalar model diverges from bracket-pair intuition:

| Prompt | Effective weights (frontend semantics) |
|---|---|
| `{{{x}}}` / `[x]` / `{[x]}` | 1.157625 / 0.952381 / 1.0 |
| `{a, [b], c` | `a` 1.05, `b` **1.0**, `c` 1.05 (no stack) |
| `a}, b` | `b` **0.952** (a stray closer weakens the rest) |
| `{1.8::holding hands}, smile` | `holding hands` 1.8; `smile` **1.714** (the `}` divides the numeric weight, and the effect leaks) |
| `1.5::{red hair}::, smile` | `red hair` 1.575 (brackets multiply inside a numeric section) |
| `{{rain::}}, smile` | `rain` 1.1025; `smile` **0.907** (closing twice is harmful) |
| `1.5::year 2000::, smile` | `year` 1.5; `smile` **2000** |
| `2::a, {b}, c::, d` | `a` 2.0, `b` 2.1, `c` 2.0, `d` 1.0 |
| `-::hat::` / `.5::coat::` / `1.2.3::hat::` | 0 / 0.5 / `hat` 2.3, with `1.` left as text |

Negative weights are official from V4.5. The docs describe them as "better at targeted removals and inversions of concepts" (`-1::hat ::`, `-6::simple illustration ::`) ([NovelAI docs](https://docs.novelai.net/en/image/strengthening-weakening)). The V4.5 release says the usable range expanded and outputs "remain reasonably stable even with numeric emphasis values up to and even beyond 10" ([V4.5 Curated](https://journal.novelai.net/novelai-diffusion-v4-5-curated-is-here-9e5ba1f628f3/)). NovelAI applies negative weights itself. The V4.5 Curated quality suffix contains `-0.8::feet::` ([docs: Quality Tags](https://docs.novelai.net/en/image/qualitytags)). V5 Full's "medium" effort has **no custom Undesired Content**, and the docs recommend `-3::hat::` instead ([Effort Toggle](https://journal.novelai.net/novelai-diffusion-v5-full-effort-toggle/)). Zero is accepted syntax: the V5 Light UC preset ends in `0::ai-generated::` ([docs: Undesired Content](https://docs.novelai.net/en/image/undesiredcontent)). No range is documented.

The same syntax applies in Undesired Content with the meaning flipped: `{}` means "avoid more". In V4+ the UC is sent as its own `v4_negative_prompt` caption ([NovelAI docs](https://docs.novelai.net/en/image/strengthening-weakening); [FE 36juzcfi11xq3.js](https://novelai.net/_next/static/chunks/36juzcfi11xq3.js)). Multi-character prompts put each character prompt and each character UC in its own `char_captions[]` string, so the running weight presumably starts at 1.0 in every box. Character limits are six on V4 per the docs, and 22 (docs) or 32 (frontend) on V5. The alternative `base | char1 | char2` syntax is disabled once any character box exists ([docs: Multi-Character](https://docs.novelai.net/en/image/multiplecharacters); [FE 0duset3_dg_wg.js](https://novelai.net/_next/static/chunks/0duset3_dg_wg.js)).

| Model | Text encoder | `{}`/`[]` | `w::` | Negative `w::` | `\|` meaning |
|---|---|---|---|---|---|
| V1/V2 (SD1.x) | CLIP | yes | no | no | prompt mixing |
| V3 (SDXL-based) | CLIP-L/G | yes | no (`::` is literal) | no | prompt mixing |
| V4 Curated/Full | T5, 512 tokens | yes | yes | undocumented | character separator |
| V4.5 Curated/Full | T5 | yes | yes, range to ~10+ | yes | character separator |
| V5 (2026-08-21) | Qwen tokenizer in FE; encoder unconfirmed | yes | yes | yes | character separator |

Sources for the table: the frontend capability flags (`numericEmphasis` false through V3) ([FE 0duset3_dg_wg.js](https://novelai.net/_next/static/chunks/0duset3_dg_wg.js)), the tokenizer map that sends V5 to `qwen35_tokenizer.def` ([FE 2gk65nl2bm8-4.js](https://novelai.net/_next/static/chunks/2gk65nl2bm8-4.js)), the V4 announcement that replaced CLIP with T5 ([V4 Full](https://journal.novelai.net/introducing-novelai-diffusion-v4-full-928e759620ea/)), and the prompt-mixing docs ("only available on V3 or lower") ([docs: Prompt Mixing](https://docs.novelai.net/en/image/promptmixing)). The frontend chunk filenames change with every build.

**NovelAI has never published how emphasis is applied inside the model.** The V1 post describes penultimate-layer CLIP conditioning over three 75-token chunks and does not mention emphasis ([NovelAI improvements on SD](https://journal.novelai.net/novelai-improvements-on-stable-diffusion-e10d38db82ac/)). The V3 paper does not discuss emphasis either, but it does show that V3 averages pooled states over all CLIP chunks ([arXiv 2409.15997](https://arxiv.org/html/2409.15997)). NovelAI's public T5 repo contains no token-weighting code ([NovelAI/t5](https://github.com/NovelAI/t5)). V4+ requests carry the raw string with braces intact, so parsing happens on the server, which is closed ([FE 36juzcfi11xq3.js](https://novelai.net/_next/static/chunks/36juzcfi11xq3.js)). The behaviour has to be inferred. Negative weights invert a concept without any UC, which implies per-token scaling inside the conditioning path rather than a CFG-branch trick. The magnitudes are clearly UI units tuned per encoder. A V4 minor update reportedly made users "need to use more {} or []" (seen only via a search summary, since the X post returned HTTP 402) ([@novelaiofficial](https://x.com/novelaiofficial/status/1898147101638562117)). V4.5 widened the range "due to changes to the architecture and improvements to how we apply emphasis". A secondary, AI-written source reports that V5 fixed weights between 0 and 1 "reflected too strongly" on August 22, 2026 ([Itsuki AI Lab](https://note.com/itsuki_ailab/n/n6e234b0a0319)). One caveat applies to everything above: the frontend is the client highlighter. The docs' operator definition agrees with it, but nothing proves the server applies identical arithmetic to edge cases such as `-::` or `1.2.3::`.

## ComfyUI overwrites nested weights and lerps each token away from the empty prompt

ComfyUI's `token_weights` multiplies by 1.1 for each `(…)` group. It then looks for `:` with `rfind`, and if `float()` parses the suffix it **assigns** `weight = float(...)`. So `((blue hair:1.5))` is 1.5, `((blue hair):1.5)` is 1.65, and nested explicit weights silently collapse ([sd1_clip.py L348–366](https://github.com/Comfy-Org/ComfyUI/blob/v0.28.0/comfy/sd1_clip.py#L348-L366)). Python's `float()` also accepts `-1`, `0`, `1e1`, `nan` and `inf`, and no line clamps the result. The only escapes are `\(` and `\)`, swapped for sentinel bytes `\0\1`/`\0\2` before parsing ([sd1_clip.py L368–376](https://github.com/Comfy-Org/ComfyUI/blob/v0.28.0/comfy/sd1_clip.py#L368-L376)). `[]` and `{}` are literal to the backend. A stray `)` drives the nesting count negative and turns **every later group into literal text**. The case that matters most for an anime tag DB is `castorice (honkai: star rail)`. Written plainly, the series qualifier is weighted 1.1, because `float(" star rail")` fails and the colon text is kept, and the parentheses vanish. The escaped form `(castorice \(honkai: star rail\):1.2)` weights the whole tag at 1.2 as intended ([ComfyUI README](https://github.com/Comfy-Org/ComfyUI/blob/v0.28.0/README.md); [sd1_clip.py L320–376](https://github.com/Comfy-Org/ComfyUI/blob/v0.28.0/comfy/sd1_clip.py#L320-L376)). NoobAI's own model card writes `arlecchino \(genshin impact\)`, so the model expects escaped literal parentheses ([NoobAI-XL card](https://huggingface.co/Laxhar/noobai-XL-Vpred-1.0)).

Weights apply after the transformer, per token position j, and only where `w ≠ 1`: `z[j] = (z[j] − z_empty[j])·w + z_empty[j]`, where `z_empty` is the encoded empty prompt ([sd1_clip.py L53–64](https://github.com/Comfy-Org/ComfyUI/blob/v0.28.0/comfy/sd1_clip.py#L53-L64)). SDXL runs the same text and the same weights through CLIP-L and CLIP-G separately, each with its own empty anchor, and concatenates them into the 2048-channel context. The pooled vector is CLIP-G's first-chunk output, taken **before** weighting, so weights never reach the ADM vector ([sdxl_clip.py L56–62](https://github.com/Comfy-Org/ComfyUI/blob/v0.28.0/comfy/sdxl_clip.py#L56-L62)). With `w = 0` the token's output is replaced by the empty-prompt vector at that position. With `w < 0` it is reflected through that vector. **Neither removes the concept.** CLIP's causal mask has already folded the token into every later token's hidden state, and the pooled vector is untouched. BlenderNeko's example shows that palm trees down-weighted to 0.1 still appear ([ADV_CLIP_emb README](https://github.com/BlenderNeko/ComfyUI_ADV_CLIP_emb)). NovelAI's `-1::hat::` therefore has no faithful ComfyUI equivalent. The only true CLIP-side removal is compel-style masking and re-encoding, available through ADV_CLIP_emb. That pack has been unmaintained since 2024 and reaches into `cond_stage_model` internals ([ADV_CLIP_emb adv_encode.py](https://github.com/BlenderNeko/ComfyUI_ADV_CLIP_emb/blob/63984deefb005da1ba90a1175e21d91040da38ab/adv_encode.py#L119-L215)).

### NovelAI's numbers are UI units, and SDXL's usable band is narrow and unmeasured

Four weighting formulas are in circulation:

| Implementation | What it does |
|---|---|
| ComfyUI | Interpolates toward or away from the empty prompt. |
| A1111 "Original" | Multiplies the hidden states, then restores the original mean. Its own comment calls this "likely not correct, but it seems to work well". A1111 also notes that "No norm" "seems to work better for SDXL" ([sd_emphasis.py](https://github.com/AUTOMATIC1111/stable-diffusion-webui/blob/82a973c04367123ae98bd9abdf80d9eda9b910e2/modules/sd_emphasis.py#L36-L54)). |
| compel | Up-weights the same way as ComfyUI and down-weights by blending in masked copies of the prompt ([compel syntax](https://github.com/damian0815/compel/blob/main/doc/syntax.md)). |
| ADV_CLIP_emb | Re-implements all of the above as selectable modes. |

AUTOMATIC1111 says NovelAI "uses my implementation from before 2022-09-29, except they have 1.05 as the multiplier" ([A1111 wiki](https://github.com/AUTOMATIC1111/stable-diffusion-webui/wiki/Features)). So NovelAI's CLIP-era emphasis was multiply-and-restore-mean with half of SD's 1.1 step. Mean restoration shrinks the other tokens a little. The same number is therefore milder under NovelAI's lineage than under ComfyUI's lerp, which BlenderNeko describes as "quite aggressively chasing concepts that are up-weighted" ([ADV_CLIP_emb README](https://github.com/BlenderNeko/ComfyUI_ADV_CLIP_emb)). At the other end of the scale, V4.5's "stable beyond 10" is a T5-era claim with no bearing on CLIP-L/G.

No maintainer guidance or measurement exists for Illustrious or NoobAI weight ranges. The community evidence is anecdotal: artist tags at 0.8, "as low as 0.2–0.3" for some, and `(carrot:0)` used to stop an artist name bleeding carrots into the image ([fluffscaler NoobAI guide](https://rentry.org/fluffscaler-noob)). Frying is consistently blamed on CFG, especially on v-pred. NovelAI's own v-pred V3 was coherent only at CFG 3.5–5 ([arXiv 2409.15997](https://arxiv.org/html/2409.15997)). An up-weighted positive token widens `cond − uncond` in its attended region, which acts as local extra CFG. NoobAI v-pred should therefore have a **lower ceiling** than Illustrious eps. RescaleCFG's std matching works on the model output and knows nothing about tokens ([nodes_model_advanced.py](https://github.com/Comfy-Org/ComfyUI/blob/v0.28.0/comfy_extras/nodes_model_advanced.py)). It may let pulsar's 0.55 rescale on NoobAI absorb slightly higher weights, but that is an inference for the sweep to settle. The community convention of "same number, different syntax", `{}`ⁿ = `(x:1.05ⁿ)`, is safe for small bracket weights. Nobody has published a calibration for the large numeric weights that V4.5/V5 users paste in.

## No public converter survives NovelAI's own edge cases

About fifteen converters exist. Five are maintained and handle V4+ numeric syntax, and **none implements the full grammar**. No surveyed tool implements "a bare `::` closes open brackets". Most leave Danbooru parentheses unescaped, and they disagree on what `[x]` is worth (0.95, 0.952, 0.9524, 0.952381 or 0.9) ([A1111 wiki](https://github.com/AUTOMATIC1111/stable-diffusion-webui/wiki/Features); [animerl](https://github.com/animerl/novelai-2-local-prompt/blob/main/javascript/naiprompt2webui.js); [yu mod.rs](https://github.com/eauesque/yu_ai_manager/blob/main/crates/yu-server/src/sd_nai/mod.rs); [RS converters.py](https://github.com/raspie10032/ComfyUI_RS_NAI_API_Request/blob/main/converters.py)).

| Tool | Licence | NAI → SD behaviour that matters |
|---|---|---|
| [animerl/novelai-2-local-prompt](https://github.com/animerl/novelai-2-local-prompt) | Unlicense | Port of A1111's scanner at 1.05 and 1/1.05; **escapes every paren**; flat output, 4 dp; pre-V4, so no `::`; stray closers kept as literal text |
| [da2el-ai/D2-nodes-ComfyUI](https://github.com/da2el-ai/D2-nodes-ComfyUI/blob/main/nodes/modules/nai_prompt_util.py#L77-L138) | MIT | Lookbehind `(?<![\w.])` protects `aaa5::`; drops negative spans by default; brace mode rounds to 1 dp and miscounts `{[x]}` as ≈1.1; no escaping |
| [eauesque/yu_ai_manager](https://github.com/eauesque/yu_ai_manager/blob/main/crates/yu-server/src/sd_nai/mod.rs) | MIT | Exact 1.05ⁿ at 6 dp; allows single colons in spans; keeps the sign of negatives; raw parens not escaped |
| [Mooshieblob1/MooshieUI](https://github.com/Mooshieblob1/MooshieUI/blob/main/src/lib/utils/promptWeightSyntax.ts) | AGPL-3.0 | Emits nested `((tag:1.05):1.05)`, which ComfyUI evaluates as **1.05**; uses 0.95 for `[]`; expects `hatsune_miku_(vocaloid)` to stay unescaped; shipped the digit-guard fix |
| [raspie10032/ComfyUI_RS_NAI_API_Request](https://github.com/raspie10032/ComfyUI_RS_NAI_API_Request/blob/main/converters.py) | GPL-3.0-only | Stack model; stray `}` applied **backward**; 0.95; drops text after a closing `::`; escapes on output |
| [bedovyy/ComfyUI_NAIDGenerator](https://github.com/bedovyy/ComfyUI_NAIDGenerator/blob/master/utils.py#L180-L198) | GPL-3.0 | SD → NAI only; **linear** brace count, so `(x:1.5)` becomes 10 braces ≈ 1.63; once a negative weight appears, numeric mode sticks for the rest of the list |
| [contrataco/novelai-powertool](https://github.com/contrataco/novelai-powertool/blob/main/app/providers/comfyui-prompt.js#L114-L131) | MIT | Numeric regex has no minus, so `-1::hat::` loses its sign |

The frontend evidence settles questions the converter authors had to guess at. Stray closers act **forward**, as the docs' "to their right" says; the backward reading (RS) and the literal reading (animerl) are both wrong. A new `w::` opened while another section is open **replaces** the weight rather than nesting. Weights **cross newlines**, where D2 assumes they do not. The ComfyUI-side lessons follow from the parser. Emit **flat** `(text:w)` with the product precomputed, because nested explicit weights are overwritten ([sd1_clip.py](https://github.com/Comfy-Org/ComfyUI/blob/v0.28.0/comfy/sd1_clip.py#L348-L366)). Escape every literal paren. Never let a span end in digits right before `::` when writing NAI syntax; D2, bedovyy and yu use a space guard (`text ::`), and NovelAI's own examples do the same.

On licensing: animerl (Unlicense), the D2 tools and yu (MIT) can be ported. ComfyUI (GPL-3.0), A1111 (AGPL-3.0), MooshieUI (AGPL-3.0), RS and bedovyy (GPL-3.0) can be read as behavioural references but not copied ([converter licences](https://github.com/eauesque/yu_ai_manager); [ComfyUI](https://github.com/Comfy-Org/ComfyUI)). **The pulsar repo has no LICENSE file** (checked), so the distribution question is open. The algorithm is about 80 lines and fully specified below, so pulsar should write it from the spec and use MooshieUI's and yu's test cases only as ideas.

## Pulsar's filter reads a different prompt than ComfyUI encodes

Today every prompt field is an opaque comma-joined string from request to ComfyUI.

- **Assembly.** `assembly.Build` joins quality tags, style prefix, user prompt and style suffix with `", "`. It builds the negative as safety, then `DEFAULT_UC`, then the user negative, then the style negative, with no parsing ([assembly.go:448–474](../core/internal/assembly/assembly.go)).
- **Prompt check.** `CheckPrompt` scans `spec.Prompt` and the character prompts and **never scans `spec.Negative`**. It splits on every comma and tests `tags.Normalize(part) == w` ([sentinel.go:64–78](../core/internal/compliance/sentinel.go)). `Normalize` lowercases and collapses whitespace but strips no punctuation, so `(`, `{`, `\` and `:w` all break equality ([tags.go:30–34](../core/internal/tags/tags.go)).
- **Watch list.** The list is `ParseForbidden(FORBIDDEN_TAG_UC)`, **env only** ([sentinel.go:52](../core/internal/compliance/sentinel.go)). The reference-fetch gate, by contrast, uses the union of a hard-coded floor and the env value, because "Config can only ADD tags" ([live.go:21–42](../core/internal/tags/live.go)).
- **Inputs already relying on ComfyUI syntax.** The style seed relies on ComfyUI's grammar (`(logo:1.3)`) ([0008_style_presets.sql:44](../db/migrations/0008_style_presets.sql)). The embeddings directory is configured ([extra_model_paths.yaml:9](../comfy/extra_model_paths.yaml)). `autoAnchor` appends design tags without escaping ([api.go:742–792](../core/internal/api/api.go)).
- **History.** orbit's history restore copies the stored negative, **with FORBIDDEN_TAG_UC and DEFAULT_UC already at its head**, into the user-editable UC box. It also re-splits the prompt on raw commas ([usePresets.ts:131–146](../orbit/app/composables/usePresets.ts)).

ComfyUI parses `( )`, `:w`, `\(` and `embedding:` in all of these strings, so the encoder sees a different text from the one the filter sees. That gap is the root cause of every issue below.

No paper documents weighting syntax specifically as a filter-evasion technique. The classes follow directly from the two grammars and are described here only at class level:

- **Wrappers.** Parens, weights, braces, escapes and comma-spanning groups launder a watched tag past exact matching while the encoder still receives it.
- **Down-weighting.** Down-weighting does not launder, because the causal attention leak keeps the concept.
- **Complement steering.** A negative weight on an adult-signal tag, adult-signal tags placed in the negative, or amplified age-adjacent descriptors shift the age distribution without naming any watched tag.
- **The mandatory negative.** It can be attacked with an inverted copy of a safety term in the user negative, or by capturing a group if system text were ever unbalanced.
- **`embedding:`.** An embedding reference splices in vectors that the filter knows only by filename.

The research is unambiguous that prompt-level defences are steering, not gates. In Ring-A-Bell, attack success against a fixed safety negative rose from **4.21% to 34.74–49.47%**, and adding a post-hoc safety checker cut it to **8.42–16.84%** ([Ring-A-Bell](https://arxiv.org/html/2310.10012v4)). MMA-Diffusion calls prompt filtering insufficient "as a standalone defense" ([arXiv 2311.17516](https://arxiv.org/abs/2311.17516)). Low-effort reframing reached **74.47%** success ([arXiv 2604.01888](https://arxiv.org/html/2604.01888v1)).

Real systems restrict weights at the boundary but do not parse with the encoder's grammar:

- **Civitai** matches with boundary regexes and audits the negative separately, but strips only unsigned decimal weights ([civitai audit.ts](https://raw.githubusercontent.com/civitai/civitai/main/src/utils/metadata/audit.ts)).
- **Midjourney** requires weights to sum to a positive number ([Midjourney docs](https://docs.midjourney.com/docs/multi-prompts)).
- **NVIDIA's hosted SDXL** accepts only weights of 1.0 and −1 ([NVIDIA NIM](https://docs.api.nvidia.com/nim/re/reference/stabilityai-stable-diffusion-xl-infer)).

Pulsar can do better than all of them because it owns the single encoder and can parse the exact bytes ComfyUI will parse. The same fix also corrects the mis-weighting of every `name (series)` tag. It also stops weighted character tags from silently losing auto-anchoring, which they would otherwise do because `autoAnchor` matches raw comma pieces against the DB.

## Conclusion

The design follows from NovelAI's own history. NovelAI kept one surface syntax across CLIP, T5 and Qwen and recalibrated the magnitudes underneath each time. A faithful port therefore copies the **arithmetic of the syntax** exactly and treats the resulting number as an input to pulsar's own per-lineage calibration, never as a ComfyUI weight. The converters fail in both directions. Some copy the numbers and break on the syntax: stacks, ×0.95, nested output. Others approximate the syntax and pass the numbers straight through, including `-6` and `10`.

The more consequential finding is that **the transpiler is pulsar's compliance fix even if no user ever pastes a NovelAI prompt**. The parser differential between `CheckPrompt` and ComfyUI exists today. The durable cure is to make the checker re-parse the exact emitted bytes with a port of ComfyUI's grammar and assert that the result equals the structured tags. That turns "trust the emitter" into a checked invariant. Rejecting non-positive weights costs pulsar almost nothing, because SDXL has no faithful meaning for them and negative intent already has a home in the UC box. It also removes the complement-steering and inverted-safety-copy classes without any measurement. The output classifier stays the gate. Everything above narrows what reaches it and never substitutes for it.

## Specification: `core/internal/prompt` and a check that reads what ComfyUI reads

### S1. Placement and call order

`core/internal/prompt` is pure and **stdlib-only** (`strings`, `strconv`, `math`, `unicode`). It has no I/O and no DB access, and it never imports `tags`, which pulls in pgx. `assembly` imports it. That adds no I/O to `assembly`, which therefore stays pure. `compliance` and `api` import it too. `tags.Normalize` moves into `prompt.Normalize`, and `tags.Normalize` becomes a one-line delegate so that one normalisation exists.

| File | Contents |
|---|---|
| `nai.go` | `Parse(s string) (Surface, error)`: the NovelAI running-weight scanner (S2) and validation (S3) |
| `calibrate.go` | `Calibration`, `(Calibration).Map(w float64) float64` (S4) |
| `emit.go` | `EmitComfy([]Tag, Calibration) string` and `FormatNAI([]Tag) string` (S5) |
| `comfy.go` | `ComfyParse(s string) ([]Tag, error)`: a port of ComfyUI v0.28.0 `escape_important` → `parse_parentheses` → `token_weights` → `unescape_important`, plus `embedding:` detection |
| `normalize.go` | `Normalize`, plus the `MatchForms` variants used by compliance (S7) |

Call order in `POST /generate` (handler comment at [api.go:115–116](../core/internal/api/api.go) fixed in the same change):

| Step | Call | Failure |
|---|---|---|
| 1 | decode request (unchanged) | 400 |
| 2 | `assembly.ParseRequest(req)` → `prompt.Parse` on `Prompt`, `Negative`, each `Characters[i].Prompt`, each a **separate surface with the running weight reset to 1.0** | 400 with code and span |
| 3 | style lookup → `prompt.Parse` on `PromptPrefix`, `PromptSuffix`, `Negative` (config rules, S3) | 500, logged; never silently skipped |
| 4 | `autoAnchor(parsed)` resolves `Tag.Name`; anchors are appended as `Tag{Source: Anchor}` at weight 1 | as today |
| 5 | `assembly.BuildParsed(parsed, opts)`: calibrate, dedupe the negative, emit, fill the structured `Spec` | 400 |
| 6 | `Checker.CheckPrompt(spec)`: re-parse the emitted strings, apply the S7 policy | 422 reject, 503 error (fail closed) |
| 7 | workflow patch of nodes 2, 3, 32, 36, 42 with the emitted strings (unchanged, so no node-ID or template changes) | |
| 8 | enqueue; the worker's `CheckOutput` is unchanged and **remains the gate** | `blocked` |

`assembly.Build(req, opts)` stays as `BuildParsed(ParseRequest(req), opts)` so the existing tests keep working. `QUALITY_TAGS`, `DEFAULT_UC` and `FORBIDDEN_TAG_UC` are parsed **at boot**, and any error refuses boot. That is the same fail-closed pattern `cmd/server` already uses for FORBIDDEN_TAG_UC presence ([main.go:58–64](../core/cmd/server/main.go)).

In `POST /translate`, `prompt.Parse` runs **before** nltag. The text at weight 1 goes through `Decompose`/`Ground` as today. Each maximal run of equal non-1 weight is grounded separately, and its grounded tags inherit that weight. The LLM never sees operators and never invents weights ([tag-grounding SKILL.md:120–121](../.claude/skills/tag-grounding/SKILL.md)). The response carries tags with weights plus `prompt.FormatNAI(tags)`, so orbit never builds syntax itself. An optional `POST /prompt/parse` returns runs, NAI weights, lineage-emitted weights and notices for orbit's live highlighter, so orbit does not need a second TypeScript parser.

### S2. Grammar (NovelAI frontend semantics, plus three pulsar lexer rules)

```ebnf
surface   = { op | escparen | char } ;
op        = "{" | "}" | "[" | "]" | colons ;
colons    = [ num ] "::" ;                 (* num is matched against the SOURCE text, see below *)
num       = [ "-" ] { digit } [ "." { digit } ] ;   (* FE regex /-?\d*\.?\d*$/ *)
escparen  = "\" ( "(" | ")" ) ;            (* pulsar: SD-habit escape, yields a literal paren *)
char      = ? any other rune; "(" ")" ":" are literal ? ;
separator = "," | "\n" ;                   (* tag boundary; weight carries across both *)
```

```go
// Scanner state per surface: w starts at 1.0; it never carries between surfaces.
for i := 0; i < len(rs); i++ {
    switch c := rs[i]; {
    case c == '\\' && next(rs, i) in {'(', ')'}: continue // literal paren follows
    case c == '{' || c == ']': w *= 1.05; brace/bracket counters for notices only
    case c == '}' || c == '[': w /= 1.05
    case c == ':' && next(rs, i) == ':':
        if next2(rs, i) == ':' { return ErrP006 }        // ":::" unverified on NAI
        num := longestSuffix(rs[:i], `-?\d*\.?\d*$`)     // SOURCE text, leftmost start
        drop len(num) runes from the text buffer          // they are always plain text
        switch num {
        case "":             w = 1                        // bare "::" = reset (closes brackets)
        case "-", ".", "-.": w = 0                        // FE special case -> P001 later
        default:             w = parseFloat(num)          // ErrRange/±Inf -> P002
        }
        lint P003 (glued number), N3/N4 (FE hints); reset notice counters; i++
    default:
        append (c, w) to the text buffer
    }
}
// Then split the buffer at separators into tags. Inside a tag, consecutive runes
// with equal w form Runs. Trim tag edges, collapse internal whitespace to one
// space, drop empty tags.
```

The number is matched against the source, not the buffer. In `1{.5::` the frontend reads `.5`, because `{` is not in the number class. Every rune of the matched number is plain text that the scanner has already appended, so removing `len(num)` runes from the buffer is exact. Commas and single colons have no syntactic role. `artist:foo`, `rating:general` and `honkai: star rail` stay literal. `(` and `)` are always literal, as they are on NovelAI. Weight can change in the middle of a tag (`blue {hair}`), which is why a `Tag` holds `Runs`.

### S3. Validation, rejection and notices

All codes apply to user surfaces. Config surfaces (style rows, `QUALITY_TAGS`, `DEFAULT_UC`) use the same parser and the same rejections, except that the upper bound is 2.0 in emitted units (see S4). **FORBIDDEN_TAG_UC must contain no operator at all**: no `{ } [ ]`, no `::`, and every tag at weight exactly 1. Otherwise boot is refused. Weight checks apply to runs that contain non-space text. Transient values such as `{{{{` followed directly by `::` are ignored.

| Code | Condition | Why | Message hint |
|---|---|---|---|
| P001 `weight_nonpositive` | any run with w ≤ 0 (includes `-1::`, `0::`, `-::`, `.::`) | No faithful SDXL meaning (the lerp reflects the token and the concept survives); complement-steering vector; inverted safety copies | "Put it in Undesired Content instead" |
| P002 `weight_too_high` | any run with w > 10, or a parse range error | NovelAI's stated stable range; catches `year 2000::` | "Did you mean `year 2000 ::` (space before `::`)?" |
| P003 `glued_number` | non-empty `num` immediately preceded by a letter, digit, `_` or `.` (`coupe50::`, `type-2::`, `1.2.3::`) | NovelAI silently reads the digits as a weight ([MooshieUI PR #937](https://github.com/Mooshieblob1/MooshieUI/pull/937)) | "Add a space before `::`" |
| P006 `triple_colon` | `:::` | NovelAI's reading is unverified | "Use `::` or a single `:`" |
| P007 `embedding` | case-insensitive `embedding:` anywhere | ComfyUI's textual-inversion loader | "Embeddings aren't supported" |
| P008 `sd_weight_syntax` | a literal `( … )` group where ComfyUI would read a weight: colon at index > 0 and a suffix that `float()` accepts | The user typed A1111/ComfyUI syntax, which would otherwise reach CLIP as junk tokens | "Use `1.2::text ::`" |
| P009 `pipe` | `\|` | NovelAI character separator or V3 prompt mixing; no pulsar meaning | "Use character boxes" |
| P010 `extra_network` | `<` + letters + `:` (`<lora:`, `<lyco:`) | LoRA allowlist | "Use the LoRA picker" |
| P011 `control_char` | C0 controls other than `\n`/`\t`, or U+FFFD | ComfyUI uses `\0\1`/`\0\2` as escape sentinels | "Remove control characters" |
| P012 `too_long` | more than 512 tags or 16 KiB per surface | bounds the work in steps 2–6 | |

The following are **notices, not errors**, matching NovelAI's frontend, which warns rather than refuses. They are returned in the `/generate` response and by `/prompt/parse`.

| Notice | Condition |
|---|---|
| N1 `open_at_end` | an unclosed `{` or `[` reaches the end of the surface (legal on NovelAI; flagged) |
| N2 `stray_closer` | `}` or `]` without an opener since the last reset (`{{rain::}}`, `{1.8::x}`); it shifts everything after it |
| N3 `spaced_number` | the frontend's error 1: a bare `::` at w = 1 preceded by `<num><space>` with \|n\| < 21 |
| N4 `number_after` | the frontend's error 2: `::` followed by `<num>` and then a space, a comma or the end, with \|n\| < 21 |
| N5 `calibrated` | \|Map(w) − w\| > 0.05, i.e. the weight was compressed for this lineage |

Unbalanced brackets are **accepted** with a notice, not rejected. Each surface resets to 1.0, and every tag is emitted as its own balanced group (S5), so no user text can capture or re-weight system text, the safety block included. Accepting them keeps NovelAI's documented idioms (`{{{{{rain ::`) working.

### S4. Weight math and per-lineage calibration

The scanner yields NAI-unit weights `w ∈ (0, 10]`. Emission maps them through a per-lineage curve. Above 1 the curve saturates; it is close to the identity near 1 and bounded by `Hi`. Below 1 it is linear down to a floor `Lo`. The upward side needs saturation because NovelAI's input range is wide (up to 10); the downward side is naturally bounded by 0.

```go
type Calibration struct{ KUp, Hi, KDown, Lo float64 } // lives in assembly.lineageSpecs

func (c Calibration) Map(w float64) float64 {
    switch {
    case w > 1:
        return 1 + (c.Hi-1)*math.Tanh(c.KUp*(w-1)/(c.Hi-1))
    case w < 1:
        return math.Max(c.Lo, 1-c.KDown*(1-w))
    }
    return 1
}
// Code-clamped bounds, enforced at init like the sentinel's thresholds:
// Hi ∈ [1.0, 1.6], Lo ∈ [0.1, 1.0], KUp, KDown ∈ [0, 2].
// Invariant: Map(w) ∈ [Lo, Hi] ⊂ (0, 1.6], Map(1) = 1, monotone non-decreasing.
```

| Lineage (`lineageSpecs` key) | KUp | Hi | KDown | Lo | Basis |
|---|---|---|---|---|---|
| `illustrious` (eps, CFG 6) | 1.0 | 1.4 | 1.0 | 0.3 | Starting point only; community up-weights rarely exceed ~1.4 |
| `noobai` (v-pred, CFG 5, rescale 0.55) | 1.0 | 1.3 | 1.0 | 0.3 | v-pred is more CFG-sensitive, so expect a lower ceiling |

These are **initial values to be replaced by the S10 sweep**. They are not sourced limits. With them, the NovelAI bracket range transfers almost unchanged, matching the community `(x:1.05ⁿ)` convention, while V4.5-style large weights saturate:

| NAI weight | 1.05 | 1.1025 | 1.1576 | 1.2763 | 1.5 | 2.1 | 3 → 10 | 0.952 | 0.71 | 0.1 |
|---|---|---|---|---|---|---|---|---|---|---|
| `illustrious` | 1.0497 | 1.1003 | 1.1499 | 1.2394 | 1.3393 | 1.3967 | 1.4 | 0.9524 | 0.71 | 0.3 |
| `noobai` | 1.0495 | 1.0987 | 1.1446 | 1.2179 | 1.2793 | 1.2996 | 1.3 | 0.9524 | 0.71 | 0.3 |

Config surfaces are written in **emitted units** with identity calibration, bounded to (0, 2.0], because operators tune style rows per lineage. The safety block is always emitted at exactly 1.0. Changing its weight is an eval question (S11), not a transpiler decision. `Spec` records the `Calibration` used, so history shows why a weight came out as it did.

### S5. Emission format for ComfyUI

| Rule | Detail |
|---|---|
| One group per run | A run whose formatted weight is `"1"` is emitted bare. Any other run becomes `(` + text + `:` + W + `)`. Never nested, never bare `()`, never a group spanning a comma. |
| Weight format | `strconv.FormatFloat(w, 'f', 4, 64)` with trailing zeros and dot trimmed. Never an exponent, NaN, Inf or `-0`. |
| Escaping | Every literal `(` → `\(` and `)` → `\)`. ComfyUI has no other escapes, and `{ } [ ]` can never reach the emitter because the NovelAI dialect consumes them. |
| Whitespace | Leading or trailing spaces of a weighted run move outside its parens. If the previous emitted rune is `\` and a group opens next, insert one space, so `\` never escapes the opener. |
| Joining | Tags are joined with `", "` and segments in the existing order: positive = quality → style prefix → user → style suffix; negative = **safety** → DEFAULT_UC → user → style negative ([assembly.go:448–474](../core/internal/assembly/assembly.go)). |
| Negative dedupe | Drop any later negative tag whose `MatchForms` equal a tag in the safety or DEFAULT_UC block. This neutralises the history round-trip and keeps the parsed safety block the only copy. |

| Parsed tag (text @ emitted) | Emitted string |
|---|---|
| `castorice (honkai: star rail)` @ 1 | `castorice \(honkai: star rail\)` |
| `castorice (honkai: star rail)` @ 1.2 | `(castorice \(honkai: star rail\):1.2)` (ComfyUI: whole tag 1.2) |
| `artist:foo` @ 0.8 | `(artist:foo:0.8)` (`rfind` takes the last colon) |
| `blue` @ 1 + `hair` @ 1.0497 | `blue (hair:1.0497)` |
| `:3` @ 1.1 | `(:3:1.1)` |
| `year 2000` @ 1.3393 | `(year 2000:1.3393)` |

`FormatNAI` is the reverse emitter for orbit and `/translate`. It writes `w::text ::` with a **space guard** before the closing `::`, writes bare text at weight 1, and must satisfy `Parse(FormatNAI(t)) ≡ t`.

### S6. Data model

```go
package prompt
type Source uint8 // Safety, DefaultUC, Quality, StylePrefix, User, StyleSuffix, StyleNegative, Anchor
type Run struct {
    Text    string  `json:"text"`    // unescaped literal text
    Weight  float64 `json:"w"`       // NAI units as parsed
    Emitted float64 `json:"emitted"` // after Calibration.Map (identity for config)
}
type Tag struct {
    Runs   []Run  `json:"runs"`
    Text   string `json:"text"`   // runs concatenated, whitespace-collapsed
    Name   string `json:"name"`   // Normalize(Text): lowercase, whitespace -> "_"
    Source Source `json:"source"`
}
type Notice struct{ Code string; Start, End int } // rune offsets into the surface
```

`assembly.Spec` keeps `Prompt`, `Negative` and `Characters[].Prompt` as **the emitted ComfyUI strings**, so workflow patching is unchanged. It adds `PromptTags`, `NegativeTags`, `Characters[].Tags` and `Calibration`. The job row stores `Spec` as JSON already, so no SQL migration is needed. "Same Seed + same Spec must reproduce" still holds, because the stored strings are what the worker sends.

### S7. Compliance: `CheckPrompt` on parsed tags of every string ComfyUI receives

| Step | Rule |
|---|---|
| 1. Surfaces | Exactly the strings patched into CLIPTextEncode `text` inputs: `spec.Prompt` (node 2), `spec.Negative` (node 3), each `spec.Characters[i].Prompt` (nodes 32/36/42). Hires reuses 2 and 3. A missing structured field returns an error (503, fail closed). |
| 2. Re-parse | `prompt.ComfyParse(s)`, the exact encoder grammar, gives per-rune weights. Split at commas into tags. Any `embedding:` word → reject. |
| 3. Differential check | The re-parsed tags must equal the structured tags: same boundaries, same non-space runes, emitted weights equal within 5e-5. **Mismatch → reject.** The checker never trusts the structured fields alone. |
| 4. Safety position | The first `len(safety)` tags of the parsed negative must equal `ParseForbidden(FORBIDDEN_TAG_UC)` in order, at weight 1.0. Otherwise reject. |
| 5. Watch set | `tags.WatchSet(env)` = hard-coded floor ∪ `FORBIDDEN_TAG_UC`, one function shared by `NewLive` and `NewSentinel`. It also widens the classifier's `include=` list ([sentinel.go:52](../core/internal/compliance/sentinel.go); [reference.go:17–24](../core/internal/tags/reference.go)). |
| 6. Match forms | For each tag: `Name`; `Name` with `( ) { } [ ] \ " '` stripped; that form with `_` removed. Compare against the watch entries and their `_`-removed forms. Second net, enabled after the collision review in S11: a watch entry's `_`-word sequence appearing contiguously in a tag's word sequence. `lolita_fashion` still passes, as [sentinel_test.go](../core/internal/compliance/sentinel_test.go) requires. |
| 7. Policy, positive and characters | A watched tag at **any** weight → reject (weight never launders). A complement-list tag with any run emitted below 1 → reject. |
| 8. Policy, negative | A watched tag outside the safety block → reject (dedupe removed legitimate copies, so one remaining is a bypass or a bug). A complement-list tag anywhere outside the safety block → reject. |
| 9. Complement list | Code floor ∪ env, presence enforced at boot like FORBIDDEN_TAG_UC, never toggleable off. Its contents are a product and safety decision (S11) and are not reproduced here. |

None of these rules is env-toggleable. Thresholds tune only within code-clamped bounds. A pass means "no known pre-filter hole", never "safe". `CheckOutput` stays the gate ([worker.go:110](../core/internal/worker/worker.go); [docs/compliance.md](../docs/compliance.md)).

### S8. Existing gaps and their fixes

| Gap | Where | Fix |
|---|---|---|
| Negative prompt never scanned | [sentinel.go:64–68](../core/internal/compliance/sentinel.go) | S7 steps 1, 4 and 8 |
| Exact match on raw comma pieces; `Normalize` keeps punctuation | [sentinel.go:69–78](../core/internal/compliance/sentinel.go), [tags.go:30–34](../core/internal/tags/tags.go) | `ComfyParse` + match forms (S7 steps 2, 6) |
| Watch list env-only | [sentinel.go:52](../core/internal/compliance/sentinel.go) | `tags.WatchSet` union (S7 step 5) |
| `embedding:` reaches a configured TI directory | [extra_model_paths.yaml:9](../comfy/extra_model_paths.yaml) | P007 on every surface, including config; `ComfyParse` rejects it too |
| Unescaped parentheses (`name_(series)` weighted ×1.1, parens dropped) | plain mode and [autoAnchor api.go:742–792](../core/internal/api/api.go) | Emitter escaping (S5); `autoAnchor` appends `Tag`s, not strings |
| Weighted character tags stop anchoring | [api.go:259–286](../core/internal/api/api.go) | `autoAnchor` on parsed `Tag.Name` (S1 step 4) |
| History round-trip re-sends the safety negative as user text and tears comma groups | [usePresets.ts:131–146](../orbit/app/composables/usePresets.ts), [HistoryDetail.vue:54–55](../orbit/app/components/HistoryDetail.vue) | Restore the UC box from `negative_tags` where `source == user`; display from tags; core dedupe (S5) covers legacy rows |
| `Live.Ground` can return a watched tag | [live.go:53–75](../core/internal/tags/live.go) | Refuse `WatchSet` members (defence in depth) |
| Style seed uses ComfyUI syntax | [0008_style_presets.sql:44](../db/migrations/0008_style_presets.sql) (still untracked) | Rewrite as `1.3::logo ::` before the migration is committed; otherwise add 0009 |
| Safety-first tested only at string level | [assembly_test.go:215–247](../core/internal/assembly/assembly_test.go) | Assert on parsed `NegativeTags` (S9) |
| Docs teach `(tag:1.3)` | [docs/tagdb.md:87–88](../docs/tagdb.md), [docs/tuning.md:160](../docs/tuning.md) | Teach `1.3::tag ::`; add parse-before-check and negative scanning to `docs/compliance.md` |

### S9. Tests

The `prompt` golden table for `Parse` (one surface; text @ NAI weight):

| # | Input | Expected |
|---|---|---|
| 1 | `{{{x}}}` | x @ 1.157625 |
| 2 | `{[x]}` | x @ 1.0 |
| 3 | `{a, [b], c` | a @ 1.05, b @ 1.0, c @ 1.05; N1 |
| 4 | `a}, b` | a @ 1, b @ 0.952381; N2 |
| 5 | `1girl, 1.5::rain, night ::, 0.5::coat ::, black shoes` | rain, night @ 1.5; coat @ 0.5; others @ 1 |
| 6 | `{1.8::holding hands}, smile` | holding hands @ 1.8; smile @ 1.714286; N2 |
| 7 | `[0.71::long legs], smile` | long legs @ 0.71; smile @ 0.7455; N2 |
| 8 | `1.5::{red hair}::, smile` | red hair @ 1.575; smile @ 1 |
| 9 | `{{{{{rain ::, smile` | rain @ 1.276282; smile @ 1; no notice |
| 10 | `{{rain::}}, smile` | rain @ 1.1025; smile @ 0.907029; N2 |
| 11 | `2::a, {b}, c::, d` | a @ 2, b @ 2.1, c @ 2, d @ 1 |
| 12 | `1.5::year 2000::, smile` | **P002** |
| 13 | `1.5::year 2000 ::, smile` | year 2000 @ 1.5; smile @ 1 |
| 14 | `1.2::artist:coupe50::` / `1.2.3::hat::` | **P003** |
| 15 | `-1::hat ::` / `0::hat::` / `-::hat::` | **P001** |
| 16 | `.5::coat::` | coat @ 0.5 |
| 17 | `10::x::` / `11::x::` / 48 × `{` + `x` | x @ 10 / **P002** / **P002** (1.05⁴⁸ = 10.40) |
| 18 | `blue {hair}, x` | one tag, runs `blue` @ 1 and `hair` @ 1.05; x @ 1 |
| 19 | `1girl,\n{red eyes\nsmile` | red eyes @ 1.05 and smile @ 1.05 (carries across newline); N1 |
| 20 | `thin legs ::, x` | both @ 1 (no-op reset, NovelAI's own example) |
| 21 | `1.5 ::hat::` | `1.5 hat` @ 1; N3 |
| 22 | `castorice (honkai: star rail), 1girl` | parens literal, @ 1 |
| 23 | `makima \(chainsaw man\)` | `makima (chainsaw man)` @ 1 |
| 24 | `(blue hair:1.2)` | **P008** |
| 25 | `Embedding:Foo`, `a \| b`, `<lora:x:1>`, `a:::b`, NUL | **P007**, **P009**, **P010**, **P006**, **P011** |

The `ComfyParse` parity tests use a JSON fixture produced by an `eval/` script. The script extracts `parse_parentheses`/`token_weights`/`escape_important` from the pinned `comfy/ComfyUI` checkout with `ast` and runs them as-is. The fixture records the ComfyUI commit, and CI fails if the checkout moves without a regenerated fixture. The cases are taken from the ComfyUI analysis:

- `castorice (honkai: star rail)` → series @ 1.1, parens dropped
- `((blue hair:1.5))` → 1.5; `((blue hair):1.5)` → 1.65
- `(a:1.2:1.3)` → `a:1.2` @ 1.3; `(:1.5)` → literal `:1.5` @ 1.1
- `close) (a:1.2)` → whole rest literal
- `(x:nan)`, `(x:inf)`, `(x:1e1)`, `(x: 1.3 )`
- escaped forms

Property and fuzz tests in `prompt`, all pure:

- `FuzzEmitRoundTrip`: random Unicode with parens, colons, backslashes and digits; any input that `Parse` accepts must satisfy `ComfyParse(EmitComfy(t)) ≡ t`.
- `FuzzParse`: never panics, and stays linear on 1 MiB of `{`.
- `Parse(FormatNAI(t)) ≡ t`.
- `Calibration.Map`: bounded in `[Lo, Hi]`, monotone, `Map(1) = 1` for every lineage.

`assembly` tests:

- **Safety-first invariant.** Extend `TestForbiddenTagUCIsUnconditional` so the first `len(safety)` entries of `NegativeTags` have `Source = Safety`, emitted weight 1 and names equal to `ParseForbidden`, and `ComfyParse(spec.Negative)` agrees. Run it under every request shape: an empty negative; a user negative holding safety copies at 1.05 and 0.952; a stray `}` or unclosed `{` at the start of the user negative; UC preset off; a style negative present; a history-restored negative.
- **Boot refusals.** FORBIDDEN_TAG_UC containing any operator fails boot.
- **Per-surface reset.** An unclosed `{` in the user prompt leaves the style-suffix weights at 1.

`compliance` tests use a placeholder fixture list (`testwatch`, `test_watch_two`) and a placeholder complement tag (`testadult`), never real terms:

- **Reject a watched tag in every form.** Bare; inside NAI braces or a numeric section; with escaped parens; split by an operator inside the word; in a character prompt; in the negative outside the safety block.
- **Allow it in the safety block.** A watched tag inside the safety block passes.
- **Complement tag.** In the user negative → reject. In the positive with `[ ]` → reject. With `{ }` → allow.
- **Differential.** A hand-built `Spec` whose structured tags disagree with its string → reject.
- **Floor term.** A floor term missing from env → still rejected.

`api` e2e tests:

- **Text inputs.** Every CLIPTextEncode `text` input across `workflows/*.json` and the patches maps to a field that `CheckPrompt` scans. This guards future patch points.
- **Anchoring.** A weighted character tag still anchors.
- **History.** History returns only the user negative.
- **Notices.** The `/generate` response carries notices.

### S10. Calibration eval

Sweep layout (a new directory, `eval/sweeps/weighting/`):

| Dimension | Values |
|---|---|
| Lineage | `illustrious`, `noobai` |
| Target tags | 12 well-grounded tags (post count far above floor) across colour, clothing, composition and artist |
| Emitted weights | {0.3, 0.5, 0.7, 0.9, 1.0, 1.05, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6} |
| CFG | each lineage's default |
| Rescale (`noobai` only) | 0.4, 0.55, 0.7 |
| Seeds | 8 fixed |

Metrics:

- **Adherence.** The sentinel tagger's probability for the target tag (`/classify?include=`).
- **Overcook.** Saturation and clipped-pixel fraction against the weight-1 baseline.
- **Stability.** Image-embedding similarity to the weight-1 baseline.

Fitting rules:

- **`Hi`** is the largest emitted weight at which overcook stays within a fixed tolerance of baseline on at least 7 of 8 seeds.
- **`Lo`** is the smallest weight that still moves adherence monotonically.
- **`KUp`** is set so that NAI `{{x}}` (1.1025) produces a perceptible but sub-overcook adherence gain.

NovelAI-equivalent visual strength cannot be measured without NovelAI outputs, and V5 is a different model anyway. The target is monotone, perceptible and not fried. Rejecting non-positive weights makes any safety-content sweep of inverted safety copies unnecessary. Do not add one.

### S11. Open questions and product decisions (one batch)

| # | Decision | Default in this spec | Cost of the default |
|---|---|---|---|
| 1 | Negative weights from pasted V4.5/V5 prompts (`-1::hat ::`) | **Reject (P001).** The alternative is relocating `-w::x::` spans into the negative after the system blocks, subject to S7 step 8. | Pasted V5 prompts get a 400 with a hint |
| 2 | SD/A1111 `(x:1.2)` input | **Reject (P008) with a conversion hint**; one user dialect only | SD users must retype weights |
| 3 | NovelAI `\|` multi-character syntax | **Reject (P009)**; could be transpiled into pulsar's 2–3 character boxes | Pasted multi-character prompts get a 400 |
| 4 | Calibration starting values (`Hi` 1.4/1.3, `Lo` 0.3, `K` 1.0) and whether power users may see emitted weights | Ship as initial values; S10 replaces them | Big NAI weights saturate (N5) |
| 5 | Prompt watch set = floor ∪ env. The floor includes broad terms that will reject some SFW scenes. | **Union** (fail closed) | SFW false positives |
| 6 | Enable the word-boundary net (S7 step 6) | Only after running it over the whole `tags` table and reviewing the collision list | Possible compound-tag false positives |
| 7 | Contents of the complement list; whether the "below 1 in positive" rule applies on rating-general requests | Applies to every request | False positives on legitimate down-weighting |
| 8 | Unbalanced brackets: notices or errors | **Notices** (NovelAI parity) | Users can still shoot themselves in the foot |
| 9 | Weight of the safety block | **1.0, unchanged**; any change only after eval | None |
| 10 | Licence: pulsar has no LICENSE file | Write from the spec; copy no GPL/AGPL code | None |
| 11 | Re-running old history through the new emitter changes outputs (escaping fixes `name (series)`) | Accept as a correctness fix; replays from stored `Spec` are unaffected | Old seeds don't reproduce via re-assembly |
