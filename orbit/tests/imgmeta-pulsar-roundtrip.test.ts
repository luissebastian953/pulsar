// @vitest-environment node
import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { handleMetaRequest } from '~/utils/imgmeta/worker-protocol'
import { readImageMeta } from '~/utils/imgmeta/read'
import type { PulsarMetaV1 } from '~/utils/imgmeta/types'
import { buildMetaImport, DEFAULT_IMPORT_OPTIONS } from '~/utils/meta-import'
import { fakeParse } from './fixtures/fake-parse'

// The round trip writer → reader. `fixtures/pulsar-download.png` is a real
// `GET /images/{id}?meta=1` response from core: its e2e test
// (core/internal/api/outmeta_e2e_test.go, TestDownloadFixtureMatchesOrbit)
// writes this file and fails if core's output ever drifts from it. So what is
// parsed here is byte-for-byte what core serves.
const fixture = new Uint8Array(readFileSync(new URL('./fixtures/pulsar-download.png', import.meta.url)))

describe('a pulsar download read back by the inspector', () => {
  it('is recognised as pulsar, from the container, with every field', async () => {
    const meta = await readImageMeta(fixture)

    expect(meta.format).toBe('png')
    expect(meta.generator).toBe('pulsar')
    expect(meta.via).toBe('container')
    expect(meta.signature).toBeNull()
    expect(meta.warnings).toEqual([])
    // the three chunks core writes, in order, all iTXt — and nothing else
    expect(meta.entries.map(e => [e.source, e.key])).toEqual([
      ['png:iTXt', 'XML:com.adobe.xmp'], ['png:iTXt', 'parameters'], ['png:iTXt', 'pulsar'],
    ])

    const own: PulsarMetaV1 = {
      v: 1,
      prompt: '2girls, garden, (red dress:1.2285), <not html>, rim light, golden hour',
      negative: 'blurry, (extra fingers:1.1748), hat',
      characters: ['silver hair, (smile:1.1748)', 'black hair, school uniform'],
      width: 1216,
      height: 832,
      steps: 22,
      cfg: 5,
      sampler: 'euler',
      scheduler: 'normal',
      rescale: 0.55,
      seed: 4242424242,
      lineage: 'noobai',
      style: 'chibi',
      kind: 'txt2img',
      model: 'test-ckpt',
      hires_scale: 1.5,
      hires_denoise: 0.4,
      chunks: ['Light'],
      nai: {
        prompt: '2girls, garden, 1.3::red dress ::, <not html>, rim light, golden hour',
        negative: 'blurry, 1.2::extra fingers ::, hat',
        characters: ['silver hair, 1.2::smile ::', 'black hair, school uniform'],
      },
    }

    expect(meta.params!.pulsar).toEqual(own)
    expect(meta.params).toMatchObject({
      generator: 'pulsar',
      prompt: own.prompt,
      negative: own.negative,
      characters: own.characters.map(prompt => ({ prompt })),
      width: 1216,
      height: 832,
      steps: 22,
      cfg: 5,
      rescale: 0.55,
      seed: 4242424242,
      sampler: 'euler',
      scheduler: 'normal',
      model: 'test-ckpt',
    })
    expect(meta.params!.extra).toMatchObject({ lineage: 'noobai', style: 'chibi', kind: 'txt2img', chunks: 'Light' })
  })

  it('carries the AI-generated mark and an A1111-readable copy', async () => {
    const meta = await readImageMeta(fixture)
    const text = (key: string) => meta.entries.find(e => e.key === key)!.text

    expect(text('XML:com.adobe.xmp')).toContain('Iptc4xmpExt:DigitalSourceType="http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia"')
    expect(text('XML:com.adobe.xmp')).toContain('xmp:CreatorTool="pulsar"')

    const lines = text('parameters').split('\n')

    expect(lines[0]).toBe('2girls, garden, (red dress:1.2285), <not html>, rim light, golden hour')
    expect(lines[1]).toBe('Negative prompt: blurry, (extra fingers:1.1748), hat')
    expect(lines[2]).toMatch(/^Steps: 22, Sampler: Euler, Schedule type: Normal, CFG scale: 5, Seed: 4242424242, Size: 1216x832, Model: test-ckpt, /)
    expect(lines[2]).toMatch(/Version: pulsar$/)
  })

  it('never names NovelAI, an id, a path, or the config text', async () => {
    const meta = await readImageMeta(fixture)
    const all = meta.entries.map(e => e.text).join('\n')

    expect(all).not.toMatch(/novelai/i)
    expect(all).not.toMatch(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i)
    expect(all).not.toMatch(/\.safetensors|\/images\/|\/jobs\//)
    // the deployment's quality tags and default negative (core's test config)
    expect(all).not.toMatch(/masterpiece|best quality|lowres|bad anatomy/)
    expect(meta.entries.some(e => e.source === 'stealth')).toBe(false)
  })

  it('goes through the worker protocol the same way', async () => {
    const res = await handleMetaRequest({ id: 7, file: new Blob([fixture]) })

    expect(res).toMatchObject({ id: 7, ok: true })
    expect(res.ok && res.meta.generator).toBe('pulsar')
  })

  it('imports losslessly: the text as typed, not the calibrated weights', async () => {
    const meta = await readImageMeta(fixture)
    const { patch } = await buildMetaImport(meta.params!, { ...DEFAULT_IMPORT_OPTIONS, settings: true, seed: true }, { parse: fakeParse() })

    expect(patch).toEqual({
      tagList: ['2girls', 'garden', '1.3::red dress ::', '<not html>', 'rim light', 'golden hour'],
      und: 'blurry, 1.2::extra fingers ::, hat',
      chars: ['silver hair, 1.2::smile ::', 'black hair, school uniform'],
      aspect: 'landscape',
      steps: 22,
      seedText: '4242424242',
    })
  })
})
