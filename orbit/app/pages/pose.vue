<script setup lang="ts">
import type { JobPayload } from '~/types/api'
import type { RecentItem } from '~/components/RecentStrip.vue'
import { POSE_CAMERA, POSE_FRAMING, POSE_PROPS, paramsToPoseConfig, poseConfigSignature, usePosePresets } from '~/composables/usePosePresets'

// Keep this page's composer state alive across navigation (SPA-only, ssr:false,
// so KeepAlive's client-only nature is a non-issue). Survives route switches;
// a full reload still resets — use a saved configuration to restore that.
definePageMeta({ keepalive: true })

const api = useApi()

const {
  posePrompt, translate, camera, framing, figure, background, heldProps,
  emptyHands, toggleProp, toggleEmptyHands, combined, stat,
  presetTurnaround, presetAction,
  cells, selected, tab, busy, anyDone, generate, usePose,
  recent,
  presets: posePresets, drawerPresets, applyConfig, onSaveName, onUsePreset, onDeletePresets,
} = usePoseComposer()

const recentItems = recent.items

// ── modal open flags ──────────────────────────────────────────────
const drawerOpen = ref(false)
const saveOpen = ref(false) // composer save
const detailSaveOpen = ref(false) // history-detail save

// ── recent detail modal ───────────────────────────────────────────
const detailJob = ref<JobPayload | null>(null)
const detailOpen = ref(false)

function openRecent(item: RecentItem) {
  const j = recent.jobs.value.find(x => x.id === item.id)

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

  applyConfig(paramsToPoseConfig(detailJob.value.params))
  detailOpen.value = false
}

// the composer's own save path (onSaveName) stores the live composer; this
// one stores the opened generation's config instead
const { save: savePosePreset } = usePosePresets()

function onDetailSaveName(name: string) {
  if (detailJob.value) savePosePreset(name, paramsToPoseConfig(detailJob.value.params))
}
</script>

<template>
  <main class="flex min-h-0 flex-1 flex-col overflow-y-auto md:flex-row md:overflow-hidden">
    <!-- composer rail -->
    <aside class="order-3 flex flex-none flex-col bg-surface md:order-1 md:h-full md:w-[372px] md:min-h-0 md:border-r md:border-line">
      <div class="min-h-0 p-4 md:flex-1 md:overflow-y-auto">
        <BaseSectionLabel class="mb-2.5">
          Pose
        </BaseSectionLabel>
        <BaseInput
          v-model="posePrompt"
          type="textarea"
          placeholder="twisting to look back, reaching toward the viewer…"
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

        <BaseSectionLabel class="mb-2.5 mt-4">
          Camera
        </BaseSectionLabel>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-for="c in POSE_CAMERA"
            :key="c.tag"
            class="focus-ring cursor-pointer rounded-full border px-[13px] py-[5px] font-mono text-[11.5px]"
            :class="camera.includes(c.tag) ? 'pill-on' : 'border-line text-dim hover:border-line-hi'"
            :aria-pressed="camera.includes(c.tag)"
            @click="toggleInPlace(camera, c.tag)"
          >
            {{ c.tag.replaceAll('_', ' ') }}
          </button>
        </div>

        <BaseSectionLabel class="mb-2.5 mt-4">
          Framing
        </BaseSectionLabel>
        <div class="flex flex-wrap gap-1.5">
          <button
            v-for="f in POSE_FRAMING"
            :key="f.tag"
            class="focus-ring cursor-pointer rounded-full border px-[13px] py-[5px] font-mono text-[11.5px]"
            :class="framing.includes(f.tag) ? 'pill-on' : 'border-line text-dim hover:border-line-hi'"
            :aria-pressed="framing.includes(f.tag)"
            @click="toggleInPlace(framing, f.tag)"
          >
            {{ f.tag.replaceAll('_', ' ') }}
          </button>
        </div>
        <div class="mt-1.5 text-[10.5px] leading-relaxed text-faint">
          Full body alone also negatives cropped limbs — hands and feet stay in frame.
        </div>

        <BaseSectionLabel class="mb-2.5 mt-4">
          Props
        </BaseSectionLabel>
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

        <BaseSectionLabel class="mb-2.5 mt-4">
          Background
        </BaseSectionLabel>
        <div class="flex items-center">
          <span class="flex-1 text-[12.5px] text-dim">Show background</span>
          <BaseToggleSwitch v-model="background" />
        </div>
        <div class="mt-1.5 text-[10.5px] leading-relaxed text-faint">
          Off keeps the sheet clean — figure-only lines and better skeleton detection.
        </div>

        <BaseSectionLabel class="mb-2.5 mt-4">
          Figure
        </BaseSectionLabel>
        <div class="flex flex-col gap-2">
          <div
            v-for="row in ([['Girls', 'girls', 'girl'], ['Boys', 'boys', 'boy']] as const)"
            :key="row[1]"
            class="flex items-center gap-2.5"
          >
            <span class="flex-1 text-[12.5px] text-dim">{{ row[0] }}</span>
            <span class="font-mono text-[11px] text-faint">
              {{ figure[row[1]] > 0 ? figureTag(figure[row[1]], row[2]) : '—' }}
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

        <BaseSectionLabel class="mb-2.5 mt-4">
          Quick starts
        </BaseSectionLabel>
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
        <GenerateButton
          :cost="20"
          :busy="busy"
          icon="lucide:zap"
          @click="generate"
        />
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
          <div
            v-else
            class="text-center"
          >
            <div class="font-mono text-xs text-faint">
              {{ cell.busy.value ? '…' : `▢ ${i + 1}` }}
            </div>
            <div
              v-if="i === 0 && cell.phase.value === 'idle'"
              class="mt-1 text-[10.5px] text-line-hi"
            >
              hit Generate for 4 options
            </div>
            <div
              v-if="cell.phase.value === 'failed'"
              class="mt-1 text-[10.5px] text-t-artist"
            >
              failed
            </div>
            <div
              v-if="cell.phase.value === 'blocked'"
              class="mt-1 text-[10.5px] text-warn"
            >
              blocked
            </div>
          </div>
          <div
            v-if="cell.busy.value"
            class="absolute inset-0 overflow-hidden"
          >
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
        <BaseSegment
          v-model="tab"
          class="w-auto"
          :options="[
            { value: 'lineart', label: 'Lineart' },
            { value: 'skeleton', label: 'Skeleton' },
            { value: 'source', label: 'Source' },
          ]"
        />
        <div class="flex-1" />
        <BaseButton
          type="secondary"
          :disabled="!anyDone"
          @click="usePose"
        >
          Use this pose
          <span class="inline-flex items-center gap-1 text-[11px] opacity-75">
            <Icon
              name="lucide:arrow-right"
              :size="13"
            />Create
          </span>
        </BaseButton>
      </div>
    </section>

    <!-- recent: horizontal on mobile, right rail on desktop -->
    <RecentStrip
      class="order-2 flex-none md:hidden"
      orientation="horizontal"
      :items="recentItems"
      @open="openRecent"
      @error="recent.removeErrored"
    >
      <template #actions>
        <ConfigActions
          :count="posePresets.length"
          @save="saveOpen = true"
          @open="drawerOpen = true"
        />
      </template>
    </RecentStrip>
    <RecentStrip
      class="order-3 hidden md:flex"
      orientation="vertical"
      :items="recentItems"
      @open="openRecent"
      @error="recent.removeErrored"
    >
      <template #actions>
        <ConfigActions
          :count="posePresets.length"
          @save="saveOpen = true"
          @open="drawerOpen = true"
        />
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
    <PresetSaveDialog
      v-model:open="saveOpen"
      @save="onSaveName"
    />
    <PresetSaveDialog
      v-model:open="detailSaveOpen"
      @save="onDetailSaveName"
    />
    <PresetsDrawer
      v-model:open="drawerOpen"
      :presets="drawerPresets"
      @use="onUsePreset"
      @delete="onDeletePresets"
    />
  </main>
</template>
