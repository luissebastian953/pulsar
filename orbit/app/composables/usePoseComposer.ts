import type { GenerationRequest, TranslatedTag } from '~/types/api'
import {
  POSE_BG_TAGS, POSE_CAMERA, POSE_FRAMING, POSE_PROPS,
  usePendingPoseConfig, usePosePresets,
  type PoseConfig, type PosePreset,
} from '~/composables/usePosePresets'

// The whole /pose composer: pill state, the combined tag strip, the 4-cell
// batch generation, and preset glue. The page owns template wiring, the
// detail modal, and its open flags. Grounds against the closed pose
// vocabulary (docs/tagdb.md#pose-subset).
export function usePoseComposer() {
  const api = useApi()

  const posePrompt = ref('')
  const translate = useTranslate('pose')
  watch(posePrompt, t => translate.onInput(t, 'plain'))

  // prompt chunks: the pose box takes `!macro:Name!` like any prompt field.
  // /translate expands it (chunk text skips the LLM) and the tags join the
  // strip below, so /generate is sent plain tags and needs no chunks.
  const chunkStore = useChunks()
  const poseLive = usePromptParse(() => posePrompt.value, 'prompt', () => '')
  watch(() => JSON.stringify(chunkStore.referenced([posePrompt.value])), () => {
    if (posePrompt.value.trim()) translate.onInput(posePrompt.value, 'plain')
  })
  onScopeDispose(chunkStore.registerField(async (edit) => {
    posePrompt.value = await edit(posePrompt.value, 'prompt')
  }))

  const GIRL_N = [0, 5000000, 1500000, 400000, 150000, 80000]
  const BOY_N = [0, 1700000, 350000, 90000, 40000, 20000]

  const camera = ref<string[]>([])
  const framing = ref<string[]>(['full_body'])
  const figure = reactive({ girls: 1, boys: 0 })
  // false = clean white sheet (the sketch default): figure-only lineart and far
  // more reliable skeleton detection. true = let the model paint a scene.
  const background = ref(false)
  // props are mutually exclusive with the explicit empty-hands state — action
  // poses invent weapons unless negatived away
  const heldProps = ref<string[]>([])
  const emptyHands = ref(false)

  function toggleProp(key: string) {
    emptyHands.value = false
    toggleInPlace(heldProps.value, key)
  }

  function toggleEmptyHands() {
    emptyHands.value = !emptyHands.value

    if (emptyHands.value) heldProps.value = []
  }

  const mk = (tag: string, count: number): TranslatedTag =>
    ({ canonical: tag, category: 'general', count, concept: '', stage: 'exact' })

  const combined = computed<TranslatedTag[]>(() => {
    const out: TranslatedTag[] = []

    if (figure.girls > 0) out.push(mk(figureTag(figure.girls, 'girl'), GIRL_N[Math.min(figure.girls, 5)]!))

    if (figure.boys > 0) out.push(mk(figureTag(figure.boys, 'boy'), BOY_N[Math.min(figure.boys, 5)]!))

    if (figure.girls + figure.boys === 0) out.push(mk('no_humans', 600000))

    out.push(...translate.tags.value)
    out.push(...POSE_CAMERA.filter(c => camera.value.includes(c.tag)).map(c => mk(c.tag, c.count)))
    out.push(...POSE_FRAMING.filter(f => framing.value.includes(f.tag)).map(f => mk(f.tag, f.count)))

    for (const p of POSE_PROPS) {
      if (heldProps.value.includes(p.key)) out.push(...p.tags.map(t => mk(t.tag, t.count)))
    }

    if (!background.value) out.push(...POSE_BG_TAGS.map(t => mk(t.tag, t.count)))

    // solo steers OpenPose onto one detectable figure (.claude/skills/
    // comfy-workflow) — skipped when the prompt itself asks for multiple views
    const multiView = out.some(t => ['multiple_views', 'turnaround', 'reference_sheet'].includes(t.canonical))

    if (figure.girls + figure.boys === 1 && !multiView) out.push(mk('solo', 5000000))

    const seen = new Set<string>() // a prop pill + typed prose can double a tag

    return out.filter(t => !seen.has(t.canonical) && !!seen.add(t.canonical))
      .sort((a, b) => b.count - a.count)
  })

  const stat = computed(() => {
    if (translate.resolving.value) return 'resolving…'

    const mapped = combined.value.length
    const total = mapped + translate.dropped.value.length

    if (total === 0) return '—'

    return translate.dropped.value.length
      ? `${mapped}/${total} mapped · ${translate.dropped.value.length} dropped`
      : `${mapped}/${total} mapped`
  })

  // ── quick starts ──────────────────────────────────────────────────
  function presetTurnaround() {
    posePrompt.value = 'turnaround, multiple views, standing, arms at sides'
    camera.value = []
    framing.value = ['full_body']
    background.value = false
    heldProps.value = []
    emptyHands.value = false
  }

  function presetAction() {
    posePrompt.value = 'jumping, twisting to look back, reaching toward the viewer, action pose'
    camera.value = ['from_below', 'foreshortening']
    framing.value = ['full_body']
    background.value = false
  }

  // ── recent: mirrors the Pose history (12 newest done jobs) ────────
  const poseThumb = (j: { images?: Record<string, string>, image_url?: string }) =>
    j.images?.lineart ?? j.image_url ?? ''
  const recent = useRecentHistory(
    j => j.kind === 'pose' && !!(j.image_url || j.images),
    j => ({ id: j.id, imageUrl: api.imageUrl(poseThumb(j)), title: `seed ${j.params.seed}` }),
  )

  // ── generation: batch of 4 real jobs ──────────────────────────────
  const cells = [useJob(recent.add), useJob(recent.add), useJob(recent.add), useJob(recent.add)]
  const selected = ref(0)
  const tab = ref<'lineart' | 'skeleton' | 'source'>('source')
  const busy = computed(() => cells.some(c => c.busy.value))
  const anyDone = computed(() => cells.some(c => c.phase.value === 'done'))

  // full-figure sketches keep every limb in frame; applied only when full body
  // is the sole framing — mixing framings means the user wants a crop
  const ANTI_CROP = 'cropped legs, cropped torso, cropped arms, feet out of frame, head out of frame, close-up'

  function generate() {
    if (busy.value || !combined.value.length) return

    const tags = combined.value.map(t => t.canonical)
    const neg: string[] = []

    if (framing.value.length === 1 && framing.value[0] === 'full_body') neg.push(ANTI_CROP)

    if (emptyHands.value) neg.push('weapon, holding')

    if (tags.includes('solo')) neg.push('multiple views, reference sheet, chibi')

    const req: GenerationRequest = {
      kind: 'pose',
      prompt: tags.join(', '),
      negative: neg.length ? neg.join(', ') : undefined,
      width: 832,
      height: 1216,
      steps: 24,
      quality_tags: true,
    }
    selected.value = 0
    cells.forEach(c => c.start(req))
  }

  // pose → create handoff
  const poseRef = useState<{ seed: string, label: string } | null>('poseRef', () => null)
  const openRefFold = useState('openRefFold', () => false)

  async function usePose() {
    const cell = cells[selected.value]!

    if (cell.phase.value !== 'done') return

    poseRef.value = { seed: cell.seed.value, label: `from /pose · seed ${cell.seed.value}` }
    openRefFold.value = true
    await navigateTo('/create')
  }

  // ── configurations (presets) ──────────────────────────────────────
  const { presets, save: savePreset, remove: removePresets } = usePosePresets()

  function buildConfig(): PoseConfig {
    return {
      prompt: posePrompt.value,
      camera: [...camera.value],
      framing: [...framing.value],
      girls: figure.girls,
      boys: figure.boys,
      background: background.value,
      emptyHands: emptyHands.value,
      props: [...heldProps.value],
    }
  }

  function applyConfig(cfg: PoseConfig) {
    posePrompt.value = cfg.prompt ?? ''
    camera.value = [...(cfg.camera ?? [])]
    framing.value = [...(cfg.framing ?? ['full_body'])]
    figure.girls = cfg.girls ?? 1
    figure.boys = cfg.boys ?? 0
    background.value = cfg.background ?? false
    emptyHands.value = cfg.emptyHands ?? false
    heldProps.value = [...(cfg.props ?? [])]
  }

  function poseSubtitle(p: PosePreset): string {
    const c = p.config
    const fig = [c.girls ? figureTag(c.girls, 'girl') : '', c.boys ? figureTag(c.boys, 'boy') : '']
      .filter(Boolean).join(' ')

    return [
      fig || 'no humans',
      c.background ? 'scene' : 'clean bg',
      c.props.length ? c.props.join(' ') : (c.emptyHands ? 'empty hands' : ''),
    ].filter(Boolean).join(' · ')
  }

  const drawerPresets = computed(() =>
    presets.value.map(p => ({ id: p.id, name: p.name, subtitle: poseSubtitle(p) })))

  function onSaveName(name: string) {
    savePreset(name, buildConfig())
  }

  function onUsePreset(id: string) {
    const p = presets.value.find(x => x.id === id)

    if (p) applyConfig(p.config)
  }

  function onDeletePresets(ids: string[]) {
    removePresets(ids)
  }

  // "Use related config" from the /history pose tab hands a config off here;
  // immediate covers cold navigation, the watch covers keep-alive re-entry.
  const pendingPoseConfig = usePendingPoseConfig()
  watch(pendingPoseConfig, (c) => {
    if (!c) return

    pendingPoseConfig.value = null
    applyConfig(c)
  }, { immediate: true })

  return {
    // composer
    posePrompt, poseLive, translate, camera, framing, figure, background, heldProps,
    emptyHands, toggleProp, toggleEmptyHands, combined, stat,
    presetTurnaround, presetAction,
    // generation
    cells, selected, tab, busy, anyDone, generate, usePose,
    // recent
    recent,
    // presets
    presets, drawerPresets, applyConfig, onSaveName, onUsePreset, onDeletePresets,
  }
}
