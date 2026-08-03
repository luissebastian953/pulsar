// Blob-anchor image download shared by the history page and detail modal.

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
