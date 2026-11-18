<script setup lang="ts">
import { useToast } from '~/composables/useToast'

// Transient messages (useToast): bottom centre, above the mobile tab bar.
const { toasts, dismiss } = useToast()
</script>

<template>
  <div
    class="pointer-events-none fixed inset-x-0 bottom-16 z-70 flex flex-col items-center gap-2 px-4 md:bottom-6"
    aria-live="polite"
  >
    <button
      v-for="t in toasts"
      :key="t.id"
      data-test="toast"
      class="pop-anim pointer-events-auto max-w-[min(420px,100%)] cursor-pointer rounded-ctrl border bg-raised px-3.5 py-2 text-left text-[12.5px] leading-snug shadow-[0_12px_30px_rgba(0,0,0,.5)]"
      :class="t.tone === 'warn' ? 'border-warn/40 text-warn' : 'border-line text-text'"
      @click="dismiss(t.id)"
    >
      {{ t.text }}
    </button>
  </div>
</template>
