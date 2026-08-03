import type { JobPayload } from '~/types/api'

// The /history page's job list, tab split, selection, and the contextual
// download/delete actions. The detail modal and preset glue stay in the page.
export function useHistoryPage() {
  const api = useApi()

  const jobs = ref<JobPayload[]>([])
  const loaded = ref(false)
  const tab = ref<'create' | 'pose'>('create')
  const selecting = ref(false)
  const confirmAllOpen = ref(false)

  // the pose surface generates skeleton refs; everything else is a Create result
  const isPose = (j: JobPayload) => j.kind === 'pose'
  // pose cards read as a sketch library: show the lineart, not the source render
  const imgOf = (j: JobPayload) => (isPose(j) ? j.images?.lineart : undefined)
    ?? j.image_url ?? Object.values(j.images ?? {})[0] ?? ''
  const shown = computed(() => jobs.value.filter(j => (tab.value === 'pose') === isPose(j)))
  const counts = computed(() => ({
    create: jobs.value.filter(j => !isPose(j)).length,
    pose: jobs.value.filter(isPose).length,
  }))

  // checkbox selection
  const checked = ref<Set<string>>(new Set())
  watch(selecting, (on) => {
    if (!on) checked.value = new Set()
  })

  function toggleCheck(id: string) {
    checked.value = toggledSet(checked.value, id)
  }

  // download is contextual like the bin: checked items in select mode, else
  // everything. Selection survives a download so "select → download → delete"
  // works in one pass.
  const downloading = ref(false)
  const dlDone = ref(0)
  const dlTotal = ref(0)

  async function onDownloadClick() {
    const targets = selecting.value ? jobs.value.filter(j => checked.value.has(j.id)) : jobs.value

    if (!targets.length || downloading.value) return

    downloading.value = true
    dlDone.value = 0
    dlTotal.value = targets.length

    for (const j of targets) {
      const url = api.imageUrl(j.image_url ?? imgOf(j))
      await downloadImage(url, `pulsar-${j.params.seed ?? 'image'}-${j.id.slice(0, 8)}.png`)
      dlDone.value++
      // cached fetches resolve instantly; pace the clicks so the browser keeps them all
      await new Promise(r => setTimeout(r, 150))
    }

    downloading.value = false
  }

  // the top-right bin is contextual: delete the checked items in select mode,
  // otherwise open the delete-all confirmation
  function onBinClick() {
    if (selecting.value) deleteSelected()
    else confirmAllOpen.value = true
  }

  async function deleteSelected() {
    const ids = [...checked.value]

    if (!ids.length) return

    await Promise.allSettled(ids.map(id => api.del(`/history/${id}`)))
    const drop = new Set(ids)
    jobs.value = jobs.value.filter(x => !drop.has(x.id))
    selecting.value = false
  }

  async function deleteAll() {
    confirmAllOpen.value = false

    try {
      await api.del('/history')
      jobs.value = []
      selecting.value = false
    }
    catch { /* core offline */ }
  }

  onMounted(async () => {
    try {
      const res = await api.get<{ jobs: JobPayload[] }>('/history')
      jobs.value = res.jobs.filter(j => j.state === 'done' && (j.image_url || j.images))
    }
    catch { /* core offline */ }

    loaded.value = true
  })

  return {
    jobs, loaded, tab, shown, counts, isPose, imgOf,
    selecting, checked, toggleCheck,
    downloading, dlDone, dlTotal, onDownloadClick,
    confirmAllOpen, onBinClick, deleteAll,
  }
}
