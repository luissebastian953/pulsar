<script setup lang="ts">
export interface Props {
  label?: string
  cost?: number
  busy?: boolean
  icon?: string
}

export interface Emits {
  click: []
}

withDefaults(defineProps<Props>(), { label: 'Generate', cost: 0, busy: false, icon: 'lucide:sparkles' })

const emit = defineEmits<Emits>()
</script>

<template>
  <BaseButton
    type="primary-gradient"
    size="large"
    class="w-full"
    :icon="icon"
    :loading="busy"
    @click="emit('click')"
  >
    {{ busy ? 'Generating' : label }}
    <span
      v-if="cost"
      class="rounded-lg bg-white/20 px-[7px] py-0.5 font-mono text-[11.5px] font-medium"
    >{{ cost }}</span>
  </BaseButton>
</template>
