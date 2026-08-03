<script setup lang="ts">
// The Recent strip's action pair: save the current composer config, and open
// the saved-configurations drawer (with a count badge). Rendered twice per
// composer page (mobile + desktop strips), so it lives in one place.
export interface Props {
  count: number
}

export interface Emits {
  save: []
  open: []
}

defineProps<Props>()
const emit = defineEmits<Emits>()
</script>

<template>
  <BaseButton
    icon="lucide:bookmark-plus"
    size="large"
    variant="outline"
    aria-label="Save configuration"
    title="Save current configuration"
    @click="emit('save')"
  />
  <span class="relative">
    <BaseButton
      icon="lucide:folder-open"
      size="large"
      variant="outline"
      aria-label="See configurations"
      title="See saved configurations"
      @click="emit('open')"
    />
    <span
      v-if="count"
      class="pointer-events-none absolute -right-1 -top-1 min-w-4 rounded-full bg-accent px-1 text-center font-mono text-[9px] leading-4 text-void"
    >{{ count }}</span>
  </span>
</template>
