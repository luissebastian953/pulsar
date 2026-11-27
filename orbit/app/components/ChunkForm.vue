<script setup lang="ts">
import { ColorSwatchPickerItem, ColorSwatchPickerRoot } from 'reka-ui'
import { computed, ref } from 'vue'
import BaseButton from '~/components/base/Button.vue'
import BaseDropdown from '~/components/base/Dropdown.vue'
import BaseInput from '~/components/base/Input.vue'
import PromptIssues from '~/components/PromptIssues.vue'
import { useChunkApi } from '~/composables/useChunkApi'
import { useChunks } from '~/composables/useChunks'
import { useToast } from '~/composables/useToast'
import type { PromptIssue } from '~/types/api'
import {
  CHUNK_COLORS, CHUNK_COLOR_NAMES, DEFAULT_CHUNK_COLOR, MAX_CHUNK_NAME,
  chunkContentError, chunkNameError, sortedCategories,
  type ChunkCategory, type ChunkColor, type PromptChunk,
} from '~/utils/chunks'

// Add / edit a prompt chunk or a category (docs/frontend.md#prompt-chunks).
// A chunk: name, content, category, colour. A category: name, colour.
// Colour is one of the eight palette tokens (Reka ColorSwatchPicker) — no
// free hex. Saving a chunk first asks core to parse its content; what core
// rejects is shown located in the content and blocks the save.
export interface Props {
  kind: 'chunk' | 'category'
  /** the record being edited; absent = adding */
  chunk?: PromptChunk
  category?: ChunkCategory
}

export interface Emits {
  done: []
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const store = useChunks()
const chunkApi = useChunkApi()
const toast = useToast()

const editing = computed(() => (props.kind === 'chunk' ? props.chunk : props.category))
const name = ref(editing.value?.name ?? '')
const content = ref(props.chunk?.content ?? '')
const categoryId = ref(props.chunk?.categoryId ?? '')
const color = ref<string>(editing.value?.color ?? DEFAULT_CHUNK_COLOR)
const error = ref('')
const issues = ref<PromptIssue[]>([])
const saving = ref(false)

const categoryOptions = computed(() => [
  { value: '', label: 'Uncategorized' },
  ...sortedCategories(store.state.value).map(c => ({ value: c.id, label: c.name })),
])

function clearIssues() {
  error.value = ''
  issues.value = []
}

async function save() {
  clearIssues()
  const n = name.value.trim()

  if (props.kind === 'category') {
    const res = store.saveCategory(props.category?.id ?? null, n, color.value)

    if (!res.ok) error.value = res.error
    else emit('done')

    return
  }

  // local rules first (D4), so an invalid name never goes to core
  const taken = store.chunks.value.filter(c => c.id !== props.chunk?.id).map(c => c.name)
  error.value = chunkNameError(n, taken) || chunkContentError(content.value)

  if (error.value) return

  saving.value = true
  const checked = await chunkApi.validate(n, content.value.trim(), props.chunk?.id)
  saving.value = false

  if (checked?.errors.length) {
    issues.value = checked.errors

    return
  }

  const input = { name: n, content: content.value, color: color.value, categoryId: categoryId.value }
  const res = props.chunk ? store.update(props.chunk.id, input) : store.add(input)

  if (!res.ok) {
    error.value = res.error

    return
  }

  if (checked?.ucNote) toast.show(`"${n}" saved. ${checked.ucNote}`, 'warn', 7000)

  emit('done')
}

async function remove() {
  if (props.kind === 'category' && props.category) store.deleteCategory(props.category.id)
  // references are baked into the prompts first, so nothing dangles
  else if (props.chunk) await store.remove([props.chunk.id], chunkApi.parser)

  emit('done')
}
</script>

<template>
  <form
    class="flex flex-col gap-3"
    data-test="chunk-form"
    @submit.prevent="save"
  >
    <BaseInput
      v-model="name"
      label="Name"
      :maxlength="MAX_CHUNK_NAME"
      :placeholder="kind === 'chunk' ? 'e.g. Silver Wolf' : 'e.g. Characters'"
      data-test="chunk-name"
      @input="clearIssues"
    />
    <div v-if="kind === 'chunk'">
      <BaseInput
        v-model="content"
        type="textarea"
        variant="mono"
        size="small"
        label="Content"
        placeholder="silver hair, {grey eyes}, 1.2::drill hair ::"
        :invalid="issues.length > 0"
        :highlights="issues"
        data-test="chunk-content"
        @input="clearIssues"
      />
      <PromptIssues :errors="issues" />
      <div class="mt-1.5 text-2xs leading-relaxed text-faint">
        Tags, with any weighting. Nest another chunk with <span class="font-mono text-dim">!macro:Name!</span>.
      </div>
    </div>
    <div v-if="kind === 'chunk'">
      <div class="mb-[5px] text-xs text-dim">
        Category
      </div>
      <BaseDropdown
        v-model="categoryId"
        :options="categoryOptions"
        aria-label="Category"
      />
    </div>
    <div>
      <div class="mb-[5px] text-xs text-dim">
        Colour
      </div>
      <ColorSwatchPickerRoot
        v-model="color"
        class="flex flex-wrap gap-1.5"
        aria-label="Colour"
        data-test="chunk-colors"
      >
        <ColorSwatchPickerItem
          v-for="c in CHUNK_COLORS"
          :key="c"
          :value="c"
          :aria-label="CHUNK_COLOR_NAMES[c as ChunkColor]"
          :title="CHUNK_COLOR_NAMES[c as ChunkColor]"
          class="chunk-color focus-ring flex size-6 cursor-pointer items-center justify-center rounded-full border border-transparent outline-none data-[state=checked]:border-text data-highlighted:border-line-hi"
        >
          <span class="chunk-dot size-4 rounded-full" />
        </ColorSwatchPickerItem>
      </ColorSwatchPickerRoot>
    </div>
    <div
      v-if="error"
      role="alert"
      data-test="chunk-form-error"
      class="text-[11.5px] font-semibold text-danger"
    >
      {{ error }}
    </div>
    <div class="flex items-center gap-2">
      <BaseButton
        v-if="editing"
        size="small"
        variant="outline"
        type="warn"
        icon="lucide:trash-2"
        data-test="chunk-delete"
        @click="remove"
      >
        Delete
      </BaseButton>
      <span class="flex-1" />
      <BaseButton
        size="small"
        variant="outline"
        @click="emit('done')"
      >
        Cancel
      </BaseButton>
      <BaseButton
        size="small"
        type="primary"
        native-type="submit"
        :loading="saving"
        data-test="chunk-save"
      >
        Save
      </BaseButton>
    </div>
  </form>
</template>
