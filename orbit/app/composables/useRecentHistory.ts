import type { RecentItem } from '~/components/RecentStrip.vue'
import type { JobPayload } from '~/types/api'

// The Recent strip's data: mirrors /history (12 newest matching done jobs).
// Reloaded on every activation, not just first mount — both composer pages are
// kept-alive, so a mount-only snapshot would keep showing thumbnails whose
// blobs were deleted elsewhere.
export function useRecentHistory(
  filter: (j: JobPayload) => boolean,
  toItem: (j: JobPayload) => RecentItem,
) {
  const api = useApi()
  const jobs = ref<JobPayload[]>([])
  const items = computed<RecentItem[]>(() => jobs.value.map(toItem))

  function add(j: JobPayload) {
    if (!filter(j)) return

    jobs.value = [j, ...jobs.value].slice(0, 12)
  }

  async function load() {
    try {
      const res = await api.get<{ jobs: JobPayload[] }>('/history')
      jobs.value = res.jobs.filter(j => j.state === 'done' && filter(j)).slice(0, 12)
    }
    catch { /* core offline — leave the strip as-is */ }
  }

  // drop a thumbnail whose image 404s (its blob was deleted elsewhere)
  function removeErrored(id: string) {
    jobs.value = jobs.value.filter(j => j.id !== id)
  }

  onActivated(load)

  return { jobs, items, add, load, removeErrored }
}
