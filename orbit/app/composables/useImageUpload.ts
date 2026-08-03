import type { UploadResult } from '~/types/api'

// One upload slot: file-input pick → POST /uploads → id + preview + dims.
// Shared by the character-reference picker, img2img, and preset restore —
// every picker used to hand-roll this exact try/catch.
export function useImageUpload() {
  const api = useApi()

  const id = ref('')
  const preview = ref('')
  const error = ref('')
  const file = ref<File | null>(null)
  const name = ref('')
  const width = ref(0)
  const height = ref(0)

  function set(up: UploadResult, f: File | null) {
    id.value = up.image_id
    width.value = up.width
    height.value = up.height
    preview.value = api.imageUrl(`/images/${up.image_id}`)
    file.value = f
    name.value = f?.name ?? ''
  }

  /** Upload a picked file. Returns true on success; failure lands in `error`. */
  async function upload(f: File): Promise<boolean> {
    error.value = ''
    try {
      set(await api.upload<UploadResult>('/uploads', f), f)
      return true
    }
    catch (err: unknown) {
      error.value = apiErrorMessage(err, 'upload failed')
      return false
    }
  }

  /** Change-event adapter for `<input type="file">`. */
  async function pick(e: Event): Promise<boolean> {
    const f = (e.target as HTMLInputElement).files?.[0]
    if (!f) return false

    return upload(f)
  }

  function clear() {
    id.value = ''
    preview.value = ''
    error.value = ''
    file.value = null
    name.value = ''
    width.value = 0
    height.value = 0
  }

  return { id, preview, error, file, name, width, height, upload, pick, clear }
}

export type ImageUploadSlot = ReturnType<typeof useImageUpload>
