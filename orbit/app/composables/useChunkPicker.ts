import { computed, ref } from 'vue'
import { useChunks } from '~/composables/useChunks'
import { chunkToken, orderedChunks, pickerTrigger, rankChunks, type PromptChunk } from '~/utils/chunks'

// The `@` chunk picker's behaviour, NovelAI's rules (docs/frontend.md
// #prompt-chunks): `@` opens it only at the start, after whitespace or after
// a comma; the query runs to the caret (spaces allowed, a comma closes it);
// ArrowUp/Down move, Enter or Tab inserts, Escape closes, and it closes
// 150 ms after blur. `@` is only an editing affordance: picking writes the
// `!macro:Name!` token (decision D3). It works whether or not tag
// suggestions are disabled (decision D12).
export interface PickerHost {
  text: () => string
  caret: () => number
  /** replace text[start, end) with the picked chunk's token */
  pick: (start: number, end: number, token: string, chunk: PromptChunk) => void
}

export const PICKER_BLUR_MS = 150

export function useChunkPicker(host: PickerHost) {
  const store = useChunks()
  const open = ref(false)
  const start = ref(0)
  const query = ref('')
  const index = ref(0)
  // Escape closes the picker for this `@`; it reopens on a new one
  let dismissed = -1
  let blurTimer: ReturnType<typeof setTimeout> | undefined

  const items = computed(() => (open.value ? rankChunks(orderedChunks(store.state.value), store.categories.value, query.value) : []))

  function close() {
    open.value = false
    query.value = ''
    index.value = 0
  }

  /** Re-read the text around the caret: call after every input or caret move. */
  function refresh() {
    clearTimeout(blurTimer)
    const t = pickerTrigger(host.text(), host.caret())

    if (!t) {
      dismissed = -1
      close()

      return
    }

    if (t.start === dismissed) return

    dismissed = -1

    if (!open.value || t.start !== start.value || t.query !== query.value) index.value = 0

    open.value = true
    start.value = t.start
    query.value = t.query
  }

  function pick(chunk: PromptChunk) {
    if (!open.value) return

    const at = start.value
    close()
    host.pick(at, host.caret(), chunkToken(chunk.name), chunk)
  }

  /** Returns true when the key was the picker's (the caller stops there). */
  function onKeydown(e: KeyboardEvent): boolean {
    if (!open.value) return false

    const n = items.value.length
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        if (!n) return false

        e.preventDefault()
        index.value = (index.value + (e.key === 'ArrowDown' ? 1 : n - 1)) % n

        return true
      case 'Enter':
      case 'Tab':
        if (!n) return false

        e.preventDefault()
        pick(items.value[index.value]!)

        return true
      case 'Escape':
        e.preventDefault()
        dismissed = start.value
        close()

        return true
    }

    return false
  }

  function onBlur() {
    clearTimeout(blurTimer)
    blurTimer = setTimeout(close, PICKER_BLUR_MS)
  }

  return { open, query, index, items, refresh, pick, onKeydown, onBlur, close }
}
