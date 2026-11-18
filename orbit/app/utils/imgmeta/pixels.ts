// Bit-exact pixel access for the stealth scan and the NovelAI signature. Both
// depend on every LSB, so the decode must not premultiply, colour-manage or
// resample.
//
// PNG is decoded here, by hand: concatenated IDAT → inflate (capped at exactly
// the size IHDR implies) → unfilter → RGBA. That covers 8-bit, non-interlaced
// PNGs of every colour type, which is what every generator writes, with no
// dependency and the same result in every engine (and in tests).
//
// Everything else (WebP, 16-bit or interlaced PNG) goes through
// createImageBitmap with premultiplication and colour conversion off, read
// back through a WebGL framebuffer; a 2D canvas is only the last resort
// because its premultiplied backing store can perturb RGB under alpha < 255.
import { inflateCapped } from './inflate'
import { MAX_PIXELS } from './limits'
import type { PngInfo } from './png'
import type { Bytes, ImageFormat, PixelData } from './types'

/** Returns null when the pixels cannot be decoded bit-exactly in this environment. */
export type PixelDecoder = (bytes: Bytes, format: ImageFormat, png?: PngInfo) => Promise<PixelData | null>

const CHANNELS: Record<number, number> = { 0: 1, 2: 3, 3: 1, 4: 2, 6: 4 }

export async function decodePngPixels(bytes: Bytes, info: PngInfo): Promise<PixelData | null> {
  const { width, height } = info
  const channels = CHANNELS[info.colorType]

  if (!channels || info.bitDepth !== 8 || info.interlace !== 0 || info.idat.length === 0) return null

  if (width * height > MAX_PIXELS) return null

  if (info.colorType === 3 && !info.palette) return null

  let compressedSize = 0

  for (const [, length] of info.idat) compressedSize += length

  const compressed = new Uint8Array(compressedSize)
  let at = 0

  for (const [offset, length] of info.idat) {
    compressed.set(bytes.subarray(offset, offset + length), at)
    at += length
  }

  const stride = width * channels
  const expected = (stride + 1) * height
  let raw: Uint8Array

  try {
    raw = await inflateCapped(compressed, 'deflate', expected)
  }
  catch {
    return null
  }

  if (raw.length !== expected) return null

  if (!unfilter(raw, stride, height, channels)) return null

  const data = new Uint8Array(width * height * 4)
  const palette = info.palette
  const trns = info.transparency
  let o = 0

  for (let y = 0; y < height; y++) {
    let i = y * (stride + 1) + 1

    for (let x = 0; x < width; x++) {
      if (channels === 4) {
        data[o] = raw[i]!
        data[o + 1] = raw[i + 1]!
        data[o + 2] = raw[i + 2]!
        data[o + 3] = raw[i + 3]!
      }
      else if (channels === 3) {
        data[o] = raw[i]!
        data[o + 1] = raw[i + 1]!
        data[o + 2] = raw[i + 2]!
        data[o + 3] = 255
      }
      else if (palette) {
        const p = raw[i]!

        data[o] = palette[p * 3] ?? 0
        data[o + 1] = palette[p * 3 + 1] ?? 0
        data[o + 2] = palette[p * 3 + 2] ?? 0
        data[o + 3] = trns?.[p] ?? 255
      }
      else {
        data[o] = data[o + 1] = data[o + 2] = raw[i]!
        data[o + 3] = channels === 2 ? raw[i + 1]! : 255
      }

      i += channels
      o += 4
    }
  }

  return { width, height, data }
}

/** Reverse PNG scanline filters in place. False on an unknown filter type. */
function unfilter(raw: Uint8Array, stride: number, height: number, bpp: number): boolean {
  for (let y = 0; y < height; y++) {
    const row = y * (stride + 1) + 1
    const prev = row - (stride + 1)
    const type = raw[row - 1]

    if (type === 0) continue

    if (type === undefined || type > 4) return false

    for (let x = 0; x < stride; x++) {
      const a = x >= bpp ? raw[row + x - bpp]! : 0
      const b = y > 0 ? raw[prev + x]! : 0
      const c = x >= bpp && y > 0 ? raw[prev + x - bpp]! : 0
      let predictor: number

      if (type === 1) {
        predictor = a
      }
      else if (type === 2) {
        predictor = b
      }
      else if (type === 3) {
        predictor = (a + b) >> 1
      }
      else {
        const p = a + b - c
        const pa = Math.abs(p - a)
        const pb = Math.abs(p - b)
        const pc = Math.abs(p - c)

        predictor = pa <= pb && pa <= pc ? a : pb <= pc ? b : c
      }

      raw[row + x] = (raw[row + x]! + predictor) & 0xff
    }
  }

  return true
}

const MIME: Record<ImageFormat, string> = { png: 'image/png', jpeg: 'image/jpeg', webp: 'image/webp' }

/** Browser decode for formats without a hand-written decoder. Null outside a browser/worker. */
export async function decodeBitmapPixels(bytes: Bytes, format: ImageFormat): Promise<PixelData | null> {
  if (typeof createImageBitmap !== 'function' || typeof OffscreenCanvas === 'undefined') return null

  let bitmap: ImageBitmap

  try {
    bitmap = await createImageBitmap(new Blob([bytes], { type: MIME[format] }), {
      premultiplyAlpha: 'none',
      colorSpaceConversion: 'none',
    })
  }
  catch {
    return null
  }

  try {
    if (bitmap.width * bitmap.height > MAX_PIXELS) return null

    return readThroughWebgl(bitmap) ?? readThrough2d(bitmap)
  }
  finally {
    bitmap.close()
  }
}

function readThroughWebgl(bitmap: ImageBitmap): PixelData | null {
  const { width, height } = bitmap
  const gl = new OffscreenCanvas(1, 1).getContext('webgl', { premultipliedAlpha: false }) as WebGLRenderingContext | null

  if (!gl) return null

  try {
    if (Math.max(width, height) > gl.getParameter(gl.MAX_TEXTURE_SIZE)) return null

    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL, 0)
    gl.pixelStorei(gl.UNPACK_COLORSPACE_CONVERSION_WEBGL, gl.NONE)
    gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, 0)
    gl.bindTexture(gl.TEXTURE_2D, gl.createTexture())
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, bitmap)
    gl.bindFramebuffer(gl.FRAMEBUFFER, gl.createFramebuffer())
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, gl.getParameter(gl.TEXTURE_BINDING_2D), 0)

    if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) return null

    // Texel row 0 is the image's first row (no flip), so the readback is
    // already top-down.
    const data = new Uint8Array(width * height * 4)

    gl.readPixels(0, 0, width, height, gl.RGBA, gl.UNSIGNED_BYTE, data)

    return gl.getError() === gl.NO_ERROR ? { width, height, data } : null
  }
  catch {
    return null
  }
  finally {
    gl.getExtension('WEBGL_lose_context')?.loseContext()
  }
}

function readThrough2d(bitmap: ImageBitmap): PixelData | null {
  const ctx = new OffscreenCanvas(bitmap.width, bitmap.height).getContext('2d', { willReadFrequently: true }) as OffscreenCanvasRenderingContext2D | null

  if (!ctx) return null

  ctx.globalCompositeOperation = 'copy'
  ctx.drawImage(bitmap, 0, 0)

  const image = ctx.getImageData(0, 0, bitmap.width, bitmap.height)

  return { width: image.width, height: image.height, data: image.data }
}

/** The default decoder: the hand-written PNG path first, then the browser. */
export const decodePixels: PixelDecoder = async (bytes, format, png) => {
  if (format === 'png' && png) {
    const px = await decodePngPixels(bytes, png)

    if (px) return px
  }

  return decodeBitmapPixels(bytes, format)
}
