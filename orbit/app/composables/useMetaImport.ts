import { useChunks } from '~/composables/useChunks'
import type { PromptParseResult } from '~/types/api'
import type { GenParams } from '~/utils/imgmeta/types'
import { buildMetaImport, type ImportOptions, type ImportResult, type ParseFn } from '~/utils/meta-import'

// The import dialog's calls into core (docs/frontend.md#image-inspector).
// Orbit never splits or cleans prompt text itself: POST /prompt/parse with
// `clean: true` does both. Nothing here uploads or generates — the result is
// a patch for the composer, whose fields are validated and compliance-checked
// like typed text.
export function useMetaImport() {
  const api = useApi()
  const store = useChunks()

  const parse: ParseFn = (text, surface, opts = {}) =>
    api.post<PromptParseResult>('/prompt/parse', {
      text,
      surface,
      ...(opts.clean ? { clean: true } : {}),
      ...(opts.chunks?.length ? { chunks: opts.chunks } : {}),
    })

  function build(params: GenParams, options: ImportOptions): Promise<ImportResult> {
    return buildMetaImport(params, options, {
      parse,
      chunks: store.chunks.value.map(c => ({ name: c.name, content: c.content })),
      referenced: store.referenced,
    })
  }

  return { build }
}
