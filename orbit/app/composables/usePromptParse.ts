import { useDebounceFn } from '@vueuse/core'
import { computed, ref, watch } from 'vue'
import { useChunks } from '~/composables/useChunks'
import type { ChunkSpan, ParsedTag, PromptIssue, PromptMark, PromptParseResult, PromptSurface } from '~/types/api'

// Live prompt validation: debounced POST /prompt/parse, the same parser and
// lineage calibration /generate uses — orbit has no second grammar
// (docs/frontend.md#prompt-validation). Stale answers are dropped; a
// network failure clears the marks (core still validates on generate).
//
// The request carries the prompt chunks the text references, so core
// expands `!macro:Name!` and reports where each resolved reference sits
// (`chunks`) and where the typed weighting is (`marks`). Both are offsets
// into the text that was parsed, so they are exposed only while the field
// still holds that text — a highlight never lands on shifted characters.
export function usePromptParse(text: () => string, surface: PromptSurface, lineage: () => string) {
  const api = useApi()
  const store = useChunks()
  const errors = ref<PromptIssue[]>([])
  const notices = ref<PromptIssue[]>([])
  const tags = ref<ParsedTag[]>([])
  const spans = ref<ChunkSpan[]>([])
  const marks = ref<PromptMark[]>([])
  const parsed = ref('') // the text the refs above describe
  let seq = 0

  function clear() {
    errors.value = []
    notices.value = []
    tags.value = []
    spans.value = []
    marks.value = []
    parsed.value = ''
  }

  // editing a referenced chunk re-validates the fields that use it
  const payload = computed(() => store.referenced([text()]))
  const payloadKey = computed(() => JSON.stringify(payload.value))

  const run = useDebounceFn(async (t: string, l: string) => {
    const mySeq = ++seq
    if (!t.trim()) {
      clear()

      return
    }

    try {
      const res = await api.post<PromptParseResult>('/prompt/parse', { text: t, surface, lineage: l, chunks: payload.value })
      if (mySeq !== seq) return

      errors.value = res.errors
      notices.value = res.notices
      tags.value = res.tags
      spans.value = res.chunks ?? []
      marks.value = res.marks ?? []
      parsed.value = t
    }
    catch {
      if (mySeq === seq) clear()
    }
  }, 250)

  watch([text, lineage, payloadKey], ([t, l]) => {
    run(t, l)
  }, { immediate: true })

  const fresh = computed(() => parsed.value === text())
  const chunks = computed(() => (fresh.value ? spans.value : []))
  const liveMarks = computed(() => (fresh.value ? marks.value : []))
  const liveTags = computed(() => (fresh.value ? tags.value : []))

  return { errors, notices, tags, chunks, marks: liveMarks, liveTags }
}
