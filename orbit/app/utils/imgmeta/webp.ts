// WebP (RIFF) chunk walker: `EXIF` (→ exif.ts) and `XMP ` chunks, canvas size
// from VP8X / VP8L / VP8, and whether the image is lossless — the only kind
// that can carry a stealth payload or a NovelAI signature.
import { parseExif } from './exif'
import { MAX_SEGMENTS } from './limits'
import { latin1, utf8 } from './text'
import { ImgMetaError } from './types'
import type { Bytes, Collector } from './types'

export interface WebpInfo {
  width: number
  height: number
  /** A VP8L bitstream and no lossy VP8 one. */
  lossless: boolean
  hasAlpha: boolean
  animated: boolean
}

export function isWebp(bytes: Uint8Array): boolean {
  return bytes.length >= 12 && latin1(bytes.subarray(0, 4)) === 'RIFF' && latin1(bytes.subarray(8, 12)) === 'WEBP'
}

export function walkWebp(bytes: Bytes, out: Collector): WebpInfo {
  if (!isWebp(bytes)) throw new ImgMetaError('malformed', 'WebP: no RIFF/WEBP header')

  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const info: WebpInfo = { width: 0, height: 0, lossless: false, hasAlpha: false, animated: false }
  let sawLossy = false
  let sawLossless = false
  let pos = 12

  for (let n = 0; n < MAX_SEGMENTS && pos + 8 <= bytes.length; n++) {
    const type = latin1(bytes.subarray(pos, pos + 4))
    const length = view.getUint32(pos + 4, true)
    const start = pos + 8

    if (length > bytes.length - start) {
      out.warn('truncated', type)
      break
    }

    const data = bytes.subarray(start, start + length)

    if (type === 'VP8X' && length >= 10) {
      info.hasAlpha ||= (data[0]! & 0x10) !== 0
      info.animated ||= (data[0]! & 0x02) !== 0
      info.width = 1 + (data[4]! | (data[5]! << 8) | (data[6]! << 16))
      info.height = 1 + (data[7]! | (data[8]! << 8) | (data[9]! << 16))
    }
    else if (type === 'VP8L' && length >= 5 && data[0] === 0x2f) {
      sawLossless = true

      const bits = view.getUint32(start + 1, true)

      info.width ||= 1 + (bits & 0x3fff)
      info.height ||= 1 + ((bits >>> 14) & 0x3fff)
      info.hasAlpha ||= ((bits >>> 28) & 1) === 1
    }
    else if (type === 'VP8 ') {
      sawLossy = true

      if (length >= 10 && data[3] === 0x9d && data[4] === 0x01 && data[5] === 0x2a) {
        info.width ||= view.getUint16(start + 6, true) & 0x3fff
        info.height ||= view.getUint16(start + 8, true) & 0x3fff
      }
    }
    else if (type === 'ALPH') {
      info.hasAlpha = true
    }
    else if (type === 'ANIM' || type === 'ANMF') {
      info.animated = true
    }
    else if (type === 'EXIF') {
      parseExif(data, out)
    }
    else if (type === 'XMP ') {
      if (length > out.room()) out.tooLarge('XMP', length)
      else out.add('xmp', 'XMP', utf8(data))
    }

    pos = start + length + (length & 1)
  }

  info.lossless = sawLossless && !sawLossy

  return info
}
