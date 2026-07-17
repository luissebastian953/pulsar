// Wire types for core's API (docs/architecture.md#endpoints).

export type TagCategory = 'general' | 'character' | 'copyright' | 'artist' | 'meta'

export type TranslatedTag = {
  canonical: string
  category: TagCategory
  count: number
  concept: string
  stage: 'exact' | 'alias' | 'fuzzy'
}

export type TranslateResult = {
  tags: TranslatedTag[]
  dropped: string[]
}

export type TagSuggestion = {
  canonical: string
  category: TagCategory
  count: number
}

export type GenerationRequest = {
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
  error?: string
  queue_position?: number
  created_at: string
}
