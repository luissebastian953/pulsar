<script setup lang="ts">
// Keep the whole composer alive across navigation to /pose and back (SPA-only,
// ssr:false, so KeepAlive's client-only nature is a non-issue). Route switches
// preserve state; a full reload still resets — that's what saved configs are for.
definePageMeta({ keepalive: true })

const { public: { modelName } } = useRuntimeConfig()

const {
  prompt, mode, tagList, und, chars, translate, stripTags, charBadge,
  promptErrors, promptMarks, promptNotices, invalidChips, undErrors, undNotices, charServerErrors, charInvalid, charsInvalid,
  promptLive, undLive,
  aspect, batch, sizeTier, dims,
  lineage, cfgRescale, checkpoints, activeCheckpoint, steps, guidance,
  seedText, sampler, scheduler, qualityTags, faceDetail,
  loras, loraName, loraStrength, lora2Name, lora2Strength,
  stylesForLineage, styleSlug, activeStyle, styleHasLora,
  charRef, charRefMode, charRefStrength, charRefFidelity, poseRef, refStrength,
  i2i, i2iStrength, maskId, maskEditing, onI2iPick, onMaskSave, clearI2i,
  folds,
  job, generate, cost, recent, openRecent,
  presets, drawerPresets, onSaveName, onUsePreset, onDeletePresets,
} = useCreateComposer()

const recentItems = recent.items

// modal open flags — the only state the page itself owns
const drawerOpen = ref(false)
const saveOpen = ref(false)

// Dropping an image onto the composer opens the same import dialog as
// /inspect (docs/frontend.md#image-inspector). The file is only read here —
// in the metadata worker — and is uploaded only if the dialog says to use it
// as a base image or reference, through the composer's normal upload path.
const IMPORT_TYPES = ['image/png', 'image/jpeg', 'image/webp']
const dropZone = useTemplateRef<HTMLElement>('dropZone')
const dropMeta = useImageMeta()
const dropFile = shallowRef<File | null>(null)
const importOpen = ref(false)
const { isOverDropZone } = useDropZone(dropZone, {
  dataTypes: IMPORT_TYPES,
  multiple: false,
  onDrop: (files) => {
    const f = files?.find(x => IMPORT_TYPES.includes(x.type))

    if (!f) return

    dropFile.value = f
    importOpen.value = true
    dropMeta.inspect(f)
  },
})
</script>

<template>
  <main
    ref="dropZone"
    class="relative flex min-h-0 flex-1 flex-col overflow-y-auto md:flex-row md:overflow-hidden"
  >
    <!-- composer rail -->
    <aside class="order-3 flex flex-none flex-col bg-surface md:order-1 md:h-full md:w-[372px] md:min-h-0 md:border-r md:border-line">
      <div class="min-h-0 p-4 md:flex-1 md:overflow-y-auto">
        <div class="mb-2.5 flex items-center gap-1.5">
          <BaseSectionLabel
            class="flex-1"
            :invalid="promptErrors.length > 0"
          >
            Describe
          </BaseSectionLabel>
          <ChunkPanel />
        </div>
        <PromptInput
          v-if="mode === 'plain'"
          v-model="prompt"
          size="large"
          placeholder="a silver-haired girl in a school uniform, looking back at sunset…"
          :errors="promptMarks"
          :invalid="promptErrors.length > 0"
          :spans="promptLive.chunks.value"
          :marks="promptLive.marks.value"
        />
        <TagInput
          v-else
          v-model="tagList"
          :invalid="promptErrors.length > 0"
          :invalid-chips="invalidChips"
          :spans="promptLive.chunks.value"
          :tags="promptLive.liveTags.value"
        />
        <PromptIssues
          :errors="promptErrors"
          :notices="promptNotices"
        />
        <BaseSegment
          v-model="mode"
          class="mt-2"
          :options="[{ value: 'plain', label: 'plain language' }, { value: 'tags', label: 'tags' }]"
        />

        <ResolvedStrip
          class="mt-3"
          :tags="stripTags"
          :dropped="translate.dropped.value"
          :resolving="translate.resolving.value"
          :offline="translate.offline.value"
          :failed="translate.failed.value"
          :stat="translate.stat.value"
        />

        <BaseFold
          v-model="folds.style"
          label="Style"
          :badge="activeStyle ? activeStyle.name : 'none'"
          class="mt-3.5"
        >
          <StylePicker
            v-model="styleSlug"
            :styles="stylesForLineage"
          />
          <div class="mt-2 text-[11.5px] leading-relaxed text-faint">
            A style adds its tags, negatives and LoRA on the server and sets the
            sampler recipe under Advanced. Your own tags still lead the subject.
            Styles with a LoRA can't combine with references or multiple characters yet.
          </div>
        </BaseFold>

        <BaseFold
          v-model="folds.und"
          label="Undesired content"
          :badge="undErrors.length ? 'error' : (und.trim() ? 'custom' : 'none')"
          :invalid="undErrors.length > 0"
        >
          <PromptInput
            v-model="und"
            size="small"
            placeholder="lowres, extra fingers, watermark…"
            hint="The model's default negative is merged in automatically."
            :errors="undErrors"
            :spans="undLive.chunks.value"
            :marks="undLive.marks.value"
          />
          <PromptIssues
            :errors="undErrors"
            :notices="undNotices"
          />
        </BaseFold>

        <BaseFold
          v-model="folds.chars"
          label="Characters"
          :badge="charBadge"
          :invalid="charsInvalid"
        >
          <div class="flex flex-col gap-2">
            <CharacterCard
              v-for="(c, i) in chars"
              :key="i"
              v-model:enabled="c.enabled"
              v-model:text="c.text"
              v-model:ref-id="c.refId"
              :index="i"
              :lineage="lineage"
              :server-errors="charServerErrors(i)"
              @invalid="v => (charInvalid[i] = v)"
              @remove="chars.splice(i, 1)"
            />
            <button
              class="focus-ring w-full cursor-pointer rounded-ctrl border border-dashed border-line-hi py-[9px] text-center text-[12.5px] text-dim hover:border-dim hover:text-text"
              @click="chars.push({ text: '', enabled: true, refId: '' })"
            >
              <span class="inline-flex items-center justify-center gap-1.5">
                <Icon
                  name="lucide:plus"
                  :size="13"
                />Add character
              </span>
            </button>
            <div class="text-[11.5px] leading-relaxed text-faint">
              Each character gets its own prompt, undesired content, and
              position. Regions keep their attributes from bleeding into
              each other.
            </div>
          </div>
        </BaseFold>

        <BaseFold
          v-model="folds.ref"
          label="Reference"
          :badge="charRef.id.value ? 'character' : (poseRef ? 'pose skeleton' : 'off')"
        >
          <div class="flex flex-col gap-2">
            <template v-if="poseRef">
              <div class="flex items-center gap-2.5 rounded-ctrl border border-line bg-raised px-[11px] py-[9px]">
                <span class="flex h-[34px] w-[26px] flex-none items-center justify-center rounded-lg border border-line-hi bg-void font-mono text-[9px] text-faint">╱</span>
                <div class="flex-1">
                  <div class="text-[12.5px] text-text">
                    Pose skeleton attached
                  </div>
                  <div class="font-mono text-[10.5px] text-faint">
                    {{ poseRef.label }}
                  </div>
                </div>
                <BaseButton
                  size="small"
                  variant="clear"
                  type="warn"
                  icon="lucide:x"
                  aria-label="remove pose skeleton"
                  @click="poseRef = null"
                />
              </div>
              <BaseSliderRow
                v-model="refStrength"
                label="Strength"
                :min="0.1"
                :max="1.5"
                :step="0.05"
                :decimals="2"
              />
              <div class="text-[11.5px] leading-relaxed text-faint">
                Above 1.0 the reference gets hard to override with words.
              </div>
            </template>
            <template v-if="charRef.id.value">
              <div class="flex items-center gap-2.5 rounded-ctrl border border-line bg-void p-2">
                <img
                  :src="charRef.preview.value"
                  alt="reference"
                  class="size-14 flex-none rounded-lg object-cover"
                >
                <div class="min-w-0 flex-1">
                  <div class="text-[12.5px] text-text">
                    Character reference
                  </div>
                  <div class="font-mono text-[10.5px] text-faint">
                    identity via IP-Adapter Plus
                  </div>
                </div>
                <BaseButton
                  size="small"
                  variant="clear"
                  type="warn"
                  icon="lucide:x"
                  aria-label="remove reference"
                  @click="charRef.clear()"
                />
              </div>
              <BaseDropdown
                v-model="charRefMode"
                :options="[
                  { value: 'character_style', label: 'Character & Style' },
                  { value: 'character', label: 'Character' },
                  { value: 'style', label: 'Style' },
                ]"
              />
              <BaseSliderRow
                v-model="charRefStrength"
                label="Strength"
                :min="0"
                :max="1.2"
                :step="0.05"
                :decimals="2"
              />
              <BaseSliderRow
                v-model="charRefFidelity"
                label="Fidelity"
                :min="0"
                :max="1"
                :step="0.05"
                :decimals="2"
              />
              <div class="text-[11.5px] leading-relaxed text-faint">
                Strength: how hard the reference pushes (~0.5 loose, ~0.8 strong).
                Fidelity: how long it steers — higher follows the reference into fine detail.
              </div>
            </template>
            <label
              v-else
              class="focus-ring flex w-full cursor-pointer items-center justify-center gap-1.5 rounded-ctrl border border-dashed border-line py-[9px] text-center text-[12.5px] text-dim hover:border-line-hi"
            >
              <Icon
                name="lucide:plus"
                :size="13"
              />Character reference
              <input
                type="file"
                accept="image/png,image/jpeg,image/webp"
                class="hidden"
                @change="charRef.pick"
              >
            </label>
            <div
              v-if="charRef.error.value"
              class="text-[11px] text-warn"
            >
              {{ charRef.error.value }}
            </div>
          </div>
        </BaseFold>

        <BaseFold
          v-model="folds.i2i"
          label="Image to image"
          :badge="maskId ? 'inpaint' : (i2i.id.value ? 'on' : 'off')"
        >
          <div class="grid gap-2.5">
            <template v-if="i2i.id.value">
              <div class="flex items-center gap-2.5 rounded-ctrl border border-line bg-void p-2">
                <img
                  :src="i2i.preview.value"
                  alt="input"
                  class="size-14 flex-none rounded-lg object-cover"
                >
                <div class="min-w-0 flex-1">
                  <div class="truncate text-[12px] text-dim">
                    {{ i2i.name.value }}
                  </div>
                  <div class="font-mono text-[10.5px] text-faint">
                    fitted to the nearest bucket on upload
                  </div>
                </div>
                <BaseButton
                  size="small"
                  variant="clear"
                  type="warn"
                  icon="lucide:x"
                  aria-label="remove image"
                  @click="clearI2i"
                />
              </div>
              <div class="flex items-center gap-2">
                <BaseButton
                  size="small"
                  variant="outline"
                  class="flex-1"
                  @click="maskEditing = true"
                >
                  {{ maskId ? 'Repaint mask' : 'Paint mask (inpaint)' }}
                </BaseButton>
                <BaseButton
                  v-if="maskId"
                  size="small"
                  variant="outline"
                  type="warn"
                  @click="maskId = ''"
                >
                  drop mask
                </BaseButton>
              </div>
              <BaseSliderRow
                v-model="i2iStrength"
                label="Strength"
                :min="0.05"
                :max="0.95"
                :step="0.05"
                :decimals="2"
              />
              <div class="text-[10.5px] leading-relaxed text-faint">
                {{ maskId ? 'only the painted area is repainted' : 'higher = more change; ~0.6 restructures while keeping composition' }}
              </div>
            </template>
            <label
              v-else
              class="focus-ring flex cursor-pointer items-center justify-center gap-2 rounded-ctrl border border-dashed border-line px-3 py-4 text-[12px] text-dim hover:border-line-hi"
            >
              <Icon
                name="lucide:image-plus"
                :size="14"
              />
              choose an image (png / jpeg / webp)
              <input
                type="file"
                accept="image/png,image/jpeg,image/webp"
                class="hidden"
                @change="onI2iPick"
              >
            </label>
            <div
              v-if="i2i.error.value"
              class="text-[11px] text-warn"
            >
              {{ i2i.error.value }}
            </div>
          </div>
        </BaseFold>
        <MaskEditor
          v-if="maskEditing && i2i.id.value"
          :src="i2i.preview.value"
          :width="i2i.width.value"
          :height="i2i.height.value"
          @save="onMaskSave"
          @close="maskEditing = false"
        />

        <BaseSectionLabel class="mb-2.5 mt-3.5 border-t border-line pt-3">
          Image
        </BaseSectionLabel>
        <BaseDropdown
          v-model="aspect"
          aria-label="Aspect"
          :options="[
            { value: 'portrait', label: 'Portrait · 832 × 1216' },
            { value: 'landscape', label: 'Landscape · 1216 × 832' },
            { value: 'square', label: 'Square · 1024 × 1024' },
          ]"
        />
        <BaseSegment
          v-model="batch"
          class="mt-2"
          :options="[1, 2, 3, 4].map(n => ({ value: n, label: String(n) }))"
        />

        <BaseFold
          v-model="folds.adv"
          label="Advanced"
          :badge="lineage === 'noobai' ? 'NoobAI · v-pred' : `tuned for ${modelName}`"
          class="mt-3.5"
        >
          <div class="flex flex-col gap-3">
            <div>
              <div class="mb-[5px] text-xs text-dim">
                Model lineage
              </div>
              <BaseDropdown
                v-model="lineage"
                aria-label="Model lineage"
                :options="[
                  { value: 'illustrious', label: 'Illustrious · eps' },
                  { value: 'noobai', label: 'NoobAI · v-pred' },
                ]"
              />
              <div class="mt-1 text-[10.5px] leading-relaxed text-faint">
                v-pred (NoobAI) gives a cleaner, less-fried finish. Switching resets
                sampler and guidance — and needs a matching base model below.
              </div>
            </div>
            <div v-if="checkpoints.length > 1">
              <div class="mb-[5px] text-xs text-dim">
                Base model
              </div>
              <BaseDropdown
                v-model="activeCheckpoint"
                aria-label="Base model"
                :options="checkpoints.map(c => ({ value: c, label: stripModelExt(c) }))"
              />
              <div class="mt-1 text-[10.5px] leading-relaxed text-faint">
                Swapping the base changes the look; same-lineage settings still apply.
              </div>
            </div>
            <BaseSliderRow
              v-model="steps"
              label="Steps"
              :min="1"
              :max="50"
            />
            <BaseSliderRow
              v-model="guidance"
              label="Prompt guidance"
              :min="1"
              :max="12"
              :step="0.1"
              :decimals="1"
            />
            <BaseSliderRow
              v-if="lineage === 'noobai'"
              v-model="cfgRescale"
              label="CFG rescale"
              title="Tempers v-pred colour burn from high guidance; 0 disables it"
              :min="0"
              :max="1"
              :step="0.05"
              :decimals="2"
            />
            <BaseInput
              v-model="seedText"
              label="Seed"
              variant="mono"
              placeholder="random"
              inputmode="numeric"
            />
            <div class="flex gap-2">
              <div class="min-w-0 flex-1">
                <div class="mb-[5px] text-xs text-dim">
                  Sampler
                </div>
                <BaseDropdown
                  v-model="sampler"
                  aria-label="Sampler"
                  :options="[
                    { value: 'euler_ancestral', label: 'Euler a' },
                    { value: 'euler', label: 'Euler' },
                    { value: 'dpmpp_2m', label: 'DPM++ 2M' },
                  ]"
                />
              </div>
              <div class="min-w-0 flex-1">
                <div class="mb-[5px] text-xs text-dim">
                  Noise schedule
                </div>
                <BaseDropdown
                  v-model="scheduler"
                  aria-label="Noise schedule"
                  :options="[
                    { value: 'karras', label: 'Karras' },
                    { value: 'normal', label: 'Native' },
                  ]"
                />
              </div>
            </div>
            <div class="flex items-center">
              <span class="flex-1 text-xs text-dim">Quality tags</span>
              <BaseToggleSwitch v-model="qualityTags" />
            </div>
            <div class="flex items-center">
              <span class="flex-1 text-xs text-dim">Face detail pass</span>
              <BaseToggleSwitch
                v-model="faceDetail"
                title="Wired when the FaceDetailer workflow lands"
              />
            </div>
            <div>
              <div class="mb-[5px] text-xs text-dim">
                Size
              </div>
              <BaseDropdown
                v-model="sizeTier"
                aria-label="Size"
                :options="[
                  { value: 'normal', label: 'Normal (native)' },
                  { value: 'large', label: 'Large (×1.5 refine)' },
                  { value: 'ultra', label: 'Ultra (×2.0 refine)' },
                ]"
              />
              <div class="mt-1 text-[10.5px] leading-relaxed text-faint">
                Large and Ultra add a refine pass — smoother detail, roughly
                2× the time. Ultra is heavy on unified memory.
              </div>
            </div>
            <div
              v-if="loras.length"
              class="border-t border-line pt-3"
            >
              <div class="mb-[5px] text-xs text-dim">
                Style / character LoRA
              </div>
              <BaseDropdown
                v-model="loraName"
                aria-label="LoRA"
                :options="[{ value: '', label: 'None' }, ...loras.map(l => ({ value: l, label: stripModelExt(l) }))]"
              />
              <BaseSliderRow
                v-if="loraName"
                v-model="loraStrength"
                label="LoRA strength"
                :min="0"
                :max="1.5"
                :step="0.05"
                :decimals="2"
                class="mt-2"
              />
              <div
                v-if="loraName && styleHasLora"
                class="mt-2 text-[10.5px] leading-relaxed text-faint"
              >
                The style's LoRA fills the second slot.
              </div>
              <template v-if="loraName && !styleHasLora">
                <div class="mb-[5px] mt-3 text-xs text-dim">
                  Second LoRA (chained)
                </div>
                <BaseDropdown
                  v-model="lora2Name"
                  aria-label="Second LoRA"
                  :options="[{ value: '', label: 'None' }, ...loras.filter(l => l !== loraName).map(l => ({ value: l, label: stripModelExt(l) }))]"
                />
                <BaseSliderRow
                  v-if="lora2Name"
                  v-model="lora2Strength"
                  label="Second strength"
                  :min="0"
                  :max="1.5"
                  :step="0.05"
                  :decimals="2"
                  class="mt-2"
                />
              </template>
            </div>
            <div class="text-[11.5px] leading-relaxed text-faint">
              These defaults are set for {{ modelName }}. Switching models
              resets them — the settings don't carry across.
            </div>
          </div>
        </BaseFold>
      </div>

      <div class="sticky bottom-0 flex-none border-t border-line bg-surface px-4 py-3.5 md:static">
        <GenerateButton
          :cost="cost"
          :busy="job.busy.value"
          @click="generate"
        />
      </div>
    </aside>

    <!-- stage -->
    <section class="order-1 flex flex-none min-w-0 items-start justify-center px-4 pt-3.5 md:order-2 md:flex-1 md:items-center md:p-8">
      <StageCanvas
        :job="job"
        :width="dims[0]"
        :height="dims[1]"
        class="md:hidden"
        fit="block"
      />
      <StageCanvas
        :job="job"
        :width="dims[0]"
        :height="dims[1]"
        class="hidden md:flex"
        fit="viewport"
      />
    </section>

    <!-- recent: horizontal on mobile, right rail on desktop -->
    <RecentStrip
      class="order-2 flex-none md:hidden"
      orientation="horizontal"
      :items="recentItems"
      @open="openRecent"
      @error="recent.removeErrored"
    >
      <template #actions>
        <ConfigActions
          :count="presets.length"
          @save="saveOpen = true"
          @open="drawerOpen = true"
        />
      </template>
    </RecentStrip>
    <RecentStrip
      class="order-3 hidden md:flex"
      orientation="vertical"
      :items="recentItems"
      @open="openRecent"
      @error="recent.removeErrored"
    >
      <template #actions>
        <ConfigActions
          :count="presets.length"
          @save="saveOpen = true"
          @open="drawerOpen = true"
        />
      </template>
    </RecentStrip>

    <div
      v-if="isOverDropZone"
      class="pointer-events-none absolute inset-0 z-30 flex items-center justify-center border-2 border-dashed border-accent bg-void/80 text-sm text-text"
      data-test="create-drop-overlay"
    >
      Drop to import its prompt or use the image
    </div>
    <MetaImportDialog
      v-model:open="importOpen"
      :file="dropFile"
      :meta="dropMeta.meta.value"
      :pending="dropMeta.pending.value"
      :error="dropMeta.error.value"
    />

    <PresetSaveDialog
      v-model:open="saveOpen"
      @save="onSaveName"
    />
    <PresetsDrawer
      v-model:open="drawerOpen"
      :presets="drawerPresets"
      @use="onUsePreset"
      @delete="onDeletePresets"
    />
  </main>
</template>
