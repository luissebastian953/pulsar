# Plug the PNG leak before cloning NovelAI

Before pulsar copies anything from NovelAI, it has to stop shipping its own recipe. ComfyUI runs without `--disable-metadata`, and core passes ComfyUI's output bytes through untouched. So every PNG pulsar delivers carries the full patched API graph in a `prompt` tEXt chunk: the complete `FORBIDDEN_TAG_UC` block, `DEFAULT_UC`, quality and style text, checkpoint and LoRA filenames, and the **UUIDs of the user's reference and img2img uploads**. Anyone who can reach core can fetch those uploads from the unauthenticated `GET /images/{id}`. **65 of 99 stored blobs carry the chunk, and all 24 leaked upload IDs still resolve** ([read-only scan of core/data, 2026-11-15](../core/data); [worker.go:101–131](../core/internal/worker/worker.go)). That is step 0. It needs only one confirmation: a launch flag plus a pure, byte-level PNG chunk allow-list run in the worker and again on the serve path, so pixels stay bit-identical to what `CheckOutput` classified.

NovelAI's Prompt Chunks, read from its production bundle, are client-side text macros. `@` only opens a picker. The picked chunk becomes an atomic pill, serialised as `⌜macro:<uuid>⌟`. Nesting uses a typeable `!macro:Name!`. Everything is expanded by plain substitution **at send time**, so the API, the history and the PNG metadata only ever see expanded text. The Settings tab beside the chunk list holds two toggles. One is a per-browser flag that silences only Danbooru autocomplete. The other is an account setting, on by default, that paints a running emphasis weight: orange up, blue down, green `::`.

novelai.net/inspect is a read-only, browser-only page. It reads PNG text, then EXIF UserComment, then an alpha-channel `stealth_pngcomp` payload. It shows nothing unless a `Comment` JSON key exists. It shows a ✓ only when an ed25519 `signed_hash` over pixels plus Comment verifies against NovelAI's published key.

A pulsar viewer should read NovelAI, A1111/Forge `parameters`, ComfyUI graphs, all four stealth variants, SwarmUI and InvokeAI. It should use a hand-written, zero-dependency walker in a Web Worker. Chunks should be rebuilt on a different architecture from NovelAI's: pure expansion inside `core/internal/prompt` with an origin map, chunks kept in localStorage and sent inline with each request, and a `!macro:Name!` token that collides with none of the 121,173 tags (bare `@name` would shadow `@_@`, 54,084 posts). The inspector runs client-side and hands images to the existing upload path. Twenty-four product decisions are batched below with defaults. The plan ships as four merge requests (MRs): Step 0 today, then three more.

## Every delivered PNG ships the safety negative and a key to the user's uploads

The leak is a pass-through, not a bug in any one component. Both ComfyUI launchers start `main.py` without `--disable-metadata` ([comfy/Dockerfile:41–43](../comfy/Dockerfile); [comfy/run-native.sh:60–64](../comfy/run-native.sh)). Core `SaveImage` therefore builds a `PngInfo` and calls `metadata.add_text("prompt", json.dumps(prompt))` on every save ([ComfyUI nodes.py](https://github.com/Comfy-Org/ComfyUI/blob/157db2ea2f83/nodes.py#L1669-L1728)). Core never sends `extra_data`, so the editor-format `workflow` chunk is absent, but the API-format graph is always there ([client.go:173–177](../core/internal/comfy/client.go)). The bytes travel unchanged through four steps:
- `GET /view` reads them with `io.ReadAll`.
- The worker runs `CheckOutput` and writes the same slice with `Blobs.Put` ([worker.go:101–131](../core/internal/worker/worker.go)).
- `handleImage` `io.Copy`s the blob with `Content-Type: image/png` ([api.go:699–714](../core/internal/api/api.go)).
- Orbit's download saves the blob as-is.

No test caught this because `comfytest.Fake` returns a tiny PNG with no text chunk ([fake.go:34–48](../core/internal/comfy/comfytest/fake.go)).

A read-only scan of `core/data` shows the exposure. **65 of 99 blobs carry `tEXt:prompt`. All 24 generated images among the 30 newest hold the complete `FORBIDDEN_TAG_UC` block at the head of the negative `CLIPTextEncode`**, next to checkpoint names such as `noobai-xl-vpred.safetensors`, LoRA filenames, and `LoadImage` inputs of the form `image=<uuid>.png`. The graphs reference 24 distinct upload IDs, and every one of those upload blobs is still stored. `imageIDPattern` accepts any UUID, and no route has auth middleware ([api.go:88–115](../core/internal/api/api.go)).

The safety string itself is already semi-public: it is committed in `.env.example`, and `GET /jobs/{id}` returns it inside `params`. What is new is that a PNG travels off-platform, and it carries a **capability URL to the user's private reference photo**. Upload blobs cannot simply be withdrawn from `/images`, because orbit's upload previews and character-reference thumbnails fetch `/images/{upload_id}` ([useImageUpload.ts:21](../orbit/app/composables/useImageUpload.ts); [CharacterCard.vue:32](../orbit/app/components/CharacterCard.vue)). The fix therefore has to remove the identifiers from the files.

### The fix: a launch flag, then a byte-level chunk filter

The launch flag is necessary but not sufficient. `--disable-metadata` is honoured by `SaveImage`, `PreviewImage` and the v3 save helpers. But `SaveSVGNode` writes `prompt` and `extra_pnginfo` with no check, and custom nodes decide for themselves ([nodes_images.py L816–830](https://github.com/Comfy-Org/ComfyUI/blob/157db2ea2f83/comfy_extras/nodes_images.py#L816-L830)). Today's pinned packs are a ControlNet preprocessor set and IP-Adapter Plus, not save nodes ([comfy/custom-nodes.txt](../comfy/custom-nodes.txt)). The next pin could change that.

The robust layer is a filter in core, and it has to be byte-level. Go's PNG encoder writes only IHDR, PLTE, tRNS, IDAT and IEND, and its decoder silently skips every other chunk after checking its CRC ([go writer.go](https://github.com/golang/go/blob/df41f44f4c00/src/image/png/writer.go#L96-L198); [go reader.go](https://github.com/golang/go/blob/df41f44f4c00/src/image/png/reader.go#L870-L935)). Re-encoding would discard colour chunks and change the bytes. Chunk splicing keeps the pixels bit-exact: each chunk is `u32be len | type | data | crc32(type‖data)`, about 60 lines of stdlib code. ComfyUI's own new `inject_png_metadata` does exactly this ([nodes_images.py L1505–1534](https://github.com/Comfy-Org/ComfyUI/blob/157db2ea2f83/comfy_extras/nodes_images.py#L1505-L1534)).

The filter keeps an allow-list:
- the critical chunks: `IHDR`, `PLTE`, `tRNS`, `IDAT`, `IEND`
- the colour chunks: `gAMA`, `cHRM`, `sRGB`, `iCCP`, `cICP`, `mDCV`, `cLLI`, `sBIT`
- `pHYs`

Everything else is dropped, including `tEXt`, `zTXt`, `iTXt`, `eXIf`, `tIME`, ComfyUI's APNG `comf` and any bytes after IEND. The filter fails closed on a bad CRC, because ComfyUI output never has one.

It runs in two places:
- **In the worker, before `CheckOutput`**, so the classifier sees the exact bytes that will be stored.
- **On the serve path**, so the 65 legacy blobs come out clean without a migration.

### What step 0 cannot fix

Step 0 cleans the files, not the API. Core never reads the `Authorization` header that orbit sends. `jobs.user_id` is declared but never written. `History` is a global `SELECT … ORDER BY created_at DESC` ([store.go:158–175](../core/internal/job/store.go)). `GET /jobs/{id}` returns `params`, the stored `Spec`, including the full emitted negative with its safety block and every config tag ([api.go:477–500](../core/internal/api/api.go); [assembly.go:76–117](../core/internal/assembly/assembly.go)).

The image ID is the job ID ([worker.go:146–150](../core/internal/worker/worker.go)). The audit's suggested metadata payload included the job ID, and this report overrules that. **Pulsar's own metadata must never embed the job ID, or anything else that resolves to a core URL, until core enforces bearer auth.** Otherwise the PNG becomes a key to `/jobs/{id}`.

ComfyUI needs no upgrade for step 0. The pinned `COMFYUI_REF=v0.28.0` ([comfy/Dockerfile:4](../comfy/Dockerfile)) is the release that fixes the stored-XSS CVEs on `/view` and `/userdata`, per OSV ([CVE-2026-56670](https://osv.dev/vulnerability/CVE-2026-56670); [CVE-2026-56672](https://api.osv.dev/v1/vulns/CVE-2026-56672)).

## NovelAI's chunks are send-time text macros its servers never see

In the code the feature is called "macros" (`ImagePromptMacros`, a `macro` ProseMirror node); "Prompt Chunks" is only the UI label.

### How a prompt refers to a chunk

The `@` key opens a picker only in four positions: at a paragraph start, after whitespace, after a comma, or in an empty field. A mid-word `@` stays text ([bundle 2pglf7ew9_mj1.js](https://novelai.net/_next/static/chunks/2pglf7ew9_mj1.js)). The query is the text between `@` and the cursor. Spaces are allowed, and a comma closes the picker. Ranking is case-insensitive and filled in tiers, without duplicates:
1. label starts with the query
2. label contains the query
3. label contains the query as a fuzzy subsequence
4. the same three tests on `"<Category>: <label>"`
5. the content contains the query

ArrowUp and ArrowDown move the selection. Enter or Tab inserts, Escape closes, and the picker also closes 150 ms after blur. Each row shows an 8 px colour dot, the label, the category in uppercase at 40 % opacity, and a one-line preview. The selected row gets a `${color}20` background and a 3 px left border. The panel is 200–320 px wide and at most 280 px tall.

Inserting replaces the typed `@query` with an inline atom node. That node serialises as `⌜macro:<uuid>⌟`, using the corner brackets U+231C and U+231F. A second, typeable form, `!macro:<Name>!`, resolves by exact label after trimming, so names are case-sensitive ([bundle 0duset3_dg_wg.js](https://novelai.net/_next/static/chunks/0duset3_dg_wg.js?dpl=ee86535-43595739745-1); [NovelAI docs](https://docs.novelai.net/en/image/promptchunks)). There is no `@[keywords]` syntax: `@` is purely an editing affordance. Labels are not enforced unique, so with duplicate labels the `!macro:` form resolves to whichever duplicate comes last.

### Expansion: when it runs and what it does

The generate action runs these steps in order:
1. the furry-mode prefix
2. chunk expansion
3. auto-text
4. the identical-parameters check, on the **expanded** text
5. the request

Expansion covers the base prompt, the UC and every character's prompt and UC ([bundle 36juzcfi11xq3.js](https://novelai.net/_next/static/chunks/36juzcfi11xq3.js?dpl=ee86535-43595739745-1)).

The expander runs in two passes. Pass one replaces each id token with its chunk's raw content, once and without re-scanning. Pass two recursively resolves `!macro:Name!` with a per-branch visited set. In toast mode, a cycle or a missing name is **removed and reported, and generation still proceeds**. An id token whose chunk was deleted elsewhere survives as literal text. The docs state the consequence: "Chunk definitions are not stored in image metadata. Only the expanded text is saved" ([NovelAI docs (JA)](https://docs.novelai.net/ja/image/promptchunks)).

Two smaller behaviours matter for a reimplementation:
- **Import re-tokenizing.** On image import NovelAI runs the reverse step. It resolves every chunk, sorts by length (longest first), and `split(…).join(token)`s every exact substring ([bundle 0duset3_dg_wg.js](https://novelai.net/_next/static/chunks/0duset3_dg_wg.js?dpl=ee86535-43595739745-1)). There is no tag-boundary check, so a chunk whose content is just `1girl` would be turned into a pill inside any imported prompt.
- **Deletion and edits.** Deleting a chunk first "bakes" its expansion into the five live prompt fields, so normal deletion leaves no dangling tokens. Editing a chunk rewrites the pill attributes in every open editor ([bundle 2laalea3awu3k.js](https://novelai.net/_next/static/chunks/2laalea3awu3k.js)).

Because expansion is plain substitution, a pill wrapped in `{…}` or `1.5::…::` weights its whole expansion. An unclosed `::` inside a chunk leaks into the rest of the prompt, and it is invisible in the editor, because the highlighter walks only text nodes. The docs warn about exactly this ([NovelAI docs](https://docs.novelai.net/en/image/promptchunks)).

### Storage, records and scope

The data model is deliberately flat. A chunk and a category are the same record: `{containerId, remoteId, id, version:1, label, expansion, color="#6B7280", isCategory, childOrder?, categoryOrder?}`.
- A hidden root category with id `"default"` holds the order of uncategorised chunks and of categories.
- A chunk belongs to whichever category's `childOrder` contains it.
- Deleting a category moves its chunks to Uncategorized.
- "Delete All" asks for confirmation, then bakes every chunk into the prompts and wipes the store ([bundle 05bme-2zweosi.js](https://novelai.net/_next/static/chunks/05bme-2zweosi.js?dpl=ee86535-43595739745-1); [bundle 2laalea3awu3k.js](https://novelai.net/_next/static/chunks/2laalea3awu3k.js)).

Every record is its own encrypted object at `/user/objects/promptmacros`. Each is sealed with a per-container libsodium secretbox key from the user's keystore. The privacy policy, effective 10 September 2026, says prompt chunks are "encrypted in your browser before they are saved" ([privacy bundle 2c0ojtsjysma-.js](https://novelai.net/_next/static/chunks/2c0ojtsjysma-.js)). There is no export or import, no `maxLength`, and no client-side size cap. The form validates only "Name is required" and "Content is required".

On scope:
- Chunks work in every editor built on the shared ProseMirror component: prompt, UC, random prompt, character prompt and character UC.
- No model gate exists.
- Director reference descriptions bypass expansion.
- On mobile, the chunk list becomes a sheet tab.

No dated changelog exists. The privacy policy above is the earliest firm anchor, and V5 launched around 20–21 August 2026 ([weavai.app](https://weavai.app/blog/ja/2026/08/29/novelai-v5%E7%99%BB%E5%A0%B4%EF%BC%9A22%E4%BA%BA%E5%90%8C%E7%94%BB%E9%9D%A2%E3%82%84%E8%83%8C%E6%99%AF%E9%80%8F%E9%81%8E%E3%82%92%E8%A7%A3%E8%AA%AC/)).

### Pulsar should expand in core with an origin map, behind a `!macro:` token

NovelAI's client-side expansion follows from its encryption. Its servers cannot read chunk definitions, so they cannot expand them. Pulsar has no such constraint, and expanding in orbit would break five things the audit found:
- **Offsets.** Every 400 and notice would index the expanded text, so orbit would need its own source map. That contradicts "Core owns the grammar; orbit never parses or builds prompt syntax" ([docs/frontend.md:200–212](../docs/frontend.md)).
- **Chip mapping.** Chip indexes break once one chip expands to several pieces.
- **Plain mode** would send chunk content through the LLM.
- **Other API clients** (shells, eval) would not get chunks.
- **History** could never record which chunk produced a tag.

The right home is a pure `Expand` in `core/internal/prompt`, called by `assembly.ParseRequest` (prompt, negative, characters and pose), `/prompt/parse` and `/translate` ([assembly.go:336–358](../core/internal/assembly/assembly.go)). It recognises the token before scanning and splices the content runes. It hands `newScanner` an origin map, so the precomputed `u16`/`piece` arrays resolve every expanded rune to the reference's UTF-16 span and piece index. Every issue and tag span inside a chunk then lands on the `!macro:Name!` the user typed ([nai.go:144–187](../core/internal/prompt/nai.go)).

Compliance needs no code change. `CheckPrompt` re-parses the exact emitted strings patched into ComfyUI and never trusts `Spec` alone. `TestEveryTextInputIsChecked` already asserts that every `text` input in every graph was seen ([compliance/prompt.go:12–133](../core/internal/compliance/prompt.go); [prompt_e2e_test.go:225–274](../core/internal/api/prompt_e2e_test.go)). Pulsar's stricter grammar is a bonus. It rejects unbalanced brackets (`unclosed_bracket`, `stray_closer`), so the leaking-`::` hazard that NovelAI documents becomes a located error on the reference, and chunk content can be validated on save.

The token choice is settled by the tag DB, not by taste. Here are the three candidates:

| Candidate | Effect on the vocabulary and grammar |
|---|---|
| Bare `@name` | Shadows 8 real tags, including `@_@` with **54,084 posts** |
| `@[name]` | Touches no tag, but `[` is the de-emphasis operator, so `@[x]` already parses today as `@x` at ÷1.05 |
| `!macro:Name!` | **Zero** of 121,173 tag names contain `!macro` or `macro:` |

The three real `!`-prefixed tags (`!` at 55,192 posts, `!?` at 37,407, `!!` at 16,608) stay unaffected, because the lexer would require the full prefix (local DB, read-only query 2026-11-15, [tags table](../db/migrations/0002_tags.sql)). It is also NovelAI's own documented typeable form, so a NovelAI user's chunk content with nested references pastes in and works.

Pulsar's editor imposes one more constraint. `BaseInput` paints highlights on a mirror `div` behind a transparent textarea, so the glyphs shown must be the glyphs typed ([Input.vue](../orbit/app/components/base/Input.vue)). An id-keyed pill showing a label is impossible there. A readable name token is the only option. In tags mode a chip can show the label, but the chip normaliser lowercases and underscores anything without `{}[]` or `::`, so picker-inserted chunk chips must bypass it ([useTagAutocomplete.ts:42–50](../orbit/app/composables/useTagAutocomplete.ts)).

Storage follows the presets precedent. Core has no identity, saved configs already live in `useLocalStorage('pulsar.presets.v1')`, and they must work under `capacitor://` and `file://` origins ([usePresets.ts:82–113](../orbit/app/composables/usePresets.ts)). Chunks therefore go in `pulsar.chunks.v1`, and orbit sends only the referenced `{name, content}` pairs on each request. A `0009_prompt_chunks.sql` table today would be shared by everyone on the deployment, just as history is.

Two follow-ons:
- **Export and import.** NovelAI has neither, but pulsar should add JSON export and import: until auth exists, that is the only way to move chunks between devices.
- **History.** History keeps the expanded tags as the authoritative, compliance-checked record, plus an optional `chunk` name and content hash per tag. This mirrors the style-preset rule that "a later edit to the preset never changes what this row reproduces" ([0008_style_presets.sql](../db/migrations/0008_style_presets.sql)).

## One toggle silences autocomplete locally; the other paints a running weight

NovelAI's chunk popover has exactly two tabs, "Prompt Chunks" and "Settings". The Settings tab holds exactly two toggles; a "Lock Settings" tab renders only in non-production builds ([bundle 2laalea3awu3k.js](https://novelai.net/_next/static/chunks/2laalea3awu3k.js)).

### Disable Tag Suggestions

This toggle is the localStorage key `imagegen-tag-suggestions-disabled`. It is written through a hook that notifies the current page but deliberately skips cross-tab sync. One flag is passed to every editor. It turns off only the Danbooru tag popup. The `@` picker keeps working, because the suggestion plugin checks the flag and then also yields whenever `@` is typed ([bundle 2pglf7ew9_mj1.js](https://novelai.net/_next/static/chunks/2pglf7ew9_mj1.js); [bundle 1trz8ek4n3-dh.js](https://novelai.net/_next/static/chunks/1trz8ek4n3-dh.js?dpl=ee86535-43595739745-1)).

### Highlight Emphasis

This toggle is the user setting `imagePromptEmphasis`, **on by default**, saved through `saveSettings`. That probably syncs it to the account; the endpoint was not traced. It gates every inline decoration in the editors ([bundle 0duset3_dg_wg.js](https://novelai.net/_next/static/chunks/0duset3_dg_wg.js?dpl=ee86535-43595739745-1)).

The highlighter tracks one running weight `h`, starting at 1:
- `{` and `]` multiply it by 1.05; `}` and `[` divide it by 1.05.
- On numeric models, `N::` sets it to N, and a bare `::` resets it to 1.

Below |h−1| < 0.01 nothing is painted. Above that, the colour class is "high" for h > 1 and "low" otherwise, so negative weights are blue too. The `::` marker and its number always render as "mid" at 50 % opacity. Opacity is `0.2 + 0.4·clamp(|h−1|/1)`; the divisor becomes 0.5 for h ≤ 0. The result is one of 41 classes (`low|mid|high-intensity-color-N`, N = 0…40), coloured by the theme. The code's fallback colours are high `#ED5807`, low `#079CED` and mid `#7acc29` ([bundle 26qb5r-044fu5.js](https://novelai.net/_next/static/chunks/26qb5r-044fu5.js)).

A separate, ungated plugin bolds `|` and `||`. Three emphasis lint hints appear on blur:
- a space between the number and `::` (for |N| < 21)
- the number placed after the `::`
- |w| > 70, which usually means the user forgot a space

### How these map onto pulsar

**Disable Tag Suggestions is a one-line gate.** `useTagAutocomplete.search` is the only place DB and live-Danbooru suggestions originate. When `pulsar.settings.tagSuggestions` is false, it returns with `suggestions = []` and `open = false`, while comma-commit, Enter-commit and backspace-pop keep working ([useTagAutocomplete.ts](../orbit/app/composables/useTagAutocomplete.ts)).

Two related points:
- **The chunk branch needs its own gate regardless of the setting.** Without it, every chunk name the user types is sent to `/tags`, and after three characters it falls back to a live Danbooru lookup.
- **The resolved-strip `/tags` calls should keep running.** They are per-chip previews, not suggestions.

The setting stays per-browser because pulsar has no account settings.

**Highlight Emphasis needs core to expose positions.** Orbit may not compute weights. Today `/prompt/parse` returns per-run `{text, w, emitted}` with **no offsets**, and tag spans cover text runes but not operators ([api/prompt.go:56–84](../core/internal/api/prompt.go)). Core must therefore record run spans and operator spans in the parser and return them.

Orbit then turns the boolean `mark` in `segmentText`/`BaseInput` into typed marks (`error`, `up`, `down`, `op`, `chunk`). Each kind is styled by component classes in `main.css` that read three new `@theme` tokens (`--color-emph-up`, `--color-emph-down`, `--color-emph-op`) and an intensity custom property. This is required because the token file has none for emphasis and arbitrary Tailwind values are banned ([main.css:9–46](../orbit/app/assets/css/main.css)). Chunk tokens are painted in their chunk colour, never by emphasis, matching NovelAI's untinted pills.

There is one honest cost. NovelAI paints synchronously, while pulsar's marks arrive with the 250 ms debounced parse, and they vanish while the surface has any parse error, because `tags` comes back empty on error ([usePromptParse.ts](../orbit/app/composables/usePromptParse.ts)). That is the price of a single grammar, and error underlines keep precedence. Pulsar rejects `|` outright, so there is nothing to bold.

NovelAI's three lint hints are already covered. Pulsar's scanner ports the first two as notices: N3 for a spaced number before `::`, and N4 for a number after `::`. It also rejects |w| > 10 outright, which subsumes the "extremely high" hint ([nai.go:636–680](../core/internal/prompt/nai.go)). `PromptIssues.vue` already renders those notices, so the lint half of the feature needs no new work.

## NovelAI's inspector trusts one JSON key and a signature over pixels

### What /inspect does

/inspect is a Next.js page with SSR off. It accepts PNG, JPEG or WebP through a file picker, a full-window drop overlay (first file only) or paste ([inspect bundle 217d_wdvrdzx7.js](https://novelai.net/_next/static/chunks/217d_wdvrdzx7.js?dpl=ee86535-43595739745-1)). It reads metadata in a strict order:
1. PNG `tEXt` and `iTXt`, but not `zTXt`.
2. Only if that found nothing, the EXIF UserComment. Decoding is lenient: `UNICODE\0` with a BOM-chosen byte order, `ASCII\0\0\0`, 8 NULs or no prefix, with trailing NULs stripped.
3. Only then the alpha-channel payload, skipped for images over 16,777,216 px.

It shows "This image contains no metadata" unless a `Comment` key exists. Every other tool's PNG (A1111's `parameters`, ComfyUI's `prompt`) is therefore treated as metadata-free.

The display has three parts:
- **Header:** Title, Description, Software and Source. Source is rendered as `"<Source> (<model display name>)"`, followed by ` ✓` when the signature verifies.
- **Request Type,** with suffixes for ControlNet, Vibe Transfer (and "embedded") and Precise Reference.
- **Parameters,** in a "Simplified" view or a persisted "Raw Parameters" view (`JSON.stringify(q,null,4)`).

The Simplified view renders, only when defined: Prompt, Undesired Content, Character N Prompt and Character N UC (from `v4_prompt`), Resolution, Seed, Steps, Sampler (with noise schedule and SMEA tags), CFG, CFG Rescale and Undesired Content Strength. `Generation time` is not rendered. The page has **no copy, import, save or strip control**.

Import lives on the generator instead. Dropping an image there opens "What do you want to do with this image?". Its toggles default to prompt, UC and characters **on**, and vibe transfer, settings and seed **off**. It also offers "Clean Imports" and "Actual Prompt". The metadata branch is offered only when `Software` includes "NovelAI" and `req_type` is absent ([bundle 2laalea3awu3k.js](https://novelai.net/_next/static/chunks/2laalea3awu3k.js?dpl=ee86535-43595739745-1)). Metadata survives only through the save icon, not right-click save or the clipboard ([NovelAI FAQ](https://docs.novelai.net/en/faq)).

### The metadata format

The container is a six-key dictionary that has been stable since V3:

| Key | Content | Signed |
|---|---|---|
| `Title` | Static string; dropped from the alpha copy | No |
| `Description` | Final prompt as sent, quality tags included; importer reads the prompt from here | No |
| `Software` | `"NovelAI"` | No |
| `Source` | Model name + 8-hex hash, e.g. `NovelAI Diffusion V5 DB276663` | No |
| `Generation time` | Seconds, as a string | No |
| `Comment` | Server-resolved request JSON ending in `signed_hash` | **Yes** |

Sources: [nai_sig.py](https://github.com/NovelAI/novelai-image-metadata/blob/main/nai_sig.py); [nai_meta_writer.py](https://github.com/NovelAI/novelai-image-metadata/blob/main/nai_meta_writer.py); [bundle 2fqyltv7zgwpa.js](https://novelai.net/_next/static/chunks/2fqyltv7zgwpa.js?dpl=ee86535-43595739745-1).

**The Comment JSON.** `Comment` is Python `json.dumps` output, with `", "`/`": "` separators, floats like `7.0`, non-ASCII escaped, and `signed_hash` last. The frontend bundle embeds 81 real V5 examples, all `PromptGenerateRequest` on `DB276663` with `k_euler_ancestral` and `karras`. They share **52 keys** ([bundle 2laalea3awu3k.js](https://novelai.net/_next/static/chunks/2laalea3awu3k.js?dpl=ee86535-43595739745-1)). The keys include:
- the usual settings: `prompt`, `uc`, `steps`, `width`, `height`, `scale`, `cfg_rescale`, `seed`
- the V4+ structure: `v4_prompt`/`v4_negative_prompt`, each with `base_caption` and `char_captions[{char_caption, centers[{x,y}]}]`
- `request_type`, `model_name` and `model_hash`
- server-only flags such as `cfg_sched_eligibility` and `wonky_vibe_correlation`

**Model mapping.** `Source` maps to a model by its trailing hash, back to the 2022 SD-1.x models. `DB276663` means V5 Curated. Hashes the table doesn't know become "Unknown". Strings with a `DiffusionModelMetaName.NAIv4next` prefix exist, so a parser must match on the hash, not the prefix ([bundle 0duset3_dg_wg.js](https://novelai.net/_next/static/chunks/0duset3_dg_wg.js?dpl=ee86535-43595739745-1)).

**Import quirks.** The importer copies back a fixed key set and takes the prompt from `Description`. It prefers `actual_prompts`, the resolved random-syntax values, under "Actual Prompt". It divides `skip_cfg_above_sigma` by the same resolution factor the request builder multiplied in.

**File formats.** In the V5 frontend every generation is requested as `image_format: "webp"`. The WebP carries an EXIF UserComment holding the whole key dictionary as JSON, plus the alpha payload. When the user's format setting is PNG (the default), the browser converts lossless WebP to PNG locally and writes the keys as tEXt, or as iTXt when non-Latin-1. NovelAI never outputs JPEG for images ([bundle 2fqyltv7zgwpa.js](https://novelai.net/_next/static/chunks/2fqyltv7zgwpa.js?dpl=ee86535-43595739745-1)).

### The alpha-channel copy ("stealth")

The alpha copy is the A1111 "stealth pnginfo" format in its `stealth_pngcomp` variant ([nai_meta.py](https://github.com/NovelAI/novelai-image-metadata/blob/main/nai_meta.py); [stealth_pnginfo.py](https://github.com/ashen-sensored/sd_webui_stealth_pnginfo/blob/main/scripts/stealth_pnginfo.py)):
1. Alpha LSBs are read **column-major** and packed MSB-first.
2. A 15-byte magic follows.
3. Then a **32-bit big-endian length counted in bits**.
4. Then gzip-compressed JSON of the keys, with Title omitted.
5. An optional FEC block may follow; "NovelAI does not add FEC at the moment".

The frontend normalises imported alpha values (254 → 255, 1 → 0). That implies the server embedder writes `alpha & 0xFE | bit` rather than forcing 255.

The two copies survive differently. As of December 2023, 4chan wiped PNG text but kept alpha. Discord and Pixiv kept both. Twitter wiped both, except alpha at certain resolutions ([rentry hdg-nai-v3](https://rentry.org/hdg-nai-v3)). Any lossy re-encode, resize or alpha flattening destroys the payload.

### The signature

The signature is the part pulsar can verify but never produce. `signed_hash` is a base64 ed25519 detached signature over two things concatenated:
1. The raw pixel bytes: RGB, or for transparent images RGBA with the alpha LSB cleared. The second candidate was added on 2026-08-20.
2. The exact UTF-8 Comment bytes minus the `signed_hash` member.

The public key is `Y2JcQAOhLwzwSDUJPNgL04nS0Tbqm7cSRc4xk0vRMic=` (32 bytes) ([nai_sig.py](https://github.com/NovelAI/novelai-image-metadata/blob/main/nai_sig.py); [commit 3428907a](https://github.com/NovelAI/novelai-image-metadata/commit/3428907a)). The browser removes the member textually with `/"signed_hash": "([^"]*)"/`, because any JS re-serialisation drops Python's spacing and `.0` floats and breaks verification. Title, Description, Software and Source are unsigned and can be edited without losing the ✓.

Three implications follow for pulsar:
- **It can verify NovelAI images client-side,** given a bit-exact pixel decode.
- **It can never mint a valid NovelAI signature.**
- **It must never write `Software: NovelAI`.** That string is NovelAI's own import gate and one of Danbooru's AI-image triggers ([danbooru exif_tool.rb](https://github.com/danbooru/danbooru/blob/788c372cd53d/app/logical/exif_tool.rb#L150-L162)). Writing it would misattribute provenance.

Writing a NovelAI-shaped `Comment` under `Software: pulsar` would only get /inspect to render it as "(Unknown)" with no ✓, and NovelAI's importer would still refuse it. That is not worth the schema.

## A pulsar viewer needs six readers, one hardened walker and no dependencies

### The formats

The ecosystem reuses two containers, PNG text chunks and EXIF UserComment, plus one steganographic layer. Each tool adds its own quirks:

| Producer | PNG | JPEG / WebP | Payload and gotchas |
|---|---|---|---|
| A1111 / Forge / reForge | `parameters` tEXt; **iTXt** when the prompt is not Latin-1 | UserComment `UNICODE\0` + UTF-16**BE** | Prompt lines, `Negative prompt:`, one `Key: value` line; values containing `,` `\n` `:` are JSON-quoted; Forge embeds stealth by default |
| Stealth (A1111 ext., Forge, reForge, SwarmUI, NovelAI) | Alpha or RGB LSB | Lossless WebP only | `stealth_{png\|rgb}{info\|comp}`, 32-bit bit-length, payload differs per tool |
| ComfyUI | `prompt` (API graph) + `workflow` tEXt; APNG `comf` chunk **after IDAT** | WebP EXIF 0x0110 Model `prompt:{json}`, 0x010F Make `workflow:{json}` | `NaN`/`Infinity` tokens; `workflow` can hold stale widget text |
| SwarmUI | `parameters` JSON with `sui_image_params` | UserComment UTF-16**LE** | Lowercase param ids, `sui_extra_data`, `sui_models` |
| InvokeAI | `invokeai_metadata` (+ workflow/graph); legacy `sd-metadata`, `Dream` | — | JSON; `Dream` is a CLI string |
| Fooocus | Off by default; `parameters` + `fooocus_scheme` | UserComment, Software, MakerNote | — |
| Draw Things | XMP `exif:UserComment` JSON (`c`, `uc`, `size`) | — | Reverse-engineered |

Sources: [A1111 images.py](https://github.com/AUTOMATIC1111/stable-diffusion-webui/blob/82a973c04367/modules/images.py#L565-L622); [infotext_utils.py](https://github.com/AUTOMATIC1111/stable-diffusion-webui/blob/82a973c04367/modules/infotext_utils.py#L16-L18); [Pillow add_text](https://github.com/python-pillow/Pillow/blob/main/src/PIL/PngImagePlugin.py#L359-L393); [Forge shared_options.py](https://github.com/lllyasviel/stable-diffusion-webui-forge/blob/main/modules/shared_options.py#L361); [SwarmUI MetadataHelper.cs](https://github.com/mcmonkeyprojects/SwarmUI/blob/d347f3061ce9/src/Utils/MetadataHelper.cs#L14-L50); [ComfyUI _ui.py](https://github.com/Comfy-Org/ComfyUI/blob/157db2ea2f83/comfy_api/latest/_ui.py#L81-L131); [InvokeAI image_files_disk.py](https://github.com/invoke-ai/InvokeAI/blob/bceb4e3e629e/invokeai/app/services/image_files/image_files_disk.py#L262-L276); [Fooocus config.py](https://github.com/lllyasviel/Fooocus/blob/main/modules/config.py#L652-L662); [receyuki drawthings.py](https://github.com/receyuki/stable-diffusion-prompt-reader/blob/119b36e01b6e/sd_prompt_reader/format/drawthings.py).

### Parser rules that matter

Four rules decide whether a reader works on real files:
- **A1111: port the regex verbatim.** Use `\s*(\w[\w \-/]+):\s*("(?:\\.|[^\\"])+"|[^,]*)(?:,|$)`, and treat the last line as settings only when it yields three or more pairs. Civitai's earlier divergent parser produced junk objects from quoted prose ([civitai format-references.md](https://github.com/civitai/media-metadata/blob/898f6aa69ac0/docs/format-references.md)).
- **UserComment: handle both byte orders.** A1111 writes UTF-16BE and SwarmUI writes UTF-16LE, so decode with BOM, then a zero-byte heuristic, then the TIFF byte order, as Civitai does ([civitai user-comment.ts](https://github.com/civitai/media-metadata/blob/898f6aa69ac0/src/image/read/user-comment.ts)).
- **Stealth: all four signatures and both channel layouts.** Readers that only accept `stealth_pngcomp`, such as sd-parsers and receyuki, miss uncompressed and RGB-mode images.
- **ComfyUI: walk the graph backwards from the sampler.** Pick the sampler fed by `EmptyLatentImage`, then walk `positive`/`negative` back through conditioning nodes to text encoders, under a **512-visit budget**, because diamond DAGs cost 2^depth. Use `workflow` only as a fallback restricted to text-encode nodes, because of its stale widgets. Civitai documents that 5 of its 35 ComfyUI fixtures still yield no prompt ([civitai graph.ts](https://github.com/civitai/media-metadata/blob/898f6aa69ac0/src/image/parsers/comfyui/graph.ts); [corpus-findings.md](https://github.com/civitai/media-metadata/blob/898f6aa69ac0/docs/corpus-findings.md)).

**Detection order is behaviour.** Civitai's registry comment says so ([registry.ts](https://github.com/civitai/media-metadata/blob/898f6aa69ac0/src/image/parsers/registry.ts)). The viewer should therefore always show the raw chunks beside its interpretation.

### Library choice

The library choice favours writing it by hand:

| Option | Gzip size | Gap |
|---|---|---|
| [@civitai/generation-metadata](https://bundlephobia.com/package/@civitai/generation-metadata@0.4.0) 0.4.0 (MIT) | 78.9 kB | No NovelAI, InvokeAI or stealth |
| [ExifReader](https://bundlephobia.com/package/exifreader@4.54.0) 4.54.0 (MPL-2.0) | 44.8 kB | Format coverage only, no generator parsing |
| [exifr](https://bundlephobia.com/package/exifr@7.1.3) 7.1.3 (2021) | 26.1 kB | No WebP, no zTXt |
| png-chunks-extract + png-chunk-text (2015) | ~1.8 kB | No iTXt or zTXt, and Pillow writes non-Latin-1 prompts as iTXt |
| Hand-written walker + parsers | 0 kB | ~250–350 lines (estimate) |

The browser's `DecompressionStream` handles both zlib (`deflate`) and gzip, and has been Baseline since May 2023 ([MDN](https://developer.mozilla.org/en-US/docs/Web/API/DecompressionStream)). A hand-written walker plus a port of Civitai's MIT ComfyUI heuristics fits the mobile bundle rule. It also lets pulsar own NovelAI and stealth support, which no package has.

### Where parsing runs, and where its results go

Parsing has to run in the browser, before upload. `ingest.Normalize` decodes, resamples to an SDXL bucket and re-encodes, which drops every text chunk and any LSB payload ([ingest.go:1–3](../core/internal/ingest/ingest.go)). A server endpoint that accepted images just to read metadata would be a second image-accepting path, which the one-ingest-path invariant forbids ([CLAUDE.md](../CLAUDE.md)).

**Images.** "Use as base image" or "Use as reference" hands the same `File` to `useImageUpload().upload`, which keeps `Normalize`, `CheckUpload` and the fail-closed behaviour identical. Ingest accepts only PNG and JPEG today. `golang.org/x/image` v0.44.0 is already an indirect dependency, so WebP decoding costs one import ([core/go.mod](../core/go.mod)).

**Prompts and settings.** Prompts land in the editor and go through `/prompt/parse` and `/generate` like typed text. **Settings need restraint.** CLAUDE.md's rule that settings don't transfer between model lineages means NovelAI's CFG 7 with karras, or an A1111 sampler choice, says nothing useful about Illustrious or NoobAI. Import size (snapped to a bucket), steps and seed at most, and display the rest.

### Treat every file as hostile

The parser runs inside an app that holds a bearer token in memory, so an XSS in the viewer would hand that token over. The limits, with their sources:
- **File and chunk length.** Reject files above about 50 MB. Bound every chunk length by `min(2^31−1, remaining)`.
- **Decompressed size.** Cap at about 4 MiB per chunk and 16 MiB in total. Pillow uses 1 MiB per chunk and 64 MiB in total; ExifReader uses 128 MiB in total ([Pillow](https://github.com/python-pillow/Pillow/blob/main/src/PIL/PngImagePlugin.py#L105-L160); [ExifReader README](https://github.com/mattiasw/ExifReader#limiting-decompressed-metadata-size)).
- **Stealth length.** Reject length fields that are not multiples of 8 or that exceed the image's capacity, and cap gzip output at 1 MiB. A1111's own reader decompresses without a limit.
- **Isolation.** Run the walk in a Web Worker.
- **Rendering.** Every string goes through `{{ }}`. Use no `v-html` and no auto-linking, strip control characters, and never let a metadata value drive a fetch ([Vue security](https://vuejs.org/guide/best-practices/security); [W3C PNG §13.3](https://www.w3.org/TR/2025/REC-png-3-20250624/)).

ComfyUI's run of stored-XSS CVEs on its upload and view endpoints shows that attackers do use these surfaces ([CVE-2025-6092](https://www.sentinelone.com/vulnerability-database/cve-2025-6092/)).

### What pulsar writes back is a provenance decision under Art. 50

Once step 0 strips everything, the question is what pulsar adds back. The recommended set has three layers:
1. An `XML:com.adobe.xmp` iTXt packet with `Iptc4xmpExt:DigitalSourceType`, set to `trainedAlgorithmicMedia` for txt2img and to `compositeWithTrainedAlgorithmicMedia` for img2img or inpaint ([IPTC CV](https://cv.iptc.org/newscodes/digitalsourcetype/)).
2. An A1111-format `parameters` iTXt. Civitai, Forge's PNG Info tab, receyuki, sd-parsers and Danbooru's detector all read it.
3. A versioned `pulsar` iTXt JSON for lossless round-trip into `/create`.

Both text payloads are built from the same user-only projection that `paramsToPresetConfig` already uses: `source === 'user'` tags only ([usePresets.ts:132–183](../orbit/app/composables/usePresets.ts)). Excluded:
- the safety negative
- `DEFAULT_UC`
- quality and style text
- upload IDs
- file paths
- the job ID

Insert the payloads at serve time from `jobs.params`. That way a change to the sanitiser applies retroactively, and the unauthenticated thumbnail path never carries prompts. Stealth alpha should be read but not written: it survives a "download clean" choice unless separately removed, which would surprise users.

The XMP layer is not optional polish if pulsar is offered to others. **Art. 50(2) of the AI Act requires providers of generative systems to mark outputs "in a machine-readable format and detectable as artificially generated"** ([AI Act Art. 50](https://artificialintelligenceact.eu/article/50/)). The timeline:
- It applies from 2 August 2026.
- Systems already on the market before that date get until 2 December 2026, under the Digital Omnibus.
- The final Code of Practice of 10 June 2026 expects "at least two layers", with metadata "digitally signed and time-stamped" ([Freshfields](https://www.freshfields.com/en/our-thinking/blogs/technology-quotient/eu-ai-act-unpacked-33-the-final-code-of-practice-on-transparency-of-ai-generate-102n4yx); [Faegre Drinker](https://www.faegredrinker.com/en/insights/publications/2026/7/eu-ai-act-commission-confirms-transparency-code-of-practice-as-adequate-and-publishes-final-version-of-its-guidelines-on-transparency-obligations)).

Neither exemption helps:
- **Open source:** the free-and-open-source exemption explicitly excludes Art. 50 systems.
- **Personal use:** the personal-use carve-out covers deployers, not providers ([AI Act Art. 2](https://artificialintelligenceact.eu/article/2/)).

Pulsar has not launched: CLAUDE.md makes the child-safety gate a launch prerequisite. So any non-private launch to EU users is in scope from day one, with no grace period; that is an inference, not legal advice. Unsigned XMP alone does not meet the Code's bar. C2PA has no Go SDK; the options are `c2pa-rs` (MIT/Apache) via cgo or a `c2patool` sidecar ([c2pa-rs](https://github.com/contentauth/c2pa-rs)). Its `caBX` chunk is exactly what pulsar's allow-list strips, so **signing must be the last byte operation, and the serve-time filter must never run over signed outputs**.

## Conclusion

Image metadata turns out to be a compliance surface, not a convenience. The pre-delivery hook guards pixels with a fail-closed classifier, yet the same bytes carried the safety recipe and a private-upload key past it, because no invariant named the non-pixel chunks. Step 0 should therefore be pinned the way `FORBIDDEN_TAG_UC` is pinned: by a named test asserting that delivered bytes carry only allow-listed chunks. The same reasoning reaches the API: the image ID is a job ID, and `/jobs/{id}` is open. Until core verifies bearer tokens, pulsar should embed nothing that resolves back to it.

The NovelAI study also changes where pulsar should diverge on purpose. NovelAI expands chunks in the browser because encryption leaves it no choice. It tolerates broken references and unbalanced weights because its grammar is a running multiplier with no errors. Pulsar can do better on both counts: core-side expansion with an origin map, and located errors on the reference. The inspector, meanwhile, is where foreign prompts meet the lineage rule. Importing prompts is worth doing; importing settings mostly is not. Shared, reproducible state belongs to pulsar's own `pulsar` JSON chunk, not to anyone else's format.

## Decisions to settle in one batch

Step 0 needs only D2. Everything else is batched here so planning can proceed without mid-run questions. Each default is the recommendation this report argues for.

| ID | Decision | Recommended default | Alternatives | Blocks |
|---|---|---|---|---|
| D1 | Deployment scope and identity | Single-owner self-host for now. Bearer verification in core, a users table, `jobs.user_id`, and owner-scoped `/history`, `/jobs`, `/images` run as a separate project **before any shared or public deployment** | Build auth now; stay single-user forever | Server-side chunk store; Phase 6 |
| D2 | Leaked upload blobs | Delete the upload blobs referenced in legacy graphs (24 today) once the strip ships; core blob IDs are already "ephemeral" by contract | Keep them; purge all uploads older than N days | Step 0.5 |
| D3 | Chunk reference token | `!macro:Name!` in text; `@` is an editor-only trigger (NovelAI parity) | `@[name]` (repurposes `[`); bare `@name` (shadows `@_@` and 7 tags); NovelAI's id token `⌜macro:id⌟` (unreadable in a textarea) | Phases 1–2 |
| D4 | Names and limits | Trimmed, case-sensitive, **unique** (unlike NovelAI), ≤ 64 chars, no `! , \| { } [ ] ::` or newline; ≤ 256 chunks, ≤ 4 KiB each, ≤ 64 per request; `MaxBytes`/`MaxTags` apply after expansion | NovelAI's unlimited and non-unique labels | Phases 1–2 |
| D5 | Unknown, cyclic or too-deep reference | Located 400 (`unknown_chunk`, `chunk_cycle`, `chunk_depth`) on the reference span | NovelAI's drop-and-toast | Phase 1 |
| D6 | Nesting | Allowed via `!macro:` in chunk content, depth ≤ 8, per-branch cycle check | No nesting | Phase 1 |
| D7 | Chunk storage | `useLocalStorage('pulsar.chunks.v1')`, plus JSON export/import; referenced chunks sent inline per request | `0009_prompt_chunks.sql`, shared deployment-wide; per-user Postgres after D1 | Phase 2 |
| D8 | History provenance | Store `chunk {name, hash}` on each expanded tag; history restore shows the token only when the local chunk's hash matches, otherwise expanded tags | Expanded tags only | Phases 1–2 |
| D9 | Plain (NL) mode | Text from a chunk skips the LLM and is grounded as tags | Send chunk content through `Decompose` | Phase 1 |
| D10 | Chunk colours | A fixed swatch palette from new `@theme` tokens, default neutral | Free hex like NovelAI, validated and contrast-clamped | Phase 2 |
| D11 | Re-tokenize on import | Only on whole-tag sequence matches, for chunks of ≥ 2 tags, with a notice | NovelAI's raw substring replace; off | Phase 5 |
| D12 | Disable Tag Suggestions | Per-browser; suppresses only the DB and live-Danbooru popup; keeps the `@` picker and resolved-strip previews | Also stop the previews | Phase 3 |
| D13 | Highlight Emphasis | Per-browser, **on** by default, colours NovelAI units (`w`) rather than calibrated `emitted` | Off by default; colour `emitted` | Phase 3 |
| D14 | What pulsar writes | XMP `DigitalSourceType` + `CreatorTool` on every served image; A1111 `parameters` iTXt + `pulsar` iTXt JSON on downloads | `pulsar` JSON only; nothing | Phase 4 |
| D15 | Excluded fields | Exclude the safety negative, `DEFAULT_UC`, quality and style text, upload IDs, file paths and the **job ID**; models and LoRAs as display names | Filenames; omit models entirely | Phase 4 |
| D16 | Embedding default | Download with metadata by default; a "Download clean" option keeps only the XMP mark | Clean by default | Phase 4 |
| D17 | NovelAI-compatible keys | Never `Software: NovelAI`; no NovelAI six-key schema | NovelAI-shaped `Comment` under `Software: pulsar` (shows in /inspect as "Unknown") | Phase 4 |
| D18 | Stealth alpha | Read all four variants; write none | Write `stealth_pngcomp` to survive chunk-stripping hosts | Phases 4–5 |
| D19 | EU AI Act Art. 50 | Treat any non-private launch as in scope: XMP layer now; C2PA (`c2patool` sidecar) plus a second layer before such a launch; confirm with counsel | Private use only, XMP only | Phases 4, 6 |
| D20 | Formats the viewer reads | Tier 1: NovelAI (PNG, WebP EXIF, stealth, signature), A1111/Forge, stealth ×4, ComfyUI, SwarmUI, InvokeAI (+ legacy), pulsar. Tier 2 later: Fooocus, RuinedFooocus, Draw Things, Easy Diffusion | Tier 1 minus InvokeAI | Phase 5 |
| D21 | NovelAI signature | Verify client-side; show "Signed by NovelAI ✓" only when valid, nothing otherwise | Skip verification | Phase 5 |
| D22 | Import mapping | Prompt, UC and characters **on**; settings and seed **off**. "Settings" means a bucket-snapped size and steps only, never CFG, sampler, scheduler or rescale across lineages. Prefer NovelAI `actual_prompts`. Strip NovelAI's V5 quality suffix and UC preset when `tag_hint_*` says they were added automatically. Merge character UCs into the main UC with a notice, because pulsar has no per-character negative | Import everything | Phase 5 |
| D23 | Inspector entry points | An `/inspect` page in header and tab bar (four mobile tabs; check the width), plus drop-onto-`/create` opening the same import dialog | Page only | Phase 5 |
| D24 | WebP uploads | Add `x/image/webp` decoding to `ingest.Normalize` (still one path) | Inspected WebP can be viewed but not used as a base image | Phase 5 |

## Implementation plan

The order follows risk first. Step 0 (MR-A) stops an active leak and ships alone, today. The remaining work goes into three MRs, following the "fewer MRs" rule:
- **MR-B** (Phases 1–3): chunks and the Settings tab. Core and orbit must ship together, because `decodeJSON` rejects unknown fields, so an older core 400s a newer orbit ([api.go:994–1014](../core/internal/api/api.go)).
- **MR-C** (Phases 4–5): pulsar's own metadata and the inspector. They share format code and round-trip tests.
- **MR-D** (Phase 6): signed provenance, conditional on D1 and D19.

Every MR must pass all of these:
- `make test` with `PULSAR_TEST_DATABASE_URL` set
- `pnpm --dir orbit test`
- `pnpm --dir orbit lint` (no arbitrary Tailwind values)
- `pnpm --dir orbit typecheck`
- `TestForbiddenTagUCIsUnconditional`, passing unmodified

### Step 0 — stop the output-metadata leak (ship today; needs only D2)

#### 0.1 Launch ComfyUI with `--disable-metadata`

**Files.** [comfy/Dockerfile:41–43](../comfy/Dockerfile) (add the flag to `CMD`), [comfy/run-native.sh:60–64](../comfy/run-native.sh). Add a note in `.claude/skills/comfy-workflow/` that `SaveImage` writes the graph unless this flag is set.

**Tests.** None automated (it is launch config). Run one manual generation on each launcher.

**Done when.** A fresh ComfyUI output fetched from `/view` contains no `tEXt` chunk.

#### 0.2 Add a pure `core/internal/pngmeta` package

**Files.** New `core/internal/pngmeta/pngmeta.go` with `Strip(png []byte) ([]byte, error)` and `Rewrite(png []byte, keep func(typ string) bool, add []Chunk) ([]byte, error)`. Stdlib only (`encoding/binary`, `hash/crc32`).

**Behaviour.**
- Require the 8-byte signature and IHDR first.
- Bound each length by `min(2^31−1, remaining)` and verify every CRC.
- Keep the allow-list from the leak section.
- Drop everything after IEND, and require IEND to be present.

**Tests.** Table tests:
- `tEXt`/`zTXt`/`iTXt`/`eXIf`/`tIME`/`comf`/`caBX` are dropped; colour chunks are kept.
- IDAT bytes are identical, and `image/png` decodes to equal pixels before and after.
- A bad CRC, a truncated file, an oversized length or a missing IEND each return an error.
- Trailing bytes are dropped.
- `Strip(Strip(x)) == Strip(x)`.

Add `FuzzRewrite`, which must never panic.

**Done when.** Coverage is 100 % of branches, and the package imports nothing outside the stdlib.

#### 0.3 Strip in the worker before the compliance check

**Files.** [core/internal/worker/worker.go:101–131](../core/internal/worker/worker.go). Replace each `outs[node]` with `pngmeta.Strip(outs[node])` before the `CheckOutput` loop. A strip error fails the job closed (`MarkFailed`). `CheckOutput` and `Blobs.Put` then both use the cleaned bytes. Also update [comfytest/fake.go](../core/internal/comfy/comfytest/fake.go) so `Fake.PNG` can carry a `prompt` tEXt chunk.

**Tests.** A new e2e, `TestDeliveredImagesCarryNoGraph`. The fake returns a PNG whose `prompt` chunk contains `ForbiddenTagUC` and a `LoadImage` UUID. The test asserts that `GET /images/{id}` has no text chunk, no `loli` substring and no UUID-shaped string. The existing `denyOutput` blocked-path test must still block. A malformed fake PNG must end the job as `failed`, not `done`.

**Done when.** `make test` is green with the new e2e.

#### 0.4 Strip on the serve path

**Files.** `handleImage` in [api.go:699–714](../core/internal/api/api.go). Read the blob (bounded by a cap), `Strip` it, then write it. On error, return 500 "image unavailable" and log. Keep the existing `Cache-Control`.

**Tests.** Write a legacy blob with a `tEXt` chunk straight into the temp blob dir. `GET /images/{id}` must return it stripped, with pixels equal to the original.

**Done when.** Fetching all 99 local blobs through core yields zero text chunks. A backfill rewrite of `DATA_DIR` is optional, for backup hygiene only.

#### 0.5 Purge the leaked upload blobs (D2)

**Files.** A one-off `core/cmd/purgeleaked` (or a documented `make` target). It scans the stored `jobs.workflow` graphs for `LoadImage` upload IDs and deletes those blobs. Use dry-run by default and `-apply` to delete.

**Tests.** A unit test that extracts IDs from a graph fixture.

**Done when.** `GET /images/<each leaked id>` returns 404, and orbit's preset restore still works, because it re-uploads from IndexedDB.

#### 0.6 Pin the invariant in the docs

**Files.**
- [docs/compliance.md](../docs/compliance.md): a new "Output metadata" section stating that delivered bytes carry only allow-listed chunks, and that anything pulsar adds is generated by `pngmeta` from the user-only projection.
- [docs/architecture.md](../docs/architecture.md): the flow becomes `… → strip → CheckOutput → store → serve(strip)`.
- [.claude/skills/core-job/SKILL.md](../.claude/skills/core-job/SKILL.md): add the strip to the flow.

**Done when.** A reviewer can find the invariant and its pinning test from `docs/compliance.md`.

### Phase 1 — chunk expansion in core (MR-B; D3–D6, D8, D9)

#### 1.1 Pure `Expand` in `core/internal/prompt`

**Files.** A new `core/internal/prompt/chunk.go` with:
- `type Chunk struct{ Name, Content string }`
- `type ChunkRef struct{ Name, Hash string }`, where the hash is the first 16 hex digits of the content's sha256
- `Expand(src string, chunks []Chunk, m Mode) (Expansion, error)`, returning the expanded text, a per-rune `Origin`, and the reference spans

Lex rule: `!macro:` + a name obeying D4 + `!`. Expansion is recursive to depth 8 with a per-branch visited set. New located codes: `unknown_chunk`, `chunk_cycle`, `chunk_depth`, and `chunk_in_config` for `ConfigPositive`, `ConfigNegative` and `ParseSafety`, so a token in `QUALITY_TAGS` or `FORBIDDEN_TAG_UC` refuses boot.

**Tests.** `chunk_test.go` goldens:
- an astral character before the reference (UTF-16 offsets)
- commas inside a chunk, which keep the reference's piece index
- A→B→A cycles, self-reference and depth 9
- unknown names
- `!`, `!?` and `!!` still parse as tags
- the token rejected in all config and safety modes

Plus `FuzzExpand`.

**Done when.** The package stays stdlib-only and pure.

#### 1.2 Origin-mapped scanner and tag provenance

**Files.** [nai.go](../core/internal/prompt/nai.go): `newScanner` takes the origin, so `u16`/`piece` resolve expanded runes to the reference span. A `ParseWith(src, mode, chunks)` wrapper is added; `Parse` stays unchanged. [prompt.go](../core/internal/prompt/prompt.go): `Tag` gains ``Chunk *ChunkRef `json:"chunk,omitempty"` ``, and `Equivalent` ignores it. `MaxBytes` and `MaxTags` apply after expansion, with `too_long` attributed to the field.

**Tests.** Differential `TestExpandEqualsInline`: for a corpus of prompts, `ParseWith(ref)` yields the same runes, weights and emitted strings as `Parse(inlined)`, apart from `Span` and `Chunk`.

**Done when.** The differential test is green and the existing `nai_test.go`, `emit_test.go` and `comfy_test.go` pass unchanged.

#### 1.3 Request plumbing in assembly and the API

**Files.**
- [assembly.go](../core/internal/assembly/assembly.go): ``Request.Chunks []prompt.Chunk `json:"chunks,omitempty"` ``; `ParseRequest` uses `ParseWith` for prompt, negative and characters (pose included). Assembly stays pure, because chunks are request data.
- [api/prompt.go](../core/internal/api/prompt.go) and [api/translate.go](../core/internal/api/translate.go): request structs gain `chunks`. Enforce the D4 limits at decode with a 400. Errors use the existing `writePromptError`. The `/prompt/parse` response adds `chunks: [{name, start, end}]` reference spans for orbit's tinting and baking.

**Tests.** In `assembly_test.go`: chunks across all surfaces; the safety block still first; a safety term inside a UC chunk produces `duplicate_dropped`; `Spec` tags carry `chunk`.

**Done when.** `Spec` JSON for a chunked request equals the inlined request's, apart from `chunk` fields.

#### 1.4 Plain mode and compliance coverage

**Files.** In [translate.go](../core/internal/api/translate.go), tags that came from a chunk bypass `Decompose` and are appended as grounded tags (D9). `compliance/*.go` gets no change.

**Tests.** Rows in [prompt_e2e_test.go](../core/internal/api/prompt_e2e_test.go):
- the 400 shape for an error inside a chunk asserts `field`, `code` and the reference's `start`/`end`
- `TestEveryTextInputIsChecked` with chunks in every surface
- a watched tag inside a chunk returns 422
- a history row returns `chunk` provenance and restores `nai`

**Done when.** No production change touches `core/internal/compliance`.

#### 1.5 Phase 1 docs

**Files.** [docs/architecture.md](../docs/architecture.md) (request fields and codes), [docs/frontend.md](../docs/frontend.md) (token rule, and "orbit inserts tokens, core resolves them"), and [.claude/skills/tag-grounding/SKILL.md](../.claude/skills/tag-grounding/SKILL.md) (chunk text bypasses autocomplete and the LLM).

### Phase 2 — chunks in orbit (MR-B; D3, D4, D7, D8, D10)

#### 2.1 `useChunks` store

**Files.** A new `orbit/app/composables/useChunks.ts`, the single owner of `useLocalStorage('pulsar.chunks.v1')`.
- Records: `{id, name, content, color, categoryId, order, createdAt, updatedAt}`; categories: `{id, name, color, order}`; collapsed set in `pulsar.chunks.collapsed`.
- CRUD validates D4. Saving validates content through `/prompt/parse` (surfaces `prompt` and `negative`) and shows located errors in the form.
- Rename rewrites references in the live composer fields, saved presets and pose presets, and inside other chunks.
- Delete bakes using the reference spans returned by `/prompt/parse`. Delete-category moves its chunks to Uncategorized. Delete-all asks for confirmation.
- JSON export and import.

[usePresets.ts](../orbit/app/composables/usePresets.ts): `PresetConfig.chunks?` stores a snapshot of the referenced chunks. Restore recreates missing ones and notes any mismatch.

**Tests.** `tests/chunks-store.test.ts`: CRUD, uniqueness, forbidden characters, the rename rewrite, delete-bake by span, export/import round-trip, the storage key, preset snapshot restore.

**Done when.** Chunks persist across a reload in the browser and in the Capacitor shell.

#### 2.2 The `@` picker

**Files.**
- A new `orbit/app/composables/useChunkPicker.ts` implementing:
  - the trigger rule (start, after whitespace, after a comma, empty field)
  - the query running to the caret, and a comma closing the picker
  - the five-tier ranking
  - Arrow keys, Enter/Tab, Escape, and a 150 ms close on blur
- A new `orbit/app/components/ChunkPicker.vue`, using a Reka listbox anchored at the caret.
- Wiring in `base/Input.vue`, `TagInput.vue`, `CharacterCard.vue`, `create.vue` and `pose.vue`.

Insertion writes `!macro:Name!` into a textarea, or a chunk chip in tags mode. The chip carries a flag so `normalize()` skips it ([useTagAutocomplete.ts:42–50](../orbit/app/composables/useTagAutocomplete.ts)).

**Tests.** `tests/chunk-picker.test.ts` covers the trigger positions (a mid-word `@` does nothing), the comma close, a ranking fixture that mirrors NovelAI's tiers, and the keys. `tests/tag-autocomplete.test.ts` asserts zero `/tags` or `/tags/lookup` calls while the picker is active, and that chunk chips stay unnormalised.

**Done when.** Typing `@sil` then Enter inserts `!macro:Silver Wolf!`, and generating produces chunk provenance in the `Spec`.

#### 2.3 Chunk panel and form

**Files.**
- A new `orbit/app/components/ChunkPanel.vue`: a Reka Popover with Tabs "Prompt Chunks" and "Settings", opened from a header button on the prompt box.
  - Uncategorized first, then categories.
  - Chips show a ✎ and a tooltip with the content plus "Click to insert, drag to reorder".
  - Click inserts at the last-focused field's caret, with a toast if no field has been focused.
  - HTML5 drag reorders, using its own MIME types.
- `ChunkForm.vue`: name, content, a category select (Uncategorized first), and a Reka `ColorSwatchPicker` over the D10 palette. Delete in edit mode.
- On mobile, a drawer tab.

**Tests.** A component test for insert-at-caret, and the toast when nothing is focused.

**Done when.** The panel is fully keyboard-reachable.

#### 2.4 Rendering and the request wiring

**Files.**
- [prompt-issues.ts](../orbit/app/utils/prompt-issues.ts): `Range` gains `kind` (`error | chunk | up | down | op`).
- [base/Input.vue](../orbit/app/components/base/Input.vue): kinds map to component classes.
- [main.css](../orbit/app/assets/css/main.css): `--color-chunk-1…8` palette tokens, and a `.chunk-mark` class that reads `--chip`.
- [base/TagChip.vue](../orbit/app/components/base/TagChip.vue): renders chunk chips via `--chip`.
- A caret inside a token shows its expansion in a tooltip. Double-click bakes it by span.
- [usePromptParse.ts](../orbit/app/composables/usePromptParse.ts), [useCreateComposer.ts](../orbit/app/composables/useCreateComposer.ts) and [useGeneration.ts](../orbit/app/composables/useGeneration.ts) send only the referenced chunks.
- [types/api.ts](../orbit/app/types/api.ts) gains the new fields.

History restore shows the token when the hash matches (D8) and the expanded `nai` tags otherwise.

**Tests.** `base-input.test.ts` covers typed marks with error precedence. A colour-validation test rejects any non-palette string.

**Done when.** `pnpm --dir orbit lint` finds no arbitrary colour values.

### Phase 3 — the Settings tab (MR-B; D12, D13)

#### 3.1 Disable Tag Suggestions

**Files.** [useTagAutocomplete.ts](../orbit/app/composables/useTagAutocomplete.ts) reads `useLocalStorage('pulsar.settings.tagSuggestions', true)`. When it is false, `search` short-circuits. A toggle in the `ChunkPanel` Settings tab uses `base/ToggleSwitch.vue`.

**Tests.** Mocked API: no `/tags` or `/tags/lookup` call while disabled. The commit keys still work, the `@` picker still opens, and `useTranslate.resolveTags` previews still run.

**Done when.** The toggle changes every editor on the page immediately.

#### 3.2 Highlight Emphasis

**Files.**
- **Core.** In [nai.go](../core/internal/prompt/nai.go), `Run` gains a `Span` (`json:"-"`) recorded in `build()`, and the scanner records operator spans (`{ } [ ] N:: ::`) with the running weight. [api/prompt.go](../core/internal/api/prompt.go) returns `marks: [{start, end, kind, w}]`, emitted only for user surfaces, and skips text that came from a chunk.
- **Orbit.** Map `w` to intensity with NovelAI's formula: `0.2 + 0.4·clamp(|w−1|)`, nothing below `|w−1| < 0.01`, `op` at 0.5. Add the tokens `--color-emph-up`, `--color-emph-down` and `--color-emph-op` to `@theme`. The toggle is `pulsar.settings.highlightEmphasis`, defaulting to true. In tags mode, `BaseTagChip` takes an intensity prop.

**Tests.** A core golden for the spans of `{a}, [b], 1.5::c::, d`. A vitest test of mark segmentation and that toggling off removes emphasis but not error marks.

**Done when.** `{{silver hair}}` tints orange within one parse cycle, and the setting persists.

### Phase 4 — pulsar's own output metadata (MR-C; D14–D19)

#### 4.1 A pure payload builder

**Files.** A new `core/internal/outmeta`:
- `FromSpec(spec assembly.Spec, kind string) Payload` builds the user-only projection: `source == user` tags for prompt, UC and characters; size, steps, CFG, sampler, scheduler, rescale, seed, lineage, style slug, hires settings, and chunk provenance names.
- `A1111(p)` is a port of `create_infotext` ordering plus `quote()`. Its prompt is `prompt.EmitComfy` of the user tags, the SD syntax the model actually received.
- `JSON(p)` produces `{"v":1,…}`.
- `XMP(kind)` produces the D14 packet.
- `pngmeta` gains `ITXt(keyword, text)` and inserts after IHDR.

**Tests.**
- Goldens.
- A Go port of A1111's `re_param_code` proves the settings line yields at least three pairs and that quoted values round-trip.
- A deny-list test: no `cfg.ForbiddenTagUC` term, no `DEFAULT_UC` or `QUALITY_TAGS` text, no style prefix or suffix, no UUID-shaped string, no `.safetensors` path, no job ID.

**Done when.** The package is pure and has no DB import.

#### 4.2 Serve-time insertion and the download setting

**Files.**
- `handleImage`: after `Strip`, always insert the XMP packet. When `?meta=1`, look up `jobs.params` (strip the role suffix to get the job ID) and also insert `parameters` and `pulsar`.
- [download.ts](../orbit/app/utils/download.ts), [HistoryDetail.vue](../orbit/app/components/HistoryDetail.vue) and [useHistoryPage.ts](../orbit/app/composables/useHistoryPage.ts): append `?meta=1` unless "Download clean" (`pulsar.settings.downloadMetadata`, D16) is chosen.

**Tests.** e2e:
- `GET /images/{id}` carries XMP only.
- `?meta=1` carries all three chunks and none of the deny-list.
- Pose role images are covered.
- A blocked job still has no image.

**Done when.** The Forge PNG Info tab or receyuki reads a pulsar download (a manual check, recorded in the MR).

#### 4.3 Phase 4 docs

**Files.** [docs/compliance.md](../docs/compliance.md) records what is written, what is excluded and why (including the job-ID rule). [docs/frontend.md](../docs/frontend.md) records the download setting.

### Phase 5 — the inspector (MR-C; D11, D20–D24)

#### 5.1 Container walkers in a Web Worker

**Files.** A new `orbit/app/utils/imgmeta/`:

| File | Contents |
|---|---|
| `png.ts` | Walks past IDAT; tEXt as Latin-1; zTXt/iTXt through `DecompressionStream('deflate')` under caps; APNG `comf` |
| `jpeg.ts` | APP1 `Exif\0\0` → IFD0/ExifIFD: 0x9286 UserComment, 0x0110 Model, 0x010F Make, ImageDescription, Software |
| `webp.ts` | RIFF `EXIF`/`XMP ` chunks |
| `usercomment.ts` | Prefix, BOM, zero-byte heuristic, then TIFF byte order |
| `stealth.ts` | Four signatures, alpha and RGB, column-major, bit-length bounds, gzip cap 1 MiB, pixel cap 16.7 MP |
| `limits.ts` | The limits from the hostile-files section |

Plus `orbit/app/workers/imgmeta.worker.ts` and `useImageMeta.ts`. Pixels are decoded with `createImageBitmap(…, {premultiplyAlpha:'none', colorSpaceConversion:'none'})`. If the signature golden in 5.3 fails, adopt `fast-png` (11 kB gz) for PNG.

**Tests.** Fixtures built in-test:
- a PNG builder writing tEXt, zTXt and iTXt (including CJK as iTXt)
- UserComment in UTF-16BE (A1111) and UTF-16LE (SwarmUI)
- an APNG `comf` chunk after IDAT
- a WebP with an EXIF `prompt:` Model tag
- a stealth encoder covering all four variants

Hostile files (all rejected within the caps, and the worker is terminated after a timeout):
- a chunk length of `0x7fffffff`
- a zTXt bomb
- a stealth length larger than the image's capacity
- a 100k × 100k IHDR
- 10k text chunks

**Done when.** No parse runs on the main thread.

#### 5.2 Format parsers and detection

**Files.** `orbit/app/utils/imgmeta/parsers/`:

| Parser | Contents |
|---|---|
| `novelai.ts` | Six keys; `Comment`; characters from `v4_prompt`; `actual_prompts`; request type |
| `nai-models.ts` | The hash → model table |
| `a1111.ts` | The verbatim regex; the ≥ 3-pair rule; `Negative prompt:`; unquote; hashes sub-split |
| `comfyui.ts` | `NaN`/`Infinity` sanitise; sampler fed by `EmptyLatentImage`; 512-visit walk; text-encode-only `workflow` fallback |
| `swarmui.ts` | `sui_image_params` |
| `invokeai.ts` | `invokeai_metadata`, plus legacy `sd-metadata` and `Dream` |
| `pulsar.ts` | Pulsar's own `pulsar` JSON |

`detect.ts` is the ordered cascade: pulsar → NovelAI (`Software` + `Comment`) → SwarmUI → A1111 → InvokeAI → ComfyUI. Stealth payloads are re-dispatched by content: A1111 text, NovelAI JSON or SwarmUI JSON.

**Tests.**
- A1111's docstring example.
- NovelAI's bundle example `0001.webp` embedded in a synthetic PNG and in WebP EXIF.
- A ComfyUI graph with a hires second sampler, where the first-pass prompt must win.
- A ComfyUI graph whose text goes through an unknown node, where the raw view still shows it.
- A pulsar download round-tripping from Phase 4.

**Done when.** Each Tier-1 format has at least two fixtures.

#### 5.3 NovelAI signature verification (D21)

**Files.** `orbit/app/utils/imgmeta/nai-verify.ts`. Strip `signed_hash` textually with NovelAI's exact regex. The message is the RGB bytes (or the RGBA bytes with alpha LSBs cleared) followed by the UTF-8 of the stripped Comment. Verify ed25519 against an injectable key, defaulting to NovelAI's. Use WebCrypto Ed25519 where the WebView supports it, otherwise a small audited JS implementation; measure the bundle cost in the MR.

**Tests.**
- A test keypair signs a synthetic image: it is valid; one flipped pixel is invalid; an edited Comment is invalid; an edited Description is still valid.
- A real NovelAI PNG from the owner's account, committed under `orbit/tests/fixtures/` as the golden.

**Done when.** The real golden verifies.

#### 5.4 `/inspect` page and navigation

**Files.**
- A new `orbit/app/pages/inspect.vue`: `useDropZone`, `useFileDialog` and paste.
  - Header: Software, model, and "Signed by NovelAI ✓".
  - A Simplified/Raw toggle, persisted in `pulsar.inspect.raw`.
  - A raw-chunk list, rendered with `{{ }}` in `pre-wrap`, with control characters escaped.
- Navigation in [AppHeader.vue](../orbit/app/components/AppHeader.vue) and [base/TabBar.vue](../orbit/app/components/base/TabBar.vue).
- A page-table row in [docs/frontend.md](../docs/frontend.md).

**Tests.** A component test that metadata containing `<img src=x onerror=…>` renders as text and creates no element.

**Done when.** Four mobile tabs fit at 360 px width.

#### 5.5 Import into `/create` (D11, D22, D23)

**Files.**
- A new `orbit/app/components/MetaImportDialog.vue` with toggles per D22.
  - Prompts are split by calling `/prompt/parse` and using `tags[].nai` and `relocated`; orbit never splits syntax itself.
  - Chunk re-tokenizing happens at whole-tag boundaries (D11).
  - The result goes into `usePendingConfig()`, followed by `navigateTo('/create')`.
  - "Use as base image" and "Use as reference" call `useImageUpload().upload(file)`.
  - Dropping a file onto `/create` opens the same dialog.
- **Core.** [api/prompt.go](../core/internal/api/prompt.go): `/prompt/parse` gains `"clean": true`. With it, `<lora:…>`, `embedding:`, `|` and `||…||` are dropped with notices instead of errors. Grammar stays in core.

**Tests.**
- Core: `clean` rows in `prompt_e2e_test.go`.
- Orbit: an import of a NovelAI fixture fills prompt, UC and characters with settings untouched; CFG and sampler are never applied.

**Done when.** An A1111 prompt with LoRA tags imports to a generatable state in two clicks.

#### 5.6 WebP ingest (D24)

**Files.** [ingest.go](../core/internal/ingest/ingest.go) registers `golang.org/x/image/webp`, and [core/go.mod](../core/go.mod) promotes `x/image` to a direct dependency.

**Tests.** `ingest_test.go`: a WebP input normalises to PNG, and `CheckUpload` still runs.

**Done when.** An inspected WebP can be used as an img2img base.

### Phase 6 — signed provenance (MR-D; conditional on D1 and D19)

#### 6.1 C2PA manifest and second marking layer

**Files.**
- A `c2patool` sidecar invoked by core after `pngmeta` insertion. It writes a `c2pa.created` action with `digitalSourceType` set to `trainedAlgorithmicMedia` or the composite term ([C2PA 2.2](https://spec.c2pa.org/specifications/specifications/2.2/specs/C2PA_Specification.html)).
- Signed outputs are stored at rest, and the serve path skips `Strip` for them, because it would delete `caBX`.
- Evaluate a pixel watermark as the second layer, and plan for the free detection that the Code of Practice expects.

**Tests.** The C2PA manifest validates with `c2patool` on a delivered file. A served signed file is byte-identical to the stored one.

**Done when.** Counsel signs off on the D19 posture before any non-private launch.
