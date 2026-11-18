// Capped inflate/gunzip on the platform's DecompressionStream (Baseline 2023):
// no bundled zlib. The cap is enforced on the OUTPUT while streaming, so a
// bomb costs at most `cap` bytes plus one stream chunk before it is cancelled.
import type { Bytes } from './types'

export class InflateCapError extends Error {
  constructor() {
    super('decompressed data exceeds the cap')
    this.name = 'InflateCapError'
  }
}

/**
 * Decompress `data` (zlib for `deflate`, RFC 1952 for `gzip`), rejecting with
 * InflateCapError as soon as the output passes `cap` bytes. Corrupt input
 * rejects with the stream's own error.
 */
export async function inflateCapped(data: Bytes, format: 'deflate' | 'gzip', cap: number): Promise<Bytes> {
  const stream = new DecompressionStream(format)
  const writer = stream.writable.getWriter()
  const reader = stream.readable.getReader()
  // Not awaited before reading: the write only settles as output is drained.
  const writing = writer.write(data).then(() => writer.close()).catch(() => {})
  const parts: Uint8Array[] = []
  let total = 0

  try {
    for (;;) {
      const { done, value } = await reader.read()

      if (done) break

      total += value.length

      if (total > cap) throw new InflateCapError()

      parts.push(value)
    }
  }
  catch (err) {
    await reader.cancel().catch(() => {})
    await writer.abort().catch(() => {})
    throw err
  }

  await writing

  const out = new Uint8Array(total)
  let at = 0

  for (const p of parts) {
    out.set(p, at)
    at += p.length
  }

  return out
}
