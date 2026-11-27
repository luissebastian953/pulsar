// @vitest-environment node
// useImageMeta / inspectImage: the parse runs behind a Worker interface, and
// the worker is terminated on every exit path — result, timeout, abort, crash.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { inspectImage, useImageMeta } from '~/composables/useImageMeta'
import type { MetaWorker } from '~/composables/useImageMeta'
import { MAX_FILE_BYTES, WORKER_TIMEOUT_MS } from '~/utils/imgmeta/limits'
import { handleMetaRequest } from '~/utils/imgmeta/worker-protocol'
import type { MetaRequest, MetaResponse } from '~/utils/imgmeta/worker-protocol'
import { bin, shellPng, tEXt } from './fixtures/imgmeta'

const A1111 = 'a prompt\nSteps: 20, Sampler: Euler a, Seed: 5'

class FakeWorker implements MetaWorker {
  onmessage: ((e: MessageEvent<MetaResponse>) => void) | null = null
  onerror: ((e: ErrorEvent) => void) | null = null
  terminated = false
  requests: MetaRequest[] = []

  constructor(private behaviour: 'answer' | 'hang' | 'crash' = 'answer') {}

  postMessage(req: MetaRequest) {
    this.requests.push(req)

    if (this.behaviour === 'crash') {
      queueMicrotask(() => this.onerror?.({} as ErrorEvent))
    }
    else if (this.behaviour === 'answer') {
      // The same handler the real worker runs, with a structured clone on each
      // side like a real postMessage.
      handleMetaRequest({ ...req, options: structuredClone(req.options) }).then((res) => {
        if (!this.terminated) this.onmessage?.({ data: structuredClone(res) } as MessageEvent<MetaResponse>)
      })
    }
  }

  terminate() {
    this.terminated = true
  }
}

function factory(behaviour: 'answer' | 'hang' | 'crash' = 'answer') {
  const workers: FakeWorker[] = []

  return {
    workers,
    createWorker: () => {
      const w = new FakeWorker(behaviour)

      workers.push(w)
      return w
    },
  }
}

const png = () => new Blob([shellPng([tEXt('parameters', A1111)])])

afterEach(() => {
  vi.useRealTimers()
})

describe('inspectImage', () => {
  it('parses in the worker and terminates it once the result is in', async () => {
    const f = factory()
    const meta = await inspectImage(png(), { createWorker: f.createWorker, stealth: 'never' })

    expect(meta.generator).toBe('a1111')
    expect(meta.params?.prompt).toBe('a prompt')
    expect(f.workers).toHaveLength(1)
    expect(f.workers[0]!.terminated).toBe(true)
    // Only cloneable options cross the boundary; the Blob is posted as-is.
    expect(f.workers[0]!.requests[0]!.options).toEqual({ stealth: 'never' })
    expect(f.workers[0]!.requests[0]!.file).toBeInstanceOf(Blob)
  })

  it('terminates the worker and rejects with `timeout` when a parse hangs', async () => {
    vi.useFakeTimers()

    const f = factory('hang')
    const result = inspectImage(png(), { createWorker: f.createWorker }).catch((e: unknown) => e)

    await vi.advanceTimersByTimeAsync(WORKER_TIMEOUT_MS - 1)
    expect(f.workers[0]!.terminated).toBe(false)

    await vi.advanceTimersByTimeAsync(1)
    expect(f.workers[0]!.terminated).toBe(true)
    await expect(result).resolves.toMatchObject({ name: 'ImgMetaError', code: 'timeout' })
  })

  it('honours a custom timeout', async () => {
    vi.useFakeTimers()

    const f = factory('hang')
    const result = inspectImage(png(), { createWorker: f.createWorker, timeoutMs: 50 }).catch((e: unknown) => e)

    await vi.advanceTimersByTimeAsync(50)
    await expect(result).resolves.toMatchObject({ code: 'timeout' })
    expect(f.workers[0]!.terminated).toBe(true)
  })

  it('refuses an oversized file without starting a worker or reading it', async () => {
    const f = factory()
    const huge = { size: MAX_FILE_BYTES + 1, arrayBuffer: vi.fn() } as unknown as Blob

    await expect(inspectImage(huge, { createWorker: f.createWorker })).rejects.toMatchObject({ code: 'too_large' })
    expect(f.workers).toHaveLength(0)
    expect(huge.arrayBuffer).not.toHaveBeenCalled()
  })

  it('passes typed errors through from the worker', async () => {
    const f = factory()

    await expect(inspectImage(new Blob([bin('GIF89a not supported')]), { createWorker: f.createWorker })).rejects.toMatchObject({ code: 'unsupported_format' })
    expect(f.workers[0]!.terminated).toBe(true)
  })

  it('terminates on abort and on a worker crash', async () => {
    const hang = factory('hang')
    const controller = new AbortController()
    const aborted = inspectImage(png(), { createWorker: hang.createWorker, signal: controller.signal }).catch((e: unknown) => e)

    controller.abort()
    await expect(aborted).resolves.toMatchObject({ code: 'aborted' })
    expect(hang.workers[0]!.terminated).toBe(true)

    const crash = factory('crash')

    await expect(inspectImage(png(), { createWorker: crash.createWorker })).rejects.toMatchObject({ code: 'worker' })
    expect(crash.workers[0]!.terminated).toBe(true)

    await expect(inspectImage(png(), {
      createWorker: () => {
        throw new Error('CSP blocks workers')
      },
    })).rejects.toMatchObject({ code: 'worker' })
  })

  it('ignores a response that carries another request\'s id', async () => {
    vi.useFakeTimers()

    const w = new FakeWorker('hang')
    const result = inspectImage(png(), { createWorker: () => w, timeoutMs: 100 }).catch((e: unknown) => e)

    w.onmessage?.({ data: { id: -1, ok: true, meta: {} } } as MessageEvent<MetaResponse>)
    await vi.advanceTimersByTimeAsync(100)
    await expect(result).resolves.toMatchObject({ code: 'timeout' })
  })
})

describe('worker handler', () => {
  it('never rejects: every failure becomes a typed response', async () => {
    expect(await handleMetaRequest({ id: 7, file: new Blob([bin('nope')]) })).toEqual({ id: 7, ok: false, code: 'unsupported_format', message: 'unsupported_format' })
    expect(await handleMetaRequest({ id: 8, file: { size: MAX_FILE_BYTES + 1 } as Blob })).toMatchObject({ id: 8, ok: false, code: 'too_large' })
    expect(await handleMetaRequest({ id: 9, file: { size: 4, arrayBuffer: () => Promise.reject(new Error('read failed')) } as unknown as Blob })).toEqual({ id: 9, ok: false, code: 'worker', message: 'read failed' })
    expect(await handleMetaRequest({ id: 10, file: png() })).toMatchObject({ id: 10, ok: true, meta: { generator: 'a1111' } })
  })
})

describe('useImageMeta', () => {
  it('exposes pending / meta / error for one inspection slot', async () => {
    const f = factory()
    const { meta, error, pending, inspect } = useImageMeta({ createWorker: f.createWorker })
    const running = inspect(png())

    expect(pending.value).toBe(true)
    expect(await running).toMatchObject({ generator: 'a1111' })
    expect(pending.value).toBe(false)
    expect(meta.value?.generator).toBe('a1111')
    expect(error.value).toBe('')

    expect(await inspect(new Blob([bin('GIF89a')]))).toBeNull()
    expect(error.value).toBe('unsupported_format')
    expect(meta.value).toBeNull()
  })

  it('a newer inspect supersedes the one in flight and terminates its worker', async () => {
    const workers: FakeWorker[] = []
    const createWorker = () => {
      const w = new FakeWorker(workers.length === 0 ? 'hang' : 'answer')

      workers.push(w)
      return w
    }
    const { meta, error, pending, inspect } = useImageMeta({ createWorker })
    const first = inspect(png())
    const second = inspect(png())

    expect(await first).toBeNull()
    expect(workers[0]!.terminated).toBe(true)
    expect(await second).toMatchObject({ generator: 'a1111' })
    expect(meta.value?.generator).toBe('a1111')
    expect(error.value).toBe('')
    expect(pending.value).toBe(false)
  })

  it('reports a timeout as an error code, and clear() / scope disposal stop the worker', async () => {
    vi.useFakeTimers()

    const f = factory('hang')
    const scope = effectScope()
    const api = scope.run(() => useImageMeta({ createWorker: f.createWorker, timeoutMs: 200 }))!
    const timedOut = api.inspect(png())

    await vi.advanceTimersByTimeAsync(200)
    expect(await timedOut).toBeNull()
    expect(api.error.value).toBe('timeout')
    expect(api.pending.value).toBe(false)

    void api.inspect(png())
    api.clear()
    expect(f.workers[1]!.terminated).toBe(true)
    expect(api.pending.value).toBe(false)
    expect(api.error.value).toBe('')

    void api.inspect(png())
    scope.stop()
    expect(f.workers[2]!.terminated).toBe(true)
  })
})
