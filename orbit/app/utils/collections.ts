// Selection/toggle helpers shared across orbit. Auto-imported by Nuxt.

/** Immutable Set toggle — returns a new Set so `ref<Set>` reactivity fires. */
export function toggledSet<T>(set: Set<T>, value: T): Set<T> {
  const next = new Set(set)
  if (next.has(value)) next.delete(value)
  else next.add(value)

  return next
}

/** In-place array toggle — for reactive arrays mutated where they live. */
export function toggleInPlace<T>(list: T[], value: T): void {
  const i = list.indexOf(value)
  if (i === -1) list.push(value)
  else list.splice(i, 1)
}
