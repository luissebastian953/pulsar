import { useLocalStorage } from '@vueuse/core'
import { computed, effectScope } from 'vue'
import { rewritePosePresetPrompts } from '~/composables/usePosePresets'
import { rewritePresetTexts } from '~/composables/usePresets'
import type { ChunkPayload, ChunkSpan, PromptSurface } from '~/types/api'
import {
  CHUNKS_COLLAPSED_KEY, CHUNKS_KEY, DEFAULT_CHUNK_COLOR,
  addCategory, addChunk, bakeSpans, chunkToken, emptyChunkState, exportChunks, importChunks, isChunkColor,
  moveCategory, moveChunk, referencedChunks, removeCategory, removeChunks, renameInText, sanitizeChunkState,
  tokenNames, updateCategory, updateChunk,
  type ChunkCategory, type ChunkColor, type ChunkInput, type ChunkState, type PromptChunk,
} from '~/utils/chunks'

// The single owner of the prompt-chunk store: useLocalStorage
// ('pulsar.chunks.v1'), like saved configs — core has no identity, and the
// Electron/Capacitor shells have no cookies or server sessions (decision D7).
// Core never stores a chunk: each request carries the ones it references.
//
// Everything that needs core (validating content, baking a reference into
// plain text) goes through a `ChunkParser` the caller builds over
// POST /prompt/parse — orbit never expands a chunk itself.

/** The resolved references core found in `text`, or null when core can't be asked. */
export type ChunkParser = (text: string, surface: PromptSurface, chunks: ChunkPayload[]) => Promise<ChunkSpan[] | null>

/** A live editor field: applies an edit to its own text (rename, bake). */
export type FieldRewriter = (edit: (text: string, surface: PromptSurface) => string | Promise<string>) => void | Promise<void>

/** Where a clicked chunk goes: the last-focused prompt field. */
export interface ActiveField {
  insert: (token: string) => void
}

/** A preset's copy of the chunks it references (PresetConfig.chunks). */
export type ChunkSnapshot = { name: string, content: string, color?: string }

function create() {
  const raw = useLocalStorage<ChunkState>(CHUNKS_KEY, emptyChunkState())
  const collapsed = useLocalStorage<string[]>(CHUNKS_COLLAPSED_KEY, [])
  const state = computed(() => sanitizeChunkState(raw.value))
  const chunks = computed(() => state.value.chunks)
  const categories = computed(() => state.value.categories)
  const byName = computed(() => new Map(chunks.value.map(c => [c.name, c])))

  const fields = new Set<FieldRewriter>()
  let active: ActiveField | null = null

  const newId = () => crypto.randomUUID()

  function commit(next: ChunkState) {
    raw.value = next
  }

  /** Register a live editor field; returns the unregister function. */
  function registerField(f: FieldRewriter): () => void {
    fields.add(f)

    return () => fields.delete(f)
  }

  async function rewriteFields(edit: (text: string, surface: PromptSurface) => string | Promise<string>) {
    await Promise.all([...fields].map(f => f(edit)))
  }

  function add(input: ChunkInput): { ok: true, chunk: PromptChunk } | { ok: false, error: string } {
    const res = addChunk(state.value, input, newId(), Date.now())

    if (!res.ok) return res

    commit(res.state)

    return { ok: true, chunk: res.value }
  }

  /**
   * Save an edit. A rename rewrites every `!macro:Old!`: in the other
   * chunks, the live composer fields, saved configs and pose presets.
   */
  function update(id: string, input: ChunkInput): { ok: true, chunk: PromptChunk } | { ok: false, error: string } {
    const before = state.value.chunks.find(c => c.id === id)
    const res = updateChunk(state.value, id, input, Date.now())

    if (!res.ok) return res

    commit(res.state)

    if (before && before.name !== res.value.name) {
      const [from, to] = [before.name, res.value.name]
      const edit = (text: string) => renameInText(text, from, to)
      void rewriteFields(edit)
      rewritePresetTexts(edit, snap => snap.map(s => (s.name === from ? { ...s, name: to } : { ...s, content: edit(s.content) })))
      rewritePosePresetPrompts(edit)
    }

    return { ok: true, chunk: res.value }
  }

  /**
   * Bake references to `names` into plain text — in the live fields and in
   * the chunks that nest them — using the spans core returns. A field core
   * can't parse right now keeps its token (it then shows `unknown_chunk`).
   */
  async function bake(names: string[], parse: ChunkParser) {
    const all = chunks.value.map(c => ({ name: c.name, content: c.content }))
    const edit = async (text: string, surface: PromptSurface) => {
      if (!tokenNames(text).length) return text

      const spans = await parse(text, surface, referencedChunks([text], all)).catch(() => null)

      // the top-level reference may be another chunk that nests a doomed one:
      // its own content is baked below, so only direct references bake here
      return spans ? bakeSpans(text, spans, names) : text
    }
    await rewriteFields(edit)
    let next = state.value
    for (const c of state.value.chunks) {
      if (names.includes(c.name) || !tokenNames(c.content).some(n => names.includes(n))) continue

      const content = await edit(c.content, 'prompt')

      if (content !== c.content) {
        next = { ...next, chunks: next.chunks.map(x => (x.id === c.id ? { ...x, content, updatedAt: Date.now() } : x)) }
      }
    }

    commit(next)
  }

  /** Delete chunks; their references are baked first, so none dangle. */
  async function remove(ids: string[], parse: ChunkParser) {
    const names = chunks.value.filter(c => ids.includes(c.id)).map(c => c.name)
    await bake(names, parse)
    commit(removeChunks(state.value, ids))
  }

  /** Delete everything (the caller confirms first): bake all, then wipe. */
  async function removeAll(parse: ChunkParser) {
    await rewriteFields(async (text, surface) => {
      if (!tokenNames(text).length) return text

      const spans = await parse(text, surface, referencedChunks([text], chunks.value)).catch(() => null)

      return spans ? bakeSpans(text, spans) : text
    })
    commit(emptyChunkState())
    collapsed.value = []
  }

  function saveCategory(id: string | null, name: string, color: string): { ok: true, category: ChunkCategory } | { ok: false, error: string } {
    const res = id ? updateCategory(state.value, id, name, color) : addCategory(state.value, name, color, newId())

    if (!res.ok) return res

    commit(res.state)

    return { ok: true, category: res.value }
  }

  function deleteCategory(id: string) {
    commit(removeCategory(state.value, id))
    collapsed.value = collapsed.value.filter(c => c !== id)
  }

  function toggleCollapsed(id: string) {
    collapsed.value = collapsed.value.includes(id) ? collapsed.value.filter(c => c !== id) : [...collapsed.value, id]
  }

  const reorderChunk = (id: string, categoryId: string, index: number) => commit(moveChunk(state.value, id, categoryId, index))
  const reorderCategory = (id: string, index: number) => commit(moveCategory(state.value, id, index))

  const toJSON = () => JSON.stringify(exportChunks(state.value), null, 2)

  function fromJSON(json: string): { ok: true, added: number, updated: number, skipped: string[] } | { ok: false, error: string } {
    let data: unknown
    try {
      data = JSON.parse(json)
    }
    catch {
      return { ok: false, error: 'That file is not valid JSON' }
    }

    const res = importChunks(state.value, data, newId, Date.now())

    if ('error' in res) return { ok: false, error: res.error }

    commit(res.state)

    return { ok: true, added: res.added, updated: res.updated, skipped: res.skipped }
  }

  /** The chunks these texts reference, nested ones included: a request's `chunks`. */
  const referenced = (texts: string[]): ChunkPayload[] => referencedChunks(texts, chunks.value)

  const colorOf = (name: string): ChunkColor => byName.value.get(name)?.color ?? DEFAULT_CHUNK_COLOR

  /** A saved config's copy of the chunks it references. */
  const snapshot = (texts: string[]): ChunkSnapshot[] =>
    referenced(texts).map(c => ({ ...c, color: colorOf(c.name) }))

  /**
   * Applying a saved config: recreate the chunks it needs that are gone;
   * report the ones that exist with different content (the local chunk
   * wins — it is what the tokens will expand to).
   */
  function restoreSnapshot(snap: ChunkSnapshot[] | undefined): { created: string[], mismatched: string[] } {
    const out = { created: [] as string[], mismatched: [] as string[] }
    for (const s of snap ?? []) {
      const cur = byName.value.get(s.name)

      if (cur) {
        if (cur.content !== s.content) out.mismatched.push(s.name)

        continue
      }

      const res = add({ name: s.name, content: s.content, color: isChunkColor(s.color) ? s.color : undefined })

      if (res.ok) out.created.push(s.name)
    }

    return out
  }

  function setActiveField(f: ActiveField | null) {
    active = f
  }

  function clearActiveField(f: ActiveField) {
    if (active === f) active = null
  }

  /** Insert a chunk's token at the last-focused field's caret; false = no field. */
  function insertIntoActive(name: string): boolean {
    if (!active) return false

    active.insert(chunkToken(name))

    return true
  }

  return {
    state, chunks, categories, byName, collapsed,
    add, update, remove, removeAll, bake, saveCategory, deleteCategory, toggleCollapsed,
    reorderChunk, reorderCategory, toJSON, fromJSON,
    referenced, colorOf, snapshot, restoreSnapshot,
    registerField, setActiveField, clearActiveField, insertIntoActive,
  }
}

let shared: ReturnType<typeof create> | null = null

/**
 * One store per page: every editor and the panel share it. It lives in its
 * own detached effect scope — created inside whichever component asks
 * first, it must not stop persisting when that component unmounts.
 */
export function useChunks() {
  return (shared ??= effectScope(true).run(create)!)
}

/** Tests only: drop the shared instance so the next call re-reads storage. */
export function resetChunksForTest() {
  shared = null
}
