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
import { useTagAutocomplete } from '~/composables/useTagAutocomplete'
import { formatCount } from '~/utils/format'

// Reka combobox over core's /tags (docs/frontend.md §1); the search/commit
// rules live in useTagAutocomplete. invalid / invalidChips come from core's
// prompt parser (docs/frontend.md#prompt-validation): the border turns red
// and the offending chips red and bold.
export interface Props {
  subset?: 'all' | 'pose'
  invalid?: boolean
  /** indices into the model of chips an error overlaps */
  invalidChips?: number[]
}

const props = withDefaults(defineProps<Props>(), { subset: 'all', invalid: false, invalidChips: () => [] })
const model = defineModel<string[]>({ required: true })

const { query, suggestions, open, add, remove, onKeydown }
  = useTagAutocomplete(model, () => props.subset)
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
        v-for="(t, i) in model"
        :key="t"
        :name="t"
        category="general"
        :invalid="invalidChips.includes(i)"
        removable
        @remove="remove(t)"
      />
      <ComboboxInput
        v-model="query"
        placeholder="add tags…"
        class="min-w-24 flex-1 bg-transparent font-mono text-[13px] text-text outline-none placeholder:text-faint"
        @keydown="onKeydown"
      />
    </ComboboxAnchor>
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
