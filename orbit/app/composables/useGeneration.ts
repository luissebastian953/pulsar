import type { GenerationRequest, JobPayload } from '~/types/api'

export type GenPhase = 'idle' | 'queued' | 'generating' | 'done' | 'failed' | 'blocked'

// One tracked generation: POST /generate then native EventSource on
// /jobs/:id/stream. Reconnection is safe — every SSE connection starts from
// a fresh DB snapshot server-side, and the server closes after terminal.
export function useJob(onDone?: (job: JobPayload) => void) {
  const api = useApi()
  const phase = ref<GenPhase>('idle')
  const step = ref(0)
  const total = ref(0)
  const queuePos = ref(0)
  const imageUrl = ref('')
  const error = ref('')
  const seed = ref('')
  let es: EventSource | null = null

  function close() {
    es?.close()
    es = null
  }

  async function start(req: GenerationRequest) {
    close()
    phase.value = 'queued'
    step.value = 0
    total.value = req.steps ?? 24
    queuePos.value = 0
    imageUrl.value = ''
    error.value = ''
    try {
      const { job_id } = await api.post<{ job_id: string }>('/generate', req)
      es = new EventSource(`${api.base}/jobs/${job_id}/stream`)
      es.addEventListener('queue', (e) => {
        const p = JSON.parse(e.data) as JobPayload
        phase.value = 'queued'
        queuePos.value = p.queue_position ?? 0
      })
      es.addEventListener('progress', (e) => {
        const p = JSON.parse(e.data) as { step: number, total: number }
        phase.value = 'generating'
        step.value = p.step
        total.value = p.total
      })
      es.addEventListener('done', (e) => {
        const p = JSON.parse(e.data) as JobPayload
        phase.value = 'done'
        imageUrl.value = api.imageUrl(p.image_url)
        seed.value = String(p.params?.seed ?? '')
        close()
        onDone?.(p)
      })
      es.addEventListener('failed', (e) => {
        const p = JSON.parse(e.data) as JobPayload
        phase.value = 'failed'
        error.value = p.error || 'generation failed'
        close()
      })
      es.addEventListener('blocked', () => {
        phase.value = 'blocked'
        close()
      })
    }
    catch (e: unknown) {
      phase.value = 'failed'
      const err = e as { data?: { error?: string } }
      error.value = err.data?.error || 'request failed — is core running?'
    }
  }

  function showResult(url: string, resultSeed: string) {
    close()
    phase.value = 'done'
    imageUrl.value = url
    seed.value = resultSeed
  }

  const busy = computed(() => phase.value === 'queued' || phase.value === 'generating')

  onScopeDispose(close)

  return { phase, step, total, queuePos, imageUrl, error, seed, start, showResult, busy }
}

export type JobTracker = ReturnType<typeof useJob>
