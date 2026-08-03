<script setup lang="ts">
export interface Props {
  label: string
  badge?: string
}

defineProps<Props>()
const open = defineModel<boolean>({ default: false })
</script>

<template>
  <div>
    <button
      class="focus-ring flex w-full cursor-pointer items-center gap-2 border-t border-line pb-2.5 pt-3 text-left"
      :aria-expanded="open"
      @click="open = !open"
    >
      <Icon
        name="lucide:chevron-right"
        :size="13"
        class="flex-none text-faint transition-transform duration-100"
        :class="open && 'rotate-90'"
      />
      <span class="section-label">{{ label }}</span>
      <span class="flex-1" />
      <span
        v-if="badge"
        class="font-mono text-[11px] text-dim"
      >{{ badge }}</span>
    </button>
    <div
      v-show="open"
      class="pb-3"
    >
      <slot />
    </div>
  </div>
</template>
