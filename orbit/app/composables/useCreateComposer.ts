import type { GenerationRequest, PromptIssue, StylePreset, TranslatedTag } from '~/types/api'
import { blobKeys, usePendingConfig, usePendingImport, type Preset, type PresetConfig } from '~/composables/usePresets'
import { applyMetaImport, type MetaImport } from '~/utils/meta-import'
import { chipsWithIssues } from '~/utils/prompt-issues'

// The whole /create composer: state, lineage rules, uploads, generation, and
// preset capture/apply. The page owns only template wiring and modal open
// flags. Called once from the kept-alive page, so all of this survives route
// switches and dies on reload — exactly the previous in-page behaviour.
export function useCreateComposer() {
  const api = useApi()
  const chunkStore = useChunks()
  const toast = useToast()

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

  // ── style presets ─────────────────────────────────────────────────
  // Server-side recipes (GET /styles): prompt carriers, finish tags, negative
  // and LoRA merge in core. The sampler profile is written into the visible
  // fields here so what the user sees is what runs — core lets request values
  // win, so the fields stay editable after a style is picked.
  const styles = ref<StylePreset[]>([])
  const styleSlug = ref('')
  const stylesForLineage = computed(() => styles.value.filter(s => s.lineage === lineage.value))
  const activeStyle = computed(() => stylesForLineage.value.find(s => s.slug === styleSlug.value))
  // a style's LoRA takes the second LoRA slot in core (two-LoRA cap)
  const styleHasLora = computed(() => !!activeStyle.value?.lora)

  function applyStyleSettings(s: StylePreset | undefined) {
    const d = LINEAGE_DEFAULTS[lineage.value]
    steps.value = s?.steps ?? d.steps
    guidance.value = s?.cfg ?? d.guidance
    sampler.value = s?.sampler ?? d.sampler
    scheduler.value = s?.scheduler ?? d.scheduler
    cfgRescale.value = s?.cfg_rescale ?? d.rescale
    if (s?.hires != null) sizeTier.value = s.hires ? ((s.hires_scale ?? 1.5) >= 2 ? 'ultra' : 'large') : 'normal'
    else if (!s) sizeTier.value = 'normal'
  }
  watch(styleSlug, (slug, prev) => {
    // '' → '' happens when a preset without a style loads; nothing to reset
    if (!slug && !prev) return

    applyStyleSettings(activeStyle.value)
  })

  watch(lineage, (l) => {
    const d = LINEAGE_DEFAULTS[l]
    steps.value = d.steps
    guidance.value = d.guidance
    sampler.value = d.sampler
    scheduler.value = d.scheduler
    cfgRescale.value = d.rescale
    activeCheckpoint.value = pickCheckpoint(l, checkpoints.value, activeCheckpoint.value)
    // a style is a per-lineage recipe: keep the slug if the new lineage has a
    // row for it (and re-apply that row over the reset), otherwise drop it
    if (styleSlug.value && !stylesForLineage.value.some(s => s.slug === styleSlug.value)) styleSlug.value = ''
    else if (styleSlug.value) applyStyleSettings(activeStyle.value)
  })

  const loraName = ref('')
  const loraStrength = ref(0.8)
  // second slot chains behind the first (character identity + franchise style)
  const lora2Name = ref('')
  const lora2Strength = ref(0.65)

  const folds = reactive({ style: false, und: false, chars: false, ref: false, i2i: false, adv: false })

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
  // editing a chunk the prose references re-translates it (core expands
  // chunks for /translate; their text never reaches the LLM)
  watch(() => JSON.stringify(chunkStore.referenced([prompt.value])), () => {
    if (mode.value === 'plain' && prompt.value.trim()) translate.onInput(prompt.value, 'plain')
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
    // plain mode sends core's NovelAI-syntax rendering of the grounded tags
    // (weights kept); older cores without it fall back to bare names
    const base = mode.value === 'plain'
      ? (translate.nai.value ? [translate.nai.value] : translate.tags.value.map(t => t.canonical))
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

  // ── prompt validation (docs/frontend.md#prompt-validation) ──────────
  // Live: debounced POST /prompt/parse per surface. Server: /generate's
  // located 400, shown while the field's text is what was sent.
  const promptSurfaceText = computed(() => (mode.value === 'tags' ? tagList.value.join(', ') : prompt.value))
  const promptLive = usePromptParse(() => promptSurfaceText.value, 'prompt', () => lineage.value)
  const undLive = usePromptParse(() => und.value, 'negative', () => lineage.value)

  // what the last /generate sent, to map its 400 back onto a field
  const sent = ref<{ mode: 'plain' | 'tags', chips: string, und: string, regional: { idx: number, text: string }[], merged: { idx: number, text: string } | null } | null>(null)

  const serverIssues = computed(() => {
    const out = { prompt: [] as PromptIssue[], promptMarks: [] as PromptIssue[], negative: [] as PromptIssue[], chars: {} as Record<number, PromptIssue[]> }
    const iss = job.issue.value
    const s = sent.value
    if (!iss || !s) return out

    if (iss.field === 'negative' && und.value === s.und) out.negative = [iss]
    else if (iss.field.startsWith('characters.')) {
      const r = s.regional[Number(iss.field.slice('characters.'.length))]
      if (r && chars.value[r.idx]?.text.trim() === r.text) out.chars[r.idx] = [iss]
    }
    else if (iss.field === 'prompt') {
      if (s.mode === 'plain') {
        // offsets index core's rendering, not the prose: message only
        if (prompt.value && mode.value === 'plain') out.prompt = [iss]
      }
      else if (s.merged && (s.chips === '' || iss.start >= s.chips.length + 2)) {
        // a single character merges into the prompt: shift into its card
        const shift = s.chips.length ? s.chips.length + 2 : 0
        if (chars.value[s.merged.idx]?.text.trim() === s.merged.text) {
          out.chars[s.merged.idx] = [{ ...iss, start: iss.start - shift, end: iss.end - shift }]
        }
      }
      else if (tagList.value.join(', ') === s.chips && mode.value === 'tags') {
        out.prompt = [iss]
        out.promptMarks = [iss]
      }
    }

    return out
  })

  const promptErrors = computed(() => {
    if (promptLive.errors.value.length) return promptLive.errors.value

    if (mode.value === 'plain' && translate.issue.value) return [translate.issue.value]

    return serverIssues.value.prompt
  })
  const promptMarks = computed(() => {
    if (promptLive.errors.value.length) return promptLive.errors.value

    if (mode.value === 'plain' && translate.issue.value) return [translate.issue.value]

    return serverIssues.value.promptMarks
  })
  const promptNotices = computed(() => promptLive.notices.value)
  const invalidChips = computed(() => [...chipsWithIssues(tagList.value, promptMarks.value)])
  const undErrors = computed(() => (undLive.errors.value.length ? undLive.errors.value : serverIssues.value.negative))
  const undNotices = computed(() => undLive.notices.value)
  const charServerErrors = (i: number) => serverIssues.value.chars[i] ?? []
  // per-card validity reported by CharacterCard (its live parse lives there)
  const charInvalid = reactive<Record<number, boolean>>({})
  const charsInvalid = computed(() =>
    chars.value.some((_, i) => charInvalid[i]) || Object.keys(serverIssues.value.chars).length > 0)

  // a 400 inside a folded section opens it, so the mark is visible
  watch(() => job.issue.value, (iss) => {
    if (iss?.field === 'negative') folds.und = true

    if (iss?.field.startsWith('characters.') || (iss?.field === 'prompt' && sent.value?.merged)) folds.chars = true
  })

  function generate() {
    if (job.busy.value || !promptText.value) return

    const regionalOn = activeChars.value.length >= 2 && activeChars.value.length <= 3
    const active = chars.value
      .map((c, idx) => ({ idx, text: c.text.trim(), on: c.enabled && !!c.text.trim() }))
      .filter(c => c.on)
    sent.value = {
      mode: mode.value,
      chips: tagList.value.join(', '),
      und: und.value,
      regional: regionalOn ? active.map(({ idx, text }) => ({ idx, text })) : [],
      merged: !regionalOn && active.length === 1 ? { idx: active[0]!.idx, text: active[0]!.text } : null,
    }

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

    if (styleSlug.value) req.style = styleSlug.value

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

    // only the chunks this request references; core expands them and stores
    // the expanded tags with their chunk provenance
    const refs = chunkStore.referenced([req.prompt, req.negative ?? '', ...(req.characters ?? []).map(c => c.prompt)])
    if (refs.length) req.chunks = refs

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
      const st = await api.get<{ styles: StylePreset[] }>('/styles')
      styles.value = st.styles
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
      style: styleSlug.value,
      charRef: charRefImg,
      charRefMode: charRefMode.value,
      charRefStrength: charRefStrength.value,
      charRefFidelity: charRefFidelity.value,
      poseRef: poseRef.value ? { ...poseRef.value } : null,
      refStrength: refStrength.value,
      i2i: i2iImg,
      i2iStrength: i2iStrength.value,
      mask: maskImg,
      chunks: chunkStore.snapshot([prompt.value, tagList.value.join(', '), und.value, ...chars.value.map(c => c.text)]),
    }
  }

  async function applyConfig(cfg: PresetConfig) {
    // lineage first: its watcher resets sampler/guidance/steps/rescale, so let it
    // flush (nextTick) before writing the preset's own values, or they're clobbered.
    // chunks first: the texts below may reference ones this browser lacks
    const restored = chunkStore.restoreSnapshot(cfg.chunks)
    if (restored.created.length) toast.show(`Restored prompt chunk${restored.created.length > 1 ? 's' : ''}: ${restored.created.join(', ')}`)

    if (restored.mismatched.length) {
      toast.show(`Your chunk${restored.mismatched.length > 1 ? 's' : ''} ${restored.mismatched.join(', ')} changed since this was saved — the current version is used`, 'warn', 7000)
    }

    lineage.value = cfg.lineage ?? 'illustrious'
    await nextTick()
    // style second: its watcher writes the recipe's sampler profile, which the
    // preset's own values below must override
    styleSlug.value = cfg.style ?? ''
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
      c.style ? `style · ${c.style}` : '',
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

  // ── import from an inspected image (docs/frontend.md#image-inspector) ──
  // A partial patch: only what the dialog's toggles asked for is written,
  // and never guidance, sampler, schedule or rescale. The text lands in the
  // normal editor fields, so it is validated live and checked at /generate
  // like anything typed. Images go through the one upload path.
  async function applyImport(p: MetaImport) {
    applyMetaImport({ mode, tagList, und, chars, aspect, steps, seedText }, p)

    if (p.und !== undefined) folds.und = !!und.value.trim()

    if (p.chars !== undefined) folds.chars = chars.value.length > 0

    if (p.steps !== undefined || p.seedText !== undefined) folds.adv = true

    if (p.baseImage) {
      maskId.value = ''
      folds.i2i = true
      await i2i.upload(p.baseImage)
    }

    if (p.reference) {
      folds.ref = true
      await charRef.upload(p.reference)
    }
  }

  const pendingImport = usePendingImport()
  watch(pendingImport, async (p) => {
    if (!p) return

    pendingImport.value = null
    await applyImport(p)
  }, { immediate: true })

  // ── prompt chunks: the live fields a rename or a bake rewrites ─────
  // registered here, not in the inputs, so a field that isn't on screen (the
  // prose box while in tags mode, a folded character) is rewritten too
  const unregisterFields = chunkStore.registerField(async (edit) => {
    prompt.value = await edit(prompt.value, 'prompt')
    const joined = tagList.value.join(', ')
    const next = await edit(joined, 'prompt')
    if (next !== joined) tagList.value = [...new Set(next.split(/[,\n]/).map(t => t.trim()).filter(Boolean))]

    und.value = await edit(und.value, 'negative')
    for (const c of chars.value) c.text = await edit(c.text, 'character')
  })
  onScopeDispose(unregisterFields)

  // ── fold badges ───────────────────────────────────────────────────
  const charBadge = computed(() =>
    chars.value.length ? `${chars.value.filter(c => c.enabled).length}/${chars.value.length} on` : 'none')

  return {
    // describe
    prompt, mode, tagList, und, chars, translate, stripTags, charBadge,
    // prompt validation
    promptErrors, promptMarks, promptNotices, invalidChips, undErrors, undNotices, charServerErrors, charInvalid, charsInvalid,
    // chunk references + emphasis marks core located in the live text
    promptLive, undLive,
    // image
    aspect, batch, sizeTier, dims,
    // advanced
    lineage, cfgRescale, checkpoints, activeCheckpoint, steps, guidance,
    seedText, sampler, scheduler, qualityTags, faceDetail,
    loras, loraName, loraStrength, lora2Name, lora2Strength,
    // style presets
    stylesForLineage, styleSlug, activeStyle, styleHasLora,
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
