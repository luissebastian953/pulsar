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

**Stage**
- `Canvas` (aspect-matched, empty/progress/result states) · `ProgressBar` (SSE) ·
  `ResultGrid` (2×2 for `/pose`) · `OutputTabs` (lineart/skeleton/source) ·
  `RecentStrip`

**Chrome**
- `Header` (animated mark, nav, model chip, credits) · `Toast` · `AgeGate`

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
