// SPA by design: Electron and Capacitor shells can't run SSR
// (docs/frontend.md §6.1). Never assume same-origin — the API base is
// absolute and per-target (§6.2).
export default defineNuxtConfig({
  modules: ['@nuxt/icon', '@nuxt/eslint', '@vueuse/nuxt'],
  ssr: false,
  app: {
    head: {
      title: 'pulsar',
      link: [{ rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
      htmlAttrs: { lang: 'en' },
    },
  },
  css: ['~/assets/css/main.css'],
  runtimeConfig: {
    public: {
      apiBase: 'http://localhost:8080', // NUXT_PUBLIC_API_BASE overrides
      modelName: 'Illustrious', // NUXT_PUBLIC_MODEL_NAME overrides
    },
  },
  compatibilityDate: '2026-07-15',
  // Tailwind v4 via PostCSS — the @tailwindcss/vite plugin conflicts with
  // Nuxt's vite-builder entry resolution
  postcss: {
    plugins: { '@tailwindcss/postcss': {} },
  },
  eslint: {
    // registers @stylistic rules; project style already matches its defaults
    config: { stylistic: true },
  },
  icon: {
    // ships to shells — bundle used icons, no runtime Iconify fetches
    clientBundle: { scan: true },
  },
})
