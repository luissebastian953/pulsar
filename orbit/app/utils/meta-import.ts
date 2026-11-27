// Turning an inspected image's metadata into something /create can take
// (docs/frontend.md#image-inspector, decisions D11 and D22).
//
// Everything here is UNTRUSTED file content on its way into the normal
// editor: the result is plain prompt text that the composer shows, that
// POST /prompt/parse validates live, and that passes the usual compliance
// check at /generate — an import bypasses nothing.
//
// Orbit owns no grammar: prompts are split and cleaned only by core
// (`parse` below is POST /prompt/parse with `clean: true`), and this module
// works on the tags it returns — whole tags, never substrings.
import type { ChunkPayload, PromptParseResult, PromptSurface } from '~/types/api'
import { chunkToken } from '~/utils/chunks'
import type { GenParams } from '~/utils/imgmeta/types'

/** The toggles of the import dialog. */
export interface ImportOptions {
  prompt: boolean
  negative: boolean
  characters: boolean
  /** Size (snapped to a bucket) and steps — nothing else. */
  settings: boolean
  seed: boolean
}

/** D22: prompt, Undesired Content and characters on; settings and seed off. */
export const DEFAULT_IMPORT_OPTIONS: Readonly<ImportOptions> = Object.freeze({
  prompt: true, negative: true, characters: true, settings: false, seed: false,
})

export type Aspect = 'portrait' | 'landscape' | 'square'

/**
 * What an import may change on /create. There is deliberately no CFG,
 * sampler, scheduler or rescale here: settings don't transfer between model
 * lineages, so an import can never carry them (CLAUDE.md, D22).
 */
export interface MetaImport {
  /** Prompt as chips (tags mode). */
  tagList?: string[]
  /** Undesired Content. */
  und?: string
  /** One prompt per character. */
  chars?: string[]
  aspect?: Aspect
  steps?: number
  seedText?: string
  /** Uploaded through the normal path (useImageUpload) as the img2img base. */
  baseImage?: File
  /** Uploaded through the normal path as the character reference. */
  reference?: File
}

export interface ImportResult {
  patch: MetaImport
  /** Plain sentences for the user: what was dropped, merged or re-tokenized. */
  notices: string[]
}

export type ParseFn = (
  text: string,
  surface: PromptSurface,
  opts?: { clean?: boolean, chunks?: ChunkPayload[] },
) => Promise<PromptParseResult>

export interface ImportDeps {
  /** POST /prompt/parse. A rejection means core is unreachable. */
  parse: ParseFn
  /** The local chunk store, for re-tokenizing (D11). */
  chunks?: { name: string, content: string }[]
  /** The chunks a set of texts references, transitively (useChunks().referenced). */
  referenced?: (texts: string[]) => ChunkPayload[]
}

// the composer's three aspect buckets
const ASPECTS: [Aspect, number, number][] = [['portrait', 832, 1216], ['landscape', 1216, 832], ['square', 1024, 1024]]
const MAX_STEPS = 50
/** Chunks of fewer tags than this are never re-tokenized (D11). */
export const MIN_RETOKENIZE_TAGS = 2
// requests stay under core's per-request chunk limit
const CHUNKS_PER_REQUEST = 32

/** The bucket whose aspect ratio is nearest (log distance, as core's ingest does). */
export function snapAspect(width: number, height: number): Aspect | null {
  if (!(width > 0) || !(height > 0)) return null

  const ar = Math.log(width / height)
  let best: Aspect = 'portrait'
  let bestDiff = Infinity

  for (const [name, w, h] of ASPECTS) {
    const d = Math.abs(ar - Math.log(w / h))

    if (d < bestDiff) {
      best = name
      bestDiff = d
    }
  }

  return best
}

/**
 * NovelAI text that its client adds automatically and records with
 * `tag_hint_qt` / `tag_hint_uc_preset` — stripped on import so pulsar's own
 * quality tags and default negative are not stacked on NovelAI's. Tag names
 * as core normalises them. Only values seen in real metadata are listed
 * (NovelAI Diffusion V5 Curated, from the frontend bundle's example image);
 * a preset that is not here is left in the text, which is always safe.
 */
export const NAI_QUALITY_SUFFIXES: string[][] = [
  ['very_aesthetic', 'masterpiece', 'no_text'],
]

export const NAI_UC_PRESETS: string[][] = [
  ['lowres', 'artistic_error', 'film_grain', 'scan_artifacts', 'worst_quality', 'bad_quality', 'jpeg_artifacts',
    'very_displeasing', 'chromatic_aberration', 'dithering', 'halftone', 'screentone', 'multiple_views', 'logo',
    'too_many_watermarks', 'negative_space', 'blank_page'],
]

/** One imported tag: its NovelAI-dialect text and its normalised name. */
interface Piece {
  nai: string
  name: string
  /** unweighted, so it can match a preset tag */
  plain: boolean
}

interface Surface {
  /** null = core could not parse it (or is unreachable): `raw` is used as written */
  pieces: Piece[] | null
  relocated: Piece[]
  raw: string
}

const isPlain = (t: { nai: string, text: string }) => t.nai === t.text

/** The text sources of one image, by generator. */
export function importSources(p: GenParams): {
  prompt: string
  negative: string
  characters: { prompt: string, negative: string }[]
  /** NovelAI said it appended its quality tags / applied a UC preset. */
  naiQuality: boolean
  naiUcPreset: boolean
} {
  // pulsar's own chunk carries the text as typed; prefer it over the
  // calibrated SD form
  if (p.generator === 'pulsar' && p.pulsar?.nai) {
    const n = p.pulsar.nai

    return {
      prompt: n.prompt, negative: n.negative,
      characters: n.characters.map(prompt => ({ prompt, negative: '' })),
      naiQuality: false, naiUcPreset: false,
    }
  }

  const raw = (typeof p.raw === 'object' && p.raw !== null ? p.raw : {}) as Record<string, unknown>
  const hint = (key: string) => Object.hasOwn(raw, key) ? raw[key] : undefined
  const actual = p.generator === 'novelai' ? p.novelai?.actual : undefined
  const qt = hint('tag_hint_qt')
  const uc = hint('tag_hint_uc_preset')

  return {
    // NovelAI `actual_prompts`: the resolved values of randomised sections
    prompt: actual?.prompt ?? p.prompt ?? '',
    negative: actual?.negative ?? p.negative ?? '',
    characters: p.characters.map((c, i) => ({ prompt: actual?.characters[i] || c.prompt, negative: c.negative ?? '' })),
    naiQuality: p.generator === 'novelai' && (qt === true || (typeof qt === 'number' && qt > 0)),
    naiUcPreset: p.generator === 'novelai' && typeof uc === 'number',
  }
}

function endsWith(pieces: Piece[], names: string[]): boolean {
  if (pieces.length < names.length) return false

  const off = pieces.length - names.length

  return names.every((n, i) => pieces[off + i]!.plain && pieces[off + i]!.name === n)
}

function startsWith(pieces: Piece[], names: string[]): boolean {
  return pieces.length >= names.length && names.every((n, i) => pieces[i]!.plain && pieces[i]!.name === n)
}

/**
 * Replace runs of tags that are exactly a local chunk's expansion with its
 * `!macro:Name!` token (D11): whole-tag sequences only, chunks of at least
 * MIN_RETOKENIZE_TAGS tags, longest chunk first. Returns the names used.
 */
export function retokenize(pieces: Piece[], expansions: { name: string, nais: string[] }[]): { pieces: Piece[], used: string[] } {
  const used: string[] = []
  let out = pieces
  const order = expansions
    .filter(e => e.nais.length >= MIN_RETOKENIZE_TAGS)
    .sort((a, b) => b.nais.length - a.nais.length || a.name.localeCompare(b.name))

  for (const e of order) {
    const next: Piece[] = []
    let hit = false

    for (let i = 0; i < out.length;) {
      const match = i + e.nais.length <= out.length
        // a token already placed is never part of a later match
        && e.nais.every((n, k) => out[i + k]!.name !== '' && out[i + k]!.nai === n)

      if (match) {
        next.push({ nai: chunkToken(e.name), name: '', plain: false })
        i += e.nais.length
        hit = true
      }
      else {
        next.push(out[i]!)
        i++
      }
    }

    if (hit) {
      out = next
      used.push(e.name)
    }
  }

  return { pieces: out, used }
}

const list = (names: string[]) => names.map(n => `“${n}”`).join(', ')

/**
 * Build the patch for /create from an image's parsed metadata. Never throws
 * for bad content or an unreachable core: what cannot be cleaned is imported
 * as written, so it lands in the editor where core's live validation marks it.
 */
export async function buildMetaImport(params: GenParams, options: ImportOptions, deps: ImportDeps): Promise<ImportResult> {
  const notices: string[] = []
  const note = (s: string) => {
    if (!notices.includes(s)) notices.push(s)
  }
  const patch: MetaImport = {}
  const src = importSources(params)
  let unparsed = false
  let offline = false

  async function surface(text: string, kind: PromptSurface, label: string): Promise<Surface> {
    if (!text.trim()) return { pieces: [], relocated: [], raw: '' }

    let res: PromptParseResult

    try {
      res = await deps.parse(text, kind, { clean: true })
    }
    catch {
      offline = true

      return { pieces: null, relocated: [], raw: text }
    }

    for (const n of res.notices) {
      if (n.code.startsWith('cleaned_')) note(`${label}: ${n.message}.`)
    }

    if (res.errors.length) {
      unparsed = true

      return { pieces: null, relocated: [], raw: res.cleaned ?? text }
    }

    const piece = (t: { nai: string, name: string, text: string }): Piece => ({ nai: t.nai, name: t.name, plain: isPlain(t) })
    let pieces = res.tags.map(piece)

    // A1111's BREAK is a conditioning-chunk separator, not a tag
    if (params.generator === 'a1111' || params.generator === 'comfyui') {
      const kept = pieces.filter(p => p.nai !== 'BREAK')

      if (kept.length !== pieces.length) note(`${label}: BREAK separators were dropped — pulsar has no equivalent.`)

      pieces = kept
    }

    return { pieces, relocated: res.relocated.map(piece), raw: res.cleaned ?? text }
  }

  const wantNeg = options.negative
  const prompt = options.prompt ? await surface(src.prompt, 'prompt', 'Prompt') : null
  const negative = wantNeg ? await surface(src.negative, 'negative', 'Undesired Content') : null
  const chars = options.characters
    ? await Promise.all(src.characters.map((c, i) => surface(c.prompt, 'character', `Character ${i + 1}`)))
    : []
  const charNegs = options.characters && wantNeg
    ? await Promise.all(src.characters.map((c, i) => surface(c.negative, 'negative', `Character ${i + 1} Undesired Content`)))
    : []

  // NovelAI's automatic text (D22)
  if (prompt?.pieces && src.naiQuality) {
    const hit = NAI_QUALITY_SUFFIXES.find(s => endsWith(prompt.pieces!, s))

    if (hit) {
      prompt.pieces = prompt.pieces.slice(0, prompt.pieces.length - hit.length)
      note('NovelAI\'s automatic quality tags were left out — pulsar adds its own.')
    }
  }

  if (negative?.pieces && src.naiUcPreset) {
    const hit = NAI_UC_PRESETS.find(s => startsWith(negative.pieces!, s))

    if (hit) {
      negative.pieces = negative.pieces.slice(hit.length)
      note('NovelAI\'s Undesired Content preset was left out — pulsar merges its own default negative.')
    }
  }

  // tags weighted negative in a prompt belong in Undesired Content here
  const relocated = [...(prompt?.relocated ?? []), ...chars.flatMap(c => c.relocated)]
  const extraNeg: Piece[] = []

  if (relocated.length) {
    if (wantNeg) {
      extraNeg.push(...relocated)
      note(`Negative-weight tags moved to Undesired Content: ${relocated.map(p => p.nai).join(', ')}.`)
    }
    else {
      note(`Negative-weight tags were left out (Undesired Content is not being imported): ${relocated.map(p => p.nai).join(', ')}.`)
    }
  }

  // per-character Undesired Content → the main one (pulsar has no per-character negative)
  const merged = charNegs.flatMap(c => c.pieces ?? [])
  const mergedRaw = charNegs.filter(c => c.pieces === null).map(c => c.raw)

  if (merged.length || mergedRaw.length) {
    extraNeg.push(...merged)
    note('Character Undesired Content was merged into the main Undesired Content — pulsar has no per-character negative.')
  }
  else if (options.characters && !wantNeg && src.characters.some(c => c.negative.trim())) {
    note('Character Undesired Content was left out (Undesired Content is not being imported).')
  }

  // D11: put local chunks back where the imported tags are exactly their expansion
  const expansions = await chunkExpansions(deps)
  const usedChunks = new Set<string>()
  const tokenized = (pieces: Piece[]): Piece[] => {
    if (!expansions.length) return pieces

    const r = retokenize(pieces, expansions)

    for (const n of r.used) usedChunks.add(n)

    return r.pieces
  }
  const unique = (pieces: Piece[]): string[] => [...new Set(pieces.map(p => p.nai).filter(Boolean))]

  if (prompt) {
    patch.tagList = prompt.pieces ? unique(tokenized(prompt.pieces)) : (prompt.raw.trim() ? [prompt.raw.trim()] : [])
  }

  if (negative) {
    const base = negative.pieces ? unique(tokenized([...negative.pieces, ...extraNeg])).join(', ') : negative.raw.trim()
    const tail = negative.pieces ? [] : unique(extraNeg)

    patch.und = [base, ...tail, ...mergedRaw.map(r => r.trim())].filter(Boolean).join(', ')
  }

  if (options.characters) {
    patch.chars = chars
      .map(c => (c.pieces ? unique(tokenized(c.pieces)).join(', ') : c.raw.trim()))
      .filter(Boolean)

    if (patch.chars.length > 3) note(`${patch.chars.length} characters imported: pulsar positions 2–3, and merges any other number into the prompt.`)
  }

  if (usedChunks.size) note(`Matching tags were replaced by your prompt chunk${usedChunks.size > 1 ? 's' : ''} ${list([...usedChunks])}.`)

  if (unparsed) note('Some text has syntax pulsar can\'t read. It was imported as written — fix the marked spot before generating.')

  if (offline) note('Core is unreachable, so the text was imported as written, without cleaning.')

  if (options.settings) {
    const aspect = snapAspect(params.width ?? 0, params.height ?? 0)

    if (aspect) {
      patch.aspect = aspect
      const [, w, h] = ASPECTS.find(a => a[0] === aspect)!

      if (params.width !== w || params.height !== h) note(`Size ${params.width} × ${params.height} snapped to ${w} × ${h}.`)
    }

    if (typeof params.steps === 'number' && Number.isFinite(params.steps) && params.steps >= 1) {
      patch.steps = Math.min(MAX_STEPS, Math.round(params.steps))

      if (patch.steps !== params.steps) note(`Steps ${params.steps} limited to ${patch.steps}.`)
    }

    note('Guidance, sampler, schedule and rescale are never imported: they don\'t transfer between models.')
  }

  if (options.seed) {
    if (typeof params.seed === 'number' && Number.isSafeInteger(params.seed) && params.seed >= 0) patch.seedText = String(params.seed)
    else if (params.seed !== undefined) note('The seed is too large to import exactly and was left out.')
  }

  return { patch, notices }
}

/** What each local chunk expands to, per core. Chunks core rejects are skipped. */
async function chunkExpansions(deps: ImportDeps): Promise<{ name: string, nais: string[] }[]> {
  const chunks = deps.chunks ?? []

  if (!chunks.length || !deps.referenced) return []

  const out: { name: string, nais: string[] }[] = []

  async function resolve(names: string[]): Promise<boolean> {
    const text = names.map(chunkToken).join(', ')
    const payload = deps.referenced!([text])

    if (payload.length > 64) return false

    let res: PromptParseResult

    try {
      res = await deps.parse(text, 'prompt', { chunks: payload })
    }
    catch {
      return true // unreachable: nothing to re-tokenize, and no point retrying one by one
    }

    // a chunk with relocated (negative-weight) tags is not a plain tag run
    if (res.errors.length || res.relocated.length) return false

    const by = new Map<string, string[]>()

    for (const t of res.tags) {
      if (!t.chunk) return false

      by.set(t.chunk.name, [...(by.get(t.chunk.name) ?? []), t.nai])
    }

    for (const [name, nais] of by) out.push({ name, nais })

    return true
  }

  for (let i = 0; i < chunks.length; i += CHUNKS_PER_REQUEST) {
    const names = chunks.slice(i, i + CHUNKS_PER_REQUEST).map(c => c.name)

    // one bad chunk fails its batch: fall back to asking one at a time
    if (!(await resolve(names))) {
      for (const n of names) await resolve([n])
    }
  }

  return out
}

/** The composer state an import writes — refs, so the composer passes its own. */
export interface ImportTarget {
  mode: { value: 'plain' | 'tags' }
  tagList: { value: string[] }
  und: { value: string }
  chars: { value: { text: string, enabled: boolean, refId: string }[] }
  aspect: { value: Aspect }
  steps: { value: number }
  seedText: { value: string }
}

/**
 * Write a patch into the composer. Only the fields the patch carries are
 * touched; everything else — and always guidance, sampler, schedule,
 * rescale, lineage, model, LoRAs, style — stays as the user had it.
 */
export function applyMetaImport(target: ImportTarget, patch: MetaImport) {
  if (patch.tagList !== undefined) {
    target.mode.value = 'tags'
    target.tagList.value = [...patch.tagList]
  }

  if (patch.und !== undefined) target.und.value = patch.und

  if (patch.chars !== undefined) target.chars.value = patch.chars.map(text => ({ text, enabled: true, refId: '' }))

  if (patch.aspect !== undefined) target.aspect.value = patch.aspect

  if (patch.steps !== undefined) target.steps.value = patch.steps

  if (patch.seedText !== undefined) target.seedText.value = patch.seedText
}
