<script setup lang="ts">
import { useMediaQuery } from '@vueuse/core'
import {
  DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle, DialogTrigger,
  PopoverClose, PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger,
} from 'reka-ui'
import { ref } from 'vue'
import BaseButton from '~/components/base/Button.vue'
import ChunkPanelBody from '~/components/ChunkPanelBody.vue'

// The prompt box's header button and its panel (docs/frontend.md
// #prompt-chunks): a popover on desktop, a bottom sheet on a phone. The
// popover stays open while you click into a prompt field — that is how
// "click a field, then click a chunk" works — and closes on its button,
// Escape, or the trigger.
const open = ref(false)
const desktop = useMediaQuery('(min-width: 768px)')
</script>

<template>
  <PopoverRoot
    v-if="desktop"
    v-model:open="open"
  >
    <PopoverTrigger as-child>
      <BaseButton
        size="small"
        variant="clear"
        :type="open ? 'primary' : 'default'"
        icon="lucide:puzzle"
        title="Prompt Chunks"
        aria-label="Prompt Chunks"
        data-test="chunk-panel-button"
      />
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent
        side="right"
        align="start"
        :side-offset="14"
        :collision-padding="12"
        class="pop-anim z-60 flex max-h-[min(560px,calc(100vh-24px))] w-[340px] flex-col rounded-ctrl border border-line bg-surface shadow-[0_12px_30px_rgba(0,0,0,.5)] focus:outline-none"
        aria-label="Prompt chunks"
        @interact-outside.prevent
        @open-auto-focus.prevent
      >
        <ChunkPanelBody>
          <template #close>
            <PopoverClose as-child>
              <BaseButton
                size="small"
                variant="clear"
                icon="lucide:x"
                aria-label="Close"
                class="self-center"
              />
            </PopoverClose>
          </template>
        </ChunkPanelBody>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>

  <DialogRoot
    v-else
    v-model:open="open"
  >
    <DialogTrigger as-child>
      <BaseButton
        size="small"
        variant="clear"
        icon="lucide:puzzle"
        title="Prompt Chunks"
        aria-label="Prompt Chunks"
        data-test="chunk-panel-button"
      />
    </DialogTrigger>
    <DialogPortal>
      <DialogOverlay class="o-anim fixed inset-0 z-40 bg-void/70" />
      <DialogContent
        class="pop-anim fixed inset-x-0 bottom-0 z-50 flex h-[70dvh] flex-col rounded-t-canvas border-t border-line bg-surface shadow-[0_0_40px_rgba(0,0,0,.5)] focus:outline-none"
        aria-describedby="chunk-panel-desc"
        @open-auto-focus.prevent
      >
        <DialogTitle class="sr-only">
          Prompt Chunks
        </DialogTitle>
        <DialogDescription
          id="chunk-panel-desc"
          class="sr-only"
        >
          Saved prompt snippets and editor settings. Tap a chunk to insert it into the prompt field you last used.
        </DialogDescription>
        <ChunkPanelBody>
          <template #close>
            <DialogClose as-child>
              <BaseButton
                size="small"
                variant="clear"
                icon="lucide:x"
                aria-label="Close"
                class="self-center"
              />
            </DialogClose>
          </template>
        </ChunkPanelBody>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
