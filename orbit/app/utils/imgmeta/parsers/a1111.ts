// A1111 / Forge / reForge infotext: prompt lines, an optional
// `Negative prompt:` block, then ONE settings line of `Key: value` pairs.
// This is a port of `parse_generation_parameters` (modules/infotext_utils.py),
// not a new grammar — divergent readers turn quoted prose into junk.
import { isRecord, num, parseJson } from '../text'
import type { GenParams, Resource } from '../types'

// A1111's re_param_code, verbatim:
//   \s*(\w[\w \-/]+):\s*("(?:\\.|[^\\"])+"|[^,]*)(?:,|$)
// with Python's Unicode-aware \w spelled out, since JS \w is ASCII-only.
const RE_PARAM = /\s*([\p{L}\p{N}_][\p{L}\p{N}_ \-/]+):\s*("(?:\\.|[^\\"])+"|[^,]*)(?:,|$)/gu
const RE_IMAGESIZE = /^(\d+)x(\d+)$/
const NEGATIVE = 'Negative prompt:'

export interface InfotextParts {
  prompt: string
  negative: string
  /** Settings in file order, values unquoted. Empty when the last line was not a settings line. */
  settings: [string, string][]
}

/** `unquote`: a value wrapped in double quotes is a JSON string. */
function unquote(text: string): string {
  if (text.length < 2 || !text.startsWith('"') || !text.endsWith('"')) return text

  try {
    const v: unknown = JSON.parse(text)

    return typeof v === 'string' ? v : text
  }
  catch {
    return text
  }
}

export function splitInfotext(text: string): InfotextParts {
  const lines = text.replace(/\r\n?/g, '\n').trim().split('\n')
  const lastLine = lines.pop() ?? ''
  let pairs = [...lastLine.matchAll(RE_PARAM)]

  // The last line is settings only when the regex finds three or more pairs;
  // otherwise it is prompt text.
  if (pairs.length < 3) {
    lines.push(lastLine)
    pairs = []
  }

  let prompt = ''
  let negative = ''
  let inNegative = false

  for (let line of lines) {
    line = line.trim()

    if (line.startsWith(NEGATIVE)) {
      inNegative = true
      line = line.slice(NEGATIVE.length).trim()
    }

    if (inNegative) negative += (negative === '' ? '' : '\n') + line
    else prompt += (prompt === '' ? '' : '\n') + line
  }

  return { prompt, negative, settings: pairs.map(m => [m[1]!, unquote((m[2] ?? '').trim())]) }
}

/** `Lora hashes` / `TI hashes`: `name: hash, name: hash` (A1111's own re-split). */
function subSplitHashes(kind: string, value: string): Resource[] {
  const out: Resource[] = []

  for (const part of value.split(',').slice(0, 256)) {
    const at = part.indexOf(':')
    const name = (at < 0 ? part : part.slice(0, at)).trim()

    if (!name) continue

    out.push(at < 0 ? { kind, name } : { kind, name, hash: part.slice(at + 1).trim() })
  }

  return out
}

/**
 * Parse an infotext. With `requireSettings`, text whose last line is not a
 * settings line is refused — used for EXIF comments, where arbitrary camera
 * text would otherwise be claimed as a prompt.
 */
export function parseA1111(text: string, opts: { requireSettings?: boolean } = {}): GenParams | null {
  const { prompt, negative, settings } = splitInfotext(text)

  if (settings.length === 0 && (opts.requireSettings || prompt === '')) return null

  const params: GenParams = { generator: 'a1111', prompt, characters: [], extra: {} }

  const resources: Resource[] = []

  if (negative) params.negative = negative

  for (const [key, value] of settings) {
    const size = key === 'Size' ? value.match(RE_IMAGESIZE) : null

    if (size) {
      params.width = Number(size[1])
      params.height = Number(size[2])
    }
    else if (key === 'Steps') {
      params.steps = num(value)
    }
    else if (key === 'CFG scale') {
      params.cfg = num(value)
    }
    else if (key === 'Seed') {
      params.seed = num(value)
    }
    else if (key === 'Sampler') {
      params.sampler = value
    }
    else if (key === 'Schedule type') {
      params.scheduler = value
    }
    else if (key === 'Model') {
      params.model = value
    }
    else if (key === 'Model hash') {
      params.modelHash = value
    }
    else if (key === 'Lora hashes' || key === 'TI hashes') {
      resources.push(...subSplitHashes(key === 'Lora hashes' ? 'lora' : 'embedding', value))
    }
    else if (key === 'Hashes' && isRecord(parseJson(value))) {
      // Civitai-style resource hashes: a JSON object behind the quotes, keyed
      // `model`, `lora:name`, `embed:name`, …
      for (const [k, v] of Object.entries(parseJson(value) as Record<string, unknown>).slice(0, 256)) {
        const at = k.indexOf(':')

        resources.push({ kind: at < 0 ? k : k.slice(0, at), name: at < 0 ? k : k.slice(at + 1), hash: String(v) })
      }
    }
    else {
      params.extra[key] = value
    }
  }

  if (resources.length) params.resources = resources

  return params
}
