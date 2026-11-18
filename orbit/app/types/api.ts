// Wire types for core's API (docs/architecture.md#endpoints).

export type TagCategory = 'general' | 'character' | 'copyright' | 'artist' | 'meta'

export type TranslatedTag = {
  canonical: string
  category: TagCategory
  count: number
  concept: string
  // 'chunk': the tag came out of a prompt chunk and never went to the LLM
  stage: 'exact' | 'alias' | 'fuzzy' | 'live' | 'chunk'
  // NovelAI-unit weight the source text carried (1 = unweighted; < 0 is
  // relocated to Undesired Content by core)
  weight?: number
  // the prompt chunk the tag came out of
  chunk?: string
}

export type TranslateResult = {
  tags: TranslatedTag[]
  dropped: string[]
  // the tags in NovelAI syntax, ready to send — orbit never builds syntax
  prompt?: string
  notices?: PromptIssue[]
}

// ── prompt grammar (core/internal/prompt; docs/frontend.md#prompt-validation)
// Offsets are UTF-16 code units into the surface text (JS string indices);
// `tag` is the comma/newline piece index.
export type PromptIssue = {
  code: string
  message: string
  hint?: string
  start: number
  end: number
  tag: number
}

export type PromptSurface = 'prompt' | 'negative' | 'character'

export type PromptSource
  = | 'user' | 'safety' | 'default_uc' | 'quality'
    | 'style_prefix' | 'style_suffix' | 'style_negative' | 'anchor'

export type PromptRun = { text: string, w: number, emitted: number }

// ── prompt chunks (docs/frontend.md#prompt-chunks) ──────────────────
// A chunk as sent with a request: orbit stores chunks and sends the ones a
// text references; core expands `!macro:Name!` and owns every rule.
export type ChunkPayload = { name: string, content: string }

// a tag's provenance: the chunk it came out of and core's hash of what
// that chunk resolved to
export type ChunkRef = { name: string, hash: string }

// one resolved `!macro:Name!` in a parsed text: where the token sits and
// the text it expands to (nested chunks included)
export type ChunkSpan = ChunkRef & { start: number, end: number, tag: number, text: string }

// typed weighting located by core, for the emphasis highlight: weighted
// text (up / down) and operators (`{ } [ ] N:: ::`); `w` is NovelAI units
export type PromptMark = { start: number, end: number, kind: 'up' | 'down' | 'op', w: number }

// a tag as stored on the job row (assembly.Spec)
export type PromptTag = {
  runs: PromptRun[]
  text: string
  name: string
  source: PromptSource
  nai?: string
  relocated?: boolean
  chunk?: ChunkRef
}

// a tag from POST /prompt/parse, located in the surface
export type ParsedTag = {
  text: string
  name: string
  nai: string
  start: number
  end: number
  tag: number
  runs: PromptRun[]
  relocated?: boolean
  // the tag came out of this chunk reference; start/end are the token's
  chunk?: ChunkRef
}

export type PromptParseResult = {
  tags: ParsedTag[]
  relocated: ParsedTag[]
  errors: PromptIssue[]
  notices: PromptIssue[]
  // absent on a core older than prompt chunks
  chunks?: ChunkSpan[]
  marks?: PromptMark[]
  emitted: string
}

// core's 400 for a prompt that failed to parse; `error` is the message
export type PromptErrorBody = PromptIssue & {
  error: string
  field: string
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

// A style preset row from GET /styles (docs/tuning.md#style-presets). Prompt
// fields, negative and LoRA merge in core; the settings are the recipe the
// composer writes into its own fields on selection (request values win in
// core, so what's shown is what runs). `hires` is a size-tier hint only.
export type StylePreset = {
  slug: string
  lineage: 'illustrious' | 'noobai'
  name: string
  description: string
  family: string
  lora?: string
  lora_strength?: number
  prompt_prefix: string
  prompt_suffix: string
  negative: string
  sampler?: string
  scheduler?: string
  cfg?: number
  steps?: number
  cfg_rescale?: number
  hires?: boolean
  hires_scale?: number
  hires_denoise?: number
  // false = the preset's LoRA is not installed on this deployment
  available: boolean
}

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
  // style preset slug for this lineage (GET /styles); merged in core
  style?: string
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
  // the prompt chunks the surfaces reference as `!macro:Name!`
  chunks?: ChunkPayload[]
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
  characters?: { prompt: string, reference_id?: string, ref_strength?: number, tags?: PromptTag[] }[]
  reference_id?: string
  ref_mode?: 'character' | 'style' | 'character_style'
  ref_strength?: number
  ref_fidelity?: number
  quality_tags?: boolean
  style?: string
  // structured copies of the emitted strings (absent on legacy rows)
  prompt_tags?: PromptTag[]
  negative_tags?: PromptTag[]
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
