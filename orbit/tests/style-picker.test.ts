import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import StylePicker from '~/components/StylePicker.vue'
import type { StylePreset } from '~/types/api'

const base = { lineage: 'noobai' as const, prompt_prefix: '', prompt_suffix: '', negative: '', available: true }
const hsr: StylePreset = {
  ...base, slug: 'hsr', name: 'Honkai: Star Rail', family: 'mihoyo', description: 'miHoYo house style',
  lora: 'honkai star rail_v3.safetensors', lora_strength: 0.8,
}
const chibi: StylePreset = { ...base, slug: 'chibi', name: 'Chibi', family: 'anime', description: 'SD proportions' }
const missing: StylePreset = { ...hsr, slug: 'genshin', name: 'Genshin Impact', lora: 'genshin.safetensors', available: false }

function mountPicker(modelValue: string, styles = [hsr, chibi, missing]) {
  return mount(StylePicker, { props: { styles, 'modelValue': modelValue, 'onUpdate:modelValue': () => {} } })
}

describe('StylePicker', () => {
  it('renders None plus one card per style and marks the selection', () => {
    const w = mountPicker('hsr')
    expect(w.findAll('button')).toHaveLength(4)
    expect(w.find('[data-test="style-hsr"]').attributes('aria-pressed')).toBe('true')
    expect(w.find('[data-test="style-hsr"]').attributes('class')).toContain('pill-on')
    expect(w.find('[data-test="style-none"]').attributes('class')).not.toContain('pill-on')
    expect(w.find('[data-test="style-chibi"]').attributes('aria-pressed')).toBe('false')
  })

  it('emits the slug on click and an empty string for None', async () => {
    const w = mountPicker('')
    await w.find('[data-test="style-chibi"]').trigger('click')
    await w.find('[data-test="style-none"]').trigger('click')
    expect(w.emitted('update:modelValue')).toEqual([['chibi'], ['']])
  })

  it('disables a style whose LoRA is missing and says which one', () => {
    const w = mountPicker('')
    const card = w.find('[data-test="style-genshin"]')
    expect(card.attributes('disabled')).toBeDefined()
    expect(card.attributes('class')).toContain('cursor-not-allowed')
    expect(card.text()).toContain('LoRA missing · genshin')
    expect(card.attributes('title')).toContain('genshin.safetensors')
  })

  it('shows the LoRA and strength on an available style, nothing on a tag-only one', () => {
    const w = mountPicker('')
    expect(w.find('[data-test="style-hsr"]').text()).toContain('+ honkai star rail_v3 · 0.8')
    expect(w.find('[data-test="style-chibi"]').text()).not.toContain('+ ')
    expect(w.find('[data-test="style-chibi"]').text()).toContain('anime')
  })
})
