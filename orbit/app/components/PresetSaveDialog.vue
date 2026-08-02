<script setup lang="ts">
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'

const emit = defineEmits<{ save: [name: string] }>()
const open = defineModel<boolean>('open', { required: true })

const name = ref('')
const input = ref<HTMLInputElement | null>(null)

watch(open, (v) => {
  if (v) {
    name.value = ''
    nextTick(() => input.value?.focus())
  }
})

function submit() {
  const n = name.value.trim()
  if (!n) return
  emit('save', n)
  open.value = false
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="o-anim fixed inset-0 z-[60] bg-void/70" />
      <DialogContent
        class="pop-anim fixed left-1/2 top-1/2 z-[61] w-[320px] max-w-[90vw] -translate-x-1/2 -translate-y-1/2 rounded-canvas border border-line bg-raised p-5 shadow-[0_20px_50px_rgba(0,0,0,.6)] focus:outline-none"
        @keydown.enter.prevent="submit"
      >
        <DialogTitle class="text-[14px] font-semibold text-text">
          Save configuration
        </DialogTitle>
        <DialogDescription class="mt-1.5 text-[12.5px] leading-relaxed text-dim">
          Stores the entire composer — prompt, characters, references, images, and every setting.
        </DialogDescription>
        <input
          ref="input"
          v-model="name"
          placeholder="e.g. Firefly swimsuit"
          maxlength="60"
          class="mt-3 w-full rounded-ctrl border border-line bg-void px-3 py-2 text-[13px] text-text outline-none placeholder:text-faint focus:border-line-hi"
        >
        <div class="mt-4 flex justify-end gap-2">
          <button
            class="focus-ring cursor-pointer rounded-ctrl border border-line px-3.5 py-1.5 text-[12.5px] text-dim hover:border-line-hi hover:text-text"
            @click="open = false"
          >
            Cancel
          </button>
          <button
            class="focus-ring cursor-pointer rounded-ctrl bg-accent px-3.5 py-1.5 text-[12.5px] font-medium text-void hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-40"
            :disabled="!name.trim()"
            @click="submit"
          >
            Save
          </button>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
