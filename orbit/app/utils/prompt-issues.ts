// Mapping core's prompt issues onto the composer's inputs. Core owns the
// grammar (POST /prompt/parse); these helpers only locate its answers.
// Offsets are UTF-16 code units — exactly JS string indices.
import type { ChunkSpan, PromptErrorBody, PromptIssue, PromptMark, PromptTag } from '~/types/api'
import { OP_INTENSITY, emphasisIntensity, type ChunkColor } from '~/utils/chunks'

// What a highlighted stretch is. `error` (the default) is a parse error;
// `chunk` a resolved `!macro:Name!`; `up` / `down` / `op` the emphasis
// highlight. Where they overlap the earlier kind here wins: an error is
// never hidden, and a chunk token is painted as a chunk, not by weight.
export type MarkKind = 'error' | 'chunk' | 'up' | 'down' | 'op'
const PRIORITY: MarkKind[] = ['error', 'chunk', 'op', 'up', 'down']

export type Range = {
  start: number
  end: number
  kind?: MarkKind
  /** up / down / op: tint strength, 0..1 */
  intensity?: number
  /** chunk: palette key */
  color?: ChunkColor
  /** chunk: what the token expands to (tooltip) */
  title?: string
}

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

export type Segment = {
  text: string
  /** true for an error stretch (the red wavy mark) */
  mark: boolean
  kind?: MarkKind
  intensity?: number
  color?: ChunkColor
  title?: string
}

/**
 * Split text into stretches for a highlight overlay. Ranges may overlap;
 * each stretch takes the highest-priority range covering it, and plain
 * stretches carry no kind.
 */
export function segmentText(text: string, ranges: Range[]): Segment[] {
  const marks = ranges
    .map(r => ({ ...r, kind: r.kind ?? 'error' as MarkKind, start: Math.max(0, r.start), end: Math.min(text.length, r.end > r.start ? r.end : r.start + 1) }))
    .filter(r => r.end > r.start)

  if (!marks.length) return text ? [{ text, mark: false }] : []

  const cuts = [...new Set([0, text.length, ...marks.flatMap(r => [r.start, r.end])])].sort((a, b) => a - b)
  const out: Segment[] = []
  for (let i = 0; i + 1 < cuts.length; i++) {
    const [a, b] = [cuts[i]!, cuts[i + 1]!]
    const cover = marks.filter(r => r.start <= a && r.end >= b)
      .sort((x, y) => PRIORITY.indexOf(x.kind) - PRIORITY.indexOf(y.kind) || x.start - y.start)[0]
    const seg: Segment = cover
      ? { text: text.slice(a, b), mark: cover.kind === 'error', kind: cover.kind }
      : { text: text.slice(a, b), mark: false }

    if (cover?.intensity !== undefined) seg.intensity = cover.intensity

    if (cover?.color) seg.color = cover.color

    if (cover?.title) seg.title = cover.title

    const last = out[out.length - 1]

    // merge neighbours painted by the same range kind (an error split by a cut)
    if (last && last.kind === seg.kind && last.intensity === seg.intensity && last.color === seg.color && last.title === seg.title
      && (seg.kind === undefined || seg.kind === 'error')) last.text += seg.text
    else out.push(seg)
  }

  return out
}

/** core's emphasis marks as overlay ranges (decision D13). */
export function emphasisRanges(marks: PromptMark[] | undefined): Range[] {
  return (marks ?? [])
    .map(m => ({ start: m.start, end: m.end, kind: m.kind, intensity: m.kind === 'op' ? OP_INTENSITY : emphasisIntensity(m.w) }))
    .filter(r => r.intensity > 0)
}

/** core's resolved references as overlay ranges, in each chunk's colour. */
export function chunkRanges(spans: ChunkSpan[] | undefined, colorOf: (name: string) => ChunkColor): Range[] {
  return (spans ?? []).map(s => ({ start: s.start, end: s.end, kind: 'chunk' as const, color: colorOf(s.name), title: s.text }))
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
