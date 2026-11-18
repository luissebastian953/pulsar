// JPEG segment walker: APP1 `Exif\0\0` (→ exif.ts), APP1 XMP, COM, and the
// frame header for dimensions. Stops at the first scan (SOS): generators
// insert metadata before it, and nothing after is length-prefixed.
import { parseExif } from './exif'
import { MAX_SEGMENTS } from './limits'
import { latin1, utf8 } from './text'
import { ImgMetaError } from './types'
import type { Bytes, Collector } from './types'

const XMP_NS = 'http://ns.adobe.com/xap/1.0/\0'

export interface JpegInfo {
  width: number
  height: number
}

export function isJpeg(bytes: Uint8Array): boolean {
  return bytes.length >= 3 && bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff
}

export function walkJpeg(bytes: Bytes, out: Collector): JpegInfo {
  if (!isJpeg(bytes)) throw new ImgMetaError('malformed', 'JPEG: no SOI')

  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
  const info: JpegInfo = { width: 0, height: 0 }
  let pos = 2

  for (let n = 0; n < MAX_SEGMENTS && pos + 4 <= bytes.length; n++) {
    if (bytes[pos] !== 0xff) {
      out.warn('truncated', 'marker')
      break
    }

    const marker = bytes[pos + 1]!

    // Fill bytes and the length-less markers (TEM, RSTn, SOI).
    if (marker === 0xff) {
      pos++
      continue
    }

    if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd8)) {
      pos += 2
      continue
    }

    if (marker === 0xda || marker === 0xd9) break

    const length = view.getUint16(pos + 2)

    if (length < 2 || pos + 2 + length > bytes.length) {
      out.warn('truncated', `0x${marker.toString(16)}`)
      break
    }

    const data = bytes.subarray(pos + 4, pos + 2 + length)

    if (marker === 0xe1 && latin1(data.subarray(0, 6)) === 'Exif\0\0') {
      parseExif(data.subarray(6), out)
    }
    else if (marker === 0xe1 && latin1(data.subarray(0, XMP_NS.length)) === XMP_NS) {
      out.add('xmp', 'XMP', utf8(data.subarray(XMP_NS.length)))
    }
    else if (marker === 0xfe) {
      out.add('jpeg:COM', 'COM', utf8(data))
    }
    else if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc && data.length >= 5) {
      info.height = view.getUint16(pos + 5)
      info.width = view.getUint16(pos + 7)
    }

    pos += 2 + length
  }

  return info
}
