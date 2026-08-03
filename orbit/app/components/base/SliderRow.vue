<script setup lang="ts">
import { computed } from 'vue'

export interface Props {
  label: string
  min: number
  max: number
  step?: number
  decimals?: number
}

const props = withDefaults(defineProps<Props>(), { step: 1, decimals: 0 })

const model = defineModel<number>({ required: true })
const display = computed(() => model.value.toFixed(props.decimals))
</script>

<template>
  <div>
    <div class="mb-[5px] flex items-center">
      <span class="flex-1 text-xs text-dim">{{ label }}</span>
      <span class="font-mono text-xs text-text">{{ display }}</span>
    </div>
    <input
      v-model.number="model"
      type="range"
      class="w-full"
      :min="min"
      :max="max"
      :step="step"
      :aria-label="label"
    >
  </div>
</template>
