// Shared types of the image-metadata library. Everything returned is plain,
// structured-cloneable data (it crosses a Worker boundary), and every string
// in it is UNTRUSTED file content: render with {{ }} only, never v-html, never
// as a URL, never as a path.
import { MAX_ENTRY_BYTES, MAX_KEY_CHARS, MAX_TEXT_ENTRIES, MAX_TOTAL_BYTES } from './limits'

/** A byte view over a real ArrayBuffer (what Blob and WebCrypto accept). */
export type Bytes = Uint8Array<ArrayBuffer>

export type ImageFormat = 'png' | 'jpeg' | 'webp'

/** Where a raw metadata text was found. */
export type EntrySource
  = | 'png:tEXt' | 'png:zTXt' | 'png:iTXt' | 'png:comf'
    | 'exif' | 'xmp' | 'jpeg:COM' | 'stealth'

/** One raw metadata text, exactly as stored (after charset decoding). */
export interface RawEntry {
  source: EntrySource
  /** PNG keyword, EXIF tag name, `XMP`, `COM`, or the stealth signature. */
  key: string
  text: string
}

export type WarningCode
  = | 'truncated' // a chunk/segment ran past the end of the file; the walk stopped there
    | 'bad_crc' // a PNG metadata chunk failed its CRC (still shown)
    | 'bad_chunk' // a metadata chunk was structurally invalid and was skipped
    | 'bad_compressed' // zTXt/iTXt/gzip data did not inflate
    | 'entry_too_large' // one text exceeded MAX_ENTRY_BYTES (inflate aborted or text skipped)
    | 'total_too_large' // the file's texts together exceeded MAX_TOTAL_BYTES
    | 'too_many_entries' // more than MAX_TEXT_ENTRIES texts; the rest were ignored
    | 'bad_exif' // EXIF had an invalid header or pointer
    | 'pixels_over_cap' // dimensions exceed MAX_PIXELS: no stealth scan, no signature check
    | 'pixels_unavailable' // pixels could not be decoded bit-exactly here
    | 'stealth_bad_length' // stealth length not a multiple of 8, zero, or beyond the image capacity
    | 'stealth_too_large' // stealth payload exceeded MAX_STEALTH_BYTES
    | 'stealth_bad_payload' // stealth payload did not gunzip / decode
    | 'bad_json' // a recognised key held unparseable (or too deep) JSON
    | 'pulsar_unknown_version' // a `pulsar` chunk with a `v` this reader does not know
    | 'comfy_walk_truncated' // the ComfyUI prompt walk hit its visit budget
    | 'comfy_workflow_fallback' // prompt text came from `workflow` widgets and may be stale

export interface MetaWarning {
  code: WarningCode
  detail?: string
}

/** RGBA, 8 bits per channel, row-major, not premultiplied. */
export interface PixelData {
  width: number
  height: number
  data: Uint8Array | Uint8ClampedArray
}

export type Generator = 'pulsar' | 'novelai' | 'swarmui' | 'a1111' | 'invokeai' | 'comfyui'

export interface CharacterPrompt {
  prompt: string
  negative?: string
  /** NovelAI `centers[0]`, 0..1. */
  x?: number
  y?: number
}

/**
 * Pulsar's own download metadata: the JSON held by the `pulsar` iTXt chunk
 * of a `GET /images/{id}?meta=1` download. This is the contract with the Go
 * writer (core/internal/outmeta, `Payload`) — JSON keys are exactly these
 * names; the fields up to `kind` are always written, the rest only when the
 * generation used them. Only user-authored values belong here; see
 * docs/compliance.md#output-metadata for what must never be written (the
 * safety and default negatives, quality and style text, upload ids, file
 * paths, the job id).
 */
export interface PulsarMetaV1 {
  v: 1
  /** User prompt in the SD syntax the model received (lineage-calibrated weights). */
  prompt: string
  /** User negative only — never the forbidden-tag or default negative. */
  negative: string
  /** One prompt string per character, in order. */
  characters: string[]
  width: number
  height: number
  steps: number
  cfg: number
  sampler: string
  scheduler: string
  /** CFG rescale. */
  rescale: number
  seed: number
  /** Model lineage, e.g. `illustrious` or `noobai`. */
  lineage: string
  /** Style preset slug; empty when none. */
  style: string
  /** Job kind, e.g. `txt2img`, `img2img`, `inpaint`, `pose`. */
  kind: string
  /** Base model display name — never a filename or path. */
  model?: string
  /** LoRAs by display name. */
  loras?: { name: string, strength: number }[]
  /** img2img / inpaint strength. */
  denoise?: number
  /** Refine-pass upscale factor and strength; absent without a refine pass. */
  hires_scale?: number
  hires_denoise?: number
  /** Names of the prompt chunks the user's tags came from. */
  chunks?: string[]
  /**
   * The same user text in the dialect the editor takes, with weights as
   * typed. Import prefers it: `prompt`/`negative`/`characters` carry
   * calibrated weights and would be calibrated a second time.
   */
  nai?: { prompt: string, negative: string, characters: string[] }
}

/** A model resource named in the metadata (LoRA, embedding, checkpoint). */
export interface Resource {
  /** `lora`, `embedding`, `model`, … — free text from the file for Civitai-style keys. */
  kind: string
  name: string
  hash?: string
  weight?: number
}

/** NovelAI-specific fields beside the common ones. */
export interface NovelAIInfo {
  title?: string
  description?: string
  software?: string
  source?: string
  generationTime?: string
  /** Resolved from the trailing hash of `Source`; `id` is `unknown` when not in the table. */
  model: { id: string, name: string, hash?: string }
  requestType?: string
  /** `Text to Image`, `Image to Image`, `Inpainting` or `Unknown Request Type`. */
  requestTypeLabel: string
  controlnetModel?: string
  vibeTransfer: boolean
  vibeEmbedded: boolean
  preciseReference: boolean
  smea: 'smea' | 'smea_dyn' | null
  uncondScale?: number
  /** `actual_prompts`: the resolved values of randomised prompt sections. */
  actual?: { prompt?: string, negative?: string, characters: string[] }
  /** The Comment carries a `signed_hash` (says nothing about validity). */
  signed: boolean
}

/** One generator's interpretation of the raw entries. Display-only unless a caller decides otherwise. */
export interface GenParams {
  generator: Generator
  prompt?: string
  negative?: string
  characters: CharacterPrompt[]
  width?: number
  height?: number
  steps?: number
  cfg?: number
  rescale?: number
  /** Seeds above 2^53 lose precision when they come from JSON. */
  seed?: number
  sampler?: string
  scheduler?: string
  model?: string
  modelHash?: string
  /** Remaining settings as display strings (A1111 settings line, LoRAs, …). */
  extra: Record<string, string>
  resources?: Resource[]
  novelai?: NovelAIInfo
  pulsar?: Partial<PulsarMetaV1> & { v: 1 }
  /** The parsed JSON payload when the format has one (Comment, graph, …). */
  raw?: unknown
}

export type NaiSignatureStatus
  = | 'valid' // signed by the given key over these exact pixels and Comment
    | 'invalid'
    | 'no_signature' // no `signed_hash` in the Comment
    | 'unsupported' // JPEG: NovelAI never signs it
    | 'unavailable' // signed but not checkable: no Ed25519 in this WebCrypto, or no bit-exact pixels

export interface ImageMeta {
  format: ImageFormat
  /** From the container header; 0 when it could not be read. */
  width: number
  height: number
  /** Every metadata text found, in file order, then stealth. Show this beside any interpretation. */
  entries: RawEntry[]
  generator: Generator | null
  /** Whether the interpretation came from container metadata or the LSB payload. */
  via: 'container' | 'stealth' | null
  params: GenParams | null
  /** Set only when the generator is NovelAI and verification was not switched off. */
  signature: NaiSignatureStatus | null
  warnings: MetaWarning[]
}

export type ImgMetaErrorCode
  = | 'too_large' // over MAX_FILE_BYTES
    | 'unsupported_format' // not PNG, JPEG or WebP by magic bytes
    | 'malformed' // the container header itself is invalid
    | 'timeout' // the worker was terminated
    | 'aborted' // superseded by a newer parse or cancelled
    | 'worker' // the worker crashed or could not start

export class ImgMetaError extends Error {
  code: ImgMetaErrorCode

  constructor(code: ImgMetaErrorCode, message?: string) {
    super(message ?? code)
    this.name = 'ImgMetaError'
    this.code = code
  }
}

/** Collects entries and warnings under the per-entry, per-file and count caps. */
export class Collector {
  entries: RawEntry[] = []
  warnings: MetaWarning[] = []
  private total = 0

  warn(code: WarningCode, detail?: string) {
    if (this.warnings.some(w => w.code === code && w.detail === detail) || this.warnings.length >= 32) return

    this.warnings.push(detail === undefined ? { code } : { code, detail })
  }

  /** Whether another entry may be collected at all (warns once when not). */
  hasRoom(): boolean {
    if (this.entries.length >= MAX_TEXT_ENTRIES) {
      this.warn('too_many_entries')
      return false
    }

    if (this.total >= MAX_TOTAL_BYTES) {
      this.warn('total_too_large')
      return false
    }

    return true
  }

  /** Bytes the next entry may still occupy. */
  room(): number {
    return Math.max(0, Math.min(MAX_ENTRY_BYTES, MAX_TOTAL_BYTES - this.total))
  }

  /** Record that a text of `length` bytes was refused without being decoded. */
  tooLarge(key: string, length: number) {
    this.warn(length > MAX_ENTRY_BYTES ? 'entry_too_large' : 'total_too_large', key.slice(0, MAX_KEY_CHARS))
  }

  add(source: EntrySource, key: string, text: string): boolean {
    if (!this.hasRoom()) return false

    if (text.length > this.room()) {
      this.tooLarge(key, text.length)
      return false
    }

    this.total += text.length
    this.entries.push({ source, key: key.slice(0, MAX_KEY_CHARS), text })
    return true
  }
}
