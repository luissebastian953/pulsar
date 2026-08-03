<script setup lang="ts">
import type { JobTracker } from '~/composables/useGeneration'

export interface Props {
  job: JobTracker
  width: number
  height: number
  // viewport: desktop stage, sized against the viewport height.
  // block: mobile, fills the parent width.
  fit?: 'viewport' | 'block'
}

const props = withDefaults(defineProps<Props>(), { fit: 'viewport' })

const dims = computed(() => `${props.width} × ${props.height}`)
const ratio = computed(() => props.width / props.height)

const containerStyle = computed(() => ({
  aspectRatio: `${props.width} / ${props.height}`,
  width: props.fit === 'viewport'
    ? `min(100%, calc((100vh - 116px) * ${ratio.value.toFixed(4)}))`
    : '100%',
}))

const statusText = computed(() => {
  const j = props.job
  if (j.phase.value === 'queued') {
    const ahead = Math.max(0, j.queuePos.value - 1)
    return ahead > 0 ? `Queued · ${ahead} ahead` : 'Queued'
  }

  return `Generating · ${j.step.value}/${j.total.value}`
})

const barPct = computed(() => {
  const j = props.job
  if (j.phase.value === 'queued') return '3%'

  if (!j.total.value) return '0%'

  return `${Math.round((j.step.value / j.total.value) * 100)}%`
})
</script>

<template>
  <div
    class="relative flex items-center justify-center overflow-hidden rounded-canvas border border-line bg-surface"
    :style="containerStyle"
  >
    <!-- empty -->
    <div
      v-if="job.phase.value === 'idle'"
      class="p-6 text-center"
    >
      <div class="mb-3 flex justify-center text-line-hi">
        <Icon
          name="lucide:image"
          :size="28"
        />
      </div>
      <div class="mb-2 font-mono text-[13px] text-faint">
        {{ dims }}
      </div>
      <div class="text-[13px] leading-relaxed text-dim">
        Nothing here yet. Describe something and hit Generate.
      </div>
    </div>

    <!-- result -->
    <img
      v-else-if="job.phase.value === 'done'"
      :src="job.imageUrl.value"
      :alt="`generated image, seed ${job.seed.value}`"
      class="absolute inset-0 size-full object-cover"
    >

    <!-- blocked: never delivered (docs/compliance.md) -->
    <div
      v-else-if="job.phase.value === 'blocked'"
      class="max-w-xs p-6 text-center"
    >
      <div class="mb-3 flex justify-center text-warn">
        <Icon
          name="lucide:shield-alert"
          :size="28"
        />
      </div>
      <div class="mb-2 text-[13px] font-semibold text-warn">
        Blocked
      </div>
      <div class="text-[13px] leading-relaxed text-dim">
        This result failed the content check and was never delivered.
        Adjust the prompt and try again.
      </div>
    </div>

    <!-- failed -->
    <div
      v-else-if="job.phase.value === 'failed'"
      class="max-w-xs p-6 text-center"
    >
      <div class="mb-3 flex justify-center text-t-artist">
        <Icon
          name="lucide:circle-alert"
          :size="28"
        />
      </div>
      <div class="mb-2 text-[13px] font-semibold text-t-artist">
        Generation failed
      </div>
      <div class="font-mono text-xs leading-relaxed text-dim">
        {{ job.error.value }}
      </div>
    </div>

    <!-- generating -->
    <template v-if="job.busy.value">
      <div class="absolute inset-0 overflow-hidden">
        <div
          class="absolute inset-x-0 h-1/3"
          style="background: linear-gradient(180deg, transparent, color-mix(in srgb, var(--color-accent) 10%, transparent), transparent); animation: sweep 1.5s linear infinite"
        />
      </div>
      <div class="absolute inset-x-0 bottom-0 border-t border-line bg-void/90 px-3 py-[9px]">
        <div class="mb-[7px] font-mono text-[11.5px] text-dim">
          {{ statusText }}
        </div>
        <div class="h-0.5 overflow-hidden rounded-px bg-line">
          <div
            class="h-full bg-accent transition-[width] duration-100 ease-linear"
            :style="{ width: barPct }"
          />
        </div>
      </div>
    </template>
  </div>
</template>
