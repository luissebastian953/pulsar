// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { detectContainer, detectStealth } from '~/utils/imgmeta/detect'
import { parseA1111, splitInfotext } from '~/utils/imgmeta/parsers/a1111'
import { parseComfyUI } from '~/utils/imgmeta/parsers/comfyui'
import { parseDream } from '~/utils/imgmeta/parsers/invokeai'
import { lookupNaiModel } from '~/utils/imgmeta/parsers/nai-models'
import { parseNovelAI } from '~/utils/imgmeta/parsers/novelai'
import { parsePulsar } from '~/utils/imgmeta/parsers/pulsar'
import { readImageMeta } from '~/utils/imgmeta/read'
import { parseJson } from '~/utils/imgmeta/text'
import type { MetaWarning, PulsarMetaV1, RawEntry } from '~/utils/imgmeta/types'
import { embedStealth, encodePng, iTXt, noise, pillowText, shellPng, tEXt, tiff, userComment, webp } from './fixtures/imgmeta'
import { NAI_0001 } from './fixtures/nai-0001'

const read = (chunks: Uint8Array[]) => readImageMeta(shellPng(chunks), { stealth: 'never' })

describe('a1111', () => {
  it('parses the upstream docstring example', () => {
    // modules/infotext_utils.py, parse_generation_parameters.
    const text = [
      'girl with an artist\'s beret, determined, blue eyes, desert scene, computer monitors, heavy makeup, by Alphonse Mucha and Charlie Bowater, ((eyeshadow)), (coquettish), detailed, intricate',
      'Negative prompt: ugly, fat, obese, chubby, (((deformed))), [blurry], bad anatomy, disfigured, poorly drawn face, mutation, mutated, (extra_limb), (ugly), (poorly drawn hands), messy drawing',
      'Steps: 20, Sampler: Euler a, CFG scale: 7, Seed: 965400086, Size: 512x512, Model hash: 45dee52b',
    ].join('\n')
    const p = parseA1111(text)

    expect(p).toMatchObject({
      generator: 'a1111',
      prompt: text.split('\n')[0],
      negative: text.split('\n')[1]!.slice('Negative prompt: '.length),
      steps: 20,
      sampler: 'Euler a',
      cfg: 7,
      seed: 965400086,
      width: 512,
      height: 512,
      modelHash: '45dee52b',
    })
    expect(p!.extra).toEqual({})
  })

  it('unquotes JSON-quoted values and sub-splits the hash lists', () => {
    const text = [
      'masterpiece, best quality, 1girl, silver hair, <lora:detailer:0.6>',
      'Negative prompt: lowres, bad anatomy',
      'Steps: 28, Sampler: Euler a, Schedule type: Automatic, CFG scale: 6, Seed: 123456789, Size: 832x1216, Model hash: 0fd2f9c0a3, Model: illustriousXL_v01, Lora hashes: "detailer: 0123456789ab, style-x: ba9876543210", TI hashes: "easynegative: c74b4e810b03", Template: "a \\"quoted\\", comma: and colon\\nnewline", Hashes: {"model": "0fd2f9c0a3"}, Version: v1.10.1',
    ].join('\n')
    const p = parseA1111(text)!

    expect(p).toMatchObject({ scheduler: 'Automatic', model: 'illustriousXL_v01', modelHash: '0fd2f9c0a3', width: 832, height: 1216 })
    expect(p.resources).toEqual([
      { kind: 'lora', name: 'detailer', hash: '0123456789ab' },
      { kind: 'lora', name: 'style-x', hash: 'ba9876543210' },
      { kind: 'embedding', name: 'easynegative', hash: 'c74b4e810b03' },
      // Civitai-style `Hashes`: a JSON object, readable only while it has no comma.
      { kind: 'model', name: 'model', hash: '0fd2f9c0a3' },
    ])
    expect(p.extra.Template).toBe('a "quoted", comma: and colon\nnewline')
    expect(p.extra.Version).toBe('v1.10.1')
  })

  it('treats the last line as prompt text unless it yields three or more pairs', () => {
    expect(splitInfotext('a cat\nSteps: 20, Sampler: Euler')).toEqual({ prompt: 'a cat\nSteps: 20, Sampler: Euler', negative: '', settings: [] })
    expect(splitInfotext('a cat\nSteps: 20, Sampler: Euler, Seed: 1').settings).toEqual([['Steps', '20'], ['Sampler', 'Euler'], ['Seed', '1']])
    // Quoted prose in the prompt is not mistaken for settings.
    expect(splitInfotext('she said: "hello, world", then left').settings).toEqual([])
  })

  it('joins multi-line prompts and negatives, tolerates CRLF, and accepts non-ASCII keys', () => {
    const p = splitInfotext('line one\r\nline two\r\nNegative prompt: bad\r\nworse\r\nSteps: 1, Sampler: x, 种子: 5')

    expect(p.prompt).toBe('line one\nline two')
    expect(p.negative).toBe('bad\nworse')
    expect(p.settings.at(-1)).toEqual(['种子', '5'])
  })

  it('refuses comment text without a settings line when asked to', () => {
    expect(parseA1111('Photo taken in Kyoto', { requireSettings: true })).toBeNull()
    expect(parseA1111('just a prompt')).toMatchObject({ prompt: 'just a prompt' })
    expect(parseA1111('')).toBeNull()
  })
})

describe('novelai', () => {
  const expected = {
    generator: 'novelai',
    prompt: '1girl, bed, candles, bedroom, location, evening, very aesthetic, masterpiece, no text',
    characters: [{ prompt: 'girl, doll joints, ', x: 0.5, y: 0.5 }],
    width: 832,
    height: 1216,
    steps: 23,
    cfg: 7,
    rescale: 0,
    seed: 3216798432,
    sampler: 'k_euler_ancestral',
    scheduler: 'karras',
    model: 'NAI Diffusion V5 Curated',
    modelHash: 'DB276663',
  }

  it('reads NovelAI\'s own bundled example from PNG text chunks', async () => {
    const meta = await read(Object.entries(NAI_0001).map(([k, v]) => pillowText(k, v)))

    expect(meta.generator).toBe('novelai')
    expect(meta.via).toBe('container')
    expect(meta.params).toMatchObject(expected)
    expect(meta.params!.negative).toMatch(/^lowres, artistic error, .* blank page$/)
    expect(meta.params!.novelai).toMatchObject({
      software: 'NovelAI',
      source: 'NovelAI Diffusion V5 DB276663',
      generationTime: '2.186762098921463',
      model: { id: 'nai-diffusion-5-curated', name: 'NAI Diffusion V5 Curated', hash: 'DB276663' },
      requestType: 'PromptGenerateRequest',
      requestTypeLabel: 'Text to Image',
      vibeTransfer: false,
      vibeEmbedded: false,
      preciseReference: false,
      smea: null,
      uncondScale: 0,
      signed: true,
    })
    // 52 keys, unknown ones included, for the raw view.
    expect(Object.keys(meta.params!.raw as object)).toHaveLength(52)
    // The signed_hash is real, but it signed NovelAI's pixels, not this 1×1 shell.
    expect(meta.signature).toBe('invalid')
  })

  it('reads the same dictionary from a WebP EXIF UserComment (ASCII prefix)', async () => {
    const file = webp({ width: 832, height: 1216, alpha: true, exif: tiff({ littleEndian: true, userComment: userComment('ASCII', JSON.stringify(NAI_0001)) }) })
    const meta = await readImageMeta(file, { stealth: 'never' })

    expect(meta.format).toBe('webp')
    expect(meta.generator).toBe('novelai')
    expect(meta.params).toMatchObject(expected)
    expect(meta.params!.novelai!.description).toBe(NAI_0001.Description)
    // No WebP pixel decoder outside a browser: signed, but not checkable here.
    expect(meta.signature).toBe('unavailable')
    expect(meta.warnings).toEqual([{ code: 'pixels_unavailable' }])
  })

  it('accepts a bare Comment object in UserComment, filling Source and Description from EXIF tags', async () => {
    const exif = tiff({ userComment: userComment('ASCII', NAI_0001.Comment), ifd0: { 0x0131: NAI_0001.Source, 0x010e: NAI_0001.Description, 0x010d: 'NovelAI generated image' } })
    const meta = await readImageMeta(webp({ width: 832, height: 1216, exif }), { stealth: 'never' })

    expect(meta.generator).toBe('novelai')
    expect(meta.params!.novelai).toMatchObject({ source: NAI_0001.Source, description: NAI_0001.Description, title: 'NovelAI generated image' })
  })

  it('reads the alpha-channel copy (stealth_pngcomp, Title dropped) when there are no text chunks', async () => {
    const [width, height] = [112, 112]
    const rgba = noise(width, height)

    embedStealth(rgba, width, height, 'stealth_pngcomp', JSON.stringify(NAI_0001))

    const meta = await readImageMeta(encodePng({ width, height, rgba }))

    expect(meta.generator).toBe('novelai')
    expect(meta.via).toBe('stealth')
    expect(meta.params).toMatchObject(expected)
    // A real signature over pixels we do not have: checked, and rightly not valid.
    expect(meta.signature).toBe('invalid')
  })

  it('reads a 2022 image: small Comment, prompt only in Description, no signature', async () => {
    const meta = await read([
      tEXt('Title', 'AI generated image'),
      tEXt('Description', 'masterpiece, best quality,  cat, space, icon'),
      tEXt('Software', 'NovelAI'),
      tEXt('Source', 'Stable Diffusion 1D44365E'),
      tEXt('Comment', '{"steps": 50, "sampler": "k_euler_ancestral", "seed": 2253955223, "strength": 0.4, "noise": 0.0, "scale": 10.0, "uc": "lowres, bad anatomy"}'),
    ])

    expect(meta.params).toMatchObject({
      prompt: 'masterpiece, best quality,  cat, space, icon',
      negative: 'lowres, bad anatomy',
      steps: 50,
      cfg: 10,
      seed: 2253955223,
      characters: [],
      extra: { strength: '0.4', noise: '0' },
    })
    expect(meta.params!.novelai).toMatchObject({ model: { id: 'safe-diffusion', hash: '1D44365E' }, requestTypeLabel: 'Unknown Request Type', signed: false })
    expect(meta.signature).toBe('no_signature')
  })

  it('reports request-type flags, SMEA, per-character negatives and actual_prompts', () => {
    const p = parseNovelAI({
      Software: 'NovelAI',
      Comment: JSON.stringify({
        request_type: 'Img2ImgRequest',
        controlnet_model: 'hed',
        reference_strength_multiple: [0.6],
        reference_image_multiple: ['AAAA'],
        director_reference_strengths: [1],
        sm: true,
        sm_dyn: true,
        v4_prompt: { caption: { base_caption: 'base', char_captions: [{ char_caption: 'c1', centers: [{ x: 0.1, y: 0.9 }] }, { char_caption: 'c2' }] } },
        v4_negative_prompt: { caption: { base_caption: 'neg', char_captions: [{ char_caption: 'n1' }, { char_caption: '' }] } },
        actual_prompts: { prompt: { base_caption: 'resolved', char_captions: [{ char_caption: 'r1' }] }, negative_prompt: { base_caption: 'rneg', char_captions: [] } },
      }),
    })!

    expect(p.characters).toEqual([{ prompt: 'c1', negative: 'n1', x: 0.1, y: 0.9 }, { prompt: 'c2' }])
    expect(p.novelai).toMatchObject({
      requestTypeLabel: 'Image to Image',
      controlnetModel: 'hed',
      vibeTransfer: true,
      vibeEmbedded: true,
      preciseReference: true,
      smea: 'smea_dyn',
      actual: { prompt: 'resolved', negative: 'rneg', characters: ['r1'] },
    })
  })

  it('maps Source by its trailing hash, not its prefix', () => {
    expect(lookupNaiModel('DiffusionModelMetaName.NAIv4next 4BDE2A90').id).toBe('nai-diffusion-4-5-full')
    expect(lookupNaiModel('NovelAI Diffusion V4 5AB81C7C').id).toBe('nai-diffusion-4-5-curated')
    expect(lookupNaiModel('NovelAI Diffusion V5 0ADF9AB7').id).toBe('nai-diffusion-5-full')
    expect(lookupNaiModel('Stable Diffusion XL C1E1DE52').id).toBe('nai-diffusion-3')
    expect(lookupNaiModel('Stable Diffusion 81274D13').id).toBe('nai-diffusion')
    // Unknown hashes fall back on the version in the name, as NovelAI does.
    expect(lookupNaiModel('NovelAI Diffusion V5 00000000')).toEqual({ id: 'nai-diffusion-5-curated', name: 'NAI Diffusion V5 Curated', hash: '00000000' })
    expect(lookupNaiModel('NovelAI Diffusion V4.5 00000000').id).toBe('nai-diffusion-4-5-curated')
    expect(lookupNaiModel('Something Else 00000000')).toEqual({ id: 'unknown', name: 'Unknown', hash: '00000000' })
    expect(lookupNaiModel(undefined)).toEqual({ id: 'unknown', name: 'Unknown' })
  })

  it('does not claim a generic `Comment` chunk for NovelAI', async () => {
    const meta = await read([tEXt('Comment', '{"prompt": "x", "steps": 5}'), tEXt('Software', 'GIMP')])

    expect(meta.generator).toBeNull()
    expect(meta.signature).toBeNull()
  })
})

describe('comfyui', () => {
  const warnings = (): MetaWarning[] => []

  it('takes the first-pass sampler (fed by EmptyLatentImage), not the hires pass', async () => {
    // The hires sampler has the lower id, so it is listed first.
    const graph = {
      1: { class_type: 'KSampler', inputs: { seed: 111, steps: 12, cfg: 4, sampler_name: 'dpmpp_2m', scheduler: 'karras', denoise: 0.45, model: ['20', 0], positive: ['11', 0], negative: ['12', 0], latent_image: ['9', 0] } },
      3: { class_type: 'KSampler', inputs: { seed: 42, steps: 28, cfg: 6.0, sampler_name: 'euler_ancestral', scheduler: 'normal', denoise: 1.0, model: ['21', 0], positive: ['6', 0], negative: ['7', 0], latent_image: ['5', 0] } },
      5: { class_type: 'EmptyLatentImage', inputs: { width: 832, height: 1216, batch_size: 1 } },
      6: { class_type: 'CLIPTextEncode', inputs: { text: '1girl, silver hair', clip: ['20', 1] } },
      7: { class_type: 'CLIPTextEncode', inputs: { text: 'lowres', clip: ['20', 1] } },
      9: { class_type: 'LatentUpscale', inputs: { samples: ['3', 0], width: 1248, height: 1824 } },
      11: { class_type: 'CLIPTextEncode', inputs: { text: 'hires detail prompt', clip: ['20', 1] } },
      12: { class_type: 'CLIPTextEncode', inputs: { text: 'hires negative', clip: ['20', 1] } },
      20: { class_type: 'CheckpointLoaderSimple', inputs: { ckpt_name: 'illustriousXL_v01.safetensors' } },
      21: { class_type: 'LoraLoader', inputs: { lora_name: 'detailer.safetensors', strength_model: 0.6, strength_clip: 1, model: ['20', 0], clip: ['20', 1] } },
    }
    const meta = await read([tEXt('prompt', JSON.stringify(graph)), tEXt('workflow', '{"nodes":[],"links":[]}')])

    expect(meta.generator).toBe('comfyui')
    expect(meta.params).toMatchObject({
      prompt: '1girl, silver hair',
      negative: 'lowres',
      seed: 42,
      steps: 28,
      cfg: 6,
      sampler: 'euler_ancestral',
      scheduler: 'normal',
      width: 832,
      height: 1216,
      model: 'illustriousXL_v01.safetensors',
      resources: [{ kind: 'lora', name: 'detailer.safetensors', weight: 0.6 }],
      extra: { denoise: '1', sampler_node: 'KSampler' },
    })
    expect(meta.warnings).toEqual([])
  })

  it('yields no prompt when text runs through an unknown node — and still returns the raw chunk', async () => {
    const graph = {
      3: { class_type: 'KSampler', inputs: { seed: 1, steps: 20, cfg: 7, sampler_name: 'euler', scheduler: 'normal', positive: ['6', 0], negative: ['7', 0], latent_image: ['5', 0] } },
      5: { class_type: 'EmptyLatentImage', inputs: { width: 512, height: 512 } },
      6: { class_type: 'MysteryPromptRouter', inputs: { payload: 'the text is in here', mode: 3 } },
      7: { class_type: 'CLIPTextEncode', inputs: { text: 'known negative' } },
    }
    const json = JSON.stringify(graph)
    const meta = await read([tEXt('prompt', json)])

    expect(meta.generator).toBe('comfyui')
    expect(meta.params!.prompt).toBeUndefined()
    expect(meta.params!.negative).toBe('known negative')
    expect(meta.params!.steps).toBe(20)
    expect(meta.entries).toEqual([{ source: 'png:tEXt', key: 'prompt', text: json }])
    expect(meta.entries[0]!.text).toContain('the text is in here')
    expect(meta.params!.raw).toEqual(graph)
  })

  it('sanitises Python NaN / Infinity tokens outside strings only', () => {
    expect(parseJson('{"a": NaN, "b": [Infinity, -Infinity], "c": "NaN and Infinity stay"}', { sanitize: true }))
      .toEqual({ a: null, b: [null, null], c: 'NaN and Infinity stay' })
    expect(parseJson('{"a": NaN}')).toBeUndefined()

    const p = parseComfyUI('{"1": {"class_type": "KSampler", "inputs": {"cfg": NaN, "steps": 9, "positive": ["2", 0], "negative": ["2", 0]}}, "2": {"class_type": "CLIPTextEncode", "inputs": {"text": "NaN girl"}}}', undefined, warnings())

    expect(p).toMatchObject({ prompt: 'NaN girl', steps: 9 })
    expect(p!.cfg).toBeUndefined()
  })

  it('walks conditioning pass-throughs, SDXL encoders, string nodes and concatenation', () => {
    const graph = {
      s: { class_type: 'KSamplerAdvanced', inputs: { noise_seed: ['seed', 0], steps: 30, cfg: 5, sampler_name: 'euler', scheduler: 'sgm_uniform', positive: ['cn', 0], negative: ['cn', 1], latent_image: ['l', 0] } },
      l: { class_type: 'EmptySD3LatentImage', inputs: { width: ['w', 0], height: 1024 } },
      w: { class_type: 'PrimitiveInt', inputs: { value: 1152 } },
      seed: { class_type: 'Seed (rgthree)', inputs: { seed: 987654321 } },
      cn: { class_type: 'ControlNetApplyAdvanced', inputs: { positive: ['fg', 0], negative: ['neg', 0], strength: 0.8 } },
      fg: { class_type: 'FluxGuidance', inputs: { conditioning: ['xl', 0], guidance: 3.5 } },
      xl: { class_type: 'CLIPTextEncodeSDXL', inputs: { text_g: ['cat', 0], text_l: ['cat', 0] } },
      cat: { class_type: 'StringConcatenate', inputs: { string_a: ['a', 0], string_b: 'city at night', delimiter: ', ' } },
      a: { class_type: 'PrimitiveString', inputs: { value: '1girl' } },
      neg: { class_type: 'ConditioningCombine', inputs: { conditioning_1: ['n1', 0], conditioning_2: ['n2', 0] } },
      n1: { class_type: 'CLIPTextEncode', inputs: { text: 'lowres' } },
      n2: { class_type: 'ImpactWildcardEncode', inputs: { text: '__bad__', populated_text: 'blurry' } },
    }
    const p = parseComfyUI(JSON.stringify(graph), undefined, warnings())

    expect(p).toMatchObject({ prompt: '1girl, city at night', negative: 'lowres, blurry', seed: 987654321, width: 1152, height: 1024, steps: 30, scheduler: 'sgm_uniform' })
  })

  it('reads a Flux-style SamplerCustomAdvanced through its helper nodes', () => {
    const graph = {
      13: { class_type: 'SamplerCustomAdvanced', inputs: { noise: ['25', 0], guider: ['22', 0], sampler: ['16', 0], sigmas: ['17', 0], latent_image: ['27', 0] } },
      16: { class_type: 'KSamplerSelect', inputs: { sampler_name: 'euler' } },
      17: { class_type: 'BasicScheduler', inputs: { scheduler: 'simple', steps: 20, denoise: 1, model: ['12', 0] } },
      22: { class_type: 'BasicGuider', inputs: { model: ['12', 0], conditioning: ['26', 0] } },
      25: { class_type: 'RandomNoise', inputs: { noise_seed: 5 } },
      26: { class_type: 'FluxGuidance', inputs: { guidance: 3.5, conditioning: ['6', 0] } },
      27: { class_type: 'EmptySD3LatentImage', inputs: { width: 1024, height: 1024 } },
      6: { class_type: 'CLIPTextEncode', inputs: { text: 'a flux prompt' } },
      12: { class_type: 'UNETLoader', inputs: { unet_name: 'flux1-dev.safetensors' } },
    }

    expect(parseComfyUI(JSON.stringify(graph), undefined, warnings())).toMatchObject({
      prompt: 'a flux prompt',
      seed: 5,
      steps: 20,
      sampler: 'euler',
      scheduler: 'simple',
      model: 'flux1-dev.safetensors',
      width: 1024,
    })
  })

  it('falls back to `workflow` only through text-encode nodes wired to the sampler, and says so', () => {
    const workflow = {
      nodes: [
        { id: 3, type: 'KSampler', inputs: [{ name: 'positive', link: 4 }, { name: 'negative', link: 6 }], widgets_values: [1, 'fixed', 20] },
        { id: 6, type: 'CLIPTextEncode', widgets_values: ['from the workflow'] },
        { id: 7, type: 'CLIPTextEncode', widgets_values: ['wf negative'] },
        // Stale text in a node that is not a text encoder must never be used.
        { id: 8, type: 'ShowText', widgets_values: ['stale widget text'] },
      ],
      links: [[4, 6, 0, 3, 1, 'CONDITIONING'], [6, 7, 0, 3, 2, 'CONDITIONING']],
    }
    const w = warnings()
    const p = parseComfyUI(undefined, JSON.stringify(workflow), w)

    expect(p).toMatchObject({ generator: 'comfyui', prompt: 'from the workflow', negative: 'wf negative' })
    expect(w).toEqual([{ code: 'comfy_workflow_fallback' }])

    const rewired = { ...workflow, links: [[4, 8, 0, 3, 1, 'CONDITIONING']] }

    expect(parseComfyUI(undefined, JSON.stringify(rewired), warnings())!.prompt).toBeUndefined()
  })

  it('prefers the API graph over workflow text when both are present', () => {
    const graph = { 3: { class_type: 'KSampler', inputs: { positive: ['6', 0], negative: ['6', 0] } }, 6: { class_type: 'CLIPTextEncode', inputs: { text: 'what actually ran' } } }
    const workflow = { nodes: [{ id: 3, type: 'KSampler', inputs: [{ name: 'positive', link: 1 }] }, { id: 6, type: 'CLIPTextEncode', widgets_values: ['stale'] }], links: [[1, 6, 0, 3, 1, 'CONDITIONING']] }
    const w = warnings()

    expect(parseComfyUI(JSON.stringify(graph), JSON.stringify(workflow), w)!.prompt).toBe('what actually ran')
    expect(w).toEqual([])
  })
})

describe('swarmui', () => {
  const json = JSON.stringify({
    sui_image_params: { prompt: 'a cat', negativeprompt: 'dog', model: 'OfficialStableDiffusion/sd_xl_base_1.0', seed: 1234, steps: 20, cfgscale: 7.5, aspectratio: '1:1', width: 1024, height: 1024, sampler: 'euler', scheduler: 'karras', swarm_version: '0.9.6.2' },
    sui_extra_data: { date: '2026-01-02', generation_time: '3.2 sec' },
    sui_models: [{ name: 'sd_xl_base_1.0.safetensors', param: 'model', hash: `0x${'ab'.repeat(32)}` }],
  })

  it('reads sui_image_params from a PNG `parameters` chunk (before A1111 gets a look)', async () => {
    const meta = await read([tEXt('parameters', json)])

    expect(meta.generator).toBe('swarmui')
    expect(meta.params).toMatchObject({
      prompt: 'a cat',
      negative: 'dog',
      model: 'OfficialStableDiffusion/sd_xl_base_1.0',
      modelHash: `0x${'ab'.repeat(32)}`,
      seed: 1234,
      steps: 20,
      cfg: 7.5,
      width: 1024,
      height: 1024,
      sampler: 'euler',
      scheduler: 'karras',
      extra: { aspectratio: '1:1', swarm_version: '0.9.6.2' },
    })
  })

  it('reads a stealth_rgbcomp payload by content', async () => {
    const [width, height] = [64, 64]
    const rgba = noise(width, height)

    embedStealth(rgba, width, height, 'stealth_rgbcomp', json)

    const meta = await readImageMeta(encodePng({ width, height, rgba, colorType: 2 }))

    expect(meta.generator).toBe('swarmui')
    expect(meta.via).toBe('stealth')
    expect(meta.params!.prompt).toBe('a cat')
  })
})

describe('invokeai', () => {
  it('reads invokeai_metadata', async () => {
    const meta = await read([
      tEXt('invokeai_metadata', JSON.stringify({ generation_mode: 'sdxl_txt2img', positive_prompt: 'a lighthouse', negative_prompt: 'fog', width: 1024, height: 768, seed: 77, steps: 30, cfg_scale: 6.5, cfg_rescale_multiplier: 0.7, scheduler: 'dpmpp_2m_k', model: { name: 'juggernautXL', hash: 'blake3:abcd', base: 'sdxl' }, app_version: '5.4.2' })),
      tEXt('invokeai_graph', '{}'),
    ])

    expect(meta.generator).toBe('invokeai')
    expect(meta.params).toMatchObject({ prompt: 'a lighthouse', negative: 'fog', width: 1024, height: 768, seed: 77, steps: 30, cfg: 6.5, rescale: 0.7, sampler: 'dpmpp_2m_k', model: 'juggernautXL', modelHash: 'blake3:abcd', extra: { generation_mode: 'sdxl_txt2img', app_version: '5.4.2' } })
  })

  it('reads legacy sd-metadata, with the negative in [brackets]', async () => {
    const meta = await read([tEXt('sd-metadata', JSON.stringify({ model: 'stable diffusion', model_weights: 'stable-diffusion-1.5', model_hash: 'cc6cb27103', app_id: 'invoke-ai/InvokeAI', app_version: '2.3.0', image: { prompt: [{ prompt: 'a castle on a hill [blurry] [low quality, text]', weight: 1 }], steps: 50, cfg_scale: 7.5, seed: 3141592653, width: 512, height: 640, sampler: 'k_lms', type: 'txt2img' } }))])

    expect(meta.generator).toBe('invokeai')
    expect(meta.params).toMatchObject({ prompt: 'a castle on a hill', negative: 'blurry, low quality, text', steps: 50, cfg: 7.5, seed: 3141592653, width: 512, height: 640, sampler: 'k_lms', model: 'stable-diffusion-1.5', modelHash: 'cc6cb27103' })
  })

  it('reads the legacy Dream CLI string', async () => {
    const meta = await read([tEXt('Dream', '"a \\"quoted\\" forest [fog, rain]" -s 50 -S 1234567 -W 512 -H 704 -C 7.5 -A k_euler_a -G 0.8')])

    expect(meta.generator).toBe('invokeai')
    expect(meta.params).toMatchObject({ prompt: 'a "quoted" forest', negative: 'fog, rain', steps: 50, seed: 1234567, width: 512, height: 704, cfg: 7.5, sampler: 'k_euler_a', extra: { '-G': '0.8' } })
    expect(parseDream('no quotes here -s 5')).toBeNull()
  })
})

describe('pulsar', () => {
  const own: PulsarMetaV1 = {
    v: 1,
    prompt: '1girl, silver hair, (smile:1.2)',
    negative: 'hat',
    characters: ['girl, red eyes', 'boy, glasses'],
    width: 832,
    height: 1216,
    steps: 28,
    cfg: 5.5,
    sampler: 'euler_ancestral',
    scheduler: 'normal',
    rescale: 0.7,
    seed: 4242,
    lineage: 'illustrious',
    style: 'mihoyo-genshin',
    kind: 'txt2img',
  }
  const a1111Copy = '1girl, silver hair, (smile:1.2)\nNegative prompt: hat\nSteps: 28, Sampler: euler_ancestral, CFG scale: 5.5, Seed: 4242, Size: 832x1216'

  it('reads a pulsar download: XMP, the A1111-compatible copy and the `pulsar` chunk — and the pulsar chunk wins', async () => {
    const meta = await read([
      iTXt('XML:com.adobe.xmp', '<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:Description Iptc4xmpExt:DigitalSourceType="…/trainedAlgorithmicMedia"/></x:xmpmeta>'),
      tEXt('parameters', a1111Copy),
      iTXt('pulsar', JSON.stringify(own)),
    ])

    expect(meta.generator).toBe('pulsar')
    expect(meta.entries.map(e => e.key)).toEqual(['XML:com.adobe.xmp', 'parameters', 'pulsar'])
    expect(meta.params!.pulsar).toEqual(own)
    expect(meta.params).toMatchObject({
      prompt: own.prompt,
      negative: 'hat',
      characters: [{ prompt: 'girl, red eyes' }, { prompt: 'boy, glasses' }],
      width: 832,
      height: 1216,
      steps: 28,
      cfg: 5.5,
      rescale: 0.7,
      seed: 4242,
      sampler: 'euler_ancestral',
      scheduler: 'normal',
      extra: { lineage: 'illustrious', style: 'mihoyo-genshin', kind: 'txt2img' },
    })
    expect(meta.signature).toBeNull()
  })

  it('falls through to the A1111 copy when `v` is not one it knows, and says so', async () => {
    const meta = await read([tEXt('parameters', a1111Copy), iTXt('pulsar', JSON.stringify({ ...own, v: 2 }))])

    expect(meta.generator).toBe('a1111')
    expect(meta.params!.seed).toBe(4242)
    expect(meta.warnings).toEqual([{ code: 'pulsar_unknown_version' }])
  })

  it('drops wrongly-typed fields instead of guessing', () => {
    const parsed = parsePulsar('{"v":1,"prompt":7,"negative":null,"characters":["ok",3,{"prompt":"x"}],"width":"832","steps":20,"seed":1e400,"kind":"pose"}')

    expect(parsed.ok && parsed.params.pulsar).toEqual({ v: 1, characters: ['ok'], steps: 20, kind: 'pose' })
    expect(parsePulsar('[1]')).toEqual({ ok: false, reason: 'bad_json' })
    expect(parsePulsar('{"v":"1"}')).toEqual({ ok: false, reason: 'unknown_version' })
  })
})

describe('detection order', () => {
  const entry = (key: string, text: string): RawEntry => ({ source: 'png:tEXt', key, text })
  const detect = (entries: RawEntry[]) => detectContainer(entries, [])?.generator ?? null

  it('is pulsar → NovelAI → SwarmUI → A1111 → InvokeAI → ComfyUI', () => {
    const all = [
      entry('prompt', '{"1":{"class_type":"KSampler","inputs":{}}}'),
      entry('invokeai_metadata', '{"positive_prompt":"x"}'),
      entry('parameters', 'a\nSteps: 1, Sampler: b, Seed: 2'),
      entry('Software', 'NovelAI'),
      entry('Comment', '{"prompt": "x"}'),
      entry('pulsar', '{"v":1}'),
    ]

    expect(detect(all)).toBe('pulsar')
    expect(detect(all.slice(0, 5))).toBe('novelai')
    expect(detect([...all.slice(0, 2), entry('parameters', '{"sui_image_params":{"prompt":"x"}}')])).toBe('swarmui')
    expect(detect(all.slice(0, 3))).toBe('a1111')
    expect(detect(all.slice(0, 2))).toBe('invokeai')
    expect(detect(all.slice(0, 1))).toBe('comfyui')
    expect(detect([entry('Title', 'holiday')])).toBeNull()
  })

  it('leaves JSON under `parameters` from other tools uninterpreted', () => {
    expect(detect([entry('parameters', '{"Prompt": "x", "software": "RuinedFooocus"}'), entry('fooocus_scheme', 'fooocus')])).toBeNull()
  })

  it('dispatches stealth payloads by content', () => {
    expect(detectStealth('a\nSteps: 1, Sampler: b, Seed: 2')?.generator).toBe('a1111')
    expect(detectStealth('{"sui_image_params":{"prompt":"x"}}')?.generator).toBe('swarmui')
    expect(detectStealth(JSON.stringify(NAI_0001))?.generator).toBe('novelai')
    expect(detectStealth('{"something":"else"}')).toBeNull()
  })
})
