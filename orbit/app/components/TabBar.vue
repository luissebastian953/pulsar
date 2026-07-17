<script setup lang="ts">
const route = useRoute()
const tabs = [
  { to: '/create', label: 'Create', icon: 'lucide:sparkles' },
  { to: '/pose', label: 'Pose', icon: 'lucide:person-standing' },
  { to: '/history', label: 'History', icon: 'lucide:history' },
]
</script>

<template>
  <nav class="flex flex-none border-t border-line bg-surface pb-[env(safe-area-inset-bottom)]">
    <NuxtLink
      v-for="tab in tabs"
      :key="tab.to"
      :to="tab.to"
      class="flex flex-1 flex-col items-center gap-[3px] py-[7px]"
      :class="route.path.startsWith(tab.to) ? 'text-accent-soft' : 'text-faint'"
    >
      <Icon :name="tab.icon" :size="18" />
      <span class="text-2xs font-semibold">{{ tab.label }}</span>
    </NuxtLink>
  </nav>
</template>
