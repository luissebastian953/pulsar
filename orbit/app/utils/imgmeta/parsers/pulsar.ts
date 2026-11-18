// Pulsar's own metadata: a `pulsar` iTXt chunk holding JSON `{"v":1,…}` —
// see PulsarMetaV1 in ../types for the field contract the Go writer must
// match. The reader is strict about `v` and lenient about everything else: a
// missing or wrongly-typed field is dropped, never guessed.
import { isRecord, num, parseJson, str } from '../text'
import type { GenParams, PulsarMetaV1 } from '../types'

export const PULSAR_KEY = 'pulsar'

export type PulsarParse
  = | { ok: true, params: GenParams }
    | { ok: false, reason: 'bad_json' | 'unknown_version' }

export function parsePulsar(text: string): PulsarParse {
  const m = parseJson(text)

  if (!isRecord(m)) return { ok: false, reason: 'bad_json' }

  if (m.v !== 1) return { ok: false, reason: 'unknown_version' }

  const meta: Partial<PulsarMetaV1> & { v: 1 } = { v: 1 }

  for (const key of ['prompt', 'negative', 'sampler', 'scheduler', 'lineage', 'style', 'kind'] as const) {
    const v = str(m[key])

    if (v !== undefined) meta[key] = v
  }

  for (const key of ['width', 'height', 'steps', 'cfg', 'rescale', 'seed'] as const) {
    const v = typeof m[key] === 'number' ? num(m[key]) : undefined

    if (v !== undefined) meta[key] = v
  }

  if (Array.isArray(m.characters)) meta.characters = m.characters.filter(c => typeof c === 'string').slice(0, 64)

  const extra: Record<string, string> = {}

  if (meta.lineage) extra.lineage = meta.lineage

  if (meta.style) extra.style = meta.style

  if (meta.kind) extra.kind = meta.kind

  return {
    ok: true,
    params: {
      generator: 'pulsar',
      prompt: meta.prompt,
      negative: meta.negative,
      characters: (meta.characters ?? []).map(prompt => ({ prompt })),
      width: meta.width,
      height: meta.height,
      steps: meta.steps,
      cfg: meta.cfg,
      rescale: meta.rescale,
      seed: meta.seed,
      sampler: meta.sampler,
      scheduler: meta.scheduler,
      extra,
      pulsar: meta,
      raw: m,
    },
  }
}
