<script setup lang="ts">
import type { TagLookupResult, TagSuggestion } from '~/types/api'
import {
  ComboboxAnchor,
  ComboboxContent,
  ComboboxInput,
  ComboboxItem,
  ComboboxPortal,
  ComboboxRoot,
  ComboboxViewport,
} from 'reka-ui'

// Reka combobox over core's /tags (docs/frontend.md §1)
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
    let tags = res.tags.filter(s => !model.value.includes(s.canonical))
    // DB miss → live Danbooru lookup (art subset only)
    if (!tags.length && props.subset === 'all' && q.trim().length >= 3) {
      const live = await api.get<TagLookupResult>(`/tags/lookup?name=${encodeURIComponent(q)}`)
      if (live.found && !model.value.includes(live.tag.canonical)) tags = [live.tag]
    }
    suggestions.value = tags
    open.value = suggestions.value.length > 0
  }
  catch {
    suggestions.value = []
  }
}, 150)

// tag-name form: lowercase, trimmed, whitespace runs → single underscore
// (matches core's tags.Normalize)
function normalize(s: string) {
  return s.trim().toLowerCase().split(/\s+/).join('_')
}

// commas commit tags — typed or pasted. Everything before the last comma
// becomes chips (in ONE assignment, so a big paste doesn't lose segments to
// reka's controlled-input reactivity); the trailing fragment stays in the
// box for autocomplete.
watch(query, (q) => {
  if (q.includes(',')) {
    const parts = q.split(',')
    const tail = parts.pop() ?? ''
    commitMany(parts)
    query.value = tail
    return
  }
  search(q)
})

function commitMany(raws: string[]) {
  const existing = new Set(model.value)
  const add: string[] = []
  for (const raw of raws) {
    const name = normalize(raw)
    if (name && !existing.has(name)) {
      existing.add(name)
      add.push(name)
    }
  }
  if (add.length) model.value = [...model.value, ...add]
}

function commit(raw: string) {
  commitMany([raw])
}

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
  // Enter commits the raw token when no suggestion is highlighted, so a
  // free-typed tag doesn't require an autocomplete hit
  if (e.key === 'Enter' && query.value.trim() && !document.querySelector('[role="option"][data-highlighted]')) {
    e.preventDefault()
    commit(query.value)
    query.value = ''
    open.value = false
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
            <span class="font-mono text-2xs text-faint">{{ s.count >= 1e6 ? `${(s.count / 1e6).toFixed(1)}M` : s.count >= 1e3 ? `${Math.round(s.count / 1e3)}k` : s.count }}</span>
          </ComboboxItem>
        </ComboboxViewport>
      </ComboboxContent>
    </ComboboxPortal>
  </ComboboxRoot>
</template>
