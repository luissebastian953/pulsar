<script setup lang="ts">
// The one text field. type="textarea" renders a textarea; everything else is
// a native input type. Caller class/style land on the wrapper so labels and
// hints move with the field; all other attrs (placeholder, maxlength,
// inputmode, rows…) fall through to the field itself.
//
// invalid: red outline + red bold label. highlights (textarea only): UTF-16
// ranges of the text to mark — drawn by a mirror layer behind a transparent
// textarea, so the caret and selection stay native.
import { computed, ref, useAttrs, type HTMLAttributes, type StyleValue } from 'vue'
import { segmentText, type Range } from '~/utils/prompt-issues'

export interface Props {
  label?: string
  hint?: string
  /** native input type, or 'textarea' */
  type?: 'text' | 'email' | 'password' | 'number' | 'search' | 'tel' | 'url' | 'textarea'
  variant?: 'default' | 'mono'
  size?: 'small' | 'default' | 'large'
  disabled?: boolean
  invalid?: boolean
  highlights?: Range[]
}

const props = withDefaults(defineProps<Props>(), {
  label: '',
  hint: '',
  type: 'text',
  variant: 'default',
  size: 'default',
  disabled: false,
  invalid: false,
  highlights: () => [],
})

defineOptions({ inheritAttrs: false })
const model = defineModel<string>({ default: '' })

const attrs = useAttrs() as Record<string, unknown>
const rootAttrs = computed(() => ({ class: attrs.class as HTMLAttributes['class'], style: attrs.style as StyleValue }))
const fieldAttrs = computed(() => {
  const { class: _class, style: _style, ...rest } = attrs
  return rest
})

const SIZE = {
  small: { field: 'px-[11px] py-[9px] text-[12.5px]', area: 'min-h-[60px]' },
  default: { field: 'px-3 py-2 text-[13px]', area: 'min-h-[78px]' },
  large: { field: 'px-3 py-2.5 text-sm', area: 'min-h-[92px]' },
} as const

const marking = computed(() => props.type === 'textarea' && props.highlights.length > 0 && !!model.value)
const segments = computed(() => (marking.value ? segmentText(model.value, props.highlights) : []))

const cls = computed(() => [
  'block w-full rounded-ctrl border text-text outline-none placeholder:text-faint',
  'disabled:cursor-not-allowed disabled:opacity-45',
  props.invalid ? 'border-danger focus:border-danger' : 'border-line focus:border-line-hi',
  marking.value ? 'relative bg-transparent' : 'bg-void',
  SIZE[props.size].field,
  props.type === 'textarea' && ['resize-y leading-relaxed', SIZE[props.size].area],
  props.variant === 'mono' && 'font-mono',
])

const mirrorCls = computed(() => [
  'pointer-events-none absolute inset-0 overflow-hidden whitespace-pre-wrap break-words rounded-ctrl border border-transparent bg-void text-transparent leading-relaxed',
  SIZE[props.size].field,
  props.variant === 'mono' && 'font-mono',
])

const el = ref<HTMLInputElement | HTMLTextAreaElement | null>(null)
const mirror = ref<HTMLDivElement | null>(null)
function syncScroll() {
  if (mirror.value && el.value) mirror.value.scrollTop = el.value.scrollTop
}
defineExpose({ focus: () => el.value?.focus() })
</script>

<template>
  <div v-bind="rootAttrs">
    <div
      v-if="label"
      class="mb-[5px] text-xs"
      :class="invalid ? 'font-bold text-danger' : 'text-dim'"
      data-test="input-label"
    >
      {{ label }}
    </div>
    <div
      v-if="type === 'textarea'"
      class="relative"
    >
      <div
        v-if="marking"
        ref="mirror"
        aria-hidden="true"
        data-test="input-mirror"
        :class="mirrorCls"
      >
        <span
          v-for="(seg, i) in segments"
          :key="i"
          :data-test="seg.mark ? 'input-mark' : undefined"
          :class="seg.mark && 'rounded-sm bg-danger/30 underline decoration-danger decoration-wavy'"
        >{{ seg.text }}</span>
      </div>
      <textarea
        ref="el"
        v-model="model"
        :disabled="disabled"
        :class="cls"
        :aria-invalid="invalid || undefined"
        v-bind="fieldAttrs"
        @scroll="syncScroll"
      />
    </div>
    <input
      v-else
      ref="el"
      v-model="model"
      :type="type"
      :disabled="disabled"
      :class="cls"
      :aria-invalid="invalid || undefined"
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
