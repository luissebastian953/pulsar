// Entry point of the image-metadata library: bytes in, plain data out.
//
//   sniff → walk the container (png / jpeg / webp) → detect the generator
//         → decode pixels only if needed → stealth scan → NovelAI signature
//
// This does real work on untrusted input. Call it from the worker
// (useImageMeta / inspectImage), not from the main thread.
import { detectContainer, detectStealth } from './detect'
import type { Detection } from './detect'
import { isJpeg, walkJpeg } from './jpeg'
import { MAX_FILE_BYTES, MAX_PIXELS } from './limits'
import { NAI_PUBLIC_KEY, verifyNaiSignature } from './nai-verify'
import { decodePixels } from './pixels'
import type { PixelDecoder } from './pixels'
import { isPng, walkPng } from './png'
import type { PngInfo } from './png'
import { readStealth } from './stealth'
import { Collector, ImgMetaError } from './types'
import type { Bytes, ImageFormat, ImageMeta, NaiSignatureStatus, PixelData } from './types'
import { isWebp, walkWebp } from './webp'

/** The options that can cross the worker boundary. */
export interface ReadOptions {
  /**
   * `auto` (default) scans pixel LSBs when the container metadata matched no
   * generator, or when pixels are decoded anyway for a signature. `always`
   * scans every PNG / lossless WebP; `never` skips the scan.
   */
  stealth?: 'auto' | 'always' | 'never'
  /** Verify NovelAI signatures (default true). */
  verify?: boolean
  /** Base64 Ed25519 public key; defaults to NovelAI's published key. */
  naiPublicKey?: string
}

export interface ReadDeps {
  /** Pixel decoder override (tests, or a platform without the browser fallback). */
  decodePixels?: PixelDecoder
}

export function sniffFormat(bytes: Uint8Array): ImageFormat | null {
  if (isPng(bytes)) return 'png'

  if (isJpeg(bytes)) return 'jpeg'

  if (isWebp(bytes)) return 'webp'

  return null
}

/**
 * Read every piece of generation metadata from an image.
 *
 * Rejects with ImgMetaError only for a file over MAX_FILE_BYTES, an unknown
 * format, or an invalid container header. Anything hostile or broken INSIDE
 * a valid container is refused piecewise and reported in `warnings`, so the
 * rest of the file can still be shown.
 */
export async function readImageMeta(input: Uint8Array | ArrayBuffer, options: ReadOptions = {}, deps: ReadDeps = {}): Promise<ImageMeta> {
  if (input.byteLength > MAX_FILE_BYTES) throw new ImgMetaError('too_large')

  const bytes = (input instanceof ArrayBuffer ? new Uint8Array(input) : input) as Bytes
  const format = sniffFormat(bytes)

  if (!format) throw new ImgMetaError('unsupported_format')

  const out = new Collector()
  const container = await walkContainer(bytes, format, out)
  const { width, height, png, hasAlpha, lossless } = container

  let detection: Detection | null = detectContainer(out.entries, out.warnings)
  const comments = new Set<string>()

  if (detection?.naiComment) comments.add(detection.naiComment)

  const verify = options.verify !== false
  const mode = options.stealth ?? 'auto'
  const signed = detection?.params.novelai?.signed === true
  const wantsSignature = verify && lossless && signed && comments.size > 0
  const wantsStealth = lossless && mode !== 'never' && (mode === 'always' || !detection || wantsSignature)
  let pixels: PixelData | null = null

  if (wantsSignature || wantsStealth) {
    // Decide from the header, before anything is inflated or allocated.
    if (width * height > MAX_PIXELS) {
      out.warn('pixels_over_cap', `${width}x${height}`)
    }
    else {
      try {
        pixels = await (deps.decodePixels ?? decodePixels)(bytes, format, png)
      }
      catch {
        pixels = null
      }

      if (pixels && (pixels.width !== width || pixels.height !== height || pixels.data.length < width * height * 4)) pixels = null

      if (!pixels) out.warn('pixels_unavailable')
    }
  }

  if (pixels && wantsStealth) {
    const stealth = await readStealth(pixels, { alpha: hasAlpha })

    if (stealth && 'error' in stealth) {
      out.warn(stealth.error, stealth.signature)
    }
    else if (stealth && out.add('stealth', stealth.signature, stealth.text)) {
      const fromStealth = detectStealth(stealth.text)

      if (fromStealth?.naiComment) comments.add(fromStealth.naiComment)

      detection ??= fromStealth
    }
  }

  let signature: NaiSignatureStatus | null = null

  if (detection?.generator === 'novelai' && verify) {
    const isSigned = detection.params.novelai?.signed === true

    if (!isSigned) signature = 'no_signature'
    else if (format === 'jpeg') signature = 'unsupported'
    // Signed, but not checkable here: lossy pixels, no decoder, over the
    // pixel cap, or the Comment was not stored as text.
    else if (!pixels || comments.size === 0) signature = 'unavailable'
    else signature = await verifyAny(pixels, [...comments], options.naiPublicKey ?? NAI_PUBLIC_KEY)
  }

  return {
    format,
    width,
    height,
    entries: out.entries,
    generator: detection?.generator ?? null,
    via: detection?.via ?? null,
    params: detection?.params ?? null,
    signature,
    warnings: out.warnings,
  }
}

interface Container {
  width: number
  height: number
  png?: PngInfo
  hasAlpha: boolean
  /** Only lossless containers can carry LSB payloads or a pixel signature. */
  lossless: boolean
}

async function walkContainer(bytes: Bytes, format: ImageFormat, out: Collector): Promise<Container> {
  if (format === 'png') {
    const png = await walkPng(bytes, out)

    return {
      width: png.width,
      height: png.height,
      png,
      hasAlpha: png.colorType === 4 || png.colorType === 6 || png.transparency !== undefined,
      lossless: true,
    }
  }

  if (format === 'jpeg') return { ...walkJpeg(bytes, out), hasAlpha: false, lossless: false }

  return walkWebp(bytes, out)
}

/**
 * The file-level and alpha copies of the Comment are checked independently
 * and either may vouch for the image (NovelAI's nai_sig.py does the same).
 */
async function verifyAny(pixels: PixelData, comments: string[], publicKey: string): Promise<NaiSignatureStatus> {
  let result: NaiSignatureStatus = 'no_signature'

  for (const comment of comments) {
    const status = await verifyNaiSignature(pixels, comment, publicKey)

    if (status === 'valid') return 'valid'

    if (status !== 'no_signature') result = status
  }

  return result
}
