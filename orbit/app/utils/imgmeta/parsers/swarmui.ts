// SwarmUI: one JSON object — in the PNG `parameters` chunk, the EXIF
// UserComment (UTF-16LE) or a stealth payload — whose `sui_image_params` holds
// lowercase parameter ids. Values are often strings.
import { display, isRecord, num, parseJson, str } from '../text'
import type { GenParams } from '../types'

const COMMON = new Set(['prompt', 'negativeprompt', 'model', 'seed', 'steps', 'cfgscale', 'width', 'height', 'sampler', 'scheduler'])

/** Cheap pre-check so non-Swarm `parameters` text is not JSON-parsed twice. */
export function looksLikeSwarm(text: string): boolean {
  return text.trimStart().startsWith('{') && text.includes('sui_image_params')
}

export function parseSwarmUI(text: string): GenParams | null {
  const root = parseJson(text, { sanitize: true })

  if (!isRecord(root) || !isRecord(root.sui_image_params)) return null

  const p = root.sui_image_params
  const extra: Record<string, string> = {}

  for (const [key, value] of Object.entries(p).slice(0, 200)) {
    const v = COMMON.has(key) ? undefined : display(value)

    if (v !== undefined) extra[key] = v
  }

  const models = Array.isArray(root.sui_models) ? root.sui_models.find(m => isRecord(m) && m.param === 'model') : undefined

  return {
    generator: 'swarmui',
    prompt: str(p.prompt),
    negative: str(p.negativeprompt),
    characters: [],
    width: num(p.width),
    height: num(p.height),
    steps: num(p.steps),
    cfg: num(p.cfgscale),
    seed: num(p.seed),
    sampler: str(p.sampler),
    scheduler: str(p.scheduler),
    model: str(p.model),
    modelHash: isRecord(models) ? str(models.hash) : undefined,
    extra,
    raw: root,
  }
}
