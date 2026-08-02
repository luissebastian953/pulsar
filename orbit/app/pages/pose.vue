<script setup lang="ts">
import type { GenerationRequest, JobPayload, TranslatedTag } from '~/types/api'
import type { RecentItem } from '~/components/RecentStrip.vue'
import {
  POSE_BG_TAGS, POSE_CAMERA, POSE_FRAMING, POSE_PROPS,
  paramsToPoseConfig, poseConfigSignature, usePendingPoseConfig, usePosePresets,
  type PoseConfig, type PosePreset,
} from '~/composables/usePosePresets'

// Keep this page's composer state alive across navigation (SPA-only, ssr:false,
// so KeepAlive's client-only nature is a non-issue). Survives route switches;
// a full reload still resets — use a saved configuration to restore that.
definePageMeta({ keepalive: true })

const api = useApi()

// /pose grounds against the closed pose vocabulary (docs/tagdb.md#pose-subset)
// and generates a batch of 4 — pick the one that works.
const posePrompt = ref('')
const translate = useTranslate('pose')
watch(posePrompt, t => translate.onInput(t, 'plain'))

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

// Template refs auto-unwrap, so this receives the raw array; mutate in place.
function toggleIn(list: string[], tag: string) {
  const i = list.indexOf(tag)
  if (i === -1) list.push(tag)
  else list.splice(i, 1)
}

function toggleProp(key: string) {
  emptyHands.value = false
  toggleIn(heldProps.value, key)
}

function toggleEmptyHands() {
  emptyHands.value = !emptyHands.value
  if (emptyHands.value) heldProps.value = []
}

const figTag = (n: number, word: string) => n === 1 ? `1${word}` : `${n}${word}s`

const mk = (tag: string, count: number): TranslatedTag =>
  ({ canonical: tag, category: 'general', count, concept: '', stage: 'exact' })

const combined = computed<TranslatedTag[]>(() => {
  const out: TranslatedTag[] = []
  if (figure.girls > 0) out.push(mk(figTag(figure.girls, 'girl'), GIRL_N[Math.min(figure.girls, 5)]!))
  if (figure.boys > 0) out.push(mk(figTag(figure.boys, 'boy'), BOY_N[Math.min(figure.boys, 5)]!))
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
const recentJobs = ref<JobPayload[]>([])
const poseThumb = (j: JobPayload) => j.images?.lineart ?? j.image_url ?? ''
const recent = computed<RecentItem[]>(() => recentJobs.value.map(j => ({
  id: j.id,
  imageUrl: api.imageUrl(poseThumb(j)),
  title: `seed ${j.params.seed}`,
})))

function addRecent(j: JobPayload) {
  if (!poseThumb(j)) return
  recentJobs.value = [j, ...recentJobs.value].slice(0, 12)
}

// Reloaded on every activation (kept-alive page) so the strip stays in sync
// after history deletions, same as /create.
async function loadRecent() {
  try {
    const res = await api.get<{ jobs: JobPayload[] }>('/history')
    recentJobs.value = res.jobs
      .filter(j => j.kind === 'pose' && j.state === 'done' && (j.image_url || j.images))
      .slice(0, 12)
  }
  catch { /* core offline — leave the strip as-is */ }
}
onActivated(loadRecent)

function onRecentError(id: string) {
  recentJobs.value = recentJobs.value.filter(j => j.id !== id)
}

// ── generation: batch of 4 real jobs ──────────────────────────────
const cells = [useJob(addRecent), useJob(addRecent), useJob(addRecent), useJob(addRecent)]
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

const poseRef = useState<{ seed: string, label: string } | null>('poseRef', () => null)
const openRefFold = useState('openRefFold', () => false)

function usePose() {
  const cell = cells[selected.value]!
  if (cell.phase.value !== 'done') return
  poseRef.value = { seed: cell.seed.value, label: `from /pose · seed ${cell.seed.value}` }
  openRefFold.value = true
  navigateTo('/create')
}

// ── configurations (presets) ──────────────────────────────────────
const { presets: posePresets, save: savePosePreset, remove: removePosePresets } = usePosePresets()
const drawerOpen = ref(false)
const saveOpen = ref(false) // composer save
const detailSaveOpen = ref(false) // history-detail save

function buildPoseConfig(): PoseConfig {
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

function applyPoseConfig(cfg: PoseConfig) {
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
  const fig = [c.girls ? figTag(c.girls, 'girl') : '', c.boys ? figTag(c.boys, 'boy') : '']
    .filter(Boolean).join(' ')
  return [
    fig || 'no humans',
    c.background ? 'scene' : 'clean bg',
    c.props.length ? c.props.join(' ') : (c.emptyHands ? 'empty hands' : ''),
  ].filter(Boolean).join(' · ')
}
const drawerPresets = computed(() =>
  posePresets.value.map(p => ({ id: p.id, name: p.name, subtitle: poseSubtitle(p) })))

function onSaveName(name: string) {
  savePosePreset(name, buildPoseConfig())
}
function onUsePreset(id: string) {
  const p = posePresets.value.find(x => x.id === id)
  if (p) applyPoseConfig(p.config)
}
function onDeletePresets(ids: string[]) {
  removePosePresets(ids)
}

// "Use related config" from the /history pose tab hands a config off here;
// immediate covers cold navigation, the watch covers keep-alive re-entry.
const pendingPoseConfig = usePendingPoseConfig()
watch(pendingPoseConfig, (c) => {
  if (!c) return
  pendingPoseConfig.value = null
  applyPoseConfig(c)
}, { immediate: true })

// ── recent detail modal ───────────────────────────────────────────
const detailJob = ref<JobPayload | null>(null)
const detailOpen = ref(false)

function openRecent(item: RecentItem) {
  const j = recentJobs.value.find(x => x.id === item.id)
  if (!j) return
  detailJob.value = j
  detailOpen.value = true
}

const detailSaved = computed(() => {
  if (!detailJob.value) return false
  const sig = poseConfigSignature(paramsToPoseConfig(detailJob.value.params))
  return posePresets.value.some(p => poseConfigSignature(p.config) === sig)
})

function useDetailConfig() {
  if (!detailJob.value) return
  applyPoseConfig(paramsToPoseConfig(detailJob.value.params))
  detailOpen.value = false
}

function onDetailSaveName(name: string) {
  if (detailJob.value) savePosePreset(name, paramsToPoseConfig(detailJob.value.params))
}
</script>

<template>
  <main class="flex min-h-0 flex-1 flex-col overflow-y-auto md:flex-row md:overflow-hidden">
    <!-- composer rail -->
    <aside class="order-3 flex flex-none flex-col bg-surface md:order-1 md:h-full md:w-[372px] md:min-h-0 md:border-r md:border-line">
      <div class="min-h-0 p-4 md:flex-1 md:overflow-y-auto">
        <SectionLabel class="mb-2.5">
          Pose
        </SectionLabel>
        <textarea
          v-model="posePrompt"
          placeholder="twisting to look back, reaching toward the viewer…"
          class="min-h-[78px] w-full resize-y rounded-ctrl border border-line bg-void px-3 py-2.5 text-sm leading-relaxed text-text outline-none placeholder:text-faint focus:border-line-hi"
        />

        <ResolvedStrip
          class="mt-3"
          :tags="combined"
          :dropped="translate.dropped.value"
          :resolving="translate.resolving.value"
          :offline="translate.offline.value"
          :failed="translate.failed.value"
          :stat="stat"
          empty-hint="Describe a body pose and it becomes pose tags here."
        />

        <SectionLabel class="mb-2.5 mt-4">
          Camera
        </SectionLabel>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-for="c in POSE_CAMERA"
            :key="c.tag"
            class="focus-ring cursor-pointer rounded-full border px-[13px] py-[5px] font-mono text-[11.5px]"
            :class="camera.includes(c.tag) ? 'pill-on' : 'border-line text-dim hover:border-line-hi'"
            :aria-pressed="camera.includes(c.tag)"
            @click="toggleIn(camera, c.tag)"
          >
            {{ c.tag.replaceAll('_', ' ') }}
          </button>
        </div>

        <SectionLabel class="mb-2.5 mt-4">
          Framing
        </SectionLabel>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-for="f in POSE_FRAMING"
            :key="f.tag"
            class="focus-ring cursor-pointer rounded-full border px-[13px] py-[5px] font-mono text-[11.5px]"
            :class="framing.includes(f.tag) ? 'pill-on' : 'border-line text-dim hover:border-line-hi'"
            :aria-pressed="framing.includes(f.tag)"
            @click="toggleIn(framing, f.tag)"
          >
            {{ f.tag.replaceAll('_', ' ') }}
          </button>
        </div>
        <div class="mt-1.5 text-[10.5px] leading-relaxed text-faint">
          Full body alone also negatives cropped limbs — hands and feet stay in frame.
        </div>

        <SectionLabel class="mb-2.5 mt-4">
          Props
        </SectionLabel>
        <div class="flex flex-wrap gap-1.5">
          <button
            class="focus-ring cursor-pointer rounded-full border px-[13px] py-[5px] font-mono text-[11.5px]"
            :class="emptyHands ? 'pill-on' : 'border-line text-dim hover:border-line-hi'"
            :aria-pressed="emptyHands"
            @click="toggleEmptyHands"
          >
            empty hands
          </button>
          <button
            v-for="p in POSE_PROPS"
            :key="p.key"
            class="focus-ring cursor-pointer rounded-full border px-[13px] py-[5px] font-mono text-[11.5px]"
            :class="heldProps.includes(p.key) ? 'pill-on' : 'border-line text-dim hover:border-line-hi'"
            :aria-pressed="heldProps.includes(p.key)"
            @click="toggleProp(p.key)"
          >
            {{ p.label }}
          </button>
        </div>
        <div class="mt-1.5 text-[10.5px] leading-relaxed text-faint">
          Empty hands keeps invented weapons out of action poses.
        </div>

        <SectionLabel class="mb-2.5 mt-4">
          Background
        </SectionLabel>
        <div class="flex items-center">
          <span class="flex-1 text-[12.5px] text-dim">Show background</span>
          <ToggleSwitch v-model="background" />
        </div>
        <div class="mt-1.5 text-[10.5px] leading-relaxed text-faint">
          Off keeps the sheet clean — figure-only lines and better skeleton detection.
        </div>

        <SectionLabel class="mb-2.5 mt-4">
          Figure
        </SectionLabel>
        <div class="flex flex-col gap-2">
          <div v-for="row in ([['Girls', 'girls', 'girl'], ['Boys', 'boys', 'boy']] as const)" :key="row[1]" class="flex items-center gap-2.5">
            <span class="flex-1 text-[12.5px] text-dim">{{ row[0] }}</span>
            <span class="font-mono text-[11px] text-faint">
              {{ figure[row[1]] > 0 ? figTag(figure[row[1]], row[2]) : '—' }}
            </span>
            <span class="flex items-center overflow-hidden rounded-ctrl border border-line">
              <button
                class="focus-ring-inset cursor-pointer px-[11px] py-1.5 text-sm leading-none text-dim hover:bg-line hover:text-text"
                :class="figure[row[1]] === 0 && 'opacity-35'"
                @click="figure[row[1]] = Math.max(0, figure[row[1]] - 1)"
              >−</button>
              <span class="min-w-[26px] border-x border-line py-1.5 text-center font-mono text-[12.5px] text-text">{{ figure[row[1]] }}</span>
              <button
                class="focus-ring-inset cursor-pointer px-[11px] py-1.5 text-sm leading-none text-dim hover:bg-line hover:text-text"
                :class="figure[row[1]] >= 5 && 'opacity-35'"
                @click="figure[row[1]] = Math.min(5, figure[row[1]] + 1)"
              >+</button>
            </span>
          </div>
        </div>

        <SectionLabel class="mb-2.5 mt-4">
          Quick starts
        </SectionLabel>
        <div class="flex flex-col gap-2">
          <button
            class="focus-ring w-full cursor-pointer rounded-ctrl border border-line bg-raised px-3 py-[9px] text-left text-[12.5px] text-text hover:border-line-hi"
            @click="presetTurnaround"
          >
            Turnaround sheet <span class="ml-1.5 font-mono text-[10.5px] text-faint">4 views, full body</span>
          </button>
          <button
            class="focus-ring w-full cursor-pointer rounded-ctrl border border-line bg-raised px-3 py-[9px] text-left text-[12.5px] text-text hover:border-line-hi"
            @click="presetAction"
          >
            Dynamic action <span class="ml-1.5 font-mono text-[10.5px] text-faint">low angle, foreshortened</span>
          </button>
        </div>
      </div>

      <div class="sticky bottom-0 flex-none border-t border-line bg-surface px-4 py-3.5 md:static">
        <GenerateButton :cost="20" :busy="busy" icon="lucide:zap" @click="generate" />
        <div class="mt-[7px] text-center font-mono text-[10.5px] text-faint">
          batch of 4 · pick the one that works
        </div>
      </div>
    </aside>

    <!-- results -->
    <section class="order-1 flex flex-none min-w-0 flex-col gap-3.5 px-4 pt-3.5 md:order-2 md:flex-1 md:px-8 md:py-6">
      <div class="mx-auto grid w-full max-w-[820px] grid-cols-2 gap-3 md:min-h-0 md:flex-1 md:grid-rows-2">
        <button
          v-for="(cell, i) in cells"
          :key="i"
          class="focus-ring relative flex cursor-pointer items-center justify-center overflow-hidden rounded-canvas border bg-surface aspect-[832/1216] md:aspect-auto"
          :class="cell.phase.value === 'done' && selected === i ? 'border-dim' : 'border-line'"
          @click="cell.phase.value === 'done' && (selected = i)"
        >
          <img
            v-if="cell.phase.value === 'done'"
            :src="cell.images.value[tab] ?? cell.imageUrl.value"
            :alt="`pose ${i + 1} ${tab}`"
            class="absolute inset-0 size-full object-cover"
            :class="{ 'bg-white': tab !== 'source' }"
          >
          <div v-else class="text-center">
            <div class="font-mono text-xs text-faint">
              {{ cell.busy.value ? '…' : `▢ ${i + 1}` }}
            </div>
            <div v-if="i === 0 && cell.phase.value === 'idle'" class="mt-1 text-[10.5px] text-line-hi">
              hit Generate for 4 options
            </div>
            <div v-if="cell.phase.value === 'failed'" class="mt-1 text-[10.5px] text-t-artist">failed</div>
            <div v-if="cell.phase.value === 'blocked'" class="mt-1 text-[10.5px] text-warn">blocked</div>
          </div>
          <div v-if="cell.busy.value" class="absolute inset-0 overflow-hidden">
            <div
              class="absolute inset-x-0 h-1/3"
              style="background: linear-gradient(180deg, transparent, color-mix(in srgb, var(--color-accent) 10%, transparent), transparent); animation: sweep 1.5s linear infinite"
            />
          </div>
          <span
            v-if="cell.phase.value === 'done' && selected === i"
            class="absolute right-2 top-2 rounded-lg border border-line-hi bg-void/75 px-[7px] py-0.5 text-2xs uppercase tracking-[.1em] text-text"
          >selected</span>
        </button>
      </div>

      <div class="mx-auto flex w-full max-w-[820px] flex-none items-center gap-3 pb-3.5 md:pb-0">
        <Segment
          v-model="tab"
          class="w-auto"
          :options="[
            { value: 'lineart', label: 'Lineart' },
            { value: 'skeleton', label: 'Skeleton' },
            { value: 'source', label: 'Source' },
          ]"
        />
        <div class="flex-1" />
        <button
          class="focus-ring flex cursor-pointer items-center gap-2 rounded-ctrl px-4 py-[9px] text-[13px] font-semibold text-accent-soft"
          style="background: color-mix(in srgb, var(--color-accent) 15%, transparent)"
          :class="!anyDone && 'cursor-default opacity-45'"
          @click="usePose"
        >
          Use this pose
          <span class="inline-flex items-center gap-1 text-[11px] opacity-75">
            <Icon name="lucide:arrow-right" :size="13" />Create
          </span>
        </button>
      </div>
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
          <span v-if="posePresets.length" class="absolute -right-1 -top-1 min-w-4 rounded-full bg-accent px-1 text-center font-mono text-[9px] leading-4 text-void">{{ posePresets.length }}</span>
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
          <span v-if="posePresets.length" class="absolute -right-1 -top-1 min-w-4 rounded-full bg-accent px-1 text-center font-mono text-[9px] leading-4 text-void">{{ posePresets.length }}</span>
        </button>
      </template>
    </RecentStrip>

    <HistoryDetail
      v-model:open="detailOpen"
      :job="detailJob"
      :image-url="detailJob ? api.imageUrl(detailJob.image_url) : ''"
      :saved="detailSaved"
      @use="useDetailConfig"
      @save="detailSaveOpen = true"
    />
    <PresetSaveDialog v-model:open="saveOpen" @save="onSaveName" />
    <PresetSaveDialog v-model:open="detailSaveOpen" @save="onDetailSaveName" />
    <PresetsDrawer
      v-model:open="drawerOpen"
      :presets="drawerPresets"
      @use="onUsePreset"
      @delete="onDeletePresets"
    />
  </main>
</template>
