<script setup lang="ts">
import {
  SelectContent,
  SelectIcon,
  SelectItem,
  SelectItemIndicator,
  SelectItemText,
  SelectPortal,
  SelectRoot,
  SelectTrigger,
  SelectValue,
  SelectViewport,
} from 'reka-ui'

defineProps<{ options: { value: string, label: string }[], ariaLabel?: string }>()
const model = defineModel<string>({ required: true })
</script>

<template>
  <SelectRoot v-model="model">
    <SelectTrigger
      class="focus-ring flex w-full cursor-pointer items-center justify-between gap-2 rounded-ctrl border border-line bg-void px-3 py-2 text-[12.5px] text-text hover:border-line-hi"
      :aria-label="ariaLabel"
    >
      <SelectValue class="overflow-hidden text-ellipsis whitespace-nowrap" />
      <SelectIcon class="flex text-faint">
        <Icon name="lucide:chevron-down" :size="13" />
      </SelectIcon>
    </SelectTrigger>
    <SelectPortal>
      <SelectContent
        position="popper"
        :side-offset="6"
        class="z-60 w-[var(--reka-select-trigger-width)] rounded-ctrl border border-line bg-raised p-1.5 shadow-[0_12px_30px_rgba(0,0,0,.5)]"
      >
        <SelectViewport>
          <SelectItem
            v-for="o in options"
            :key="o.value"
            :value="o.value"
            class="flex cursor-pointer items-center justify-between gap-2 rounded-lg px-2.5 py-2 text-[12.5px] text-text outline-none data-highlighted:bg-line data-[state=checked]:text-accent-soft"
          >
            <SelectItemText>{{ o.label }}</SelectItemText>
            <SelectItemIndicator class="flex text-accent-soft">
              <Icon name="lucide:check" :size="13" />
            </SelectItemIndicator>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
