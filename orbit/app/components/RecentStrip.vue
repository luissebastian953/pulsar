<script setup lang="ts">
export interface RecentItem {
  id: string
  imageUrl: string
  title: string
}

withDefaults(defineProps<{
  items: RecentItem[]
  orientation?: 'vertical' | 'horizontal'
}>(), { orientation: 'vertical' })

const emit = defineEmits<{ open: [item: RecentItem] }>()
</script>

<template>
  <div
    v-if="orientation === 'vertical'"
    class="flex w-[62px] flex-none flex-col items-center gap-2 overflow-y-auto border-l border-line bg-surface py-3"
  >
    <span class="section-label mb-0.5 [writing-mode:vertical-rl]">Recent</span>
    <button
      v-for="item in items"
      :key="item.id"
      class="focus-ring size-[46px] flex-none cursor-pointer overflow-hidden rounded-lg border border-line hover:border-line-hi"
      :title="item.title"
      @click="emit('open', item)"
    >
      <img :src="item.imageUrl" :alt="item.title" class="size-full object-cover">
    </button>
    <div v-if="!items.length" class="size-[46px] flex-none rounded-lg border border-dashed border-line" />
  </div>

  <div v-else class="flex items-center gap-2 overflow-x-auto px-4 pt-3">
    <span class="section-label flex-none">Recent</span>
    <button
      v-for="item in items"
      :key="item.id"
      class="focus-ring size-11 flex-none cursor-pointer overflow-hidden rounded-lg border border-line"
      :title="item.title"
      @click="emit('open', item)"
    >
      <img :src="item.imageUrl" :alt="item.title" class="size-full object-cover">
    </button>
    <template v-if="!items.length">
      <span v-for="i in 3" :key="i" class="size-11 flex-none rounded-lg border border-dashed border-line" />
    </template>
  </div>
</template>
