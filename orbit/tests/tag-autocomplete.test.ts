import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { resetChunksForTest, useChunks } from '~/composables/useChunks'
import { TAG_SUGGESTIONS_KEY, resetPromptSettingsForTest, usePromptSettings } from '~/composables/usePromptSettings'
import { useTagAutocomplete } from '~/composables/useTagAutocomplete'
import { useTranslate } from '~/composables/useTranslate'
import { pickerTrigger } from '~/utils/chunks'

// core, mocked: every request is recorded so "no call was made" is checkable
const calls: string[] = []
const posts: { path: string, body: Record<string, unknown> }[] = []
const api = {
  get: vi.fn(async (path: string) => {
    calls.push(path)

    if (path.startsWith('/tags/lookup')) return { found: true, tag: { canonical: 'live_tag', category: 'general', count: 5, stage: 'live' } }

    const q = decodeURIComponent(/q=([^&]*)/.exec(path)?.[1] ?? '')

    return { tags: q.startsWith('silver') ? [{ canonical: 'silver_hair', category: 'general', count: 500000 }] : [] }
  }),
  post: vi.fn(async (path: string, body: Record<string, unknown>) => {
    posts.push({ path, body })

    return { tags: [], dropped: [], prompt: '' }
  }),
}

async function settle(ms = 500) {
  await vi.advanceTimersByTimeAsync(ms)
  await nextTick()
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('useApi', () => api)
  localStorage.clear()
  resetChunksForTest()
  resetPromptSettingsForTest()
  calls.length = 0
  posts.length = 0
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useTagAutocomplete', () => {
  it('searches /tags and falls back to the live lookup — the baseline', async () => {
    const model = ref<string[]>([])
    const ac = useTagAutocomplete(model, () => 'all')
    ac.query.value = 'silver'
    await settle()
    expect(calls).toEqual(['/tags?q=silver&subset=all'])
    expect(ac.suggestions.value.map(s => s.canonical)).toEqual(['silver_hair'])
    expect(ac.open.value).toBe(true)
    ac.query.value = 'unknownthing'
    await settle()
    expect(calls.slice(1)).toEqual(['/tags?q=unknownthing&subset=all', '/tags/lookup?name=unknownthing'])
  })

  it('makes zero /tags or /tags/lookup calls while the @ picker is active', async () => {
    useChunks().add({ name: 'Silver Wolf', content: 'grey hair' })
    const model = ref<string[]>([])
    const ac = useTagAutocomplete(model, () => 'all')
    for (const q of ['@', '@s', '@sil', '@silver wolf', '@nothing matches this']) {
      ac.query.value = q
      expect(pickerTrigger(q, q.length)).not.toBeNull()
      await settle()
    }

    expect(calls).toEqual([])
    expect(ac.suggestions.value).toEqual([])
    expect(ac.open.value).toBe(false)
    // an in-flight tag search is dropped once the box turns into a picker query
    ac.query.value = 'silver'
    await vi.advanceTimersByTimeAsync(100) // still inside the debounce
    ac.query.value = '@sil'
    await settle()
    expect(calls).toEqual([])
    // a mid-word @ is a tag, not a picker query: the search runs as usual
    ac.query.value = 'silver@x'
    await settle()
    expect(calls).toEqual(['/tags?q=silver%40x&subset=all'])
  })

  it('keeps chunk chips verbatim; normalises everything else as before', async () => {
    const model = ref<string[]>([])
    const ac = useTagAutocomplete(model, () => 'all')
    ac.add('!macro:Silver Wolf!') // what the picker inserts
    ac.commitMany(['Silver Hair', '  !macro:Outfit A!  ', '{Blue Eyes}', '1.2::Long Hair ::'])
    expect(model.value).toEqual(['!macro:Silver Wolf!', 'silver_hair', '!macro:Outfit A!', '{Blue Eyes}', '1.2::Long Hair ::'])
    // typed or pasted with commas: the same rule
    ac.query.value = 'Red Bow, !macro:Case Sensitive Name!, '
    await nextTick()
    expect(model.value.slice(-2)).toEqual(['red_bow', '!macro:Case Sensitive Name!'])
    expect(calls).toEqual([])
  })

  it('"Disable Tag Suggestions" silences the popup but not the commit keys', async () => {
    const settings = usePromptSettings()
    const model = ref<string[]>([])
    const ac = useTagAutocomplete(model, () => 'all')
    settings.tagSuggestions.value = false
    await nextTick()
    expect(localStorage.getItem(TAG_SUGGESTIONS_KEY)).toBe('false')

    for (const q of ['s', 'silver', 'unknownthing']) {
      ac.query.value = q
      await settle()
    }

    expect(calls).toEqual([]) // no /tags, no /tags/lookup
    expect(ac.suggestions.value).toEqual([])
    expect(ac.open.value).toBe(false)

    // comma-commit
    ac.query.value = 'silver hair, long'
    await nextTick()
    expect(model.value).toEqual(['silver_hair'])
    expect(ac.query.value).toBe(' long')
    // Enter-commit
    const enter = new KeyboardEvent('keydown', { key: 'Enter', cancelable: true })
    ac.onKeydown(enter)
    expect(enter.defaultPrevented).toBe(true)
    expect(model.value).toEqual(['silver_hair', 'long'])
    // backspace-pop
    ac.query.value = ''
    await nextTick()
    ac.onKeydown(new KeyboardEvent('keydown', { key: 'Backspace' }))
    expect(model.value).toEqual(['silver_hair'])
    expect(calls).toEqual([])

    // the @ picker's trigger is independent of the setting
    expect(pickerTrigger('@sil', 4)).toEqual({ start: 0, query: 'sil' })

    // turning it back on restores the search
    model.value = []
    settings.tagSuggestions.value = true
    ac.query.value = 'silver'
    await settle()
    expect(calls).toEqual(['/tags?q=silver&subset=all'])
  })

  it('switching the setting off closes an open popup and drops a late answer', async () => {
    const settings = usePromptSettings()
    const model = ref<string[]>([])
    const ac = useTagAutocomplete(model, () => 'all')
    ac.query.value = 'silver'
    await settle()
    expect(ac.open.value).toBe(true)
    settings.tagSuggestions.value = false
    await nextTick()
    expect(ac.open.value).toBe(false)
    expect(ac.suggestions.value).toEqual([])
  })
})

describe('useTranslate with chunks and settings', () => {
  it('resolved-strip previews still run when tag suggestions are disabled', async () => {
    usePromptSettings().tagSuggestions.value = false
    const tr = useTranslate('all')
    await tr.resolveTags('silver hair, {long hair}')
    expect(calls).toEqual(['/tags?q=silver_hair', '/tags?q=long_hair'])
    expect(tr.tags.value.map(t => t.canonical)).toEqual(['silver_hair', 'long_hair'])
  })

  it('never looks a chunk chip up as a tag', async () => {
    const tr = useTranslate('all')
    await tr.resolveTags('1girl, !macro:Silver Wolf!, smile')
    expect(calls).toEqual(['/tags?q=1girl', '/tags?q=smile'])
  })

  it('a picker query being typed is never sent to /translate', async () => {
    const tr = useTranslate('all')
    tr.onInput('a girl with a ponytail, @silv', 'plain')
    await settle()
    expect(posts.map(p => p.body.text)).toEqual(['a girl with a ponytail, '])
    // a picked token is prose-side text again and goes along, with its chunk
    tr.onInput('a girl, !macro:X!', 'plain')
    await settle()
    expect(posts.at(-1)!.body.text).toBe('a girl, !macro:X!')
  })

  it('sends only the referenced chunks with /translate', async () => {
    const store = useChunks()
    store.add({ name: 'Hair', content: 'silver hair, !macro:Len!' })
    store.add({ name: 'Len', content: 'long hair' })
    store.add({ name: 'Unused', content: 'x' })
    const tr = useTranslate('all')
    await tr.resolvePlain('a girl, !macro:Hair!')
    expect(posts).toEqual([{ path: '/translate', body: {
      text: 'a girl, !macro:Hair!', subset: 'all',
      chunks: [{ name: 'Hair', content: 'silver hair, !macro:Len!' }, { name: 'Len', content: 'long hair' }],
    } }])
    await tr.resolvePlain('a girl')
    expect(posts[1]!.body.chunks).toEqual([])
  })
})
