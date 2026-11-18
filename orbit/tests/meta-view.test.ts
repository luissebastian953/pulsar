import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import MetaView from '~/components/MetaView.vue'
import { readImageMeta } from '~/utils/imgmeta/read'
import type { GenParams, ImageMeta, NaiSignatureStatus } from '~/utils/imgmeta/types'
import { iTXt, shellPng, tEXt } from './fixtures/imgmeta'

// The inspector shows untrusted file content inside an app that holds a
// bearer token. Whatever a file says must reach the DOM as text.
const PAYLOADS = [
  '<img src=x onerror="window.__pwned=1">',
  '<script>window.__pwned=1</script>',
  '<svg onload="window.__pwned=1">',
  '<a href="javascript:window.__pwned=1">click</a>',
  '<iframe src="javascript:window.__pwned=1"></iframe>',
  '{{ window.__pwned = 1 }}',
  '"><img src=x onerror=window.__pwned=1>',
]
const HOSTILE = PAYLOADS.join(' ')
const FORBIDDEN = 'img, script, svg, iframe, a, object, embed, link, style, form, input, video, audio'

const pwned = () => (window as unknown as { __pwned?: number }).__pwned

function metaOf(params: GenParams | null, over: Partial<ImageMeta> = {}): ImageMeta {
  return {
    format: 'png', width: 64, height: 64, entries: [], generator: params?.generator ?? null,
    via: params ? 'container' : null, params, signature: null, warnings: [], ...over,
  }
}

afterEach(() => {
  document.body.innerHTML = ''
  delete (window as unknown as { __pwned?: number }).__pwned
})

describe('MetaView renders metadata as text', () => {
  it('a real PNG whose every text chunk is an HTML/script payload creates no element', async () => {
    const png = shellPng([
      tEXt('parameters', `${HOSTILE}\nNegative prompt: ${HOSTILE}\nSteps: 20, Sampler: ${PAYLOADS[0]}, CFG scale: 7, Model: "${PAYLOADS[1]!.replaceAll('"', '\\"')}", Lora hashes: "${PAYLOADS[3]!.replaceAll('"', '\\"')}"`),
      tEXt(PAYLOADS[0]!.slice(0, 60), HOSTILE),
      iTXt('Comment', HOSTILE),
    ])
    const meta = await readImageMeta(png, { stealth: 'never' })

    expect(meta.generator).toBe('a1111')
    expect(meta.params!.prompt).toBe(HOSTILE)

    for (const raw of [false, true]) {
      const w = mount(MetaView, { props: { meta, raw }, attachTo: document.body })

      expect(w.element.querySelector(FORBIDDEN)).toBeNull()
      // the icon stub is the only non-text element type besides layout tags
      expect([...w.element.querySelectorAll('*')].every(el => ['DIV', 'DL', 'DT', 'DD', 'PRE', 'SECTION', 'SPAN', 'UL', 'LI', 'I'].includes(el.tagName))).toBe(true)
      // …and the payload is there, verbatim, as text
      expect(w.text()).toContain('<img src=x onerror="window.__pwned=1">')
      expect(w.text()).toContain('<script>window.__pwned=1</script>')
      expect(w.html()).toContain('&lt;img src=x onerror=')
      // no real tag: the only `<img` etc. in the markup are escaped (&lt;img)
      expect(w.html()).not.toMatch(/<(?:img|script|svg|iframe|a)[\s>]/i)
      expect(pwned()).toBeUndefined()
      w.unmount()
    }
  })

  it('every field of every view is text: header, prompts, characters, settings, resources, raw keys, warnings', () => {
    const params: GenParams = {
      generator: 'novelai',
      prompt: HOSTILE, negative: HOSTILE,
      characters: [{ prompt: HOSTILE, negative: HOSTILE }],
      sampler: HOSTILE, scheduler: HOSTILE, model: HOSTILE, modelHash: HOSTILE,
      extra: { [HOSTILE]: HOSTILE },
      resources: [{ kind: HOSTILE, name: HOSTILE, hash: HOSTILE }],
      novelai: {
        title: HOSTILE, description: HOSTILE, software: HOSTILE, source: HOSTILE,
        model: { id: 'unknown', name: HOSTILE }, requestTypeLabel: HOSTILE,
        vibeTransfer: false, vibeEmbedded: false, preciseReference: false, smea: null, signed: true,
      },
    }
    const meta = metaOf(params, {
      via: 'stealth',
      entries: [{ source: 'png:tEXt', key: HOSTILE, text: HOSTILE }, { source: 'stealth', key: 'stealth_pngcomp', text: HOSTILE }],
      warnings: [{ code: 'bad_json', detail: HOSTILE }],
    })

    for (const raw of [false, true]) {
      const w = mount(MetaView, { props: { meta, raw }, attachTo: document.body })

      expect(w.element.querySelector(FORBIDDEN)).toBeNull()
      expect(w.html()).not.toMatch(/<(?:img|script|svg|iframe|a)[\s>]/i)
      expect(w.text()).toContain(PAYLOADS[0])
      expect(pwned()).toBeUndefined()
      w.unmount()
    }
  })

  it('makes control and bidi characters visible instead of emitting them', () => {
    const meta = metaOf({ generator: 'a1111', prompt: 'a\u0000b\u001b[31mc‮d', characters: [], extra: {} },
      { entries: [{ source: 'png:tEXt', key: 'k\u0007', text: 'x\u0085y' }] })
    const simple = mount(MetaView, { props: { meta, raw: false } })

    expect(simple.get('[data-test="meta-prompt"]').text()).toBe('a\\x00b\\x1b[31mc\\u202ed')

    const rawView = mount(MetaView, { props: { meta, raw: true } })

    expect(rawView.get('[data-test="meta-entry"]').text()).toContain('k\\x07')
    expect(rawView.get('[data-test="meta-entry"]').text()).toContain('x\\x85y')
    // eslint-disable-next-line no-control-regex
    expect(rawView.html()).not.toMatch(/[\u0000\u0007\u001b\u0085‮]/)
  })
})

describe('MetaView content', () => {
  const nai = (signature: NaiSignatureStatus | null): ImageMeta => metaOf({
    generator: 'novelai', prompt: '1girl', negative: 'lowres', characters: [{ prompt: 'girl' }], width: 832, height: 1216,
    steps: 23, cfg: 7, seed: 5, sampler: 'k_euler_ancestral', scheduler: 'karras', model: 'NAI Diffusion V5 Curated', extra: {},
    novelai: {
      software: 'NovelAI', source: 'NovelAI Diffusion V5 DB276663', model: { id: 'v5', name: 'NAI Diffusion V5 Curated', hash: 'DB276663' },
      requestTypeLabel: 'Text to Image', vibeTransfer: false, vibeEmbedded: false, preciseReference: false, smea: null, signed: true,
    },
  }, { signature })

  it('shows "Signed by NovelAI ✓" only for a valid signature — nothing otherwise (D21)', () => {
    const valid = mount(MetaView, { props: { meta: nai('valid'), raw: false } })

    expect(valid.get('[data-test="nai-signed"]').text()).toBe('Signed by NovelAI ✓')

    for (const s of ['invalid', 'no_signature', 'unsupported', 'unavailable', null] as const) {
      const w = mount(MetaView, { props: { meta: nai(s), raw: false } })

      expect(w.find('[data-test="nai-signed"]').exists()).toBe(false)
      expect(w.text()).not.toMatch(/sign|✓|invalid|verif/i)
    }

    // a valid status on a non-NovelAI image is not a NovelAI signature
    const other = mount(MetaView, { props: { meta: metaOf({ generator: 'a1111', prompt: 'x', characters: [], extra: {} }, { signature: 'valid' }), raw: false } })

    expect(other.find('[data-test="nai-signed"]').exists()).toBe(false)
  })

  it('shows Software, model, prompts and settings in the simplified view', () => {
    const w = mount(MetaView, { props: { meta: nai(null), raw: false } })

    expect(w.get('[data-test="meta-made-with"]').text()).toBe('NovelAI')
    expect(w.get('[data-test="meta-software"]').text()).toBe('NovelAI')
    expect(w.get('[data-test="meta-source"]').text()).toBe('NovelAI Diffusion V5 DB276663 (NAI Diffusion V5 Curated)')
    expect(w.get('[data-test="meta-prompt"]').text()).toBe('1girl')
    expect(w.get('[data-test="meta-negative"]').text()).toBe('lowres')
    expect(w.get('[data-test="meta-character"]').text()).toBe('girl')
    expect(w.text()).toContain('832 × 1216')
    expect(w.text()).toContain('k_euler_ancestral · karras')
    expect(w.find('[data-test="meta-raw"]').exists()).toBe(false)
  })

  it('raw view lists every entry with its key and where it was found', () => {
    const meta = metaOf({ generator: 'a1111', prompt: 'x', characters: [], extra: {} }, {
      entries: [{ source: 'png:tEXt', key: 'parameters', text: 'x\nSteps: 1' }, { source: 'exif', key: 'UserComment', text: 'hello' }],
    })
    const w = mount(MetaView, { props: { meta, raw: true } })
    const entries = w.findAll('[data-test="meta-entry"]')

    expect(entries.map(e => e.text())).toEqual(['parametersPNG tEXtx\nSteps: 1', 'UserCommentEXIFhello'])
    expect(entries[0]!.get('pre').classes()).toContain('whitespace-pre-wrap')
    expect(w.find('[data-test="meta-prompt"]').exists()).toBe(false)
  })

  it('falls back to the raw entries when nothing is recognised', () => {
    const w = mount(MetaView, { props: { meta: metaOf(null, { entries: [{ source: 'png:tEXt', key: 'Comment', text: 'hi' }] }), raw: false } })

    expect(w.get('[data-test="meta-none"]').text()).toContain('No generation metadata was recognised')
    expect(w.findAll('[data-test="meta-entry"]')).toHaveLength(1)

    const empty = mount(MetaView, { props: { meta: metaOf(null), raw: false } })

    expect(empty.text()).toContain('This file carries no metadata text.')
  })

  it('cuts a very long entry for display', () => {
    const w = mount(MetaView, { props: { meta: metaOf(null, { entries: [{ source: 'png:tEXt', key: 'k', text: 'a'.repeat(100_500) }] }), raw: true } })

    expect(w.get('pre').text()).toMatch(/a{1000}\n… \(500 more characters not shown\)$/)
  })

  it('explains warnings in words', () => {
    const w = mount(MetaView, { props: { meta: metaOf(null, { warnings: [{ code: 'pixels_over_cap', detail: '9000x9000' }] }), raw: false } })

    expect(w.get('[data-test="meta-warnings"]').text()).toContain('too large to scan its pixels')
    expect(w.get('[data-test="meta-warnings"]').text()).toContain('(9000x9000)')
  })
})
