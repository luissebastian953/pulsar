<script setup lang="ts">
// Style preset cards for the composer — the catalogue shape (name, family,
// one-line description, LoRA note). A preset whose LoRA isn't installed is
// shown disabled rather than hidden, so the gap is visible.
import type { StylePreset } from '~/types/api'
import { stripModelExt } from '~/utils/format'

export interface Props {
  styles: StylePreset[]
}
defineProps<Props>()
const model = defineModel<string>({ required: true })

const card = 'focus-ring flex min-h-[56px] cursor-pointer flex-col rounded-ctrl border px-2.5 py-2 text-left transition-colors'
const idle = 'border-line bg-raised hover:border-line-hi'
</script>

<template>
  <div class="grid grid-cols-2 gap-2">
    <button
      type="button"
      data-test="style-none"
      :class="[card, model === '' ? 'pill-on' : idle]"
      @click="model = ''"
    >
      <span class="text-[12.5px] text-text">None</span>
      <span class="mt-0.5 text-[10.5px] leading-snug text-dim">Your tags only, lineage defaults.</span>
    </button>
    <button
      v-for="s in styles"
      :key="s.slug"
      type="button"
      :data-test="`style-${s.slug}`"
      :disabled="!s.available"
      :aria-pressed="model === s.slug"
      :title="s.available ? s.description : `LoRA not installed: ${s.lora}`"
      :class="[card, model === s.slug ? 'pill-on' : idle, !s.available && 'cursor-not-allowed opacity-50']"
      @click="model = s.slug"
    >
      <span class="flex items-baseline justify-between gap-1.5">
        <span class="text-[12.5px] leading-tight text-text">{{ s.name }}</span>
        <span class="flex-none text-2xs uppercase tracking-wider text-faint">{{ s.family }}</span>
      </span>
      <span class="mt-0.5 line-clamp-2 text-[10.5px] leading-snug text-dim">{{ s.description }}</span>
      <span
        v-if="s.lora"
        class="mt-1 truncate font-mono text-2xs"
        :class="s.available ? 'text-faint' : 'text-warn'"
      >{{ s.available ? `+ ${stripModelExt(s.lora)} · ${s.lora_strength ?? 0.8}` : `LoRA missing · ${stripModelExt(s.lora)}` }}</span>
    </button>
  </div>
</template>
