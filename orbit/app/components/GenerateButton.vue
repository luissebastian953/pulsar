<script setup lang="ts">
withDefaults(defineProps<{
  label?: string
  cost?: number
  busy?: boolean
  icon?: string
}>(), { label: 'Generate', cost: 0, busy: false, icon: 'lucide:sparkles' })

const emit = defineEmits<{ click: [] }>()
</script>

<template>
  <button
    class="gen-shadow focus-ring flex w-full cursor-pointer items-center justify-center gap-2.5 rounded-ctrl bg-accent py-3 text-sm font-semibold text-white"
    :class="busy && 'opacity-55'"
    :disabled="busy"
    @click="emit('click')"
  >
    <Icon :name="icon" :size="16" class="flex-none" />
    {{ busy ? 'Generating' : label }}
    <span v-if="cost" class="rounded-lg bg-white/20 px-[7px] py-0.5 font-mono text-[11.5px] font-medium">{{ cost }}</span>
  </button>
</template>
