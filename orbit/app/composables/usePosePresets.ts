import { useLocalStorage } from '@vueuse/core'
import type { GenerationParams } from '~/types/api'

// The /pose composer's closed tag tables. Counts are display/sort weights
// mirroring Danbooru magnitudes (the DB is ground truth — docs/tagdb.md);
// they only order chips in the resolved strip, nothing server-side reads them.
export interface PoseTagOpt {
  tag: string
  count: number
}

export const POSE_CAMERA: PoseTagOpt[] = [
  { tag: 'from_above', count: 700000 },
  { tag: 'from_below', count: 500000 },
  { tag: 'from_side', count: 400000 },
  { tag: 'from_behind', count: 600000 },
  { tag: 'dutch_angle', count: 150000 },
  { tag: 'foreshortening', count: 80000 },
]

export const POSE_FRAMING: PoseTagOpt[] = [
  { tag: 'full_body', count: 1200000 },
  { tag: 'upper_body', count: 1600000 },
  { tag: 'cowboy_shot', count: 500000 },
  { tag: 'portrait', count: 800000 },
]

// Each prop pill emits holding_X plus the object tag — the pair adheres far
// better than either alone (docs/tagdb.md#pose-subset).
export interface PosePropOpt {
  key: string
  label: string
  tags: PoseTagOpt[]
}

export const POSE_PROPS: PosePropOpt[] = [
  { key: 'weapon', label: 'weapon', tags: [{ tag: 'weapon', count: 1000000 }, { tag: 'holding_weapon', count: 400000 }] },
  { key: 'sword', label: 'sword', tags: [{ tag: 'sword', count: 700000 }, { tag: 'holding_sword', count: 150000 }] },
  { key: 'katana', label: 'katana', tags: [{ tag: 'katana', count: 200000 }, { tag: 'holding_sword', count: 150000 }] },
  { key: 'polearm', label: 'polearm', tags: [{ tag: 'polearm', count: 100000 }, { tag: 'holding_polearm', count: 40000 }] },
  { key: 'gun', label: 'gun', tags: [{ tag: 'gun', count: 300000 }, { tag: 'holding_gun', count: 180000 }] },
  { key: 'bow', label: 'bow', tags: [{ tag: 'bow_(weapon)', count: 60000 }, { tag: 'holding_bow_(weapon)', count: 25000 }] },
  { key: 'staff', label: 'staff', tags: [{ tag: 'staff', count: 90000 }, { tag: 'holding_staff', count: 35000 }] },
  { key: 'shield', label: 'shield', tags: [{ tag: 'shield', count: 60000 }, { tag: 'holding_shield', count: 25000 }] },
  { key: 'knife', label: 'knife', tags: [{ tag: 'knife', count: 100000 }, { tag: 'holding_knife', count: 70000 }] },
  { key: 'scythe', label: 'scythe', tags: [{ tag: 'scythe', count: 40000 }, { tag: 'holding_scythe', count: 15000 }] },
  { key: 'umbrella', label: 'umbrella', tags: [{ tag: 'umbrella', count: 160000 }, { tag: 'holding_umbrella', count: 90000 }] },
  { key: 'book', label: 'book', tags: [{ tag: 'book', count: 260000 }, { tag: 'holding_book', count: 100000 }] },
  { key: 'phone', label: 'phone', tags: [{ tag: 'cellphone', count: 220000 }, { tag: 'holding_phone', count: 160000 }] },
  { key: 'microphone', label: 'microphone', tags: [{ tag: 'microphone', count: 110000 }, { tag: 'holding_microphone', count: 45000 }] },
  { key: 'guitar', label: 'guitar', tags: [{ tag: 'guitar', count: 60000 }, { tag: 'holding_instrument', count: 35000 }] },
]

// background=false steers onto the clean-sheet distribution: no scene lines in
// the extracted sketch, and OpenPose detects far more reliably.
export const POSE_BG_TAGS: PoseTagOpt[] = [
  { tag: 'simple_background', count: 5400000 },
  { tag: 'white_background', count: 1900000 },
]

// PoseConfig is the complete serializable snapshot of the /pose composer —
// primitives only, so it round-trips through JSON. Every field is read through
// a default on apply, so configs saved before a field existed still load.
export interface PoseConfig {
  prompt: string
  camera: string[]
  framing: string[]
  girls: number
  boys: number
  // true = scene allowed; false = clean white sheet (the sketch default)
  background: boolean
  // explicit "nothing in hand" — negatives weapons/props the model would
  // otherwise invent for action poses; mutually exclusive with props
  emptyHands: boolean
  props: string[] // keys into POSE_PROPS
}

export interface PosePreset {
  id: string
  name: string
  createdAt: number
  config: PoseConfig
}

// Separate store from the /create presets — the shapes share nothing.
const STORAGE_KEY = 'pulsar.pose-presets.v1'

export function usePosePresets() {
  const presets = useLocalStorage<PosePreset[]>(STORAGE_KEY, [])

  function save(name: string, config: PoseConfig): PosePreset {
    const preset: PosePreset = {
      id: crypto.randomUUID(),
      name: name.trim() || 'Untitled',
      createdAt: Date.now(),
      config,
    }
    presets.value = [preset, ...presets.value]
    return preset
  }

  function remove(ids: string[]) {
    const drop = new Set(ids)
    presets.value = presets.value.filter(p => !drop.has(p.id))
  }

  return { presets, save, remove }
}

// Same-recipe detection for the history modal's "Saved" state.
export function poseConfigSignature(c: PoseConfig): string {
  return JSON.stringify([
    c.prompt, [...c.camera].sort(), [...c.framing].sort(),
    c.girls, c.boys, c.background, c.emptyHands, [...c.props].sort(),
  ])
}

// server-side quality tags stored with the prompt; stripped on round-trip
const QUALITY_TAGS = new Set(['masterpiece', 'best_quality', 'very_aesthetic', 'absurdres', 'very_awa'])

// paramsToPoseConfig turns a stored pose generation's params back into an
// applyable composer config: known tags land in their controls, the remainder
// becomes prompt text (canonical tags ground exactly, so nothing is lost).
export function paramsToPoseConfig(p: GenerationParams): PoseConfig {
  // structured rows: the user's own tags by name (quality/style/anchors are
  // core's); legacy rows: the emitted string, split
  const raw = p.prompt_tags
    ? p.prompt_tags.filter(t => t.source === 'user').map(t => t.name)
    : p.prompt.split(',')
  let tags = raw
    .map(t => t.trim().replaceAll(' ', '_'))
    .filter(t => t && !QUALITY_TAGS.has(t) && t !== 'solo')

  const take = (tag: string): boolean => {
    const had = tags.includes(tag)
    if (had) tags = tags.filter(t => t !== tag)

    return had
  }

  let girls = 0
  let boys = 0
  for (const t of [...tags]) {
    const m = t.match(/^([1-5])(girl|boy)s?$/)
    if (!m) continue

    if (m[2] === 'girl') girls = Number(m[1])
    else boys = Number(m[1])

    take(t)
  }

  take('no_humans')

  const camera = POSE_CAMERA.filter(c => take(c.tag)).map(c => c.tag)
  const framing = POSE_FRAMING.filter(f => take(f.tag)).map(f => f.tag)
  const bgHidden = POSE_BG_TAGS.map(b => take(b.tag)).some(Boolean)
  // a prop matches only when its full tag pair is present; a lone "sword"
  // typed in prose stays prompt text
  const props = POSE_PROPS
    .filter(pr => pr.tags.every(t => tags.includes(t.tag)))
    .map((pr) => {
      pr.tags.forEach(t => take(t.tag))
      return pr.key
    })

  return {
    prompt: tags.map(t => t.replaceAll('_', ' ')).join(', '),
    camera,
    framing: framing.length ? framing : ['full_body'],
    girls,
    boys,
    background: !bgHidden,
    // emptyHands lives in the negative; the composed marker is "weapon, holding"
    emptyHands: /\bweapon, holding\b/.test(p.negative ?? ''),
    props,
  }
}

// Cross-page hand-off: the history modal sets a config, navigates to /pose,
// the composer consumes + clears it (mirrors usePendingConfig for /create).
export function usePendingPoseConfig() {
  return useState<PoseConfig | null>('pulsar:pending-pose-config', () => null)
}
