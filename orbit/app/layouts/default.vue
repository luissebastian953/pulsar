<template>
  <div class="flex h-dvh flex-col overflow-hidden bg-void font-sans text-text">
    <AppHeader />
    <div class="flex min-h-0 flex-1 flex-col">
      <slot />
    </div>
    <BaseTabBar class="md:hidden" />
    <AppToasts />
  </div>
</template>
