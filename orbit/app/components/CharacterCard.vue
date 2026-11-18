<script setup lang="ts">
import type { PromptIssue, UploadResult } from '~/types/api'

export interface Props {
  index: number
  /** lineage for live validation (calibration notices are per lineage) */
  lineage?: string
  /** core's /generate 400 for this character, while its text is unchanged */
  serverErrors?: PromptIssue[]
}

export interface Emits {
  remove: []
  /** whether this card's prompt currently has an error (fold label) */
  invalid: [value: boolean]
}

const props = withDefaults(defineProps<Props>(), { lineage: '', serverErrors: () => [] })
const emit = defineEmits<Emits>()
const enabled = defineModel<boolean>('enabled', { required: true })
const text = defineModel<string>('text', { required: true })

// each character prompt is its own surface (weights reset per box)
const live = usePromptParse(() => text.value, 'character', () => props.lineage)
const errors = computed(() => (live.errors.value.length ? live.errors.value : props.serverErrors))
watch(() => errors.value.length > 0, v => emit('invalid', v), { immediate: true })
// per-character Precise Reference (both cards need one for it to apply)
const refId = defineModel<string>('refId', { required: true })

const api = useApi()
const refError = ref('')
const refPreview = computed(() => refId.value ? api.imageUrl(`/images/${refId.value}`) : '')

// refId is the parent's model, so this stays a direct upload rather than a
// useImageUpload slot (whose id lives in the slot, not the parent)
async function onRefPick(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]

  if (!file) return

  refError.value = ''

  try {
    const up = await api.upload<UploadResult>('/uploads', file)
    refId.value = up.image_id
  }
  catch (err: unknown) {
    refError.value = apiErrorMessage(err, 'upload failed')
  }
}
</script>

<template>
  <div class="rounded-ctrl border border-line bg-raised p-2.5">
    <div class="mb-[7px] flex items-center gap-2">
      <BaseToggleSwitch
        v-model="enabled"
        :title="enabled ? 'On — included in generation' : 'Off — kept, not sent'"
      />
      <span
        class="text-[10.5px]"
        :class="errors.length ? 'font-bold text-danger' : 'text-dim'"
        data-test="character-label"
      >Character {{ index + 1 }}</span>
      <span
        v-if="!enabled"
        class="text-2xs text-faint"
      >off</span>
      <span class="flex-1" />
      <BaseButton
        size="small"
        variant="clear"
        type="warn"
        icon="lucide:x"
        title="Remove character"
        @click="emit('remove')"
      />
    </div>
    <BaseInput
      v-model="text"
      type="textarea"
      size="small"
      placeholder="her own prompt…"
      :invalid="errors.length > 0"
      :highlights="errors"
      :class="!enabled && 'opacity-45'"
    />
    <PromptIssues
      :errors="errors"
      :notices="live.notices.value"
    />
    <div
      class="mt-2 flex items-center gap-2"
      :class="!enabled && 'opacity-45'"
    >
      <template v-if="refId">
        <img
          :src="refPreview"
          alt="reference"
          class="size-8 flex-none rounded-md object-cover"
        >
        <span class="flex-1 truncate font-mono text-[10.5px] text-faint">own reference attached</span>
        <BaseButton
          size="small"
          variant="clear"
          type="warn"
          icon="lucide:x"
          aria-label="remove reference"
          @click="refId = ''"
        />
      </template>
      <label
        v-else
        class="focus-ring flex cursor-pointer items-center gap-1.5 text-[10.5px] text-faint hover:text-dim"
      >
        <Icon
          name="lucide:image-plus"
          :size="11"
        />own reference
        <input
          type="file"
          accept="image/png,image/jpeg"
          class="hidden"
          @change="onRefPick"
        >
      </label>
      <span
        v-if="refError"
        class="text-[10px] text-warn"
      >{{ refError }}</span>
    </div>
  </div>
</template>
