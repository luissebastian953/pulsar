// "Stealth pnginfo" reader: metadata bit-packed into pixel LSBs. One layout,
// four signatures (A1111 extension, Forge, reForge, SwarmUI, NovelAI):
//
//   magic    15 ASCII bytes  stealth_{png|rgb}{info|comp}
//              png = alpha-channel LSBs, 1 bit per pixel
//              rgb = R,G,B LSBs, 3 bits per pixel
//              info = raw UTF-8, comp = gzip
//   length   32 bits, big-endian, counting payload BITS
//   payload  length/8 bytes
//
// Bits run column-major (x outer, y inner) and pack MSB-first. Read-only by
// design: pulsar never writes this format.
//
// Hostile input: the length field can claim 512 MiB. It is rejected unless it
// is a positive multiple of 8 that fits in the pixels left after the header,
// and the payload (raw or gunzipped) is capped at MAX_STEALTH_BYTES.
import { InflateCapError, inflateCapped } from './inflate'
import { MAX_PIXELS, MAX_STEALTH_BYTES } from './limits'
import { utf8 } from './text'
import type { PixelData, WarningCode } from './types'

export type StealthSignature = 'stealth_pnginfo' | 'stealth_pngcomp' | 'stealth_rgbinfo' | 'stealth_rgbcomp'

export type StealthResult
  = | { signature: StealthSignature, text: string }
    | { signature: StealthSignature, error: Extract<WarningCode, `stealth_${string}`> }

const HEADER_BITS = (15 + 4) * 8

/** Sequential LSB reader over one channel layout. */
function bitReader(px: PixelData, mode: 'alpha' | 'rgb') {
  const { width, height, data } = px
  let k = 0

  const bit = mode === 'alpha'
    ? () => {
        const i = 4 * ((k % height) * width + Math.floor(k / height)) + 3

        k++
        return data[i]! & 1
      }
    : () => {
        const p = Math.floor(k / 3)
        const i = 4 * ((p % height) * width + Math.floor(p / height)) + (k % 3)

        k++
        return data[i]! & 1
      }

  return {
    byte() {
      let b = 0

      for (let n = 0; n < 8; n++) b = (b << 1) | bit()

      return b
    },
  }
}

async function readMode(px: PixelData, mode: 'alpha' | 'rgb'): Promise<StealthResult | null> {
  const capacity = px.width * px.height * (mode === 'alpha' ? 1 : 3)

  if (capacity < HEADER_BITS) return null

  const reader = bitReader(px, mode)
  let magic = ''

  for (let i = 0; i < 15; i++) magic += String.fromCharCode(reader.byte())

  const info = mode === 'alpha' ? 'stealth_pnginfo' : 'stealth_rgbinfo'
  const comp = mode === 'alpha' ? 'stealth_pngcomp' : 'stealth_rgbcomp'

  if (magic !== info && magic !== comp) return null

  const signature: StealthSignature = magic
  const bits = reader.byte() * 0x1000000 + (reader.byte() << 16) + (reader.byte() << 8) + reader.byte()

  if (bits === 0 || bits % 8 !== 0 || bits > capacity - HEADER_BITS) return { signature, error: 'stealth_bad_length' }

  const length = bits / 8

  // Compressed input larger than the output cap cannot be a payload we would
  // accept either; refuse before copying it out of the pixels.
  if (length > MAX_STEALTH_BYTES) return { signature, error: 'stealth_too_large' }

  const payload = new Uint8Array(length)

  for (let i = 0; i < length; i++) payload[i] = reader.byte()

  if (signature === info) return { signature, text: utf8(payload) }

  try {
    return { signature, text: utf8(await inflateCapped(payload, 'gzip', MAX_STEALTH_BYTES)) }
  }
  catch (err) {
    return { signature, error: err instanceof InflateCapError ? 'stealth_too_large' : 'stealth_bad_payload' }
  }
}

/**
 * Look for a stealth payload in decoded pixels. `alpha` should be false for
 * images without an alpha channel (their decoded alpha is constant). Returns
 * null when no signature is present, or when the image is over MAX_PIXELS.
 */
export async function readStealth(px: PixelData, opts: { alpha?: boolean, rgb?: boolean } = {}): Promise<StealthResult | null> {
  if (px.width * px.height > MAX_PIXELS || px.data.length < px.width * px.height * 4) return null

  if (opts.alpha !== false) {
    const found = await readMode(px, 'alpha')

    if (found) return found
  }

  if (opts.rgb !== false) return readMode(px, 'rgb')

  return null
}
