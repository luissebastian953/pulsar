// The API base is absolute and per-target — never assume same-origin
// (docs/frontend.md §6.2). Auth is bearer tokens in storage, never cookies
// (§6.3); core has no auth yet, so the token slot is simply empty.
export function useApi() {
  const { public: { apiBase } } = useRuntimeConfig()
  const token = useLocalStorage<string>('pulsar-token', '')

  function headers(): Record<string, string> {
    return token.value ? { Authorization: `Bearer ${token.value}` } : {}
  }

  return {
    base: apiBase,
    token,
    get: <T>(path: string) =>
      $fetch<T>(`${apiBase}${path}`, { headers: headers() }),
    post: <T>(path: string, body: unknown) =>
      $fetch<T>(`${apiBase}${path}`, { method: 'POST', body, headers: headers() }),
    imageUrl: (path?: string) => (path ? `${apiBase}${path}` : ''),
  }
}
