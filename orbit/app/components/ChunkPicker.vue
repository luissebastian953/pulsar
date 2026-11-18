<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { ChunkCategory, PromptChunk } from '~/utils/chunks'

// The `@` picker's list (docs/frontend.md#prompt-chunks): a listbox drawn at
// the caret while focus stays in the editor — the editor owns the keys
// (useChunkPicker), this only shows the ranked chunks and takes a click.
// Each row: the chunk's colour dot, its name, its category in small caps,
// and a one-line preview of the content.
export interface Props {
  open: boolean
  items: PromptChunk[]
  index: number
  categories: ChunkCategory[]
  /** viewport point to hang the list from (the caret's bottom-left) */
  at: { left: number, top: number }
}

export interface Emits {
  pick: [chunk: PromptChunk]
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const categoryName = computed(() => new Map(props.categories.map(c => [c.id, c.name])))

// keep the list on screen: 320 wide, 280 tall at most
const style = computed(() => {
  const vw = typeof window === 'undefined' ? 1024 : window.innerWidth
  const vh = typeof window === 'undefined' ? 768 : window.innerHeight
  const left = Math.max(8, Math.min(props.at.left, vw - 328))
  const below = props.at.top + 288 <= vh

  return below
    ? { left: `${left}px`, top: `${props.at.top + 4}px` }
    : { left: `${left}px`, bottom: `${Math.max(8, vh - props.at.top + 24)}px` }
})

const list = ref<HTMLElement | null>(null)
watch(() => props.index, async () => {
  await nextTick()
  list.value?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' })
})
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      ref="list"
      role="listbox"
      aria-label="Prompt chunks"
      data-test="chunk-picker"
      class="pop-anim fixed z-70 max-h-[280px] w-max min-w-[200px] max-w-[320px] overflow-y-auto rounded-ctrl border border-line bg-raised p-1 shadow-[0_12px_30px_rgba(0,0,0,.5)]"
      :style="style"
      @mousedown.prevent
    >
      <div
        v-for="(c, i) in items"
        :key="c.id"
        role="option"
        :aria-selected="i === index"
        :data-color="c.color"
        data-test="chunk-option"
        class="chunk-color chunk-row cursor-pointer rounded-md px-2 py-1.5"
        @click="emit('pick', c)"
      >
        <div class="flex items-center gap-2">
          <span class="chunk-dot size-2 flex-none rounded-full" />
          <span class="min-w-0 flex-1 truncate text-[12.5px] text-text">{{ c.name }}</span>
          <span
            v-if="categoryName.get(c.categoryId)"
            class="flex-none text-2xs uppercase tracking-wider text-text opacity-40"
          >{{ categoryName.get(c.categoryId) }}</span>
        </div>
        <div class="mt-0.5 truncate pl-4 font-mono text-2xs text-faint">
          {{ c.content }}
        </div>
      </div>
      <div
        v-if="!items.length"
        data-test="chunk-picker-empty"
        class="px-2 py-1.5 text-[11.5px] text-faint"
      >
        No prompt chunks found.
      </div>
    </div>
  </Teleport>
</template>
