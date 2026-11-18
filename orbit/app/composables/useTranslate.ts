import type { PromptIssue, TagCategory, TagSuggestion, TranslateResult, TranslatedTag } from '~/types/api'
import { plainTagName, promptErrorFrom } from '~/utils/prompt-issues'

// prose→tag translation for the resolved strip; drops are surfaced
// (docs/frontend.md §5).
export function useTranslate(subset: 'all' | 'pose' = 'all') {
  const api = useApi()
  const tags = ref<TranslatedTag[]>([])
  // the grounded tags in NovelAI syntax (weights kept) — what plain mode sends
  const nai = ref('')
  // a parse error in the prose itself (core parses before nltag)
  const issue = ref<PromptIssue | null>(null)
  const dropped = ref<string[]>([])
  const resolving = ref(false)
  const offline = ref(false) // 503 — no translation provider configured
  const failed = ref(false)
  let seq = 0

  async function resolvePlain(text: string) {
    const mySeq = ++seq
    if (!text.trim()) {
      tags.value = []
      nai.value = ''
      issue.value = null
      dropped.value = []
      resolving.value = false
      return
    }

    try {
      const res = await api.post<TranslateResult>('/translate', { text, subset })
      if (mySeq !== seq) return

      tags.value = res.tags
      nai.value = res.prompt ?? ''
      issue.value = null
      dropped.value = res.dropped
      offline.value = false
      failed.value = false
    }
    catch (e: unknown) {
      if (mySeq !== seq) return

      const status = e as { statusCode?: number, status?: number }
      issue.value = promptErrorFrom(e)
      if (status.statusCode === 503 || status.status === 503) offline.value = true
      else if (!issue.value) failed.value = true

      tags.value = []
      nai.value = ''
      dropped.value = []
    }
    finally {
      if (mySeq === seq) resolving.value = false
    }
  }

  async function resolveTags(text: string) {
    const mySeq = ++seq
    // preview only: weighting syntax is stripped for the lookup; what is
    // sent is the chips as typed, parsed by core
    // a negative weight moves the tag to Undesired Content (core relocates
    // it), so it isn't previewed as a positive tag
    const negative = /^\s*(-[\d.]*::|\(.+:\s*-[\d.]+\s*\)\s*$)/
    const tokens = [...new Set(text.split(',').filter(t => !negative.test(t)).map(t => plainTagName(t)).filter(Boolean))]
    const results = await Promise.all(tokens.map(async (tok): Promise<TranslatedTag> => {
      const norm = tok
      try {
        const res = await api.get<{ tags: TagSuggestion[] }>(`/tags?q=${encodeURIComponent(tok)}`)
        const hit = res.tags.find(s => s.canonical === norm)
        if (hit) return { canonical: hit.canonical, category: hit.category, count: hit.count, concept: tok, stage: 'exact' }
      }
      catch { /* neutral chip below */ }

      return { canonical: norm, category: 'general' as TagCategory, count: 0, concept: tok, stage: 'exact' }
    }))
    if (mySeq !== seq) return

    tags.value = results
    dropped.value = []
    resolving.value = false
    failed.value = false
  }

  const debouncedPlain = useDebounceFn(resolvePlain, 400)
  const debouncedTags = useDebounceFn(resolveTags, 400)

  function onInput(text: string, mode: 'plain' | 'tags' = 'plain') {
    resolving.value = true
    if (mode === 'tags') debouncedTags(text)
    else debouncedPlain(text)
  }

  const stat = computed(() => {
    if (resolving.value) return 'resolving…'

    const mapped = tags.value.length
    const total = mapped + dropped.value.length
    if (total === 0) return '—'

    return dropped.value.length
      ? `${mapped}/${total} mapped · ${dropped.value.length} dropped`
      : `${mapped}/${total} mapped`
  })

  return { tags, nai, issue, dropped, resolving, offline, failed, onInput, resolvePlain, resolveTags, stat }
}
