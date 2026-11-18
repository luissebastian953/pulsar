<script setup lang="ts">
import {
  ComboboxAnchor,
  ComboboxContent,
  ComboboxInput,
  ComboboxItem,
  ComboboxPortal,
  ComboboxRoot,
  ComboboxViewport,
} from 'reka-ui'
import { computed, onBeforeUnmount, ref } from 'vue'
import ChunkPicker from '~/components/ChunkPicker.vue'
import { useChunkPicker } from '~/composables/useChunkPicker'
import { useChunks, type ActiveField } from '~/composables/useChunks'
import { usePromptSettings } from '~/composables/usePromptSettings'
import { useTagAutocomplete } from '~/composables/useTagAutocomplete'
import type { ChunkSpan, ParsedTag } from '~/types/api'
import { bakeSpans, chipChunkName, emphasisIntensity } from '~/utils/chunks'
import { formatCount } from '~/utils/format'

// Reka combobox over core's /tags (docs/frontend.md §1); the search/commit
// rules live in useTagAutocomplete. invalid / invalidChips come from core's
// prompt parser (docs/frontend.md#prompt-validation): the border turns red
// and the offending chips red and bold.
//
// Prompt chunks (docs/frontend.md#prompt-chunks): `@` in the box opens the
// chunk picker instead of tag suggestions; picking adds a chunk chip — the
// `!macro:Name!` token, shown as the chunk's name in its colour. Hovering a
// chunk chip shows what it expands to; double-clicking bakes it into tag
// chips. With Highlight Emphasis on, a weighted chip is tinted by the
// weight core parsed.
export interface Props {
  subset?: 'all' | 'pose'
  invalid?: boolean
  /** indices into the model of chips an error overlaps */
  invalidChips?: number[]
  /** resolved references core located in `model.join(', ')` */
  spans?: ChunkSpan[]
  /** the tags core parsed from `model.join(', ')` (their `tag` is the chip index) */
  tags?: ParsedTag[]
}

const props = withDefaults(defineProps<Props>(), {
  subset: 'all', invalid: false, invalidChips: () => [], spans: () => [], tags: () => [],
})
const model = defineModel<string[]>({ required: true })

const { query, suggestions, open, add, remove, onKeydown: onTagKeydown, commitMany }
  = useTagAutocomplete(model, () => props.subset)

const store = useChunks()
const settings = usePromptSettings()

// ── the @ picker (the query box is the "text", its end the caret) ───
const box = ref<HTMLElement | null>(null)
const at = ref({ left: 0, top: 0 })
const picker = useChunkPicker({
  text: () => query.value,
  caret: () => query.value.length,
  pick: (start, _end, token) => {
    // anything typed before the `@` is committed as its own chip(s)
    commitMany(query.value.slice(0, start).split(','))
    add(token)
  },
})

function refreshPicker() {
  picker.refresh()
  const r = box.value?.getBoundingClientRect()

  if (r) at.value = { left: r.left, top: r.bottom }
}

function onKeydown(e: KeyboardEvent) {
  if (picker.onKeydown(e)) return

  onTagKeydown(e)
}

// ── chunk chips and emphasis ────────────────────────────────────────
const chips = computed(() => model.value.map((text, i) => {
  const chunk = chipChunkName(text)
  const span = chunk ? props.spans.find(s => s.tag === i && s.name === chunk) : undefined
  // the strongest weight any run of this chip carries
  const w = props.tags.filter(t => t.tag === i && !t.chunk).flatMap(t => t.runs.map(r => r.w))
    .reduce((best, x) => (Math.abs(x - 1) > Math.abs(best - 1) ? x : best), 1)
  const intensity = settings.highlightEmphasis.value && !chunk ? emphasisIntensity(w) : 0

  return {
    text,
    chunk,
    color: chunk ? store.colorOf(chunk) : undefined,
    title: chunk ? (span ? `${span.text}\n\nDouble-click to expand into tags` : (store.byName.value.get(chunk)?.content ?? 'Unknown chunk')) : undefined,
    span,
    emphasis: intensity > 0 ? { kind: w > 1 ? 'up' as const : 'down' as const, intensity } : undefined,
  }
}))

/** Bake a chunk chip into the tag chips it expands to, by core's span. */
function bake(span: ChunkSpan | undefined) {
  if (!span) return

  const text = bakeSpans(model.value.join(', '), [span])
  model.value = [...new Set(text.split(/[,\n]/).map(t => t.trim()).filter(Boolean))]
}

// ── the chunk panel inserts into the last-focused field ─────────────
const self: ActiveField = { insert: token => add(token) }
onBeforeUnmount(() => store.clearActiveField(self))
</script>

<template>
  <ComboboxRoot
    v-model:open="open"
    :ignore-filter="true"
    :reset-search-term-on-blur="false"
    @update:model-value="v => add(String(v))"
  >
    <ComboboxAnchor
      class="flex min-h-[92px] w-full flex-wrap content-start items-center gap-1.5 rounded-ctrl border bg-void p-2.5"
      :class="invalid ? 'border-danger' : 'border-line focus-within:border-line-hi'"
      :data-invalid="invalid || undefined"
      data-test="tag-input"
    >
      <BaseTagChip
        v-for="(c, i) in chips"
        :key="c.text"
        :name="c.chunk ?? c.text"
        category="general"
        :invalid="invalidChips.includes(i)"
        :chunk-color="c.color"
        :title="c.title"
        :emphasis="c.emphasis"
        removable
        @remove="remove(c.text)"
        @dblclick="bake(c.span)"
      />
      <span
        ref="box"
        class="flex min-w-24 flex-1"
      >
        <ComboboxInput
          v-model="query"
          placeholder="add tags… (@ for chunks)"
          class="min-w-0 flex-1 bg-transparent font-mono text-[13px] text-text outline-none placeholder:text-faint"
          @keydown="onKeydown"
          @input="refreshPicker"
          @keyup="refreshPicker"
          @focus="store.setActiveField(self)"
          @blur="picker.onBlur"
        />
      </span>
    </ComboboxAnchor>
    <ChunkPicker
      :open="picker.open.value"
      :items="picker.items.value"
      :index="picker.index.value"
      :categories="store.categories.value"
      :at="at"
      @pick="picker.pick"
    />
    <!-- no empty popover: it would cover the error/notice labels below -->
    <ComboboxPortal v-if="suggestions.length">
      <ComboboxContent
        position="popper"
        :side-offset="6"
        class="z-60 max-h-64 w-[var(--reka-combobox-trigger-width)] overflow-hidden rounded-ctrl border border-line bg-raised p-1.5 shadow-[0_12px_30px_rgba(0,0,0,.5)]"
      >
        <ComboboxViewport>
          <ComboboxItem
            v-for="s in suggestions"
            :key="s.canonical"
            :value="s.canonical"
            class="flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-2 outline-none data-highlighted:bg-line"
          >
            <span
              class="size-1 flex-none rounded-full"
              :class="{
                'bg-t-general': s.category === 'general',
                'bg-t-character': s.category === 'character',
                'bg-t-copyright': s.category === 'copyright',
                'bg-t-artist': s.category === 'artist',
                'bg-t-meta': s.category === 'meta',
              }"
            />
            <span class="flex-1 font-mono text-[12.5px] text-text">{{ s.canonical.replaceAll('_', ' ') }}</span>
            <span
              v-if="s.newer_than_model"
              class="rounded-full border border-warn/40 px-1.5 text-2xs text-warn"
              title="on Danbooru, but newer than this model's training data — may not render"
            >new</span>
            <span
              v-else-if="s.stage === 'live'"
              class="rounded-full border border-line-hi px-1.5 text-2xs text-dim"
              title="validated live against Danbooru"
            >live</span>
            <span class="font-mono text-2xs text-faint">{{ formatCount(s.count) || s.count }}</span>
          </ComboboxItem>
        </ComboboxViewport>
      </ComboboxContent>
    </ComboboxPortal>
  </ComboboxRoot>
</template>
