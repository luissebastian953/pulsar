// Inpaint-mask painting: strokes land on an offscreen mask canvas at the
// image's natural dims (white = repaint), mirrored onto the visible overlay
// as a red tint. Export is a black/white PNG at exactly width×height.
export function useMaskCanvas(width: () => number, height: () => number) {
  const overlay = ref<HTMLCanvasElement>()
  const brush = ref(56)
  const hasStrokes = ref(false)
  let mask: HTMLCanvasElement
  let drawing = false

  onMounted(() => {
    mask = document.createElement('canvas')
    mask.width = width()
    mask.height = height()
  })

  function toNatural(e: PointerEvent): [number, number] {
    const rect = overlay.value!.getBoundingClientRect()

    return [
      (e.clientX - rect.left) * (width() / rect.width),
      (e.clientY - rect.top) * (height() / rect.height),
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
    ctx.clearRect(0, 0, width(), height())
    ctx.drawImage(mask, 0, 0)
    ctx.globalCompositeOperation = 'source-in'
    ctx.fillStyle = 'rgba(255, 70, 70, 0.6)'
    ctx.fillRect(0, 0, width(), height())
    ctx.globalCompositeOperation = 'source-over'
  }

  function clear() {
    mask.getContext('2d')!.clearRect(0, 0, width(), height())
    overlay.value!.getContext('2d')!.clearRect(0, 0, width(), height())
    hasStrokes.value = false
  }

  function exportMask(): Promise<File | null> {
    const out = document.createElement('canvas')
    out.width = width()
    out.height = height()
    const ctx = out.getContext('2d')!
    ctx.fillStyle = '#000'
    ctx.fillRect(0, 0, width(), height())
    ctx.drawImage(mask, 0, 0)

    return new Promise((resolve) => {
      out.toBlob((blob) => {
        resolve(blob ? new File([blob], 'mask.png', { type: 'image/png' }) : null)
      }, 'image/png')
    })
  }

  return { overlay, brush, hasStrokes, start, move, end, clear, exportMask }
}
