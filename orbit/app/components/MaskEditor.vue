<script setup lang="ts">
// paint the inpaint mask on the normalized input (white = repaint);
// export is a black/white PNG at the image's exact dims
const props = defineProps<{ src: string, width: number, height: number }>()
const emit = defineEmits<{ save: [File], close: [] }>()

const overlay = ref<HTMLCanvasElement>()
const brush = ref(56)
const hasStrokes = ref(false)
let mask: HTMLCanvasElement
let drawing = false

onMounted(() => {
  mask = document.createElement('canvas')
  mask.width = props.width
  mask.height = props.height
})

function toNatural(e: PointerEvent): [number, number] {
  const rect = overlay.value!.getBoundingClientRect()
  return [
    (e.clientX - rect.left) * (props.width / rect.width),
    (e.clientY - rect.top) * (props.height / rect.height),
  ]
}

function start(e: PointerEvent) {
  drawing = true
  overlay.value!.setPointerCapture(e.pointerId)
  const ctx = mask.getContext('2d')!
  const [x, y] = toNatural(e)
  ctx.strokeStyle = '#fff'
  ctx.lineWidth = brush.value
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'
  ctx.beginPath()
  ctx.moveTo(x, y)
  move(e)
}

function move(e: PointerEvent) {
  if (!drawing) return
  const ctx = mask.getContext('2d')!
  const [x, y] = toNatural(e)
  ctx.lineTo(x, y)
  ctx.stroke()
  hasStrokes.value = true
  paintOverlay()
}

function end() {
  drawing = false
}

// mirror the mask as a red tint over the image
function paintOverlay() {
  const ctx = overlay.value!.getContext('2d')!
  ctx.clearRect(0, 0, props.width, props.height)
  ctx.drawImage(mask, 0, 0)
  ctx.globalCompositeOperation = 'source-in'
  ctx.fillStyle = 'rgba(255, 70, 70, 0.6)'
  ctx.fillRect(0, 0, props.width, props.height)
  ctx.globalCompositeOperation = 'source-over'
}

function clear() {
  mask.getContext('2d')!.clearRect(0, 0, props.width, props.height)
  overlay.value!.getContext('2d')!.clearRect(0, 0, props.width, props.height)
  hasStrokes.value = false
}

function save() {
  const out = document.createElement('canvas')
  out.width = props.width
  out.height = props.height
  const ctx = out.getContext('2d')!
  ctx.fillStyle = '#000'
  ctx.fillRect(0, 0, props.width, props.height)
  ctx.drawImage(mask, 0, 0)
  out.toBlob((blob) => {
    if (blob) emit('save', new File([blob], 'mask.png', { type: 'image/png' }))
  }, 'image/png')
}
</script>

<template>
  <div class="fixed inset-0 z-70 flex items-center justify-center bg-void/85 p-4" @click.self="emit('close')">
    <div class="flex max-h-full flex-col gap-3 rounded-canvas border border-line bg-surface p-4">
      <div class="flex items-center justify-between gap-4">
        <div class="section-label">
          Paint mask — red areas are repainted
        </div>
        <button class="focus-ring cursor-pointer text-faint hover:text-text" aria-label="close" @click="emit('close')">
          <Icon name="lucide:x" :size="16" />
        </button>
      </div>
      <div class="relative min-h-0 overflow-auto">
        <div class="relative mx-auto" style="max-height: 62vh; aspect-ratio: v-bind('`${width}/${height}`')">
          <img :src="src" alt="input" class="h-full w-full select-none rounded-lg" draggable="false">
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
        <SliderRow v-model="brush" label="Brush" :min="8" :max="160" class="flex-1" />
        <button class="focus-ring cursor-pointer rounded-ctrl border border-line px-3 py-1.5 text-[12px] text-dim hover:border-line-hi" @click="clear">
          clear
        </button>
        <button
          class="focus-ring cursor-pointer rounded-ctrl bg-accent px-4 py-1.5 text-[12px] text-white disabled:opacity-40"
          :disabled="!hasStrokes"
          @click="save"
        >
          Use mask
        </button>
      </div>
    </div>
  </div>
</template>
