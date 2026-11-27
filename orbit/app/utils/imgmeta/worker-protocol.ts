// Messages between useImageMeta (main thread) and imgmeta.worker. The Blob is
// posted as-is — structured clone shares it without copying — and is only
// READ inside the worker.
import { MAX_FILE_BYTES } from './limits'
import { readImageMeta } from './read'
import type { ReadDeps, ReadOptions } from './read'
import { ImgMetaError } from './types'
import type { ImageMeta, ImgMetaErrorCode } from './types'

export interface MetaRequest {
  id: number
  file: Blob
  options?: ReadOptions
}

export type MetaResponse
  = | { id: number, ok: true, meta: ImageMeta }
    | { id: number, ok: false, code: ImgMetaErrorCode, message: string }

/** Runs one request to completion. Never rejects: failures become `ok: false`. */
export async function handleMetaRequest(req: MetaRequest, deps: ReadDeps = {}): Promise<MetaResponse> {
  try {
    if (req.file.size > MAX_FILE_BYTES) throw new ImgMetaError('too_large')

    const meta = await readImageMeta(await req.file.arrayBuffer(), req.options, deps)

    return { id: req.id, ok: true, meta }
  }
  catch (err) {
    return err instanceof ImgMetaError
      ? { id: req.id, ok: false, code: err.code, message: err.message }
      : { id: req.id, ok: false, code: 'worker', message: err instanceof Error ? err.message : 'parse failed' }
  }
}
