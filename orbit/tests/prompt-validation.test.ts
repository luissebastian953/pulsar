import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import BaseFold from '~/components/base/Fold.vue'
import BaseInput from '~/components/base/Input.vue'
import BaseSectionLabel from '~/components/base/SectionLabel.vue'
import BaseTagChip from '~/components/base/TagChip.vue'
import PromptIssues from '~/components/PromptIssues.vue'
import TagInput from '~/components/TagInput.vue'
import type { PromptIssue, PromptTag } from '~/types/api'
import {
  chipSpans, chipsWithIssues, plainTagName, promptErrorFrom, segmentText, tagLabel, userTagTexts,
} from '~/utils/prompt-issues'

// TagInput's autocomplete talks to core; the marking under test doesn't
vi.mock('~/composables/useTagAutocomplete', () => ({
  useTagAutocomplete: () => ({
    query: ref(''), suggestions: ref([]), open: ref(false),
    add: () => {}, remove: () => {}, onKeydown: () => {},
  }),
}))

const issue = (start: number, end: number, extra: Partial<PromptIssue> = {}): PromptIssue => ({
  code: 'stray_closer', message: '`}` has no matching `{`', hint: 'Remove it', start, end, tag: 0, ...extra,
})

describe('prompt-issues utils', () => {
  it('locates chips in the joined string core parsed', () => {
    expect(chipSpans(['1girl', 'red}', 'x'])).toEqual([{ start: 0, end: 5 }, { start: 7, end: 11 }, { start: 13, end: 14 }])
  })

  it('maps an issue span onto the chips it overlaps (UTF-16 offsets)', () => {
    // "1girl, 😀 red}" — the emoji is two UTF-16 units, so `}` sits at 13
    expect([...chipsWithIssues(['1girl', '😀 red}'], [issue(13, 14)])]).toEqual([1])
    // an unclosed opener in chip 0 marks chip 0 only
    expect([...chipsWithIssues(['{a', 'b', 'c'], [issue(0, 1)])]).toEqual([0])
    // a mismatched pair spanning chips marks both ends and between
    expect([...chipsWithIssues(['[a', 'b', 'c}'], [issue(0, 10)])].sort()).toEqual([0, 1, 2])
    expect(chipsWithIssues(['a', 'b'], []).size).toBe(0)
  })

  it('segments text for the highlight overlay', () => {
    expect(segmentText('a}, b', [issue(1, 2)])).toEqual([
      { text: 'a', mark: false }, { text: '}', mark: true }, { text: ', b', mark: false },
    ])
    // zero-width issues mark one character; out-of-range is clamped
    expect(segmentText('ab', [issue(1, 1)])).toEqual([{ text: 'a', mark: false }, { text: 'b', mark: true }])
    expect(segmentText('ab', [issue(5, 9)])).toEqual([{ text: 'ab', mark: false }])
    // overlapping ranges don't duplicate text
    expect(segmentText('abcdef', [issue(1, 4), issue(2, 5)]).map(s => s.text).join('')).toBe('abcdef')
  })

  it('reads core\'s located 400 and ignores other errors', () => {
    const body = { error: 'prompt: …', code: 'stray_closer', message: '`}` has no matching `{`', field: 'prompt', start: 10, end: 11, tag: 1 }
    expect(promptErrorFrom({ data: body })).toMatchObject({ field: 'prompt', start: 10, end: 11, tag: 1, code: 'stray_closer' })
    expect(promptErrorFrom({ data: { error: 'cfg must be 1..12' } })).toBeNull()
    expect(promptErrorFrom(new Error('network'))).toBeNull()
  })

  it('restores only user-sourced tags, in NovelAI syntax', () => {
    const tag = (text: string, source: PromptTag['source'], nai?: string): PromptTag => ({
      text, name: text, source, nai, runs: [{ text, w: 1, emitted: 1 }],
    })
    const stored = [tag('loli', 'safety'), tag('lowres', 'default_uc'), tag('blurry', 'user', 'blurry'),
      tag('bad hands', 'user', '1.2::bad hands ::'), tag('logo', 'style_negative')]
    expect(userTagTexts(stored)).toEqual(['blurry', '1.2::bad hands ::'])
    expect(userTagTexts(undefined)).toEqual([])
  })

  it('labels stored tags with their emitted weight', () => {
    expect(tagLabel({ text: 'smile', name: 'smile', source: 'user', runs: [{ text: 'smile', w: 1.05, emitted: 1.0497 }] })).toBe('smile ×1.0497')
    expect(tagLabel({ text: 'x', name: 'x', source: 'user', runs: [{ text: 'x', w: 1, emitted: 1 }] })).toBe('x')
  })

  it('strips weighting syntax for preview lookups only', () => {
    expect(plainTagName('1.2::blue hair ::')).toBe('blue_hair')
    expect(plainTagName('{{red eyes}}')).toBe('red_eyes')
    expect(plainTagName('(smile:1.2)')).toBe('smile')
    expect(plainTagName('makima \\(chainsaw man\\)')).toBe('makima_(chainsaw_man)')
  })
})

describe('error marking', () => {
  it('BaseTagChip: invalid chips are flagged (red + bold via the danger token)', () => {
    const w = mount(BaseTagChip, { props: { name: 'red}', category: 'general', invalid: true } })
    expect(w.attributes('data-invalid')).toBeDefined()
    expect(w.attributes('aria-invalid')).toBe('true')
    expect(mount(BaseTagChip, { props: { name: 'ok', category: 'general' } }).attributes('data-invalid')).toBeUndefined()
  })

  it('TagInput: red outline on the box and red bold on the offending chips', () => {
    const w = mount(TagInput, {
      props: { 'modelValue': ['1girl', 'red}', 'smile'], 'onUpdate:modelValue': () => {}, 'invalid': true, 'invalidChips': [1] },
      global: { components: { BaseTagChip } },
    })
    const box = w.find('[data-test="tag-input"]')
    expect(box.attributes('class')).toContain('border-danger')
    expect(box.attributes('class')).not.toContain('border-line')
    const chips = w.findAll('.tag-chip')
    expect(chips).toHaveLength(3)
    expect(chips.map(c => c.attributes('data-invalid') !== undefined)).toEqual([false, true, false])

    const clean = mount(TagInput, {
      props: { 'modelValue': ['1girl'], 'onUpdate:modelValue': () => {} },
      global: { components: { BaseTagChip } },
    })
    expect(clean.find('[data-test="tag-input"]').attributes('class')).toContain('border-line')
  })

  it('BaseInput: invalid textarea gets the red outline and a red bold label', () => {
    const w = mount(BaseInput, { props: { type: 'textarea', label: 'Undesired content', invalid: true, modelValue: 'x' } })
    expect(w.find('textarea').attributes('class')).toContain('border-danger')
    expect(w.find('textarea').attributes('aria-invalid')).toBe('true')
    const label = w.find('[data-test="input-label"]')
    expect(label.attributes('class')).toContain('text-danger')
    expect(label.attributes('class')).toContain('font-bold')
  })

  it('BaseInput: highlights mark the offending span in a mirror layer (UTF-16 aware)', () => {
    const text = '1girl, 😀 red}'
    const w = mount(BaseInput, { props: { type: 'textarea', modelValue: text, invalid: true, highlights: [issue(13, 14)] } })
    const marks = w.findAll('[data-test="input-mark"]')
    expect(marks).toHaveLength(1)
    expect(marks[0]!.text()).toBe('}')
    expect(marks[0]!.attributes('class')).toContain('bg-danger/30')
    // the mirror reproduces the text exactly, so the mark lines up
    expect(w.find('[data-test="input-mirror"]').text()).toBe(text)
    // no highlights → no mirror, opaque field as before
    const plain = mount(BaseInput, { props: { type: 'textarea', modelValue: text } })
    expect(plain.find('[data-test="input-mirror"]').exists()).toBe(false)
    expect(plain.find('textarea').attributes('class')).toContain('bg-void')
  })

  it('PromptIssues: error message (with hint) below the input, notices quieter', () => {
    const w = mount(PromptIssues, {
      props: {
        errors: [issue(1, 2)],
        notices: [{ code: 'relocated_to_uc', message: '`hat` (weight -1) moved to Undesired Content at weight 1', start: 0, end: 3, tag: 0 }],
      },
    })
    const err = w.find('[data-test="prompt-error"]')
    expect(err.attributes('role')).toBe('alert')
    expect(err.attributes('class')).toContain('text-danger')
    expect(err.text()).toContain('} has no matching {')
    expect(w.find('[data-test="prompt-hint"]').text()).toContain('Remove it')
    const note = w.find('[data-test="prompt-notice"]')
    expect(note.attributes('data-code')).toBe('relocated_to_uc')
    expect(note.attributes('class')).toContain('text-faint')
    expect(note.find('.font-mono').text()).toBe('hat')
    expect(mount(PromptIssues).html()).not.toContain('prompt-error')
  })

  it('labels turn red and bold: section label and fold header', () => {
    const label = mount(BaseSectionLabel, { props: { invalid: true }, slots: { default: 'Describe' } })
    expect(label.find('.section-label').attributes('data-invalid')).toBeDefined()
    expect(mount(BaseSectionLabel, { slots: { default: 'Describe' } }).find('.section-label').attributes('data-invalid')).toBeUndefined()
    const fold = mount(BaseFold, { props: { label: 'Undesired content', invalid: true } })
    expect(fold.find('.section-label').attributes('data-invalid')).toBeDefined()
  })
})
