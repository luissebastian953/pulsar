import type { Ref } from 'vue'
import type { TagLookupResult, TagSuggestion } from '~/types/api'

// Autocomplete + commit rules for the tag chip input (docs/frontend.md §1):
// debounced /tags search with a live-Danbooru fallback (art subset only),
// comma-commit for typed or pasted lists, backspace-pops, and Enter committing
// the raw token when no suggestion is highlighted.
export function useTagAutocomplete(model: Ref<string[]>, subset: () => 'all' | 'pose') {
  const api = useApi()
  const query = ref('')
  const suggestions = ref<TagSuggestion[]>([])
  const open = ref(false)

  const search = useDebounceFn(async (q: string) => {
    if (!q.trim()) {
      suggestions.value = []

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
  function normalize(s: string) {
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

    search(q)
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

  return { query, suggestions, open, add, remove, onKeydown }
}
