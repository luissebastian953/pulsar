// SPA by design: Electron and Capacitor shells can't run SSR
// (docs/frontend.md §6.1). Never assume same-origin — the API base is
// absolute and per-target (§6.2).
export default defineNuxtConfig({
  compatibilityDate: '2026-07-15',
  ssr: false,
  modules: ['@nuxt/icon', '@nuxt/eslint', '@vueuse/nuxt'],
  css: ['~/assets/css/main.css'],
  // Tailwind v4 via PostCSS — the @tailwindcss/vite plugin conflicts with
  // Nuxt's vite-builder entry resolution
  postcss: {
    plugins: { '@tailwindcss/postcss': {} },
  },
  runtimeConfig: {
    public: {
      apiBase: 'http://localhost:8080', // NUXT_PUBLIC_API_BASE overrides
      modelName: 'Illustrious',         // NUXT_PUBLIC_MODEL_NAME overrides
    },
  },
  icon: {
    // ships to shells — bundle used icons, no runtime Iconify fetches
    clientBundle: { scan: true },
  },
  app: {
    head: {
      title: 'pulsar',
      link: [{ rel: 'icon', type: 'image/svg+xml', href: '/favicon.svg' }],
      htmlAttrs: { lang: 'en' },
    },
  },
})
