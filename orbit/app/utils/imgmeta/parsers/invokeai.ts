// InvokeAI, three generations:
//   invokeai_metadata  JSON: positive_prompt, negative_prompt, scheduler, model{…}, …  (3.x+)
//   sd-metadata        JSON: model_weights, image{prompt[{prompt,weight}], sampler, …}   (2.x)
//   Dream              CLI:  "prompt [negative]" -s 50 -S 123 -W 512 -H 512 -C 7.5 -A k_lms
// In the two legacy forms the negative is written inline as [bracketed] spans.
import { display, isRecord, num, parseJson, str } from '../text'
import type { GenParams } from '../types'

/** Legacy InvokeAI: `[...]` spans are the negative prompt. */
function splitBracketNegative(text: string): { prompt: string, negative?: string } {
  const negatives: string[] = []
  const prompt = text.replace(/\[([^[\]]*)\]/g, (_, inner: string) => {
    negatives.push(inner.trim())
    return ''
  }).replace(/\s{2,}/g, ' ').trim()

  return negatives.length ? { prompt, negative: negatives.join(', ') } : { prompt }
}

function modelName(v: unknown): { model?: string, modelHash?: string } {
  if (typeof v === 'string') return { model: v }

  if (!isRecord(v)) return {}

  return { model: str(v.name) ?? str(v.model_name), modelHash: str(v.hash) }
}

export function parseInvokeMetadata(text: string): GenParams | null {
  const m = parseJson(text, { sanitize: true })

  if (!isRecord(m)) return null

  const extra: Record<string, string> = {}

  for (const key of ['generation_mode', 'app_version', 'clip_skip', 'seamless_x', 'seamless_y', 'refiner_model', 'vae', 'loras', 'positive_style_prompt', 'negative_style_prompt']) {
    const v = display(m[key])

    if (v !== undefined) extra[key] = v
  }

  return {
    generator: 'invokeai',
    prompt: str(m.positive_prompt),
    negative: str(m.negative_prompt),
    characters: [],
    width: num(m.width),
    height: num(m.height),
    steps: num(m.steps),
    cfg: num(m.cfg_scale),
    rescale: num(m.cfg_rescale_multiplier),
    seed: num(m.seed),
    // InvokeAI's "scheduler" names the sampling algorithm (euler_a, dpmpp_2m_k).
    sampler: str(m.scheduler),
    ...modelName(m.model),
    extra,
    raw: m,
  }
}

export function parseSdMetadata(text: string): GenParams | null {
  const m = parseJson(text, { sanitize: true })

  if (!isRecord(m)) return null

  const image = isRecord(m.image) ? m.image : {}
  const rawPrompt = Array.isArray(image.prompt)
    ? image.prompt.slice(0, 64).map(p => (isRecord(p) ? str(p.prompt) ?? '' : '')).filter(Boolean).join(', ')
    : str(image.prompt) ?? ''
  const extra: Record<string, string> = {}

  for (const [key, value] of [['app_id', m.app_id], ['app_version', m.app_version], ['type', image.type]] as const) {
    const v = display(value)

    if (v !== undefined) extra[key] = v
  }

  return {
    generator: 'invokeai',
    ...splitBracketNegative(rawPrompt),
    characters: [],
    width: num(image.width),
    height: num(image.height),
    steps: num(image.steps),
    cfg: num(image.cfg_scale),
    seed: num(image.seed),
    sampler: str(image.sampler),
    model: str(m.model_weights),
    modelHash: str(m.model_hash),
    extra,
    raw: m,
  }
}

const DREAM_FLAGS: Record<string, 'steps' | 'seed' | 'width' | 'height' | 'cfg'> = {
  s: 'steps',
  S: 'seed',
  W: 'width',
  H: 'height',
  C: 'cfg',
}

export function parseDream(text: string): GenParams | null {
  // The prompt is everything between the first and the LAST double quote
  // (prompts may contain quotes); the flags follow it.
  const trimmed = text.trim()
  const close = trimmed.lastIndexOf('"')

  if (!trimmed.startsWith('"') || close < 1) return null

  const flags = trimmed.slice(close + 1)
  const params: GenParams = {
    generator: 'invokeai',
    ...splitBracketNegative(trimmed.slice(1, close).replace(/\\"/g, '"')),
    characters: [],
    extra: {},
  }

  for (const [, flag, value] of ` ${flags}`.matchAll(/\s+-(\w+)\s+(\S+)/g)) {
    const field = Object.hasOwn(DREAM_FLAGS, flag!) ? DREAM_FLAGS[flag!] : undefined

    if (field) params[field] = num(value)
    else if (flag === 'A') params.sampler = value
    else params.extra[`-${flag}`] = value!
  }

  return params
}
