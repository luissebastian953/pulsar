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
  // Precise Reference: uploaded id + fidelity 0-1.2
  reference_id?: string
  ref_strength?: number
  ref_mode?: 'character' | 'style' | 'character_style'
  ref_fidelity?: number
  hires?: boolean
  // refine upscale factor 1.1..2.0 (default 1.5 = Large; 2.0 = Ultra)
  hires_scale?: number
  // 2-3 entries: each prompt conditions its own column (regional);
  // reference_id per character needs exactly 2, both set
  characters?: { prompt: string, reference_id?: string, ref_strength?: number }[]
  loras?: { name: string, strength?: number }[]
  checkpoint?: string
  // model lineage: eps (illustrious) or v-pred (noobai). Overrides the
  // deployment default; pair with a matching checkpoint.
  lineage?: 'illustrious' | 'noobai'
  // v-pred colour-burn rescale (0..1); only meaningful on a v-pred lineage.
  cfg_rescale?: number
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

// Marshaled assembly.Spec persisted per job and returned by /history — the
// core fields are always present; the rest appear when the generation used
// them. This is the "config stored with the image" the history modal reads.
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
  cfg_rescale?: number
  checkpoint?: string
  hires?: boolean
  hires_scale?: number
  hires_denoise?: number
  denoise?: number
  loras?: { name: string, strength?: number }[]
  characters?: { prompt: string, reference_id?: string, ref_strength?: number }[]
  reference_id?: string
  ref_mode?: 'character' | 'style' | 'character_style'
  ref_strength?: number
  ref_fidelity?: number
  quality_tags?: boolean
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
