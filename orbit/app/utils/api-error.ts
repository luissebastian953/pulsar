// Error-shape helper for core API failures. Auto-imported by Nuxt.

/** Extract core's `{ error }` body from a failed $fetch, with a fallback. */
export function apiErrorMessage(err: unknown, fallback = 'request failed'): string {
  const ex = err as { data?: { error?: string } }
  return ex?.data?.error || fallback
}
