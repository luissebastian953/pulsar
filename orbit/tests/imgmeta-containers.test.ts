// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { decodePngPixels } from '~/utils/imgmeta/pixels'
import { crc32, walkPng } from '~/utils/imgmeta/png'
import { readImageMeta, sniffFormat } from '~/utils/imgmeta/read'
import { readStealth } from '~/utils/imgmeta/stealth'
import { escapeControlChars } from '~/utils/imgmeta/text'
import { Collector, ImgMetaError } from '~/utils/imgmeta/types'
import { decodeUserComment } from '~/utils/imgmeta/usercomment'
import { bin, chunk, comf, embedStealth, encodePng, iTXt, jpeg, noise, shellPng, tEXt, tiff, userComment, webp, zTXt } from './fixtures/imgmeta'
import type { StealthVariant } from './fixtures/imgmeta'

const A1111_TEXT = '1girl, silver hair\nNegative prompt: lowres\nSteps: 28, Sampler: Euler a, CFG scale: 6, Seed: 123456789, Size: 832x1216'

describe('sniffing and whole-file rejection', () => {
  it('recognises the three formats by magic bytes only', () => {
    expect(sniffFormat(shellPng([]))).toBe('png')
    expect(sniffFormat(jpeg())).toBe('jpeg')
    expect(sniffFormat(webp({ width: 4, height: 4 }))).toBe('webp')
    expect(sniffFormat(bin('GIF89a..........'))).toBeNull()
  })

  it('rejects unknown formats and broken headers with a typed error', async () => {
    await expect(readImageMeta(bin('<svg onload=alert(1)>'))).rejects.toMatchObject({ code: 'unsupported_format' })
    await expect(readImageMeta(shellPng([]).subarray(0, 20))).rejects.toMatchObject({ code: 'malformed' })
    await expect(readImageMeta(shellPng([]).subarray(0, 20))).rejects.toBeInstanceOf(ImgMetaError)
  })
})

describe('png walker', () => {
  it('reads tEXt as Latin-1, byte for byte', async () => {
    const meta = await readImageMeta(shellPng([tEXt('Title', 'caf\xe9 \x93x\x94')]))

    // 0x93/0x94 stay C1 controls: `TextDecoder('latin1')` would have made them curly quotes.
    expect(meta.entries).toEqual([{ source: 'png:tEXt', key: 'Title', text: 'café \u0093x\u0094' }])
    expect(meta.format).toBe('png')
    expect(meta.generator).toBeNull()
  })

  it('inflates zTXt and compressed iTXt, and reads CJK as UTF-8 iTXt', async () => {
    const meta = await readImageMeta(shellPng([
      zTXt('parameters', A1111_TEXT),
      iTXt('note', '銀髪の少女 ✨'),
      iTXt('packed', '圧縮 '.repeat(500), { compressed: true }),
    ]))

    expect(meta.entries.map(e => [e.source, e.key])).toEqual([['png:zTXt', 'parameters'], ['png:iTXt', 'note'], ['png:iTXt', 'packed']])
    expect(meta.entries[0]!.text).toBe(A1111_TEXT)
    expect(meta.entries[1]!.text).toBe('銀髪の少女 ✨')
    expect(meta.entries[2]!.text).toBe('圧縮 '.repeat(500))
    expect(meta.warnings).toEqual([])
    expect(meta.generator).toBe('a1111')
  })

  it('reads an A1111 prompt that Pillow wrote as iTXt because it is not Latin-1', async () => {
    const meta = await readImageMeta(shellPng([iTXt('parameters', `1girl, 銀髪\n${A1111_TEXT.split('\n').slice(1).join('\n')}`)]))

    expect(meta.generator).toBe('a1111')
    expect(meta.params?.prompt).toBe('1girl, 銀髪')
    expect(meta.params?.seed).toBe(123456789)
  })

  it('keeps walking past IDAT: ComfyUI APNG `comf` chunks sit after the pixel data', async () => {
    const graph = JSON.stringify({
      3: { class_type: 'KSampler', inputs: { seed: 7, steps: 20, cfg: 5, sampler_name: 'euler', scheduler: 'normal', positive: ['6', 0], negative: ['7', 0], latent_image: ['5', 0] } },
      5: { class_type: 'EmptyLatentImage', inputs: { width: 832, height: 1216 } },
      6: { class_type: 'CLIPTextEncode', inputs: { text: 'after idat' } },
      7: { class_type: 'CLIPTextEncode', inputs: { text: 'bad' } },
    })
    const meta = await readImageMeta(shellPng([chunk('acTL', new Uint8Array(8))], { after: [comf('prompt', graph)] }))

    expect(meta.entries).toEqual([{ source: 'png:comf', key: 'prompt', text: graph }])
    expect(meta.generator).toBe('comfyui')
    expect(meta.params?.prompt).toBe('after idat')
  })

  it('still shows a chunk with a bad CRC, but flags it', async () => {
    const bad = chunk('tEXt', bin('k\0v'), { badCrc: true })
    const meta = await readImageMeta(shellPng([bad]))

    expect(meta.entries).toEqual([{ source: 'png:tEXt', key: 'k', text: 'v' }])
    expect(meta.warnings).toEqual([{ code: 'bad_crc', detail: 'tEXt' }])
  })

  it('computes the standard CRC-32', () => {
    expect(crc32(bin('123456789'))).toBe(0xcbf43926)
  })

  it('reads an eXIf chunk through the shared EXIF parser', async () => {
    const meta = await readImageMeta(shellPng([chunk('eXIf', tiff({ userComment: userComment('UNICODE', A1111_TEXT, 'utf16be') }))]))

    expect(meta.entries).toEqual([{ source: 'exif', key: 'UserComment', text: A1111_TEXT }])
    expect(meta.generator).toBe('a1111')
  })
})

describe('EXIF UserComment', () => {
  it('decodes A1111 (UTF-16BE, no BOM, big-endian TIFF) from a JPEG', async () => {
    const meta = await readImageMeta(jpeg({ exif: tiff({ userComment: userComment('UNICODE', A1111_TEXT, 'utf16be') }), width: 832, height: 1216 }))

    expect(meta.format).toBe('jpeg')
    expect([meta.width, meta.height]).toEqual([832, 1216])
    expect(meta.entries).toEqual([{ source: 'exif', key: 'UserComment', text: A1111_TEXT }])
    expect(meta.generator).toBe('a1111')
    expect(meta.params?.negative).toBe('lowres')
  })

  it('decodes SwarmUI (UTF-16LE) even inside a big-endian TIFF', async () => {
    const json = JSON.stringify({ sui_image_params: { prompt: 'a cat', negativeprompt: 'dog', seed: '42', steps: '20', cfgscale: '7', width: '1024', height: '768', model: 'sdxl', swarm_version: '0.9.6' } })

    for (const littleEndian of [false, true]) {
      const meta = await readImageMeta(jpeg({ exif: tiff({ littleEndian, userComment: userComment('UNICODE', json, 'utf16le') }) }))

      expect(meta.entries[0]!.text).toBe(json)
      expect(meta.generator).toBe('swarmui')
      expect(meta.params).toMatchObject({ prompt: 'a cat', negative: 'dog', seed: 42, steps: 20, cfg: 7, width: 1024, height: 768, model: 'sdxl' })
    }
  })

  it('honours a BOM over everything else', () => {
    expect(decodeUserComment(userComment('UNICODE', 'héllo', 'utf16le', true), false)).toBe('héllo')
    expect(decodeUserComment(userComment('UNICODE', 'héllo', 'utf16be', true), true)).toBe('héllo')
  })

  it('falls back to the TIFF byte order when the zero-byte count is a tie (pure CJK)', () => {
    // No ASCII-range characters: neither byte position holds zeros.
    expect(decodeUserComment(userComment('UNICODE', '銀髪少女', 'utf16be'), false)).toBe('銀髪少女')
    expect(decodeUserComment(userComment('UNICODE', '銀髪少女', 'utf16le'), true)).toBe('銀髪少女')
  })

  it('reads ASCII, undefined and prefix-less comments as UTF-8 and strips trailing NULs', () => {
    expect(decodeUserComment(userComment('ASCII', '{"a":1}\0\0'), false)).toBe('{"a":1}')
    expect(decodeUserComment(userComment('UNDEFINED', 'plain ✓'), false)).toBe('plain ✓')
    expect(decodeUserComment(userComment('NONE', 'bare'), true)).toBe('bare')
  })

  it('reads the other text tags and JPEG COM / XMP segments as raw entries', async () => {
    const com = new Uint8Array([0xff, 0xfe, 0, 7, ...bin('hello')])
    const meta = await readImageMeta(jpeg({ exif: tiff({ littleEndian: true, ifd0: { 0x0131: 'Fooocus v2.5', 0x010e: 'a description' } }), extra: [com] }))

    expect(meta.entries).toEqual([
      { source: 'exif', key: 'ImageDescription', text: 'a description' },
      { source: 'exif', key: 'Software', text: 'Fooocus v2.5' },
      { source: 'jpeg:COM', key: 'COM', text: 'hello' },
    ])
    expect(meta.generator).toBeNull()
  })
})

describe('webp walker', () => {
  it('reads a ComfyUI graph from the EXIF Model tag (`prompt:{json}`) and workflow from Make', async () => {
    const graph = JSON.stringify({
      1: { class_type: 'KSampler', inputs: { seed: 9, steps: 12, cfg: 4.5, sampler_name: 'dpmpp_2m', scheduler: 'karras', positive: ['2', 0], negative: ['3', 0], latent_image: ['4', 0] } },
      2: { class_type: 'CLIPTextEncode', inputs: { text: 'webp prompt' } },
      3: { class_type: 'CLIPTextEncode', inputs: { text: 'webp negative' } },
      4: { class_type: 'EmptyLatentImage', inputs: { width: 1024, height: 1024 } },
    })
    const file = webp({ width: 1024, height: 1024, exif: tiff({ littleEndian: true, ifd0: { 0x0110: `prompt:${graph}`, 0x010f: 'workflow:{"nodes":[],"links":[]}' } }), xmp: '<x:xmpmeta/>' })
    const meta = await readImageMeta(file, { stealth: 'never' })

    expect(meta.format).toBe('webp')
    expect([meta.width, meta.height]).toEqual([1024, 1024])
    expect(meta.entries.map(e => [e.source, e.key])).toEqual([['exif', 'Make'], ['exif', 'Model'], ['xmp', 'XMP']])
    expect(meta.generator).toBe('comfyui')
    expect(meta.params).toMatchObject({ prompt: 'webp prompt', negative: 'webp negative', seed: 9, steps: 12, cfg: 4.5, sampler: 'dpmpp_2m', scheduler: 'karras', width: 1024, height: 1024 })
  })

  it('never scans a lossy WebP for stealth', async () => {
    let decoded = 0
    const meta = await readImageMeta(webp({ width: 64, height: 64, lossy: true }), { stealth: 'always' }, {
      decodePixels: async () => {
        decoded++
        return null
      },
    })

    expect(decoded).toBe(0)
    expect(meta.warnings).toEqual([])
  })
})

describe('png pixel decoder', () => {
  it.each([[6, 'RGBA'], [2, 'RGB'], [4, 'grey+alpha'], [0, 'grey']] as const)('decodes colour type %i (%s) through all five filters, bit-exactly', async (colorType) => {
    const [width, height] = [13, 11]
    const rgba = noise(width, height, colorType + 1)

    for (let i = 3; i < rgba.length; i += 4) rgba[i] = (i * 7) & 255

    const file = encodePng({ width, height, rgba, colorType, filters: true, idatParts: 3 })
    const px = await decodePngPixels(file, await walkPng(file, new Collector()))
    const expected = new Uint8Array(rgba)

    for (let i = 0; i < expected.length; i += 4) {
      if (colorType === 0 || colorType === 4) expected[i + 1] = expected[i + 2] = expected[i]!

      if (colorType === 0 || colorType === 2) expected[i + 3] = 255
    }

    expect(px).not.toBeNull()
    expect([px!.width, px!.height]).toEqual([width, height])
    expect(Array.from(px!.data)).toEqual(Array.from(expected))
  })

  it('returns null rather than guessing for layouts it does not implement', async () => {
    const file = encodePng({ width: 4, height: 4, rgba: noise(4, 4) })
    const info = await walkPng(file, new Collector())

    expect(await decodePngPixels(file, { ...info, bitDepth: 16 })).toBeNull()
    expect(await decodePngPixels(file, { ...info, interlace: 1 })).toBeNull()
    // IHDR lies about the size: the inflated data no longer matches.
    expect(await decodePngPixels(file, { ...info, width: 5 })).toBeNull()
  })
})

describe('stealth reader', () => {
  const variants: [StealthVariant, 2 | 6][] = [
    ['stealth_pnginfo', 6],
    ['stealth_pngcomp', 6],
    ['stealth_rgbinfo', 2],
    ['stealth_rgbcomp', 2],
    ['stealth_rgbcomp', 6],
  ]

  it.each(variants)('reads %s from a real PNG (colour type %i) with no text chunks', async (variant, colorType) => {
    const [width, height] = [48, 40]
    const rgba = noise(width, height, 3)

    embedStealth(rgba, width, height, variant, A1111_TEXT)

    const meta = await readImageMeta(encodePng({ width, height, rgba, colorType, filters: true }))

    expect(meta.entries).toEqual([{ source: 'stealth', key: variant, text: A1111_TEXT }])
    expect(meta.generator).toBe('a1111')
    expect(meta.via).toBe('stealth')
    expect(meta.params).toMatchObject({ prompt: '1girl, silver hair', negative: 'lowres', steps: 28, width: 832, height: 1216 })
    expect(meta.warnings).toEqual([])
  })

  it('reads UTF-8 payloads and prefers container metadata over the stealth copy', async () => {
    const [width, height] = [40, 40]
    const rgba = noise(width, height)

    embedStealth(rgba, width, height, 'stealth_pngcomp', `銀髪\n${A1111_TEXT}`)

    const file = encodePng({ width, height, rgba, before: [tEXt('parameters', A1111_TEXT)] })
    const auto = await readImageMeta(file)
    const always = await readImageMeta(file, { stealth: 'always' })

    // auto: the text chunk matched, so the pixels are never decoded.
    expect(auto.entries.map(e => e.source)).toEqual(['png:tEXt'])
    expect(always.entries.map(e => e.source)).toEqual(['png:tEXt', 'stealth'])
    expect(always.entries[1]!.text).toBe(`銀髪\n${A1111_TEXT}`)
    expect(always.via).toBe('container')
  })

  it('finds nothing in an ordinary image, and ignores the alpha channel when told there is none', async () => {
    const [width, height] = [32, 32]
    const rgba = noise(width, height)

    expect(await readStealth({ width, height, data: rgba })).toBeNull()

    embedStealth(rgba, width, height, 'stealth_pnginfo', 'hello')

    expect(await readStealth({ width, height, data: rgba }, { alpha: false })).toBeNull()
    expect(await readStealth({ width, height, data: rgba })).toEqual({ signature: 'stealth_pnginfo', text: 'hello' })
  })

  it('reads a lossless WebP through the injected pixel decoder', async () => {
    const [width, height] = [40, 40]
    const rgba = noise(width, height)

    embedStealth(rgba, width, height, 'stealth_pngcomp', A1111_TEXT)

    const meta = await readImageMeta(webp({ width, height, alpha: true }), {}, { decodePixels: async () => ({ width, height, data: rgba }) })

    expect(meta.generator).toBe('a1111')
    expect(meta.via).toBe('stealth')
  })

  it('reports when pixels cannot be decoded here instead of pretending there is no payload', async () => {
    const meta = await readImageMeta(webp({ width: 40, height: 40, alpha: true }))

    expect(meta.warnings).toEqual([{ code: 'pixels_unavailable' }])
  })
})

describe('escapeControlChars', () => {
  it('makes control and bidi-override characters visible and leaves markup alone', () => {
    expect(escapeControlChars('a\x1b[31mb\0c\u0085d‮e\tf\ng')).toBe('a\\x1b[31mb\\x00c\\x85d\\u202ee\tf\ng')
    // Markup is not this function's job: it stays text, and the view must render it with {{ }}.
    expect(escapeControlChars('<img src=x onerror=alert(1)>')).toBe('<img src=x onerror=alert(1)>')
  })
})
