// Minimal EXIF (TIFF) reader shared by JPEG APP1, WebP `EXIF` and PNG `eXIf`.
// It reads only the tags generators write metadata into:
//
//   IFD0     0x010D DocumentName   (NovelAI: Title)
//            0x010E ImageDescription (NovelAI: prompt; ComfyUI: third key)
//            0x010F Make           (ComfyUI WebP: `workflow:{json}`)
//            0x0110 Model          (ComfyUI WebP: `prompt:{json}`)
//            0x0131 Software       (NovelAI: Source; Fooocus)
//            0x013B Artist, 0x8298 Copyright
//            0x8769 → Exif IFD
//   ExifIFD  0x9286 UserComment    (A1111, SwarmUI, NovelAI, Fooocus)
//
// Every offset is bounds-checked ("decoders should be prepared to encounter
// invalid pointers", PNG §13.3) and each IFD is read once, so pointer loops
// cannot spin.
import { MAX_IFD_ENTRIES } from './limits'
import { stripTrailingNuls, utf8 } from './text'
import type { Collector } from './types'
import { decodeUserComment } from './usercomment'

const TEXT_TAGS: Record<number, string> = {
  0x010d: 'DocumentName',
  0x010e: 'ImageDescription',
  0x010f: 'Make',
  0x0110: 'Model',
  0x0131: 'Software',
  0x013b: 'Artist',
  0x8298: 'Copyright',
}
const TAG_EXIF_IFD = 0x8769
const TAG_USER_COMMENT = 0x9286

// Bytes per value for TIFF types 1..12; 0 = unknown.
const TYPE_SIZE = [0, 1, 1, 2, 4, 8, 1, 1, 2, 4, 8, 4, 8]

export function parseExif(tiff: Uint8Array, out: Collector): void {
  // Some writers keep the JPEG `Exif\0\0` marker inside WebP/PNG blocks.
  if (tiff.length >= 6 && tiff[0] === 0x45 && tiff[1] === 0x78 && tiff[2] === 0x69 && tiff[3] === 0x66 && tiff[4] === 0 && tiff[5] === 0) {
    tiff = tiff.subarray(6)
  }

  if (tiff.length < 8) {
    out.warn('bad_exif')
    return
  }

  const view = new DataView(tiff.buffer, tiff.byteOffset, tiff.byteLength)
  const order = view.getUint16(0)
  const le = order === 0x4949

  if ((!le && order !== 0x4d4d) || view.getUint16(2, le) !== 42) {
    out.warn('bad_exif')
    return
  }

  const exifIfd = readIfd(tiff, view, le, view.getUint32(4, le), out)

  if (exifIfd !== null) readIfd(tiff, view, le, exifIfd, out)
}

/** Reads one IFD's text tags; returns the Exif IFD offset when it points to one. */
function readIfd(tiff: Uint8Array, view: DataView, le: boolean, offset: number, out: Collector): number | null {
  if (offset < 8 || offset + 2 > tiff.length) {
    out.warn('bad_exif')
    return null
  }

  const count = Math.min(view.getUint16(offset, le), MAX_IFD_ENTRIES)
  let exifIfd: number | null = null

  for (let i = 0; i < count; i++) {
    const at = offset + 2 + i * 12

    if (at + 12 > tiff.length) {
      out.warn('bad_exif')
      break
    }

    const tag = view.getUint16(at, le)

    if (tag === TAG_EXIF_IFD) {
      exifIfd = view.getUint32(at + 8, le)
      continue
    }

    const name = tag === TAG_USER_COMMENT ? 'UserComment' : TEXT_TAGS[tag]

    if (!name) continue

    const size = (TYPE_SIZE[view.getUint16(at + 2, le)] ?? 0) * view.getUint32(at + 4, le)
    const start = size <= 4 ? at + 8 : view.getUint32(at + 8, le)

    if (size === 0 || start + size > tiff.length) {
      out.warn('bad_exif', name)
      continue
    }

    if (!out.hasRoom()) break

    if (size > out.room()) {
      out.tooLarge(name, size)
      continue
    }

    const value = tiff.subarray(start, start + size)

    out.add('exif', name, tag === TAG_USER_COMMENT ? decodeUserComment(value, le) : stripTrailingNuls(utf8(value)))
  }

  return exifIfd
}
