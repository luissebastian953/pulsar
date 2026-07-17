<script setup lang="ts">
import type { GenerationRequest, TranslatedTag } from '~/types/api'

// /pose grounds against the closed pose vocabulary (docs/tagdb.md#pose-subset)
// and generates a batch of 4 — pick the one that works.
const posePrompt = ref('')
const translate = useTranslate('pose')
watch(posePrompt, t => translate.onInput(t, 'plain'))

const CAMERA = [
  { tag: 'from_above', count: 700000 },
  { tag: 'from_below', count: 500000 },
  { tag: 'from_side', count: 400000 },
  { tag: 'from_behind', count: 600000 },
  { tag: 'dutch_angle', count: 150000 },
  { tag: 'foreshortening', count: 80000 },
]
const FRAMING = [
  { tag: 'full_body', count: 1200000 },
  { tag: 'upper_body', count: 1600000 },
  { tag: 'cowboy_shot', count: 500000 },
  { tag: 'portrait', count: 800000 },
]
const GIRL_N = [0, 5000000, 1500000, 400000, 150000, 80000]
const BOY_N = [0, 1700000, 350000, 90000, 40000, 20000]

const camera = ref<string[]>([])
const framing = ref<string[]>(['full_body'])
const figure = reactive({ girls: 1, boys: 0 })

function toggleIn(list: Ref<string[]>, tag: string) {
  list.value = list.value.includes(tag)
    ? list.value.filter(t => t !== tag)
    : [...list.value, tag]
}

const figTag = (n: number, word: string) => n === 1 ? `1${word}` : `${n}${word}s`

const combined = computed<TranslatedTag[]>(() => {
  const fig: TranslatedTag[] = []
  if (figure.girls > 0) fig.push({ canonical: figTag(figure.girls, 'girl'), category: 'general', count: GIRL_N[Math.min(figure.girls, 5)]!, concept: '', stage: 'exact' })
  if (figure.boys > 0) fig.push({ canonical: figTag(figure.boys, 'boy'), category: 'general', count: BOY_N[Math.min(figure.boys, 5)]!, concept: '', stage: 'exact' })
  if (figure.girls + figure.boys === 0) fig.push({ canonical: 'no_humans', category: 'general', count: 600000, concept: '', stage: 'exact' })
  const cam = CAMERA.filter(c => camera.value.includes(c.tag))
    .map((c): TranslatedTag => ({ canonical: c.tag, category: 'general', count: c.count, concept: '', stage: 'exact' }))
  const fr = FRAMING.filter(f => framing.value.includes(f.tag))
    .map((f): TranslatedTag => ({ canonical: f.tag, category: 'general', count: f.count, concept: '', stage: 'exact' }))
  return [...fig, ...translate.tags.value, ...cam, ...fr].sort((a, b) => b.count - a.count)
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

// ── presets ───────────────────────────────────────────────────────
function presetTurnaround() {
  posePrompt.value = 'turnaround, multiple views, standing, arms at sides'
  camera.value = []
  framing.value = ['full_body']
}
function presetAction() {
  posePrompt.value = 'jumping, twisting to look back, reaching toward the viewer, action pose'
  camera.value = ['from_below', 'foreshortening']
  framing.value = ['full_body']
}

// ── generation: batch of 4 real jobs ──────────────────────────────
const cells = [useJob(), useJob(), useJob(), useJob()]
const selected = ref(0)
const tab = ref<'lineart' | 'skeleton' | 'source'>('source')
const busy = computed(() => cells.some(c => c.busy.value))
const anyDone = computed(() => cells.some(c => c.phase.value === 'done'))

function generate() {
  if (busy.value || !combined.value.length) return
  const req: GenerationRequest = {
    prompt: combined.value.map(t => t.canonical).join(', '),
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
</script>

<template>
  <main class="flex min-h-0 flex-1 flex-col overflow-y-auto md:flex-row md:overflow-hidden">
    <!-- composer rail -->
    <aside class="order-2 flex flex-none flex-col bg-surface md:order-1 md:h-full md:w-[372px] md:min-h-0 md:border-r md:border-line">
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
            v-for="c in CAMERA"
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
            v-for="f in FRAMING"
            :key="f.tag"
            class="focus-ring cursor-pointer rounded-full border px-[13px] py-[5px] font-mono text-[11.5px]"
            :class="framing.includes(f.tag) ? 'pill-on' : 'border-line text-dim hover:border-line-hi'"
            :aria-pressed="framing.includes(f.tag)"
            @click="toggleIn(framing, f.tag)"
          >
            {{ f.tag.replaceAll('_', ' ') }}
          </button>
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
          Presets
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
            v-if="cell.phase.value === 'done' && tab === 'source'"
            :src="cell.imageUrl.value"
            :alt="`pose ${i + 1}`"
            class="absolute inset-0 size-full object-cover"
          >
          <div v-else-if="cell.phase.value === 'done'" class="p-4 text-center">
            <div class="font-mono text-xs text-dim">{{ tab }}</div>
            <div class="mt-1 text-[10.5px] leading-relaxed text-faint">
              extraction lands with the pose workflow
            </div>
          </div>
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
  </main>
</template>
