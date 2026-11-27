import { useChunks, type ChunkParser } from '~/composables/useChunks'
import { paramsToPresetConfig, type PresetConfig } from '~/composables/usePresets'
import type { GenerationParams, PromptIssue, PromptParseResult, PromptSurface, PromptTag } from '~/types/api'
import { chunkToken, referencedChunks, restoreUserTags, type ChunkResolution } from '~/utils/chunks'

// The chunk store's calls into core. Orbit never expands or validates a
// chunk itself: POST /prompt/parse does both (docs/frontend.md#prompt-chunks).
export function useChunkApi() {
  const api = useApi()
  const store = useChunks()

  /** Where core finds resolved references in `text` (for baking); null = core unreachable. */
  const parser: ChunkParser = async (text, surface, chunks) => {
    try {
      const res = await api.post<PromptParseResult>('/prompt/parse', { text, surface, chunks })

      return res.chunks ?? []
    }
    catch {
      return null
    }
  }

  /**
   * Validate a chunk's content before saving it. `errors` are located in
   * the content and block the save: what core rejects in a prompt. A chunk
   * that parses in a prompt but not in Undesired Content (a negative
   * weight) is saved with `ucNote`. null = core unreachable; core still
   * validates wherever the chunk is used.
   */
  async function validate(name: string, content: string, selfId = ''): Promise<{ errors: PromptIssue[], ucNote: string } | null> {
    // the chunk as it would be saved, so a self-reference is caught as a cycle
    const others = store.chunks.value.filter(c => c.id !== selfId && c.name !== name)
    const chunks = referencedChunks([content], [...others, { name, content }])
    try {
      const [pos, neg] = await Promise.all((['prompt', 'negative'] as PromptSurface[]).map(surface =>
        api.post<PromptParseResult>('/prompt/parse', { text: content, surface, chunks })))

      return {
        errors: pos!.errors,
        ucNote: !pos!.errors.length && neg!.errors.length ? `Not usable in Undesired Content: ${neg!.errors[0]!.message}` : '',
      }
    }
    catch {
      return null
    }
  }

  /** What `!macro:name!` resolves to right now, per core (history restore). */
  const resolver = (surface: PromptSurface) => async (name: string): Promise<ChunkResolution | null> => {
    if (!store.byName.value.has(name)) return null

    const text = chunkToken(name)
    const res = await api.post<PromptParseResult>('/prompt/parse', { text, surface, chunks: store.referenced([text]) })
    const ref = res.chunks?.[0]

    if (res.errors.length || !ref) return null

    return { hash: ref.hash, nais: res.tags.map(t => t.nai) }
  }

  /**
   * A stored generation as an applyable config (decision D8). The expanded
   * tags are the record; a run of them becomes its `!macro:Name!` again only
   * where today's local chunk still resolves to exactly those tags.
   */
  async function restoreConfig(p: GenerationParams): Promise<PresetConfig> {
    const cfg = paramsToPresetConfig(p)
    const hasChunk = (ts?: PromptTag[]) => (ts ?? []).some(t => t.chunk)

    if (!hasChunk(p.prompt_tags) && !hasChunk(p.negative_tags) && !(p.characters ?? []).some(c => hasChunk(c.tags))) return cfg

    try {
      const pos = await restoreUserTags(p.prompt_tags, resolver('prompt'))
      const chars = await Promise.all((p.characters ?? []).map(c => restoreUserTags(c.tags, resolver('character'))))
      // a chunk restored as a token re-creates its own relocated tags
      const back = new Set([pos, ...chars].flatMap(r => r.restored))
      const neg = await restoreUserTags(
        (p.negative_tags ?? []).filter(t => !(t.relocated && t.chunk && back.has(t.chunk.name))), resolver('negative'))

      if (p.prompt_tags) cfg.tagList = [...new Set(pos.pieces)]

      if (p.negative_tags) cfg.und = neg.pieces.join(', ')

      cfg.chars = cfg.chars.map((c, i) => (p.characters?.[i]?.tags ? { ...c, text: chars[i]!.pieces.join(', ') } : c))
      cfg.chunks = store.snapshot([cfg.tagList.join(', '), cfg.und, ...cfg.chars.map(c => c.text)])
    }
    catch { /* core unreachable: the expanded tags already in cfg are the record */ }

    return cfg
  }

  return { parser, validate, restoreConfig }
}
