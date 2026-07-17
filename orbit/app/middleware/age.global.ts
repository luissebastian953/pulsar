// Age gate — adults only (docs/compliance.md). The gate lives at /login.
export default defineNuxtRouteMiddleware((to) => {
  if (to.path === '/login') return
  if (import.meta.client && !localStorage.getItem('pulsar-age-ok')) {
    return navigateTo('/login')
  }
})
