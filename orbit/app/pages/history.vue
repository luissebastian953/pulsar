<script setup lang="ts">
import type { JobPayload } from '~/types/api'

const api = useApi()
const jobs = ref<JobPayload[]>([])
const loaded = ref(false)

onMounted(async () => {
  try {
    const res = await api.get<{ jobs: JobPayload[] }>('/history')
    jobs.value = res.jobs.filter(j => j.state === 'done' && j.image_url)
  }
  catch { /* core offline */ }
  loaded.value = true
})
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto p-4 md:p-8">
    <SectionLabel class="mx-auto mb-4 max-w-5xl">
      History
    </SectionLabel>

    <div v-if="jobs.length" class="mx-auto grid max-w-5xl grid-cols-2 gap-3 md:grid-cols-4">
      <a
        v-for="j in jobs"
        :key="j.id"
        :href="api.imageUrl(j.image_url)"
        target="_blank"
        rel="noopener"
        class="focus-ring group overflow-hidden rounded-ctrl border border-line bg-surface hover:border-line-hi"
      >
        <img
          :src="api.imageUrl(j.image_url)"
          :alt="`generation ${j.id}`"
          loading="lazy"
          class="w-full object-cover"
          :style="{ aspectRatio: `${j.params.width} / ${j.params.height}` }"
        >
        <div class="border-t border-line px-2.5 py-2">
          <div class="truncate font-mono text-2xs text-dim" :title="j.params.prompt">{{ j.params.prompt }}</div>
          <div class="mt-1 font-mono text-2xs text-faint">
            {{ j.params.width }} × {{ j.params.height }} · seed {{ j.params.seed }}
          </div>
        </div>
      </a>
    </div>

    <div v-else-if="loaded" class="mx-auto flex max-w-5xl flex-col items-center gap-3 py-24 text-center">
      <Icon name="lucide:history" :size="28" class="text-line-hi" />
      <div class="text-[13px] text-dim">Nothing generated yet.</div>
      <NuxtLink to="/create" class="text-[13px] text-t-general hover:text-text">
        Head to Create →
      </NuxtLink>
    </div>
  </main>
</template>
