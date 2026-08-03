<script setup lang="ts">
export interface Props {
  options: { value: string | number, label: string }[]
}

defineProps<Props>()
const model = defineModel<string | number>({ required: true })
</script>

<template>
  <div class="flex overflow-hidden rounded-ctrl border border-line">
    <button
      v-for="(o, i) in options"
      :key="o.value"
      class="focus-ring-inset flex-1 cursor-pointer px-3 py-1.5 text-center text-xs"
      :class="[i > 0 && 'border-l border-line', model === o.value ? 'seg-active' : 'text-dim hover:text-text']"
      :aria-pressed="model === o.value"
      @click="model = o.value"
    >
      {{ o.label }}
    </button>
  </div>
</template>
