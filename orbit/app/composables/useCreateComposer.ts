import type { GenerationRequest, TranslatedTag } from '~/types/api'
import { blobKeys, usePendingConfig, type Preset, type PresetConfig } from '~/composables/usePresets'

// The whole /create composer: state, lineage rules, uploads, generation, and
// preset capture/apply. The page owns only template wiring and modal open
// flags. Called once from the kept-alive page, so all of this survives route
// switches and dies on reload — exactly the previous in-page behaviour.
export function useCreateComposer() {
  const api = useApi()

  // ── composer state ────────────────────────────────────────────────
  const prompt = ref('')
  const mode = ref<'plain' | 'tags'>('plain')
  const tagList = ref<string[]>([])
  const und = ref('')
  const chars = ref<{ text: string, enabled: boolean, refId: string }[]>([])
  const aspect = ref<'portrait' | 'landscape' | 'square'>('portrait')
  const batch = ref(1)
  // defaults match the NoobAI (v-pred) lineage below — the lineage watcher only
  // fires on change, so the initial values must be that lineage's recommended set
  const steps = ref(28)
  const guidance = ref(5.0)
  const seedText = ref('')
  const sampler = ref('euler')
  const scheduler = ref('normal')
  const qualityTags = ref(true)
  const faceDetail = ref(true)
  // size tier = the refine pass: normal (native bucket), large (x1.5), ultra
  // (x2.0). SDXL samples on-bucket only; bigger sizes are hires multiples.
  const sizeTier = ref<'normal' | 'large' | 'ultra'>('normal')
  const hires = computed(() => sizeTier.value !== 'normal')
  const hiresScale = computed(() => (sizeTier.value === 'ultra' ? 2.0 : 1.5))
  const loras = ref<string[]>([])
  const checkpoints = ref<string[]>([])
  const activeCheckpoint = ref('')

  // model lineage: eps (illustrious) vs v-pred (noobai). Settings don't
  // transfer between lineages, so switching resets sampler/guidance/steps to
  // that lineage's recommended values (docs/tuning.md).
  const lineage = ref<'illustrious' | 'noobai'>('noobai')
  const cfgRescale = ref(0.55)
  const LINEAGE_DEFAULTS = {
    illustrious: { steps: 24, guidance: 5.5, sampler: 'euler_ancestral', scheduler: 'karras', rescale: 0 },
    noobai: { steps: 28, guidance: 5.0, sampler: 'euler', scheduler: 'normal', rescale: 0.55 },
  } as const

  // Infer a checkpoint's prediction type from its name (mirrors
  // assembly.CheckpointIsVpred in core) so the picker never lands on a
  // lineage/checkpoint pair core would reject. null = unclassifiable (kept as-is).
  function ckptIsVpred(name: string): boolean | null {
    const n = name.toLowerCase()
    if (/vpred|v-pred|v_pred|vprediction/.test(n)) return true

    if (/eps|illustrious/.test(n)) return false

    return null
  }
  // Keep the current checkpoint if it matches (or is unclassifiable); otherwise
  // switch to the first installed checkpoint whose type matches the lineage.
  function pickCheckpoint(l: 'illustrious' | 'noobai', list: string[], current: string): string {
    const wantVpred = l === 'noobai'
    if (current && ckptIsVpred(current) !== !wantVpred) return current

    return list.find(c => ckptIsVpred(c) === wantVpred) ?? current
  }

  watch(lineage, (l) => {
    const d = LINEAGE_DEFAULTS[l]
    steps.value = d.steps
    guidance.value = d.guidance
    sampler.value = d.sampler
    scheduler.value = d.scheduler
    cfgRescale.value = d.rescale
    activeCheckpoint.value = pickCheckpoint(l, checkpoints.value, activeCheckpoint.value)
  })

  const loraName = ref('')
  const loraStrength = ref(0.8)
  // second slot chains behind the first (character identity + franchise style)
  const lora2Name = ref('')
  const lora2Strength = ref(0.65)

  const folds = reactive({ und: false, chars: false, ref: false, i2i: false, adv: false })

  // pose → create handoff ("Use this pose")
  const poseRef = useState<{ seed: string, label: string } | null>('poseRef', () => null)
  const openRefFold = useState('openRefFold', () => false)
  const refStrength = ref(0.6)
  if (openRefFold.value) {
    folds.ref = true
    openRefFold.value = false
  }

  // Precise Reference: character identity from an uploaded image
  const charRef = useImageUpload()
  const charRefMode = ref<'character_style' | 'character' | 'style'>('character_style')
  const charRefStrength = ref(0.8)
  const charRefFidelity = ref(0.5)

  // image-to-image / inpaint: eager upload on pick so the mask editor can
  // paint on the normalized image (dims always match the bucket)
  const i2i = useImageUpload()
  const i2iStrength = ref(0.6)
  const maskId = ref('')
  const maskEditing = ref(false)

  async function onI2iPick(e: Event) {
    maskId.value = ''
    await i2i.pick(e)
  }

  async function onMaskSave(file: File) {
    maskEditing.value = false
    try {
      const up = await api.upload<{ image_id: string }>('/uploads', file)
      maskId.value = up.image_id
    }
    catch (err: unknown) {
      i2i.error.value = apiErrorMessage(err, 'mask upload failed')
    }
  }

  function clearI2i() {
    i2i.clear()
    maskId.value = ''
  }

  const ASPECTS: Record<string, [number, number]> = {
    portrait: [832, 1216],
    landscape: [1216, 832],
    square: [1024, 1024],
  }
  const dims = computed(() => ASPECTS[aspect.value]!)

  // ── live translation (the signature strip) ────────────────────────
  const translate = useTranslate('all')
  watch(prompt, (t) => {
    if (mode.value === 'plain') translate.onInput(t, 'plain')
  })
  watch([tagList, mode], () => {
    if (mode.value === 'tags') translate.onInput(tagList.value.join(', '), 'tags')
    else translate.onInput(prompt.value, 'plain')
  })

  // quality tags are applied server-side; the strip previews them so what
  // you see is what gets sent (values mirror the Illustrious lineage list)
  const QUALITY: TranslatedTag[] = [
    { canonical: 'masterpiece', category: 'meta', count: 1300000, concept: '', stage: 'exact' },
    { canonical: 'best_quality', category: 'meta', count: 1100000, concept: '', stage: 'exact' },
    { canonical: 'very_aesthetic', category: 'meta', count: 640000, concept: '', stage: 'exact' },
    { canonical: 'absurdres', category: 'meta', count: 2100000, concept: '', stage: 'exact' },
  ]
  const stripTags = computed(() => {
    const base = translate.tags.value
    if (!base.length) return base

    return qualityTags.value ? [...QUALITY, ...base] : base
  })

  // ── recent strip (mirrors the non-pose history) ───────────────────
  const recent = useRecentHistory(
    j => !!j.image_url && j.kind !== 'pose',
    j => ({
      id: j.id,
      imageUrl: api.imageUrl(j.image_url),
      title: `${j.params.width} × ${j.params.height} · seed ${j.params.seed}`,
    }),
  )

  // ── generation ────────────────────────────────────────────────────
  const job = useJob(recent.add)
  // pre-created pool so batch > 1 fires parallel jobs (core queues them)
  const extraJobs = [useJob(recent.add), useJob(recent.add), useJob(recent.add)]

  const activeChars = computed(() =>
    chars.value.filter(c => c.enabled && c.text.trim()))

  const promptText = computed(() => {
    const base = mode.value === 'plain'
      ? translate.tags.value.map(t => t.canonical)
      : tagList.value
    // 2-3 characters go regional (each prompt conditions its own column);
    // a single character just merges into the prompt
    const merge = activeChars.value.length >= 2 && activeChars.value.length <= 3
      ? []
      : activeChars.value.map(c => c.text.trim())
    return [...base, ...merge].join(', ')
  })

  const cost = computed(() =>
    batch.value * (17 + (sizeTier.value === 'ultra' ? 16 : hires.value ? 8 : 0)))

  function generate() {
    if (job.busy.value || !promptText.value) return

    const [width, height] = dims.value
    const req: GenerationRequest = {
      prompt: promptText.value,
      negative: und.value.trim() || undefined,
      width,
      height,
      steps: steps.value,
      cfg: guidance.value,
      sampler: sampler.value,
      scheduler: scheduler.value,
      quality_tags: qualityTags.value,
      hires: hires.value || undefined,
      hires_scale: hires.value ? hiresScale.value : undefined,
    }
    if (loraName.value) {
      req.loras = [{ name: loraName.value, strength: loraStrength.value }]
      if (lora2Name.value && lora2Name.value !== loraName.value) {
        req.loras.push({ name: lora2Name.value, strength: lora2Strength.value })
      }
    }

    if (activeCheckpoint.value) req.checkpoint = activeCheckpoint.value

    req.lineage = lineage.value
    if (lineage.value === 'noobai') req.cfg_rescale = cfgRescale.value

    if (i2i.id.value) {
      req.kind = maskId.value ? 'inpaint' : 'img2img'
      req.image_id = i2i.id.value
      if (maskId.value) req.mask_id = maskId.value

      req.denoise = i2iStrength.value
    }

    if (charRef.id.value) {
      req.reference_id = charRef.id.value
      req.ref_mode = charRefMode.value
      req.ref_strength = charRefStrength.value
      req.ref_fidelity = charRefFidelity.value
    }

    if (activeChars.value.length >= 2 && activeChars.value.length <= 3) {
      // per-character references apply only when exactly two characters both
      // have one (and no global reference is set) — core enforces the same
      const cs = activeChars.value
      const perCharRefs = cs.length === 2 && cs.every(c => c.refId) && !charRef.id.value
      req.characters = cs.map(c => perCharRefs
        ? { prompt: c.text.trim(), reference_id: c.refId, ref_strength: charRefFidelity.value }
        : { prompt: c.text.trim() })
    }

    const s = seedText.value.trim()
    if (/^\d+$/.test(s)) req.seed = Number(s)

    job.start(req)
    for (let i = 1; i < batch.value; i++) extraJobs[i - 1]!.start(req)
  }

  function openRecent(item: { id: string, imageUrl: string, title: string }) {
    const seed = item.title.split('seed ')[1] ?? ''
    job.showResult(item.imageUrl, seed)
  }

  onMounted(async () => {
    try {
      const lr = await api.get<{ loras: string[] }>('/loras')
      loras.value = lr.loras
      const ck = await api.get<{ checkpoints: string[], active: string }>('/checkpoints')
      checkpoints.value = ck.checkpoints
      // default lineage is NoobAI (v-pred); pair it with a matching checkpoint so
      // the first generation doesn't hit core's lineage/checkpoint mismatch guard
      activeCheckpoint.value = pickCheckpoint(lineage.value, ck.checkpoints, ck.active)
    }
    catch { /* no loras endpoint or none installed */ }
  })

  // ── configurations (presets) ──────────────────────────────────────
  // The whole left panel is captured to a preset: primitive settings go to
  // localStorage (usePresets), image bytes to IndexedDB (useBlobStore). Core
  // blob ids are ephemeral, so images are re-uploaded on apply for fresh ids.
  const blobs = useBlobStore()
  const { presets, save: savePreset, remove: removePreset } = usePresets()
  const { captureImage, restoreImage } = usePresetImages()
  const coreImg = (id: string) => (id ? api.imageUrl(`/images/${id}`) : '')

  async function buildConfig(): Promise<PresetConfig> {
    const [charRefImg, i2iImg, maskImg] = await Promise.all([
      captureImage(charRef.id.value ? (charRef.preview.value || coreImg(charRef.id.value)) : '', 'reference.png'),
      captureImage(i2i.file.value || coreImg(i2i.id.value), i2i.name.value || 'input.png', i2i.width.value, i2i.height.value),
      captureImage(coreImg(maskId.value), 'mask.png'),
    ])
    const charRefs = await Promise.all(chars.value.map(c => captureImage(coreImg(c.refId), 'char-ref.png')))
    return {
      prompt: prompt.value,
      mode: mode.value,
      tagList: [...tagList.value],
      und: und.value,
      chars: chars.value.map((c, i) => ({ text: c.text, enabled: c.enabled, ref: charRefs[i] ?? null })),
      aspect: aspect.value,
      batch: batch.value,
      sizeTier: sizeTier.value,
      lineage: lineage.value,
      activeCheckpoint: activeCheckpoint.value,
      steps: steps.value,
      guidance: guidance.value,
      cfgRescale: cfgRescale.value,
      seedText: seedText.value,
      sampler: sampler.value,
      scheduler: scheduler.value,
      qualityTags: qualityTags.value,
      faceDetail: faceDetail.value,
      loraName: loraName.value,
      loraStrength: loraStrength.value,
      lora2Name: lora2Name.value,
      lora2Strength: lora2Strength.value,
      charRef: charRefImg,
      charRefMode: charRefMode.value,
      charRefStrength: charRefStrength.value,
      charRefFidelity: charRefFidelity.value,
      poseRef: poseRef.value ? { ...poseRef.value } : null,
      refStrength: refStrength.value,
      i2i: i2iImg,
      i2iStrength: i2iStrength.value,
      mask: maskImg,
    }
  }

  async function applyConfig(cfg: PresetConfig) {
    // lineage first: its watcher resets sampler/guidance/steps/rescale, so let it
    // flush (nextTick) before writing the preset's own values, or they're clobbered.
    lineage.value = cfg.lineage ?? 'illustrious'
    await nextTick()

    prompt.value = cfg.prompt ?? ''
    mode.value = cfg.mode ?? 'plain'
    tagList.value = [...(cfg.tagList ?? [])]
    und.value = cfg.und ?? ''
    aspect.value = cfg.aspect ?? 'portrait'
    batch.value = cfg.batch ?? 1
    sizeTier.value = cfg.sizeTier ?? 'normal'
    // empty = no explicit checkpoint; keep the lineage watcher's auto-picked match
    if (cfg.activeCheckpoint) activeCheckpoint.value = cfg.activeCheckpoint

    steps.value = cfg.steps ?? steps.value
    guidance.value = cfg.guidance ?? guidance.value
    cfgRescale.value = cfg.cfgRescale ?? cfgRescale.value
    seedText.value = cfg.seedText ?? ''
    sampler.value = cfg.sampler ?? sampler.value
    scheduler.value = cfg.scheduler ?? scheduler.value
    qualityTags.value = cfg.qualityTags ?? true
    faceDetail.value = cfg.faceDetail ?? true
    loraName.value = cfg.loraName ?? ''
    loraStrength.value = cfg.loraStrength ?? 0.8
    lora2Name.value = cfg.lora2Name ?? ''
    lora2Strength.value = cfg.lora2Strength ?? 0.65
    charRefMode.value = cfg.charRefMode ?? 'character_style'
    charRefStrength.value = cfg.charRefStrength ?? 0.8
    charRefFidelity.value = cfg.charRefFidelity ?? 0.5
    poseRef.value = cfg.poseRef ? { ...cfg.poseRef } : null
    refStrength.value = cfg.refStrength ?? 0.6
    i2iStrength.value = cfg.i2iStrength ?? 0.6

    const charRefUp = await restoreImage(cfg.charRef)
    charRef.id.value = charRefUp?.id ?? ''
    charRef.preview.value = charRefUp?.preview ?? ''

    const i2iUp = await restoreImage(cfg.i2i)
    if (i2iUp) {
      i2i.file.value = i2iUp.file
      i2i.name.value = cfg.i2i?.name ?? 'input.png'
      i2i.id.value = i2iUp.id
      i2i.preview.value = i2iUp.preview
      i2i.width.value = i2iUp.width
      i2i.height.value = i2iUp.height
      maskId.value = (await restoreImage(cfg.mask))?.id ?? ''
    }
    else {
      clearI2i()
    }

    const restoredChars = await Promise.all((cfg.chars ?? []).map(async c => ({
      text: c.text,
      enabled: c.enabled,
      refId: (await restoreImage(c.ref))?.id ?? '',
    })))
    chars.value = restoredChars

    // reveal folds that now hold content
    folds.und = !!und.value.trim()
    folds.chars = restoredChars.length > 0
    folds.ref = !!charRef.id.value || !!poseRef.value
    folds.i2i = !!i2i.id.value
  }

  async function onSaveName(name: string) {
    savePreset(name, await buildConfig())
  }

  // rows for the shared drawer: it only shows name + subtitle, ids come back
  function presetSubtitle(p: Preset): string {
    const c = p.config
    return [
      c.lineage === 'noobai' ? 'v-pred' : 'eps',
      c.aspect,
      c.sizeTier !== 'normal' ? c.sizeTier : '',
      c.i2i ? 'img2img' : '',
      c.charRef ? 'reference' : '',
      c.loraName ? 'lora' : '',
    ].filter(Boolean).join(' · ')
  }
  const drawerPresets = computed(() =>
    presets.value.map(p => ({ id: p.id, name: p.name, subtitle: presetSubtitle(p) })))

  async function onUsePreset(id: string) {
    const p = presets.value.find(x => x.id === id)
    if (p) await applyConfig(p.config)
  }
  function onDeletePresets(ids: string[]) {
    const set = new Set(ids)
    const keys = presets.value.filter(p => set.has(p.id)).flatMap(p => blobKeys(p.config))
    blobs.remove(keys)
    removePreset(ids)
  }

  // "Use related config" from the history modal hands a config off here; apply it
  // then clear the channel. immediate covers a cold navigation into /create; the
  // watch covers the keep-alive case where this page is already mounted.
  const pendingConfig = usePendingConfig()
  watch(pendingConfig, async (c) => {
    if (!c) return

    pendingConfig.value = null
    await applyConfig(c)
  }, { immediate: true })

  // ── fold badges ───────────────────────────────────────────────────
  const charBadge = computed(() =>
    chars.value.length ? `${chars.value.filter(c => c.enabled).length}/${chars.value.length} on` : 'none')

  return {
    // describe
    prompt, mode, tagList, und, chars, translate, stripTags, charBadge,
    // image
    aspect, batch, sizeTier, dims,
    // advanced
    lineage, cfgRescale, checkpoints, activeCheckpoint, steps, guidance,
    seedText, sampler, scheduler, qualityTags, faceDetail,
    loras, loraName, loraStrength, lora2Name, lora2Strength,
    // reference + pose handoff
    charRef, charRefMode, charRefStrength, charRefFidelity, poseRef, refStrength,
    // img2img / inpaint
    i2i, i2iStrength, maskId, maskEditing, onI2iPick, onMaskSave, clearI2i,
    // folds
    folds,
    // generation + recent
    job, generate, cost, recent, openRecent,
    // presets
    presets, drawerPresets, onSaveName, onUsePreset, onDeletePresets,
  }
}
