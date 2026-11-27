// Prompt chunks — the pure half (docs/frontend.md#prompt-chunks). A chunk is
// a named snippet a prompt refers to as `!macro:Name!`. Orbit stores chunks
// and inserts tokens; core resolves them (expansion, nesting, errors,
// offsets). Nothing here parses prompt syntax: it only recognises where a
// token is, so it can be inserted, renamed, tinted and sent along.
import type { ChunkPayload, ChunkSpan, PromptTag } from '~/types/api'

export const CHUNKS_KEY = 'pulsar.chunks.v1'
export const CHUNKS_COLLAPSED_KEY = 'pulsar.chunks.collapsed'

// decision D4 — the same bounds core enforces per request
export const MAX_CHUNKS = 256
export const MAX_CHUNK_BYTES = 4 << 10
export const MAX_CHUNK_NAME = 64
export const MAX_CHUNKS_PER_REQUEST = 64

// decision D10 — a fixed palette of @theme tokens (--color-chunk-1…8), stored
// by key. No free hex: a colour that isn't a token isn't in the design.
export const CHUNK_COLORS = ['chunk-1', 'chunk-2', 'chunk-3', 'chunk-4', 'chunk-5', 'chunk-6', 'chunk-7', 'chunk-8'] as const
export type ChunkColor = typeof CHUNK_COLORS[number]
export const DEFAULT_CHUNK_COLOR: ChunkColor = 'chunk-1'
export const CHUNK_COLOR_NAMES: Record<ChunkColor, string> = {
  'chunk-1': 'neutral',
  'chunk-2': 'red',
  'chunk-3': 'orange',
  'chunk-4': 'yellow',
  'chunk-5': 'green',
  'chunk-6': 'teal',
  'chunk-7': 'blue',
  'chunk-8': 'violet',
}

export function isChunkColor(v: unknown): v is ChunkColor {
  return typeof v === 'string' && (CHUNK_COLORS as readonly string[]).includes(v)
}

export interface PromptChunk {
  id: string
  name: string
  content: string
  color: ChunkColor
  /** '' = Uncategorized */
  categoryId: string
  order: number
  createdAt: number
  updatedAt: number
}

export interface ChunkCategory {
  id: string
  name: string
  color: ChunkColor
  order: number
}

export interface ChunkState {
  chunks: PromptChunk[]
  categories: ChunkCategory[]
}

export const emptyChunkState = (): ChunkState => ({ chunks: [], categories: [] })

// ── names ───────────────────────────────────────────────────────────

const NAME_FORBIDDEN = /[!,|{}[\]\n\r\t]|::/

/** D4 name rules; returns the problem, or '' when the name is fine. */
export function chunkNameError(name: string, taken: string[] = []): string {
  if (!name) return 'Name is required'

  if (name !== name.trim()) return 'Name can\'t start or end with a space'

  if ([...name].length > MAX_CHUNK_NAME) return `Name can be at most ${MAX_CHUNK_NAME} characters`

  // eslint-disable-next-line no-control-regex
  if (NAME_FORBIDDEN.test(name) || /[\u0000-\u001F\u007F]/.test(name)) return 'Name can\'t contain ! , | { } [ ] :: or a line break'

  if (taken.includes(name)) return `A chunk named "${name}" already exists`

  return ''
}

export function chunkContentError(content: string): string {
  if (!content.trim()) return 'Content is required'

  if (new TextEncoder().encode(content).length > MAX_CHUNK_BYTES) return `Content can be at most ${MAX_CHUNK_BYTES} bytes`

  return ''
}

// ── tokens ──────────────────────────────────────────────────────────

/** The reference token (decision D3). */
export const chunkToken = (name: string) => `!macro:${name}!`

const tokenRe = () => /!macro:([^!\n]+)!/g

/** Names referenced in a text, in order, deduplicated (core trims; so do we). */
export function tokenNames(text: string): string[] {
  const out: string[] = []
  for (const m of text.matchAll(tokenRe())) {
    const name = m[1]!.trim()

    if (name && !out.includes(name)) out.push(name)
  }

  return out
}

/** The chunk name when a whole chip is exactly one token, else null. */
export function chipChunkName(chip: string): string | null {
  const m = /^!macro:([^!\n]+)!$/.exec(chip.trim())

  return m ? m[1]!.trim() : null
}

/**
 * The chunks a set of texts needs, nested references included — what goes
 * in a request's `chunks`. Unknown names are left out: core answers them
 * with a located `unknown_chunk`.
 */
export function referencedChunks(texts: string[], chunks: Pick<PromptChunk, 'name' | 'content'>[]): ChunkPayload[] {
  const byName = new Map(chunks.map(c => [c.name, c]))
  const out: ChunkPayload[] = []
  const seen = new Set<string>()
  const queue = texts.flatMap(tokenNames)
  while (queue.length && out.length < MAX_CHUNKS_PER_REQUEST) {
    const name = queue.shift()!

    if (seen.has(name)) continue

    seen.add(name)
    const c = byName.get(name)

    if (!c) continue

    out.push({ name: c.name, content: c.content })
    queue.push(...tokenNames(c.content))
  }

  return out
}

/** Rewrite every reference to `from` as a reference to `to`. */
export function renameInText(text: string, from: string, to: string): string {
  return text.replace(tokenRe(), (whole, name: string) => (name.trim() === from ? chunkToken(to) : whole))
}

/**
 * Bake: replace references with their expansion, by the spans core
 * returned for exactly this text (POST /prompt/parse `chunks`). `names`
 * limits it to some chunks; omit to bake all.
 */
export function bakeSpans(text: string, spans: ChunkSpan[], names?: string[]): string {
  let out = text
  for (const s of [...spans].sort((a, b) => b.start - a.start)) {
    if (names && !names.includes(s.name)) continue

    if (s.start < 0 || s.end > out.length || s.end <= s.start) continue

    out = out.slice(0, s.start) + s.text + out.slice(s.end)
  }

  return out
}

/** The reference span the caret is inside, if any (touching an edge doesn't count). */
export function spanAt(spans: ChunkSpan[], caret: number): ChunkSpan | null {
  return spans.find(s => caret > s.start && caret < s.end) ?? null
}

// ── the @ picker ────────────────────────────────────────────────────

export interface PickerTrigger {
  /** index of the `@` */
  start: number
  query: string
}

/**
 * NovelAI's trigger rule: an `@` opens the picker only at the start of the
 * field or a line, after whitespace, or after a comma — never mid-word. The
 * query runs from the `@` to the caret; spaces are allowed, a comma or a
 * line break closes it.
 */
export function pickerTrigger(text: string, caret: number): PickerTrigger | null {
  for (let i = caret - 1; i >= 0; i--) {
    const ch = text[i]!

    if (ch === ',' || ch === '\n') return null

    if (ch !== '@') continue

    const before = i === 0 ? '' : text[i - 1]!

    if (before === '' || before === ',' || /\s/.test(before)) return { start: i, query: text.slice(i + 1, caret) }

    return null
  }

  return null
}

function subsequence(hay: string, needle: string): boolean {
  let k = 0
  for (const ch of hay) {
    if (ch === needle[k]) k++

    if (k === needle.length) return true
  }

  return needle.length === 0
}

/**
 * NovelAI's ranking, case-insensitive, filled in tiers without duplicates:
 * name starts with the query; name contains it; name contains it as a
 * subsequence; the same three on "Category: name"; content contains it.
 */
export function rankChunks(chunks: PromptChunk[], categories: ChunkCategory[], query: string): PromptChunk[] {
  const q = query.toLowerCase()
  const out: PromptChunk[] = []
  const seen = new Set<string>()
  const fill = (test: (c: PromptChunk) => boolean) => {
    for (const c of chunks) {
      if (!seen.has(c.id) && test(c)) {
        seen.add(c.id)
        out.push(c)
      }
    }
  }
  const catName = new Map(categories.map(c => [c.id, c.name]))
  const full = (c: PromptChunk) => `${catName.get(c.categoryId) ?? 'Uncategorized'}: ${c.name}`.toLowerCase()
  fill(c => c.name.toLowerCase().startsWith(q))
  fill(c => c.name.toLowerCase().includes(q))
  fill(c => subsequence(c.name.toLowerCase(), q))
  fill(c => full(c).startsWith(q))
  fill(c => full(c).includes(q))
  fill(c => subsequence(full(c), q))
  fill(c => c.content.toLowerCase().includes(q))

  return out
}

// ── the store, as pure transitions ──────────────────────────────────

const byOrder = <T extends { order: number }>(a: T, b: T) => a.order - b.order

/** Chunks of one category ('' = Uncategorized), in display order. */
export function chunksIn(state: ChunkState, categoryId: string): PromptChunk[] {
  return state.chunks.filter(c => c.categoryId === categoryId).sort(byOrder)
}

export function sortedCategories(state: ChunkState): ChunkCategory[] {
  return [...state.categories].sort(byOrder)
}

/** Every chunk in display order: Uncategorized first, then by category. */
export function orderedChunks(state: ChunkState): PromptChunk[] {
  return [...chunksIn(state, ''), ...sortedCategories(state).flatMap(c => chunksIn(state, c.id))]
}

export type ChunkInput = { name: string, content: string, color?: string, categoryId?: string }
export type Result<T> = { ok: true, state: ChunkState, value: T } | { ok: false, error: string }

function nextOrder(items: { order: number }[]): number {
  return items.reduce((m, i) => Math.max(m, i.order), -1) + 1
}

function checkInput(state: ChunkState, input: ChunkInput, selfId = ''): string {
  const taken = state.chunks.filter(c => c.id !== selfId).map(c => c.name)

  return chunkNameError(input.name, taken) || chunkContentError(input.content)
    || (input.color !== undefined && !isChunkColor(input.color) ? 'Pick a colour from the palette' : '')
    || (input.categoryId && !state.categories.some(c => c.id === input.categoryId) ? 'Unknown category' : '')
}

export function addChunk(state: ChunkState, input: ChunkInput, id: string, now: number): Result<PromptChunk> {
  if (state.chunks.length >= MAX_CHUNKS) return { ok: false, error: `You can keep at most ${MAX_CHUNKS} chunks` }

  const error = checkInput(state, input)

  if (error) return { ok: false, error }

  const categoryId = input.categoryId ?? ''
  const chunk: PromptChunk = {
    id, name: input.name, content: input.content.trim(), color: isChunkColor(input.color) ? input.color : DEFAULT_CHUNK_COLOR,
    categoryId, order: nextOrder(chunksIn(state, categoryId)), createdAt: now, updatedAt: now,
  }

  return { ok: true, value: chunk, state: { ...state, chunks: [...state.chunks, chunk] } }
}

/**
 * Update a chunk. A rename also rewrites `!macro:Old!` inside every other
 * chunk; the caller rewrites the composer fields and saved presets.
 */
export function updateChunk(state: ChunkState, id: string, input: ChunkInput, now: number): Result<PromptChunk> {
  const cur = state.chunks.find(c => c.id === id)

  if (!cur) return { ok: false, error: 'That chunk no longer exists' }

  const error = checkInput(state, input, id)

  if (error) return { ok: false, error }

  const categoryId = input.categoryId ?? cur.categoryId
  const next: PromptChunk = {
    ...cur, name: input.name, content: input.content.trim(), color: isChunkColor(input.color) ? input.color : cur.color,
    categoryId, order: categoryId === cur.categoryId ? cur.order : nextOrder(chunksIn(state, categoryId)), updatedAt: now,
  }
  const renamed = cur.name !== next.name
  const chunks = state.chunks.map((c) => {
    if (c.id === id) return next

    if (!renamed) return c

    const content = renameInText(c.content, cur.name, next.name)

    return content === c.content ? c : { ...c, content, updatedAt: now }
  })

  return { ok: true, value: next, state: { ...state, chunks } }
}

export function removeChunks(state: ChunkState, ids: string[]): ChunkState {
  return { ...state, chunks: state.chunks.filter(c => !ids.includes(c.id)) }
}

export function addCategory(state: ChunkState, name: string, color: string, id: string): Result<ChunkCategory> {
  const error = categoryError(state, name, color)

  if (error) return { ok: false, error }

  const cat: ChunkCategory = { id, name: name.trim(), color: color as ChunkColor, order: nextOrder(state.categories) }

  return { ok: true, value: cat, state: { ...state, categories: [...state.categories, cat] } }
}

function categoryError(state: ChunkState, name: string, color: string, selfId = ''): string {
  const n = name.trim()

  if (!n) return 'Name is required'

  if ([...n].length > MAX_CHUNK_NAME) return `Name can be at most ${MAX_CHUNK_NAME} characters`

  if (state.categories.some(c => c.id !== selfId && c.name === n)) return `A category named "${n}" already exists`

  if (!isChunkColor(color)) return 'Pick a colour from the palette'

  return ''
}

export function updateCategory(state: ChunkState, id: string, name: string, color: string): Result<ChunkCategory> {
  const cur = state.categories.find(c => c.id === id)

  if (!cur) return { ok: false, error: 'That category no longer exists' }

  const error = categoryError(state, name, color, id)

  if (error) return { ok: false, error }

  const next = { ...cur, name: name.trim(), color: color as ChunkColor }

  return { ok: true, value: next, state: { ...state, categories: state.categories.map(c => (c.id === id ? next : c)) } }
}

/** Deleting a category keeps its chunks: they move to Uncategorized. */
export function removeCategory(state: ChunkState, id: string): ChunkState {
  let order = nextOrder(chunksIn(state, ''))

  return {
    categories: state.categories.filter(c => c.id !== id),
    chunks: [...state.chunks].sort(byOrder).map(c => (c.categoryId === id ? { ...c, categoryId: '', order: order++ } : c)),
  }
}

/** Move a chunk to `index` within a category (drag and drop). */
export function moveChunk(state: ChunkState, id: string, categoryId: string, index: number): ChunkState {
  const moving = state.chunks.find(c => c.id === id)

  if (!moving || (categoryId && !state.categories.some(c => c.id === categoryId))) return state

  const target = chunksIn(state, categoryId).filter(c => c.id !== id)
  target.splice(Math.max(0, Math.min(index, target.length)), 0, moving)
  const order = new Map(target.map((c, i) => [c.id, i]))

  return {
    ...state,
    chunks: state.chunks.map(c => (order.has(c.id) ? { ...c, categoryId, order: order.get(c.id)! } : c)),
  }
}

export function moveCategory(state: ChunkState, id: string, index: number): ChunkState {
  const moving = state.categories.find(c => c.id === id)

  if (!moving) return state

  const rest = sortedCategories(state).filter(c => c.id !== id)
  rest.splice(Math.max(0, Math.min(index, rest.length)), 0, moving)

  return { ...state, categories: rest.map((c, i) => ({ ...c, order: i })) }
}

// ── export / import ─────────────────────────────────────────────────

export interface ChunkExport {
  format: 'pulsar.chunks'
  version: 1
  categories: { name: string, color: ChunkColor }[]
  chunks: { name: string, content: string, color: ChunkColor, category: string }[]
}

export function exportChunks(state: ChunkState): ChunkExport {
  const cats = sortedCategories(state)
  const catName = new Map(cats.map(c => [c.id, c.name]))

  return {
    format: 'pulsar.chunks',
    version: 1,
    categories: cats.map(c => ({ name: c.name, color: c.color })),
    chunks: orderedChunks(state).map(c => ({ name: c.name, content: c.content, color: c.color, category: catName.get(c.categoryId) ?? '' })),
  }
}

export interface ImportReport {
  state: ChunkState
  added: number
  updated: number
  skipped: string[]
}

/**
 * Merge an export into the store: a chunk whose name exists is updated,
 * the rest are added; categories are matched by name. Every record passes
 * the same D4 validation as the form — an imported file is untrusted. A
 * colour outside the palette falls back to the default.
 */
export function importChunks(state: ChunkState, data: unknown, newId: () => string, now: number): ImportReport | { error: string } {
  const d = data as Partial<ChunkExport> | null

  if (!d || typeof d !== 'object' || d.format !== 'pulsar.chunks' || d.version !== 1 || !Array.isArray(d.chunks)) {
    return { error: 'Not a pulsar chunk export' }
  }

  let next: ChunkState = { chunks: [...state.chunks], categories: [...state.categories] }
  const report: ImportReport = { state: next, added: 0, updated: 0, skipped: [] }
  const color = (v: unknown) => (isChunkColor(v) ? v : DEFAULT_CHUNK_COLOR)
  const catId = (name: unknown, c?: unknown): string => {
    if (typeof name !== 'string' || !name.trim()) return ''

    const hit = next.categories.find(x => x.name === name.trim())

    if (hit) return hit.id

    const res = addCategory(next, name, color(c), newId())

    if (!res.ok) return ''

    next = res.state

    return res.value.id
  }
  for (const c of Array.isArray(d.categories) ? d.categories : []) catId(c?.name, c?.color)

  for (const raw of d.chunks) {
    const name = typeof raw?.name === 'string' ? raw.name : ''
    const content = typeof raw?.content === 'string' ? raw.content : ''
    const input: ChunkInput = { name, content, color: color(raw?.color), categoryId: catId(raw?.category) }
    const existing = next.chunks.find(c => c.name === name)
    const res = existing ? updateChunk(next, existing.id, input, now) : addChunk(next, input, newId(), now)

    if (!res.ok) {
      report.skipped.push(name || '(unnamed)')
      continue
    }

    next = res.state

    if (existing) report.updated++
    else report.added++
  }

  report.state = next

  return report
}

/** Read persisted state defensively: localStorage is user-editable. */
export function sanitizeChunkState(v: unknown): ChunkState {
  const s = v as Partial<ChunkState> | null

  if (!s || typeof s !== 'object') return emptyChunkState()

  const categories = (Array.isArray(s.categories) ? s.categories : [])
    .filter(c => c && typeof c.id === 'string' && typeof c.name === 'string')
    .map((c, i) => ({ id: c.id, name: c.name, color: isChunkColor(c.color) ? c.color : DEFAULT_CHUNK_COLOR, order: Number.isFinite(c.order) ? c.order : i }))
  const chunks = (Array.isArray(s.chunks) ? s.chunks : [])
    .filter(c => c && typeof c.id === 'string' && typeof c.name === 'string' && typeof c.content === 'string' && !chunkNameError(c.name))
    .slice(0, MAX_CHUNKS)
    .map((c, i) => ({
      id: c.id, name: c.name, content: c.content, color: isChunkColor(c.color) ? c.color : DEFAULT_CHUNK_COLOR,
      categoryId: categories.some(k => k.id === c.categoryId) ? c.categoryId : '',
      order: Number.isFinite(c.order) ? c.order : i, createdAt: Number(c.createdAt) || 0, updatedAt: Number(c.updatedAt) || 0,
    }))

  return { chunks, categories }
}

// ── history restore (decision D8) ───────────────────────────────────

/** What a bare `!macro:Name!` resolves to today, per core. */
export type ChunkResolution = { hash: string, nais: string[] }

/**
 * Turn a stored surface's user tags back into editor pieces. A run of tags
 * that came from one chunk becomes the `!macro:Name!` token again only when
 * today's chunk of that name still resolves to exactly those tags (same
 * hash, same tags in NovelAI syntax — so an outer weight or a since-edited
 * chunk falls back). Otherwise the expanded tags are restored: they are the
 * authoritative record.
 */
export async function restoreUserTags(
  tags: PromptTag[] | undefined,
  resolve: (name: string) => Promise<ChunkResolution | null>,
): Promise<{ pieces: string[], restored: string[] }> {
  const user = (tags ?? []).filter(t => t.source === 'user')
  const out: string[] = []
  const restored: string[] = []
  const cache = new Map<string, ChunkResolution | null>()
  let i = 0
  while (i < user.length) {
    const t = user[i]!
    const ref = t.chunk

    if (!ref) {
      out.push(t.nai || t.text)
      i++
      continue
    }

    let j = i
    while (j < user.length && user[j]!.chunk?.name === ref.name && user[j]!.chunk?.hash === ref.hash) j++

    const group = user.slice(i, j).map(g => g.nai || g.text)

    if (!cache.has(ref.name)) cache.set(ref.name, await resolve(ref.name).catch(() => null))

    const now = cache.get(ref.name)
    const n = now?.nais.length ?? 0
    // one token per repetition of the chunk's own tag list
    const exact = !!now && now.hash === ref.hash && n > 0 && group.length % n === 0
      && group.every((g, k) => g === now.nais[k % n])

    if (exact) {
      for (let k = 0; k < group.length / n; k++) out.push(chunkToken(ref.name))

      if (!restored.includes(ref.name)) restored.push(ref.name)
    }
    else {
      out.push(...group)
    }

    i = j
  }

  return { pieces: out, restored }
}

// ── emphasis (decision D13) ─────────────────────────────────────────

/**
 * NovelAI's highlight intensity for a weight: nothing within 0.01 of 1,
 * else 0.2 + 0.4·clamp(|w−1| / span), span 1 for w > 0 and 0.5 for w ≤ 0.
 */
export function emphasisIntensity(w: number): number {
  const d = Math.abs(w - 1)

  if (d < 0.01) return 0

  return 0.2 + 0.4 * Math.min(1, d / (w > 0 ? 1 : 0.5))
}

/** The fixed intensity of an operator mark (`{ } [ ] N:: ::`). */
export const OP_INTENSITY = 0.5
