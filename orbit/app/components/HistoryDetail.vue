<script setup lang="ts">
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import type { JobPayload } from '~/types/api'

export interface Props {
  job: JobPayload | null
  imageUrl: string
  saved: boolean
}

export interface Emits {
  use: []
  save: []
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()
const open = defineModel<boolean>('open', { required: true })

const api = useApi()
const p = computed(() => props.job?.params)

// multi-output kinds (pose): the primary is `imageUrl`, extractions come from
// the payload's images map and get their own tabs
const role = ref('source')
watch(() => props.job?.id, () => {
  role.value = 'source'
})

const roleOptions = computed(() => {
  const extra = Object.keys(props.job?.images ?? {})

  if (!extra.length) return []

  return [
    { value: 'source', label: 'Source' },
    ...extra.map(r => ({ value: r, label: r[0]!.toUpperCase() + r.slice(1) })),
  ]
})
const shownUrl = computed(() => {
  const path = props.job?.images?.[role.value as keyof NonNullable<JobPayload['images']>]

  return role.value !== 'source' && path ? api.imageUrl(path) : props.imageUrl
})

// displayed from the stored structure (weights shown as emitted); legacy
// rows without it fall back to the comma split
const promptTags = computed(() => {
  const v = p.value
  if (v?.prompt_tags) return v.prompt_tags.map(t => tagLabel(t))

  return (v?.prompt ?? '').split(',').map(t => t.trim()).filter(Boolean)
})
const negativeText = computed(() => {
  const v = p.value
  if (v?.negative_tags) return v.negative_tags.map(t => tagLabel(t)).join(', ')

  return v?.negative ?? ''
})

// labelled config rows, omitting anything the generation didn't use
const rows = computed(() => {
  const v = p.value

  if (!v) return []

  const kind = props.job?.kind
  const out: { label: string, value: string }[] = [
    { label: 'Size', value: `${v.width} × ${v.height}${v.hires ? ` · ${(v.hires_scale ?? 1.5) >= 2 ? 'Ultra' : 'Large'} refine` : ''}` },
    { label: 'Seed', value: String(v.seed) },
    { label: 'Steps', value: String(v.steps) },
    { label: 'Guidance', value: String(v.cfg) },
  ]

  if (v.cfg_rescale) out.push({ label: 'CFG rescale', value: String(v.cfg_rescale) })

  out.push(
    { label: 'Sampler', value: v.sampler },
    { label: 'Schedule', value: v.scheduler },
    { label: 'Lineage', value: v.lineage === 'noobai' ? 'NoobAI · v-pred' : 'Illustrious · eps' },
  )

  if (v.style) out.push({ label: 'Style', value: v.style })

  if (v.checkpoint) out.push({ label: 'Base model', value: stripModelExt(v.checkpoint) })

  if (kind && kind !== 'txt2img') out.push({ label: 'Mode', value: kind })

  if (v.denoise != null) out.push({ label: 'Denoise', value: String(v.denoise) })

  for (const l of v.loras ?? []) out.push({ label: 'LoRA', value: `${stripModelExt(l.name)} · ${l.strength ?? 0.8}` })

  if (v.reference_id) out.push({ label: 'Reference', value: `${v.ref_mode ?? 'character_style'} · str ${v.ref_strength ?? 0.8} · fid ${v.ref_fidelity ?? 0.5}` })

  return out
})

// network/CORS failure is swallowed — the browser tab fallback stays available
function download() {
  const suffix = role.value !== 'source' ? `-${role.value}` : ''
  downloadImage(shownUrl.value, `pulsar-${p.value?.seed ?? 'image'}${suffix}.png`)
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="o-anim fixed inset-0 z-40 bg-void/80" />
      <DialogContent
        class="pop-anim fixed left-1/2 top-1/2 z-50 flex h-[min(760px,92vh)] w-[min(1060px,94vw)] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-canvas border border-line bg-surface shadow-[0_24px_60px_rgba(0,0,0,.6)] focus:outline-none md:flex-row"
        aria-describedby="hist-desc"
      >
        <DialogTitle class="sr-only">
          Generation detail
        </DialogTitle>
        <DialogDescription
          id="hist-desc"
          class="sr-only"
        >
          The generated image with a download button, and the full configuration used to produce it.
        </DialogDescription>

        <!-- image -->
        <div class="relative flex min-h-0 flex-1 items-center justify-center bg-void p-4">
          <img
            v-if="shownUrl"
            :src="shownUrl"
            alt="generation"
            class="max-h-full max-w-full rounded-lg object-contain"
            :class="{ 'bg-white': role !== 'source' }"
          >
          <BaseSegment
            v-if="roleOptions.length"
            v-model="role"
            class="absolute bottom-6 left-6 w-auto"
            :options="roleOptions"
          />
          <BaseButton
            class="absolute bottom-6 right-6 bg-surface/90 backdrop-blur"
            variant="outline"
            icon="lucide:download"
            @click="download"
          >
            Download
          </BaseButton>
        </div>

        <!-- config -->
        <div class="flex min-h-0 flex-none flex-col border-line md:w-[340px] md:border-l">
          <header class="flex items-center gap-2 border-b border-line px-4 py-3">
            <span class="section-label flex-1">Configuration</span>
            <DialogClose as-child>
              <BaseButton
                size="small"
                variant="clear"
                icon="lucide:x"
                aria-label="Close"
              />
            </DialogClose>
          </header>

          <div class="min-h-0 flex-1 overflow-y-auto px-4 py-3">
            <div class="section-label mb-2">
              Prompt
            </div>
            <div class="flex flex-wrap gap-1.5">
              <span
                v-for="(t, i) in promptTags"
                :key="i"
                class="rounded-tag border border-line bg-void px-2 py-0.5 font-mono text-[11px] text-dim"
              >{{ t.replaceAll('_', ' ') }}</span>
            </div>

            <template v-if="negativeText.trim()">
              <div class="section-label mb-1.5 mt-4">
                Negative
              </div>
              <div class="rounded-ctrl border border-line bg-void px-2.5 py-2 font-mono text-[10.5px] leading-relaxed text-faint">
                {{ negativeText }}
              </div>
            </template>

            <div class="section-label mb-1.5 mt-4">
              Settings
            </div>
            <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5">
              <template
                v-for="r in rows"
                :key="r.label + r.value"
              >
                <dt class="font-mono text-[11px] text-faint">
                  {{ r.label }}
                </dt>
                <dd class="break-words text-right font-mono text-[11px] text-dim">
                  {{ r.value }}
                </dd>
              </template>
            </dl>
          </div>

          <footer class="flex-none border-t border-line p-3">
            <div class="flex gap-2">
              <BaseButton
                type="primary"
                class="flex-1"
                @click="emit('use')"
              >
                Use related config
              </BaseButton>
              <BaseButton
                v-if="saved"
                variant="outline"
                disabled
                icon="lucide:check"
              >
                Saved
              </BaseButton>
              <BaseButton
                v-else
                variant="outline"
                icon="lucide:bookmark-plus"
                title="Save this configuration as a preset"
                @click="emit('save')"
              >
                Save
              </BaseButton>
            </div>
          </footer>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
