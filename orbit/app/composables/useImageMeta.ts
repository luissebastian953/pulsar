import { getCurrentScope, onScopeDispose, ref, shallowRef } from 'vue'
import { MAX_FILE_BYTES, WORKER_TIMEOUT_MS } from '~/utils/imgmeta/limits'
import type { ReadOptions } from '~/utils/imgmeta/read'
import { ImgMetaError } from '~/utils/imgmeta/types'
import type { ImageMeta, ImgMetaErrorCode } from '~/utils/imgmeta/types'
import type { MetaRequest, MetaResponse } from '~/utils/imgmeta/worker-protocol'

/** The slice of Worker this module uses (so tests can substitute one). */
export interface MetaWorker {
  postMessage(message: MetaRequest): void
  terminate(): void
  onmessage: ((e: MessageEvent<MetaResponse>) => void) | null
  onerror: ((e: ErrorEvent) => void) | null
  onmessageerror?: ((e: MessageEvent) => void) | null
}

export interface InspectOptions extends ReadOptions {
  /** Terminate the worker after this long (default WORKER_TIMEOUT_MS). */
  timeoutMs?: number
  /** Abort early; the worker is terminated and the promise rejects with `aborted`. */
  signal?: AbortSignal
  /** Worker factory override, for tests. */
  createWorker?: () => MetaWorker
}

function defaultWorker(): MetaWorker {
  return new Worker(new URL('../workers/imgmeta.worker.ts', import.meta.url), { type: 'module' }) as MetaWorker
}

let nextId = 1

/**
 * Parse an image's generation metadata in a dedicated Web Worker — never on
 * the main thread. One worker per file: it is terminated when the result
 * arrives, on timeout, and on abort, so a hostile file cannot leave anything
 * running. Rejects with ImgMetaError; every string in the result is
 * untrusted file content.
 */
export function inspectImage(file: Blob, options: InspectOptions = {}): Promise<ImageMeta> {
  const { timeoutMs = WORKER_TIMEOUT_MS, signal, createWorker = defaultWorker, ...read } = options

  // Size is known without reading: refuse before a worker even starts.
  if (file.size > MAX_FILE_BYTES) return Promise.reject(new ImgMetaError('too_large'))

  if (signal?.aborted) return Promise.reject(new ImgMetaError('aborted'))

  return new Promise<ImageMeta>((resolve, reject) => {
    let worker: MetaWorker

    try {
      worker = createWorker()
    }
    catch {
      reject(new ImgMetaError('worker', 'could not start the metadata worker'))
      return
    }

    const id = nextId++
    let settled = false
    const finish = (result: ImageMeta | ImgMetaError) => {
      if (settled) return

      settled = true
      clearTimeout(timer)
      signal?.removeEventListener('abort', onAbort)
      worker.terminate()

      if (result instanceof ImgMetaError) reject(result)
      else resolve(result)
    }
    const onAbort = () => finish(new ImgMetaError('aborted'))
    const timer = setTimeout(() => finish(new ImgMetaError('timeout')), timeoutMs)

    signal?.addEventListener('abort', onAbort)
    worker.onerror = () => finish(new ImgMetaError('worker', 'the metadata worker crashed'))
    worker.onmessageerror = () => finish(new ImgMetaError('worker', 'the metadata worker sent an unreadable result'))
    worker.onmessage = (e) => {
      const res = e.data

      if (!res || res.id !== id) return

      finish(res.ok ? res.meta : new ImgMetaError(res.code, res.message))
    }

    try {
      worker.postMessage({ id, file, options: read })
    }
    catch {
      finish(new ImgMetaError('worker', 'could not send the file to the metadata worker'))
    }
  })
}

/**
 * Reactive wrapper around inspectImage for a page with one inspection slot.
 * A new `inspect` supersedes the one in flight (its worker is terminated),
 * and leaving the scope cancels.
 */
export function useImageMeta(defaults: InspectOptions = {}) {
  const meta = shallowRef<ImageMeta | null>(null)
  const error = ref<ImgMetaErrorCode | ''>('')
  const pending = ref(false)
  let current: AbortController | null = null

  /** Returns the metadata, or null on failure (the code lands in `error`) or when superseded. */
  async function inspect(file: Blob, options: InspectOptions = {}): Promise<ImageMeta | null> {
    current?.abort()

    const controller = new AbortController()

    current = controller
    pending.value = true
    error.value = ''
    meta.value = null

    try {
      const result = await inspectImage(file, { ...defaults, ...options, signal: controller.signal })

      if (current !== controller) return null

      meta.value = result
      return result
    }
    catch (err: unknown) {
      if (current !== controller) return null

      error.value = err instanceof ImgMetaError ? err.code : 'worker'
      return null
    }
    finally {
      if (current === controller) {
        current = null
        pending.value = false
      }
    }
  }

  /** Stop the parse in flight (if any) and clear the slot. */
  function clear() {
    current?.abort()
    current = null
    pending.value = false
    error.value = ''
    meta.value = null
  }

  if (getCurrentScope()) onScopeDispose(() => current?.abort())

  return { meta, error, pending, inspect, clear }
}
