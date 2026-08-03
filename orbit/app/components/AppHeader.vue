<script setup lang="ts">
const route = useRoute()
const { public: { modelName } } = useRuntimeConfig()

const nav = [
  { to: '/create', label: 'Create', icon: 'lucide:sparkles' },
  { to: '/pose', label: 'Pose', icon: 'lucide:person-standing' },
  { to: '/history', label: 'History', icon: 'lucide:history' },
]
</script>

<template>
  <header class="flex h-[52px] flex-none items-center gap-4 border-b border-line bg-surface px-4 md:gap-7">
    <NuxtLink
      to="/create"
      class="flex items-center text-white"
      aria-label="pulsar home"
    >
      <PulsarMark :size="26" />
    </NuxtLink>

    <nav class="hidden h-full md:flex md:gap-1">
      <NuxtLink
        v-for="item in nav"
        :key="item.to"
        :to="item.to"
        class="focus-ring-inset flex items-center gap-[7px] border-y-2 border-transparent px-3 text-sm"
        :class="route.path.startsWith(item.to) ? 'border-b-line-hi text-text' : 'text-dim hover:text-text'"
      >
        <Icon
          :name="item.icon"
          :size="15"
          class="flex-none"
        />
        {{ item.label }}
      </NuxtLink>
    </nav>

    <div class="flex-1" />

    <div class="flex items-center gap-2">
      <BaseButton
        size="small"
        variant="outline"
        icon="lucide:chevron-down"
        icon-right
      >
        {{ modelName }}
      </BaseButton>
      <div
        class="flex items-center gap-1.5 rounded-ctrl border border-line px-2.5 py-[5px] text-xs text-dim"
        title="Credits"
      >
        <Icon
          name="lucide:zap"
          :size="13"
        />
        1,240
      </div>
    </div>
  </header>
</template>
