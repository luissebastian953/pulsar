<script setup lang="ts">
export interface Props {
  /** the input this labels has an error: red and bold */
  invalid?: boolean
}

withDefaults(defineProps<Props>(), { invalid: false })
</script>

<template>
  <div class="flex items-center gap-2.5">
    <span
      class="section-label"
      :data-invalid="invalid || undefined"
    ><slot /></span>
    <span class="h-px flex-1 bg-line" />
  </div>
</template>
