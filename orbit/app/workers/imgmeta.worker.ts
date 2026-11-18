// Web Worker that parses image metadata off the main thread. All parsing of
// untrusted image bytes happens here: a hostile file can at worst spin this
// thread until useImageMeta's timeout terminates it.
import { handleMetaRequest } from '../utils/imgmeta/worker-protocol'
import type { MetaRequest, MetaResponse } from '../utils/imgmeta/worker-protocol'

// Typed locally: the app is compiled against the DOM lib, not WebWorker.
const scope = globalThis as unknown as {
  onmessage: ((e: MessageEvent<MetaRequest>) => void) | null
  postMessage(message: MetaResponse): void
}

scope.onmessage = (e) => {
  const req = e.data

  handleMetaRequest(req).then((res) => {
    try {
      scope.postMessage(res)
    }
    catch {
      // The result itself could not be cloned.
      scope.postMessage({ id: req.id, ok: false, code: 'worker', message: 'result could not be transferred' })
    }
  })
}
