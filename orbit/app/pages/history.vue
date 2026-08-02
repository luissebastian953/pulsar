<script setup lang="ts">
import {
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogOverlay,
  AlertDialogPortal,
  AlertDialogRoot,
  AlertDialogTitle,
} from 'reka-ui'
import type { JobPayload } from '~/types/api'
import { configSignature, paramsToPresetConfig, usePendingConfig } from '~/composables/usePresets'
import { paramsToPoseConfig, poseConfigSignature, usePendingPoseConfig, usePosePresets } from '~/composables/usePosePresets'

const api = useApi()
const { presets, save: savePreset } = usePresets()
const { presets: posePresets, save: savePosePreset } = usePosePresets()
const pendingConfig = usePendingConfig()
const pendingPoseConfig = usePendingPoseConfig()

const jobs = ref<JobPayload[]>([])
const loaded = ref(false)
const tab = ref<'create' | 'pose'>('create')
const selecting = ref(false)
const confirmAllOpen = ref(false)

// the pose surface generates skeleton refs; everything else is a Create result
const isPose = (j: JobPayload) => j.kind === 'pose'
// pose cards read as a sketch library: show the lineart, not the source render
const imgOf = (j: JobPayload) => (isPose(j) ? j.images?.lineart : undefined)
  ?? j.image_url ?? Object.values(j.images ?? {})[0] ?? ''
const shown = computed(() => jobs.value.filter(j => (tab.value === 'pose') === isPose(j)))
const counts = computed(() => ({
  create: jobs.value.filter(j => !isPose(j)).length,
  pose: jobs.value.filter(isPose).length,
}))

// detail modal
const selected = ref<JobPayload | null>(null)
const detailOpen = ref(false)
const saveOpen = ref(false)

const selectedSaved = computed(() => {
  if (!selected.value) return false
  if (isPose(selected.value)) {
    const sig = poseConfigSignature(paramsToPoseConfig(selected.value.params))
    return posePresets.value.some(p => poseConfigSignature(p.config) === sig)
  }
  const sig = configSignature(paramsToPresetConfig(selected.value.params))
  return presets.value.some(p => configSignature(p.config) === sig)
})

function openDetail(j: JobPayload) {
  selected.value = j
  detailOpen.value = true
}

// each surface consumes its own pending-config channel
async function useConfig() {
  if (!selected.value) return
  detailOpen.value = false
  if (isPose(selected.value)) {
    pendingPoseConfig.value = paramsToPoseConfig(selected.value.params)
    await navigateTo('/pose')
    return
  }
  pendingConfig.value = paramsToPresetConfig(selected.value.params)
  await navigateTo('/create')
}

function onSaveName(name: string) {
  if (!selected.value) return
  if (isPose(selected.value)) savePosePreset(name, paramsToPoseConfig(selected.value.params))
  else savePreset(name, paramsToPresetConfig(selected.value.params))
}

// checkbox selection (distinct from `selected`, which is the detail-modal job)
const checked = ref<Set<string>>(new Set())
watch(selecting, (on) => { if (!on) checked.value = new Set() })

function toggleCheck(id: string) {
  const next = new Set(checked.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  checked.value = next
}

function cardClick(j: JobPayload) {
  if (selecting.value) toggleCheck(j.id)
  else openDetail(j)
}

// the top-right bin is contextual: delete the checked items in select mode,
// otherwise open the delete-all confirmation
function onBinClick() {
  if (selecting.value) deleteSelected()
  else confirmAllOpen.value = true
}

async function deleteSelected() {
  const ids = [...checked.value]
  if (!ids.length) return
  await Promise.allSettled(ids.map(id => api.del(`/history/${id}`)))
  const drop = new Set(ids)
  jobs.value = jobs.value.filter(x => !drop.has(x.id))
  selecting.value = false
}

async function deleteAll() {
  confirmAllOpen.value = false
  try {
    await api.del('/history')
    jobs.value = []
    selecting.value = false
  }
  catch { /* core offline */ }
}

onMounted(async () => {
  try {
    const res = await api.get<{ jobs: JobPayload[] }>('/history')
    jobs.value = res.jobs.filter(j => j.state === 'done' && (j.image_url || j.images))
  }
  catch { /* core offline */ }
  loaded.value = true
})
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto p-4 md:p-8">
    <div class="mx-auto mb-4 flex max-w-5xl items-center gap-3">
      <SectionLabel>History</SectionLabel>
      <div class="flex-1" />
      <Segment
        v-model="tab"
        class="w-[200px]"
        :options="[
          { value: 'create', label: `Create${counts.create ? ` (${counts.create})` : ''}` },
          { value: 'pose', label: `Pose${counts.pose ? ` (${counts.pose})` : ''}` },
        ]"
      />
      <button
        v-if="jobs.length"
        class="focus-ring flex size-8 flex-none cursor-pointer items-center justify-center rounded-ctrl border border-line text-dim hover:border-line-hi hover:text-text"
        :class="selecting && 'border-accent text-accent-soft'"
        :aria-label="selecting ? 'Done selecting' : 'Select to delete'"
        :title="selecting ? 'Done' : 'Select items to delete'"
        @click="selecting = !selecting"
      >
        <Icon :name="selecting ? 'lucide:x' : 'lucide:list-checks'" :size="15" />
      </button>
      <button
        v-if="jobs.length"
        class="focus-ring flex h-8 flex-none cursor-pointer items-center gap-1.5 rounded-ctrl border border-line px-2.5 text-[12px] text-warn hover:border-warn/60 hover:bg-warn/10 disabled:cursor-not-allowed disabled:opacity-40"
        :disabled="selecting && !checked.size"
        :aria-label="selecting ? 'Delete selected history' : 'Delete all history'"
        :title="selecting ? 'Delete selected history' : 'Delete all history'"
        @click="onBinClick"
      >
        <Icon name="lucide:trash-2" :size="15" />
        <span>{{ selecting ? `Delete Selected${checked.size ? ` (${checked.size})` : ''}` : 'Delete all' }}</span>
      </button>
    </div>

    <div v-if="shown.length" class="mx-auto grid max-w-5xl grid-cols-2 gap-3 md:grid-cols-4">
      <div
        v-for="j in shown"
        :key="j.id"
        class="relative overflow-hidden rounded-ctrl border bg-surface"
        :class="selecting && checked.has(j.id) ? 'border-accent' : (selecting ? 'border-line' : 'border-line hover:border-line-hi')"
      >
        <button
          class="focus-ring-inset block w-full cursor-pointer text-left"
          :aria-pressed="selecting ? checked.has(j.id) : undefined"
          @click="cardClick(j)"
        >
          <img
            :src="api.imageUrl(imgOf(j))"
            :alt="`generation ${j.id}`"
            loading="lazy"
            class="w-full object-cover"
            :class="selecting && !checked.has(j.id) && 'opacity-70'"
            :style="{ aspectRatio: `${j.params.width} / ${j.params.height}` }"
          >
          <div class="border-t border-line px-2.5 py-2">
            <div class="truncate font-mono text-2xs text-dim" :title="j.params.prompt">{{ j.params.prompt }}</div>
            <div class="mt-1 font-mono text-2xs text-faint">
              {{ j.params.width }} × {{ j.params.height }} · seed {{ j.params.seed }}
            </div>
          </div>
        </button>
        <span
          v-if="selecting"
          class="pointer-events-none absolute right-2 top-2 flex size-[22px] items-center justify-center rounded-md border"
          :class="checked.has(j.id) ? 'border-accent bg-accent text-void' : 'border-line-hi bg-void/70 text-transparent'"
        >
          <Icon name="lucide:check" :size="14" />
        </span>
      </div>
    </div>

    <div v-else-if="loaded" class="mx-auto flex max-w-5xl flex-col items-center gap-3 py-24 text-center">
      <Icon name="lucide:history" :size="28" class="text-line-hi" />
      <div class="text-[13px] text-dim">Nothing in {{ tab === 'pose' ? 'Pose' : 'Create' }} history yet.</div>
      <NuxtLink :to="tab === 'pose' ? '/pose' : '/create'" class="text-[13px] text-t-general hover:text-text">
        Head to {{ tab === 'pose' ? 'Pose' : 'Create' }} →
      </NuxtLink>
    </div>

    <!-- the modal's base image is the primary (source); role tabs cover the rest -->
    <HistoryDetail
      v-model:open="detailOpen"
      :job="selected"
      :image-url="selected ? api.imageUrl(selected.image_url ?? imgOf(selected)) : ''"
      :saved="selectedSaved"
      @use="useConfig"
      @save="saveOpen = true"
    />
    <PresetSaveDialog v-model:open="saveOpen" @save="onSaveName" />

    <AlertDialogRoot :open="confirmAllOpen" @update:open="v => { if (!v) confirmAllOpen = false }">
      <AlertDialogPortal>
        <AlertDialogOverlay class="o-anim fixed inset-0 z-[60] bg-void/70" />
        <AlertDialogContent class="pop-anim fixed left-1/2 top-1/2 z-[61] w-[320px] max-w-[90vw] -translate-x-1/2 -translate-y-1/2 rounded-canvas border border-line bg-raised p-5 shadow-[0_20px_50px_rgba(0,0,0,.6)] focus:outline-none">
          <AlertDialogTitle class="text-[14px] font-semibold text-text">
            Delete all history?
          </AlertDialogTitle>
          <AlertDialogDescription class="mt-1.5 text-[12.5px] leading-relaxed text-dim">
            Permanently removes all {{ jobs.length }} generated {{ jobs.length === 1 ? 'image' : 'images' }} and their configs. This can't be undone.
          </AlertDialogDescription>
          <div class="mt-4 flex justify-end gap-2">
            <AlertDialogCancel class="focus-ring cursor-pointer rounded-ctrl border border-line px-3.5 py-1.5 text-[12.5px] text-dim hover:border-line-hi hover:text-text">
              Cancel
            </AlertDialogCancel>
            <button class="focus-ring flex cursor-pointer items-center gap-1.5 rounded-ctrl bg-warn px-3.5 py-1.5 text-[12.5px] font-medium text-void hover:brightness-110" @click="deleteAll">
              <Icon name="lucide:trash-2" :size="13" />Delete all
            </button>
          </div>
        </AlertDialogContent>
      </AlertDialogPortal>
    </AlertDialogRoot>
  </main>
</template>
