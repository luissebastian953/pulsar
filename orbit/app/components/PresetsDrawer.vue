<script setup lang="ts">
import {
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogOverlay,
  AlertDialogPortal,
  AlertDialogRoot,
  AlertDialogTitle,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'

// Presentation-only rows: both /create and /pose feed their own preset shapes
// through this one drawer, so it never knows a config's fields — the caller
// renders the subtitle and resolves the emitted id back to its preset.
export interface DrawerPreset {
  id: string
  name: string
  subtitle: string
}

export interface Props {
  presets: DrawerPreset[]
}

export interface Emits {
  use: [id: string]
  delete: [ids: string[]]
}

defineProps<Props>()
const emit = defineEmits<Emits>()
const open = defineModel<boolean>('open', { required: true })

const selecting = ref(false)
const selected = ref<Set<string>>(new Set())
const pending = ref<DrawerPreset | null>(null) // the row awaiting "use" confirmation

// leaving select mode (or closing) clears the checkbox selection
watch([open, selecting], () => {
  if (!open.value || !selecting.value) selected.value = new Set()
})

function rowClick(p: DrawerPreset) {
  if (selecting.value) selected.value = toggledSet(selected.value, p.id)
  else pending.value = p
}

function confirmUse() {
  // capture before clearing: AlertDialog's own close path also nulls `pending`,
  // and if it runs first the emit would be skipped (the "doesn't autofill" bug)
  const p = pending.value
  pending.value = null

  if (p) {
    emit('use', p.id)
    open.value = false
  }
}

function deleteSelected() {
  if (selected.value.size) emit('delete', [...selected.value])

  selecting.value = false
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="o-anim fixed inset-0 z-40 bg-void/70" />
      <DialogContent
        class="d-anim fixed inset-y-0 right-0 z-50 flex w-[320px] max-w-[86vw] flex-col border-l border-line bg-surface shadow-[0_0_40px_rgba(0,0,0,.5)] focus:outline-none"
        aria-describedby="presets-desc"
      >
        <header class="flex items-center gap-2 border-b border-line px-4 py-3">
          <DialogTitle class="section-label flex-1">
            Configurations
          </DialogTitle>
          <DialogDescription
            id="presets-desc"
            class="sr-only"
          >
            Saved composer configurations. Click one to apply it, or use select mode to delete.
          </DialogDescription>
          <BaseButton
            v-if="presets.length"
            size="small"
            variant="clear"
            :type="selecting ? 'primary' : 'default'"
            :icon="selecting ? 'lucide:x' : 'lucide:list-checks'"
            :aria-label="selecting ? 'Exit select mode' : 'Select configurations'"
            :title="selecting ? 'Exit select mode' : 'Select'"
            @click="selecting = !selecting"
          />
          <DialogClose as-child>
            <BaseButton
              size="small"
              variant="clear"
              icon="lucide:panel-right-close"
              aria-label="Close"
            />
          </DialogClose>
        </header>

        <div class="min-h-0 flex-1 overflow-y-auto p-2">
          <div
            v-if="!presets.length"
            class="px-3 py-10 text-center text-[12.5px] leading-relaxed text-faint"
          >
            No saved configurations yet.<br>Use <span class="text-dim">Save config</span> to store the current setup.
          </div>
          <button
            v-for="p in presets"
            :key="p.id"
            class="focus-ring group flex w-full cursor-pointer items-center gap-2.5 rounded-ctrl border border-transparent px-2.5 py-2 text-left hover:border-line hover:bg-raised"
            :class="selecting && selected.has(p.id) && 'border-line bg-raised'"
            @click="rowClick(p)"
          >
            <span
              v-if="selecting"
              class="flex size-[18px] flex-none items-center justify-center rounded-[5px] border"
              :class="selected.has(p.id) ? 'border-accent bg-accent text-void' : 'border-line-hi text-transparent'"
            >
              <Icon
                name="lucide:check"
                :size="12"
              />
            </span>
            <div class="min-w-0 flex-1">
              <div class="truncate text-[13px] text-text">
                {{ p.name }}
              </div>
              <div class="truncate font-mono text-[10.5px] text-faint">
                {{ p.subtitle }}
              </div>
            </div>
            <Icon
              v-if="!selecting"
              name="lucide:corner-down-left"
              :size="13"
              class="flex-none text-transparent group-hover:text-faint"
            />
          </button>
        </div>

        <footer
          v-if="selecting"
          class="flex-none border-t border-line p-3"
        >
          <BaseButton
            class="w-full"
            variant="outline"
            type="warn"
            icon="lucide:trash-2"
            :disabled="!selected.size"
            @click="deleteSelected"
          >
            Delete{{ selected.size ? ` (${selected.size})` : '' }}
          </BaseButton>
        </footer>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>

  <!-- Use-configuration confirm -->
  <AlertDialogRoot
    :open="!!pending"
    @update:open="v => { if (!v) pending = null }"
  >
    <AlertDialogPortal>
      <AlertDialogOverlay class="o-anim fixed inset-0 z-[60] bg-void/70" />
      <AlertDialogContent class="pop-anim fixed left-1/2 top-1/2 z-[61] w-[300px] max-w-[90vw] -translate-x-1/2 -translate-y-1/2 rounded-canvas border border-line bg-raised p-5 shadow-[0_20px_50px_rgba(0,0,0,.6)] focus:outline-none">
        <AlertDialogTitle class="text-[14px] font-semibold text-text">
          Use this configuration?
        </AlertDialogTitle>
        <AlertDialogDescription class="mt-1.5 text-[12.5px] leading-relaxed text-dim">
          Switches the composer to <span class="text-text">{{ pending?.name }}</span>. Your current setup isn't saved unless you saved it first.
        </AlertDialogDescription>
        <div class="mt-4 flex justify-end gap-2">
          <AlertDialogCancel as-child>
            <BaseButton variant="outline">
              No
            </BaseButton>
          </AlertDialogCancel>
          <BaseButton
            type="primary"
            @click="confirmUse"
          >
            Yes, use it
          </BaseButton>
        </div>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>
