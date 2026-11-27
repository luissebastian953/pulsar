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
const {
  jobs, loaded, tab, shown, counts, isPose, imgOf,
  selecting, checked, toggleCheck,
  downloading, dlDone, dlTotal, onDownloadClick,
  confirmAllOpen, onBinClick, deleteAll,
} = useHistoryPage()

// ── detail modal + preset glue ────────────────────────────────────
const { presets, save: savePreset } = usePresets()
const { presets: posePresets, save: savePosePreset } = usePosePresets()
const pendingConfig = usePendingConfig()
const pendingPoseConfig = usePendingPoseConfig()
const chunkApi = useChunkApi()

// stored structure when present (no ComfyUI escapes); legacy rows show the string
function promptLine(j: JobPayload) {
  return j.params.prompt_tags ? j.params.prompt_tags.map(t => tagLabel(t)).join(', ') : j.params.prompt
}

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

function cardClick(j: JobPayload) {
  if (selecting.value) {
    toggleCheck(j.id)

    return
  }

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

  // chunk provenance: a stored tag run becomes its `!macro:Name!` again only
  // where today's chunk still resolves to exactly those tags (decision D8)
  pendingConfig.value = await chunkApi.restoreConfig(selected.value.params)
  await navigateTo('/create')
}

function onSaveName(name: string) {
  if (!selected.value) return

  if (isPose(selected.value)) savePosePreset(name, paramsToPoseConfig(selected.value.params))
  else savePreset(name, paramsToPresetConfig(selected.value.params))
}
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto p-4 md:p-8">
    <div class="mx-auto mb-4 flex max-w-5xl items-center gap-3">
      <BaseSectionLabel>History</BaseSectionLabel>
      <div class="flex-1" />
      <BaseSegment
        v-model="tab"
        class="w-[200px]"
        :options="[
          { value: 'create', label: `Create${counts.create ? ` (${counts.create})` : ''}` },
          { value: 'pose', label: `Pose${counts.pose ? ` (${counts.pose})` : ''}` },
        ]"
      />
      <BaseButton
        v-if="jobs.length"
        class="flex-none"
        variant="outline"
        :type="selecting ? 'primary' : 'default'"
        :icon="selecting ? 'lucide:x' : 'lucide:list-checks'"
        :aria-label="selecting ? 'Done selecting' : 'Select items'"
        :title="selecting ? 'Done' : 'Select items'"
        @click="selecting = !selecting"
      />
      <BaseButton
        v-if="jobs.length"
        class="flex-none"
        size="small"
        variant="outline"
        icon="lucide:download"
        :loading="downloading"
        :disabled="selecting && !checked.size"
        :aria-label="selecting ? 'Download selected history' : 'Download all history'"
        :title="selecting ? 'Download selected history' : 'Download all history'"
        @click="onDownloadClick"
      >
        {{ downloading ? `Downloading ${dlDone}/${dlTotal}` : (selecting ? `Download Selected${checked.size ? ` (${checked.size})` : ''}` : 'Download all') }}
      </BaseButton>
      <BaseButton
        v-if="jobs.length"
        class="flex-none"
        size="small"
        variant="outline"
        type="warn"
        icon="lucide:trash-2"
        :disabled="selecting && !checked.size"
        :aria-label="selecting ? 'Delete selected history' : 'Delete all history'"
        :title="selecting ? 'Delete selected history' : 'Delete all history'"
        @click="onBinClick"
      >
        {{ selecting ? `Delete Selected${checked.size ? ` (${checked.size})` : ''}` : 'Delete all' }}
      </BaseButton>
    </div>

    <div
      v-if="shown.length"
      class="mx-auto grid max-w-5xl grid-cols-2 gap-3 md:grid-cols-4"
    >
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
            <div
              class="truncate font-mono text-2xs text-dim"
              :title="promptLine(j)"
            >
              {{ promptLine(j) }}
            </div>
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
          <Icon
            name="lucide:check"
            :size="14"
          />
        </span>
      </div>
    </div>

    <div
      v-else-if="loaded"
      class="mx-auto flex max-w-5xl flex-col items-center gap-3 py-24 text-center"
    >
      <Icon
        name="lucide:history"
        :size="28"
        class="text-line-hi"
      />
      <div class="text-[13px] text-dim">
        Nothing in {{ tab === 'pose' ? 'Pose' : 'Create' }} history yet.
      </div>
      <NuxtLink
        :to="tab === 'pose' ? '/pose' : '/create'"
        class="text-[13px] text-t-general hover:text-text"
      >
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
    <PresetSaveDialog
      v-model:open="saveOpen"
      @save="onSaveName"
    />

    <AlertDialogRoot
      :open="confirmAllOpen"
      @update:open="v => { if (!v) confirmAllOpen = false }"
    >
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
            <AlertDialogCancel as-child>
              <BaseButton variant="outline">
                Cancel
              </BaseButton>
            </AlertDialogCancel>
            <BaseButton
              type="warn"
              icon="lucide:trash-2"
              @click="deleteAll"
            >
              Delete all
            </BaseButton>
          </div>
        </AlertDialogContent>
      </AlertDialogPortal>
    </AlertDialogRoot>
  </main>
</template>
