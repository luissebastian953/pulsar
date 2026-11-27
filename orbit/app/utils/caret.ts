// Where a textarea's caret is on screen, for anchoring a popup to it. A
// textarea exposes no caret geometry, so the text up to the caret is laid
// out in an off-screen copy with the same box and font, and a marker span
// is measured (the usual mirror technique; no dependency).
const COPIED = [
  'boxSizing', 'width', 'borderTopWidth', 'borderRightWidth', 'borderBottomWidth', 'borderLeftWidth',
  'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft',
  'fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'letterSpacing', 'lineHeight', 'textTransform',
  'wordSpacing', 'textIndent', 'tabSize',
] as const

export interface CaretPoint {
  /** viewport coordinates of the caret's left edge */
  left: number
  top: number
  /** line height at the caret */
  height: number
}

export function caretPoint(el: HTMLTextAreaElement | HTMLInputElement, pos: number): CaretPoint {
  const box = el.getBoundingClientRect()
  const style = getComputedStyle(el)
  const copy = document.createElement('div')
  for (const p of COPIED) copy.style[p] = style[p]

  copy.style.position = 'absolute'
  copy.style.visibility = 'hidden'
  copy.style.top = '0'
  copy.style.left = '-9999px'
  copy.style.whiteSpace = el instanceof HTMLTextAreaElement ? 'pre-wrap' : 'pre'
  copy.style.overflowWrap = 'break-word'
  copy.textContent = el.value.slice(0, pos)
  const marker = document.createElement('span')
  marker.textContent = el.value.slice(pos) || '.'
  copy.appendChild(marker)
  document.body.appendChild(copy)
  const height = Number.parseFloat(style.lineHeight) || Number.parseFloat(style.fontSize) * 1.4 || 18
  const point = {
    left: box.left + marker.offsetLeft - el.scrollLeft,
    top: box.top + marker.offsetTop - el.scrollTop,
    height,
  }
  copy.remove()

  return point
}
