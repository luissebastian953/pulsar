import { useLocalStorage } from '@vueuse/core'
import type { GenerationParams } from '~/types/api'

// A stored image: its bytes live in IndexedDB under `blob` (see useBlobStore);
// name/type let us reconstruct a File to re-upload to core on apply, because
// core blob ids are ephemeral (its /data is wiped on container recreate — see
// docs/tuning.md ops notes). Empty `blob` means no image in that slot.
export interface PresetImage {
  blob: string
  name: string
  type: string
  width: number
  height: number
}

// PresetConfig is the complete, serializable snapshot of the composer's left
// panel — every field a generation depends on. Primitive-only + PresetImage
// refs, so it round-trips through JSON and stays well under localStorage's quota
// (the actual pixels live in IndexedDB, keyed by PresetImage.blob). Adding a
// field is safe: applyPreset in create.vue reads every field through a default,
// so presets saved before the field existed still load.
export interface PresetConfig {
  // describe
  prompt: string
  mode: 'plain' | 'tags'
  tagList: string[]
  und: string
  chars: { text: string, enabled: boolean, ref: PresetImage | null }[]
  // image
  aspect: 'portrait' | 'landscape' | 'square'
  batch: number
  sizeTier: 'normal' | 'large' | 'ultra'
  // advanced
  lineage: 'illustrious' | 'noobai'
  activeCheckpoint: string
  steps: number
  guidance: number
  cfgRescale: number
  seedText: string
  sampler: string
  scheduler: string
  qualityTags: boolean
  faceDetail: boolean
  loraName: string
  loraStrength: number
  lora2Name: string
  lora2Strength: number
  // reference (Precise Reference + pose handoff)
  charRef: PresetImage | null
  charRefMode: 'character_style' | 'character' | 'style'
  charRefStrength: number
  charRefFidelity: number
  poseRef: { seed: string, label: string } | null
  refStrength: number
  // image to image / inpaint
  i2i: PresetImage | null
  i2iStrength: number
  mask: PresetImage | null
}

// blobKeys lists every IndexedDB key a config owns, so deleting presets can
// garbage-collect their images.
export function blobKeys(c: PresetConfig): string[] {
  const keys: string[] = []
  for (const img of [c.charRef, c.i2i, c.mask, ...c.chars.map(ch => ch.ref)]) {
    if (img?.blob) keys.push(img.blob)
  }

  return keys
}

export interface Preset {
  id: string
  name: string
  createdAt: number
  config: PresetConfig
}

// Bump the version suffix on a breaking schema change to abandon (rather than
// misread) presets from an older shape. The composable is the single owner of
// persistence, so swapping this backend for IndexedDB later touches one file.
const STORAGE_KEY = 'pulsar.presets.v1'

export function usePresets() {
  const presets = useLocalStorage<Preset[]>(STORAGE_KEY, [])

  function save(name: string, config: PresetConfig): Preset {
    const preset: Preset = {
      id: crypto.randomUUID(),
      name: name.trim() || 'Untitled',
      createdAt: Date.now(),
      config,
    }
    // newest first, so the drawer lists most-recent at the top
    presets.value = [preset, ...presets.value]
    return preset
  }

  function remove(ids: string[]) {
    const drop = new Set(ids)
    presets.value = presets.value.filter(p => !drop.has(p.id))
  }

  function rename(id: string, name: string) {
    presets.value = presets.value.map(p =>
      p.id === id ? { ...p, name: name.trim() || p.name } : p)
  }

  return { presets, save, remove, rename }
}

// configSignature identifies "the same configuration" for saved-match detection
// in the history modal — the fields that define a generation's recipe, ignoring
// volatile ids/previews. Two presets with the same signature are the same setup.
export function configSignature(c: PresetConfig): string {
  return JSON.stringify([
    c.prompt, c.tagList, c.und, c.lineage, c.activeCheckpoint,
    c.steps, c.guidance, c.cfgRescale, c.seedText, c.sampler, c.scheduler,
    c.aspect, c.sizeTier, c.loraName, c.loraStrength, c.lora2Name, c.lora2Strength,
  ])
}

const ASPECT_BY_DIMS: Record<string, PresetConfig['aspect']> = {
  '832x1216': 'portrait',
  '1216x832': 'landscape',
  '1024x1024': 'square',
}

// paramsToPresetConfig turns a stored generation's params (from /history) into
// an applyable config. The stored prompt/negative already include the
// server-side quality tags and safety negative, so quality tags are set off to
// avoid double-adding, and the prompt is carried as an exact tag list. Uploaded
// source/reference images are NOT restored here (their blobs aren't in the
// browser) — Save-as-preset from the composer is the path for full image round-trip.
export function paramsToPresetConfig(p: GenerationParams): PresetConfig {
  const hires = !!p.hires
  const sizeTier: PresetConfig['sizeTier']
    = hires ? ((p.hires_scale ?? 1.5) >= 2 ? 'ultra' : 'large') : 'normal'
  const [l1, l2] = p.loras ?? []
  return {
    prompt: '',
    mode: 'tags',
    tagList: p.prompt.split(',').map(t => t.trim()).filter(Boolean),
    und: (p.negative ?? '').trim(),
    chars: (p.characters ?? []).map(c => ({ text: c.prompt, enabled: true, ref: null })),
    aspect: ASPECT_BY_DIMS[`${p.width}x${p.height}`] ?? 'portrait',
    batch: 1,
    sizeTier,
    lineage: (p.lineage === 'noobai' ? 'noobai' : 'illustrious'),
    activeCheckpoint: p.checkpoint ?? '',
    steps: p.steps,
    guidance: p.cfg,
    cfgRescale: p.cfg_rescale ?? 0.55,
    seedText: p.seed != null ? String(p.seed) : '',
    sampler: p.sampler,
    scheduler: p.scheduler,
    qualityTags: false,
    faceDetail: true,
    loraName: l1?.name ?? '',
    loraStrength: l1?.strength ?? 0.8,
    lora2Name: l2?.name ?? '',
    lora2Strength: l2?.strength ?? 0.65,
    charRef: null,
    charRefMode: p.ref_mode ?? 'character_style',
    charRefStrength: p.ref_strength ?? 0.8,
    charRefFidelity: p.ref_fidelity ?? 0.5,
    poseRef: null,
    refStrength: 0.6,
    i2i: null,
    i2iStrength: p.denoise ?? 0.6,
    mask: null,
  }
}

// usePendingConfig is the cross-page hand-off: the history modal sets a config
// to apply, navigates to /create, and the composer consumes + clears it.
export function usePendingConfig() {
  return useState<PresetConfig | null>('pulsar:pending-config', () => null)
}
