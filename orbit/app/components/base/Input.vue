<script setup lang="ts">
// The one text field. type="textarea" renders a textarea; everything else is
// a native input type. Caller class/style land on the wrapper so labels and
// hints move with the field; all other attrs (placeholder, maxlength,
// inputmode, rows…) fall through to the field itself.
import { computed, ref, useAttrs } from 'vue'

export interface Props {
  label?: string
  hint?: string
  /** native input type, or 'textarea' */
  type?: 'text' | 'email' | 'password' | 'number' | 'search' | 'tel' | 'url' | 'textarea'
  variant?: 'default' | 'mono'
  size?: 'small' | 'default' | 'large'
  disabled?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  label: '',
  hint: '',
  type: 'text',
  variant: 'default',
  size: 'default',
  disabled: false,
})

defineOptions({ inheritAttrs: false })
const model = defineModel<string>({ default: '' })

const attrs = useAttrs() as Record<string, unknown>
const rootAttrs = computed(() => ({ class: attrs.class, style: attrs.style }))
const fieldAttrs = computed(() => {
  const { class: _class, style: _style, ...rest } = attrs
  return rest
})

const SIZE = {
  small: { field: 'px-[11px] py-[9px] text-[12.5px]', area: 'min-h-[60px]' },
  default: { field: 'px-3 py-2 text-[13px]', area: 'min-h-[78px]' },
  large: { field: 'px-3 py-2.5 text-sm', area: 'min-h-[92px]' },
} as const

const cls = computed(() => [
  'block w-full rounded-ctrl border border-line bg-void text-text outline-none placeholder:text-faint',
  'focus:border-line-hi disabled:cursor-not-allowed disabled:opacity-45',
  SIZE[props.size].field,
  props.type === 'textarea' && ['resize-y leading-relaxed', SIZE[props.size].area],
  props.variant === 'mono' && 'font-mono',
])

const el = ref<HTMLInputElement | HTMLTextAreaElement | null>(null)
defineExpose({ focus: () => el.value?.focus() })
</script>

<template>
  <div v-bind="rootAttrs">
    <div
      v-if="label"
      class="mb-[5px] text-xs text-dim"
    >
      {{ label }}
    </div>
    <textarea
      v-if="type === 'textarea'"
      ref="el"
      v-model="model"
      :disabled="disabled"
      :class="cls"
      v-bind="fieldAttrs"
    />
    <input
      v-else
      ref="el"
      v-model="model"
      :type="type"
      :disabled="disabled"
      :class="cls"
      v-bind="fieldAttrs"
    >
    <div
      v-if="hint"
      class="mt-1.5 text-[11.5px] leading-relaxed text-faint"
    >
      {{ hint }}
    </div>
  </div>
</template>
