import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import BaseButton from '~/components/base/Button.vue'

const cls = (w: ReturnType<typeof mount>) => w.attributes('class') ?? ''

describe('BaseButton', () => {
  it('defaults: native button, type="button", outline-less solid scheme, hoverable', () => {
    const w = mount(BaseButton, { slots: { default: 'Go' } })
    expect(w.element.tagName).toBe('BUTTON')
    expect(w.attributes('type')).toBe('button')
    expect(cls(w)).toContain('cursor-pointer')
    expect(cls(w)).toContain('hover:')
  })

  it('disabled: cursor-not-allowed, disabled attr, and NO hover class anywhere', () => {
    const w = mount(BaseButton, { props: { disabled: true }, slots: { default: 'Go' } })
    expect(cls(w)).toContain('cursor-not-allowed')
    expect(cls(w)).toContain('opacity-40')
    expect(cls(w)).not.toContain('cursor-pointer')
    expect(cls(w)).not.toContain('hover:')
    expect(w.attributes('disabled')).toBeDefined()
  })

  it('loading: behaves as disabled, spins a loader icon, sets aria-busy', () => {
    const w = mount(BaseButton, { props: { loading: true, icon: 'lucide:trash-2' }, slots: { default: 'Del' } })
    expect(cls(w)).toContain('cursor-not-allowed')
    expect(cls(w)).not.toContain('hover:')
    expect(w.attributes('aria-busy')).toBe('true')
    expect(w.attributes('disabled')).toBeDefined()
    const icon = w.find('[data-test="icon"]')
    expect(icon.attributes('data-icon')).toBe('lucide:loader-circle')
    expect(icon.attributes('class')).toContain('animate-spin')
  })

  it('schemes: primary solid, outline warn, clear warn hover, secondary tint', () => {
    expect(cls(mount(BaseButton, { props: { type: 'primary' }, slots: { default: 'x' } })))
      .toContain('bg-accent')
    const warn = cls(mount(BaseButton, { props: { type: 'warn', variant: 'outline' }, slots: { default: 'x' } }))
    expect(warn).toContain('text-warn')
    expect(warn).toContain('hover:border-warn/60')
    expect(cls(mount(BaseButton, { props: { type: 'warn', variant: 'clear' }, slots: { default: 'x' } })))
      .toContain('hover:text-t-artist')
    expect(cls(mount(BaseButton, { props: { type: 'secondary' }, slots: { default: 'x' } })))
      .toContain('seg-active')
  })

  it('primary-gradient: glow when enabled, no glow when disabled', () => {
    const on = mount(BaseButton, { props: { type: 'primary-gradient' }, slots: { default: 'Generate' } })
    expect(cls(on)).toContain('gen-shadow')
    const off = mount(BaseButton, { props: { type: 'primary-gradient', disabled: true }, slots: { default: 'Generate' } })
    expect(cls(off)).not.toContain('gen-shadow')
  })

  it('icon-only (no slot) renders square; with slot renders padded', () => {
    const sq = mount(BaseButton, { props: { icon: 'lucide:x' } })
    expect(cls(sq)).toContain('size-8')
    const padded = mount(BaseButton, { props: { icon: 'lucide:x' }, slots: { default: 'Close' } })
    expect(cls(padded)).toContain('px-3.5')
  })

  it('sizes map: small and large paddings and icon sizes', () => {
    expect(cls(mount(BaseButton, { props: { size: 'small' }, slots: { default: 'x' } }))).toContain('px-2.5')
    const lg = mount(BaseButton, { props: { size: 'large', icon: 'lucide:zap' }, slots: { default: 'x' } })
    expect(cls(lg)).toContain('py-3')
    expect(lg.find('[data-test="icon"]').attributes('data-size')).toBe('16')
  })

  it('iconRight places the icon after the slot content', () => {
    const w = mount(BaseButton, { props: { icon: 'lucide:arrow-right', iconRight: true }, slots: { default: 'Next' } })
    expect(w.element.lastElementChild?.getAttribute('data-icon')).toBe('lucide:arrow-right')
  })

  it('nativeType submit lands on the type attr', () => {
    const w = mount(BaseButton, { props: { nativeType: 'submit' }, slots: { default: 'Save' } })
    expect(w.attributes('type')).toBe('submit')
  })

  it('tag="a" disabled: aria-disabled + pointer-events-none, never a disabled attr', () => {
    const w = mount(BaseButton, { props: { tag: 'a', disabled: true }, slots: { default: 'link' } })
    expect(w.element.tagName).toBe('A')
    expect(w.attributes('aria-disabled')).toBe('true')
    expect(w.attributes('disabled')).toBeUndefined()
    expect(cls(w)).toContain('pointer-events-none')
    expect(w.attributes('type')).toBeUndefined()
  })

  it('emits click through attrs when enabled; native disabled swallows it', async () => {
    let clicks = 0
    const w = mount(BaseButton, { attrs: { onClick: () => clicks++ }, slots: { default: 'x' } })
    await w.trigger('click')
    expect(clicks).toBe(1)
  })
})
