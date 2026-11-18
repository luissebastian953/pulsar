<script setup lang="ts">
import type { ImageMeta } from '~/utils/imgmeta/types'
import { ERROR_MESSAGES } from '~/utils/meta-labels'

// Image inspector (docs/frontend.md#image-inspector): drop, pick or paste a
// PNG, JPEG or WebP and read the generation metadata it carries.
//
// The file never leaves the browser here: it is parsed in a Web Worker
// (useImageMeta), nothing is uploaded, and no metadata value is ever used as
// a URL. Uploading happens only if the user imports the image as a base or
// reference, through the normal upload path on /create.
const ACCEPT = ['image/png', 'image/jpeg', 'image/webp']

const { meta, error, pending, inspect } = useImageMeta()

const file = shallowRef<File | null>(null)
const preview = ref('')
// Simplified / Raw, remembered per browser
const rawView = useLocalStorage('pulsar.inspect.raw', false)
const view = computed({
  get: () => (rawView.value ? 'raw' : 'simplified'),
  set: (v: string | number) => { rawView.value = v === 'raw' },
})
const importOpen = ref(false)

function setPreview(f: File | null) {
  if (preview.value) URL.revokeObjectURL(preview.value)

  preview.value = f ? URL.createObjectURL(f) : ''
}

async function load(f: File | null | undefined) {
  if (!f) return

  file.value = f
  setPreview(f)
  await inspect(f)
}

// drop anywhere on the page; first file only
const zone = useTemplateRef<HTMLElement>('zone')
const { isOverDropZone } = useDropZone(zone, {
  onDrop: files => load(files?.[0]),
})

const { open: pick, onChange } = useFileDialog({ accept: ACCEPT.join(','), multiple: false, reset: true })
onChange(files => load(files?.[0]))

// paste an image (or a copied file) from the clipboard
useEventListener(document, 'paste', (e: ClipboardEvent) => {
  const f = [...(e.clipboardData?.files ?? [])].find(x => x.type.startsWith('image/'))

  if (f) {
    e.preventDefault()
    load(f)
  }
})

onBeforeUnmount(() => setPreview(null))

const errorText = computed(() => (error.value ? ERROR_MESSAGES[error.value] : ''))
const shown = computed<ImageMeta | null>(() => meta.value)
const fileLine = computed(() => {
  const f = file.value
  const m = shown.value

  if (!f) return ''

  const size = f.size >= 1 << 20 ? `${(f.size / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(f.size / 1024))} KB`

  return [m ? m.format.toUpperCase() : '', m && m.width ? `${m.width} × ${m.height}` : '', size].filter(Boolean).join(' · ')
})
</script>

<template>
  <main
    ref="zone"
    class="relative flex min-h-0 flex-1 flex-col overflow-y-auto md:flex-row md:overflow-hidden"
    data-test="inspect-page"
  >
    <!-- image -->
    <section class="flex flex-none flex-col items-center justify-center gap-3 p-4 md:min-h-0 md:flex-1 md:p-8">
      <button
        v-if="!file"
        class="focus-ring flex w-full max-w-md cursor-pointer flex-col items-center gap-3 rounded-canvas border border-dashed border-line-hi px-6 py-12 text-center hover:border-dim"
        data-test="inspect-pick"
        @click="pick()"
      >
        <Icon
          name="lucide:scan-search"
          :size="28"
          class="text-dim"
        />
        <span class="text-sm text-text">Drop an image here, paste one, or click to choose</span>
        <span class="text-xs leading-relaxed text-faint">
          PNG, JPEG or WebP. Reads NovelAI, A1111 / Forge, ComfyUI, SwarmUI, InvokeAI and pulsar metadata.
          The file is read in your browser and is not uploaded.
        </span>
      </button>
      <template v-else>
        <img
          :src="preview"
          alt="inspected image"
          class="max-h-64 max-w-full rounded-lg object-contain md:max-h-full md:min-h-0"
          data-test="inspect-preview"
        >
        <div class="flex flex-none flex-wrap items-center justify-center gap-2">
          <span class="font-mono text-2xs text-faint">{{ fileLine }}</span>
          <BaseButton
            size="small"
            variant="outline"
            icon="lucide:image-up"
            @click="pick()"
          >
            Another image
          </BaseButton>
        </div>
      </template>
    </section>

    <!-- metadata -->
    <aside class="flex flex-none flex-col border-line bg-surface md:h-full md:min-h-0 md:w-1/2 md:max-w-xl md:border-l">
      <header class="flex flex-none items-center gap-2 border-b border-line px-4 py-3">
        <span class="section-label flex-1">Inspect</span>
        <BaseSegment
          v-model="view"
          class="flex-none"
          data-test="inspect-view"
          :options="[{ value: 'simplified', label: 'Simplified' }, { value: 'raw', label: 'Raw' }]"
        />
      </header>

      <div class="min-h-0 flex-1 p-4 md:overflow-y-auto">
        <div
          v-if="pending"
          class="text-xs text-dim"
          data-test="inspect-pending"
        >
          Reading metadata…
        </div>
        <div
          v-else-if="errorText"
          class="rounded-ctrl border border-danger/40 px-3 py-2.5 text-xs leading-relaxed text-danger"
          data-test="inspect-error"
        >
          {{ errorText }}
        </div>
        <MetaView
          v-else-if="shown"
          :meta="shown"
          :raw="rawView"
        />
        <div
          v-else
          class="text-xs leading-relaxed text-faint"
        >
          Nothing inspected yet. Metadata shown here is exactly what the file says — it can be edited by anyone,
          so treat it as a claim, not a fact.
        </div>
      </div>

      <div
        v-if="file && !pending"
        class="sticky bottom-0 flex flex-none border-t border-line bg-surface px-4 py-3 md:static"
      >
        <BaseButton
          type="primary"
          class="w-full"
          icon="lucide:import"
          :disabled="!!errorText"
          data-test="inspect-import"
          @click="importOpen = true"
        >
          Import to Create
        </BaseButton>
      </div>
    </aside>

    <!-- drop overlay -->
    <div
      v-if="isOverDropZone"
      class="pointer-events-none absolute inset-0 z-30 flex items-center justify-center border-2 border-dashed border-accent bg-void/80 text-sm text-text"
    >
      Drop to inspect
    </div>

    <MetaImportDialog
      v-model:open="importOpen"
      :file="file"
      :meta="shown"
      :error="error"
    />
  </main>
</template>
