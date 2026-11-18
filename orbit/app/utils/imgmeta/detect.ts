// Format detection: an ordered cascade over the raw entries. ORDER IS
// BEHAVIOUR — formats overlap (ComfyUI extensions add A1111 `parameters`,
// SwarmUI reuses the `parameters` key, NovelAI's `Comment` is a generic
// keyword) and the first match wins:
//
//   pulsar → NovelAI → SwarmUI → A1111 → InvokeAI → ComfyUI
//
// A stealth payload is dispatched by its content instead (A1111 text,
// NovelAI JSON or SwarmUI JSON), and only consulted when the container
// metadata matched nothing. Because any interpretation here is a guess, the
// raw entries are always returned beside it.
import { parseA1111 } from './parsers/a1111'
import { parseComfyUI } from './parsers/comfyui'
import { parseDream, parseInvokeMetadata, parseSdMetadata } from './parsers/invokeai'
import { parseNovelAI } from './parsers/novelai'
import type { NaiDict } from './parsers/novelai'
import { parsePulsar, PULSAR_KEY } from './parsers/pulsar'
import { looksLikeSwarm, parseSwarmUI } from './parsers/swarmui'
import { isRecord, parseJson } from './text'
import type { GenParams, Generator, MetaWarning, RawEntry } from './types'

export interface Detection {
  generator: Generator
  via: 'container' | 'stealth'
  params: GenParams
  /** NovelAI only: the Comment text exactly as stored, for signature verification. */
  naiComment?: string
}

const NAI_KEYS = ['Title', 'Description', 'Software', 'Source', 'Generation time', 'Comment']

function isPng(e: RawEntry): boolean {
  return e.source.startsWith('png:')
}

function looksLikeJson(text: string): boolean {
  return text.trimStart().startsWith('{')
}

function novelai(dict: NaiDict, via: Detection['via']): Detection | null {
  const comment = dict.Comment
  const signed = typeof comment === 'string' ? comment.includes('"signed_hash"') : isRecord(comment) && 'signed_hash' in comment
  const branded = typeof dict.Software === 'string' && dict.Software.includes('NovelAI')

  // `Comment` alone is too generic a keyword to claim an image for NovelAI.
  if (!signed && !branded) return null

  const params = parseNovelAI(dict)

  if (!params) return null

  return typeof comment === 'string'
    ? { generator: 'novelai', via, params, naiComment: comment }
    : { generator: 'novelai', via, params }
}

export function detectContainer(entries: RawEntry[], warnings: MetaWarning[]): Detection | null {
  const png = (key: string) => entries.find(e => isPng(e) && e.key === key)?.text
  const exif = (key: string) => entries.find(e => e.source === 'exif' && e.key === key)?.text
  const found = (generator: Generator, params: GenParams | null): Detection | null =>
    params ? { generator, via: 'container', params } : null

  // 1. pulsar
  const own = png(PULSAR_KEY)

  if (own !== undefined) {
    const parsed = parsePulsar(own)

    if (parsed.ok) return { generator: 'pulsar', via: 'container', params: parsed.params }

    warnings.push(parsed.reason === 'unknown_version' ? { code: 'pulsar_unknown_version' } : { code: 'bad_json', detail: PULSAR_KEY })
  }

  // 2. NovelAI — PNG text keys, or the whole dictionary as JSON in the EXIF
  // UserComment (WebP). A bare params object in UserComment is accepted too.
  if (png('Comment') !== undefined) {
    const dict: NaiDict = {}

    for (const key of NAI_KEYS) {
      const v = png(key)

      if (v !== undefined) dict[key] = v
    }

    const hit = novelai(dict, 'container')

    if (hit) return hit
  }

  const userComment = exif('UserComment')

  if (userComment !== undefined && looksLikeJson(userComment)) {
    const obj = parseJson(userComment)

    if (isRecord(obj) && !('sui_image_params' in obj)) {
      // NovelAI's EXIF mapping: Software → Source, ImageDescription →
      // Description, DocumentName → Title.
      const dict: NaiDict = 'Comment' in obj ? { ...obj } : { Comment: userComment }

      dict.Source ??= exif('Software')
      dict.Description ??= exif('ImageDescription')
      dict.Title ??= exif('DocumentName')

      for (const [k, v] of Object.entries(dict)) {
        if (typeof v === 'string' && v.includes('\0')) dict[k] = v.replaceAll('\0', '')
      }

      const hit = novelai(dict, 'container')

      if (hit) return hit
    }
  }

  // 3. SwarmUI
  for (const text of [png('parameters'), userComment, exif('Model')]) {
    const hit = text !== undefined && looksLikeSwarm(text) ? found('swarmui', parseSwarmUI(text)) : null

    if (hit) return hit
  }

  // 4. A1111 / Forge / reForge. JSON under `parameters` is some other tool.
  const parameters = png('parameters')

  if (parameters !== undefined && !(looksLikeJson(parameters) && isRecord(parseJson(parameters)))) {
    const hit = found('a1111', parseA1111(parameters))

    if (hit) return hit
  }

  if (userComment !== undefined && !looksLikeJson(userComment)) {
    const hit = found('a1111', parseA1111(userComment, { requireSettings: true }))

    if (hit) return hit
  }

  // 5. InvokeAI, newest form first
  for (const [key, parse] of [['invokeai_metadata', parseInvokeMetadata], ['sd-metadata', parseSdMetadata], ['Dream', parseDream]] as const) {
    const text = png(key)
    const hit = text === undefined ? null : found('invokeai', parse(text))

    if (hit) return hit
  }

  // 6. ComfyUI — PNG `prompt`/`workflow` (tEXt, or `comf` after IDAT in
  // APNG), or EXIF tags holding `prompt:{json}` / `workflow:{json}` (WebP).
  const tagged = (prefix: string) => entries.find(e => e.source === 'exif' && e.text.startsWith(prefix))?.text.slice(prefix.length)
  const graph = png('prompt') ?? tagged('prompt:')
  const workflow = png('workflow') ?? tagged('workflow:')

  if (graph !== undefined || workflow !== undefined) return found('comfyui', parseComfyUI(graph, workflow, warnings))

  return null
}

/** Dispatch a stealth payload by content: SwarmUI JSON, NovelAI JSON, else A1111 text. */
export function detectStealth(text: string): Detection | null {
  if (looksLikeJson(text)) {
    if (looksLikeSwarm(text)) {
      const params = parseSwarmUI(text)

      if (params) return { generator: 'swarmui', via: 'stealth', params }
    }

    const obj = parseJson(text)

    if (isRecord(obj)) return 'Comment' in obj ? novelai(obj, 'stealth') : null
  }

  const params = parseA1111(text)

  return params ? { generator: 'a1111', via: 'stealth', params } : null
}
