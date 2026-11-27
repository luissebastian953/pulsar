// Limits for hostile files. Every image handed to the inspector is untrusted:
// the parse runs inside an app that holds a bearer token in memory. Sources
// for the numbers are in reports/"NovelAI prompt chunks and image inspector"
// ("Treat every file as hostile").

/** Files above this are rejected before a single byte is parsed. */
export const MAX_FILE_BYTES = 50 * 1024 * 1024

/** PNG spec maximum; the walker further bounds it by the bytes remaining. */
export const MAX_CHUNK_LENGTH = 0x7fffffff

/** Text-bearing chunks/tags collected per file; the rest are ignored. */
export const MAX_TEXT_ENTRIES = 64

/** Largest single metadata text, raw or decompressed. */
export const MAX_ENTRY_BYTES = 4 * 1024 * 1024

/** All metadata text of one file together. */
export const MAX_TOTAL_BYTES = 16 * 1024 * 1024

/** Keys are short by spec (PNG: 79 bytes); longer ones are cut. */
export const MAX_KEY_CHARS = 128

/** Decoded pixels allowed before any LSB scan or signature check (2^24). */
export const MAX_PIXELS = 16_777_216

/** Stealth payload after gunzip (or raw, for the uncompressed variants). */
export const MAX_STEALTH_BYTES = 1024 * 1024

/** IDAT segments tracked for the pixel decode. */
export const MAX_IDAT_SEGMENTS = 65_536

/** JPEG segments / RIFF chunks walked before giving up. */
export const MAX_SEGMENTS = 10_000

/** Entries read from one EXIF IFD. */
export const MAX_IFD_ENTRIES = 512

/** JSON nesting accepted from metadata (guards parse and structured clone). */
export const MAX_JSON_DEPTH = 128

/** ComfyUI graph nodes visited per prompt walk: diamonds cost 2^depth. */
export const COMFY_VISIT_BUDGET = 512

/** The worker is terminated when a parse runs longer than this. */
export const WORKER_TIMEOUT_MS = 10_000
