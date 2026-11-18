// Mapping core's prompt issues onto the composer's inputs. Core owns the
// grammar (POST /prompt/parse); these helpers only locate its answers.
// Offsets are UTF-16 code units — exactly JS string indices.
import type { PromptErrorBody, PromptIssue, PromptTag } from '~/types/api'

export type Range = { start: number, end: number }

/** Where each chip sits in `chips.join(', ')` — the string core parsed. */
export function chipSpans(chips: string[]): Range[] {
  const out: Range[] = []
  let off = 0
  for (const c of chips) {
    out.push({ start: off, end: off + c.length })
    off += c.length + 2
  }

  return out
}

function hits(r: Range, i: Range): boolean {
  // a zero-width issue marks the character at its start
  const end = i.end > i.start ? i.end : i.start + 1

  return i.start < r.end && end > r.start
}

/** Indices of the chips an issue overlaps. */
export function chipsWithIssues(chips: string[], issues: PromptIssue[]): Set<number> {
  const spans = chipSpans(chips)
  const out = new Set<number>()
  for (const i of issues) {
    spans.forEach((s, k) => {
      if (hits(s, i)) out.add(k)
    })
  }

  return out
}

export type Segment = { text: string, mark: boolean }

/** Split text into marked/unmarked stretches for a highlight overlay. */
export function segmentText(text: string, ranges: Range[]): Segment[] {
  const marks = ranges
    .map(r => ({ start: Math.max(0, r.start), end: Math.min(text.length, r.end > r.start ? r.end : r.start + 1) }))
    .filter(r => r.end > r.start)
    .sort((a, b) => a.start - b.start)
  const out: Segment[] = []
  let at = 0
  for (const r of marks) {
    const start = Math.max(r.start, at)
    if (r.end <= start) continue

    if (start > at) out.push({ text: text.slice(at, start), mark: false })

    out.push({ text: text.slice(start, r.end), mark: true })
    at = r.end
  }

  if (at < text.length) out.push({ text: text.slice(at), mark: false })

  return out
}

/** core's located 400 body from a failed $fetch, or null. */
export function promptErrorFrom(err: unknown): PromptErrorBody | null {
  const data = (err as { data?: Partial<PromptErrorBody> })?.data
  if (!data || typeof data.field !== 'string' || typeof data.code !== 'string'
    || typeof data.start !== 'number' || typeof data.end !== 'number') return null

  return {
    error: data.error ?? data.message ?? 'invalid prompt',
    code: data.code,
    message: data.message ?? data.error ?? 'invalid prompt',
    hint: data.hint,
    field: data.field,
    start: data.start,
    end: data.end,
    tag: data.tag ?? 0,
  }
}

/** The user's own tags from a stored Spec, in the syntax they edit in. */
export function userTagTexts(tags: PromptTag[] | undefined): string[] {
  return (tags ?? []).filter(t => t.source === 'user').map(t => t.nai || t.text)
}

/** Display label for a stored tag: its text, plus the emitted weight. */
export function tagLabel(t: PromptTag): string {
  const ws = new Set(t.runs.filter(r => r.text.trim()).map(r => r.emitted))
  if (ws.size === 1) {
    const [w] = [...ws]

    return w === 1 ? t.text : `${t.text} ×${w}`
  }

  return t.nai || t.text
}

/**
 * Plain tag name for preview lookups: weighting syntax stripped. Display
 * only — what is sent is always the text as typed; core parses it.
 */
export function plainTagName(token: string): string {
  return token
    .replace(/-?\d*\.?\d*::/g, ' ')
    .replace(/[{}[\]]/g, ' ')
    .replace(/^\s*\((.+):\s*[-+]?[\d.]+\s*\)\s*$/, '$1')
    .replace(/\\([()])/g, '$1')
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .join('_')
}
