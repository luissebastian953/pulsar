<script setup lang="ts">
import { DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { computed, reactive, ref, watch } from 'vue'
import BaseButton from '~/components/base/Button.vue'
import BaseSegment from '~/components/base/Segment.vue'
import BaseToggleSwitch from '~/components/base/ToggleSwitch.vue'
import { useMetaImport } from '~/composables/useMetaImport'
import { usePendingImport } from '~/composables/usePresets'
import { useToast } from '~/composables/useToast'
import { escapeControlChars } from '~/utils/imgmeta/text'
import type { ImageMeta } from '~/utils/imgmeta/types'
import { DEFAULT_IMPORT_OPTIONS, importSources, snapAspect, type ImportOptions, type MetaImport } from '~/utils/meta-import'
import { ERROR_MESSAGES, GENERATOR_LABELS } from '~/utils/meta-labels'

// "What do you want to do with this image?" — the one import dialog, opened
// from /inspect and by dropping a file onto /create
// (docs/frontend.md#image-inspector, decisions D22 and D23).
//
// Every string shown here comes from the file and is rendered as text. The
// result is handed to the composer (usePendingImport): prompts land in the
// normal editor, and an image goes through the normal upload path there.
export interface Props {
  /** The inspected file — what "use as base image / reference" uploads. */
  file: File | null
  meta: ImageMeta | null
  /** The file is still being read. */
  pending?: boolean
  /** Reading failed (an ImgMetaErrorCode), or '' */
  error?: string
}

const props = withDefaults(defineProps<Props>(), { pending: false, error: '' })
const open = defineModel<boolean>('open', { required: true })

const metaImport = useMetaImport()
const pendingImport = usePendingImport()
const toast = useToast()

const options = reactive<ImportOptions>({ ...DEFAULT_IMPORT_OPTIONS })
const imageUse = ref<'none' | 'base' | 'reference'>('none')
const busy = ref(false)

// a fresh dialog for every file: the defaults, not the last choice
watch([open, () => props.file], () => {
  Object.assign(options, DEFAULT_IMPORT_OPTIONS)
  imageUse.value = 'none'
  busy.value = false
})

const params = computed(() => props.meta?.params ?? null)
const src = computed(() => (params.value ? importSources(params.value) : null))
const show = (s: string) => escapeControlChars(s)

const sizeLine = computed(() => {
  const p = params.value

  if (!p) return ''

  const aspect = snapAspect(p.width ?? 0, p.height ?? 0)
  const parts = [
    aspect ? `${p.width} × ${p.height} → ${aspect}` : '',
    typeof p.steps === 'number' ? `${p.steps} steps` : '',
  ].filter(Boolean)

  return parts.join(' · ')
})

interface Row { key: keyof ImportOptions, label: string, preview: string, test: string }

const rows = computed<Row[]>(() => {
  const s = src.value
  const p = params.value

  if (!s || !p) return []

  const out: Row[] = []

  if (s.prompt.trim()) out.push({ key: 'prompt', label: 'Prompt', preview: s.prompt, test: 'import-prompt' })

  if (s.negative.trim()) out.push({ key: 'negative', label: 'Undesired Content', preview: s.negative, test: 'import-negative' })

  if (s.characters.length) {
    out.push({
      key: 'characters',
      label: `Characters (${s.characters.length})`,
      preview: s.characters.map(c => c.prompt).join('  |  '),
      test: 'import-characters',
    })
  }

  if (sizeLine.value) out.push({ key: 'settings', label: 'Settings', preview: sizeLine.value, test: 'import-settings' })

  if (p.seed !== undefined) out.push({ key: 'seed', label: 'Seed', preview: String(p.seed), test: 'import-seed' })

  return out
})

const title = computed(() => {
  const g = props.meta?.generator

  return g ? `Made with ${GENERATOR_LABELS[g]}` : 'No generation metadata'
})

const errorText = computed(() =>
  props.error ? (ERROR_MESSAGES[props.error as keyof typeof ERROR_MESSAGES] ?? 'That file could not be read.') : '')

const nothingChosen = computed(() =>
  imageUse.value === 'none' && !rows.value.some(r => options[r.key]))

async function submit() {
  if (busy.value || nothingChosen.value) return

  busy.value = true

  try {
    const patch: MetaImport = {}
    let notices: string[] = []
    const p = params.value

    if (p && rows.value.some(r => options[r.key])) {
      // only what is both offered and switched on
      const chosen = { ...options }

      for (const key of Object.keys(chosen) as (keyof ImportOptions)[]) {
        if (!rows.value.some(r => r.key === key)) chosen[key] = false
      }

      const res = await metaImport.build(p, chosen)

      Object.assign(patch, res.patch)
      notices = res.notices
    }

    if (props.file && imageUse.value === 'base') patch.baseImage = props.file

    if (props.file && imageUse.value === 'reference') patch.reference = props.file

    pendingImport.value = patch
    open.value = false

    notices.slice(0, 4).forEach(n => toast.show(n, 'info', 9000))

    if (notices.length > 4) toast.show(`…and ${notices.length - 4} more import notes.`, 'info', 9000)

    await navigateTo('/create')
  }
  finally {
    busy.value = false
  }
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="o-anim fixed inset-0 z-40 bg-void/80" />
      <DialogContent
        class="pop-anim fixed inset-0 z-50 flex items-center justify-center p-4 focus:outline-none"
        aria-describedby="meta-import-desc"
        data-test="meta-import"
        @click.self="open = false"
      >
        <div class="flex max-h-full w-full max-w-md flex-col overflow-hidden rounded-canvas border border-line bg-raised shadow-2xl">
          <div class="flex-none border-b border-line px-5 py-4">
            <DialogTitle class="text-sm font-semibold text-text">
              What do you want to do with this image?
            </DialogTitle>
            <DialogDescription
              id="meta-import-desc"
              class="mt-1 text-xs leading-relaxed text-dim"
            >
              <template v-if="pending">
                Reading the file…
              </template>
              <template v-else-if="errorText">
                {{ errorText }}
              </template>
              <template v-else>
                <span data-test="import-generator">{{ title }}</span><span v-if="params?.model"> · {{ show(params.model) }}</span>.
                Imported text goes into the editor like anything you type.
              </template>
            </DialogDescription>
          </div>

          <div
            v-if="!pending"
            class="min-h-0 flex-1 overflow-y-auto px-5 py-4"
          >
            <div
              v-if="rows.length"
              class="flex flex-col gap-3"
            >
              <label
                v-for="r in rows"
                :key="r.key"
                class="flex cursor-pointer items-start gap-3"
              >
                <span class="min-w-0 flex-1">
                  <span class="block text-xs font-medium text-text">{{ r.label }}</span>
                  <span
                    class="line-clamp-2 break-words font-mono text-2xs leading-relaxed text-faint"
                    :data-test="`${r.test}-preview`"
                  >{{ show(r.preview) }}</span>
                </span>
                <BaseToggleSwitch
                  v-model="options[r.key]"
                  class="mt-0.5"
                  :data-test="r.test"
                />
              </label>
              <div class="text-2xs leading-relaxed text-faint">
                Settings means size and steps only. Guidance, sampler, schedule and rescale never
                carry over — they don't transfer between models.
              </div>
            </div>
            <div
              v-else-if="!errorText"
              class="text-xs leading-relaxed text-dim"
            >
              There is no prompt to import, but the image itself can still be used.
            </div>

            <template v-if="file && !errorText">
              <div class="section-label mb-2 mt-4">
                Use the image
              </div>
              <BaseSegment
                v-model="imageUse"
                data-test="import-image-use"
                :options="[
                  { value: 'none', label: 'Don\'t use' },
                  { value: 'base', label: 'Base image' },
                  { value: 'reference', label: 'Reference' },
                ]"
              />
              <div class="mt-1.5 text-2xs leading-relaxed text-faint">
                The image is uploaded and checked like any other upload; its metadata is not kept.
              </div>
            </template>
          </div>

          <div class="flex flex-none justify-end gap-2 border-t border-line px-5 py-3">
            <BaseButton
              variant="outline"
              data-test="import-cancel"
              @click="open = false"
            >
              Cancel
            </BaseButton>
            <BaseButton
              type="primary"
              icon="lucide:import"
              :loading="busy"
              :disabled="pending || !!errorText || nothingChosen"
              data-test="import-submit"
              @click="submit"
            >
              Import
            </BaseButton>
          </div>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
