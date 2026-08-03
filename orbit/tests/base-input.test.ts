import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import BaseInput from '~/components/base/Input.vue'

describe('BaseInput', () => {
  it('renders a text input by default and round-trips v-model', async () => {
    const w = mount(BaseInput, { props: { 'modelValue': '', 'onUpdate:modelValue': (v: string) => w.setProps({ modelValue: v }) } })
    const input = w.find('input')
    expect(input.exists()).toBe(true)
    expect(input.attributes('type')).toBe('text')
    await input.setValue('miku')
    expect(w.props('modelValue')).toBe('miku')
  })

  it('type="textarea" renders a textarea with resize and min-height', () => {
    const w = mount(BaseInput, { props: { type: 'textarea', size: 'large' } })
    const area = w.find('textarea')
    expect(area.exists()).toBe(true)
    expect(w.find('input').exists()).toBe(false)
    expect(area.attributes('class')).toContain('resize-y')
    expect(area.attributes('class')).toContain('min-h-[92px]')
  })

  it('label above, hint below, both optional', () => {
    const w = mount(BaseInput, { props: { label: 'Seed', hint: 'leave empty for random' } })
    expect(w.text()).toContain('Seed')
    expect(w.text()).toContain('leave empty for random')
    const bare = mount(BaseInput)
    expect(bare.text()).toBe('') // no label, no hint — just the field
    expect(bare.element.children).toHaveLength(1)
  })

  it('disabled: attr set and cursor-not-allowed class present on the field', () => {
    const w = mount(BaseInput, { props: { disabled: true } })
    const input = w.find('input')
    expect(input.attributes('disabled')).toBeDefined()
    expect(input.attributes('class')).toContain('disabled:cursor-not-allowed')
  })

  it('splits attrs: class stays on the wrapper, field attrs fall through', () => {
    const w = mount(BaseInput, { attrs: { class: 'mt-3', placeholder: 'random', maxlength: '60', inputmode: 'numeric' } })
    expect(w.attributes('class')).toContain('mt-3')
    const input = w.find('input')
    expect(input.attributes('class') ?? '').not.toContain('mt-3')
    expect(input.attributes('placeholder')).toBe('random')
    expect(input.attributes('maxlength')).toBe('60')
    expect(input.attributes('inputmode')).toBe('numeric')
  })

  it('variant="mono" adds the mono font to the field', () => {
    const w = mount(BaseInput, { props: { variant: 'mono' } })
    expect(w.find('input').attributes('class')).toContain('font-mono')
  })

  it('exposes focus()', () => {
    const w = mount(BaseInput, { attachTo: document.body })
    ;(w.vm as unknown as { focus: () => void }).focus()
    expect(document.activeElement).toBe(w.find('input').element)
    w.unmount()
  })
})
