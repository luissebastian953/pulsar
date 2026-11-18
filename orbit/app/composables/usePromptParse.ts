import type { ParsedTag, PromptIssue, PromptParseResult, PromptSurface } from '~/types/api'

// Live prompt validation: debounced POST /prompt/parse, the same parser and
// lineage calibration /generate uses — orbit has no second grammar
// (docs/frontend.md#prompt-validation). Stale answers are dropped; a
// network failure clears the marks (core still validates on generate).
export function usePromptParse(text: () => string, surface: PromptSurface, lineage: () => string) {
  const api = useApi()
  const errors = ref<PromptIssue[]>([])
  const notices = ref<PromptIssue[]>([])
  const tags = ref<ParsedTag[]>([])
  let seq = 0

  function clear() {
    errors.value = []
    notices.value = []
    tags.value = []
  }

  const run = useDebounceFn(async (t: string, l: string) => {
    const mySeq = ++seq
    if (!t.trim()) {
      clear()

      return
    }

    try {
      const res = await api.post<PromptParseResult>('/prompt/parse', { text: t, surface, lineage: l })
      if (mySeq !== seq) return

      errors.value = res.errors
      notices.value = res.notices
      tags.value = res.tags
    }
    catch {
      if (mySeq === seq) clear()
    }
  }, 250)

  watch([text, lineage], ([t, l]) => {
    run(t, l)
  }, { immediate: true })

  return { errors, notices, tags }
}
