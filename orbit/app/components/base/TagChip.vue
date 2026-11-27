<script setup lang="ts">
import { computed } from 'vue'
import type { TagCategory } from '~/types/api'
import type { ChunkColor } from '~/utils/chunks'
import { formatCount } from '~/utils/format'

export interface Props {
  name: string
  category: TagCategory
  count?: number
  index?: number
  removable?: boolean
  /** the prompt parser rejected this chip: red and bold */
  invalid?: boolean
  /** a prompt-chunk chip: painted in the chunk's palette colour, not a tag category */
  chunkColor?: ChunkColor
  /** hover text (a chunk chip shows what it expands to) */
  title?: string
  /** emphasis highlight: the chip's weight tints its fill (0..1 strength) */
  emphasis?: { kind: 'up' | 'down', intensity: number }
}

export interface Emits {
  remove: []
}

const props = withDefaults(defineProps<Props>(), {
  count: 0, index: 0, removable: false, invalid: false, chunkColor: undefined, title: undefined, emphasis: undefined,
})

const emit = defineEmits<Emits>()

// a chunk's name is shown as typed; a tag's underscores read as spaces
const display = computed(() => (props.chunkColor ? props.name : props.name.replaceAll('_', ' ')))
const countLabel = computed(() => formatCount(props.count))
</script>

<template>
  <span
    class="tag-chip font-mono"
    :class="chunkColor && 'chunk-color'"
    :data-cat="chunkColor ? undefined : category"
    :data-color="chunkColor"
    :data-chunk="chunkColor ? name : undefined"
    :data-invalid="invalid || undefined"
    :data-emph="emphasis && !chunkColor && !invalid ? emphasis.kind : undefined"
    :aria-invalid="invalid || undefined"
    :title="title"
    :style="{ 'animationDelay': `${index * 22}ms`, '--emph': emphasis?.intensity }"
  >
    <Icon
      v-if="chunkColor"
      name="lucide:puzzle"
      :size="10"
      class="flex-none"
      style="color: var(--chip)"
    />
    <span
      v-else
      class="size-1 flex-none rounded-full"
      style="background: var(--chip)"
    />
    <span
      class="text-[11.5px]"
      style="color: var(--chip)"
    >{{ display }}</span>
    <span
      v-if="countLabel"
      class="text-2xs opacity-50"
      style="color: var(--chip)"
    >{{ countLabel }}</span>
    <button
      v-if="removable"
      class="focus-ring -mr-0.5 flex cursor-pointer opacity-60 hover:opacity-100"
      style="color: var(--chip)"
      :aria-label="`remove ${display}`"
      @click.stop="emit('remove')"
    >
      <Icon
        name="lucide:x"
        :size="10"
      />
    </button>
  </span>
</template>
