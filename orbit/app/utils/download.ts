// Blob-anchor image download shared by the history page and detail modal.

/**
 * The URL to download an image from. With `withMetadata` it asks core for
 * the copy that carries the prompt and settings (`?meta=1`, docs/compliance.md
 * #output-metadata); without, the plain URL — "Download clean" — whose only
 * metadata is the AI-generated mark core adds to every served image. Only
 * core image URLs take the parameter: blob: and data: URLs pass through.
 */
export function downloadUrl(url: string, withMetadata: boolean): string {
  if (!withMetadata || !url || /^(?:blob|data):/i.test(url)) return url

  const hash = url.indexOf('#')
  const base = hash < 0 ? url : url.slice(0, hash)

  return `${base}${base.includes('?') ? '&' : '?'}meta=1${hash < 0 ? '' : url.slice(hash)}`
}

/**
 * Fetch `url` and save it as `filename` via a temporary object-URL anchor.
 * Returns false on network/CORS failure instead of throwing — callers decide
 * whether to skip (batch) or fall back (single).
 *
 * no-store: the browser may hold an <img>-cached copy of the same URL that
 * was stored without CORS headers (core's /images cache for a year); a
 * cache hit would fail this cors-mode fetch, so always go to the network.
 */
export async function downloadImage(url: string, filename: string): Promise<boolean> {
  try {
    const blob = await (await fetch(url, { cache: 'no-store' })).blob()
    const objectUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = objectUrl
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(objectUrl)
    return true
  }
  catch {
    return false
  }
}
