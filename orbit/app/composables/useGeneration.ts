import type { GenerationRequest, JobPayload, PoseRole, PromptErrorBody, PromptIssue } from '~/types/api'
import { promptErrorFrom } from '~/utils/prompt-issues'

export type GenPhase = 'idle' | 'queued' | 'generating' | 'done' | 'failed' | 'blocked'

// one tracked generation: POST /generate + SSE stream; reconnect-safe
export function useJob(onDone?: (job: JobPayload) => void) {
  const api = useApi()
  const phase = ref<GenPhase>('idle')
  const step = ref(0)
  const total = ref(0)
  const queuePos = ref(0)
  const imageUrl = ref('')
  // per-role URLs for multi-output kinds (pose: source/lineart/skeleton)
  const images = ref<Partial<Record<PoseRole, string>>>({})
  const error = ref('')
  // core's located 400 when a prompt failed to parse (field + span)
  const issue = ref<PromptErrorBody | null>(null)
  // notices from the 202 (relocated to UC, converted from SD syntax, …)
  const notices = ref<(PromptIssue & { field: string })[]>([])
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
    images.value = {}
    error.value = ''
    issue.value = null
    try {
      const res = await api.post<{ job_id: string, notices?: (PromptIssue & { field: string })[] }>('/generate', req)
      const job_id = res.job_id
      notices.value = res.notices ?? []
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
        images.value = Object.fromEntries(
          Object.entries(p.images ?? {}).map(([role, url]) => [role, api.imageUrl(url)]),
        )
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
      issue.value = promptErrorFrom(e)
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

  return { phase, step, total, queuePos, imageUrl, images, error, issue, notices, seed, start, showResult, busy }
}

export type JobTracker = ReturnType<typeof useJob>
