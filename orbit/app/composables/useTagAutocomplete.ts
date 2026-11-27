import { useDebounceFn } from '@vueuse/core'
import { ref, watch, type Ref } from 'vue'
import { usePromptSettings } from '~/composables/usePromptSettings'
import type { TagLookupResult, TagSuggestion } from '~/types/api'
import { chipChunkName, pickerTrigger } from '~/utils/chunks'

// Autocomplete + commit rules for the tag chip input (docs/frontend.md §1):
// debounced /tags search with a live-Danbooru fallback (art subset only),
// comma-commit for typed or pasted lists, backspace-pops, and Enter committing
// the raw token when no suggestion is highlighted.
//
// Two gates sit in front of the search (docs/frontend.md#prompt-chunks):
//   · "Disable Tag Suggestions" (pulsar.settings.tagSuggestions = false)
//     silences the popup — no /tags, no live lookup — while the commit keys
//     keep working;
//   · while the `@` chunk picker's trigger is in the box, the text is a
//     chunk name, not a tag: it is never sent to /tags or Danbooru, whatever
//     the setting says.
export function useTagAutocomplete(model: Ref<string[]>, subset: () => 'all' | 'pose') {
  const api = useApi()
  const settings = usePromptSettings()
  const query = ref('')
  const suggestions = ref<TagSuggestion[]>([])
  const open = ref(false)

  /** the query is a chunk-picker query (`@…`), not a tag */
  const picking = (q: string) => pickerTrigger(q, q.length) !== null

  function hush() {
    suggestions.value = []
    open.value = false
  }

  const search = useDebounceFn(async (q: string) => {
    // the box moved on inside the debounce (cleared, committed, or turned
    // into an `@` picker query): this search is stale, send nothing
    if (q !== query.value) return

    if (!q.trim() || !settings.tagSuggestions.value || picking(q)) {
      hush()

      return
    }

    try {
      const res = await api.get<{ tags: TagSuggestion[] }>(
        `/tags?q=${encodeURIComponent(q)}&subset=${subset()}`,
      )
      let tags = res.tags.filter(s => !model.value.includes(s.canonical))

      // DB miss → live Danbooru lookup (art subset only)
      if (!tags.length && subset() === 'all' && q.trim().length >= 3) {
        const live = await api.get<TagLookupResult>(`/tags/lookup?name=${encodeURIComponent(q)}`)

        if (live.found && !model.value.includes(live.tag.canonical)) tags = [live.tag]
      }

      // the answer may arrive after the box moved on (cleared, `@`, disabled)
      if (query.value !== q || !settings.tagSuggestions.value) return

      suggestions.value = tags
      open.value = suggestions.value.length > 0
    }
    catch {
      suggestions.value = []
    }
  }, 150)

  // tag-name form: lowercase, trimmed, whitespace runs → single underscore
  // (matches core's tags.Normalize). A chip carrying weighting syntax
  // (`1.2::tag ::`, `{tag}`, `(tag:1.2)`) keeps its spaces: the space
  // before a closing `::` is load-bearing (core parses it, orbit doesn't)
  // A chunk chip (`!macro:Name!`) is kept verbatim: names are case-sensitive.
  function normalize(s: string) {
    if (chipChunkName(s)) return s.trim()

    if (/::|[{}[\]]|\(.*:\s*[-+]?[\d.]+\s*\)/.test(s)) return s.trim().split(/\s+/).join(' ')

    return s.trim().toLowerCase().split(/\s+/).join('_')
  }

  // commas commit tags — typed or pasted. Everything before the last comma
  // becomes chips (in ONE assignment, so a big paste doesn't lose segments to
  // reka's controlled-input reactivity); the trailing fragment stays in the
  // box for autocomplete.
  watch(query, (q) => {
    if (q.includes(',')) {
      const parts = q.split(',')
      const tail = parts.pop() ?? ''
      commitMany(parts)
      query.value = tail

      return
    }

    if (!settings.tagSuggestions.value || picking(q)) {
      hush()

      return
    }

    search(q)
  })

  // turning suggestions off closes an open popup at once, in every editor
  watch(settings.tagSuggestions, (on) => {
    if (!on) hush()
  })

  function commitMany(raws: string[]) {
    const existing = new Set(model.value)
    const add: string[] = []

    for (const raw of raws) {
      const name = normalize(raw)

      if (name && !existing.has(name)) {
        existing.add(name)
        add.push(name)
      }
    }

    if (add.length) model.value = [...model.value, ...add]
  }

  function add(canonical: string) {
    if (canonical && !model.value.includes(canonical)) {
      model.value = [...model.value, canonical]
    }

    query.value = ''
    suggestions.value = []
    open.value = false
  }

  function remove(canonical: string) {
    model.value = model.value.filter(t => t !== canonical)
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Backspace' && !query.value && model.value.length) {
      model.value = model.value.slice(0, -1)
    }

    // Enter commits the raw token when no suggestion is highlighted, so a
    // free-typed tag doesn't require an autocomplete hit
    if (e.key === 'Enter' && query.value.trim() && !document.querySelector('[role="option"][data-highlighted]')) {
      e.preventDefault()
      commitMany([query.value])
      query.value = ''
      open.value = false
    }
  }

  return { query, suggestions, open, add, remove, onKeydown, commitMany }
}
