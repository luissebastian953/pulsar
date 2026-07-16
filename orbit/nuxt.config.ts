export default defineNuxtConfig({
  compatibilityDate: '2026-07-15',
  devtools: { enabled: true },
  runtimeConfig: {
    public: {
      apiBase: 'http://localhost:8080', // NUXT_PUBLIC_API_BASE overrides
    },
  },
})
