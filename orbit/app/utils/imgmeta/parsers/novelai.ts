// NovelAI: a six-key dictionary (Title, Description, Software, Source,
// Generation time, Comment) where Comment is the server-resolved request as
// JSON. The same dictionary appears as PNG text chunks, as JSON in the WebP
// EXIF UserComment, and (minus Title) gzipped in the alpha LSBs.
//
// The reader is tolerant, like NovelAI's own inspector: every key but Comment
// may be missing, unknown Comment keys are normal (see `raw`), and neither
// `request_type` nor `signed_hash` is required. 2022 images have no
// `Comment.prompt` — their prompt lives only in Description.
import { display, isRecord, num, parseJson, str } from '../text'
import type { CharacterPrompt, GenParams, NovelAIInfo } from '../types'
import { lookupNaiModel } from './nai-models'

const REQUEST_TYPES: Record<string, string> = {
  PromptGenerateRequest: 'Text to Image',
  Img2ImgRequest: 'Image to Image',
  NativeInfillingRequest: 'Inpainting',
}

/** The dictionary keys; values are strings except that stealth copies may nest Comment as an object. */
export type NaiDict = Record<string, unknown>

/** The Comment as the exact text that was signed, when the dictionary holds it as a string. */
export function naiCommentText(dict: NaiDict): string | undefined {
  return str(dict.Comment)
}

function caption(v: unknown): { base?: string, chars: unknown[] } {
  const c = isRecord(v) && isRecord(v.caption) ? v.caption : isRecord(v) ? v : {}

  return { base: str(c.base_caption), chars: Array.isArray(c.char_captions) ? c.char_captions : [] }
}

function charText(v: unknown): string {
  return typeof v === 'string' ? v : isRecord(v) ? str(v.char_caption) ?? '' : ''
}

function nonEmptyArray(v: unknown): boolean {
  return Array.isArray(v) && v.length > 0
}

/** Null unless `dict.Comment` is (or parses to) a JSON object. */
export function parseNovelAI(dict: NaiDict): GenParams | null {
  const comment = typeof dict.Comment === 'string' ? parseJson(dict.Comment, { sanitize: true }) : dict.Comment

  if (!isRecord(comment)) return null

  const pos = caption(comment.v4_prompt)
  const neg = caption(comment.v4_negative_prompt)
  const characters: CharacterPrompt[] = pos.chars.slice(0, 64).map((c, i) => {
    const ch: CharacterPrompt = { prompt: charText(c) }
    const negative = charText(neg.chars[i])
    const center = isRecord(c) && Array.isArray(c.centers) && isRecord(c.centers[0]) ? c.centers[0] : null

    if (negative) ch.negative = negative

    if (center) {
      ch.x = num(center.x)
      ch.y = num(center.y)
    }

    return ch
  })

  const source = str(dict.Source)
    ?? (str(comment.model_name) && str(comment.model_hash) ? `${str(comment.model_name)} ${str(comment.model_hash)}` : undefined)
  const requestType = str(comment.request_type)
  const info: NovelAIInfo = {
    title: str(dict.Title),
    description: str(dict.Description),
    software: str(dict.Software),
    source,
    generationTime: str(dict['Generation time']),
    model: lookupNaiModel(source),
    requestType,
    // hasOwn: the key is file content (`constructor` must not resolve).
    requestTypeLabel: requestType !== undefined && Object.hasOwn(REQUEST_TYPES, requestType) ? REQUEST_TYPES[requestType]! : 'Unknown Request Type',
    controlnetModel: str(comment.controlnet_model),
    vibeTransfer: nonEmptyArray(comment.reference_strength_multiple) || (num(comment.reference_strength) ?? 0) > 0,
    vibeEmbedded: nonEmptyArray(comment.reference_image_multiple),
    preciseReference: nonEmptyArray(comment.director_reference_strengths),
    smea: comment.sm_dyn === true ? 'smea_dyn' : comment.sm === true ? 'smea' : null,
    uncondScale: num(comment.uncond_scale),
    signed: typeof comment.signed_hash === 'string',
  }

  if (isRecord(comment.actual_prompts)) {
    const ap = caption(comment.actual_prompts.prompt)
    const an = caption(comment.actual_prompts.negative_prompt)

    info.actual = { prompt: ap.base, negative: an.base, characters: ap.chars.slice(0, 64).map(charText) }
  }

  const extra: Record<string, string> = {}

  for (const key of ['strength', 'noise', 'n_samples', 'skip_cfg_above_sigma', 'dynamic_thresholding', 'legacy_v3_extend']) {
    const v = display(comment[key])

    if (v !== undefined) extra[key] = v
  }

  return {
    generator: 'novelai',
    // Structure first, then the flat field, then Description (the only place
    // 2022 images keep the prompt).
    prompt: pos.base ?? str(comment.prompt) ?? str(dict.Description),
    negative: neg.base ?? str(comment.uc),
    characters,
    width: num(comment.width),
    height: num(comment.height),
    steps: num(comment.steps),
    cfg: num(comment.scale),
    rescale: num(comment.cfg_rescale),
    seed: num(comment.seed),
    sampler: str(comment.sampler),
    scheduler: str(comment.noise_schedule),
    model: info.model.name === 'Unknown' ? source : info.model.name,
    modelHash: info.model.hash,
    extra,
    novelai: info,
    raw: comment,
  }
}
