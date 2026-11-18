// Synthetic image builders for the imgmeta tests. Everything is generated
// in-test — no binary fixtures — and uses node:zlib, i.e. an implementation
// independent of the code under test (which inflates with
// DecompressionStream and has its own CRC and PNG decoder).
import { crc32, deflateSync, gzipSync } from 'node:zlib'

export type Buf = Uint8Array<ArrayBuffer>

export function concat(...parts: Uint8Array[]): Buf {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let at = 0

  for (const p of parts) {
    out.set(p, at)
    at += p.length
  }

  return out
}

/** One byte per char (Latin-1). */
export function bin(s: string): Buf {
  return Uint8Array.from(s, c => c.charCodeAt(0))
}

export function utf8(s: string): Buf {
  return new TextEncoder().encode(s) as Buf
}

export function u32(n: number): Buf {
  return new Uint8Array([n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255])
}

// ---------------------------------------------------------------- PNG

export const PNG_SIG = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])

export function chunk(type: string, data: Uint8Array = new Uint8Array(0), opts: { badCrc?: boolean } = {}): Buf {
  const body = concat(bin(type), data)

  return concat(u32(data.length), body, u32((crc32(body) ^ (opts.badCrc ? 1 : 0)) >>> 0))
}

export function ihdr(width: number, height: number, colorType = 6, bitDepth = 8, interlace = 0): Buf {
  return chunk('IHDR', concat(u32(width), u32(height), new Uint8Array([bitDepth, colorType, 0, 0, interlace])))
}

export const tEXt = (key: string, text: string) => chunk('tEXt', concat(bin(key), new Uint8Array([0]), bin(text)))
export const zTXt = (key: string, text: string) => chunk('zTXt', concat(bin(key), new Uint8Array([0, 0]), deflateSync(bin(text))))
export const comf = (key: string, text: string) => chunk('comf', concat(bin(key), new Uint8Array([0]), bin(text)))

export function iTXt(key: string, text: string, opts: { compressed?: boolean } = {}): Buf {
  const body = opts.compressed ? deflateSync(utf8(text)) : utf8(text)

  return chunk('iTXt', concat(bin(key), new Uint8Array([0, opts.compressed ? 1 : 0, 0, 0, 0]), body))
}

/** What Pillow's add_text does: tEXt when Latin-1 encodable, otherwise iTXt. */
export function pillowText(key: string, text: string): Buf {
  return [...text].every(c => c.codePointAt(0)! < 256) ? tEXt(key, text) : iTXt(key, text)
}

/** Deterministic noise, alpha 255. */
export function noise(width: number, height: number, seed = 1): Buf {
  const out = new Uint8Array(width * height * 4)
  let s = seed >>> 0

  for (let i = 0; i < out.length; i++) {
    s = (Math.imul(s, 1664525) + 1013904223) >>> 0
    out[i] = i % 4 === 3 ? 255 : s >>> 24
  }

  return out
}

function paeth(a: number, b: number, c: number): number {
  const p = a + b - c
  const pa = Math.abs(p - a)
  const pb = Math.abs(p - b)
  const pc = Math.abs(p - c)

  return pa <= pb && pa <= pc ? a : pb <= pc ? b : c
}

export interface PngOptions {
  width: number
  height: number
  /** RGBA source pixels; colour types 2/0/4 drop the channels they lack. */
  rgba: Uint8Array
  colorType?: 0 | 2 | 4 | 6
  /** Cycle through all five scanline filters instead of filter 0. */
  filters?: boolean
  /** Chunks between IHDR and IDAT. */
  before?: Uint8Array[]
  /** Chunks between IDAT and IEND. */
  after?: Uint8Array[]
  /** Split the IDAT stream into this many chunks. */
  idatParts?: number
}

/** A real, decodable PNG. */
export function encodePng(o: PngOptions): Buf {
  const colorType = o.colorType ?? 6
  const picks = { 0: [0], 2: [0, 1, 2], 4: [0, 3], 6: [0, 1, 2, 3] }[colorType]
  const bpp = picks.length
  const stride = o.width * bpp
  const raw = new Uint8Array((stride + 1) * o.height)
  const rows: Uint8Array[] = []

  for (let y = 0; y < o.height; y++) {
    const row = new Uint8Array(stride)

    for (let x = 0; x < o.width; x++) picks.forEach((c, i) => (row[x * bpp + i] = o.rgba[(y * o.width + x) * 4 + c]!))

    rows.push(row)

    const type = o.filters ? y % 5 : 0
    const prev = rows[y - 1]

    raw[y * (stride + 1)] = type

    for (let x = 0; x < stride; x++) {
      const a = x >= bpp ? row[x - bpp]! : 0
      const b = prev ? prev[x]! : 0
      const c = x >= bpp && prev ? prev[x - bpp]! : 0
      const predictor = [0, a, b, (a + b) >> 1, paeth(a, b, c)][type]!

      raw[y * (stride + 1) + 1 + x] = (row[x]! - predictor) & 0xff
    }
  }

  const z = deflateSync(raw)
  const parts = o.idatParts ?? 1
  const size = Math.ceil(z.length / parts)
  const idat: Buf[] = []

  for (let i = 0; i < z.length; i += size) idat.push(chunk('IDAT', z.subarray(i, i + size)))

  return concat(PNG_SIG, ihdr(o.width, o.height, colorType), ...(o.before ?? []), ...idat, ...(o.after ?? []), chunk('IEND'))
}

/** A structurally valid PNG whose pixel data is never looked at by the walker. */
export function shellPng(chunks: Uint8Array[], opts: { width?: number, height?: number, after?: Uint8Array[] } = {}): Buf {
  return concat(PNG_SIG, ihdr(opts.width ?? 1, opts.height ?? 1), ...chunks, chunk('IDAT', deflateSync(new Uint8Array(5))), ...(opts.after ?? []), chunk('IEND'))
}

// ------------------------------------------------------------- stealth

export type StealthVariant = 'stealth_pnginfo' | 'stealth_pngcomp' | 'stealth_rgbinfo' | 'stealth_rgbcomp'

/** Header + payload as a bit array; `lengthBits` overrides the length field. */
export function stealthBits(variant: StealthVariant, payload: Uint8Array, lengthBits = payload.length * 8): number[] {
  const bytes = concat(bin(variant), u32(lengthBits), payload)
  const bits: number[] = []

  for (const b of bytes) {
    for (let n = 7; n >= 0; n--) bits.push((b >> n) & 1)
  }

  return bits
}

/** Write bits into pixel LSBs, column-major — the layout of the A1111 extension. */
export function writeLsb(rgba: Uint8Array, width: number, height: number, mode: 'alpha' | 'rgb', bits: number[]) {
  let k = 0

  for (let x = 0; x < width && k < bits.length; x++) {
    for (let y = 0; y < height && k < bits.length; y++) {
      const i = (y * width + x) * 4

      if (mode === 'alpha') {
        rgba[i + 3] = (rgba[i + 3]! & 0xfe) | bits[k++]!
        continue
      }

      for (let c = 0; c < 3 && k < bits.length; c++) rgba[i + c] = (rgba[i + c]! & 0xfe) | bits[k++]!
    }
  }

  if (k < bits.length) throw new Error('image too small for the stealth payload')
}

export function embedStealth(rgba: Uint8Array, width: number, height: number, variant: StealthVariant, text: string) {
  const payload = variant.endsWith('comp') ? gzipSync(utf8(text)) : utf8(text)

  writeLsb(rgba, width, height, variant.includes('png') ? 'alpha' : 'rgb', stealthBits(variant, payload))
}

// ---------------------------------------------------------------- EXIF

export function userComment(prefix: 'UNICODE' | 'ASCII' | 'UNDEFINED' | 'NONE', text: string, enc: 'utf16be' | 'utf16le' | 'utf8' = 'utf8', bom = false): Buf {
  const head = { UNICODE: bin('UNICODE\0'), ASCII: bin('ASCII\0\0\0'), UNDEFINED: new Uint8Array(8), NONE: new Uint8Array(0) }[prefix]

  if (enc === 'utf8') return concat(head, utf8(text))

  const body = new Uint8Array(text.length * 2)

  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i)

    body[i * 2 + (enc === 'utf16be' ? 0 : 1)] = c >> 8
    body[i * 2 + (enc === 'utf16be' ? 1 : 0)] = c & 255
  }

  const mark = bom ? new Uint8Array(enc === 'utf16be' ? [0xfe, 0xff] : [0xff, 0xfe]) : new Uint8Array(0)

  return concat(head, mark, body)
}

export interface TiffOptions {
  littleEndian?: boolean
  /** IFD0 ASCII tags, e.g. { 0x0110: 'prompt:{…}' }. */
  ifd0?: Record<number, string>
  userComment?: Uint8Array
}

/** A TIFF block: IFD0 with ASCII tags, plus an Exif IFD holding UserComment. */
export function tiff(o: TiffOptions): Buf {
  const le = o.littleEndian ?? false
  const tags = Object.entries(o.ifd0 ?? {}).map(([tag, text]) => ({ tag: Number(tag), type: 2, data: concat(utf8(text), new Uint8Array(1)) }))
  const n = tags.length + (o.userComment ? 1 : 0)
  const ifd0Size = 2 + 12 * n + 4
  const dataStart = 8 + ifd0Size
  const blobs: Uint8Array[] = []
  let cursor = dataStart
  const out = new DataView(new ArrayBuffer(8 + ifd0Size))

  out.setUint16(0, le ? 0x4949 : 0x4d4d)
  out.setUint16(2, 42, le)
  out.setUint32(4, 8, le)
  out.setUint16(8, n, le)

  const entry = (i: number, tag: number, type: number, count: number, value: number) => {
    out.setUint16(10 + i * 12, tag, le)
    out.setUint16(12 + i * 12, type, le)
    out.setUint32(14 + i * 12, count, le)
    out.setUint32(18 + i * 12, value, le)
  }

  tags.forEach((t, i) => {
    entry(i, t.tag, t.type, t.data.length, cursor)
    blobs.push(t.data)
    cursor += t.data.length
  })

  if (o.userComment) {
    const exif = new DataView(new ArrayBuffer(18))

    entry(tags.length, 0x8769, 4, 1, cursor)
    exif.setUint16(0, 1, le)
    exif.setUint16(2, 0x9286, le)
    exif.setUint16(4, 7, le)
    exif.setUint32(6, o.userComment.length, le)
    exif.setUint32(10, cursor + 18, le)
    blobs.push(new Uint8Array(exif.buffer), o.userComment)
  }

  return concat(new Uint8Array(out.buffer), ...blobs)
}

// ---------------------------------------------------------- JPEG / WebP

function segment(marker: number, data: Uint8Array): Buf {
  return concat(new Uint8Array([0xff, marker, (data.length + 2) >> 8, (data.length + 2) & 255]), data)
}

/** SOI, APP1 Exif, SOF0, SOS, EOI. Walkable, not decodable. */
export function jpeg(o: { exif?: Uint8Array, width?: number, height?: number, extra?: Uint8Array[] } = {}): Buf {
  const w = o.width ?? 64
  const h = o.height ?? 48

  return concat(
    new Uint8Array([0xff, 0xd8]),
    ...(o.exif ? [segment(0xe1, concat(bin('Exif\0\0'), o.exif))] : []),
    ...(o.extra ?? []),
    segment(0xc0, new Uint8Array([8, h >> 8, h & 255, w >> 8, w & 255, 1, 1, 0x11, 0])),
    segment(0xda, new Uint8Array([1, 1, 0, 0, 63, 0])),
    new Uint8Array([0x12, 0x34, 0xff, 0xd9]),
  )
}

function riffChunk(type: string, data: Uint8Array): Buf {
  const size = new Uint8Array(4)

  new DataView(size.buffer).setUint32(0, data.length, true)

  return concat(bin(type), size, data, new Uint8Array(data.length & 1))
}

/** RIFF/WEBP with VP8X, a VP8L (or lossy VP8) header and optional EXIF / XMP. Walkable, not decodable. */
export function webp(o: { exif?: Uint8Array, xmp?: string, width: number, height: number, lossy?: boolean, alpha?: boolean }): Buf {
  const w = o.width - 1
  const h = o.height - 1
  const vp8x = new Uint8Array([(o.exif ? 0x08 : 0) | (o.alpha ? 0x10 : 0) | (o.xmp ? 0x04 : 0), 0, 0, 0, w & 255, (w >> 8) & 255, w >> 16, h & 255, (h >> 8) & 255, h >> 16])
  const dims = (w | (h << 14) | ((o.alpha ? 1 : 0) << 28)) >>> 0
  const vp8l = new Uint8Array([0x2f, dims & 255, (dims >> 8) & 255, (dims >> 16) & 255, dims >>> 24, 0, 0, 0])
  const vp8 = new Uint8Array([0, 0, 0, 0x9d, 0x01, 0x2a, o.width & 255, o.width >> 8, o.height & 255, o.height >> 8])
  const body = concat(
    bin('WEBP'),
    riffChunk('VP8X', vp8x),
    o.lossy ? riffChunk('VP8 ', vp8) : riffChunk('VP8L', vp8l),
    ...(o.exif ? [riffChunk('EXIF', o.exif)] : []),
    ...(o.xmp ? [riffChunk('XMP ', utf8(o.xmp))] : []),
  )
  const size = new Uint8Array(4)

  new DataView(size.buffer).setUint32(0, body.length, true)

  return concat(bin('RIFF'), size, body)
}
