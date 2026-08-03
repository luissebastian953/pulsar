// Minimal IndexedDB key→Blob store for preset images. Kept dependency-free (a
// ~40-line native wrapper instead of idb-keyval) to honour the repo's
// bundle-size rule. localStorage holds the small preset metadata; the image
// bytes live here because base64 in localStorage would exhaust its ~5MB quota
// after a handful of references. Keys are uuids minted by the caller.
const DB_NAME = 'pulsar'
const STORE = 'preset-blobs'

let dbPromise: Promise<IDBDatabase> | null = null

function db(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise

  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1)
    req.onupgradeneeded = () => {
      if (!req.result.objectStoreNames.contains(STORE)) {
        req.result.createObjectStore(STORE)
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
  return dbPromise
}

async function tx(mode: IDBTransactionMode): Promise<IDBObjectStore> {
  const d = await db()

  return d.transaction(STORE, mode).objectStore(STORE)
}

function wrap<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

export function useBlobStore() {
  async function put(blob: Blob): Promise<string> {
    const key = crypto.randomUUID()
    await wrap((await tx('readwrite')).put(blob, key))
    return key
  }

  async function get(key: string): Promise<Blob | undefined> {
    if (!key) return undefined

    return wrap((await tx('readonly')).get(key) as IDBRequest<Blob | undefined>)
  }

  async function remove(keys: string[]): Promise<void> {
    if (!keys.length) return

    const store = await tx('readwrite')
    await Promise.all(keys.filter(Boolean).map(k => wrap(store.delete(k))))
  }

  return { put, get, remove }
}
