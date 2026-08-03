<script setup lang="ts">
// The age gate lives here (docs/compliance.md — adults only).
definePageMeta({ layout: false })

const confirmed = ref(false)

function enter() {
  if (!confirmed.value) return

  localStorage.setItem('pulsar-age-ok', '1')
  navigateTo('/create')
}
</script>

<template>
  <div class="flex min-h-dvh flex-col items-center justify-center gap-10 bg-void px-6 font-sans text-text">
    <div class="flex flex-col items-center gap-4">
      <img
        src="/pulsar-lockup.svg"
        alt="pulsar"
        class="h-12 w-auto md:h-14"
      >
      <div class="text-[13px] text-dim">
        self-hosted anime image generation
      </div>
    </div>

    <div class="w-full max-w-sm rounded-canvas border border-line bg-surface p-6">
      <div class="section-label mb-3">
        Age verification
      </div>
      <p class="mb-5 text-[13px] leading-relaxed text-dim">
        pulsar is an adults-only tool. Generated content may include material
        unsuitable for minors. You must be of legal adult age in your
        jurisdiction to continue.
      </p>
      <label class="mb-5 flex cursor-pointer items-start gap-2.5 text-[13px] leading-snug text-text">
        <input
          v-model="confirmed"
          type="checkbox"
          class="focus-ring mt-0.5 size-4 flex-none cursor-pointer accent-(--color-accent)"
        >
        I confirm that I am an adult and wish to proceed.
      </label>
      <BaseButton
        type="primary-gradient"
        size="large"
        class="w-full"
        :disabled="!confirmed"
        @click="enter"
      >
        Enter
      </BaseButton>
    </div>

    <div class="font-mono text-2xs text-faint">
      built on open weights · your GPU, your rules
    </div>
  </div>
</template>
