import { ref } from 'vue'

// A minimal transient message line (AppToasts renders it). For things that
// have no field to sit next to: "click in a prompt field first", an import
// summary, a restored chunk.
export interface Toast {
  id: number
  text: string
  tone: 'info' | 'warn'
}

const toasts = ref<Toast[]>([])
let seq = 0

export function useToast() {
  function show(text: string, tone: Toast['tone'] = 'info', ms = 4000) {
    const id = ++seq
    toasts.value = [...toasts.value, { id, text, tone }]
    setTimeout(() => dismiss(id), ms)

    return id
  }

  function dismiss(id: number) {
    toasts.value = toasts.value.filter(t => t.id !== id)
  }

  return { toasts, show, dismiss }
}
