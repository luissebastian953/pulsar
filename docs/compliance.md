# Compliance

**Read this before anything ships to real users.**

This is not a policy checkbox bolted onto a generation product. It is the layer that
determines whether pulsar is a lawful product or a criminal liability, and it is the
hardest engineering in the repo.

## Why this is not optional

Booru datasets contain sexualized depictions of characters that read as minors.
Anime models trained on them reproduce that content *readily*, because it is in the
training distribution — not as an edge case, but as something the model will produce
without being asked to.

That content is illegal in essentially every jurisdiction. It is what gets operators
prosecuted and platforms seized, not merely banned.

So the engineering reality is the inverse of the intuitive framing: the hard work in
adult anime generation is not removing safety filters to "unlock NSFW." It's
**adding rigorous apparent-minor exclusion** to both training data and outputs.
Stripping filtering wholesale doesn't produce a permissive adult product; it
produces one that reliably generates illegal content and ends the project.

Every legitimate operator — NovelAI included — runs classifiers for exactly this and
treats it as non-negotiable. So do we.

## Scope

| Surface | Requirement |
|---|---|
| Generated outputs | Classify every image before delivery. No exceptions. |
| User uploads | Classify on ingest (img2img, references, inpaint bases). |
| Training data (phase 5) | Filter the corpus **before** a single training step. |
| Manga (phase 7) | Every panel classified; story LLM needs its own guardrails — narrative context makes prompts less obviously flaggable. |
| Access | Age-gate. Adults-only. |
| Audit | Moderation logging. |

## Hooks

Two call sites exist in `internal/compliance/` from the first commit. They may be
stubs before phase 4 — the call sites are not negotiable.

```
POST /generate
  └─► compliance.CheckPrompt(spec)      ─► reject before enqueue
      ...generation...
  └─► pngmeta.Strip(image)              ─► metadata out first (see Output metadata)
  └─► compliance.CheckOutput(image)     ─► state = blocked, never delivered

POST /images
  └─► compliance.CheckUpload(image)     ─► reject on ingest
```

**`blocked` is a first-class job state** — `queued → running → done | failed |
blocked`. It exists in the schema from day one. Retrofitting it after the happy path
is built is exactly where corners get cut, because by then the delivery path assumes
success.

An output that fails classification is never written to public storage and never
returned. Log it, block it, count it.

## Design constraints

- **Fail closed.** Classifier unavailable → block, don't pass. A generation that
  can't be checked is a generation that doesn't ship.
- **Post-generation, pre-delivery.** Not post-delivery, not client-side. The image
  must not leave the server unclassified.
- **The gate is in the main line, not beside it.** Phases 1–3 are low-stakes because
  nothing is public. The moment real users can generate, this must already run.
- **Self-host the classifier.** It sees every image; don't ship them to a third party.

## Current implementation

`sentinel/` is a self-hosted WD-tagger sidecar (wd-eva02-large-tagger-v3,
Danbooru-trained — anime-native apparent-age assessment, sha-pinned, fetched by
`make run-sentinel`). It does pure inference; **policy lives in
`core/internal/compliance`**:

- The `FORBIDDEN_TAG_UC` list drives all three mechanisms: the negative prompt,
  prompt rejection (`CheckPrompt`, below), and output blocking (`CheckOutput`
  / `CheckUpload`).
- Two rules block an output, both env-tunable and code-clamped (never
  disableable): any forbidden tag ≥ `SENTINEL_BLOCK_THRESHOLD` (default 0.35),
  or the **sum** of forbidden-tag scores ≥ `SENTINEL_SUM_THRESHOLD` (default
  0.05). The sum rule is load-bearing: Danbooru purges minor content, so the
  tagger's age tags score faintly even on clear cases — the first real
  child-like output scored `child 0.054 + loli 0.033 + aged_down 0.006 =
  0.093` while clean adult images sit ≤ 0.002. Explicit-leaning images
  (`explicit + questionable ≥ 0.5`) halve both gates: false negatives are
  catastrophic, false positives merely annoying.
- Sentinel unreachable → checker errors → callers block/reject (fail closed,
  already wired in `worker` and `api`).
- `SENTINEL_URL` unset runs the permissive stub with a loud boot warning — a
  dev convenience that must never survive into public exposure.

Accuracy tuning against real generations belongs in `eval/`; the threshold is
a start, not a conclusion.

### Prompt check: parse before check, on every surface

`CheckPrompt` (`core/internal/compliance/prompt.go`) is a pre-filter, not the
gate. It exists to close prompt-level holes cheaply; **`CheckOutput` remains
the gate**, and a pass means "no known pre-filter hole", never "safe". It
runs with or without the sentinel sidecar (`PromptGuard` when `SENTINEL_URL`
is unset). The rules (reports/NovelAI prompt weighting on SDXL.md S7):

1. **Parse before check.** Every user surface is parsed by
   `core/internal/prompt` before anything reads it; assembly emits each tag
   as one flat, escaped `(text:w)` group, so no user text can open or close a
   group across tags or capture system text.
2. **Read what ComfyUI reads.** The check re-parses the EXACT strings
   patched into the CLIPTextEncode nodes — `spec.Prompt` (node 2),
   `spec.Negative` (node 3), every character prompt (nodes 32/36/42; hires
   reuses 2/3) — with `prompt.ComfyParse`, a port of ComfyUI v0.28.0's own
   grammar pinned by fixtures generated from the Python. Wrappers the encoder
   strips (`( )`, `:w`, `\(`, braces) can't launder a tag past it.
3. **Differential.** That parse must equal the Spec's structured tags (same
   boundaries, same non-space text, weights within 5e-5); a mismatch is a
   bug or a bypass and rejects. Missing structure errors (503).
4. **The negative is scanned.** Its first tags must be `FORBIDDEN_TAG_UC` in
   order at exactly weight 1. Any later copy of a safety term is dropped by
   assembly's dedupe (which also neutralises the history round-trip), so a
   watched tag remaining outside the safety block rejects.
5. **WatchSet** = hard-coded floor ∪ `FORBIDDEN_TAG_UC` (`tags.WatchSet`),
   one function shared with the reference-fetch gate and `Live.Ground`
   (which refuses to ground a watched tag). Config can only add. The
   classifier's `include=` list carries the WatchSet so every watched score
   is reported; the output gate's blocking rule is unchanged.
6. **Watched tag at any weight rejects** in the prompt or a character box —
   weight never launders. Match forms: the normalized name; that with
   `( ) { } [ ] \ " '` stripped; that with `_` removed (exact, so
   `lolita_fashion` passes). The word-boundary net stays off until the
   collision review (TODO, S11 #6).
7. **Complement list** (`AGE_COMPLEMENT_TAGS`, env only; unset = no
   complement checks): an adult-signal tag anywhere in the negative outside
   the safety block rejects, and one emitted below weight 1 in a prompt
   rejects. This closes complement steering — pushing *away* from adult
   features — without naming any watched tag. Never env-toggleable off once
   listed.
8. **`embedding:`** anywhere (any case) rejects — the parser rejects it on
   every surface, config included, and the check re-asserts it.
9. **Negative weights** in a prompt are relocated to Undesired Content at
   |w| (then subject to rules 4, 7); in a negative they are a 400, since an
   inverted copy in UC would steer toward the term. Zero weights are dropped.

`FORBIDDEN_TAG_UC` itself must be plain comma-separated tags: any operator
refuses boot, and assembly refuses per request. Every rule fails closed
(parse error 400, check error 503, rejection 422).

## Output metadata

A PNG is more than pixels, and the non-pixel part is a compliance surface.
ComfyUI's `SaveImage` writes the whole patched API graph into a `prompt` tEXt
chunk: the `FORBIDDEN_TAG_UC` block, `DEFAULT_UC`, quality and style text,
checkpoint and LoRA filenames, and the blob IDs of the user's uploads
(`LoadImage` inputs) — which `GET /images/{id}` resolves. Before 2026-11-18
core passed those bytes through untouched.

**Invariant: delivered bytes carry only allow-listed PNG chunks, plus the
chunks core itself writes on the serve path (below) — never anything that
came from ComfyUI or from the stored blob.**

- Allow-list (`internal/pngmeta`): `IHDR PLTE tRNS IDAT IEND`, the colour
  chunks `gAMA cHRM sRGB iCCP cICP mDCV cLLI sBIT`, and `pHYs`. Everything
  else is dropped — `tEXt`, `zTXt`, `iTXt`, `eXIf`, `tIME`, APNG and private
  chunks, and any bytes after `IEND`.
- It is a byte-level chunk splice, never a re-encode: pixels stay bit-identical.
- **Fail closed.** A bad signature, CRC, length or missing `IEND` is an error,
  not a pass-through. In the worker that is `state = failed`; on the serve path
  it is a 500. Never downgrade either to "serve it anyway".

Three layers, outermost last:

1. **ComfyUI runs with `--disable-metadata`** (`comfy/Dockerfile`,
   `comfy/run-native.sh`). Necessary, not sufficient: `SaveSVGNode` ignores the
   flag and custom nodes decide for themselves.
2. **The worker strips every output before `CheckOutput`.** Order matters: the
   classifier sees exactly the bytes that are stored and delivered, so there is
   no unclassified variant of an image. `CheckOutput` still runs on every role
   and one blocked role still blocks the job.
3. **`GET /images/{id}` strips again**, so blobs stored before layer 2 come out
   clean with no migration. Blobs at rest carry no text chunk at all; what
   core adds is added after this strip, per request.

Pinned by `TestDeliveredImagesCarryNoGraph` (`internal/api/pngmeta_e2e_test.go`;
with `TestMalformedOutputFailsJob` and `TestLegacyBlobServedStripped` beside
it). Like `TestForbiddenTagUCIsUnconditional`, it must pass unmodified. Its
allow-list has exactly one extension, made when the serve path began marking
images: a single iTXt chunk whose whole text is one of `outmeta.XMP`'s two
constant packets (`isProvenanceMark`). Any other text chunk still fails it.

### What pulsar writes back

Once everything is stripped, core adds its own metadata on the serve path
(`handleImage` → `pngmeta.Rewrite`, payloads from the pure `internal/outmeta`).
It is built per request from the job row — never copied from a blob, never
stored — so a change to the projection applies to every image already made.

| Chunk (iTXt) | On | Content |
|---|---|---|
| `XML:com.adobe.xmp` | **every** `GET /images/{id}` | IPTC `DigitalSourceType` = `trainedAlgorithmicMedia` (`compositeWithTrainedAlgorithmicMedia` for img2img / inpaint) and `xmp:CreatorTool="pulsar"`. A constant per source type: no prompt, no id, no date. |
| `parameters` | `?meta=1` only | A1111-format infotext (prompt, `Negative prompt:`, one settings line) — what Forge's PNG Info, Civitai and other readers already parse. |
| `pulsar` | `?meta=1` only | `{"v":1,…}` JSON for a lossless round trip into `/create`. Field contract: `outmeta.Payload` ↔ orbit's `PulsarMetaV1`. |

The plain URL is what `<img>` tags, thumbnails and the history grid fetch, so
prompts never ride on it. `?meta=1` is orbit's download; "Download clean"
(`pulsar.settings.downloadMetadata = false`, docs/frontend.md) uses the plain
URL and keeps only the mark. A blob with no job row (an upload, a mask) gets
the mark and nothing else, whatever the query says; a blocked job has no blob
and serves 404 on both URLs.

The XMP mark is the machine-readable "artificially generated" marking of
EU AI Act Art. 50(2). It is unsigned and is one layer; a signed manifest
(C2PA) and a second layer are required before any non-private launch (report
"NovelAI prompt chunks and image inspector", D19, Phase 6). Signing must be the
last byte operation: the serve-path strip would delete a `caBX` chunk.

**Both text payloads are the *user-only projection* of the job** (`outmeta.FromSpec`):
tags whose stored `source` is `user`, the settings the user chose, and display
names. Excluded, each for a reason:

| Never written | Why |
|---|---|
| The safety negative (`FORBIDDEN_TAG_UC`) and the hard-coded floor | It is the recipe for what the deployment suppresses; a file travels off-platform. `FromSpec` also drops any user-sourced tag that matches a safety-block tag. |
| `DEFAULT_UC`, `QUALITY_TAGS`, style prefix / suffix / negative text, auto-injected anchors | Deployment config and server-side additions, not what the user wrote. Core re-adds them on the next generation, so the round trip loses nothing. The style **slug** is written. |
| Upload, reference and mask ids; file paths | `GET /images/{id}` resolves any blob id with no auth: an embedded id is a capability URL to the user's private upload. |
| **The job id / image id, or anything else that resolves to a core URL** | The image id is the job id, and `GET /jobs/{id}` is unauthenticated and returns the full stored params, safety negative included. A PNG that named its job would be a key to it. This holds until core enforces bearer auth (report D1). |
| Model and LoRA filenames | Display names only (`outmeta.DisplayName`: no directory, no extension). |
| `Software: NovelAI`, NovelAI's key schema | It would misattribute provenance: that string is NovelAI's own import gate and a third-party AI-image trigger. |
| A stealth (pixel-LSB) copy | It would survive "Download clean" and chunk-stripping hosts, which a user choosing "clean" would not expect. Orbit reads stealth metadata from other tools; pulsar never writes it. |

A job row stored before structured tags existed has only the emitted strings,
which mix user text with the safety and quality blocks; `FromSpec` writes an
empty prompt for it rather than guess.

Pinned by `TestDenyList` and `TestFromSpecIsUserOnly` (`internal/outmeta`) and,
end to end, by `TestServedImagesCarryOnlyTheMark`,
`TestDownloadCarriesUserMetadataOnly`, `TestPoseRolesAreMarkedAndDownloadable`,
`TestBlockedJobServesNoImageWithMeta` and `TestUploadBlobGetsNoPromptMetadata`
(`internal/api/outmeta_e2e_test.go`). The deny-list tests read the safety terms
from config and `tags.WatchFloor()`; they are never spelled out in a test.

### Images coming in

The inspector (`/inspect`, docs/frontend.md#image-inspector) reads other
tools' metadata **in the browser**; core has no endpoint that accepts an image
just to read it — that would be a second ingest path. Nothing it reads is
trusted:

- An imported prompt is ordinary editor text. `POST /prompt/parse` with
  `"clean": true` only *removes* syntax pulsar has no place for (`<lora:…>`,
  `embedding:…`, `|`, `||…||`, control characters) and then runs the normal
  parser; `/generate` has no clean mode and runs `CheckPrompt` on the result
  exactly as on typed text (`TestCleanIsNoComplianceBypass`).
- An inspected image used as a base or reference goes through `POST /uploads`
  — `ingest.Normalize` (now PNG, JPEG or still WebP; re-encoded, so its
  metadata and any pixel-LSB payload are gone) then `CheckUpload`, fail closed
  (`TestWebPUploadIsOneIngestPath`).

What this does not fix: `GET /jobs/{id}` still returns the stored `Spec` with
the full emitted negative, and no route checks the bearer token. That is the
auth project, and it gates any shared deployment.

`cmd/purgeleaked` is the one-off that deletes the upload blobs whose IDs
already left in old PNGs (dry-run by default; `-apply` deletes). It keeps job
outputs and the inputs of queued or running jobs.

## Known risks

1. **Classifier accuracy on anime is the highest-risk unknown in the project**, and
   it's on the critical path to launch. Anime style breaks assumptions baked into
   classifiers trained on photographs — apparent age is stylistic, not
   morphological. Budget iteration time; do not assume an off-the-shelf model works.
2. **False negatives are catastrophic; false positives are merely annoying.** Tune
   accordingly. A frustrated user is a survivable outcome.
3. **`/pose` nudity is an open product decision.** Anatomical figure references are a
   legitimate artist need, but permitting them puts `/pose` inside this gate rather
   than beside it — which determines whether `/pose` can ship before the gate closes.
   Decide deliberately, not by default.

## Phase

Phase 4. Development can overlap phase 3, but it **must fully close before any
public access**. It is the longest-lead risk in the roadmap, which is a second
reason not to leave it for last.
