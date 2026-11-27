// @vitest-environment node
// NovelAI signature verification, against a TEST keypair. Nothing here can
// prove that a real NovelAI image verifies — that needs a real file (see the
// skipped test at the bottom).
import { beforeAll, describe, expect, it } from 'vitest'
import { NAI_PUBLIC_KEY, signatureMessages, stripSignedHash, verifyNaiSignature } from '~/utils/imgmeta/nai-verify'
import { readImageMeta } from '~/utils/imgmeta/read'
import { concat, embedStealth, encodePng, jpeg, noise, tEXt, tiff, userComment, utf8, webp } from './fixtures/imgmeta'
import { NAI_0001 } from './fixtures/nai-0001'

const [W, H] = [96, 96]
// Python json.dumps formatting: ", " and ": " separators, floats keep `.0`,
// non-ASCII escaped — exactly what a re-serialisation in JS would destroy.
const UNSIGNED = '{"prompt": "1girl, \\u9280\\u9aea", "steps": 28, "height": 96, "width": 96, "scale": 5.0, "seed": 123, "sampler": "k_euler_ancestral", "uc": "lowres", "request_type": "PromptGenerateRequest"}'

let publicKey = ''
let privateKey: CryptoKey

function b64(bytes: ArrayBuffer | Uint8Array): string {
  return btoa(String.fromCharCode(...new Uint8Array(bytes as ArrayBuffer)))
}

async function sign(message: Uint8Array): Promise<string> {
  return b64(await crypto.subtle.sign({ name: 'Ed25519' }, privateKey, message as Uint8Array<ArrayBuffer>))
}

/** Sign like NovelAI: pixels ‖ Comment-without-signed_hash; signed_hash goes last. */
async function signedComment(rgba: Uint8Array, candidate: 'rgb' | 'rgba', unsigned = UNSIGNED): Promise<string> {
  const [rgb, masked] = signatureMessages({ width: W, height: H, data: rgba }, unsigned)

  return `${unsigned.slice(0, -1)}, "signed_hash": "${await sign(candidate === 'rgb' ? rgb : masked)}"}`
}

function naiPng(rgba: Uint8Array, comment: string, description = 'the prompt', colorType: 2 | 6 = 2) {
  return encodePng({
    width: W,
    height: H,
    rgba,
    colorType,
    filters: true,
    before: [tEXt('Title', 'NovelAI generated image'), tEXt('Description', description), tEXt('Software', 'NovelAI'), tEXt('Source', 'NovelAI Diffusion V5 DB276663'), tEXt('Comment', comment)],
  })
}

beforeAll(async () => {
  const pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify']) as CryptoKeyPair

  privateKey = pair.privateKey
  publicKey = b64(await crypto.subtle.exportKey('raw', pair.publicKey))
})

describe('stripSignedHash', () => {
  it('removes the member textually, wherever it sits, without touching the rest', () => {
    expect(stripSignedHash('{"a": 7.0, "signed_hash": "c2ln"}')).toEqual({ stripped: '{"a": 7.0}', signature: 'c2ln' })
    expect(stripSignedHash('{"signed_hash": "c2ln", "a": 7.0}')).toEqual({ stripped: '{"a": 7.0}', signature: 'c2ln' })
    expect(stripSignedHash('{"signed_hash": "c2ln"}')).toEqual({ stripped: '{}', signature: 'c2ln' })
    expect(stripSignedHash('{"a": "\\u30db", "b": 1.0, "signed_hash": "$&$1"}')!.stripped).toBe('{"a": "\\u30db", "b": 1.0}')
    expect(stripSignedHash('{"a": 1}')).toBeNull()
    // JS-style spacing is not what NovelAI signs; its own regex does not match it either.
    expect(stripSignedHash('{"signed_hash":"c2ln"}')).toBeNull()
  })

  it('leaves NovelAI\'s real example byte-identical apart from the member', () => {
    const { stripped, signature } = stripSignedHash(NAI_0001.Comment)!

    expect(signature).toBe('4+H+JzpnAg5hqpWD7E9C4fNFHvgmiUZkeMR+7d8qBzXP2yLsIDtueo1Y1uBJLHJg3WkW8lWxYoOU5Lx88ZM5Cw==')
    expect(stripped.endsWith('"model_hash": "DB276663"}')).toBe(true)
    expect(stripped.length).toBe(NAI_0001.Comment.length - `, "signed_hash": "${signature}"`.length)
    expect(stripped).toContain('"scale": 7.0, "uncond_scale": 0.0')
  })
})

describe('signature messages', () => {
  it('are RGB ‖ text and RGBA-with-alpha-LSB-cleared ‖ text', () => {
    const data = new Uint8Array([1, 2, 3, 255, 4, 5, 6, 254, 7, 8, 9, 1, 10, 11, 12, 0])
    const [rgb, rgba] = signatureMessages({ width: 2, height: 2, data }, 'é')

    expect(Array.from(rgb)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 0xc3, 0xa9])
    expect(Array.from(rgba)).toEqual([1, 2, 3, 254, 4, 5, 6, 254, 7, 8, 9, 0, 10, 11, 12, 0, 0xc3, 0xa9])
    expect(Array.from(data)).toEqual([1, 2, 3, 255, 4, 5, 6, 254, 7, 8, 9, 1, 10, 11, 12, 0])
  })
})

describe('verification through the whole reader', () => {
  it('accepts an image signed over its RGB bytes', async () => {
    const rgba = noise(W, H, 11)
    const meta = await readImageMeta(naiPng(rgba, await signedComment(rgba, 'rgb')), { naiPublicKey: publicKey })

    expect(meta.generator).toBe('novelai')
    expect(meta.signature).toBe('valid')
    expect(meta.params).toMatchObject({ prompt: '1girl, 銀髪', steps: 28, cfg: 5, seed: 123 })
    expect(meta.warnings).toEqual([])
  })

  it('rejects the same file under NovelAI\'s real key (the default)', async () => {
    const rgba = noise(W, H, 11)
    const file = naiPng(rgba, await signedComment(rgba, 'rgb'))

    expect((await readImageMeta(file)).signature).toBe('invalid')
    expect(NAI_PUBLIC_KEY).toBe('Y2JcQAOhLwzwSDUJPNgL04nS0Tbqm7cSRc4xk0vRMic=')
    expect(Buffer.from(NAI_PUBLIC_KEY, 'base64').toString('hex')).toBe('63625c4003a12f0cf04835093cd80bd389d2d136ea9bb71245ce31934bd13227')
  })

  it('rejects one flipped pixel bit', async () => {
    const rgba = noise(W, H, 11)
    const comment = await signedComment(rgba, 'rgb')

    rgba[4 * (37 * W + 51) + 1]! ^= 1

    expect((await readImageMeta(naiPng(rgba, comment), { naiPublicKey: publicKey })).signature).toBe('invalid')
  })

  it('rejects an edited Comment', async () => {
    const rgba = noise(W, H, 11)
    const comment = await signedComment(rgba, 'rgb')
    const edited = comment.replace('"steps": 28', '"steps": 29')
    const reformatted = JSON.stringify(JSON.parse(comment))

    expect(edited).not.toBe(comment)
    expect((await readImageMeta(naiPng(rgba, edited), { naiPublicKey: publicKey })).signature).toBe('invalid')
    // Same values, re-serialised by JS: `5.0` → `5`, no spaces. No longer the signed bytes.
    expect((await readImageMeta(naiPng(rgba, reformatted), { naiPublicKey: publicKey })).signature).not.toBe('valid')
  })

  it('still accepts an edited Description — it is not signed', async () => {
    const rgba = noise(W, H, 11)
    const meta = await readImageMeta(naiPng(rgba, await signedComment(rgba, 'rgb'), 'something else entirely'), { naiPublicKey: publicKey })

    expect(meta.signature).toBe('valid')
    expect(meta.params!.novelai!.description).toBe('something else entirely')
  })

  it('accepts the RGBA candidate: signed with alpha LSBs cleared, stealth embedded afterwards', async () => {
    const rgba = noise(W, H, 5)

    for (let i = 3; i < rgba.length; i += 4) rgba[i] = 200 + (i % 50)

    const comment = await signedComment(rgba, 'rgba')
    const dict = { Description: 'the prompt', Software: 'NovelAI', Source: 'NovelAI Diffusion V5 DB276663', Comment: comment }

    embedStealth(rgba, W, H, 'stealth_pngcomp', JSON.stringify(dict))

    const withText = await readImageMeta(naiPng(rgba, comment, 'the prompt', 6), { naiPublicKey: publicKey })

    expect(withText.signature).toBe('valid')
    expect(withText.via).toBe('container')
    // Pixels were decoded for the signature, so the alpha copy is listed too.
    expect(withText.entries.map(e => e.source)).toEqual(['png:tEXt', 'png:tEXt', 'png:tEXt', 'png:tEXt', 'png:tEXt', 'stealth'])

    // Text chunks stripped by a host (4chan-style): the alpha copy alone still verifies.
    const alphaOnly = await readImageMeta(encodePng({ width: W, height: H, rgba, filters: true }), { naiPublicKey: publicKey })

    expect(alphaOnly.generator).toBe('novelai')
    expect(alphaOnly.via).toBe('stealth')
    expect(alphaOnly.signature).toBe('valid')

    // One colour bit changed after signing: neither candidate matches any more.
    rgba[0]! ^= 1
    expect((await readImageMeta(encodePng({ width: W, height: H, rgba, filters: true }), { naiPublicKey: publicKey })).signature).toBe('invalid')
  })

  it('lets either copy vouch: a tampered text chunk beside an intact alpha copy', async () => {
    const rgba = noise(W, H, 5)
    const comment = await signedComment(rgba, 'rgba')

    embedStealth(rgba, W, H, 'stealth_pngcomp', JSON.stringify({ Software: 'NovelAI', Comment: comment }))

    const meta = await readImageMeta(naiPng(rgba, comment.replace('"seed": 123', '"seed": 124'), 'x', 6), { naiPublicKey: publicKey })

    expect(meta.signature).toBe('valid')
    // The interpretation shown is still the container's; the raw view has both.
    expect(meta.params!.seed).toBe(124)
  })

  it('verifies a lossless WebP (EXIF dictionary) given decoded pixels', async () => {
    const rgba = noise(W, H, 9)
    const comment = await signedComment(rgba, 'rgb')
    const dict = JSON.stringify({ Description: 'p', Software: 'NovelAI', Source: 'NovelAI Diffusion V5 DB276663', Comment: comment })
    const file = webp({ width: W, height: H, alpha: true, exif: tiff({ littleEndian: true, userComment: userComment('ASCII', dict) }) })
    const meta = await readImageMeta(file, { naiPublicKey: publicKey }, { decodePixels: async () => ({ width: W, height: H, data: rgba }) })

    expect(meta.generator).toBe('novelai')
    expect(meta.signature).toBe('valid')

    // A decoder returning the wrong size is not trusted.
    const wrong = await readImageMeta(file, { naiPublicKey: publicKey }, { decodePixels: async () => ({ width: W, height: H - 1, data: rgba }) })

    expect(wrong.signature).toBe('unavailable')
  })

  it('reports no_signature, unsupported (JPEG), and null when switched off', async () => {
    const rgba = noise(W, H, 11)
    const comment = await signedComment(rgba, 'rgb')

    expect((await readImageMeta(naiPng(rgba, UNSIGNED))).signature).toBe('no_signature')
    expect((await readImageMeta(naiPng(rgba, comment), { naiPublicKey: publicKey, verify: false })).signature).toBeNull()

    const dict = JSON.stringify({ Software: 'NovelAI', Comment: comment })
    const asJpeg = await readImageMeta(jpeg({ exif: tiff({ userComment: userComment('ASCII', dict) }) }), { naiPublicKey: publicKey })

    expect(asJpeg.generator).toBe('novelai')
    expect(asJpeg.signature).toBe('unsupported')
  })
})

describe('verifyNaiSignature', () => {
  it('unwraps a whole key dictionary, as NovelAI\'s inspector does', async () => {
    const data = noise(W, H, 2)
    const comment = await signedComment(data, 'rgb')

    expect(await verifyNaiSignature({ width: W, height: H, data }, JSON.stringify({ Comment: comment }), publicKey)).toBe('valid')
    expect(await verifyNaiSignature({ width: W, height: H, data }, comment, publicKey)).toBe('valid')
  })

  it('never reports valid for malformed signatures, keys, comments or pixel buffers', async () => {
    const data = noise(W, H, 2)
    const px = { width: W, height: H, data }
    const comment = await signedComment(data, 'rgb')

    expect(await verifyNaiSignature(px, 'not json', publicKey)).toBe('no_signature')
    expect(await verifyNaiSignature(px, UNSIGNED, publicKey)).toBe('no_signature')
    expect(await verifyNaiSignature(px, '{"a": 1, "signed_hash": "!!!not base64!!!"}', publicKey)).toBe('invalid')
    expect(await verifyNaiSignature(px, '{"a": 1, "signed_hash": "c2hvcnQ="}', publicKey)).toBe('invalid')
    expect(await verifyNaiSignature(px, comment, 'c2hvcnQ=')).toBe('invalid')
    expect(await verifyNaiSignature({ width: W, height: H, data: data.subarray(0, 100) }, comment, publicKey)).toBe('unavailable')
    // A signature by this key over DIFFERENT text.
    const other = `{"a": 1, "signed_hash": "${await sign(concat(data, utf8('{"a": 2}')))}"}`

    expect(await verifyNaiSignature(px, other, publicKey)).toBe('invalid')
  })

  // Spec 5.3 asks for a real NovelAI PNG committed under tests/fixtures/ as the
  // golden. None is available: without it, NovelAI's key, the exact bytes it
  // signs and browser pixel exactness are all unproven against a real file.
  it.skip('verifies a real NovelAI image against NovelAI\'s published key (needs a real fixture)', () => {})
})
