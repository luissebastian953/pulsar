# orbit — frontend spec

The Nuxt 3 client. Talks only to `core`. Ships to web, desktop (Electron), and
mobile (Capacitor) from one codebase.

Design direction, tokens, and layouts live in `docs/design.md` — this doc is the
build stack and what the multi-target shells force.

---

## 1. Stack

| Layer | Choice | Why |
|---|---|---|
| Framework | **Nuxt 3** | settled |
| Primitives | **Reka UI** | headless — our tokens *are* the styling |
| CSS | **Tailwind v4** | design tokens as `@theme` vars |
| Icons | **Lucide** via `@nuxt/icon` | on-demand from Iconify, tree-shaken |
| Utilities | **VueUse** (+ `lodash-es` if needed) | see §1.2 |
| Lint | **ESLint** via `@nuxt/eslint` | flat config, Nuxt-aware |
| Desktop | **Electron** | see §6 |
| Mobile | **Capacitor** | see §6 |

### 1.1 Tailwind v4 — tokens are the source of truth

v4 configures in CSS, not `tailwind.config.js`. The design tokens map straight into
`@theme`, so `docs/design.md` stays authoritative rather than being duplicated into
class names:

```css
/* app/assets/css/main.css */
@import "tailwindcss";

@theme {
  --color-void:    #0B0E14;
  --color-surface: #141922;
  --color-raised:  #1C2230;
  --color-line:    #2A3242;
  --color-line-hi: #3A455C;
  --color-text:    #E4E8F0;
  --color-dim:     #8A94A8;
  --color-faint:   #5B6478;

  /* Danbooru categories — semantic, not decorative */
  --color-t-general:   #6AB7FF;
  --color-t-character: #35C64A;
  --color-t-copyright: #C797FF;
  --color-t-artist:    #FF8A8B;
  --color-t-meta:      #EAD084;

  --color-beam: #E8F4FF;
  --color-warn: #FFB86B;
  --color-danger: #FF5A5F;  /* prompt errors — the server will refuse it */

  --font-sans: "IBM Plex Sans", system-ui, sans-serif;
  --font-mono: "IBM Plex Mono", ui-monospace, monospace;

  --radius-tag:  3px;
  --radius-ctrl: 6px;
  --radius-canvas: 8px;
}
```

Gives you `bg-void`, `text-t-general`, `font-mono`, `rounded-ctrl` — and one place to
change a token. **Don't add arbitrary values (`bg-[#6AB7FF]`) in components.** If a
colour isn't in `@theme`, it doesn't belong in the design.

### 1.2 Utilities — VueUse over lodash

**VueUse is the better fit and covers what lodash would be for here.** It's
Vue-reactive, tree-shakeable, and two of its composables map directly onto things
already specced:

- `useDebounceFn` — the resolved strip's ~400ms debounce
- `useEventSource` — **SSE progress streaming**, with reconnection handled
- `useLocalStorage` — token storage for the shells
- `useDropZone`, `useFileDialog` — reference/img2img upload

**Lodash: only if a concrete need survives.** Modern JS covers most of it natively,
and the realistic ask here is `debounce`, which VueUse already does better (it's
reactivity-aware). If something genuinely needs it — `groupBy` on tag categories,
deep `merge` on workflow params — use **`lodash-es`**, never `lodash`, so it
tree-shakes:

```ts
import debounce from 'lodash-es/debounce'   // ✓ tree-shakes
import _ from 'lodash'                       // ✗ pulls the world
```

This is a mobile-shipped client. Bundle size is a real constraint.

### 1.3 ESLint

Use `@nuxt/eslint` — it generates a project-aware flat config rather than fighting
Nuxt's auto-imports and generated types.

```bash
npx nuxi module add eslint
```

```js
// eslint.config.mjs
import withNuxt from './.nuxt/eslint.config.mjs'

export default withNuxt({
  rules: {
    // tokens only — no arbitrary colours in components
    'vue/multi-word-component-names': 'off',
  },
})
```

Verify the module name and current setup — Nuxt's ESLint story was consolidated
recently and older blog posts describe a superseded approach.


**No styled component library.** Not Vuetify, not PrimeVue, not Nuxt UI. We have a
design spec with its own tokens and semantic colour system; a styled library replaces
those decisions with someone else's and we'd spend our time overriding. Reka UI ships
behaviour and accessibility with zero opinion about appearance — that's the fit.

**Reka UI is only needed for four things:** Combobox (tag autocomplete), Popover
(model selector), Dialog (reference upload), Toast. Everything else is native HTML —
`<details>` folds, `<input type=range>` sliders, `<button aria-pressed>` toggles and
segments. Don't import a library to get a `<details>`.

**The one thing not to hand-roll: the tag autocomplete.** It's a combobox over ~50k
rows needing ARIA combobox semantics, arrow/enter/escape/tab handling, portal
positioning to escape the rail's overflow, virtualisation, and composition-event
handling (users type Japanese). Every one of those is a subtle bug factory.

---

## 2. Type

| Face | Role | Rule |
|---|---|---|
| **Orbitron** | logo lockup only | **Never in the UI.** Display-only by design — the strict geometry causes reading fatigue below 24px. Ships as outlines, so no webfont needed. |
| **IBM Plex Sans** | all UI chrome | 400/500/600/700 |
| **IBM Plex Mono** | tags, values, params, counts | tags *are* identifiers — mono makes machine vocabulary legible and separates it from human writing |

The split is the typefaces' own design intent, not preference. Anything a human wrote
is sans; anything that is a value, count, parameter, or tag is mono.

**Orbitron alternates:** enable `ss01` (alt A) + `salt` (alt R, S) on the wordmark.
Default Orbitron is the most-reached-for sci-fi face on the web; the alternates cost
nothing and make it not-quite-anyone-else's. Set in Figma's OpenType panel before
outlining.

---

## 3. Logo assets

| Asset | Use |
|---|---|
| `pulsar-singular.svg` (60×60) | app header mark, animated |
| Mark + Orbitron lockup | brand surfaces only — marketing, login, OG image, README |
| **Small mark variant** ⚠️ **missing** | favicon, ≤32px contexts |

**The small variant is the one real gap.** The current mark dies below ~40px: the
0.2-opacity outer ring vanishes and the hairline flare aliases to noise. Rule for the
redraw: **three shapes max, nothing thinner than ~2px on the 60 grid** — one ring at
~0.8, the slashed core, one beam thickened to a real stroke.

**The mark replaces the placeholder beacon** in the header. Animate it on the 2.4s
beat — rotate the beam or pulse the ring opacity. It's the name's payoff.

---

## 4. Pages

| Route | State |
|---|---|
| `/create` | art generation — spec'd, mockup exists |
| `/pose` | pose references — spec'd |
| `/manga` | phase 7 |
| `/history` | gallery |
| `/inspect` | image inspector — read the generation metadata of a PNG / JPEG / WebP, import it into `/create` ([below](#image-inspector)) |
| `/login` | age-gate lives here (see `docs/compliance.md`) |

Both `/create` and `/pose` share the same shell: header, composer rail, stage. Only
the composer differs.

---

## 5. Component inventory

**Signature — build this first, spend the detail budget here**
- `ResolvedStrip` — live prose→tag translation, category colours, post counts,
  dropped concepts. §4 of the design spec. If this is right and everything else is
  mediocre, the product is still distinctive. If it's a generic chip list, we've
  built a NovelAI clone.

**Composer**
- `PromptBox` (textarea + mode toggle) · `TagInput` (Reka Combobox) ·
  `Fold` (`<details>` + state badge) · `StylePicker` (preset cards from
  `GET /styles`, disabled when the LoRA is missing) · `CharacterCard` ·
  `ReferenceUpload` · `AspectSelect` · `BatchSegment` · `AdvancedPanel`

**Prompt validation** (docs/tagdb.md#weighting)
- Core owns the grammar; orbit never parses or builds prompt syntax. Every
  prompt surface — Describe (chips in tags mode, the textarea in plain mode),
  Undesired content, each `CharacterCard` — is validated live through
  `usePromptParse`: a debounced `POST /prompt/parse` with the surface kind
  and the active lineage (stale answers dropped). `/generate`'s located 400
  (`{error, code, field, start, end, tag, hint}`, `field` = `prompt` |
  `negative` | `characters.N`) is mapped back onto the same field while its
  text is unchanged; a 400 inside a folded section opens the fold.
- Offsets are UTF-16 code units — JS string indices, no conversion. In tags
  mode chips are joined with `", "` exactly as sent, so an error span maps to
  the chips it overlaps (`utils/prompt-issues.ts`).
- An error shows four ways: the offending chip red and bold
  (`BaseTagChip invalid`), or the offending span marked by a mirror layer
  behind a transparent textarea (`BaseInput highlights`; caret and selection
  stay native); a red outline on the input (`invalid`); the input's label red
  and bold (`BaseSectionLabel` / `BaseFold` / the character label); and the
  message with its hint below the input (`PromptIssues`). All colour comes
  from `--color-danger`.
- Notices (relocated to Undesired Content, converted from SD syntax,
  calibrated for the lineage, unclosed `w::` section) render quietly under
  the input in `text-faint`.
- Chips that carry weighting syntax keep their spaces (the space before a
  closing `::` is load-bearing); plain chips normalise to tag-name form.
- Plain mode sends `/translate`'s `prompt` (the grounded tags in NovelAI
  syntax, weights kept), not bare canonical names.
- History round-trip: editor boxes restore from the stored structured tags
  whose `source` is `user` (their `nai` form) — never the safety block,
  `DEFAULT_UC`, quality or style tags, which core re-adds; `HistoryDetail`
  displays from tags. Legacy rows without tags fall back to the string, and
  core's negative dedupe neutralises the safety copies they carry.

<a id="prompt-chunks"></a>
**Prompt chunks** (docs/architecture.md#prompt-chunks)
- A chunk is a named, reusable snippet. The reference token is
  `!macro:Name!`. **Orbit inserts tokens; core resolves them** — expansion,
  nesting, limits, errors and every offset are core's. Orbit only recognises
  where a token is (`utils/chunks.ts`) so it can insert, rename, tint and
  send it; it never expands one.
- `@` is only an editor trigger (`useChunkPicker` + `ChunkPicker`): at the
  start, after whitespace or after a comma it opens the picker; the query
  runs to the caret (spaces allowed, a comma closes it); ArrowUp/Down, Enter
  or Tab, Escape, closes 150 ms after blur. Ranking: name starts-with,
  contains, subsequence; the same on "Category: name"; then content. Picking
  writes the token into a textarea, or adds a chunk chip in tags mode (the
  chip is the token; `normalize()` leaves it verbatim).
- Storage: `useChunks` is the single owner of
  `useLocalStorage('pulsar.chunks.v1')` (collapsed categories in
  `pulsar.chunks.collapsed`), with JSON export/import — no table, core has no
  identity. Every request (`/generate`, `/prompt/parse`, `/translate`) carries
  only the chunks its text references, nested ones included. Names: trimmed,
  case-sensitive, unique, ≤ 64 chars, none of `! , | { } [ ] ::`; ≤ 256
  chunks, ≤ 4 KiB each.
- `ChunkPanel` (a popover on desktop, a bottom sheet on a phone; the button
  sits in the prompt box header) has two tabs. *Prompt Chunks*: Uncategorized
  first, then categories; click inserts at the last-focused prompt field's
  caret (a toast if none was focused), ✎ edits, drag or Alt+←/→ reorders.
  `ChunkForm` validates content through `/prompt/parse` before saving and
  shows core's located errors. *Settings*: see below.
- Colours are a fixed palette, `--color-chunk-1…8`, stored by key and applied
  as `data-color` on a `.chunk-color` element (which sets `--chip`). Picked
  with Reka's `ColorSwatchPicker`. No free hex.
- Rendering: `PromptInput` (the prompt textarea: `BaseInput` + picker) paints
  each resolved reference core located in its chunk's colour; the caret
  inside a token shows its expansion; a double-click bakes it into plain text
  by core's span. `BaseInput highlights` ranges are typed
  (`error | chunk | op | up | down`); an error always wins, a chunk is never
  tinted by weight.
- Rename rewrites the token in the live fields, the other chunks, saved
  configs and pose presets. Delete bakes references first (by core's spans),
  so none dangle; delete-category moves its chunks to Uncategorized.
- A saved config stores a copy of the chunks it references and recreates
  missing ones on apply. History restore shows a token only where today's
  chunk still resolves to exactly the stored tags (same hash, same tags);
  otherwise the expanded tags are restored.
- Settings (per browser): `pulsar.settings.tagSuggestions` — off silences
  only the tag autocomplete popup (`useTagAutocomplete.search`); the `@`
  picker, the commit keys and the resolved-strip previews keep working. A
  picker query is never sent to `/tags`, the live lookup or `/translate`,
  whatever the setting. `pulsar.settings.highlightEmphasis` (on by default)
  tints core's `marks` in NovelAI units: `--color-emph-up` / `-down` at
  `0.2 + 0.4·clamp(|w−1|)` (nothing within 0.01 of 1), `--color-emph-op` at
  0.5 for operators; in tags mode the chip is tinted. Marks arrive with the
  debounced parse and are absent while the surface has a parse error.
- `pulsar.settings.downloadMetadata` (on by default; "Download clean" in the
  Settings tab and beside the history modal's Download button turns it off).
  On: downloads fetch `/images/{id}?meta=1` and carry the prompt and settings
  (`utils/download.ts` `downloadUrl`, used by `HistoryDetail` and
  `useHistoryPage`). Off: the plain URL, whose only metadata is the
  AI-generated mark core adds to every served image. What is and is not
  written: docs/compliance.md#output-metadata.
- `useChunks` and `usePromptSettings` are page-wide singletons in a detached
  effect scope: they must keep persisting after the component that first
  asked for them unmounts.

<a id="image-inspector"></a>
**Image inspector** (`/inspect`, and drop-onto-`/create`)
- `pages/inspect.vue`: drop (`useDropZone`), pick (`useFileDialog`) or paste a
  PNG, JPEG or WebP. The file is parsed by `useImageMeta` in a **Web Worker**
  (`workers/imgmeta.worker.ts`, terminated on result, timeout or supersede) —
  never on the main thread, and never uploaded: there is no core endpoint
  that accepts an image just to read it (one ingest path).
- `MetaView` renders the result. Header: generator, Software, model/Source.
  **"Signed by NovelAI ✓" appears only when the signature result is `valid`;
  every other outcome shows nothing.** A Simplified / Raw toggle is persisted
  in `pulsar.inspect.raw`; Raw lists every metadata entry with its key and
  container. Readers: NovelAI (PNG text, WebP EXIF, stealth alpha), A1111 /
  Forge, ComfyUI, SwarmUI, InvokeAI, pulsar (`utils/imgmeta/`).
- **Every string from a file is untrusted.** It is rendered with `{{ }}` only
  — no `v-html`, no auto-linking, never as a URL or a path — with control and
  bidi characters made visible (`escapeControlChars`). The app holds a bearer
  token; `tests/meta-view.test.ts` pins that HTML/script payloads create no
  element.
- `MetaImportDialog` ("What do you want to do with this image?") is the one
  import dialog, opened from `/inspect` and by dropping an image on `/create`.
  Defaults: **prompt, Undesired Content and characters on; settings and seed
  off.** "Settings" is a bucket-snapped size and steps only — guidance,
  sampler, schedule and rescale are never imported (settings don't transfer
  between lineages), and the patch type (`MetaImport`) has no field for them.
- Import logic is `utils/meta-import.ts` (`buildMetaImport`). Orbit owns no
  grammar: every text goes through `POST /prompt/parse` with `clean: true`,
  which drops `<lora:…>`, `embedding:…`, `|` and `||…||` with notices, and
  the result is built from `tags[].nai` and `relocated`. On top of that:
  NovelAI `actual_prompts` are preferred; NovelAI's automatic quality suffix
  and UC preset are stripped when `tag_hint_qt` / `tag_hint_uc_preset` say
  they were added (tables hold only values seen in real metadata); character
  UCs are merged into the main UC with a notice; negative-weight tags move to
  the UC; a pulsar download imports its `nai` text (weights as typed). Local
  prompt chunks are put back only where a whole-tag run equals a chunk of at
  least two tags, with a notice — never by substring. Text core cannot parse
  is imported as written, so the editor's live validation marks it.
- The result is a **partial** patch handed over with `usePendingImport()`
  (not `usePendingConfig()`: a full config would reset every setting the
  import must leave alone); `useCreateComposer.applyImport` writes only the
  fields it carries. "Use as base image / reference" hands the `File` to the
  composer, which uploads it through `useImageUpload().upload` — the normal
  `POST /uploads` path, `CheckUpload` included. Uploads accept WebP too.
- Imported text is ordinary editor text: validated live, and checked by
  `CheckPrompt` at `/generate` like anything typed.

**Stage**
- `Canvas` (aspect-matched, empty/progress/result states) · `ProgressBar` (SSE) ·
  `ResultGrid` (2×2 for `/pose`) · `OutputTabs` (lineart/skeleton/source) ·
  `RecentStrip`

**Chrome**
- `Header` (animated mark, nav, model chip, credits) · `Toast` · `AgeGate`
- Navigation is Create · Pose · History · Inspect, in the header (desktop)
  and `base/TabBar` (mobile). Four tabs are 90 px each at a 360 px viewport;
  a fifth needs a re-check.

---

## 6. Multi-target shells — read before building

One codebase, three shells. The shells are cheap; what they *force on the
architecture* is not.

### What Electron + Capacitor force

**1. Nuxt goes SPA.** Capacitor serves static files from a local bundle; Electron
loads a local build or a remote URL. Neither runs a Nuxt SSR server. So the app
targets `ssr: false` + `nuxt generate`. This is fine — `/create` is an authenticated
tool, not a marketing page, and it has no SEO need. But decide it now: retrofitting
SSR-out later means unpicking every server-side assumption.

**2. Absolute API base URL, configured per target.** On web, `orbit` can call `core`
at a relative path. In Electron and Capacitor there is no origin to be relative to.
Every target needs `NUXT_PUBLIC_API_BASE` and the client must never assume same-origin.

**3. Token auth, not cookie sessions.** Capacitor runs on `capacitor://localhost`,
Electron on `file://`. Cookie semantics differ or break outright across these origins.
Use bearer tokens in storage. **This is a `core` change, not an `orbit` change** — if
auth ships as cookie sessions first, it gets rewritten.

**4. CORS in `core`.** It must accept `capacitor://localhost`, `http://localhost`
(Electron dev), and the production web origin. Currently `core` assumes same-origin.

**5. SSE must survive the shells.** Progress streaming is `EventSource`; verify it
across the Capacitor WebView and Electron's renderer, including reconnection. This is
the one runtime behaviour most likely to break silently per-platform.

### What each shell actually buys

**Electron** — a window around a web app that talks to a remote GPU. The honest value
is narrow: filesystem access for batch export, a local ComfyUI pointed at
`localhost` without CORS friction, no browser chrome. If the answer is "it's a window",
that's a PWA, and it's free.

**Capacitor** — a real mobile app. But see below.

### ⚠️ App store distribution is a real blocker

This is an adults-only anime image generator. Apple's App Review Guidelines have
long prohibited overtly sexual or pornographic content, and an app whose purpose
includes generating it does not pass review. Google Play restricts it heavily and has
been tightening. Verify current policy before investing — but plan on the assumption
that **iOS App Store distribution is not available for the uncensored product**.

Realistic paths:
- **PWA** — installable, offline shell, home-screen icon, no store, no wrapper, no
  review. Gets most of Capacitor's value for a thin client. **Start here.**
- **Android APK / sideload or F-Droid** — direct distribution, no Play policy.
- **Capacitor iOS** — only viable for a SFW-gated build, which is a different product.

**My recommendation:** ship web first, add the **PWA manifest** (near-free, no
architectural cost beyond §6.1–6.5), and treat Electron and Capacitor as later
targets built on the same SPA + token-auth + CORS foundation. Do §6.1–6.5 *now*
regardless — they're cheap up front and expensive to retrofit, and they're what makes
any shell possible later.

### Repo shape when the shells land

```
orbit/
├── app/            # Nuxt — the shared client
├── shells/
│   ├── electron/   # main process, preload, builder config
│   └── capacitor/  # capacitor.config.ts, ios/, android/
└── package.json
```

Shells are thin and stay thin. All logic lives in `app/`. If a shell grows real
logic, that logic belongs in `core`.

---

## 7. Build order

1. **`ResolvedStrip`** — the signature. Prove it against a stub `/translate`.
2. Composer + folds — native HTML, no dependencies.
3. `TagInput` (Reka Combobox) against real `/tags`.
4. Canvas + SSE progress.
5. `/pose` — reuses everything; new work is the axis UI and output tabs.
6. PWA manifest.
7. Shells, if still wanted.

Steps 1–4 need no GPU. `core` can serve stubbed responses; the interesting frontend
work isn't blocked on a pod.
