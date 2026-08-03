import type { PresetImage } from '~/composables/usePresets'
import type { UploadResult } from '~/types/api'

// Capture/restore for preset image slots. Bytes go to IndexedDB (useBlobStore);
// core blob ids are ephemeral, so restore re-uploads for a fresh id + preview.
export function usePresetImages() {
  const api = useApi()
  const blobs = useBlobStore()

  function authHeaders(): Record<string, string> {
    return api.token.value ? { Authorization: `Bearer ${api.token.value}` } : {}
  }

  // `src` is a freshly-picked File or a core image URL. Returns null on empty
  // or any failure — a missing image must never block saving the rest.
  async function captureImage(src: File | string, name: string, w = 0, h = 0): Promise<PresetImage | null> {
    if (!src) return null

    try {
      const blob = typeof src === 'string'
        ? await (await fetch(src, { headers: authHeaders() })).blob()
        : src
      const key = await blobs.put(blob)
      return { blob: key, name, type: blob.type || 'image/png', width: w, height: h }
    }
    catch { return null }
  }

  async function restoreImage(img: PresetImage | null) {
    if (!img?.blob) return null

    try {
      const blob = await blobs.get(img.blob)
      if (!blob) return null

      const file = new File([blob], img.name || 'image.png', { type: img.type || blob.type || 'image/png' })
      const up = await api.upload<UploadResult>('/uploads', file)
      return { id: up.image_id, preview: api.imageUrl(`/images/${up.image_id}`), width: up.width, height: up.height, file }
    }
    catch { return null }
  }

  return { captureImage, restoreImage }
}
