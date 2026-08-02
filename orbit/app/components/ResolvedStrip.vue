<script setup lang="ts">
import type { TranslatedTag } from '~/types/api'

// the signature strip: chips + dropped concepts (docs/frontend.md §5)
withDefaults(defineProps<{
  tags: TranslatedTag[]
  dropped: string[]
  resolving?: boolean
  offline?: boolean
  failed?: boolean
  stat: string
  emptyHint?: string
}>(), {
  resolving: false,
  offline: false,
  failed: false,
  emptyHint: 'Type a description and it becomes Danbooru tags here — the exact ones sent to the model.',
})

// Collapsible so the strip doesn't push the rest of the composer down as the
// tag list grows; the choice persists across sessions. The stat stays in the
// header, so a summary is visible even when collapsed.
const collapsed = useLocalStorage('pulsar.resolved.collapsed', false)
</script>

<template>
  <div class="rounded-ctrl border border-line bg-void">
    <button
      class="focus-ring-inset flex w-full cursor-pointer items-center gap-2 px-3 py-[9px]"
      :class="!collapsed && 'border-b border-line'"
      :aria-expanded="!collapsed"
      @click="collapsed = !collapsed"
    >
      <span
        class="size-1.5 rounded-full bg-accent"
        :style="resolving ? { animation: 'blink .9s ease-in-out infinite' } : undefined"
      />
      <span class="section-label">Resolved</span>
      <span class="flex-1" />
      <span class="font-mono text-[10.5px] text-faint">{{ stat }}</span>
      <Icon :name="collapsed ? 'lucide:plus' : 'lucide:minus'" :size="13" class="text-faint" />
    </button>

    <div v-show="!collapsed">
      <div v-if="tags.length" class="flex flex-wrap gap-1.5 px-3 py-[11px]">
      <TagChip
        v-for="(t, i) in tags"
        :key="t.canonical"
        :name="t.canonical"
        :category="t.category"
        :count="t.count"
        :index="i"
      />
    </div>
    <div v-else-if="offline" class="px-3 py-3.5 text-xs leading-relaxed text-warn">
      Translation is offline — no language model is configured on the server.
      Switch to tags mode, or set a provider key in core.
    </div>
    <div v-else-if="failed" class="px-3 py-3.5 text-xs leading-relaxed text-warn">
      Translation failed — retrying on your next edit.
    </div>
    <div v-else class="px-3 py-3.5 text-xs leading-relaxed text-faint">
      {{ emptyHint }}
    </div>

    <div v-if="dropped.length" class="border-t border-dashed border-line-hi px-3 py-2.5">
      <div class="flex flex-wrap items-center gap-1.5">
        <span class="text-2xs font-semibold uppercase tracking-[.13em] text-warn">Dropped</span>
        <span v-for="d in dropped" :key="d" class="dropped-chip font-mono">{{ d }}</span>
      </div>
      <div class="mt-[7px] text-[11.5px] leading-relaxed text-dim">
        No tag for these — the model wouldn't know what to do with them, so
        they're left out rather than guessed at.
      </div>
      </div>
    </div>
  </div>
</template>
