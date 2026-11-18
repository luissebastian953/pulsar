// @vitest-environment node
// Hostile files. Each case must be refused inside the caps — quickly, without
// allocating what the file claims — and must not take the rest of the file's
// metadata down with it.
import { deflateSync, gzipSync } from 'node:zlib'
import { describe, expect, it, vi } from 'vitest'
import { InflateCapError, inflateCapped } from '~/utils/imgmeta/inflate'
import { COMFY_VISIT_BUDGET, MAX_ENTRY_BYTES, MAX_FILE_BYTES, MAX_PIXELS, MAX_STEALTH_BYTES, MAX_TEXT_ENTRIES, MAX_TOTAL_BYTES } from '~/utils/imgmeta/limits'
import { decodePngPixels } from '~/utils/imgmeta/pixels'
import { walkPng } from '~/utils/imgmeta/png'
import { readImageMeta } from '~/utils/imgmeta/read'
import { readStealth } from '~/utils/imgmeta/stealth'
import { parseJson } from '~/utils/imgmeta/text'
import { Collector } from '~/utils/imgmeta/types'
import { bin, chunk, concat, encodePng, ihdr, iTXt, jpeg, noise, PNG_SIG, shellPng, stealthBits, tEXt, tiff, u32, webp, writeLsb } from './fixtures/imgmeta'

/** Fails the test if a parse that should be refused cheaply does real work. */
async function quickly<T>(run: () => Promise<T>, ms = 1500): Promise<T> {
  const start = performance.now()
  const result = await run()

  expect(performance.now() - start).toBeLessThan(ms)

  return result
}

describe('chunk lengths', () => {
  it('stops at a chunk claiming 0x7fffffff bytes and keeps what came before', async () => {
    const liar = concat(u32(0x7fffffff), bin('tEXt'), bin('parameters\0never read'))
    const file = concat(PNG_SIG, ihdr(8, 8), tEXt('before', 'kept'), liar, tEXt('after', 'unreachable'), chunk('IEND'))
    const meta = await quickly(() => readImageMeta(file, { stealth: 'never' }))

    expect(meta.entries).toEqual([{ source: 'png:tEXt', key: 'before', text: 'kept' }])
    expect(meta.warnings).toEqual([{ code: 'truncated', detail: 'tEXt' }])
  })

  it('treats a length above the PNG maximum the same way', async () => {
    const file = concat(PNG_SIG, ihdr(8, 8), concat(u32(0xffffffff), bin('zTXt'), bin('k\0\0')), chunk('IEND'))
    const meta = await quickly(() => readImageMeta(file, { stealth: 'never' }))

    expect(meta.entries).toEqual([])
    expect(meta.warnings).toEqual([{ code: 'truncated', detail: 'zTXt' }])
  })

  it('rejects a file whose very first chunk lies', async () => {
    await expect(readImageMeta(concat(PNG_SIG, u32(0x7fffffff), bin('IHDR'), new Uint8Array(17)))).rejects.toMatchObject({ code: 'malformed' })
    await expect(readImageMeta(concat(PNG_SIG, ihdr(0, 10), chunk('IEND')))).rejects.toMatchObject({ code: 'malformed' })
    await expect(readImageMeta(concat(PNG_SIG, tEXt('a', 'b'), ihdr(4, 4)))).rejects.toMatchObject({ code: 'malformed' })
  })

  it('handles truncated JPEG segments and RIFF chunks', async () => {
    const cut = jpeg({ exif: tiff({ ifd0: { 0x0131: 'x'.repeat(500) } }) }).subarray(0, 100)
    const meta = await readImageMeta(cut)

    expect(meta.entries).toEqual([])
    expect(meta.warnings[0]!.code).toBe('truncated')

    const riff = webp({ width: 8, height: 8, xmp: 'x'.repeat(100) })

    new DataView(riff.buffer).setUint32(riff.length - 104, 0xfffffff0, true)

    const meta2 = await readImageMeta(riff, { stealth: 'never' })

    expect(meta2.entries).toEqual([])
    expect(meta2.warnings).toEqual([{ code: 'truncated', detail: 'XMP ' }])
  })

  it('refuses files over the size cap before parsing', async () => {
    const big = new Uint8Array(MAX_FILE_BYTES + 1)

    big.set(PNG_SIG)
    await expect(quickly(() => readImageMeta(big).catch((e: unknown) => e))).resolves.toMatchObject({ code: 'too_large' })
  })
})

describe('decompression bombs', () => {
  // 256 MiB of zeros deflates to ~260 kB.
  const bomb = deflateSync(new Uint8Array(256 * 1024 * 1024))

  it('aborts a zTXt bomb at the per-entry cap', async () => {
    const z = chunk('zTXt', concat(bin('parameters\0\0'), bomb))
    const meta = await quickly(() => readImageMeta(shellPng([tEXt('ok', 'kept'), z, tEXt('later', 'also kept')]), { stealth: 'never' }), 3000)

    expect(meta.entries.map(e => e.key)).toEqual(['ok', 'later'])
    expect(meta.warnings).toEqual([{ code: 'entry_too_large', detail: 'parameters' }])
  })

  it('aborts a compressed iTXt bomb the same way', async () => {
    const i = chunk('iTXt', concat(bin('prompt\0'), new Uint8Array([1, 0, 0, 0]), bomb))
    const meta = await quickly(() => readImageMeta(shellPng([i]), { stealth: 'never' }), 3000)

    expect(meta.entries).toEqual([])
    expect(meta.warnings).toEqual([{ code: 'entry_too_large', detail: 'prompt' }])
  })

  it('never hands back more than the cap, and reports corrupt streams', async () => {
    await expect(inflateCapped(bomb, 'deflate', 1024)).rejects.toBeInstanceOf(InflateCapError)
    await expect(inflateCapped(deflateSync(new Uint8Array(1024)), 'deflate', 1024)).resolves.toHaveLength(1024)
    await expect(inflateCapped(bin('not zlib at all'), 'deflate', 1024)).rejects.not.toBeInstanceOf(InflateCapError)

    const meta = await readImageMeta(shellPng([chunk('zTXt', bin('k\0\0garbage'))]), { stealth: 'never' })

    expect(meta.warnings).toEqual([{ code: 'bad_compressed', detail: 'k' }])
  })

  it('caps the total across chunks that each fit', async () => {
    const each = MAX_ENTRY_BYTES - 1024
    const z = chunk('zTXt', concat(bin('k\0\0'), deflateSync(new Uint8Array(each).fill(0x61))))
    const fit = Math.floor(MAX_TOTAL_BYTES / each)
    const meta = await readImageMeta(shellPng(Array.from({ length: fit + 2 }, () => z)), { stealth: 'never' })

    expect(meta.entries).toHaveLength(fit)
    expect(meta.entries.reduce((n, e) => n + e.text.length, 0)).toBeLessThanOrEqual(MAX_TOTAL_BYTES)
    expect(meta.warnings).toEqual([{ code: 'total_too_large', detail: 'k' }])
  })

  it('refuses an oversized uncompressed text without decoding it', async () => {
    const meta = await readImageMeta(shellPng([chunk('tEXt', concat(bin('k\0'), new Uint8Array(MAX_ENTRY_BYTES + 1).fill(0x41)))]), { stealth: 'never' })

    expect(meta.entries).toEqual([])
    expect(meta.warnings).toEqual([{ code: 'entry_too_large', detail: 'k' }])
  })
})

describe('chunk floods', () => {
  it('collects at most MAX_TEXT_ENTRIES of 10,000 text chunks', async () => {
    const chunks = Array.from({ length: 10_000 }, (_, i) => tEXt(`k${i}`, 'v'))
    const meta = await quickly(() => readImageMeta(shellPng(chunks), { stealth: 'never' }))

    expect(meta.entries).toHaveLength(MAX_TEXT_ENTRIES)
    expect(meta.entries.at(-1)!.key).toBe(`k${MAX_TEXT_ENTRIES - 1}`)
    expect(meta.warnings).toEqual([{ code: 'too_many_entries' }])
  })

  it('does not track an unbounded number of IDAT segments', async () => {
    const info = await walkPng(concat(PNG_SIG, ihdr(4, 4), ...Array.from({ length: 70_000 }, () => chunk('IDAT', new Uint8Array(1))), chunk('IEND')), new Collector())

    expect(info.idat).toEqual([])
  })
})

describe('giant dimensions', () => {
  it('reads the text of a 100k × 100k PNG but never decodes its pixels', async () => {
    const decodePixels = vi.fn(async () => null)
    const file = concat(PNG_SIG, ihdr(100_000, 100_000), tEXt('Comment', 'still readable'), chunk('IDAT', deflateSync(new Uint8Array(64))), chunk('IEND'))
    const meta = await quickly(() => readImageMeta(file, { stealth: 'always' }, { decodePixels }))

    expect(decodePixels).not.toHaveBeenCalled()
    expect([meta.width, meta.height]).toEqual([100_000, 100_000])
    expect(meta.entries).toEqual([{ source: 'png:tEXt', key: 'Comment', text: 'still readable' }])
    expect(meta.warnings).toEqual([{ code: 'pixels_over_cap', detail: '100000x100000' }])
  })

  it('the PNG decoder and the stealth reader refuse it on their own too', async () => {
    const file = concat(PNG_SIG, ihdr(100_000, 100_000), chunk('IDAT', deflateSync(new Uint8Array(64))), chunk('IEND'))

    expect(await quickly(async () => decodePngPixels(file, await walkPng(file, new Collector())))).toBeNull()
    expect(await readStealth({ width: 100_000, height: 100_000, data: new Uint8Array(16) })).toBeNull()
    expect(100_000 * 100_000).toBeGreaterThan(MAX_PIXELS)
  })

  it('refuses a pixel bomb: IDAT that inflates past what IHDR implies', async () => {
    const file = concat(PNG_SIG, ihdr(4, 4), chunk('IDAT', deflateSync(new Uint8Array(64 * 1024 * 1024))), chunk('IEND'))
    const meta = await quickly(() => readImageMeta(file))

    expect(meta.warnings).toEqual([{ code: 'pixels_unavailable' }])
  })
})

describe('stealth length field', () => {
  const [width, height] = [64, 64]

  async function withBits(mode: 'alpha' | 'rgb', bits: number[]) {
    const rgba = noise(width, height)

    writeLsb(rgba, width, height, mode, bits)

    return quickly(() => readImageMeta(encodePng({ width, height, rgba, colorType: mode === 'alpha' ? 6 : 2 })))
  }

  it('rejects a length larger than the image can hold', async () => {
    // 0xfffffff8 bits ≈ 512 MiB claimed, in a 4096-pixel image.
    const meta = await withBits('alpha', stealthBits('stealth_pngcomp', new Uint8Array(16), 0xfffffff8))

    expect(meta.entries).toEqual([])
    expect(meta.warnings).toEqual([{ code: 'stealth_bad_length', detail: 'stealth_pngcomp' }])

    // One byte more than fits after the 152-bit header.
    const over = width * height - 152 + 8 - ((width * height - 152) % 8)
    const meta2 = await withBits('alpha', stealthBits('stealth_pnginfo', new Uint8Array(16), over))

    expect(meta2.warnings).toEqual([{ code: 'stealth_bad_length', detail: 'stealth_pnginfo' }])
  })

  it('accepts a payload that exactly fills the image', async () => {
    const bytes = Math.floor((width * height * 3 - 152) / 8)
    const meta = await withBits('rgb', stealthBits('stealth_rgbinfo', new Uint8Array(bytes).fill(0x61)))

    expect(meta.entries).toEqual([{ source: 'stealth', key: 'stealth_rgbinfo', text: 'a'.repeat(bytes) }])
  })

  it('rejects zero and non-multiple-of-8 lengths', async () => {
    for (const bad of [0, 12, 801]) {
      const meta = await withBits('rgb', stealthBits('stealth_rgbinfo', new Uint8Array(200), bad))

      expect(meta.warnings).toEqual([{ code: 'stealth_bad_length', detail: 'stealth_rgbinfo' }])
    }
  })

  it('caps the gunzipped payload at MAX_STEALTH_BYTES', async () => {
    const payload = gzipSync(new Uint8Array(MAX_STEALTH_BYTES + 1))
    const meta = await withBits('rgb', stealthBits('stealth_rgbcomp', payload))

    expect(payload.length).toBeLessThan(1500)
    expect(meta.entries).toEqual([])
    expect(meta.warnings).toEqual([{ code: 'stealth_too_large', detail: 'stealth_rgbcomp' }])
  })

  it('reports a payload that is not gzip', async () => {
    const meta = await withBits('alpha', stealthBits('stealth_pngcomp', bin('definitely not gzip')))

    expect(meta.warnings).toEqual([{ code: 'stealth_bad_payload', detail: 'stealth_pngcomp' }])
  })
})

describe('EXIF pointers', () => {
  function rawTiff(build: (v: DataView) => void, size = 64) {
    const v = new DataView(new ArrayBuffer(size))

    v.setUint16(0, 0x4d4d)
    v.setUint16(2, 42)
    v.setUint32(4, 8)
    build(v)

    return new Uint8Array(v.buffer)
  }

  it('survives an Exif IFD that points back at IFD0 (no loop)', async () => {
    const t = rawTiff((v) => {
      v.setUint16(8, 1)
      v.setUint16(10, 0x8769)
      v.setUint16(12, 4)
      v.setUint32(14, 1)
      v.setUint32(18, 8)
    })
    const meta = await quickly(() => readImageMeta(jpeg({ exif: t })))

    expect(meta.entries).toEqual([])
  })

  it('flags out-of-range offsets, counts and sizes instead of reading them', async () => {
    const cases = [
      rawTiff(v => v.setUint32(4, 0xfffffff0)), // IFD0 beyond the block
      rawTiff(v => v.setUint16(8, 0xffff)), // entry count beyond the block
      rawTiff((v) => { // UserComment value far outside
        v.setUint16(8, 1)
        v.setUint16(10, 0x9286)
        v.setUint16(12, 7)
        v.setUint32(14, 0x7fffffff)
        v.setUint32(18, 0x7ffffff0)
      }),
      rawTiff(v => v.setUint16(2, 43)), // wrong magic
      new Uint8Array(4), // too short
    ]

    for (const t of cases) {
      const meta = await quickly(() => readImageMeta(jpeg({ exif: t })))

      expect(meta.entries).toEqual([])
      expect(meta.warnings.map(w => w.code)).toEqual(['bad_exif'])
    }
  })
})

describe('hostile JSON and graphs', () => {
  it('refuses absurdly nested JSON instead of recursing into it', async () => {
    const deep = '['.repeat(200_000) + ']'.repeat(200_000)

    expect(parseJson(deep)).toBeUndefined()
    expect(parseJson('{"a":'.repeat(100) + '1' + '}'.repeat(100))).toBeDefined()

    const meta = await quickly(() => readImageMeta(shellPng([tEXt('prompt', deep), tEXt('pulsar', deep)]), { stealth: 'never' }))

    expect(meta.generator).toBeNull()
    expect(meta.entries).toHaveLength(2)
    expect(meta.warnings.map(w => w.code)).toEqual(['bad_json', 'bad_json'])
  })

  it('bounds the ComfyUI prompt walk on a chain of diamonds (2^60 paths)', async () => {
    const graph: Record<string, unknown> = {
      s: { class_type: 'KSampler', inputs: { positive: ['d0', 0], negative: ['d0', 0], latent_image: ['e', 0], seed: 1 } },
      e: { class_type: 'EmptyLatentImage', inputs: { width: 64, height: 64 } },
      d60: { class_type: 'CLIPTextEncode', inputs: { text: 'deep' } },
    }

    for (let i = 0; i < 60; i++) graph[`d${i}`] = { class_type: 'ConditioningCombine', inputs: { conditioning_1: [`d${i + 1}`, 0], conditioning_2: [`d${i + 1}`, 0] } }

    const meta = await quickly(() => readImageMeta(shellPng([tEXt('prompt', JSON.stringify(graph))]), { stealth: 'never' }))

    expect(meta.generator).toBe('comfyui')
    expect(meta.warnings).toEqual([{ code: 'comfy_walk_truncated' }])
    expect(COMFY_VISIT_BUDGET).toBe(512)
  })

  it('does not loop on a cyclic graph', async () => {
    const graph = {
      s: { class_type: 'KSampler', inputs: { positive: ['a', 0], negative: ['a', 0], model: ['m', 0], latent_image: ['s', 0], seed: ['p', 0] } },
      a: { class_type: 'ControlNetApply', inputs: { conditioning: ['b', 0] } },
      b: { class_type: 'ControlNetApply', inputs: { conditioning: ['a', 0] } },
      m: { class_type: 'LoraLoader', inputs: { model: ['m', 0], lora_name: 'x' } },
      p: { class_type: 'Primitive', inputs: { value: ['p', 0] } },
    }
    const meta = await quickly(() => readImageMeta(shellPng([tEXt('prompt', JSON.stringify(graph))]), { stealth: 'never' }))

    expect(meta.generator).toBe('comfyui')
    expect(meta.params?.prompt).toBeUndefined()
    expect(meta.params?.seed).toBeUndefined()
  })

  it('never resolves file-supplied keys against Object.prototype', async () => {
    const nai = await readImageMeta(shellPng([tEXt('Software', 'NovelAI'), tEXt('Comment', '{"request_type": "constructor", "__proto__": {"polluted": true}, "steps": 3}')]), { stealth: 'never' })

    expect(nai.params!.novelai!.requestTypeLabel).toBe('Unknown Request Type')
    expect(({} as Record<string, unknown>).polluted).toBeUndefined()

    const dream = await readImageMeta(shellPng([tEXt('Dream', '"x" -constructor 5 -toString 6 -s 7')]), { stealth: 'never' })

    expect(dream.params).toMatchObject({ steps: 7, extra: { '-constructor': '5', '-toString': '6' } })

    const comfy = await readImageMeta(shellPng([tEXt('prompt', '{"1": {"class_type": "KSampler", "inputs": {"positive": ["constructor", 0], "negative": ["__proto__", 0], "model": ["toString", 0]}}}')]), { stealth: 'never' })

    expect(comfy.generator).toBe('comfyui')
    expect(comfy.params!.prompt).toBeUndefined()
    expect(structuredClone(comfy)).toEqual(comfy)

    const a1111 = await readImageMeta(shellPng([tEXt('parameters', 'p\nSteps: 1, __proto__: 2, constructor: 3, Seed: 4')]), { stealth: 'never' })

    expect(a1111.params).toMatchObject({ steps: 1, seed: 4, extra: { constructor: '3' } })
    expect(structuredClone(a1111)).toEqual(a1111)
  })

  it('returns markup and control characters as inert strings', async () => {
    const evil = '<img src=x onerror=alert(1)>\x1b]0;pwn\x07'
    const meta = await readImageMeta(shellPng([iTXt('parameters', `${evil}\nSteps: 1, Sampler: ${evil}, Seed: 2`)]), { stealth: 'never' })

    expect(meta.params?.prompt).toBe(evil)
    expect(typeof meta.params?.sampler).toBe('string')
    // Plain data only: the result survives structured clone unchanged.
    expect(structuredClone(meta)).toEqual(meta)
  })
})
