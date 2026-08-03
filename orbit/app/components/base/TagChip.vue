<script setup lang="ts">
import { computed } from 'vue'
import type { TagCategory } from '~/types/api'
import { formatCount } from '~/utils/format'

export interface Props {
  name: string
  category: TagCategory
  count?: number
  index?: number
  removable?: boolean
}

export interface Emits {
  remove: []
}

const props = withDefaults(defineProps<Props>(), { count: 0, index: 0, removable: false })

const emit = defineEmits<Emits>()

const display = computed(() => props.name.replaceAll('_', ' '))
const countLabel = computed(() => formatCount(props.count))
</script>

<template>
  <span
    class="tag-chip font-mono"
    :data-cat="category"
    :style="{ animationDelay: `${index * 22}ms` }"
  >
    <span
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
