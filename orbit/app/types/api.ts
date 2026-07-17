// Wire types for core's API (docs/architecture.md#endpoints).

export type TagCategory = 'general' | 'character' | 'copyright' | 'artist' | 'meta'

export type TranslatedTag = {
  canonical: string
  category: TagCategory
  count: number
  concept: string
  stage: 'exact' | 'alias' | 'fuzzy' | 'live'
}

export type TranslateResult = {
  tags: TranslatedTag[]
  dropped: string[]
}

export type TagSuggestion = {
  canonical: string
  category: TagCategory
  count: number
  // validated live against Danbooru rather than served from the local DB
  stage?: string
  // exists on Danbooru but post-dates the model's training data — it will
  // autocomplete honestly and render poorly
  newer_than_model?: boolean
}

export type UploadResult = {
  image_id: string
  width: number
  height: number
}

export type TagLookupResult
  = | { found: false }
    | { found: true, tag: TagSuggestion }

export type PoseRole = 'source' | 'lineart' | 'skeleton'

export type GenerationRequest = {
  kind?: 'txt2img' | 'img2img' | 'inpaint' | 'pose'
  // img2img/inpaint: upload ids from POST /uploads + denoise
  image_id?: string
  mask_id?: string
  denoise?: number
  prompt: string
  negative?: string
  width?: number
  height?: number
  steps?: number
  cfg?: number
  sampler?: string
  scheduler?: string
  seed?: number
  quality_tags?: boolean
}

export type GenerationParams = {
  prompt: string
  negative: string
  width: number
  height: number
  steps: number
  cfg: number
  sampler: string
  scheduler: string
  seed: number
  batch_size: number
  lineage: string
}

export type JobState = 'queued' | 'running' | 'done' | 'failed' | 'blocked'

export type JobPayload = {
  id: string
  kind: string
  state: JobState
  params: GenerationParams
  image_url?: string
  images?: Partial<Record<PoseRole, string>>
  error?: string
  queue_position?: number
  created_at: string
}
