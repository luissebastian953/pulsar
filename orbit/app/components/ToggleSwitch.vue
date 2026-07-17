<script setup lang="ts">
defineProps<{ title?: string }>()
const model = defineModel<boolean>({ required: true })
</script>

<template>
  <button
    role="switch"
    :aria-checked="model"
    :title="title"
    class="focus-ring relative h-[19px] w-[34px] flex-none cursor-pointer rounded-full transition-colors duration-100"
    :class="model ? 'bg-accent' : 'bg-line'"
    @click="model = !model"
  >
    <span
      class="absolute top-0.5 size-[15px] rounded-full transition-[left] duration-100"
      :class="model ? 'left-[17px] bg-white' : 'left-0.5 bg-faint'"
    />
  </button>
</template>
