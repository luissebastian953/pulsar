<script setup lang="ts">
defineProps<{ index: number }>()
const emit = defineEmits<{ remove: [] }>()
const enabled = defineModel<boolean>('enabled', { required: true })
const text = defineModel<string>('text', { required: true })
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
  </div>
</template>
