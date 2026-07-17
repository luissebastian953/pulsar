// absolute API base + bearer-token slot, never cookies (docs/frontend.md §6)
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
    post: <T>(path: string, body: Record<string, unknown>) =>
      $fetch<T>(`${apiBase}${path}`, { method: 'POST', body, headers: headers() }),
    upload: <T>(path: string, file: File) => {
      const form = new FormData()
      form.append('image', file)
      return $fetch<T>(`${apiBase}${path}`, { method: 'POST', body: form, headers: headers() })
    },
    imageUrl: (path?: string) => (path ? `${apiBase}${path}` : ''),
  }
}
