import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import BaseFold from '~/components/base/Fold.vue'
import BaseSegment from '~/components/base/Segment.vue'
import BaseSliderRow from '~/components/base/SliderRow.vue'
import BaseTagChip from '~/components/base/TagChip.vue'
import BaseToggleSwitch from '~/components/base/ToggleSwitch.vue'

describe('BaseSegment', () => {
  const options = [{ value: 'plain', label: 'plain language' }, { value: 'tags', label: 'tags' }]

  it('marks the active option and switches on click', async () => {
    const w = mount(BaseSegment, {
      props: { 'options': options, 'modelValue': 'plain', 'onUpdate:modelValue': (v: string | number) => w.setProps({ modelValue: v }) },
    })
    const [a, b] = w.findAll('button')
    expect(a!.attributes('class')).toContain('seg-active')
    expect(a!.attributes('aria-pressed')).toBe('true')
    await b!.trigger('click')
    expect(w.props('modelValue')).toBe('tags')
    expect(b!.attributes('class')).toContain('seg-active')
  })
})

describe('BaseToggleSwitch', () => {
  it('is a switch: aria-checked follows the model, click flips it', async () => {
    const w = mount(BaseToggleSwitch, {
      props: { 'modelValue': false, 'onUpdate:modelValue': (v: boolean) => w.setProps({ modelValue: v }) },
    })
    expect(w.attributes('role')).toBe('switch')
    expect(w.attributes('aria-checked')).toBe('false')
    await w.trigger('click')
    expect(w.props('modelValue')).toBe(true)
    expect(w.attributes('aria-checked')).toBe('true')
  })
})

describe('BaseFold', () => {
  it('collapsed by default, expands on header click, shows badge', async () => {
    const w = mount(BaseFold, { props: { label: 'Advanced', badge: 'v-pred' }, slots: { default: '<p>inner</p>' } })
    const body = () => w.get('p').element.parentElement!
    expect(w.get('button').attributes('aria-expanded')).toBe('false')
    expect(body().style.display).toBe('none') // v-show off
    expect(w.text()).toContain('v-pred')
    await w.get('button').trigger('click')
    expect(w.get('button').attributes('aria-expanded')).toBe('true')
    expect(body().style.display).not.toBe('none')
  })
})

describe('BaseTagChip', () => {
  it('displays underscores as spaces, formats the count, colours by category', () => {
    const w = mount(BaseTagChip, { props: { name: 'hatsune_miku', category: 'character', count: 1_300_000 } })
    expect(w.text()).toContain('hatsune miku')
    expect(w.text()).toContain('1.3M')
    expect(w.attributes('data-cat')).toBe('character')
    expect(w.find('button').exists()).toBe(false) // not removable by default
  })

  it('removable: renders the x and emits remove', async () => {
    const w = mount(BaseTagChip, { props: { name: 'solo', category: 'general', removable: true } })
    await w.get('button').trigger('click')
    expect(w.emitted('remove')).toHaveLength(1)
  })
})

describe('BaseSliderRow', () => {
  it('renders label, range bounds, and the decimal-formatted value', () => {
    const w = mount(BaseSliderRow, {
      props: { modelValue: 0.55, label: 'CFG rescale', min: 0, max: 1, step: 0.05, decimals: 2 },
    })
    expect(w.text()).toContain('CFG rescale')
    expect(w.text()).toContain('0.55')
    const input = w.get('input')
    expect(input.attributes('type')).toBe('range')
    expect(input.attributes('min')).toBe('0')
    expect(input.attributes('max')).toBe('1')
    expect(input.attributes('step')).toBe('0.05')
  })

  it('emits numbers, not strings', async () => {
    const w = mount(BaseSliderRow, { props: { modelValue: 10, label: 'Steps', min: 1, max: 50 } })
    await w.get('input').setValue('28')
    expect(w.emitted('update:modelValue')![0]).toEqual([28])
  })
})
