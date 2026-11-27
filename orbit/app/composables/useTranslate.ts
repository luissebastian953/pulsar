import { useDebounceFn } from '@vueuse/core'
import { computed, ref } from 'vue'
import { useChunks } from '~/composables/useChunks'
import type { PromptIssue, TagCategory, TagSuggestion, TranslateResult, TranslatedTag } from '~/types/api'
import { chipChunkName, pickerTrigger } from '~/utils/chunks'
import { plainTagName, promptErrorFrom } from '~/utils/prompt-issues'

// prose→tag translation for the resolved strip; drops are surfaced
// (docs/frontend.md §5).
export function useTranslate(subset: 'all' | 'pose' = 'all') {
  const api = useApi()
  const store = useChunks()
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
      // chunk text is tag text: core expands the references and keeps them
      // away from the LLM (docs/frontend.md#prompt-chunks)
      const res = await api.post<TranslateResult>('/translate', { text, subset, chunks: store.referenced([text]) })
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
    // a chunk chip is not a tag: it is never looked up (its name would go
    // to /tags). These per-chip previews run whether or not tag suggestions
    // are disabled — they are lookups, not suggestions.
    const tokens = [...new Set(text.split(',').filter(t => !negative.test(t) && !chipChunkName(t)).map(t => plainTagName(t)).filter(Boolean))]
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
    if (mode === 'tags') {
      debouncedTags(text)

      return
    }

    // an `@query` being typed at the end is a chunk-picker query, not prose:
    // it is never sent to /translate (and so never to the LLM)
    const picking = pickerTrigger(text, text.length)
    debouncedPlain(picking ? text.slice(0, picking.start) : text)
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
