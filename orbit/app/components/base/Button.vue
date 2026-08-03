<script setup lang="ts">
// The one button. Disabled/loading is unhoverable by construction: hover
// classes are simply absent from the class list, and cursor-not-allowed
// replaces cursor-pointer. Icon-only buttons (no default slot) render square.
import { computed, resolveComponent, useSlots } from 'vue'

export interface Props {
  /** colour scheme */
  type?: 'default' | 'primary' | 'primary-gradient' | 'secondary' | 'warn'
  /** fill: solid, bordered, borderless, or text link */
  variant?: 'default' | 'outline' | 'clear' | 'link'
  size?: 'small' | 'default' | 'large'
  /** icon name, e.g. 'lucide:trash-2' */
  icon?: string
  iconRight?: boolean
  loading?: boolean
  disabled?: boolean
  nativeType?: 'button' | 'submit' | 'reset'
  tag?: 'button' | 'a' | 'nuxt-link' | 'div'
}

const props = withDefaults(defineProps<Props>(), {
  type: 'default',
  variant: 'default',
  size: 'default',
  icon: '',
  iconRight: false,
  loading: false,
  disabled: false,
  nativeType: 'button',
  tag: 'button',
})

const slots = useSlots()
const iconOnly = computed(() => !slots.default)
const off = computed(() => props.disabled || props.loading)
// resolveComponent is only legal in setup scope, and `tag` never changes shape
const nuxtLink = props.tag === 'nuxt-link' ? resolveComponent('NuxtLink') : null
const el = computed(() => nuxtLink ?? props.tag)

const SIZE = {
  small: { pad: 'gap-1.5 px-2.5 py-1.5 text-[12px]', square: 'rounded-lg p-1', icon: 13 },
  default: { pad: 'gap-1.5 px-3.5 py-2 text-[12.5px]', square: 'size-8', icon: 15 },
  large: { pad: 'gap-2 px-4 py-3 text-sm font-semibold', square: 'size-9', icon: 16 },
} as const

// per variant/type: always-on classes + hover classes (enabled only)
const SCHEME: Record<string, { base: string, hover: string }> = {
  'default/default': { base: 'border border-line bg-raised text-text', hover: 'hover:border-line-hi' },
  'default/primary': { base: 'bg-accent font-medium text-white', hover: 'hover:brightness-110' },
  'default/primary-gradient': { base: 'bg-accent font-semibold text-white', hover: 'hover:brightness-105' },
  'default/secondary': { base: 'seg-active font-semibold', hover: '' },
  'default/warn': { base: 'bg-warn font-medium text-void', hover: 'hover:brightness-110' },
  'outline/default': { base: 'border border-line text-dim', hover: 'hover:border-line-hi hover:text-text' },
  'outline/primary': { base: 'border border-accent text-accent-soft', hover: 'hover:brightness-110' },
  'outline/secondary': { base: 'border border-line text-accent-soft', hover: 'hover:border-line-hi' },
  'outline/warn': { base: 'border border-line text-warn', hover: 'hover:border-warn/60 hover:bg-warn/10' },
  'clear/default': { base: 'text-faint', hover: 'hover:text-text' },
  'clear/primary': { base: 'text-accent-soft', hover: 'hover:text-text' },
  'clear/secondary': { base: 'text-accent-soft', hover: 'hover:text-text' },
  'clear/warn': { base: 'text-faint', hover: 'hover:text-t-artist' },
  'link/default': { base: 'text-t-general', hover: 'hover:text-text' },
}

const classes = computed(() => {
  const s = SIZE[props.size]
  const scheme = SCHEME[`${props.variant}/${props.type}`] ?? SCHEME[`${props.variant}/default`]!
  return [
    'focus-ring inline-flex items-center justify-center whitespace-nowrap transition-colors',
    props.variant === 'link' ? 'gap-1 text-[12.5px]' : (iconOnly.value ? s.square : s.pad),
    props.variant !== 'link' && !(iconOnly.value && props.size === 'small') && 'rounded-ctrl',
    scheme.base,
    props.type === 'primary-gradient' && !off.value && 'gen-shadow',
    off.value
      ? ['cursor-not-allowed opacity-40', props.tag !== 'button' && 'pointer-events-none']
      : ['cursor-pointer', scheme.hover],
  ]
})

const iconName = computed(() => props.loading ? 'lucide:loader-circle' : props.icon)
const iconSize = computed(() => SIZE[props.size].icon)
</script>

<template>
  <component
    :is="el"
    :class="classes"
    :type="tag === 'button' ? nativeType : undefined"
    :disabled="tag === 'button' ? off : undefined"
    :aria-disabled="tag !== 'button' && off ? 'true' : undefined"
    :aria-busy="loading || undefined"
  >
    <Icon
      v-if="iconName && !iconRight"
      :name="iconName"
      :size="iconSize"
      class="flex-none"
      :class="loading && 'animate-spin'"
    />
    <slot />
    <Icon
      v-if="iconName && iconRight"
      :name="iconName"
      :size="iconSize"
      class="flex-none"
      :class="loading && 'animate-spin'"
    />
  </component>
</template>
