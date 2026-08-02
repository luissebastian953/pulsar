<script setup lang="ts">
import type { GenerationRequest, JobPayload, UploadResult, TranslatedTag } from '~/types/api'
import type { RecentItem } from '~/components/RecentStrip.vue'
import { blobKeys, usePendingConfig, type Preset, type PresetConfig, type PresetImage } from '~/composables/usePresets'

// Keep the whole composer alive across navigation to /pose and back (SPA-only,
// ssr:false, so KeepAlive's client-only nature is a non-issue). Route switches
// preserve state; a full reload still resets — that's what saved configs are for.
definePageMeta({ keepalive: true })

const api = useApi()
const { public: { modelName } } = useRuntimeConfig()

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

// Precise Reference: character identity from an uploaded image
const charRefId = ref('')
const charRefPreview = ref('')
const charRefMode = ref<'character_style' | 'character' | 'style'>('character_style')
const charRefStrength = ref(0.8)
const charRefFidelity = ref(0.5)
const charRefError = ref('')

async function onCharRefPick(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  charRefError.value = ''
  try {
    const up = await api.upload<UploadResult>('/uploads', file)
    charRefId.value = up.image_id
    charRefPreview.value = api.imageUrl(`/images/${up.image_id}`)
  }
  catch (err: unknown) {
    const ex = err as { data?: { error?: string } }
    charRefError.value = ex.data?.error || 'upload failed'
  }
}

// image-to-image / inpaint: eager upload on pick so the mask editor can
// paint on the normalized image (dims always match the bucket)
const i2iFile = ref<File | null>(null)
const i2iName = ref('')
const i2iPreview = ref('')
const i2iId = ref('')
const i2iDims = ref<[number, number]>([0, 0])
const i2iStrength = ref(0.6)
const i2iError = ref('')
const maskId = ref('')
const maskEditing = ref(false)

async function onI2iPick(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  i2iError.value = ''
  maskId.value = ''
  try {
    const up = await api.upload<UploadResult>('/uploads', file)
    i2iFile.value = file
    i2iName.value = file.name
    i2iId.value = up.image_id
    i2iDims.value = [up.width, up.height]
    i2iPreview.value = api.imageUrl(`/images/${up.image_id}`)
  }
  catch (err: unknown) {
    const ex = err as { data?: { error?: string } }
    i2iError.value = ex.data?.error || 'upload failed'
  }
}

async function onMaskSave(file: File) {
  maskEditing.value = false
  try {
    const up = await api.upload<UploadResult>('/uploads', file)
    maskId.value = up.image_id
  }
  catch (err: unknown) {
    const ex = err as { data?: { error?: string } }
    i2iError.value = ex.data?.error || 'mask upload failed'
  }
}

function clearI2i() {
  i2iFile.value = null
  i2iName.value = ''
  i2iPreview.value = ''
  i2iId.value = ''
  maskId.value = ''
  i2iError.value = ''
}
if (openRefFold.value) {
  folds.ref = true
  openRefFold.value = false
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

// ── generation ────────────────────────────────────────────────────
const recent = ref<RecentItem[]>([])

function addRecent(j: JobPayload) {
  if (!j.image_url) return
  recent.value = [{
    id: j.id,
    imageUrl: api.imageUrl(j.image_url),
    title: `${j.params.width} × ${j.params.height} · seed ${j.params.seed}`,
  }, ...recent.value].slice(0, 12)
}

const job = useJob(addRecent)
// pre-created pool so batch > 1 fires parallel jobs (core queues them)
const extraJobs = [useJob(addRecent), useJob(addRecent), useJob(addRecent)]

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
  if (i2iId.value) {
    req.kind = maskId.value ? 'inpaint' : 'img2img'
    req.image_id = i2iId.value
    if (maskId.value) req.mask_id = maskId.value
    req.denoise = i2iStrength.value
  }
  if (charRefId.value) {
    req.reference_id = charRefId.value
    req.ref_mode = charRefMode.value
    req.ref_strength = charRefStrength.value
    req.ref_fidelity = charRefFidelity.value
  }
  if (activeChars.value.length >= 2 && activeChars.value.length <= 3) {
    // per-character references apply only when exactly two characters both
    // have one (and no global reference is set) — core enforces the same
    const cs = activeChars.value
    const perCharRefs = cs.length === 2 && cs.every(c => c.refId) && !charRefId.value
    req.characters = cs.map(c => perCharRefs
      ? { prompt: c.text.trim(), reference_id: c.refId, ref_strength: charRefFidelity.value }
      : { prompt: c.text.trim() })
  }
  const s = seedText.value.trim()
  if (/^\d+$/.test(s)) req.seed = Number(s)
  job.start(req)
  for (let i = 1; i < batch.value; i++) extraJobs[i - 1]!.start(req)
}

function openRecent(item: RecentItem) {
  const seed = item.title.split('seed ')[1] ?? ''
  job.showResult(item.imageUrl, seed)
}

// Recent mirrors the Create history (12 newest non-pose done images). Reloaded
// on every activation, not just first mount, so it stays in sync after history
// deletions — the page is kept-alive, so a mount-only snapshot would keep
// showing thumbnails whose blobs were deleted elsewhere.
async function loadRecent() {
  try {
    const res = await api.get<{ jobs: JobPayload[] }>('/history')
    recent.value = res.jobs
      .filter(j => j.state === 'done' && j.image_url && j.kind !== 'pose')
      .slice(0, 12)
      .map(j => ({
        id: j.id,
        imageUrl: api.imageUrl(j.image_url),
        title: `${j.params.width} × ${j.params.height} · seed ${j.params.seed}`,
      }))
  }
  catch { /* core offline — leave the strip as-is */ }
}

// drop a thumbnail whose image 404s (e.g. its blob was deleted), so a broken
// tile never lingers in the strip
function onRecentError(id: string) {
  recent.value = recent.value.filter(r => r.id !== id)
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

// fires on first mount and on every re-activation of the kept-alive page
onActivated(loadRecent)

// ── configurations (presets) ──────────────────────────────────────
// The whole left panel is captured to a preset: primitive settings go to
// localStorage (usePresets), image bytes to IndexedDB (useBlobStore). Core
// blob ids are ephemeral, so images are re-uploaded on apply for fresh ids.
const blobs = useBlobStore()
const { presets, save: savePreset, remove: removePreset } = usePresets()
const drawerOpen = ref(false)
const saveOpen = ref(false)

function authHeaders(): Record<string, string> {
  return api.token.value ? { Authorization: `Bearer ${api.token.value}` } : {}
}

// Capture one image slot into IndexedDB. `src` is a freshly-picked File or a
// core image URL (already uploaded / being re-saved). Returns null on empty or
// any failure — a missing image must never block saving the rest of the config.
async function captureImage(src: File | string, name: string, w = 0, h = 0): Promise<PresetImage | null> {
  if (!src) return null
  try {
    const blob = typeof src === 'string'
      ? await (await fetch(src, { headers: authHeaders() })).blob()
      : src
    const key = await blobs.put(blob)
    return { blob: key, name, type: blob.type || 'image/png', width: w, height: h }
  }
  catch { return null }
}

async function buildConfig(): Promise<PresetConfig> {
  const coreImg = (id: string) => (id ? api.imageUrl(`/images/${id}`) : '')
  const [charRef, i2i, mask] = await Promise.all([
    captureImage(charRefId.value ? (charRefPreview.value || coreImg(charRefId.value)) : '', 'reference.png'),
    captureImage(i2iFile.value || coreImg(i2iId.value), i2iName.value || 'input.png', i2iDims.value[0], i2iDims.value[1]),
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
    charRef,
    charRefMode: charRefMode.value,
    charRefStrength: charRefStrength.value,
    charRefFidelity: charRefFidelity.value,
    poseRef: poseRef.value ? { ...poseRef.value } : null,
    refStrength: refStrength.value,
    i2i,
    i2iStrength: i2iStrength.value,
    mask,
  }
}

// Re-upload a stored blob to core, yielding a fresh valid id + preview.
async function restoreImage(img: PresetImage | null) {
  if (!img?.blob) return null
  try {
    const blob = await blobs.get(img.blob)
    if (!blob) return null
    const file = new File([blob], img.name || 'image.png', { type: img.type || blob.type || 'image/png' })
    const up = await api.upload<UploadResult>('/uploads', file)
    return { id: up.image_id, preview: api.imageUrl(`/images/${up.image_id}`), width: up.width, height: up.height, file }
  }
  catch { return null }
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
  charRefId.value = charRefUp?.id ?? ''
  charRefPreview.value = charRefUp?.preview ?? ''

  const i2iUp = await restoreImage(cfg.i2i)
  if (i2iUp) {
    i2iFile.value = i2iUp.file
    i2iName.value = cfg.i2i?.name ?? 'input.png'
    i2iId.value = i2iUp.id
    i2iPreview.value = i2iUp.preview
    i2iDims.value = [i2iUp.width, i2iUp.height]
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
  folds.ref = !!charRefId.value || !!poseRef.value
  folds.i2i = !!i2iId.value
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
</script>

<template>
  <main class="flex min-h-0 flex-1 flex-col overflow-y-auto md:flex-row md:overflow-hidden">
    <!-- composer rail -->
    <aside class="order-3 flex flex-none flex-col bg-surface md:order-1 md:h-full md:w-[372px] md:min-h-0 md:border-r md:border-line">
      <div class="min-h-0 p-4 md:flex-1 md:overflow-y-auto">
        <SectionLabel class="mb-2.5">
          Describe
        </SectionLabel>
        <textarea
          v-if="mode === 'plain'"
          v-model="prompt"
          placeholder="a silver-haired girl in a school uniform, looking back at sunset…"
          class="min-h-[92px] w-full resize-y rounded-ctrl border border-line bg-void px-3 py-2.5 text-sm leading-relaxed text-text outline-none placeholder:text-faint focus:border-line-hi"
        />
        <TagInput v-else v-model="tagList" />
        <Segment
          v-model="mode"
          class="mt-2"
          :options="[{ value: 'plain', label: 'plain language' }, { value: 'tags', label: 'tags' }]"
        />

        <ResolvedStrip
          class="mt-3"
          :tags="stripTags"
          :dropped="translate.dropped.value"
          :resolving="translate.resolving.value"
          :offline="translate.offline.value"
          :failed="translate.failed.value"
          :stat="translate.stat.value"
        />

        <Fold v-model="folds.und" label="Undesired content" :badge="und.trim() ? 'custom' : 'none'" class="mt-3.5">
          <textarea
            v-model="und"
            placeholder="lowres, extra fingers, watermark…"
            class="min-h-[60px] w-full resize-y rounded-ctrl border border-line bg-void px-[11px] py-[9px] text-[12.5px] leading-relaxed text-text outline-none placeholder:text-faint focus:border-line-hi"
          />
          <div class="mt-1.5 text-[11.5px] leading-relaxed text-faint">
            The model's default negative is merged in automatically.
          </div>
        </Fold>

        <Fold v-model="folds.chars" label="Characters" :badge="charBadge">
          <div class="flex flex-col gap-2">
            <CharacterCard
              v-for="(c, i) in chars"
              :key="i"
              v-model:enabled="c.enabled"
              v-model:text="c.text"
              v-model:ref-id="c.refId"
              :index="i"
              @remove="chars.splice(i, 1)"
            />
            <button
              class="focus-ring w-full cursor-pointer rounded-ctrl border border-dashed border-line-hi py-[9px] text-center text-[12.5px] text-dim hover:border-dim hover:text-text"
              @click="chars.push({ text: '', enabled: true, refId: '' })"
            >
              <span class="inline-flex items-center justify-center gap-1.5">
                <Icon name="lucide:plus" :size="13" />Add character
              </span>
            </button>
            <div class="text-[11.5px] leading-relaxed text-faint">
              Each character gets its own prompt, undesired content, and
              position. Regions keep their attributes from bleeding into
              each other.
            </div>
          </div>
        </Fold>

        <Fold v-model="folds.ref" label="Reference" :badge="charRefId ? 'character' : (poseRef ? 'pose skeleton' : 'off')">
          <div class="flex flex-col gap-2">
            <template v-if="poseRef">
              <div class="flex items-center gap-2.5 rounded-ctrl border border-line bg-raised px-[11px] py-[9px]">
                <span class="flex h-[34px] w-[26px] flex-none items-center justify-center rounded-lg border border-line-hi bg-void font-mono text-[9px] text-faint">╱</span>
                <div class="flex-1">
                  <div class="text-[12.5px] text-text">Pose skeleton attached</div>
                  <div class="font-mono text-[10.5px] text-faint">{{ poseRef.label }}</div>
                </div>
                <button class="focus-ring cursor-pointer text-faint hover:text-t-artist" @click="poseRef = null">
                  <Icon name="lucide:x" :size="12" />
                </button>
              </div>
              <SliderRow v-model="refStrength" label="Strength" :min="0.1" :max="1.5" :step="0.05" :decimals="2" />
              <div class="text-[11.5px] leading-relaxed text-faint">
                Above 1.0 the reference gets hard to override with words.
              </div>
            </template>
            <template v-if="charRefId">
              <div class="flex items-center gap-2.5 rounded-ctrl border border-line bg-void p-2">
                <img :src="charRefPreview" alt="reference" class="size-14 flex-none rounded-lg object-cover">
                <div class="min-w-0 flex-1">
                  <div class="text-[12.5px] text-text">Character reference</div>
                  <div class="font-mono text-[10.5px] text-faint">identity via IP-Adapter Plus</div>
                </div>
                <button class="focus-ring cursor-pointer text-faint hover:text-t-artist" aria-label="remove reference" @click="charRefId = ''; charRefPreview = ''">
                  <Icon name="lucide:x" :size="12" />
                </button>
              </div>
              <Dropdown
                v-model="charRefMode"
                :options="[
                  { value: 'character_style', label: 'Character & Style' },
                  { value: 'character', label: 'Character' },
                  { value: 'style', label: 'Style' },
                ]"
              />
              <SliderRow v-model="charRefStrength" label="Strength" :min="0" :max="1.2" :step="0.05" :decimals="2" />
              <SliderRow v-model="charRefFidelity" label="Fidelity" :min="0" :max="1" :step="0.05" :decimals="2" />
              <div class="text-[11.5px] leading-relaxed text-faint">
                Strength: how hard the reference pushes (~0.5 loose, ~0.8 strong).
                Fidelity: how long it steers — higher follows the reference into fine detail.
              </div>
            </template>
            <label
              v-else
              class="focus-ring flex w-full cursor-pointer items-center justify-center gap-1.5 rounded-ctrl border border-dashed border-line py-[9px] text-center text-[12.5px] text-dim hover:border-line-hi"
            >
              <Icon name="lucide:plus" :size="13" />Character reference
              <input type="file" accept="image/png,image/jpeg" class="hidden" @change="onCharRefPick">
            </label>
            <div v-if="charRefError" class="text-[11px] text-warn">{{ charRefError }}</div>
          </div>
        </Fold>

        <Fold v-model="folds.i2i" label="Image to image" :badge="maskId ? 'inpaint' : (i2iId ? 'on' : 'off')">
          <div class="grid gap-2.5">
            <template v-if="i2iId">
              <div class="flex items-center gap-2.5 rounded-ctrl border border-line bg-void p-2">
                <img :src="i2iPreview" alt="input" class="size-14 flex-none rounded-lg object-cover">
                <div class="min-w-0 flex-1">
                  <div class="truncate text-[12px] text-dim">{{ i2iName }}</div>
                  <div class="font-mono text-[10.5px] text-faint">fitted to the nearest bucket on upload</div>
                </div>
                <button class="focus-ring cursor-pointer text-faint hover:text-t-artist" aria-label="remove image" @click="clearI2i">
                  <Icon name="lucide:x" :size="14" />
                </button>
              </div>
              <div class="flex items-center gap-2">
                <button
                  class="focus-ring flex-1 cursor-pointer rounded-ctrl border border-line px-3 py-1.5 text-[12px] text-dim hover:border-line-hi"
                  @click="maskEditing = true"
                >
                  {{ maskId ? 'Repaint mask' : 'Paint mask (inpaint)' }}
                </button>
                <button
                  v-if="maskId"
                  class="focus-ring cursor-pointer rounded-ctrl border border-line px-3 py-1.5 text-[12px] text-faint hover:text-t-artist"
                  @click="maskId = ''"
                >
                  drop mask
                </button>
              </div>
              <SliderRow v-model="i2iStrength" label="Strength" :min="0.05" :max="0.95" :step="0.05" :decimals="2" />
              <div class="text-[10.5px] leading-relaxed text-faint">
                {{ maskId ? 'only the painted area is repainted' : 'higher = more change; ~0.6 restructures while keeping composition' }}
              </div>
            </template>
            <label
              v-else
              class="focus-ring flex cursor-pointer items-center justify-center gap-2 rounded-ctrl border border-dashed border-line px-3 py-4 text-[12px] text-dim hover:border-line-hi"
            >
              <Icon name="lucide:image-plus" :size="14" />
              choose an image (png / jpeg)
              <input type="file" accept="image/png,image/jpeg" class="hidden" @change="onI2iPick">
            </label>
            <div v-if="i2iError" class="text-[11px] text-warn">{{ i2iError }}</div>
          </div>
        </Fold>
        <MaskEditor
          v-if="maskEditing && i2iId"
          :src="i2iPreview"
          :width="i2iDims[0]"
          :height="i2iDims[1]"
          @save="onMaskSave"
          @close="maskEditing = false"
        />

        <SectionLabel class="mb-2.5 mt-3.5 border-t border-line pt-3">
          Image
        </SectionLabel>
        <Dropdown
          v-model="aspect"
          aria-label="Aspect"
          :options="[
            { value: 'portrait', label: 'Portrait · 832 × 1216' },
            { value: 'landscape', label: 'Landscape · 1216 × 832' },
            { value: 'square', label: 'Square · 1024 × 1024' },
          ]"
        />
        <Segment
          v-model="batch"
          class="mt-2"
          :options="[1, 2, 3, 4].map(n => ({ value: n, label: String(n) }))"
        />

        <Fold v-model="folds.adv" label="Advanced" :badge="lineage === 'noobai' ? 'NoobAI · v-pred' : `tuned for ${modelName}`" class="mt-3.5">
          <div class="flex flex-col gap-3">
            <div>
              <div class="mb-[5px] text-xs text-dim">Model lineage</div>
              <Dropdown
                v-model="lineage"
                aria-label="Model lineage"
                :options="[
                  { value: 'illustrious', label: 'Illustrious · eps' },
                  { value: 'noobai', label: 'NoobAI · v-pred' },
                ]"
              />
              <div class="mt-1 text-[10.5px] leading-relaxed text-faint">
                v-pred (NoobAI) gives a cleaner, less-fried finish. Switching resets
                sampler and guidance — and needs a matching base model below.
              </div>
            </div>
            <div v-if="checkpoints.length > 1">
              <div class="mb-[5px] text-xs text-dim">Base model</div>
              <Dropdown
                v-model="activeCheckpoint"
                aria-label="Base model"
                :options="checkpoints.map(c => ({ value: c, label: c.replace(/\.(safetensors|pt)$/, '') }))"
              />
              <div class="mt-1 text-[10.5px] leading-relaxed text-faint">
                Swapping the base changes the look; same-lineage settings still apply.
              </div>
            </div>
            <SliderRow v-model="steps" label="Steps" :min="1" :max="50" />
            <SliderRow v-model="guidance" label="Prompt guidance" :min="1" :max="12" :step="0.1" :decimals="1" />
            <SliderRow
              v-if="lineage === 'noobai'"
              v-model="cfgRescale"
              label="CFG rescale"
              title="Tempers v-pred colour burn from high guidance; 0 disables it"
              :min="0"
              :max="1"
              :step="0.05"
              :decimals="2"
            />
            <div>
              <div class="mb-[5px] text-xs text-dim">Seed</div>
              <input
                v-model="seedText"
                placeholder="random"
                inputmode="numeric"
                class="w-full rounded-ctrl border border-line bg-void px-2.5 py-2 font-mono text-[12.5px] text-text outline-none placeholder:text-faint focus:border-line-hi"
              >
            </div>
            <div class="flex gap-2">
              <div class="min-w-0 flex-1">
                <div class="mb-[5px] text-xs text-dim">Sampler</div>
                <Dropdown
                  v-model="sampler"
                  aria-label="Sampler"
                  :options="[
                    { value: 'euler_ancestral', label: 'Euler a' },
                    { value: 'euler', label: 'Euler' },
                    { value: 'dpmpp_2m', label: 'DPM++ 2M' },
                  ]"
                />
              </div>
              <div class="min-w-0 flex-1">
                <div class="mb-[5px] text-xs text-dim">Noise schedule</div>
                <Dropdown
                  v-model="scheduler"
                  aria-label="Noise schedule"
                  :options="[
                    { value: 'karras', label: 'Karras' },
                    { value: 'normal', label: 'Native' },
                  ]"
                />
              </div>
            </div>
            <div class="flex items-center">
              <span class="flex-1 text-xs text-dim">Quality tags</span>
              <ToggleSwitch v-model="qualityTags" />
            </div>
            <div class="flex items-center">
              <span class="flex-1 text-xs text-dim">Face detail pass</span>
              <ToggleSwitch v-model="faceDetail" title="Wired when the FaceDetailer workflow lands" />
            </div>
            <div>
              <div class="mb-[5px] text-xs text-dim">Size</div>
              <Dropdown
                v-model="sizeTier"
                aria-label="Size"
                :options="[
                  { value: 'normal', label: 'Normal (native)' },
                  { value: 'large', label: 'Large (×1.5 refine)' },
                  { value: 'ultra', label: 'Ultra (×2.0 refine)' },
                ]"
              />
              <div class="mt-1 text-[10.5px] leading-relaxed text-faint">
                Large and Ultra add a refine pass — smoother detail, roughly
                2× the time. Ultra is heavy on unified memory.
              </div>
            </div>
            <div v-if="loras.length" class="border-t border-line pt-3">
              <div class="mb-[5px] text-xs text-dim">Style / character LoRA</div>
              <Dropdown
                v-model="loraName"
                aria-label="LoRA"
                :options="[{ value: '', label: 'None' }, ...loras.map(l => ({ value: l, label: l.replace(/\.(safetensors|pt)$/, '') }))]"
              />
              <SliderRow v-if="loraName" v-model="loraStrength" label="LoRA strength" :min="0" :max="1.5" :step="0.05" :decimals="2" class="mt-2" />
              <template v-if="loraName">
                <div class="mb-[5px] mt-3 text-xs text-dim">Second LoRA (chained)</div>
                <Dropdown
                  v-model="lora2Name"
                  aria-label="Second LoRA"
                  :options="[{ value: '', label: 'None' }, ...loras.filter(l => l !== loraName).map(l => ({ value: l, label: l.replace(/\.(safetensors|pt)$/, '') }))]"
                />
                <SliderRow v-if="lora2Name" v-model="lora2Strength" label="Second strength" :min="0" :max="1.5" :step="0.05" :decimals="2" class="mt-2" />
              </template>
            </div>
            <div class="text-[11.5px] leading-relaxed text-faint">
              These defaults are set for {{ modelName }}. Switching models
              resets them — the settings don't carry across.
            </div>
          </div>
        </Fold>
      </div>

      <div class="sticky bottom-0 flex-none border-t border-line bg-surface px-4 py-3.5 md:static">
        <GenerateButton :cost="cost" :busy="job.busy.value" @click="generate" />
      </div>
    </aside>

    <!-- stage -->
    <section class="order-1 flex flex-none min-w-0 items-start justify-center px-4 pt-3.5 md:order-2 md:flex-1 md:items-center md:p-8">
      <StageCanvas
        :job="job"
        :width="dims[0]"
        :height="dims[1]"
        class="md:hidden"
        fit="block"
      />
      <StageCanvas
        :job="job"
        :width="dims[0]"
        :height="dims[1]"
        class="hidden md:flex"
        fit="viewport"
      />
    </section>

    <!-- recent: horizontal on mobile, right rail on desktop -->
    <RecentStrip
      class="order-2 flex-none md:hidden"
      orientation="horizontal"
      :items="recent"
      @open="openRecent"
      @error="onRecentError"
    >
      <template #actions>
        <button
          class="focus-ring flex size-9 flex-none cursor-pointer items-center justify-center rounded-ctrl border border-line text-dim hover:border-line-hi hover:text-text"
          aria-label="Save configuration"
          title="Save current configuration"
          @click="saveOpen = true"
        >
          <Icon name="lucide:bookmark-plus" :size="16" />
        </button>
        <button
          class="focus-ring relative flex size-9 flex-none cursor-pointer items-center justify-center rounded-ctrl border border-line text-dim hover:border-line-hi hover:text-text"
          aria-label="See configurations"
          title="See saved configurations"
          @click="drawerOpen = true"
        >
          <Icon name="lucide:folder-open" :size="16" />
          <span v-if="presets.length" class="absolute -right-1 -top-1 min-w-4 rounded-full bg-accent px-1 text-center font-mono text-[9px] leading-4 text-void">{{ presets.length }}</span>
        </button>
      </template>
    </RecentStrip>
    <RecentStrip
      class="order-3 hidden md:flex"
      orientation="vertical"
      :items="recent"
      @open="openRecent"
      @error="onRecentError"
    >
      <template #actions>
        <button
          class="focus-ring flex size-9 flex-none cursor-pointer items-center justify-center rounded-ctrl border border-line text-dim hover:border-line-hi hover:text-text"
          aria-label="Save configuration"
          title="Save current configuration"
          @click="saveOpen = true"
        >
          <Icon name="lucide:bookmark-plus" :size="16" />
        </button>
        <button
          class="focus-ring relative flex size-9 flex-none cursor-pointer items-center justify-center rounded-ctrl border border-line text-dim hover:border-line-hi hover:text-text"
          aria-label="See configurations"
          title="See saved configurations"
          @click="drawerOpen = true"
        >
          <Icon name="lucide:folder-open" :size="16" />
          <span v-if="presets.length" class="absolute -right-1 -top-1 min-w-4 rounded-full bg-accent px-1 text-center font-mono text-[9px] leading-4 text-void">{{ presets.length }}</span>
        </button>
      </template>
    </RecentStrip>

    <PresetSaveDialog v-model:open="saveOpen" @save="onSaveName" />
    <PresetsDrawer
      v-model:open="drawerOpen"
      :presets="drawerPresets"
      @use="onUsePreset"
      @delete="onDeletePresets"
    />
  </main>
</template>
