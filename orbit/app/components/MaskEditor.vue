<script setup lang="ts">
// paint the inpaint mask on the normalized input (white = repaint);
// export is a black/white PNG at the image's exact dims
export interface Props {
  src: string
  width: number
  height: number
}

export interface Emits {
  save: [File]
  close: []
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { overlay, brush, hasStrokes, start, move, end, clear, exportMask }
  = useMaskCanvas(() => props.width, () => props.height)

async function save() {
  const file = await exportMask()

  if (file) emit('save', file)
}
</script>

<template>
  <div
    class="fixed inset-0 z-70 flex items-center justify-center bg-void/85 p-4"
    @click.self="emit('close')"
  >
    <div class="flex max-h-full flex-col gap-3 rounded-canvas border border-line bg-surface p-4">
      <div class="flex items-center justify-between gap-4">
        <div class="section-label">
          Paint mask — red areas are repainted
        </div>
        <BaseButton
          size="small"
          variant="clear"
          icon="lucide:x"
          aria-label="close"
          @click="emit('close')"
        />
      </div>
      <div class="relative min-h-0 overflow-auto">
        <div
          class="relative mx-auto"
          style="max-height: 62vh; aspect-ratio: v-bind('`${width}/${height}`')"
        >
          <img
            :src="src"
            alt="input"
            class="h-full w-full select-none rounded-lg"
            draggable="false"
          >
          <canvas
            ref="overlay"
            :width="width"
            :height="height"
            class="absolute inset-0 h-full w-full cursor-crosshair touch-none rounded-lg"
            @pointerdown="start"
            @pointermove="move"
            @pointerup="end"
            @pointercancel="end"
          />
        </div>
      </div>
      <div class="flex items-center gap-3">
        <BaseSliderRow
          v-model="brush"
          label="Brush"
          :min="8"
          :max="160"
          class="flex-1"
        />
        <BaseButton
          size="small"
          variant="outline"
          @click="clear"
        >
          clear
        </BaseButton>
        <BaseButton
          size="small"
          type="primary"
          :disabled="!hasStrokes"
          @click="save"
        >
          Use mask
        </BaseButton>
      </div>
    </div>
  </div>
</template>
