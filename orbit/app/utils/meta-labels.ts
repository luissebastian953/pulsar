// Display strings for the image inspector. Nothing here is file content.
import type { EntrySource, Generator, ImgMetaErrorCode, WarningCode } from '~/utils/imgmeta/types'

export const GENERATOR_LABELS: Record<Generator, string> = {
  pulsar: 'pulsar',
  novelai: 'NovelAI',
  a1111: 'A1111 / Forge',
  comfyui: 'ComfyUI',
  swarmui: 'SwarmUI',
  invokeai: 'InvokeAI',
}

export const SOURCE_LABELS: Record<EntrySource, string> = {
  'png:tEXt': 'PNG tEXt',
  'png:zTXt': 'PNG zTXt',
  'png:iTXt': 'PNG iTXt',
  'png:comf': 'PNG comf',
  'exif': 'EXIF',
  'xmp': 'XMP',
  'jpeg:COM': 'JPEG comment',
  'stealth': 'pixel data',
}

export const ERROR_MESSAGES: Record<ImgMetaErrorCode, string> = {
  too_large: 'That file is over 50 MB — too large to inspect.',
  unsupported_format: 'Only PNG, JPEG and WebP images can be inspected.',
  malformed: 'That file is damaged: its image header could not be read.',
  timeout: 'Reading that file took too long and was stopped.',
  aborted: 'Stopped.',
  worker: 'The metadata reader could not run in this browser.',
}

export const WARNING_MESSAGES: Record<WarningCode, string> = {
  truncated: 'The file ends early; metadata after that point was not read.',
  bad_crc: 'A metadata chunk failed its checksum. It is shown, but may be damaged.',
  bad_chunk: 'A damaged metadata chunk was skipped.',
  bad_compressed: 'Compressed metadata could not be unpacked.',
  entry_too_large: 'A metadata entry was too large and was skipped.',
  total_too_large: 'The file holds more metadata than is read; the rest was skipped.',
  too_many_entries: 'The file holds more metadata entries than are read; the rest were skipped.',
  bad_exif: 'The EXIF block is damaged.',
  pixels_over_cap: 'The image is too large to scan its pixels for hidden metadata or a signature.',
  pixels_unavailable: 'The pixels could not be read exactly here, so hidden metadata and the signature were not checked.',
  stealth_bad_length: 'Pixel-embedded metadata has an impossible length and was ignored.',
  stealth_too_large: 'Pixel-embedded metadata was too large and was ignored.',
  stealth_bad_payload: 'Pixel-embedded metadata could not be decoded.',
  bad_json: 'A metadata entry that should be JSON could not be parsed.',
  pulsar_unknown_version: 'This pulsar image is from a newer version; its own metadata was not read.',
  comfy_walk_truncated: 'The ComfyUI graph is too large to follow completely.',
  comfy_workflow_fallback: 'The prompt was read from the saved ComfyUI editor state and may be stale.',
}

/** Raw entries longer than this are cut for display (the file is never re-read). */
export const MAX_DISPLAY_CHARS = 100_000
