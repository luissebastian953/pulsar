<script setup lang="ts">
import type { GenerationRequest, JobPayload, UploadResult, TranslatedTag } from '~/types/api'
import type { RecentItem } from '~/components/RecentStrip.vue'

const api = useApi()
const { public: { modelName } } = useRuntimeConfig()

// ── composer state ────────────────────────────────────────────────
const prompt = ref('')
const mode = ref<'plain' | 'tags'>('plain')
const tagList = ref<string[]>([])
const und = ref('')
const chars = ref<{ text: string, enabled: boolean }[]>([])
const aspect = ref<'portrait' | 'landscape' | 'square'>('portrait')
const batch = ref(1)
const steps = ref(24)
const guidance = ref(5.5)
const seedText = ref('')
const sampler = ref('euler_ancestral')
const scheduler = ref('karras')
const qualityTags = ref(true)
const faceDetail = ref(true)
const hires = ref(false)

const folds = reactive({ und: false, chars: false, ref: false, i2i: false, adv: false })

// pose → create handoff ("Use this pose")
const poseRef = useState<{ seed: string, label: string } | null>('poseRef', () => null)
const openRefFold = useState('openRefFold', () => false)
const refStrength = ref(0.6)

// image-to-image / inpaint: eager upload on pick so the mask editor can
// paint on the normalized image (dims always match the bucket)
const i2iFile = ref<File | null>(null)
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

const promptText = computed(() => {
  const base = mode.value === 'plain'
    ? translate.tags.value.map(t => t.canonical)
    : tagList.value
  const charText = chars.value
    .filter(c => c.enabled && c.text.trim())
    .map(c => c.text.trim())
  return [...base, ...charText].join(', ')
})

const cost = computed(() => batch.value * (17 + (hires.value ? 8 : 0)))

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
  }
  if (i2iId.value) {
    req.kind = maskId.value ? 'inpaint' : 'img2img'
    req.image_id = i2iId.value
    if (maskId.value) req.mask_id = maskId.value
    req.denoise = i2iStrength.value
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

onMounted(async () => {
  try {
    const res = await api.get<{ jobs: JobPayload[] }>('/history')
    recent.value = res.jobs
      .filter(j => j.state === 'done' && j.image_url)
      .slice(0, 12)
      .map(j => ({
        id: j.id,
        imageUrl: api.imageUrl(j.image_url),
        title: `${j.params.width} × ${j.params.height} · seed ${j.params.seed}`,
      }))
  }
  catch { /* core offline — empty strip */ }
})

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
              :index="i"
              @remove="chars.splice(i, 1)"
            />
            <button
              class="focus-ring w-full cursor-pointer rounded-ctrl border border-dashed border-line-hi py-[9px] text-center text-[12.5px] text-dim hover:border-dim hover:text-text"
              @click="chars.push({ text: '', enabled: true })"
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

        <Fold v-model="folds.ref" label="Reference" :badge="poseRef ? 'pose skeleton' : 'off'">
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
            <button
              class="focus-ring w-full cursor-not-allowed rounded-ctrl border border-dashed border-line-hi py-[9px] text-center text-[12.5px] text-faint"
              title="Character references land with image ingest"
              disabled
            >
              <span class="inline-flex items-center justify-center gap-1.5">
                <Icon name="lucide:plus" :size="13" />Character reference
              </span>
            </button>
          </div>
        </Fold>

        <Fold v-model="folds.i2i" label="Image to image" :badge="maskId ? 'inpaint' : (i2iId ? 'on' : 'off')">
          <div class="grid gap-2.5">
            <template v-if="i2iFile">
              <div class="flex items-center gap-2.5 rounded-ctrl border border-line bg-void p-2">
                <img :src="i2iPreview" alt="input" class="size-14 flex-none rounded-lg object-cover">
                <div class="min-w-0 flex-1">
                  <div class="truncate text-[12px] text-dim">{{ i2iFile.name }}</div>
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

        <Fold v-model="folds.adv" label="Advanced" :badge="`tuned for ${modelName}`" class="mt-3.5">
          <div class="flex flex-col gap-3">
            <SliderRow v-model="steps" label="Steps" :min="1" :max="50" />
            <SliderRow v-model="guidance" label="Prompt guidance" :min="1" :max="12" :step="0.1" :decimals="1" />
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
            <div class="flex items-center">
              <span class="flex-1 text-xs text-dim">High resolution</span>
              <ToggleSwitch v-model="hires" title="Wired when the hires workflow lands" />
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
    />
    <RecentStrip
      class="order-3 hidden md:flex"
      orientation="vertical"
      :items="recent"
      @open="openRecent"
    />
  </main>
</template>
