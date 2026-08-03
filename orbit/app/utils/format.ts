// Small display formatters shared across orbit. Auto-imported by Nuxt.

/** 1_300_000 → "1.3M", 22_000 → "22k", 950 → "950", 0 → "" */
export function formatCount(n: number): string {
  if (!n) return ''

  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`

  if (n >= 1e3) return `${Math.round(n / 1e3)}k`

  return String(n)
}

/** "model.safetensors" → "model" — display name for checkpoints and LoRAs */
export function stripModelExt(name: string): string {
  return name.replace(/\.(safetensors|pt)$/, '')
}

/** 1 → "1girl", 3 → "3girls" — Danbooru figure-count tag form */
export function figureTag(n: number, word: string): string {
  return n === 1 ? `1${word}` : `${n}${word}s`
}
