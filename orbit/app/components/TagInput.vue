<script setup lang="ts">
import type { TagSuggestion } from '~/types/api'
import {
  ComboboxAnchor,
  ComboboxContent,
  ComboboxInput,
  ComboboxItem,
  ComboboxPortal,
  ComboboxRoot,
  ComboboxViewport,
} from 'reka-ui'

// The one component not to hand-roll (docs/frontend.md §1): ARIA combobox
// semantics, keyboard handling, portal positioning, and composition events
// come from Reka; the vocabulary comes from core's /tags.
const props = withDefaults(defineProps<{ subset?: 'all' | 'pose' }>(), { subset: 'all' })
const model = defineModel<string[]>({ required: true })

const api = useApi()
const query = ref('')
const suggestions = ref<TagSuggestion[]>([])
const open = ref(false)

const search = useDebounceFn(async (q: string) => {
  if (!q.trim()) {
    suggestions.value = []
    return
  }
  try {
    const res = await api.get<{ tags: TagSuggestion[] }>(
      `/tags?q=${encodeURIComponent(q)}&subset=${props.subset}`,
    )
    suggestions.value = res.tags.filter(s => !model.value.includes(s.canonical))
    open.value = suggestions.value.length > 0
  }
  catch {
    suggestions.value = []
  }
}, 150)

watch(query, q => search(q))

function add(canonical: string) {
  if (canonical && !model.value.includes(canonical)) {
    model.value = [...model.value, canonical]
  }
  query.value = ''
  suggestions.value = []
  open.value = false
}

function remove(canonical: string) {
  model.value = model.value.filter(t => t !== canonical)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Backspace' && !query.value && model.value.length) {
    model.value = model.value.slice(0, -1)
  }
}
</script>

<template>
  <ComboboxRoot
    v-model:open="open"
    :ignore-filter="true"
    :reset-search-term-on-blur="false"
    @update:model-value="v => add(String(v))"
  >
    <ComboboxAnchor
      class="flex min-h-[92px] w-full flex-wrap content-start items-center gap-1.5 rounded-ctrl border border-line bg-void p-2.5 focus-within:border-line-hi"
    >
      <TagChip
        v-for="t in model"
        :key="t"
        :name="t"
        category="general"
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
    <ComboboxPortal>
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
            <span class="font-mono text-2xs text-faint">{{ s.count >= 1e6 ? `${(s.count / 1e6).toFixed(1)}M` : s.count >= 1e3 ? `${Math.round(s.count / 1e3)}k` : s.count }}</span>
          </ComboboxItem>
        </ComboboxViewport>
      </ComboboxContent>
    </ComboboxPortal>
  </ComboboxRoot>
</template>
