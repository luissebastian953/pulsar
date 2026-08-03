<script setup lang="ts">
import {
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'

export interface Emits {
  save: [name: string]
}

const emit = defineEmits<Emits>()
const open = defineModel<boolean>('open', { required: true })

const name = ref('')
const input = useTemplateRef<{ focus: () => void }>('input')

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
        <BaseInput
          ref="input"
          v-model="name"
          class="mt-3"
          placeholder="e.g. Firefly swimsuit"
          maxlength="60"
        />
        <div class="mt-4 flex justify-end gap-2">
          <BaseButton
            variant="outline"
            @click="open = false"
          >
            Cancel
          </BaseButton>
          <BaseButton
            type="primary"
            :disabled="!name.trim()"
            @click="submit"
          >
            Save
          </BaseButton>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
