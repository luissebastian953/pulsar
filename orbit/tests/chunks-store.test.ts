import { beforeEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'
import { resetChunksForTest, useChunks, type ChunkParser } from '~/composables/useChunks'
import type { ChunkSpan, PromptTag } from '~/types/api'
import {
  CHUNKS_KEY, CHUNK_COLORS, MAX_CHUNKS, MAX_CHUNKS_PER_REQUEST, MAX_CHUNK_BYTES, MAX_CHUNK_NAME,
  bakeSpans, chipChunkName, chunkNameError, chunkToken, emphasisIntensity, isChunkColor, referencedChunks,
  renameInText, restoreUserTags, sanitizeChunkState, tokenNames,
} from '~/utils/chunks'

// A stand-in for POST /prompt/parse's `chunks`: the spans of every top-level
// token, each expanded from the chunks the request carried. Core owns the
// real thing; the store only replaces by the spans it is handed.
const fakeParser: ChunkParser = async (text, _surface, chunks) => {
  const byName = new Map(chunks.map(c => [c.name, c.content]))
  const expand = (s: string): string => s.replace(/!macro:([^!\n]+)!/g, (_m, n: string) => expand(byName.get(n.trim()) ?? ''))
  const out: ChunkSpan[] = []
  for (const m of text.matchAll(/!macro:([^!\n]+)!/g)) {
    const name = m[1]!.trim()

    if (byName.has(name)) out.push({ name, hash: 'h', start: m.index, end: m.index + m[0].length, tag: 0, text: expand(byName.get(name)!) })
  }

  return out
}

function fresh() {
  localStorage.clear()
  resetChunksForTest()

  return useChunks()
}

describe('chunk names and tokens', () => {
  it('enforces the D4 name rules', () => {
    for (const ok of ['Silver Wolf', 'a', 'Category: label', '名前', 'n'.repeat(MAX_CHUNK_NAME)]) expect(chunkNameError(ok)).toBe('')

    for (const bad of ['', ' x', 'x ', 'a!b', 'a,b', 'a|b', 'a{b', 'a}b', 'a[b', 'a]b', 'a::b', 'a\nb', 'n'.repeat(MAX_CHUNK_NAME + 1)]) {
      expect(chunkNameError(bad), JSON.stringify(bad)).not.toBe('')
    }

    // unique, case-sensitive
    expect(chunkNameError('Hair', ['Hair'])).toContain('already exists')
    expect(chunkNameError('hair', ['Hair'])).toBe('')
  })

  it('recognises tokens without parsing anything else', () => {
    expect(chunkToken('Silver Wolf')).toBe('!macro:Silver Wolf!')
    expect(tokenNames('1girl, !macro:A!, {!macro: B !}, !macro:A!, !, !?, !!')).toEqual(['A', 'B'])
    expect(chipChunkName('!macro:Silver Wolf!')).toBe('Silver Wolf')
    expect(chipChunkName('{!macro:A!}')).toBeNull()
    expect(chipChunkName('@_@')).toBeNull()
  })

  it('collects the referenced chunks, nested ones included, capped per request', () => {
    const chunks = [
      { name: 'A', content: 'a, !macro:B!' }, { name: 'B', content: 'b, !macro:C!' },
      { name: 'C', content: 'c' }, { name: 'Unused', content: 'x' },
    ]
    expect(referencedChunks(['1girl, !macro:A!', '!macro:Missing!'], chunks).map(c => c.name)).toEqual(['A', 'B', 'C'])
    // a cycle terminates
    expect(referencedChunks(['!macro:X!'], [{ name: 'X', content: '!macro:Y!' }, { name: 'Y', content: '!macro:X!' }])).toHaveLength(2)
    const many = Array.from({ length: 100 }, (_, i) => ({ name: `c${i}`, content: 'x' }))
    expect(referencedChunks([many.map(c => chunkToken(c.name)).join(', ')], many)).toHaveLength(MAX_CHUNKS_PER_REQUEST)
  })

  it('rewrites a renamed reference and bakes by span', () => {
    expect(renameInText('a, !macro:Old!, !macro:Older!, !macro: Old !', 'Old', 'New')).toBe('a, !macro:New!, !macro:Older!, !macro:New!')
    const text = '😀, !macro:A!, x, !macro:B!'
    const spans: ChunkSpan[] = [
      { name: 'A', hash: '', start: 4, end: 13, tag: 1, text: 'silver hair, long hair' },
      { name: 'B', hash: '', start: 18, end: 27, tag: 3, text: '{smile}' },
    ]
    expect(bakeSpans(text, spans)).toBe('😀, silver hair, long hair, x, {smile}')
    expect(bakeSpans(text, spans, ['B'])).toBe('😀, !macro:A!, x, {smile}')
    // spans that don't fit the text are ignored, never guessed
    expect(bakeSpans('short', spans)).toBe('short')
  })

  it('accepts only palette colours', () => {
    for (const c of CHUNK_COLORS) expect(isChunkColor(c)).toBe(true)

    for (const bad of ['#6B7280', 'red', 'chunk-9', 'chunk-0', '', null, undefined, 3, 'var(--color-chunk-1)', 'chunk-1; background:url(x)']) {
      expect(isChunkColor(bad), String(bad)).toBe(false)
    }
  })

  it('maps a weight to NovelAI\'s highlight intensity', () => {
    expect(emphasisIntensity(1)).toBe(0)
    expect(emphasisIntensity(1.005)).toBe(0)
    expect(emphasisIntensity(1.05)).toBeCloseTo(0.22)
    expect(emphasisIntensity(1.5)).toBeCloseTo(0.4)
    expect(emphasisIntensity(2)).toBeCloseTo(0.6)
    expect(emphasisIntensity(5)).toBeCloseTo(0.6) // clamped
    expect(emphasisIntensity(0.5)).toBeCloseTo(0.4)
    expect(emphasisIntensity(0)).toBeCloseTo(0.6) // w ≤ 0 uses the 0.5 span
    expect(emphasisIntensity(-2)).toBeCloseTo(0.6)
  })
})

describe('useChunks', () => {
  beforeEach(() => {
    localStorage.clear()
    resetChunksForTest()
  })

  it('persists under pulsar.chunks.v1 and survives a reload', async () => {
    const store = fresh()
    expect(store.add({ name: 'Hair', content: ' silver hair, long hair ' }).ok).toBe(true)
    await nextTick()
    const saved = JSON.parse(localStorage.getItem(CHUNKS_KEY)!)
    expect(saved.chunks).toHaveLength(1)
    expect(saved.chunks[0]).toMatchObject({ name: 'Hair', content: 'silver hair, long hair', color: 'chunk-1', categoryId: '', order: 0 })
    expect(Object.keys(saved.chunks[0]).sort()).toEqual(['categoryId', 'color', 'content', 'createdAt', 'id', 'name', 'order', 'updatedAt'])
    // "reload": a new instance reads the same storage
    resetChunksForTest()
    expect(useChunks().chunks.value.map(c => c.name)).toEqual(['Hair'])
  })

  it('validates on add and update: names, uniqueness, content, limits, colour', () => {
    const store = fresh()
    expect(store.add({ name: 'Hair', content: 'x' })).toMatchObject({ ok: true })
    expect(store.add({ name: 'Hair', content: 'y' })).toMatchObject({ ok: false })
    expect(store.add({ name: 'a,b', content: 'y' })).toMatchObject({ ok: false })
    expect(store.add({ name: 'B', content: '   ' })).toMatchObject({ ok: false, error: 'Content is required' })
    expect(store.add({ name: 'B', content: 'x'.repeat(MAX_CHUNK_BYTES + 1) })).toMatchObject({ ok: false })
    expect(store.add({ name: 'B', content: 'x', color: '#ff0000' })).toMatchObject({ ok: false, error: 'Pick a colour from the palette' })
    expect(store.add({ name: 'B', content: 'x', categoryId: 'nope' })).toMatchObject({ ok: false })
    const b = store.add({ name: 'B', content: 'x', color: 'chunk-4' })
    expect(b).toMatchObject({ ok: true, chunk: { color: 'chunk-4' } })
    // renaming onto an existing name is refused; keeping one's own name is fine
    const id = b.ok ? b.chunk.id : ''
    expect(store.update(id, { name: 'Hair', content: 'x' })).toMatchObject({ ok: false })
    expect(store.update(id, { name: 'B', content: 'z' })).toMatchObject({ ok: true, chunk: { content: 'z', color: 'chunk-4' } })

    for (let i = store.chunks.value.length; i < MAX_CHUNKS; i++) expect(store.add({ name: `c${i}`, content: 'x' }).ok).toBe(true)

    expect(store.add({ name: 'one too many', content: 'x' })).toMatchObject({ ok: false })
  })

  it('a rename rewrites other chunks, live fields, saved configs and pose presets', async () => {
    localStorage.setItem('pulsar.presets.v1', JSON.stringify([
      { id: 'p1', name: 'cfg', createdAt: 1, config: {
        prompt: 'a girl, !macro:Old!', tagList: ['1girl', '!macro:Old!'], und: '!macro:Old!, blurry',
        chars: [{ text: 'x, !macro:Old!', enabled: true, ref: null }], chunks: [{ name: 'Old', content: 'silver hair' }, { name: 'Nest', content: '!macro:Old!' }],
      } },
      { id: 'p2', name: 'untouched', createdAt: 1, config: { prompt: 'plain', tagList: [], und: '', chars: [] } },
    ]))
    localStorage.setItem('pulsar.pose-presets.v1', JSON.stringify([{ id: 'q1', name: 'pose', createdAt: 1, config: { prompt: 'standing, !macro:Old!' } }]))
    resetChunksForTest()
    const store = useChunks()
    const old = store.add({ name: 'Old', content: 'silver hair' })
    store.add({ name: 'Nest', content: 'smile, !macro:Old!' })
    const field = { text: '1girl, !macro:Old!, {!macro:Old!}' }
    store.registerField(async (edit) => {
      field.text = await edit(field.text, 'prompt')
    })

    expect(store.update(old.ok ? old.chunk.id : '', { name: 'New Name', content: 'silver hair' }).ok).toBe(true)
    await nextTick()
    await Promise.resolve()

    expect(store.byName.value.get('Nest')!.content).toBe('smile, !macro:New Name!')
    expect(field.text).toBe('1girl, !macro:New Name!, {!macro:New Name!}')
    const presets = JSON.parse(localStorage.getItem('pulsar.presets.v1')!)
    expect(presets[0].config).toMatchObject({
      prompt: 'a girl, !macro:New Name!', tagList: ['1girl', '!macro:New Name!'], und: '!macro:New Name!, blurry',
      chars: [{ text: 'x, !macro:New Name!' }], chunks: [{ name: 'New Name', content: 'silver hair' }, { name: 'Nest', content: '!macro:New Name!' }],
    })
    expect(presets[1].config.prompt).toBe('plain')
    expect(JSON.parse(localStorage.getItem('pulsar.pose-presets.v1')!)[0].config.prompt).toBe('standing, !macro:New Name!')
  })

  it('deleting a chunk bakes its references by span — in fields and nesting chunks', async () => {
    const store = fresh()
    const hair = store.add({ name: 'Hair', content: 'silver hair, {long hair}' })
    store.add({ name: 'Keep', content: 'smile' })
    store.add({ name: 'Look', content: 'school uniform, !macro:Hair!' })
    const fields = { prompt: '😀, !macro:Hair!, !macro:Keep!', und: '[!macro:Hair!]', nested: '!macro:Look!' }
    store.registerField(async (edit) => {
      fields.prompt = await edit(fields.prompt, 'prompt')
      fields.und = await edit(fields.und, 'negative')
      fields.nested = await edit(fields.nested, 'prompt')
    })

    await store.remove([hair.ok ? hair.chunk.id : ''], fakeParser)

    expect(fields.prompt).toBe('😀, silver hair, {long hair}, !macro:Keep!')
    expect(fields.und).toBe('[silver hair, {long hair}]')
    // a reference to a surviving chunk stays a reference; the survivor's own content was baked
    expect(fields.nested).toBe('!macro:Look!')
    expect(store.byName.value.get('Look')!.content).toBe('school uniform, silver hair, {long hair}')
    expect(store.chunks.value.map(c => c.name)).toEqual(['Keep', 'Look'])
  })

  it('a field core cannot parse keeps its token; delete-all bakes everything', async () => {
    const store = fresh()
    const a = store.add({ name: 'A', content: 'x, y' })
    const field = { text: '!macro:A!, z' }
    const unregister = store.registerField(async (edit) => {
      field.text = await edit(field.text, 'prompt')
    })
    await store.remove([a.ok ? a.chunk.id : ''], async () => null)
    expect(field.text).toBe('!macro:A!, z')
    expect(store.chunks.value).toHaveLength(0)

    store.add({ name: 'A', content: 'x, y' })
    store.add({ name: 'B', content: 'b, !macro:A!' })
    store.saveCategory(null, 'Cat', 'chunk-3')
    field.text = '!macro:B!, !macro:A!'
    await store.removeAll(fakeParser)
    expect(field.text).toBe('b, x, y, x, y')
    expect(store.chunks.value).toHaveLength(0)
    expect(store.categories.value).toHaveLength(0)
    // an unregistered field is left alone
    unregister()
    store.add({ name: 'A', content: 'q' })
    field.text = '!macro:A!'
    await store.removeAll(fakeParser)
    expect(field.text).toBe('!macro:A!')
  })

  it('categories: add, colour, delete moves chunks to Uncategorized, reorder', () => {
    const store = fresh()
    const cat = store.saveCategory(null, 'Characters', 'chunk-5')
    expect(cat).toMatchObject({ ok: true, category: { name: 'Characters', color: 'chunk-5' } })
    expect(store.saveCategory(null, 'Characters', 'chunk-5')).toMatchObject({ ok: false })
    expect(store.saveCategory(null, 'Other', '#123456')).toMatchObject({ ok: false })
    const catId = cat.ok ? cat.category.id : ''
    store.add({ name: 'loose', content: 'x' })
    const a = store.add({ name: 'a', content: 'x', categoryId: catId })
    store.add({ name: 'b', content: 'x', categoryId: catId })
    const c = store.add({ name: 'c', content: 'x', categoryId: catId })
    const names = (id: string) => store.chunks.value.filter(k => k.categoryId === id).sort((x, y) => x.order - y.order).map(k => k.name)
    expect(names(catId)).toEqual(['a', 'b', 'c'])
    store.reorderChunk(c.ok ? c.chunk.id : '', catId, 0)
    expect(names(catId)).toEqual(['c', 'a', 'b'])
    store.reorderChunk(a.ok ? a.chunk.id : '', '', 0) // across categories
    expect(names('')).toEqual(['a', 'loose'])
    expect(names(catId)).toEqual(['c', 'b'])
    store.toggleCollapsed(catId)
    expect(store.collapsed.value).toEqual([catId])
    store.deleteCategory(catId)
    expect(store.collapsed.value).toEqual([])
    expect(store.categories.value).toHaveLength(0)
    expect(names('')).toEqual(['a', 'loose', 'c', 'b'])
  })

  it('exports and imports JSON: round-trip, merge by name, untrusted input', () => {
    const store = fresh()
    const cat = store.saveCategory(null, 'Looks', 'chunk-7')
    store.add({ name: 'Hair', content: 'silver hair', color: 'chunk-2', categoryId: cat.ok ? cat.category.id : '' })
    store.add({ name: 'Plain', content: 'smile' })
    const json = store.toJSON()
    expect(JSON.parse(json)).toMatchObject({ format: 'pulsar.chunks', version: 1 })
    expect(json).not.toContain('"id"')

    const other = fresh()
    other.add({ name: 'Plain', content: 'old content' })
    expect(other.fromJSON(json)).toMatchObject({ ok: true, added: 1, updated: 1, skipped: [] })
    expect(other.byName.value.get('Plain')!.content).toBe('smile')
    const hair = other.byName.value.get('Hair')!
    expect(hair.color).toBe('chunk-2')
    expect(other.categories.value.find(c => c.id === hair.categoryId)).toMatchObject({ name: 'Looks', color: 'chunk-7' })
    expect(other.toJSON()).toBe(json)

    // hostile or broken files: nothing invalid gets in
    expect(other.fromJSON('not json')).toMatchObject({ ok: false })
    expect(other.fromJSON('{"format":"other"}')).toMatchObject({ ok: false })
    const res = other.fromJSON(JSON.stringify({ format: 'pulsar.chunks', version: 1, chunks: [
      { name: 'bad,name', content: 'x' }, { name: 'Huge', content: 'x'.repeat(MAX_CHUNK_BYTES + 1) }, { name: 42 },
      { name: 'Tinted', content: 'x', color: 'javascript:alert(1)', category: 'New Cat' },
    ] }))
    expect(res).toMatchObject({ ok: true, added: 1, skipped: ['bad,name', 'Huge', '(unnamed)'] })
    expect(other.byName.value.get('Tinted')!.color).toBe('chunk-1')
    expect(other.categories.value.map(c => c.name)).toContain('New Cat')
  })

  it('reads a tampered store defensively', () => {
    const s = sanitizeChunkState({ chunks: [
      { id: '1', name: 'ok', content: 'x', color: 'url(evil)', categoryId: 'gone', order: 'x' },
      { id: '2', name: 'a!b', content: 'x' }, { id: 3, name: 'n', content: 'x' }, null,
    ], categories: 'nope' })
    expect(s.categories).toEqual([])
    expect(s.chunks).toHaveLength(1)
    expect(s.chunks[0]).toMatchObject({ name: 'ok', color: 'chunk-1', categoryId: '' })
    expect(sanitizeChunkState('garbage')).toEqual({ chunks: [], categories: [] })
  })

  it('a saved config carries its chunks; restore recreates missing ones and reports mismatches', () => {
    const store = fresh()
    store.add({ name: 'Hair', content: 'silver hair, !macro:Len!', color: 'chunk-6' })
    store.add({ name: 'Len', content: 'long hair' })
    store.add({ name: 'Unused', content: 'x' })
    const snap = store.snapshot(['1girl, !macro:Hair!', ''])
    expect(snap).toEqual([{ name: 'Hair', content: 'silver hair, !macro:Len!', color: 'chunk-6' }, { name: 'Len', content: 'long hair', color: 'chunk-1' }])
    expect(store.referenced(['1girl, !macro:Hair!'])).toEqual([{ name: 'Hair', content: 'silver hair, !macro:Len!' }, { name: 'Len', content: 'long hair' }])

    const other = fresh()
    other.add({ name: 'Len', content: 'short hair' }) // same name, edited since
    expect(other.restoreSnapshot(snap)).toEqual({ created: ['Hair'], mismatched: ['Len'] })
    expect(other.byName.value.get('Hair')).toMatchObject({ content: 'silver hair, !macro:Len!', color: 'chunk-6' })
    expect(other.byName.value.get('Len')!.content).toBe('short hair') // the local chunk wins
    expect(other.restoreSnapshot(undefined)).toEqual({ created: [], mismatched: [] })
  })

  it('inserts into the last-focused field, or reports there is none', () => {
    const store = fresh()
    expect(store.insertIntoActive('Hair')).toBe(false)
    const got: string[] = []
    const field = { insert: (t: string) => got.push(t) }
    store.setActiveField(field)
    expect(store.insertIntoActive('Hair')).toBe(true)
    expect(got).toEqual(['!macro:Hair!'])
    store.clearActiveField(field)
    expect(store.insertIntoActive('Hair')).toBe(false)
  })
})

describe('history restore (D8)', () => {
  const tag = (nai: string, chunk?: { name: string, hash: string }, source: PromptTag['source'] = 'user'): PromptTag =>
    ({ runs: [], text: nai, name: nai, source, nai, chunk })
  const X = { name: 'Look', hash: 'aaaa' }

  it('shows the token only when today\'s chunk still resolves to the stored tags', async () => {
    const tags = [tag('masterpiece', undefined, 'quality'), tag('1girl'), tag('silver hair', X), tag('1.05::smile ::', X), tag('outdoors')]
    const same = async () => ({ hash: 'aaaa', nais: ['silver hair', '1.05::smile ::'] })
    expect(await restoreUserTags(tags, same)).toEqual({ pieces: ['1girl', '!macro:Look!', 'outdoors'], restored: ['Look'] })
    // the chunk was edited (hash differs): the expanded tags are the record
    expect((await restoreUserTags(tags, async () => ({ hash: 'bbbb', nais: ['silver hair', '1.05::smile ::'] }))).pieces)
      .toEqual(['1girl', 'silver hair', '1.05::smile ::', 'outdoors'])
    // the chunk is gone, or core is unreachable
    expect((await restoreUserTags(tags, async () => null)).pieces).toEqual(['1girl', 'silver hair', '1.05::smile ::', 'outdoors'])
    expect((await restoreUserTags(tags, () => Promise.reject(new Error('offline')))).pieces).toEqual(['1girl', 'silver hair', '1.05::smile ::', 'outdoors'])
  })

  it('falls back when an outer weight or a dropped tag changed the run', async () => {
    const now = async () => ({ hash: 'aaaa', nais: ['silver hair', 'smile'] })
    // `{!macro:Look!}` stored weighted tags: same hash, different tags
    expect((await restoreUserTags([tag('1.05::silver hair ::', X), tag('1.05::smile ::', X)], now)).pieces).toEqual(['1.05::silver hair ::', '1.05::smile ::'])
    // one of the chunk's tags was deduplicated away
    expect((await restoreUserTags([tag('silver hair', X)], now)).pieces).toEqual(['silver hair'])
    // the same chunk twice in a row restores two tokens
    expect((await restoreUserTags([tag('silver hair', X), tag('smile', X), tag('silver hair', X), tag('smile', X)], now)).pieces)
      .toEqual(['!macro:Look!', '!macro:Look!'])
    // legacy rows without provenance are untouched
    expect((await restoreUserTags([tag('a'), tag('b')], now)).pieces).toEqual(['a', 'b'])
    expect((await restoreUserTags(undefined, now)).pieces).toEqual([])
  })
})
