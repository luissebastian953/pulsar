<script setup lang="ts">
import { computed } from 'vue'
import { escapeControlChars } from '~/utils/imgmeta/text'
import type { ImageMeta } from '~/utils/imgmeta/types'
import { GENERATOR_LABELS, MAX_DISPLAY_CHARS, SOURCE_LABELS, WARNING_MESSAGES } from '~/utils/meta-labels'

// What an image's metadata says (docs/frontend.md#image-inspector).
//
// EVERY string here is untrusted file content. It is rendered with {{ }}
// only — never v-html, never as a URL or attribute that loads anything,
// never auto-linked — and control characters are made visible first. The
// page that mounts this holds a bearer token; an injection here would hand
// it over.
export interface Props {
  meta: ImageMeta
  /** Raw view: every metadata entry as stored. Otherwise the simplified reading. */
  raw: boolean
}

const props = defineProps<Props>()

const show = (v: unknown) => escapeControlChars(String(v))

/** Cut a long text for display, saying how much was left out. */
function clip(s: string): string {
  return s.length > MAX_DISPLAY_CHARS
    ? `${show(s.slice(0, MAX_DISPLAY_CHARS))}\n… (${s.length - MAX_DISPLAY_CHARS} more characters not shown)`
    : show(s)
}

const p = computed(() => props.meta.params)
const nai = computed(() => p.value?.novelai)

// D21: the badge exists only for a signature that verified. Every other
// outcome — invalid, absent, unchecked — shows nothing at all.
const signed = computed(() => props.meta.generator === 'novelai' && props.meta.signature === 'valid')

const header = computed(() => {
  const out: { label: string, value: string }[] = []
  const g = props.meta.generator

  out.push({ label: 'Made with', value: g ? GENERATOR_LABELS[g] : 'Unknown' })

  if (nai.value?.software) out.push({ label: 'Software', value: nai.value.software })

  if (nai.value?.source) out.push({ label: 'Source', value: `${nai.value.source} (${nai.value.model.name})` })
  else if (p.value?.model) out.push({ label: 'Model', value: p.value.model })

  if (nai.value) out.push({ label: 'Request', value: nai.value.requestTypeLabel })

  if (nai.value?.title) out.push({ label: 'Title', value: nai.value.title })

  if (props.meta.via === 'stealth') out.push({ label: 'Read from', value: 'pixel data (stealth)' })

  return out
})

const settings = computed(() => {
  const v = p.value

  if (!v) return []

  const out: { label: string, value: string }[] = []
  const add = (label: string, value: unknown) => {
    if (value !== undefined && value !== null && value !== '') out.push({ label, value: String(value) })
  }

  if (v.width && v.height) add('Resolution', `${v.width} × ${v.height}`)

  add('Seed', v.seed)
  add('Steps', v.steps)
  add('Sampler', [v.sampler, v.scheduler, nai.value?.smea].filter(Boolean).join(' · '))
  add('Guidance', v.cfg)
  add('CFG rescale', v.rescale)
  add('UC strength', nai.value?.uncondScale)
  add('Model hash', v.modelHash)

  for (const r of v.resources ?? []) {
    add(r.kind, [r.name, r.weight !== undefined ? `× ${r.weight}` : '', r.hash ?? ''].filter(Boolean).join(' '))
  }

  for (const [k, val] of Object.entries(v.extra)) add(k, val)

  return out
})

const warnings = computed(() => props.meta.warnings.map(w => ({
  text: WARNING_MESSAGES[w.code] ?? w.code,
  detail: w.detail ?? '',
})))
</script>

<template>
  <div
    class="flex flex-col gap-4"
    data-test="meta-view"
  >
    <dl class="flex flex-col gap-1.5 text-xs">
      <div
        v-for="h in header"
        :key="h.label"
        class="flex gap-3"
      >
        <dt class="w-24 flex-none text-faint">
          {{ h.label }}
        </dt>
        <dd
          class="min-w-0 flex-1 break-words text-text"
          :data-test="`meta-${h.label.toLowerCase().replace(/ /g, '-')}`"
        >
          {{ show(h.value) }}
        </dd>
      </div>
    </dl>

    <div
      v-if="signed"
      class="flex items-center gap-1.5 text-xs font-medium text-t-character"
      data-test="nai-signed"
    >
      <Icon
        name="lucide:badge-check"
        :size="14"
      />
      Signed by NovelAI ✓
    </div>

    <div
      v-if="!meta.generator"
      class="rounded-ctrl border border-line px-3 py-2.5 text-xs leading-relaxed text-dim"
      data-test="meta-none"
    >
      No generation metadata was recognised in this image{{ meta.entries.length ? ' — the raw entries below are everything it carries.' : '.' }}
    </div>

    <template v-if="!raw && p">
      <section v-if="p.prompt">
        <div class="section-label mb-1.5">
          Prompt
        </div>
        <pre
          class="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-text"
          data-test="meta-prompt"
        >{{ clip(p.prompt) }}</pre>
      </section>
      <section v-if="p.negative">
        <div class="section-label mb-1.5">
          Undesired Content
        </div>
        <pre
          class="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-dim"
          data-test="meta-negative"
        >{{ clip(p.negative) }}</pre>
      </section>
      <section
        v-for="(c, i) in p.characters"
        :key="i"
      >
        <div class="section-label mb-1.5">
          Character {{ i + 1 }}
        </div>
        <pre
          class="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-text"
          data-test="meta-character"
        >{{ clip(c.prompt) }}</pre>
        <pre
          v-if="c.negative"
          class="mt-1 whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-dim"
        >{{ clip(c.negative) }}</pre>
      </section>
      <section v-if="settings.length">
        <div class="section-label mb-1.5">
          Settings
        </div>
        <dl class="flex flex-col gap-1 text-xs">
          <div
            v-for="(s, i) in settings"
            :key="i"
            class="flex gap-3"
          >
            <dt class="w-24 flex-none break-words text-faint">
              {{ show(s.label) }}
            </dt>
            <dd class="min-w-0 flex-1 break-words font-mono text-text">
              {{ show(s.value) }}
            </dd>
          </div>
        </dl>
      </section>
    </template>

    <section
      v-if="raw || !p"
      data-test="meta-raw"
    >
      <div class="section-label mb-1.5">
        Raw metadata · {{ meta.entries.length }} {{ meta.entries.length === 1 ? 'entry' : 'entries' }}
      </div>
      <div
        v-if="!meta.entries.length"
        class="text-xs text-faint"
      >
        This file carries no metadata text.
      </div>
      <div class="flex flex-col gap-2.5">
        <div
          v-for="(e, i) in meta.entries"
          :key="i"
          class="overflow-hidden rounded-ctrl border border-line"
          data-test="meta-entry"
        >
          <div class="flex items-center gap-2 border-b border-line bg-raised px-3 py-1.5 text-xs">
            <span class="min-w-0 flex-1 break-all font-mono text-text">{{ show(e.key) }}</span>
            <span class="flex-none text-2xs text-faint">{{ SOURCE_LABELS[e.source] ?? 'metadata' }}</span>
          </div>
          <pre class="max-h-80 overflow-auto whitespace-pre-wrap break-words px-3 py-2 font-mono text-xs leading-relaxed text-dim">{{ clip(e.text) }}</pre>
        </div>
      </div>
    </section>

    <section
      v-if="warnings.length"
      data-test="meta-warnings"
    >
      <div class="section-label mb-1.5">
        Notes
      </div>
      <ul class="flex flex-col gap-1 text-xs leading-relaxed text-warn">
        <li
          v-for="(w, i) in warnings"
          :key="i"
        >
          {{ w.text }}<span
            v-if="w.detail"
            class="font-mono text-faint"
          > ({{ show(w.detail) }})</span>
        </li>
      </ul>
    </section>
  </div>
</template>
