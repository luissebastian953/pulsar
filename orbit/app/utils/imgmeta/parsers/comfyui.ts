// ComfyUI. The `prompt` chunk is the API-format graph that actually ran:
//
//   { "<id>": { "class_type": "KSampler", "inputs": { "seed": 1, "positive": ["6", 0], … } } }
//
// where an input is a literal or a link `[sourceNodeId, outputSlot]`. There
// is no "the prompt" in it, so it is recovered by heuristics ported from
// Civitai's MIT reader (civitai/media-metadata, comfyui/graph.ts):
//
//   1. pick the sampler whose latent comes straight from an empty-latent
//      node — the first txt2img pass, not a hires/refiner pass;
//   2. walk its positive/negative links back through conditioning nodes to
//      text, under a visit budget (a chain of diamonds costs 2^depth);
//   3. only if the graph gives no text, fall back to the UI `workflow`,
//      restricted to text-encode nodes wired to a sampler, because workflow
//      widgets keep stale text the run never used.
//
// Custom nodes routinely defeat this (Civitai: 5 of 35 fixtures yield no
// prompt). That is why the raw chunks are always returned beside the result.
import { COMFY_VISIT_BUDGET } from '../limits'
import { display, isRecord, num, parseJson, str } from '../text'
import type { GenParams, MetaWarning, Resource } from '../types'

interface Node {
  class_type?: unknown
  inputs?: unknown
}
type Graph = Record<string, Node>
type Side = 'positive' | 'negative'

const SAMPLER_CLASSES = new Set(['KSampler', 'KSamplerAdvanced', 'KSampler (Efficient)', 'KSampler Adv. (Efficient)', 'KSampler SDXL (Eff.)'])

function inputsOf(node: Node | undefined): Record<string, unknown> {
  return node && isRecord(node.inputs) ? node.inputs : {}
}

function isLink(v: unknown): v is [string | number, number] {
  return Array.isArray(v) && v.length === 2 && (typeof v[0] === 'string' || typeof v[0] === 'number') && typeof v[1] === 'number'
}

function follow(graph: Graph, v: unknown): Node | undefined {
  if (!isLink(v) || !Object.hasOwn(graph, String(v[0]))) return undefined

  const node = graph[String(v[0])]

  return isRecord(node) ? node : undefined
}

function classOf(node: Node | undefined): string {
  return typeof node?.class_type === 'string' ? node.class_type : ''
}

function isSampler(node: Node): boolean {
  const inputs = inputsOf(node)

  if (SAMPLER_CLASSES.has(classOf(node)) || classOf(node) === 'SamplerCustomAdvanced') return true

  // Vendor samplers, by shape.
  return 'positive' in inputs && 'negative' in inputs && ('latent_image' in inputs || 'sampler_name' in inputs)
}

interface Walk {
  graph: Graph
  visits: number
  truncated: boolean
  path: Set<Node>
}

/**
 * Resolve an input to prompt text. `asString` is set once a string edge has
 * been followed: only then are generic `value`/`string` literals trusted.
 */
function textOf(w: Walk, value: unknown, side: Side, asString: boolean): string | undefined {
  if (typeof value === 'string') return value

  const node = follow(w.graph, value)

  if (!node || w.path.has(node)) return undefined

  if (++w.visits > COMFY_VISIT_BUDGET) {
    w.truncated = true
    return undefined
  }

  // Cycle guard on the current path, not a visited set: one string subtree
  // feeding two inputs of a concatenate is legal.
  w.path.add(node)

  try {
    return nodeText(w, node, side, asString)
  }
  finally {
    w.path.delete(node)
  }
}

function joinDistinct(parts: (string | undefined)[], sep: string): string | undefined {
  const kept = parts.filter((p): p is string => typeof p === 'string' && p !== '')
  const unique = kept.filter((p, i) => kept.indexOf(p) === i)

  return unique.length ? unique.join(sep) : undefined
}

function nodeText(w: Walk, node: Node, side: Side, asString: boolean): string | undefined {
  const inputs = inputsOf(node)
  const sub = (key: string, str = true) => (key in inputs ? textOf(w, inputs[key], side, str) : undefined)

  // Wildcard processors record what they actually sent.
  if (typeof inputs.populated_text === 'string' && inputs.populated_text) return inputs.populated_text

  if ('text' in inputs) return sub('text')

  // SDXL encoders.
  if ('text_g' in inputs || 'text_l' in inputs) return joinDistinct([sub('text_g'), sub('text_l')], ', ')

  if (`text_${side}` in inputs) return sub(`text_${side}`)

  // String concatenation chains.
  if ('string_a' in inputs || 'string_b' in inputs) {
    return joinDistinct([sub('string_a'), sub('string_b')], typeof inputs.delimiter === 'string' ? inputs.delimiter : '')
  }

  if ('text_a' in inputs || 'text_b' in inputs) return joinDistinct([sub('text_a'), sub('text_b')], ', ')

  if (asString) {
    for (const key of ['value', 'string', 'prompt']) {
      if (key in inputs) return sub(key)
    }

    return undefined
  }

  // Conditioning pass-through: ControlNetApply, FluxGuidance, area/mask setters…
  if ('conditioning' in inputs) return sub('conditioning', false)

  // …two-sided ones (ControlNetApplyAdvanced), and combine/concat.
  if (isLink(inputs[side])) return sub(side, false)

  if ('conditioning_1' in inputs || 'conditioning_2' in inputs) {
    return joinDistinct([sub('conditioning_1', false), sub('conditioning_2', false)], ', ')
  }

  if ('conditioning_to' in inputs || 'conditioning_from' in inputs) {
    return joinDistinct([sub('conditioning_to', false), sub('conditioning_from', false)], ', ')
  }

  return undefined
}

/** A numeric input, following a link to a primitive/seed node when needed. */
function number(graph: Graph, value: unknown, depth = 0): number | undefined {
  if (!isLink(value)) return num(value)

  if (depth >= 4) return undefined

  const inputs = inputsOf(follow(graph, value))

  for (const key of ['value', 'seed', 'noise_seed', 'int', 'number']) {
    if (key in inputs) return number(graph, inputs[key], depth + 1)
  }

  return undefined
}

function string(graph: Graph, value: unknown): string | undefined {
  if (typeof value === 'string') return value

  const inputs = inputsOf(follow(graph, value))

  for (const key of ['sampler_name', 'scheduler', 'value', 'string']) {
    if (typeof inputs[key] === 'string') return inputs[key]
  }

  return undefined
}

/** Walk the model chain back to the checkpoint, collecting LoRAs on the way. */
function modelChain(graph: Graph, start: unknown): { model?: string, resources: Resource[] } {
  const resources: Resource[] = []
  const seen = new Set<Node>()
  let node = follow(graph, start)

  for (let i = 0; node && i < 64 && !seen.has(node); i++) {
    seen.add(node)

    const inputs = inputsOf(node)
    const model = str(inputs.ckpt_name) ?? str(inputs.unet_name)

    if (model) return { model, resources }

    if (typeof inputs.lora_name === 'string') {
      const lora: Resource = { kind: 'lora', name: inputs.lora_name }
      const weight = num(inputs.strength_model)

      if (weight !== undefined) lora.weight = weight

      resources.push(lora)
    }

    node = follow(graph, inputs.model)
  }

  return { resources }
}

function isEmptyLatent(node: Node | undefined): boolean {
  return /^Empty.*Latent/.test(classOf(node))
}

/** Flux-style SamplerCustomAdvanced splits its settings over helper nodes. */
function flattenCustomSampler(graph: Graph, inputs: Record<string, unknown>): Record<string, unknown> {
  const guider = inputsOf(follow(graph, inputs.guider))
  const sigmas = inputsOf(follow(graph, inputs.sigmas))

  return {
    positive: guider.positive ?? guider.conditioning,
    negative: guider.negative,
    cfg: guider.cfg,
    model: guider.model ?? sigmas.model,
    seed: inputsOf(follow(graph, inputs.noise)).noise_seed,
    sampler_name: inputsOf(follow(graph, inputs.sampler)).sampler_name,
    scheduler: sigmas.scheduler,
    steps: sigmas.steps,
    denoise: sigmas.denoise,
    latent_image: inputs.latent_image,
  }
}

function fromGraph(graph: Graph, warnings: MetaWarning[]): GenParams | null {
  const samplers = Object.values(graph).filter(n => isRecord(n) && isSampler(n)).slice(0, 64)
  const sampler = samplers.find(n => isEmptyLatent(follow(graph, inputsOf(n).latent_image))) ?? samplers[0]

  if (!sampler) return null

  const inputs = classOf(sampler) === 'SamplerCustomAdvanced' ? flattenCustomSampler(graph, inputsOf(sampler)) : inputsOf(sampler)
  const walk: Walk = { graph, visits: 0, truncated: false, path: new Set() }
  const prompt = textOf(walk, inputs.positive, 'positive', false)

  walk.visits = 0

  const negative = textOf(walk, inputs.negative, 'negative', false)

  if (walk.truncated) warnings.push({ code: 'comfy_walk_truncated' })

  const latent = inputsOf(follow(graph, inputs.latent_image))
  const { model, resources } = modelChain(graph, inputs.model)
  const params: GenParams = {
    generator: 'comfyui',
    prompt,
    negative,
    characters: [],
    width: number(graph, latent.width),
    height: number(graph, latent.height),
    steps: number(graph, inputs.steps),
    cfg: number(graph, inputs.cfg),
    seed: number(graph, inputs.seed ?? inputs.noise_seed),
    sampler: string(graph, inputs.sampler_name),
    scheduler: string(graph, inputs.scheduler),
    model,
    extra: {},
  }
  const denoise = display(number(graph, inputs.denoise))

  if (denoise !== undefined) params.extra.denoise = denoise

  params.extra.sampler_node = classOf(sampler)

  if (resources.length) params.resources = resources

  return params
}

/** The stale-prone fallback: text-encode widgets wired to a sampler in the UI graph. */
function fromWorkflow(workflow: unknown): { prompt?: string, negative?: string } {
  if (!isRecord(workflow) || !Array.isArray(workflow.nodes) || !Array.isArray(workflow.links)) return {}

  const nodes = workflow.nodes.filter(isRecord).slice(0, 4096)
  const links = workflow.links.slice(0, 16384)
  const sampler = nodes.find(n => SAMPLER_CLASSES.has(String(n.type)))

  if (!sampler || !Array.isArray(sampler.inputs)) return {}

  const widgetText = (side: Side): string | undefined => {
    const input = (sampler.inputs as unknown[]).find(i => isRecord(i) && i.name === side)
    const linkId = isRecord(input) ? input.link : undefined
    // Link rows are [id, originNode, originSlot, targetNode, targetSlot, type].
    const link = links.find(l => Array.isArray(l) && l[0] === linkId)
    const origin = Array.isArray(link) ? nodes.find(n => n.id === link[1]) : undefined

    if (!origin || !/CLIPTextEncode/.test(String(origin.type)) || !Array.isArray(origin.widgets_values)) return undefined

    return origin.widgets_values.find(v => typeof v === 'string')
  }

  return { prompt: widgetText('positive'), negative: widgetText('negative') }
}

/**
 * Interpret a ComfyUI image. Either JSON text may be missing. Returns null
 * only when neither parses; a graph the walk cannot read still yields a
 * result with no prompt, so the caller can say "ComfyUI" and show the raw
 * chunks.
 */
export function parseComfyUI(promptJson: string | undefined, workflowJson: string | undefined, warnings: MetaWarning[]): GenParams | null {
  // Python's json.dumps writes bare NaN/Infinity; real files contain them.
  const graph = promptJson === undefined ? undefined : parseJson(promptJson, { sanitize: true })
  const workflow = workflowJson === undefined ? undefined : parseJson(workflowJson, { sanitize: true })

  if (!isRecord(graph) && !isRecord(workflow)) {
    warnings.push({ code: 'bad_json', detail: promptJson === undefined ? 'workflow' : 'prompt' })
    return null
  }

  const params: GenParams = (isRecord(graph) ? fromGraph(graph as Graph, warnings) : null)
    ?? { generator: 'comfyui', characters: [], extra: {} }

  if (params.prompt === undefined) {
    const fallback = fromWorkflow(workflow)

    if (fallback.prompt !== undefined) {
      params.prompt = fallback.prompt
      params.negative ??= fallback.negative
      warnings.push({ code: 'comfy_workflow_fallback' })
    }
  }

  params.raw = isRecord(graph) ? graph : workflow

  return params
}
