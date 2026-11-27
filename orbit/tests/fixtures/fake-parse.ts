// A stand-in for core's POST /prompt/parse, for import tests. It is NOT the
// grammar — core owns that (core/internal/prompt, tested there and through
// prompt_e2e_test.go). It splits on commas and newlines, mimics what
// `clean: true` drops, treats `-N::x ::` as relocated, and expands chunk
// tokens from the chunks it is given, which is all the import logic reads.
import type { ChunkPayload, ParsedTag, PromptIssue, PromptParseResult, PromptSurface } from '~/types/api'
import type { ParseFn } from '~/utils/meta-import'

export interface FakeParseCall {
  text: string
  surface: PromptSurface
  clean: boolean
  chunks: ChunkPayload[]
}

const name = (s: string) => s.replace(/^-?[\d.]+::\s*|\s*::$/g, '').trim().toLowerCase().split(/\s+/).join('_')
const bare = (s: string) => s.replace(/^-?[\d.]+::\s*|\s*::$/g, '').trim()

export function fakeParse(opts: { calls?: FakeParseCall[], fail?: boolean } = {}): ParseFn {
  return async (text, surface, o = {}) => {
    opts.calls?.push({ text, surface, clean: !!o.clean, chunks: o.chunks ?? [] })

    if (opts.fail) throw new Error('core unreachable')

    const notices: PromptIssue[] = []
    const errors: PromptIssue[] = []
    const issue = (code: string, message: string): PromptIssue => ({ code, message, start: 0, end: 1, tag: 0 })
    let cleaned = text

    if (o.clean) {
      const drop = (re: RegExp, code: string, what: string) => {
        cleaned = cleaned.replace(re, (m) => {
          notices.push(issue(code, `\`${m}\` was removed — ${what}`))

          return ''
        })
      }

      drop(/<[a-z]+:[^>,\n]*>?/gi, 'cleaned_extra_network', 'use the LoRA picker')
      drop(/embedding:[^\s,()]+/gi, 'cleaned_embedding', 'embeddings aren\'t supported')
      drop(/\|\|[^\n]*?\|\|/g, 'cleaned_random', 'random choice')

      if (cleaned.includes('|')) {
        notices.push(issue('cleaned_pipe', '`|` was read as a tag break'))
        cleaned = cleaned.replaceAll('|', ',')
      }
    }
    else if (/<[a-z]+:|embedding:|\|/i.test(cleaned)) {
      errors.push(issue('extra_network', 'not supported'))
    }

    if (/[{}]/.test(cleaned) && (cleaned.split('{').length !== cleaned.split('}').length)) errors.push(issue('unclosed_bracket', '`{` is never closed'))

    const tags: ParsedTag[] = []
    const relocated: ParsedTag[] = []
    const chunks = new Map((o.chunks ?? []).map(c => [c.name, c.content]))
    const push = (piece: string, chunk?: string) => {
      const p = piece.trim()

      if (!p) return

      const token = /^!macro:(.+)!$/.exec(p)

      if (token) {
        const content = chunks.get(token[1]!)

        if (content === undefined) errors.push(issue('unknown_chunk', `no chunk named ${token[1]}`))
        else content.split(/[,\n]/).forEach(c => push(c, chunk ?? token[1]!))

        return
      }

      const neg = /^-([\d.]+)::(.*?)\s*::$/.exec(p)
      const tag: ParsedTag = {
        text: bare(p), name: name(p), nai: neg ? `${neg[1]}::${neg[2]!.trim()} ::`.replace(/^1::(.*) ::$/, '$1') : p,
        start: 0, end: 0, tag: 0, runs: [],
        ...(chunk ? { chunk: { name: chunk, hash: 'h' } } : {}),
      }

      if (neg) relocated.push({ ...tag, relocated: true })
      else tags.push(tag)
    }

    if (!errors.length) cleaned.split(/[,\n]/).forEach(c => push(c))

    const res: PromptParseResult = {
      tags: errors.length ? [] : tags, relocated: errors.length ? [] : relocated,
      errors, notices, chunks: [], marks: [], emitted: '',
      ...(o.clean ? { cleaned } : {}),
    }

    return res
  }
}
