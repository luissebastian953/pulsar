// PNG chunk walker. Reads every text-bearing chunk in the file — including
// those AFTER IDAT, where ComfyUI's APNG writer puts its private `comf` chunks
// — and records where the pixel data lives without touching it.
//
//   tEXt  keyword NUL text                      (Latin-1)
//   zTXt  keyword NUL method data               (zlib → Latin-1)
//   iTXt  keyword NUL flag method lang NUL translated NUL text   (UTF-8, zlib when flag=1)
//   comf  key NUL json                          (ComfyUI animated PNG)
//   eXIf  a TIFF block (same parser as JPEG/WebP)
import { parseExif } from './exif'
import { InflateCapError, inflateCapped } from './inflate'
import { MAX_CHUNK_LENGTH, MAX_ENTRY_BYTES, MAX_IDAT_SEGMENTS } from './limits'
import { latin1, utf8 } from './text'
import { ImgMetaError } from './types'
import type { Bytes, Collector, EntrySource } from './types'

export const PNG_SIGNATURE = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a] as const

export interface PngInfo {
  width: number
  height: number
  bitDepth: number
  /** 0 grey, 2 RGB, 3 palette, 4 grey+alpha, 6 RGBA. */
  colorType: number
  interlace: number
  /** [offset, length] of each IDAT payload, in order. Empty when there were too many to track. */
  idat: [number, number][]
  palette?: Bytes
  transparency?: Bytes
}

let crcTable: Uint32Array | null = null

export function crc32(bytes: Uint8Array, start = 0, end = bytes.length): number {
  if (!crcTable) {
    crcTable = new Uint32Array(256)

    for (let n = 0; n < 256; n++) {
      let c = n

      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1

      crcTable[n] = c
    }
  }

  let crc = 0xffffffff

  for (let i = start; i < end; i++) crc = crcTable[(crc ^ bytes[i]!) & 0xff]! ^ (crc >>> 8)

  return (crc ^ 0xffffffff) >>> 0
}

export function isPng(bytes: Uint8Array): boolean {
  return bytes.length >= 8 && PNG_SIGNATURE.every((b, i) => bytes[i] === b)
}

const TEXT_TYPES: Record<string, EntrySource> = {
  tEXt: 'png:tEXt',
  zTXt: 'png:zTXt',
  iTXt: 'png:iTXt',
  comf: 'png:comf',
}

/**
 * Walk a PNG. Throws ImgMetaError('malformed') only when IHDR is missing or
 * invalid; a chunk that runs past the end of the file stops the walk with a
 * `truncated` warning and keeps what was read before it.
 */
export async function walkPng(bytes: Bytes, out: Collector): Promise<PngInfo> {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  let info: PngInfo | null = null
  let idatOverflow = false
  let pos = 8

  while (pos + 8 <= bytes.length) {
    const length = view.getUint32(pos)
    const type = latin1(bytes.subarray(pos + 4, pos + 8))
    const dataStart = pos + 8
    const remaining = bytes.length - dataStart

    // Bound by min(2^31-1, remaining): a lying length never drives an
    // allocation or a read, it just ends the walk.
    if (length > MAX_CHUNK_LENGTH || length > remaining - 4) {
      if (!info) throw new ImgMetaError('malformed', 'PNG: first chunk is truncated')

      out.warn('truncated', type)
      break
    }

    const data = bytes.subarray(dataStart, dataStart + length)

    if (!info) {
      if (type !== 'IHDR' || length !== 13) throw new ImgMetaError('malformed', 'PNG: IHDR is not the first chunk')

      info = {
        width: view.getUint32(dataStart),
        height: view.getUint32(dataStart + 4),
        bitDepth: data[8]!,
        colorType: data[9]!,
        interlace: data[12]!,
        idat: [],
      }

      if (info.width === 0 || info.height === 0 || info.width > 0x7fffffff || info.height > 0x7fffffff) {
        throw new ImgMetaError('malformed', 'PNG: invalid dimensions')
      }

      checkCrc(bytes, view, pos, length, type, out)
    }
    else if (type === 'IDAT') {
      if (info.idat.length >= MAX_IDAT_SEGMENTS) idatOverflow = true
      else if (length > 0) info.idat.push([dataStart, length])
    }
    else if (type === 'PLTE') {
      info.palette = data
    }
    else if (type === 'tRNS') {
      info.transparency = data
    }
    else if (type === 'IEND') {
      break
    }
    else if (type === 'eXIf') {
      checkCrc(bytes, view, pos, length, type, out)
      parseExif(data, out)
    }
    else if (TEXT_TYPES[type] && out.hasRoom()) {
      checkCrc(bytes, view, pos, length, type, out)
      await readText(type, data, out)
    }

    pos = dataStart + length + 4
  }

  if (!info) throw new ImgMetaError('malformed', 'PNG: no IHDR')

  if (idatOverflow) info.idat = []

  return info
}

function checkCrc(bytes: Bytes, view: DataView, pos: number, length: number, type: string, out: Collector) {
  if (crc32(bytes, pos + 4, pos + 8 + length) !== view.getUint32(pos + 8 + length)) out.warn('bad_crc', type)
}

async function readText(type: string, data: Bytes, out: Collector) {
  const source = TEXT_TYPES[type]!
  const nul = data.indexOf(0)

  if (nul < 0) {
    out.warn('bad_chunk', type)
    return
  }

  const key = latin1(data.subarray(0, Math.min(nul, 256)))

  if (type === 'tEXt' || type === 'comf') {
    if (data.length - nul - 1 > out.room()) out.tooLarge(key, data.length - nul - 1)
    else out.add(source, key, latin1(data.subarray(nul + 1)))

    return
  }

  if (type === 'zTXt') {
    if (data[nul + 1] !== 0) {
      out.warn('bad_chunk', type)
      return
    }

    const text = await inflateText(data.subarray(nul + 2), key, out)

    if (text) out.add(source, key, latin1(text))

    return
  }

  // iTXt
  const flag = data[nul + 1]
  const method = data[nul + 2]
  const langEnd = data.indexOf(0, nul + 3)
  const transEnd = langEnd < 0 ? -1 : data.indexOf(0, langEnd + 1)

  if (transEnd < 0 || flag === undefined || flag > 1 || (flag === 1 && method !== 0)) {
    out.warn('bad_chunk', type)
    return
  }

  const body = data.subarray(transEnd + 1)

  if (flag === 0) {
    if (body.length > out.room()) out.tooLarge(key, body.length)
    else out.add(source, key, utf8(body))

    return
  }

  const text = await inflateText(body, key, out)

  if (text) out.add(source, key, utf8(text))
}

async function inflateText(data: Bytes, key: string, out: Collector): Promise<Bytes | null> {
  const cap = out.room()

  try {
    return await inflateCapped(data, 'deflate', cap)
  }
  catch (err) {
    // Past the cap: which one depends on what bounded this entry.
    if (err instanceof InflateCapError) out.warn(cap < MAX_ENTRY_BYTES ? 'total_too_large' : 'entry_too_large', key)
    else out.warn('bad_compressed', key)

    return null
  }
}
