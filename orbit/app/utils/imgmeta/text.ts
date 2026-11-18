// Charset and JSON helpers shared by the walkers and parsers.
import { MAX_ENTRY_BYTES, MAX_JSON_DEPTH } from './limits'

/** ISO-8859-1, byte for byte. (`TextDecoder('latin1')` is windows-1252.) */
export function latin1(bytes: Uint8Array): string {
  let out = ''

  for (let i = 0; i < bytes.length; i += 0x8000) {
    out += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }

  return out
}

/** Lenient UTF-8: invalid sequences become U+FFFD instead of throwing. */
export function utf8(bytes: Uint8Array): string {
  return new TextDecoder('utf-8').decode(bytes)
}

export function utf16(bytes: Uint8Array, littleEndian: boolean): string {
  return new TextDecoder(littleEndian ? 'utf-16le' : 'utf-16be').decode(bytes)
}

export function stripTrailingNuls(s: string): string {
  let n = s.length

  while (n > 0 && s.charCodeAt(n - 1) === 0) n--

  return s.slice(0, n)
}

export function indexOfByte(bytes: Uint8Array, value: number, from = 0): number {
  return bytes.indexOf(value, from)
}

/**
 * Make C0/C1 control characters visible (`\x1b`, `\u0085`) so metadata cannot
 * smuggle terminal or bidi tricks into a view. Tab and newline are kept;
 * carriage returns are kept only as part of the text flow (`\r`). For display
 * only — parsers work on the unescaped text.
 */
export function escapeControlChars(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/[\0-\x08\v\f\x0E-\x1F\x7F-\x9F‪-‮⁦-⁩]/g, (c) => {
    const code = c.charCodeAt(0)

    return code < 0x100 ? `\\x${code.toString(16).padStart(2, '0')}` : `\\u${code.toString(16).padStart(4, '0')}`
  })
}

export function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

export function str(v: unknown): string | undefined {
  return typeof v === 'string' ? v : undefined
}

/** A finite number, or a string that is plainly one (SwarmUI stores numbers as strings). */
export function num(v: unknown): number | undefined {
  if (typeof v === 'number') return Number.isFinite(v) ? v : undefined

  if (typeof v === 'string' && /^\s*-?\d+(?:\.\d+)?\s*$/.test(v)) return Number(v)

  return undefined
}

/** A short display string for a settings value; objects are JSON, capped. */
export function display(v: unknown): string | undefined {
  if (v === null || v === undefined) return undefined

  if (typeof v === 'string') return v

  if (typeof v === 'number' || typeof v === 'boolean') return String(v)

  try {
    const s = JSON.stringify(v)

    return s.length > 2000 ? `${s.slice(0, 2000)}…` : s
  }
  catch {
    return undefined
  }
}

/**
 * JSON.parse for untrusted metadata. Returns undefined instead of throwing,
 * refuses input over the entry cap or nested deeper than MAX_JSON_DEPTH, and
 * (with `sanitize`) rewrites the bare `NaN` / `Infinity` / `-Infinity` tokens
 * Python's json.dumps emits to `null` — outside string literals only.
 */
export function parseJson(text: string, opts: { sanitize?: boolean } = {}): unknown {
  if (text.length > MAX_ENTRY_BYTES) return undefined

  let depth = 0
  let inString = false
  let out: string[] | null = null
  let copied = 0

  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i)

    if (inString) {
      if (c === 0x5c) i++
      else if (c === 0x22) inString = false

      continue
    }

    if (c === 0x22) {
      inString = true
    }
    else if (c === 0x7b || c === 0x5b) {
      if (++depth > MAX_JSON_DEPTH) return undefined
    }
    else if (c === 0x7d || c === 0x5d) {
      depth--
    }
    else if (opts.sanitize && (c === 0x4e || c === 0x49 || c === 0x2d)) {
      const token = text.startsWith('NaN', i)
        ? 'NaN'
        : text.startsWith('Infinity', i)
          ? 'Infinity'
          : text.startsWith('-Infinity', i) ? '-Infinity' : ''

      if (token) {
        out ??= []
        out.push(text.slice(copied, i), 'null')
        i += token.length - 1
        copied = i + 1
      }
    }
  }

  if (out) {
    out.push(text.slice(copied))
    text = out.join('')
  }

  try {
    return JSON.parse(text)
  }
  catch {
    return undefined
  }
}
