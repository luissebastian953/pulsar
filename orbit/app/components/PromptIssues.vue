<script setup lang="ts">
// The message labels under a prompt input: parse errors in red with their
// hint, then notices (relocated to UC, converted from SD syntax,
// calibrated, unclosed section) small and quiet. Backticked spans in core's
// messages render as code.
import type { PromptIssue } from '~/types/api'

export interface Props {
  errors?: PromptIssue[]
  notices?: PromptIssue[]
}

withDefaults(defineProps<Props>(), { errors: () => [], notices: () => [] })

function parts(s: string) {
  return s.split('`').map((text, i) => ({ text, code: i % 2 === 1 }))
}
</script>

<template>
  <div
    v-if="errors.length || notices.length"
    class="mt-1.5 flex flex-col gap-1"
  >
    <div
      v-for="(e, i) in errors"
      :key="`e${i}`"
      role="alert"
      data-test="prompt-error"
      class="text-[11px] leading-snug text-danger"
    >
      <span class="font-semibold"><span
        v-for="(p, k) in parts(e.message)"
        :key="k"
        :class="p.code && 'font-mono'"
      >{{ p.text }}</span></span><span
        v-if="e.hint"
        data-test="prompt-hint"
      > — <span
        v-for="(p, k) in parts(e.hint)"
        :key="k"
        :class="p.code && 'font-mono'"
      >{{ p.text }}</span></span>
    </div>
    <div
      v-for="(n, i) in notices"
      :key="`n${i}`"
      data-test="prompt-notice"
      :data-code="n.code"
      class="text-2xs leading-snug text-faint"
    >
      <span
        v-for="(p, k) in parts(n.message)"
        :key="k"
        :class="p.code && 'font-mono text-dim'"
      >{{ p.text }}</span>
    </div>
  </div>
</template>
