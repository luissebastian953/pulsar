<script setup lang="ts">
import { useLocalStorage } from '@vueuse/core'
import {
  AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogOverlay, AlertDialogPortal,
  AlertDialogRoot, AlertDialogTitle, TabsContent, TabsList, TabsRoot, TabsTrigger,
} from 'reka-ui'
import { computed, ref } from 'vue'
import BaseButton from '~/components/base/Button.vue'
import BaseToggleSwitch from '~/components/base/ToggleSwitch.vue'
import ChunkForm from '~/components/ChunkForm.vue'
import { useChunkApi } from '~/composables/useChunkApi'
import { useChunks } from '~/composables/useChunks'
import { usePromptSettings } from '~/composables/usePromptSettings'
import { useToast } from '~/composables/useToast'
import { chunksIn, sortedCategories, type ChunkCategory, type PromptChunk } from '~/utils/chunks'

// The chunk panel's content, shared by the desktop popover and the mobile
// sheet (docs/frontend.md#prompt-chunks). Two tabs:
//   · Prompt Chunks — Uncategorized first, then the categories. Click a chip
//     to insert its token at the last-focused prompt field's caret; ✎ edits;
//     drag (or Alt+←/→ on a focused chip) reorders.
//   · Settings — "Disable Tag Suggestions" and "Highlight Emphasis".
const store = useChunks()
const settings = usePromptSettings()
const chunkApi = useChunkApi()
const toast = useToast()

// the open tab is remembered; a stored value that isn't a tab falls back
const storedTab = useLocalStorage<string>('pulsar.chunks.tab', 'chunks')
const tab = computed({
  get: () => (storedTab.value === 'settings' ? 'settings' : 'chunks'),
  set: (v: string) => { storedTab.value = v },
})

// ── add / edit ──────────────────────────────────────────────────────
type Editing = { kind: 'chunk', chunk?: PromptChunk } | { kind: 'category', category?: ChunkCategory }
const editing = ref<Editing | null>(null)
const formKey = ref(0)

function edit(e: Editing) {
  formKey.value++
  editing.value = e
}

// ── list ────────────────────────────────────────────────────────────
const uncategorized = computed(() => chunksIn(store.state.value, ''))
const groups = computed(() => sortedCategories(store.state.value).map(c => ({ category: c, chunks: chunksIn(store.state.value, c.id) })))
const empty = computed(() => !store.chunks.value.length && !store.categories.value.length)

function insert(c: PromptChunk) {
  if (!store.insertIntoActive(c.name)) toast.show('Click in a prompt field first, then click a chunk to insert it.')
}

const tip = (c: PromptChunk) => `${c.content}\n\nClick to insert, drag to reorder`

// ── drag and drop: its own MIME types, so a chip is never dropped as text ──
const CHUNK_MIME = 'application/x-pulsar-chunk'
const CATEGORY_MIME = 'application/x-pulsar-chunk-category'
const dropAt = ref('') // chunk id the dragged chip would land before

function onChunkDrag(e: DragEvent, c: PromptChunk) {
  e.dataTransfer?.setData(CHUNK_MIME, c.id)

  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}

const carries = (e: DragEvent, mime: string) => !!e.dataTransfer?.types.includes(mime)

function onChunkOver(e: DragEvent, before: string) {
  if (!carries(e, CHUNK_MIME)) return

  e.preventDefault()
  dropAt.value = before
}

function onChunkDrop(e: DragEvent, categoryId: string, index: number) {
  const id = e.dataTransfer?.getData(CHUNK_MIME)
  dropAt.value = ''

  if (!id) return

  e.preventDefault()
  e.stopPropagation()
  store.reorderChunk(id, categoryId, index)
}

function onCategoryDrag(e: DragEvent, c: ChunkCategory) {
  e.dataTransfer?.setData(CATEGORY_MIME, c.id)
}

function onCategoryDrop(e: DragEvent, index: number) {
  const id = e.dataTransfer?.getData(CATEGORY_MIME)

  if (!id) return

  e.preventDefault()
  store.reorderCategory(id, index)
}

// keyboard reorder: Alt+←/→ moves the focused chip within its category
function onChipKey(e: KeyboardEvent, c: PromptChunk, index: number) {
  if (!e.altKey || (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight')) return

  e.preventDefault()
  store.reorderChunk(c.id, c.categoryId, index + (e.key === 'ArrowLeft' ? -1 : 1))
}

// ── export / import / delete all ────────────────────────────────────
function exportAll() {
  const url = URL.createObjectURL(new Blob([store.toJSON()], { type: 'application/json' }))
  const a = document.createElement('a')
  a.href = url
  a.download = 'pulsar-chunks.json'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

async function importFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''

  if (!file) return

  const res = store.fromJSON(await file.text())

  if (!res.ok) {
    toast.show(res.error, 'warn')

    return
  }

  const parts = [`${res.added} added`, `${res.updated} updated`]

  if (res.skipped.length) parts.push(`${res.skipped.length} skipped (${res.skipped.slice(0, 3).join(', ')}${res.skipped.length > 3 ? '…' : ''})`)

  toast.show(`Chunks imported: ${parts.join(', ')}`, res.skipped.length ? 'warn' : 'info', 6000)
}

const confirmAll = ref(false)

async function deleteAll() {
  confirmAll.value = false
  // every reference is baked into the prompts first
  await store.removeAll(chunkApi.parser)
  toast.show('All prompt chunks deleted')
}

const EMPH_SAMPLES = [
  { kind: 'up', label: 'stronger', emph: 0.4 },
  { kind: 'down', label: 'weaker', emph: 0.4 },
  { kind: 'op', label: '1.5::', emph: 0.5 },
]

const suggestionsOff = computed({
  get: () => !settings.tagSuggestions.value,
  set: (off: boolean) => { settings.tagSuggestions.value = !off },
})
</script>

<template>
  <TabsRoot
    v-model="tab"
    class="flex min-h-0 flex-1 flex-col"
    data-test="chunk-panel"
  >
    <TabsList
      class="flex flex-none gap-1 border-b border-line px-2 pt-2"
      aria-label="Prompt chunk panel"
    >
      <TabsTrigger
        v-for="t in [{ value: 'chunks', label: 'Prompt Chunks' }, { value: 'settings', label: 'Settings' }]"
        :key="t.value"
        :value="t.value"
        class="focus-ring-inset cursor-pointer rounded-t-lg border-b-2 border-transparent px-3 py-2 text-[12.5px] text-dim hover:text-text data-[state=active]:border-accent data-[state=active]:text-text"
      >
        {{ t.label }}
      </TabsTrigger>
      <span class="flex-1" />
      <slot name="close" />
    </TabsList>

    <TabsContent
      value="chunks"
      class="flex min-h-0 flex-1 flex-col outline-none"
    >
      <div
        v-if="editing"
        class="min-h-0 flex-1 overflow-y-auto p-3"
      >
        <div class="section-label mb-3">
          {{ editing.kind === 'chunk' ? (editing.chunk ? 'Edit prompt chunk' : 'Add prompt chunk') : (editing.category ? 'Edit category' : 'Add category') }}
        </div>
        <ChunkForm
          :key="formKey"
          :kind="editing.kind"
          :chunk="editing.kind === 'chunk' ? editing.chunk : undefined"
          :category="editing.kind === 'category' ? editing.category : undefined"
          @done="editing = null"
        />
      </div>
      <template v-else>
        <div class="flex flex-none items-center gap-1 px-3 pt-2.5">
          <span class="flex-1 text-2xs text-faint">{{ store.chunks.value.length }} chunk{{ store.chunks.value.length === 1 ? '' : 's' }}</span>
          <BaseButton
            size="small"
            variant="clear"
            icon="lucide:folder-plus"
            title="Add category"
            aria-label="Add category"
            data-test="chunk-add-category"
            @click="edit({ kind: 'category' })"
          />
          <BaseButton
            size="small"
            variant="clear"
            icon="lucide:plus"
            title="Add prompt chunk"
            aria-label="Add prompt chunk"
            data-test="chunk-add"
            @click="edit({ kind: 'chunk' })"
          />
          <BaseButton
            size="small"
            variant="clear"
            icon="lucide:download"
            title="Export chunks as JSON"
            aria-label="Export chunks as JSON"
            :disabled="empty"
            data-test="chunk-export"
            @click="exportAll"
          />
          <label
            class="focus-ring inline-flex cursor-pointer items-center rounded-lg p-1 text-faint hover:text-text focus-within:text-text"
            title="Import chunks from JSON"
          >
            <Icon
              name="lucide:upload"
              :size="13"
            />
            <span class="sr-only">Import chunks from JSON</span>
            <input
              type="file"
              accept="application/json,.json"
              class="sr-only"
              data-test="chunk-import"
              @change="importFile"
            >
          </label>
          <BaseButton
            size="small"
            variant="clear"
            type="warn"
            icon="lucide:trash-2"
            title="Delete all prompt chunks"
            aria-label="Delete all prompt chunks"
            :disabled="empty"
            data-test="chunk-delete-all"
            @click="confirmAll = true"
          />
        </div>

        <div class="min-h-0 flex-1 overflow-y-auto p-3">
          <div
            v-if="empty"
            class="py-8 text-center text-[12.5px] leading-relaxed text-faint"
          >
            No custom prompt chunks yet. Click + to add one.
          </div>
          <template v-else>
            <!-- Uncategorized first -->
            <div
              class="flex min-h-7 flex-wrap gap-1.5"
              data-test="chunk-group"
              @dragover="onChunkOver($event, '')"
              @drop="onChunkDrop($event, '', uncategorized.length)"
            >
              <span
                v-for="(c, i) in uncategorized"
                :key="c.id"
                class="chunk-color chunk-pill inline-flex items-center rounded-tag"
                :data-color="c.color"
                :data-drop="dropAt === c.id || undefined"
                @dragover.stop="onChunkOver($event, c.id)"
                @drop="onChunkDrop($event, '', i)"
              >
                <button
                  type="button"
                  draggable="true"
                  class="focus-ring cursor-pointer rounded-tag py-1 pl-2 pr-1 font-mono text-[11.5px]"
                  :title="tip(c)"
                  data-test="chunk-chip"
                  @click="insert(c)"
                  @keydown="onChipKey($event, c, i)"
                  @dragstart="onChunkDrag($event, c)"
                  @dragend="dropAt = ''"
                >{{ c.name }}</button>
                <button
                  type="button"
                  class="focus-ring flex cursor-pointer rounded-tag py-1 pl-0.5 pr-1.5 opacity-60 hover:opacity-100"
                  :aria-label="`Edit ${c.name}`"
                  data-test="chunk-edit"
                  @click="edit({ kind: 'chunk', chunk: c })"
                >
                  <Icon
                    name="lucide:pencil"
                    :size="10"
                  />
                </button>
              </span>
            </div>

            <div
              v-for="(g, gi) in groups"
              :key="g.category.id"
              class="mt-3"
              data-test="chunk-category"
              @dragover="carries($event, CATEGORY_MIME) && $event.preventDefault()"
              @drop="onCategoryDrop($event, gi)"
            >
              <div
                class="chunk-color flex items-center gap-1.5"
                :data-color="g.category.color"
              >
                <button
                  type="button"
                  draggable="true"
                  class="focus-ring flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 rounded-md py-1 text-left"
                  :aria-expanded="!store.collapsed.value.includes(g.category.id)"
                  @click="store.toggleCollapsed(g.category.id)"
                  @dragstart="onCategoryDrag($event, g.category)"
                >
                  <Icon
                    name="lucide:chevron-right"
                    :size="12"
                    class="flex-none text-faint transition-transform duration-100"
                    :class="!store.collapsed.value.includes(g.category.id) && 'rotate-90'"
                  />
                  <span class="chunk-dot size-2 flex-none rounded-full" />
                  <span class="section-label truncate">{{ g.category.name }}</span>
                  <span class="text-2xs text-faint">{{ g.chunks.length }}</span>
                </button>
                <button
                  type="button"
                  class="focus-ring flex cursor-pointer rounded-md p-1 text-faint hover:text-text"
                  :aria-label="`Edit category ${g.category.name}`"
                  @click="edit({ kind: 'category', category: g.category })"
                >
                  <Icon
                    name="lucide:pencil"
                    :size="11"
                  />
                </button>
              </div>
              <div
                v-show="!store.collapsed.value.includes(g.category.id)"
                class="mt-1.5 flex min-h-7 flex-wrap gap-1.5 pl-[18px]"
                data-test="chunk-group"
                @dragover="onChunkOver($event, '')"
                @drop="onChunkDrop($event, g.category.id, g.chunks.length)"
              >
                <span
                  v-if="!g.chunks.length"
                  class="py-1 text-[11.5px] text-faint"
                >Empty category</span>
                <span
                  v-for="(c, i) in g.chunks"
                  :key="c.id"
                  class="chunk-color chunk-pill inline-flex items-center rounded-tag"
                  :data-color="c.color"
                  :data-drop="dropAt === c.id || undefined"
                  @dragover.stop="onChunkOver($event, c.id)"
                  @drop="onChunkDrop($event, g.category.id, i)"
                >
                  <button
                    type="button"
                    draggable="true"
                    class="focus-ring cursor-pointer rounded-tag py-1 pl-2 pr-1 font-mono text-[11.5px]"
                    :title="tip(c)"
                    data-test="chunk-chip"
                    @click="insert(c)"
                    @keydown="onChipKey($event, c, i)"
                    @dragstart="onChunkDrag($event, c)"
                    @dragend="dropAt = ''"
                  >{{ c.name }}</button>
                  <button
                    type="button"
                    class="focus-ring flex cursor-pointer rounded-tag py-1 pl-0.5 pr-1.5 opacity-60 hover:opacity-100"
                    :aria-label="`Edit ${c.name}`"
                    data-test="chunk-edit"
                    @click="edit({ kind: 'chunk', chunk: c })"
                  >
                    <Icon
                      name="lucide:pencil"
                      :size="10"
                    />
                  </button>
                </span>
              </div>
            </div>
          </template>
          <div class="mt-4 text-2xs leading-relaxed text-faint">
            Type <span class="font-mono text-dim">@</span> in a prompt to pick a chunk. Chunks stay in this browser — export to move them.
          </div>
        </div>
      </template>
    </TabsContent>

    <TabsContent
      value="settings"
      class="flex flex-col gap-3 p-3 outline-none"
    >
      <label class="flex cursor-pointer items-center gap-3">
        <span class="flex-1">
          <span class="block text-[12.5px] text-text">Disable Tag Suggestions</span>
          <span class="block text-2xs leading-relaxed text-faint">No tag autocomplete popup while typing. The @ chunk picker still works.</span>
        </span>
        <BaseToggleSwitch
          v-model="suggestionsOff"
          data-test="setting-disable-suggestions"
        />
      </label>
      <label class="flex cursor-pointer items-center gap-3">
        <span class="flex-1">
          <span class="block text-[12.5px] text-text">Highlight Emphasis</span>
          <span class="block text-2xs leading-relaxed text-faint">
            Tint weighted text:
            <span
              v-for="s in EMPH_SAMPLES"
              :key="s.kind"
              class="emph-mark mr-1 rounded-sm px-1 text-text"
              :data-kind="s.kind"
              :style="{ '--emph': s.emph }"
            >{{ s.label }}</span>
          </span>
        </span>
        <BaseToggleSwitch
          v-model="settings.highlightEmphasis.value"
          data-test="setting-highlight-emphasis"
        />
      </label>
      <div class="text-2xs leading-relaxed text-faint">
        Both settings are kept in this browser.
      </div>
    </TabsContent>

    <AlertDialogRoot v-model:open="confirmAll">
      <AlertDialogPortal>
        <AlertDialogOverlay class="o-anim fixed inset-0 z-[80] bg-void/70" />
        <AlertDialogContent class="pop-anim fixed left-1/2 top-1/2 z-[81] w-[320px] max-w-[90vw] -translate-x-1/2 -translate-y-1/2 rounded-canvas border border-line bg-raised p-5 shadow-[0_20px_50px_rgba(0,0,0,.6)] focus:outline-none">
          <AlertDialogTitle class="text-[14px] font-semibold text-text">
            Delete all prompt chunks?
          </AlertDialogTitle>
          <AlertDialogDescription class="mt-1.5 text-[12.5px] leading-relaxed text-dim">
            All {{ store.chunks.value.length }} chunks and their categories are removed. Chunks used in your prompts are written out as plain tags first. This can't be undone.
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
              data-test="chunk-delete-all-confirm"
              @click="deleteAll"
            >
              Delete all
            </BaseButton>
          </div>
        </AlertDialogContent>
      </AlertDialogPortal>
    </AlertDialogRoot>
  </TabsRoot>
</template>
