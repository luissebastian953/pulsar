<script setup lang="ts">
import type { UploadResult } from '~/types/api'

defineProps<{ index: number }>()
const emit = defineEmits<{ remove: [] }>()
const enabled = defineModel<boolean>('enabled', { required: true })
const text = defineModel<string>('text', { required: true })
// per-character Precise Reference (both cards need one for it to apply)
const refId = defineModel<string>('refId', { required: true })

const api = useApi()
const refError = ref('')
const refPreview = computed(() => refId.value ? api.imageUrl(`/images/${refId.value}`) : '')

async function onRefPick(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  refError.value = ''
  try {
    const up = await api.upload<UploadResult>('/uploads', file)
    refId.value = up.image_id
  }
  catch (err: unknown) {
    const ex = err as { data?: { error?: string } }
    refError.value = ex.data?.error || 'upload failed'
  }
}
</script>

<template>
  <div class="rounded-ctrl border border-line bg-raised p-2.5">
    <div class="mb-[7px] flex items-center gap-2">
      <ToggleSwitch
        v-model="enabled"
        :title="enabled ? 'On — included in generation' : 'Off — kept, not sent'"
      />
      <span class="text-[10.5px] text-dim">Character {{ index + 1 }}</span>
      <span v-if="!enabled" class="text-2xs text-faint">off</span>
      <span class="flex-1" />
      <button
        class="focus-ring cursor-pointer px-0.5 text-faint hover:text-t-artist"
        title="Remove character"
        @click="emit('remove')"
      >
        <Icon name="lucide:x" :size="12" />
      </button>
    </div>
    <textarea
      v-model="text"
      placeholder="her own prompt…"
      class="block min-h-14 w-full resize-y rounded-ctrl border border-line bg-void px-[11px] py-2 text-[13px] leading-normal text-text outline-none placeholder:text-faint focus:border-line-hi"
      :class="!enabled && 'opacity-45'"
    />
    <div class="mt-2 flex items-center gap-2" :class="!enabled && 'opacity-45'">
      <template v-if="refId">
        <img :src="refPreview" alt="reference" class="size-8 flex-none rounded-md object-cover">
        <span class="flex-1 truncate font-mono text-[10.5px] text-faint">own reference attached</span>
        <button class="focus-ring cursor-pointer text-faint hover:text-t-artist" aria-label="remove reference" @click="refId = ''">
          <Icon name="lucide:x" :size="11" />
        </button>
      </template>
      <label v-else class="focus-ring flex cursor-pointer items-center gap-1.5 text-[10.5px] text-faint hover:text-dim">
        <Icon name="lucide:image-plus" :size="11" />own reference
        <input type="file" accept="image/png,image/jpeg" class="hidden" @change="onRefPick">
      </label>
      <span v-if="refError" class="text-[10px] text-warn">{{ refError }}</span>
    </div>
  </div>
</template>
