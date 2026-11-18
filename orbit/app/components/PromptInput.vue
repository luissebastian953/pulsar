<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import BaseInput from '~/components/base/Input.vue'
import ChunkPicker from '~/components/ChunkPicker.vue'
import { useChunkPicker } from '~/composables/useChunkPicker'
import { useChunks, type ActiveField } from '~/composables/useChunks'
import { usePromptSettings } from '~/composables/usePromptSettings'
import type { ChunkSpan, PromptIssue, PromptMark } from '~/types/api'
import { caretPoint } from '~/utils/caret'
import { bakeSpans, spanAt } from '~/utils/chunks'
import { chunkRanges, emphasisRanges, type Range } from '~/utils/prompt-issues'

// A prompt textarea: BaseInput plus everything prompt-shaped
// (docs/frontend.md#prompt-chunks).
//   · highlights — parse errors, `!macro:Name!` tokens in their chunk's
//     colour, and (Settings → Highlight Emphasis) the weighting tint. All
//     three are located by core; nothing here reads prompt syntax.
//   · `@` opens the chunk picker; picking writes the token.
//   · the caret inside a token shows what it expands to; a double-click
//     bakes it into plain text (by core's span).
//   · it is the "last-focused field" a click in the chunk panel inserts into.
// Other attrs (placeholder, hint, class…) fall through to BaseInput.
export interface Props {
  errors?: PromptIssue[]
  /** resolved references core located in exactly this text */
  spans?: ChunkSpan[]
  /** typed weighting core located in exactly this text */
  marks?: PromptMark[]
  size?: 'small' | 'default' | 'large'
}

const props = withDefaults(defineProps<Props>(), { errors: () => [], spans: () => [], marks: () => [], size: 'default' })
const model = defineModel<string>({ default: '' })
defineOptions({ inheritAttrs: false })

const store = useChunks()
const settings = usePromptSettings()
const input = ref<InstanceType<typeof BaseInput> | null>(null)
const area = () => (input.value?.el ?? null) as HTMLTextAreaElement | null

const highlights = computed<Range[]>(() => [
  ...props.errors,
  ...chunkRanges(props.spans, store.colorOf),
  ...(settings.highlightEmphasis.value ? emphasisRanges(props.marks) : []),
])

// ── caret bookkeeping ───────────────────────────────────────────────
const caret = ref(0)
const focused = ref(false)
const at = ref({ left: 0, top: 0 })

function place() {
  const el = area()

  if (!el) return

  const p = caretPoint(el, Math.min(caret.value, el.value.length))
  at.value = { left: p.left, top: p.top + p.height }
}

function sync() {
  const el = area()

  if (!el) return

  caret.value = el.selectionStart ?? el.value.length
  picker.refresh()

  if (picker.open.value || tip.value) place()
}

// `refocus` is false for an insert from the chunk panel: focus stays there
function splice(start: number, end: number, text: string, refocus = true) {
  const el = area()
  model.value = model.value.slice(0, start) + text + model.value.slice(end)
  caret.value = start + text.length
  // after Vue writes the value back, restore the caret past the insertion
  requestAnimationFrame(() => {
    if (!el) return

    if (refocus) el.focus()

    el.setSelectionRange(caret.value, caret.value)
    picker.refresh()
  })
}

// ── the @ picker ────────────────────────────────────────────────────
const picker = useChunkPicker({
  text: () => area()?.value ?? model.value,
  caret: () => caret.value,
  pick: (start, end, token) => splice(start, end, token),
})

// ── a token under the caret: show its expansion; double-click bakes ──
const tip = computed(() => (focused.value && !picker.open.value ? spanAt(props.spans, caret.value) : null))

function onDblclick() {
  const el = area()
  const span = el ? spanAt(props.spans, el.selectionStart ?? 0) : null

  if (!span) return

  model.value = bakeSpans(model.value, [span])
  caret.value = span.start
}

// ── the panel inserts into the last-focused field ───────────────────
const self: ActiveField = {
  insert: (token) => {
    const at = Math.min(caret.value, model.value.length)
    splice(at, at, token, false)
  },
}

function onFocus() {
  focused.value = true
  store.setActiveField(self)
  sync()
}

function onBlur() {
  focused.value = false
  picker.onBlur()
}

function onKeydown(e: KeyboardEvent) {
  picker.onKeydown(e)
}

onBeforeUnmount(() => store.clearActiveField(self))
</script>

<template>
  <BaseInput
    ref="input"
    v-model="model"
    type="textarea"
    :size="size"
    :invalid="errors.length > 0"
    :highlights="highlights"
    data-test="prompt-input"
    v-bind="$attrs"
    @input="sync"
    @keyup="sync"
    @click="sync"
    @focus="onFocus"
    @blur="onBlur"
    @keydown="onKeydown"
    @dblclick="onDblclick"
  />
  <ChunkPicker
    :open="picker.open.value"
    :items="picker.items.value"
    :index="picker.index.value"
    :categories="store.categories.value"
    :at="at"
    @pick="picker.pick"
  />
  <Teleport to="body">
    <div
      v-if="tip"
      role="tooltip"
      data-test="chunk-tip"
      class="pointer-events-none fixed z-70 max-w-[min(400px,calc(100vw-20px))] whitespace-pre-wrap rounded-md border border-line bg-raised px-3 py-2 font-mono text-[11.5px] text-dim shadow-[0_12px_30px_rgba(0,0,0,.5)]"
      :style="{ left: `${Math.max(8, at.left)}px`, top: `${at.top + 4}px` }"
    >
      <span class="text-text">{{ tip.name }}</span> → {{ tip.text || '(empty)' }}
      <div class="mt-1 font-sans text-2xs text-faint">
        double-click to expand into text
      </div>
    </div>
  </Teleport>
</template>
