// NovelAI signature check. `signed_hash` inside the Comment JSON is a base64
// Ed25519 detached signature over
//
//     pixel bytes  ‖  UTF-8 of the Comment with the signed_hash member removed
//
// where the pixel bytes are either RGB, or (since 2026-08) RGBA with every
// alpha LSB cleared — the alpha LSBs carry the stealth payload, which is
// embedded after signing. Both candidates are tried, as NovelAI's own
// verifiers do.
//
// The member is removed TEXTUALLY, with NovelAI's exact regex: the signed
// bytes are Python json.dumps output (", " separators, `7.0` floats, \u
// escapes) and any JS re-serialisation changes them.
//
// Only the Comment and the pixels are signed. Title, Description, Software
// and Source can be edited freely without invalidating the signature, so a
// "valid" result vouches for the Comment's parameters and nothing else.
//
// Pulsar can verify these signatures; it can never produce one, and must
// never write `Software: NovelAI`.
import type { Bytes, NaiSignatureStatus, PixelData } from './types'

/** NovelAI's published Ed25519 public key (NovelAI/novelai-image-metadata, nai_sig.py). */
export const NAI_PUBLIC_KEY = 'Y2JcQAOhLwzwSDUJPNgL04nS0Tbqm7cSRc4xk0vRMic='

const SIGNED_HASH = /"signed_hash": "([^"]*)"/

/** Split a Comment into the text that was signed and its base64 signature. Null when unsigned. */
export function stripSignedHash(comment: string): { stripped: string, signature: string } | null {
  const m = comment.match(SIGNED_HASH)

  if (!m) return null

  const member = m[0]
  // split/join, not String.replace: a replacement pattern must never be
  // interpreted, and only the first occurrence goes (as in NovelAI's code).
  const cut = (needle: string) => {
    const at = comment.indexOf(needle)

    return at < 0 ? comment : comment.slice(0, at) + comment.slice(at + needle.length)
  }
  let stripped = cut(`, ${member}`)

  if (stripped === comment) stripped = cut(`${member}, `)

  if (stripped === comment) stripped = cut(member)

  return { stripped, signature: m[1]! }
}

function fromBase64(s: string): Bytes | null {
  try {
    const bin = atob(s)
    const out = new Uint8Array(bin.length)

    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)

    return out
  }
  catch {
    return null
  }
}

/** The two byte strings NovelAI may have signed for these pixels. */
export function signatureMessages(px: PixelData, signedText: string): [Bytes, Bytes] {
  const text = new TextEncoder().encode(signedText)
  const n = px.width * px.height
  const src = px.data
  const rgb = new Uint8Array(3 * n + text.length)

  for (let i = 0; i < n; i++) {
    rgb[3 * i] = src[4 * i]!
    rgb[3 * i + 1] = src[4 * i + 1]!
    rgb[3 * i + 2] = src[4 * i + 2]!
  }

  rgb.set(text, 3 * n)

  const rgba = new Uint8Array(4 * n + text.length)

  rgba.set(src.subarray(0, 4 * n))

  for (let i = 3; i < 4 * n; i += 4) rgba[i] = rgba[i]! & 0xfe

  rgba.set(text, 4 * n)

  return [rgb, rgba]
}

/**
 * Verify a NovelAI Comment against decoded pixels. `comment` is the Comment
 * JSON text exactly as stored (a whole key dictionary with a string `Comment`
 * member is unwrapped, as NovelAI's inspector does). `publicKey` is base64 and
 * defaults to NovelAI's.
 *
 * Returns `unavailable` when this WebCrypto has no Ed25519 — there is no JS
 * fallback, so nothing is ever reported valid without a real check.
 */
export async function verifyNaiSignature(px: PixelData, comment: string, publicKey: string = NAI_PUBLIC_KEY): Promise<NaiSignatureStatus> {
  try {
    const outer: unknown = JSON.parse(comment)

    if (outer && typeof outer === 'object' && typeof (outer as { Comment?: unknown }).Comment === 'string') {
      comment = (outer as { Comment: string }).Comment
    }
  }
  catch {
    return 'no_signature'
  }

  const split = stripSignedHash(comment)

  if (!split) return 'no_signature'

  const signature = fromBase64(split.signature)
  const keyBytes = fromBase64(publicKey)

  if (!signature || signature.length !== 64 || !keyBytes || keyBytes.length !== 32) return 'invalid'

  if (px.data.length < px.width * px.height * 4) return 'unavailable'

  let key: CryptoKey

  try {
    key = await crypto.subtle.importKey('raw', keyBytes, { name: 'Ed25519' }, false, ['verify'])
  }
  catch (err) {
    // NotSupportedError: no Ed25519 here. Anything else is a bad key.
    return err instanceof Error && err.name === 'DataError' ? 'invalid' : 'unavailable'
  }

  for (const message of signatureMessages(px, split.stripped)) {
    try {
      if (await crypto.subtle.verify({ name: 'Ed25519' }, key, signature, message)) return 'valid'
    }
    catch {
      return 'unavailable'
    }
  }

  return 'invalid'
}
