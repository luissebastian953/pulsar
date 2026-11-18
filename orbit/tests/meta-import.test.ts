import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import MetaImportDialog from '~/components/MetaImportDialog.vue'
import { resetChunksForTest, useChunks } from '~/composables/useChunks'
import { DOWNLOAD_METADATA_KEY, resetPromptSettingsForTest, usePromptSettings } from '~/composables/usePromptSettings'
import { useToast } from '~/composables/useToast'
import { downloadUrl } from '~/utils/download'
import { detectContainer } from '~/utils/imgmeta/detect'
import { parseA1111 } from '~/utils/imgmeta/parsers/a1111'
import type { GenParams, ImageMeta, RawEntry } from '~/utils/imgmeta/types'
import {
  applyMetaImport, buildMetaImport, DEFAULT_IMPORT_OPTIONS, importSources, retokenize, snapAspect,
  type ImportOptions, type ImportTarget, type MetaImport,
} from '~/utils/meta-import'
import { fakeParse, type FakeParseCall } from './fixtures/fake-parse'
import { NAI_0001 } from './fixtures/nai-0001'

// NovelAI's bundled example image, as the inspector reads it from PNG text.
function naiParams(edit: (comment: Record<string, unknown>) => void = () => {}): GenParams {
  const comment = JSON.parse(NAI_0001.Comment) as Record<string, unknown>

  edit(comment)

  const dict = { ...NAI_0001, Comment: JSON.stringify(comment) }
  const entries: RawEntry[] = Object.entries(dict).map(([key, text]) => ({ source: 'png:tEXt', key, text }))
  const hit = detectContainer(entries, [])

  expect(hit?.generator).toBe('novelai')

  return hit!.params
}

const all: ImportOptions = { prompt: true, negative: true, characters: true, settings: true, seed: true }

/** A composer as /create holds it, with settings an import must not touch. */
function composer() {
  const state = {
    mode: ref<'plain' | 'tags'>('plain'),
    tagList: ref<string[]>(['old tag']),
    und: ref('old negative'),
    chars: ref<{ text: string, enabled: boolean, refId: string }[]>([]),
    aspect: ref<'portrait' | 'landscape' | 'square'>('square'),
    steps: ref(28),
    seedText: ref(''),
    // not part of ImportTarget: an import has no way to reach these
    guidance: ref(5),
    sampler: ref('euler'),
    scheduler: ref('normal'),
    cfgRescale: ref(0.55),
    lineage: ref('noobai'),
  }
  const target: ImportTarget = state

  return { state, target }
}

describe('NovelAI import (D22)', () => {
  it('defaults: prompt, Undesired Content and characters on; settings and seed off', () => {
    expect(DEFAULT_IMPORT_OPTIONS).toEqual({ prompt: true, negative: true, characters: true, settings: false, seed: false })
  })

  it('fills prompt, UC and characters and leaves every setting alone', async () => {
    const calls: FakeParseCall[] = []
    const { patch, notices } = await buildMetaImport(naiParams(), { ...DEFAULT_IMPORT_OPTIONS }, { parse: fakeParse({ calls }) })

    // NovelAI's automatic quality suffix and UC preset are stripped (tag_hint_qt / tag_hint_uc_preset)
    expect(patch).toEqual({
      tagList: ['1girl', 'bed', 'candles', 'bedroom', 'location', 'evening'],
      und: '',
      chars: ['girl, doll joints'],
    })
    expect(notices.join('\n')).toMatch(/automatic quality tags were left out/)
    expect(notices.join('\n')).toMatch(/Undesired Content preset was left out/)
    // every text went through core's parser, with clean on — orbit split nothing itself
    expect(calls.length).toBeGreaterThanOrEqual(3)
    expect(calls.every(c => c.clean)).toBe(true)
    expect(calls.map(c => c.surface).sort()).toEqual(['character', 'negative', 'prompt'])

    const { state, target } = composer()

    applyMetaImport(target, patch)
    expect(state.mode.value).toBe('tags')
    expect(state.tagList.value).toEqual(patch.tagList)
    expect(state.und.value).toBe('')
    expect(state.chars.value).toEqual([{ text: 'girl, doll joints', enabled: true, refId: '' }])
    // settings untouched: size, steps, seed — and CFG / sampler / schedule / rescale
    expect(state.aspect.value).toBe('square')
    expect(state.steps.value).toBe(28)
    expect(state.seedText.value).toBe('')
    expect(state.guidance.value).toBe(5)
    expect(state.sampler.value).toBe('euler')
    expect(state.scheduler.value).toBe('normal')
    expect(state.cfgRescale.value).toBe(0.55)
    expect(state.lineage.value).toBe('noobai')
  })

  it('with settings and seed on, imports only a bucket-snapped size, steps and seed — never CFG or sampler', async () => {
    const params = naiParams()

    // the file does carry them
    expect(params).toMatchObject({ cfg: 7, sampler: 'k_euler_ancestral', scheduler: 'karras', rescale: 0, width: 832, height: 1216, steps: 23, seed: 3216798432 })

    const { patch, notices } = await buildMetaImport(params, all, { parse: fakeParse() })

    expect(patch.aspect).toBe('portrait')
    expect(patch.steps).toBe(23)
    expect(patch.seedText).toBe('3216798432')
    // the patch type has no field for them, and the object carries none
    expect(Object.keys(patch).sort()).toEqual(['aspect', 'chars', 'seedText', 'steps', 'tagList', 'und'])
    expect(JSON.stringify(patch)).not.toMatch(/k_euler|karras|"cfg"|guidance|sampler|scheduler|rescale/)
    expect(notices.join('\n')).toMatch(/Guidance, sampler, schedule and rescale are never imported/)

    const { state, target } = composer()

    applyMetaImport(target, patch)
    expect([state.aspect.value, state.steps.value, state.seedText.value]).toEqual(['portrait', 23, '3216798432'])
    expect([state.guidance.value, state.sampler.value, state.scheduler.value, state.cfgRescale.value]).toEqual([5, 'euler', 'normal', 0.55])
  })

  it('keeps the quality tags and UC when NovelAI did not add them', async () => {
    const params = naiParams((c) => {
      c.tag_hint_qt = 0
      c.tag_hint_uc_preset = null
    })
    const { patch, notices } = await buildMetaImport(params, { ...DEFAULT_IMPORT_OPTIONS }, { parse: fakeParse() })

    expect(patch.tagList).toEqual(['1girl', 'bed', 'candles', 'bedroom', 'location', 'evening', 'very aesthetic', 'masterpiece', 'no text'])
    expect(patch.und).toMatch(/^lowres, artistic error, film grain/)
    expect(notices.join('\n')).not.toMatch(/left out/)
  })

  it('prefers actual_prompts and merges character UCs into the main UC with a notice', async () => {
    const params = naiParams((c) => {
      const neg = c.v4_negative_prompt as { caption: { base_caption: string, char_captions: { char_caption: string }[] } }

      neg.caption.base_caption = 'blurry'
      neg.caption.char_captions[0]!.char_caption = 'hat, glasses'
      c.tag_hint_uc_preset = null
      c.actual_prompts = {
        prompt: { base_caption: '1girl, red hair, very aesthetic, masterpiece, no text', char_captions: [{ char_caption: 'girl, smile' }] },
        negative_prompt: { base_caption: 'blurry, noise', char_captions: [] },
      }
    })

    expect(importSources(params)).toMatchObject({ prompt: '1girl, red hair, very aesthetic, masterpiece, no text', negative: 'blurry, noise' })

    const { patch, notices } = await buildMetaImport(params, { ...DEFAULT_IMPORT_OPTIONS }, { parse: fakeParse() })

    expect(patch.tagList).toEqual(['1girl', 'red hair'])
    expect(patch.chars).toEqual(['girl, smile'])
    expect(patch.und).toBe('blurry, noise, hat, glasses')
    expect(notices.join('\n')).toMatch(/Character Undesired Content was merged into the main Undesired Content/)

    // with UC off the character UC is dropped, and said so
    const off = await buildMetaImport(params, { ...DEFAULT_IMPORT_OPTIONS, negative: false }, { parse: fakeParse() })

    expect(off.patch.und).toBeUndefined()
    expect(off.notices.join('\n')).toMatch(/Character Undesired Content was left out/)
  })

  it('imports only what is switched on', async () => {
    const { patch } = await buildMetaImport(naiParams(), { prompt: false, negative: true, characters: false, settings: false, seed: false }, { parse: fakeParse() })

    expect(Object.keys(patch)).toEqual(['und'])
  })
})

describe('A1111 import', () => {
  const text = [
    'masterpiece, 1girl, <lora:Acheron_v2:0.8>, (blue hair:1.2), smile BREAK, red dress, embedding:EasyPos',
    'Negative prompt: embedding:EasyNegative, lowres, bad hands | extra fingers',
    'Steps: 30, Sampler: DPM++ 2M Karras, CFG scale: 7.5, Seed: 123456, Size: 768x1152, Model: animagine',
  ].join('\n')

  it('drops LoRA, embedding and pipe syntax through core, with notices, into a generatable prompt', async () => {
    const params = parseA1111(text)!
    const calls: FakeParseCall[] = []
    const { patch, notices } = await buildMetaImport(params, all, { parse: fakeParse({ calls }) })

    expect(patch.tagList).toEqual(['masterpiece', '1girl', '(blue hair:1.2)', 'smile BREAK', 'red dress'])
    expect(patch.und).toBe('lowres, bad hands, extra fingers')
    expect(patch.tagList!.join(', ')).not.toMatch(/lora|embedding/)
    expect(notices.some(n => /Acheron_v2/.test(n) && /LoRA picker/.test(n))).toBe(true)
    expect(notices.some(n => /embedding/i.test(n))).toBe(true)
    expect(notices.some(n => /`\|` was read as a tag break/.test(n))).toBe(true)
    // 768x1152 is not a bucket: snapped, steps kept, sampler/CFG ignored
    expect(patch).toMatchObject({ aspect: 'portrait', steps: 30, seedText: '123456' })
    expect(notices.some(n => /768 × 1152 snapped to 832 × 1216/.test(n))).toBe(true)
    expect(JSON.stringify(patch)).not.toMatch(/7\.5|DPM|Karras/)
    expect(calls.every(c => c.clean)).toBe(true)
  })

  it('drops a standalone BREAK', async () => {
    const { patch, notices } = await buildMetaImport(parseA1111('1girl, BREAK, red dress\nSteps: 20, Sampler: Euler a, CFG scale: 7')!, { ...DEFAULT_IMPORT_OPTIONS }, { parse: fakeParse() })

    expect(patch.tagList).toEqual(['1girl', 'red dress'])
    expect(notices.join('\n')).toMatch(/BREAK separators were dropped/)
  })

  it('moves negative-weight tags to Undesired Content', async () => {
    const params: GenParams = { generator: 'novelai', prompt: '1girl, -1::hat ::, smile', negative: 'blurry', characters: [], extra: {} }
    const { patch, notices } = await buildMetaImport(params, { ...DEFAULT_IMPORT_OPTIONS }, { parse: fakeParse() })

    expect(patch.tagList).toEqual(['1girl', 'smile'])
    expect(patch.und).toBe('blurry, hat')
    expect(notices.join('\n')).toMatch(/Negative-weight tags moved to Undesired Content: hat/)
  })

  it('imports text core cannot parse as written, and says so', async () => {
    const params: GenParams = { generator: 'a1111', prompt: '1girl, {unclosed', negative: 'ok', characters: [], extra: {} }
    const { patch, notices } = await buildMetaImport(params, { ...DEFAULT_IMPORT_OPTIONS }, { parse: fakeParse() })

    expect(patch.tagList).toEqual(['1girl, {unclosed'])
    expect(patch.und).toBe('ok')
    expect(notices.join('\n')).toMatch(/syntax pulsar can't read/)
  })

  it('still imports, uncleaned, when core is unreachable', async () => {
    const params: GenParams = { generator: 'a1111', prompt: '1girl, smile', negative: 'lowres', characters: [], extra: {} }
    const { patch, notices } = await buildMetaImport(params, { ...DEFAULT_IMPORT_OPTIONS }, { parse: fakeParse({ fail: true }) })

    expect(patch).toEqual({ tagList: ['1girl, smile'], und: 'lowres', chars: [] })
    expect(notices.join('\n')).toMatch(/Core is unreachable/)
  })

  it('refuses a seed it cannot represent exactly', async () => {
    const params: GenParams = { generator: 'a1111', prompt: 'x', characters: [], extra: {}, seed: 2 ** 60 }
    const { patch, notices } = await buildMetaImport(params, all, { parse: fakeParse() })

    expect(patch.seedText).toBeUndefined()
    expect(notices.join('\n')).toMatch(/seed is too large/)
  })
})

describe('re-tokenizing into prompt chunks (D11)', () => {
  const chunks = [
    { name: 'Light', content: 'rim light, golden hour' },
    { name: 'Solo', content: 'solo' }, // one tag: never re-tokenized
    { name: 'Fit', content: 'red dress, 1.2::thighhighs ::' },
  ]
  const deps = (calls?: FakeParseCall[]) => ({
    parse: fakeParse({ calls }),
    chunks,
    referenced: (texts: string[]) => chunks.filter(c => texts.some(t => t.includes(`!macro:${c.name}!`))),
  })
  const params = (prompt: string): GenParams => ({ generator: 'a1111', prompt, characters: [], extra: {} })

  it('replaces a whole-tag run that equals a chunk of ≥ 2 tags, with a notice', async () => {
    const { patch, notices } = await buildMetaImport(params('1girl, solo, rim light, golden hour, smile'), { ...DEFAULT_IMPORT_OPTIONS }, deps())

    expect(patch.tagList).toEqual(['1girl', 'solo', '!macro:Light!', 'smile'])
    expect(notices.join('\n')).toMatch(/replaced by your prompt chunk “Light”/)
  })

  it('matches weights exactly and never substrings', async () => {
    // "golden hour glow" contains the chunk's text but is a different tag
    let r = await buildMetaImport(params('rim light, golden hour glow'), { ...DEFAULT_IMPORT_OPTIONS }, deps())

    expect(r.patch.tagList).toEqual(['rim light', 'golden hour glow'])
    expect(r.notices.join('\n')).not.toMatch(/prompt chunk/)

    // same tags, different weight: not the chunk
    r = await buildMetaImport(params('red dress, thighhighs'), { ...DEFAULT_IMPORT_OPTIONS }, deps())
    expect(r.patch.tagList).toEqual(['red dress', 'thighhighs'])

    r = await buildMetaImport(params('red dress, 1.2::thighhighs ::'), { ...DEFAULT_IMPORT_OPTIONS }, deps())
    expect(r.patch.tagList).toEqual(['!macro:Fit!'])

    // out of order, or interrupted: not the chunk
    r = await buildMetaImport(params('golden hour, rim light, x, rim light, y, golden hour'), { ...DEFAULT_IMPORT_OPTIONS }, deps())
    expect(r.patch.tagList!.join(', ')).not.toMatch(/macro/)
  })

  it('asks core what each chunk expands to — orbit does not expand them', async () => {
    const calls: FakeParseCall[] = []

    await buildMetaImport(params('rim light, golden hour'), { ...DEFAULT_IMPORT_OPTIONS }, deps(calls))

    const expand = calls.find(c => !c.clean)!

    expect(expand.text).toBe('!macro:Light!, !macro:Solo!, !macro:Fit!')
    expect(expand.chunks.map(c => c.name)).toEqual(['Light', 'Solo', 'Fit'])
  })

  it('retokenize: longest chunk first, non-overlapping, repeated runs', () => {
    const piece = (nai: string) => ({ nai, name: nai.replace(/ /g, '_'), plain: true })
    const r = retokenize(['a', 'b', 'c', 'x', 'a', 'b'].map(piece), [
      { name: 'AB', nais: ['a', 'b'] }, { name: 'ABC', nais: ['a', 'b', 'c'] }, { name: 'One', nais: ['x'] },
    ])

    expect(r.pieces.map(p => p.nai)).toEqual(['!macro:ABC!', 'x', '!macro:AB!'])
    expect(r.used).toEqual(['ABC', 'AB'])
  })
})

describe('snapAspect', () => {
  it('snaps to the nearest bucket by aspect ratio', () => {
    expect(snapAspect(832, 1216)).toBe('portrait')
    expect(snapAspect(512, 768)).toBe('portrait')
    expect(snapAspect(1920, 1080)).toBe('landscape')
    expect(snapAspect(1024, 1024)).toBe('square')
    expect(snapAspect(1000, 1100)).toBe('square')
    expect(snapAspect(0, 100)).toBeNull()
    expect(snapAspect(Number.NaN, 100)).toBeNull()
  })
})

describe('applyMetaImport', () => {
  it('writes nothing for an empty patch (image-only import)', () => {
    const { state, target } = composer()
    const patch: MetaImport = { baseImage: new File(['x'], 'a.png', { type: 'image/png' }) }

    applyMetaImport(target, patch)
    expect(state.mode.value).toBe('plain')
    expect(state.tagList.value).toEqual(['old tag'])
    expect(state.und.value).toBe('old negative')
  })
})

describe('download setting (D16)', () => {
  beforeEach(() => {
    localStorage.clear()
    resetPromptSettingsForTest()
  })

  it('downloads carry metadata by default; "Download clean" asks for the plain URL', () => {
    const settings = usePromptSettings()

    expect(settings.downloadMetadata.value).toBe(true)
    expect(downloadUrl('http://core/images/abc', settings.downloadMetadata.value)).toBe('http://core/images/abc?meta=1')

    settings.downloadMetadata.value = false
    expect(downloadUrl('http://core/images/abc', settings.downloadMetadata.value)).toBe('http://core/images/abc')
  })

  it('is stored under pulsar.settings.downloadMetadata', async () => {
    usePromptSettings().downloadMetadata.value = false
    await flushPromises()
    expect(localStorage.getItem(DOWNLOAD_METADATA_KEY)).toBe('false')
  })

  it('appends meta=1 correctly and leaves non-core URLs alone', () => {
    expect(downloadUrl('http://core/images/abc-lineart', true)).toBe('http://core/images/abc-lineart?meta=1')
    expect(downloadUrl('http://core/images/abc?x=1', true)).toBe('http://core/images/abc?x=1&meta=1')
    expect(downloadUrl('http://core/images/abc#frag', true)).toBe('http://core/images/abc?meta=1#frag')
    expect(downloadUrl('blob:http://x/1', true)).toBe('blob:http://x/1')
    expect(downloadUrl('data:image/png;base64,AA', true)).toBe('data:image/png;base64,AA')
    expect(downloadUrl('', true)).toBe('')
  })
})

describe('MetaImportDialog', () => {
  const pending = ref<MetaImport | null>(null)
  const navigateTo = vi.fn(async () => {})
  const post = vi.fn()

  const meta = (params: GenParams | null): ImageMeta => ({
    format: 'png', width: 832, height: 1216, entries: [], generator: params?.generator ?? null,
    via: params ? 'container' : null, params, signature: null, warnings: [],
  })

  beforeEach(() => {
    const parse = fakeParse()

    pending.value = null
    navigateTo.mockClear()
    post.mockReset()
    post.mockImplementation(async (_path: string, body: Record<string, unknown>) =>
      parse(body.text as string, body.surface as never, { clean: body.clean as boolean, chunks: body.chunks as never }))
    vi.stubGlobal('useApi', () => ({ post }))
    vi.stubGlobal('useState', () => pending)
    vi.stubGlobal('navigateTo', navigateTo)
    localStorage.clear()
    resetChunksForTest()
    useToast().toasts.value = []
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  const file = new File(['png'], 'in.png', { type: 'image/png' })
  const open = (params: GenParams | null, props: Record<string, unknown> = {}) =>
    mount(MetaImportDialog, { props: { open: true, file, meta: meta(params), ...props }, attachTo: document.body })
  const q = (sel: string) => document.body.querySelector<HTMLElement>(`[data-test="${sel}"]`)

  it('offers prompt, UC and characters on, settings and seed off, and imports on one click', async () => {
    const w = open(naiParams())

    await flushPromises()
    expect(q('import-generator')!.textContent).toBe('Made with NovelAI')
    expect(q('import-prompt')!.getAttribute('aria-checked')).toBe('true')
    expect(q('import-negative')!.getAttribute('aria-checked')).toBe('true')
    expect(q('import-characters')!.getAttribute('aria-checked')).toBe('true')
    expect(q('import-settings')!.getAttribute('aria-checked')).toBe('false')
    expect(q('import-seed')!.getAttribute('aria-checked')).toBe('false')

    q('import-submit')!.click()
    await flushPromises()

    expect(pending.value).toEqual({
      tagList: ['1girl', 'bed', 'candles', 'bedroom', 'location', 'evening'],
      und: '',
      chars: ['girl, doll joints'],
    })
    expect(navigateTo).toHaveBeenCalledWith('/create')
    expect(w.emitted('update:open')!.at(-1)).toEqual([false])
    // every request was the parse endpoint with clean on: no upload, no generate
    expect(post.mock.calls.every(c => c[0] === '/prompt/parse' && (c[1] as { clean: boolean }).clean === true)).toBe(true)
    expect(useToast().toasts.value.length).toBeGreaterThan(0)
  })

  it('hands the file over for the composer to upload — it uploads nothing itself', async () => {
    open(null)
    await flushPromises()
    expect(q('import-generator')!.textContent).toBe('No generation metadata')
    expect((q('import-submit') as HTMLButtonElement).disabled).toBe(true)

    const [, base] = [...q('import-image-use')!.querySelectorAll('button')]

    base!.click()
    await flushPromises()
    expect((q('import-submit') as HTMLButtonElement).disabled).toBe(false)
    q('import-submit')!.click()
    await flushPromises()

    expect(pending.value).toEqual({ baseImage: file })
    expect(post).not.toHaveBeenCalled()
  })

  it('renders hostile metadata as text', async () => {
    const payload = '<img src=x onerror="window.__pwned=1"><script>window.__pwned=1</script>'

    open({ generator: 'a1111', prompt: payload, negative: payload, model: payload, characters: [{ prompt: payload }], extra: {} })
    await flushPromises()

    const dialog = q('meta-import')!

    expect(dialog.querySelector('img, script, iframe, a')).toBeNull()
    expect(q('import-prompt-preview')!.textContent).toBe(payload)
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined()
  })

  it('shows a read error instead of import options', async () => {
    open(null, { meta: null, error: 'too_large' })
    await flushPromises()
    expect(q('meta-import')!.textContent).toContain('over 50 MB')
    expect(q('import-image-use')).toBeNull()
    expect((q('import-submit') as HTMLButtonElement).disabled).toBe(true)
  })

  it('uses the local chunk store for re-tokenizing', async () => {
    useChunks().add({ name: 'Light', content: 'rim light, golden hour' })
    open({ generator: 'a1111', prompt: '1girl, rim light, golden hour', characters: [], extra: {} })
    await flushPromises()
    q('import-submit')!.click()
    await flushPromises()
    expect(pending.value).toEqual({ tagList: ['1girl', '!macro:Light!'] })
  })
})
