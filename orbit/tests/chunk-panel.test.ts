import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import BaseInput from '~/components/base/Input.vue'
import BaseTagChip from '~/components/base/TagChip.vue'
import ChunkPanelBody from '~/components/ChunkPanelBody.vue'
import PromptInput from '~/components/PromptInput.vue'
import TagInput from '~/components/TagInput.vue'
import { resetChunksForTest, useChunks } from '~/composables/useChunks'
import { HIGHLIGHT_EMPHASIS_KEY, TAG_SUGGESTIONS_KEY, resetPromptSettingsForTest, usePromptSettings } from '~/composables/usePromptSettings'
import { useToast } from '~/composables/useToast'
import type { ChunkSpan, ParsedTag, PromptIssue, PromptMark } from '~/types/api'
import { chunkRanges, emphasisRanges, segmentText } from '~/utils/prompt-issues'

// core, mocked: /prompt/parse answers "no errors" unless a test says otherwise
const parse = vi.fn(async (_path: string, _body: Record<string, unknown>) => ({ tags: [], relocated: [], errors: [] as PromptIssue[], notices: [], chunks: [], marks: [], emitted: '' }))
const api = { get: vi.fn(async () => ({ tags: [] })), post: parse }

const issue = (start: number, end: number): PromptIssue => ({ code: 'stray_closer', message: '`}` has no matching `{`', start, end, tag: 0 })
const span = (name: string, start: number, end: number, text: string): ChunkSpan => ({ name, hash: 'h', start, end, tag: 0, text })

beforeEach(() => {
  vi.stubGlobal('useApi', () => api)
  localStorage.clear()
  resetChunksForTest()
  resetPromptSettingsForTest()
  useToast().toasts.value = []
  parse.mockClear()
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('typed highlight marks', () => {
  it('error wins over chunk, chunk over emphasis; plain text is unmarked', () => {
    // "{!macro:A!}, x}" — op `{`0-1, chunk 1-10, op `}`10-11, error on the last `}`
    const text = '{!macro:A!}, x}'
    const marks: PromptMark[] = [{ start: 0, end: 1, kind: 'op', w: 1.05 }, { start: 1, end: 10, kind: 'up', w: 1.05 }, { start: 10, end: 11, kind: 'op', w: 1 }]
    const segs = segmentText(text, [
      ...emphasisRanges(marks), ...chunkRanges([span('A', 1, 10, 'silver hair')], () => 'chunk-3'), issue(14, 15),
    ])
    expect(segs.map(s => [s.text, s.kind])).toEqual([
      ['{', 'op'], ['!macro:A!', 'chunk'], ['}', 'op'], [', x', undefined], ['}', 'error'],
    ])
    expect(segs[1]).toMatchObject({ color: 'chunk-3', title: 'silver hair', mark: false })
    expect(segs[0]!.intensity).toBe(0.5)
    expect(segs.map(s => s.text).join('')).toBe(text)
    // an error overlapping emphasis and a chunk hides neither end of the text
    const over = segmentText('ab{cd}', [{ start: 2, end: 6, kind: 'up', intensity: 0.4 }, issue(1, 4)])
    expect(over.map(s => [s.text, s.kind])).toEqual([['a', undefined], ['b{c', 'error'], ['d}', 'up']])
    // a weight within 0.01 of 1 paints nothing
    expect(emphasisRanges([{ start: 0, end: 3, kind: 'up', w: 1.005 }])).toEqual([])
    expect(emphasisRanges([{ start: 0, end: 3, kind: 'down', w: 0.5 }])[0]).toMatchObject({ kind: 'down', intensity: 0.4 })
  })

  it('BaseInput paints each kind with its own class, colour and intensity', () => {
    const w = mount(BaseInput, { props: { type: 'textarea', modelValue: '{a}, !macro:X!, b}', highlights: [
      { start: 0, end: 1, kind: 'op', intensity: 0.5 }, { start: 1, end: 2, kind: 'up', intensity: 0.22 },
      { start: 5, end: 14, kind: 'chunk', color: 'chunk-5' }, issue(17, 18),
    ] } })
    const up = w.get('[data-test="input-mark-up"]')
    expect(up.text()).toBe('a')
    expect(up.classes()).toContain('emph-mark')
    expect(up.attributes('style')).toContain('--emph: 0.22')
    expect(w.get('[data-test="input-mark-op"]').attributes('data-kind')).toBe('op')
    const chunk = w.get('[data-test="input-mark-chunk"]')
    expect(chunk.text()).toBe('!macro:X!')
    expect(chunk.classes()).toEqual(expect.arrayContaining(['chunk-color', 'chunk-mark']))
    expect(chunk.attributes('data-color')).toBe('chunk-5')
    const err = w.get('[data-test="input-mark"]')
    expect(err.text()).toBe('}')
    expect(err.classes()).toContain('decoration-wavy')
    // no inline colour anywhere: colours come from tokens via classes
    expect(w.html()).not.toMatch(/#[0-9a-f]{6}|rgb\(/i)
  })
})

describe('PromptInput', () => {
  const marks: PromptMark[] = [{ start: 0, end: 1, kind: 'op', w: 1.05 }, { start: 1, end: 6, kind: 'up', w: 1.05 }, { start: 6, end: 7, kind: 'op', w: 1 }]

  it('toggling Highlight Emphasis off removes emphasis but not error or chunk marks', async () => {
    useChunks().add({ name: 'X', content: 'silver hair', color: 'chunk-4' })
    const w = mount(PromptInput, { props: {
      modelValue: '{smile}, !macro:X!, b}', marks, spans: [span('X', 9, 18, 'silver hair')], errors: [issue(21, 22)],
    } })
    expect(usePromptSettings().highlightEmphasis.value).toBe(true) // on by default
    expect(w.findAll('[data-test="input-mark-up"]')).toHaveLength(1)
    expect(w.findAll('[data-test="input-mark-op"]')).toHaveLength(2)
    expect(w.get('[data-test="input-mark-chunk"]').attributes('data-color')).toBe('chunk-4')

    usePromptSettings().highlightEmphasis.value = false
    await nextTick()
    expect(localStorage.getItem(HIGHLIGHT_EMPHASIS_KEY)).toBe('false')
    expect(w.findAll('[data-test="input-mark-up"]')).toHaveLength(0)
    expect(w.findAll('[data-test="input-mark-op"]')).toHaveLength(0)
    expect(w.findAll('[data-test="input-mark"]')).toHaveLength(1) // the error stays
    expect(w.findAll('[data-test="input-mark-chunk"]')).toHaveLength(1) // so does the chunk

    usePromptSettings().highlightEmphasis.value = true
    await nextTick()
    expect(w.findAll('[data-test="input-mark-up"]')).toHaveLength(1)
  })

  it('@ opens the picker; Enter writes the token in place of the query', async () => {
    useChunks().add({ name: 'Silver Wolf', content: 'grey hair' })
    const w = mount(PromptInput, { props: { 'modelValue': '', 'onUpdate:modelValue': (v: string) => w.setProps({ modelValue: v }) }, attachTo: document.body })
    const area = w.get('textarea')
    await area.setValue('1girl, @sil')
    area.element.setSelectionRange(11, 11)
    await area.trigger('keyup')
    expect(document.querySelector('[data-test="chunk-picker"]')).not.toBeNull()
    expect([...document.querySelectorAll('[data-test="chunk-option"]')].map(o => o.textContent?.includes('Silver Wolf'))).toEqual([true])
    await area.trigger('keydown', { key: 'Enter' })
    expect(w.props('modelValue')).toBe('1girl, !macro:Silver Wolf!')
    expect(document.querySelector('[data-test="chunk-picker"]')).toBeNull()
    w.unmount()
  })

  it('a double-click inside a token bakes it by core\'s span', async () => {
    const w = mount(PromptInput, { props: {
      'modelValue': 'a, !macro:X!, b', 'spans': [span('X', 3, 12, 'silver hair, long hair')],
      'onUpdate:modelValue': (v: string) => w.setProps({ modelValue: v }),
    }, attachTo: document.body })
    const area = w.get('textarea')
    area.element.setSelectionRange(6, 6)
    await area.trigger('dblclick')
    expect(w.props('modelValue')).toBe('a, silver hair, long hair, b')
    // outside any token nothing happens
    await w.setProps({ modelValue: 'a, !macro:X!, b' })
    area.element.setSelectionRange(0, 0)
    await area.trigger('dblclick')
    expect(w.props('modelValue')).toBe('a, !macro:X!, b')
    w.unmount()
  })
})

describe('the chunk panel', () => {
  const mountPanel = () => mount(ChunkPanelBody, { attachTo: document.body })

  it('clicking a chunk with no field focused shows the hint toast', async () => {
    useChunks().add({ name: 'Hair', content: 'silver hair' })
    const panel = mountPanel()
    await panel.get('[data-test="chunk-chip"]').trigger('click')
    expect(useToast().toasts.value.map(t => t.text)).toEqual(['Click in a prompt field first, then click a chunk to insert it.'])
    panel.unmount()
  })

  it('clicking a chunk inserts its token at the last-focused field\'s caret', async () => {
    useChunks().add({ name: 'Hair', content: 'silver hair' })
    const field = mount(PromptInput, { props: { 'modelValue': '1girl, smile', 'onUpdate:modelValue': (v: string) => field.setProps({ modelValue: v }) }, attachTo: document.body })
    const panel = mountPanel()
    const area = field.get('textarea')
    area.element.setSelectionRange(7, 7) // before "smile"
    await area.trigger('focus')
    await area.trigger('blur') // focus moves to the panel; the caret is remembered
    await panel.get('[data-test="chunk-chip"]').trigger('click')
    expect(field.props('modelValue')).toBe('1girl, !macro:Hair!smile')
    expect(useToast().toasts.value).toEqual([])
    // the chip's tooltip: the content, then how to use it
    expect(panel.get('[data-test="chunk-chip"]').attributes('title')).toBe('silver hair\n\nClick to insert, drag to reorder')
    // once the field is gone there is nothing to insert into
    field.unmount()
    await panel.get('[data-test="chunk-chip"]').trigger('click')
    expect(useToast().toasts.value).toHaveLength(1)
    panel.unmount()
  })

  it('lists Uncategorized first, then categories; every control is a real button', async () => {
    const store = useChunks()
    const cat = store.saveCategory(null, 'Characters', 'chunk-2')
    store.add({ name: 'In Cat', content: 'x', categoryId: cat.ok ? cat.category.id : '' })
    store.add({ name: 'Loose', content: 'y' })
    const panel = mountPanel()
    expect(panel.findAll('[data-test="chunk-chip"]').map(c => c.text())).toEqual(['Loose', 'In Cat'])
    expect(panel.get('[data-test="chunk-category"]').text()).toContain('Characters')

    for (const el of panel.findAll('[data-test="chunk-chip"], [data-test="chunk-edit"], [data-test="chunk-add"], [data-test="chunk-add-category"], [data-test="chunk-delete-all"]')) {
      expect(el.element.tagName).toBe('BUTTON')
      expect(el.attributes('tabindex')).not.toBe('-1')
    }

    // Alt+→ reorders the focused chip from the keyboard
    store.add({ name: 'Second', content: 'z' })
    await nextTick()
    await panel.findAll('[data-test="chunk-chip"]')[0]!.trigger('keydown', { key: 'ArrowRight', altKey: true })
    expect(panel.findAll('[data-test="chunk-chip"]').map(c => c.text())).toEqual(['Second', 'Loose', 'In Cat'])
    panel.unmount()
  })

  it('the form validates through core and shows located errors; a clean chunk saves', async () => {
    const panel = mountPanel()
    await panel.get('[data-test="chunk-add"]').trigger('click')
    await panel.get('[data-test="chunk-name"]').setValue('Bad,Name')
    await panel.get('[data-test="chunk-content"]').setValue('silver hair}')
    await panel.get('[data-test="chunk-form"]').trigger('submit')
    await flushPromises()
    expect(panel.get('[data-test="chunk-form-error"]').text()).toContain('can\'t contain')
    expect(parse).not.toHaveBeenCalled() // an invalid name never reaches core

    await panel.get('[data-test="chunk-name"]').setValue('Hair')
    parse.mockImplementation(async (_p, body) => ({ tags: [], relocated: [], notices: [], chunks: [], marks: [], emitted: '',
      errors: body.surface === 'prompt' ? [issue(11, 12)] : [] }))
    await panel.get('[data-test="chunk-form"]').trigger('submit')
    await flushPromises()
    expect(parse.mock.calls.map(c => [c[0], c[1].surface, c[1].text])).toEqual([
      ['/prompt/parse', 'prompt', 'silver hair}'], ['/prompt/parse', 'negative', 'silver hair}'],
    ])
    // the chunk being saved is sent too, so core can catch a self-reference
    expect(parse.mock.calls[0]![1].chunks).toEqual([])
    expect(panel.get('[data-test="prompt-error"]').text()).toContain('has no matching')
    expect(panel.get('[data-test="input-mark"]').text()).toBe('}')
    expect(useChunks().chunks.value).toHaveLength(0)

    parse.mockImplementation(async () => ({ tags: [], relocated: [], errors: [], notices: [], chunks: [], marks: [], emitted: '' }))
    await panel.get('[data-test="chunk-content"]').setValue('silver hair, !macro:Hair!')
    await panel.get('[data-test="chunk-form"]').trigger('submit')
    await flushPromises()
    expect(parse.mock.calls.at(-1)![1].chunks).toEqual([{ name: 'Hair', content: 'silver hair, !macro:Hair!' }])
    expect(useChunks().chunks.value.map(c => c.name)).toEqual(['Hair'])
    expect(panel.find('[data-test="chunk-form"]').exists()).toBe(false) // back to the list
    panel.unmount()
  })

  it('the Settings tab holds the two toggles, each persisted per browser', async () => {
    localStorage.setItem('pulsar.chunks.tab', 'settings')
    const panel = mountPanel()
    await nextTick()
    const settings = usePromptSettings()
    const off = panel.get('[data-test="setting-disable-suggestions"]')
    const emph = panel.get('[data-test="setting-highlight-emphasis"]')
    expect(panel.findAll('[role="switch"]')).toHaveLength(2)
    expect(off.attributes('aria-checked')).toBe('false') // suggestions on by default
    expect(emph.attributes('aria-checked')).toBe('true') // highlight on by default

    await off.trigger('click')
    await emph.trigger('click')
    expect(settings.tagSuggestions.value).toBe(false)
    expect(settings.highlightEmphasis.value).toBe(false)
    expect(localStorage.getItem(TAG_SUGGESTIONS_KEY)).toBe('false')
    expect(localStorage.getItem(HIGHLIGHT_EMPHASIS_KEY)).toBe('false')
    expect(off.attributes('aria-checked')).toBe('true')
    panel.unmount()
  })
})

describe('shared stores outlive the component that created them', () => {
  it('chunks and settings keep persisting after their first user unmounts', async () => {
    // the first caller is a component: the store must not die with it
    const first = mount(PromptInput, { props: { modelValue: '' } })
    const settings = usePromptSettings()
    const store = useChunks()
    first.unmount()
    store.add({ name: 'After', content: 'x' })
    settings.highlightEmphasis.value = false
    settings.tagSuggestions.value = false
    await nextTick()
    expect(JSON.parse(localStorage.getItem('pulsar.chunks.v1')!).chunks.map((c: { name: string }) => c.name)).toEqual(['After'])
    expect(localStorage.getItem(HIGHLIGHT_EMPHASIS_KEY)).toBe('false')
    expect(localStorage.getItem(TAG_SUGGESTIONS_KEY)).toBe('false')
  })
})

describe('TagInput with chunks', () => {
  const tag = (i: number, w: number, chunk?: boolean): ParsedTag =>
    ({ text: 't', name: 't', nai: 't', start: 0, end: 1, tag: i, runs: [{ text: 't', w, emitted: w }], chunk: chunk ? { name: 'Hair', hash: 'h' } : undefined })
  const mountTags = (props: Record<string, unknown>) => mount(TagInput, { props, global: { components: { BaseTagChip } }, attachTo: document.body })

  it('renders a chunk chip by name in its palette colour; emphasis tints weighted chips', async () => {
    useChunks().add({ name: 'Hair', content: 'silver hair, long hair', color: 'chunk-6' })
    const w = mountTags({
      modelValue: ['1girl', '!macro:Hair!', '{smile}', '0.5::hat ::'],
      spans: [{ ...span('Hair', 7, 19, 'silver hair, long hair'), tag: 1 }],
      tags: [tag(0, 1), tag(1, 1, true), tag(1, 1, true), tag(2, 1.05), tag(3, 0.5)],
    })
    const chips = w.findAll('.tag-chip')
    expect(chips).toHaveLength(4)
    expect(chips[1]!.text()).toContain('Hair')
    expect(chips[1]!.text()).not.toContain('!macro')
    expect(chips[1]!.attributes('data-color')).toBe('chunk-6')
    expect(chips[1]!.attributes('data-chunk')).toBe('Hair')
    expect(chips[1]!.attributes('title')).toContain('silver hair, long hair')
    expect(chips[1]!.attributes('data-emph')).toBeUndefined() // a chunk is never tinted by weight
    expect(chips[0]!.attributes('data-emph')).toBeUndefined()
    expect(chips[2]!.attributes('data-emph')).toBe('up')
    expect(chips[3]!.attributes('data-emph')).toBe('down')
    expect(chips[3]!.attributes('style')).toContain('--emph: 0.4')

    usePromptSettings().highlightEmphasis.value = false
    await nextTick()
    expect(w.findAll('[data-emph]')).toHaveLength(0)
    expect(w.findAll('.tag-chip')[1]!.attributes('data-color')).toBe('chunk-6')
    w.unmount()
  })

  it('double-clicking a chunk chip bakes it into tag chips', async () => {
    const w = mountTags({
      'modelValue': ['1girl', '!macro:Hair!', 'smile'],
      'spans': [{ ...span('Hair', 7, 19, 'silver hair, {long hair}'), tag: 1 }],
      'onUpdate:modelValue': (v: string[]) => w.setProps({ modelValue: v }),
    })
    await w.findAll('.tag-chip')[1]!.trigger('dblclick')
    expect(w.props('modelValue')).toEqual(['1girl', 'silver hair', '{long hair}', 'smile'])
    w.unmount()
  })

  it('@ in the box opens the chunk picker and Enter adds a chunk chip — with suggestions disabled too', async () => {
    usePromptSettings().tagSuggestions.value = false
    useChunks().add({ name: 'Silver Wolf', content: 'grey hair' })
    const w = mountTags({ 'modelValue': ['1girl'], 'onUpdate:modelValue': (v: string[]) => w.setProps({ modelValue: v }) })
    const input = w.get('input')
    await input.setValue('@sil')
    await input.trigger('keyup')
    expect(document.querySelector('[data-test="chunk-picker"]')).not.toBeNull()
    await input.trigger('keydown', { key: 'Enter' })
    expect(w.props('modelValue')).toEqual(['1girl', '!macro:Silver Wolf!'])
    expect(api.get).not.toHaveBeenCalled()
    w.unmount()
  })
})
