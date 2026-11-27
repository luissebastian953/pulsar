// EXIF UserComment (0x9286) decoding. The field is an 8-byte character-code
// prefix followed by text, and generators disagree about the byte order of
// `UNICODE\0` text: A1111/Forge (piexif) write UTF-16BE without a BOM,
// SwarmUI (.NET Encoding.Unicode) writes UTF-16LE. So, in order:
//   1. the prefix picks the charset,
//   2. a BOM picks the byte order,
//   3. else a zero-byte count over the first 1000 bytes (ASCII-range text has
//      its zero in the high byte: even offsets → BE, odd → LE),
//   4. else the TIFF block's own byte order.
// NovelAI writes `ASCII\0\0\0` + JSON; undefined (8 NULs) and prefix-less
// comments are read as UTF-8. Trailing NULs are stripped.
import { stripTrailingNuls, utf16, utf8 } from './text'

const PREFIX_UNICODE = [0x55, 0x4e, 0x49, 0x43, 0x4f, 0x44, 0x45, 0x00]
const PREFIX_ASCII = [0x41, 0x53, 0x43, 0x49, 0x49, 0x00, 0x00, 0x00]
const PREFIX_JIS = [0x4a, 0x49, 0x53, 0x00, 0x00, 0x00, 0x00, 0x00]
const PREFIX_UNDEFINED = [0, 0, 0, 0, 0, 0, 0, 0]

function hasPrefix(bytes: Uint8Array, prefix: number[]): boolean {
  return bytes.length >= 8 && prefix.every((b, i) => bytes[i] === b)
}

export function decodeUserComment(bytes: Uint8Array, tiffLittleEndian: boolean): string {
  if (hasPrefix(bytes, PREFIX_UNICODE)) return stripTrailingNuls(decodeUtf16(bytes.subarray(8), tiffLittleEndian))

  if (hasPrefix(bytes, PREFIX_ASCII) || hasPrefix(bytes, PREFIX_JIS) || hasPrefix(bytes, PREFIX_UNDEFINED)) {
    return stripTrailingNuls(utf8(bytes.subarray(8)))
  }

  return stripTrailingNuls(utf8(bytes))
}

function decodeUtf16(body: Uint8Array, tiffLittleEndian: boolean): string {
  if (body[0] === 0xfe && body[1] === 0xff) return utf16(body.subarray(2), false)

  if (body[0] === 0xff && body[1] === 0xfe) return utf16(body.subarray(2), true)

  let even = 0
  let odd = 0
  const n = Math.min(body.length, 1000)

  for (let i = 0; i < n; i++) {
    if (body[i] !== 0) continue

    if (i % 2 === 0) even++
    else odd++
  }

  if (even !== odd) return utf16(body, odd > even)

  return utf16(body, tiffLittleEndian)
}
