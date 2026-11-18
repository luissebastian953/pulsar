import { useLocalStorage } from '@vueuse/core'
import { effectScope } from 'vue'

// The chunk panel's Settings tab (decisions D12, D13). Both are per browser:
// pulsar has no account settings.
export const TAG_SUGGESTIONS_KEY = 'pulsar.settings.tagSuggestions'
export const HIGHLIGHT_EMPHASIS_KEY = 'pulsar.settings.highlightEmphasis'

function create() {
  return {
    /** false = "Disable Tag Suggestions": no tag autocomplete popup. The `@` chunk picker is unaffected. */
    tagSuggestions: useLocalStorage(TAG_SUGGESTIONS_KEY, true),
    /** "Highlight Emphasis": tint weighted text and operators in the editors. On by default. */
    highlightEmphasis: useLocalStorage(HIGHLIGHT_EMPHASIS_KEY, true),
  }
}

let shared: ReturnType<typeof create> | null = null

// detached scope: the first caller is a component, and the settings must
// keep persisting after it unmounts
export function usePromptSettings() {
  return (shared ??= effectScope(true).run(create)!)
}

export function resetPromptSettingsForTest() {
  shared = null
}
