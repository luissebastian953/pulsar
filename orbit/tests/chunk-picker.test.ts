import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PICKER_BLUR_MS, useChunkPicker } from '~/composables/useChunkPicker'
import { resetChunksForTest, useChunks } from '~/composables/useChunks'
import { pickerTrigger, rankChunks, type ChunkCategory, type PromptChunk } from '~/utils/chunks'

const at = (s: string) => pickerTrigger(s.replace('|', ''), s.indexOf('|'))

describe('the @ trigger (NovelAI\'s rule)', () => {
  it('opens at the start, after whitespace, after a comma, and in an empty field', () => {
    expect(at('@|')).toEqual({ start: 0, query: '' })
    expect(at('@sil|')).toEqual({ start: 0, query: 'sil' })
    expect(at('1girl, @sil|')).toEqual({ start: 7, query: 'sil' })
    expect(at('1girl,@sil|')).toEqual({ start: 6, query: 'sil' })
    expect(at('1girl\n@|')).toEqual({ start: 6, query: '' })
    expect(at('a girl with @|')).toEqual({ start: 12, query: '' })
    // spaces are part of the query, so names with spaces can be searched
    expect(at('x, @Silver Wo|')).toEqual({ start: 3, query: 'Silver Wo' })
    // the caret may sit before the end of the text
    expect(at('@sil|ver, tail')).toEqual({ start: 0, query: 'sil' })
  })

  it('a mid-word @ does nothing', () => {
    expect(at('user@example|')).toBeNull()
    expect(at('1girl, mail@|')).toBeNull()
    // the real tag `@_@`: its second @ is mid-word, so finishing it closes the picker
    expect(at('smile, @_@|')).toBeNull()
    expect(at('smile, @_@ and more|')).toBeNull()
  })

  it('a comma or a line break closes it; no @ means no picker', () => {
    expect(at('@sil,|')).toBeNull()
    expect(at('@sil, x|')).toBeNull()
    expect(at('@sil\nx|')).toBeNull()
    expect(at('plain text|')).toBeNull()
    expect(at('|@sil')).toBeNull() // the caret is before the @
  })
})

describe('ranking (NovelAI\'s tiers)', () => {
  const cats: ChunkCategory[] = [{ id: 'k', name: 'Characters', color: 'chunk-2', order: 0 }]
  const mk = (name: string, content: string, categoryId = ''): PromptChunk =>
    ({ id: name, name, content, color: 'chunk-1', categoryId, order: 0, createdAt: 0, updatedAt: 0 })
  const chunks = [
    mk('Wolf Pack', 'wolves'), // contains "sil"? no — content only
    mk('Basil', 'herb'), // name contains
    mk('Silver Wolf', 'grey hair', 'k'), // name starts with
    mk('Sunlit Isle', 'beach'), // subsequence s-i-l
    mk('Stelle', 'silver hair, trailblazer', 'k'), // content contains
    mk('Kafka', 'purple hair', 'k'), // no match
    mk('silk dress', 'white dress'), // starts with (case-insensitive)
  ]

  it('fills tiers in order, case-insensitively, without duplicates', () => {
    expect(rankChunks(chunks, cats, 'sil').map(c => c.name)).toEqual(['Silver Wolf', 'silk dress', 'Basil', 'Sunlit Isle', 'Stelle'])
    expect(rankChunks(chunks, cats, 'SIL').map(c => c.name)).toEqual(['Silver Wolf', 'silk dress', 'Basil', 'Sunlit Isle', 'Stelle'])
  })

  it('matches "Category: name" after the name tiers, then content', () => {
    // starts-with on the full label; `Stelle`/`Kafka` match through their category
    expect(rankChunks(chunks, cats, 'characters: s').map(c => c.name)).toEqual(['Silver Wolf', 'Stelle'])
    expect(rankChunks(chunks, cats, 'characters').map(c => c.name)).toEqual(['Silver Wolf', 'Stelle', 'Kafka'])
    expect(rankChunks(chunks, cats, 'uncategorized: b').map(c => c.name)).toEqual(['Basil'])
    // content only
    expect(rankChunks(chunks, cats, 'trailblazer').map(c => c.name)).toEqual(['Stelle'])
    expect(rankChunks(chunks, cats, 'zzz')).toEqual([])
    // an empty query lists everything, in store order
    expect(rankChunks(chunks, cats, '').map(c => c.name)).toEqual(chunks.map(c => c.name))
  })
})

describe('useChunkPicker', () => {
  let text = ''
  let caret = 0
  const picked: { start: number, end: number, token: string }[] = []
  const type = (s: string) => {
    text = s.replace('|', '')
    caret = s.includes('|') ? s.indexOf('|') : text.length
  }
  const key = (k: string) => {
    const e = new KeyboardEvent('keydown', { key: k, cancelable: true })

    return { e, handled: picker.onKeydown(e) }
  }
  let picker: ReturnType<typeof useChunkPicker>

  beforeEach(() => {
    localStorage.clear()
    resetChunksForTest()
    const store = useChunks()
    store.add({ name: 'Silver Wolf', content: 'grey hair' })
    store.add({ name: 'Silk', content: 'white dress' })
    store.add({ name: 'Kafka', content: 'purple hair' })
    picked.length = 0
    picker = useChunkPicker({
      text: () => text, caret: () => caret,
      pick: (start, end, token) => picked.push({ start, end, token }),
    })
  })

  afterEach(() => vi.useRealTimers())

  it('typing @sil then Enter inserts !macro:Silver Wolf!', () => {
    type('1girl, @sil|')
    picker.refresh()
    expect(picker.open.value).toBe(true)
    expect(picker.items.value.map(c => c.name)).toEqual(['Silver Wolf', 'Silk'])
    const { e, handled } = key('Enter')
    expect(handled).toBe(true)
    expect(e.defaultPrevented).toBe(true)
    // replaces the typed `@sil` (from the @ to the caret) with the token
    expect(picked).toEqual([{ start: 7, end: 11, token: '!macro:Silver Wolf!' }])
    expect(picker.open.value).toBe(false)
  })

  it('arrows move the selection (wrapping); Tab inserts it', () => {
    type('@si|')
    picker.refresh()
    expect(picker.index.value).toBe(0)
    key('ArrowDown')
    expect(picker.index.value).toBe(1)
    key('ArrowDown')
    expect(picker.index.value).toBe(0)
    key('ArrowUp')
    expect(picker.index.value).toBe(1)
    expect(key('Tab').handled).toBe(true)
    expect(picked[0]!.token).toBe('!macro:Silk!')
    // narrowing the query resets the selection
    type('@s|')
    picker.refresh()
    key('ArrowDown')
    type('@si|')
    picker.refresh()
    expect(picker.index.value).toBe(0)
  })

  it('Escape closes it until a new @; a comma closes it', () => {
    type('@sil|')
    picker.refresh()
    expect(key('Escape').handled).toBe(true)
    expect(picker.open.value).toBe(false)
    type('@silv|')
    picker.refresh()
    expect(picker.open.value).toBe(false) // same @, still dismissed
    type('@silv, @k|')
    picker.refresh()
    expect(picker.open.value).toBe(true) // a new @
    expect(picker.items.value.map(c => c.name)).toEqual(['Kafka', 'Silk'])
    type('@silv, @k,|')
    picker.refresh()
    expect(picker.open.value).toBe(false)
  })

  it('keys pass through when closed or when nothing matches', () => {
    type('plain|')
    picker.refresh()
    expect(key('Enter').handled).toBe(false)
    expect(key('ArrowDown').handled).toBe(false)
    type('@zzz|')
    picker.refresh()
    expect(picker.open.value).toBe(true) // open, showing "No prompt chunks found."
    expect(picker.items.value).toEqual([])
    const { e, handled } = key('Enter')
    expect(handled).toBe(false)
    expect(e.defaultPrevented).toBe(false)
    expect(picked).toEqual([])
  })

  it('closes 150 ms after blur, unless the field is used again first', () => {
    vi.useFakeTimers()
    type('@sil|')
    picker.refresh()
    picker.onBlur()
    vi.advanceTimersByTime(PICKER_BLUR_MS - 1)
    expect(picker.open.value).toBe(true)
    vi.advanceTimersByTime(1)
    expect(picker.open.value).toBe(false)
    expect(PICKER_BLUR_MS).toBe(150)

    picker.refresh()
    picker.onBlur()
    vi.advanceTimersByTime(100)
    picker.refresh() // focus came back
    vi.advanceTimersByTime(200)
    expect(picker.open.value).toBe(true)
  })

  it('a click picks too', () => {
    type('x, @k|')
    picker.refresh()
    picker.pick(picker.items.value[0]!)
    expect(picked).toEqual([{ start: 3, end: 5, token: '!macro:Kafka!' }])
  })
})
