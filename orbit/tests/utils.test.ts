import { describe, expect, it, vi } from 'vitest'
import { apiErrorMessage } from '~/utils/api-error'
import { toggledSet, toggleInPlace } from '~/utils/collections'
import { downloadImage } from '~/utils/download'
import { figureTag, formatCount, stripModelExt } from '~/utils/format'

describe('formatCount', () => {
  it('formats millions with one decimal', () => {
    expect(formatCount(1_300_000)).toBe('1.3M')
    expect(formatCount(5_000_000)).toBe('5.0M')
  })
  it('formats thousands rounded', () => {
    expect(formatCount(22_400)).toBe('22k')
    expect(formatCount(999_600)).toBe('1000k')
  })
  it('passes small numbers through and hides zero', () => {
    expect(formatCount(950)).toBe('950')
    expect(formatCount(0)).toBe('')
  })
})

describe('stripModelExt', () => {
  it('strips safetensors and pt, once, at the end only', () => {
    expect(stripModelExt('noob-v1.safetensors')).toBe('noob-v1')
    expect(stripModelExt('style.pt')).toBe('style')
    expect(stripModelExt('safetensors-guide.txt')).toBe('safetensors-guide.txt')
  })
})

describe('figureTag', () => {
  it('singular at 1, plural otherwise', () => {
    expect(figureTag(1, 'girl')).toBe('1girl')
    expect(figureTag(3, 'girl')).toBe('3girls')
    expect(figureTag(2, 'boy')).toBe('2boys')
  })
})

describe('toggledSet', () => {
  it('adds when absent, removes when present, never mutates the input', () => {
    const base = new Set(['a'])
    const added = toggledSet(base, 'b')
    expect([...added].sort()).toEqual(['a', 'b'])
    const removed = toggledSet(added, 'a')
    expect([...removed]).toEqual(['b'])
    expect([...base]).toEqual(['a'])
    expect(added).not.toBe(base)
  })
})

describe('toggleInPlace', () => {
  it('mutates the array both directions', () => {
    const list = ['full_body']
    toggleInPlace(list, 'cowboy_shot')
    expect(list).toEqual(['full_body', 'cowboy_shot'])
    toggleInPlace(list, 'full_body')
    expect(list).toEqual(['cowboy_shot'])
  })
})

describe('apiErrorMessage', () => {
  it('extracts the core error body', () => {
    expect(apiErrorMessage({ data: { error: 'image too large' } })).toBe('image too large')
  })
  it('falls back on any other shape', () => {
    expect(apiErrorMessage(new Error('boom'), 'upload failed')).toBe('upload failed')
    expect(apiErrorMessage(undefined, 'upload failed')).toBe('upload failed')
    expect(apiErrorMessage({ data: { error: '' } }, 'upload failed')).toBe('upload failed')
  })
})

describe('downloadImage', () => {
  it('fetches, clicks a temp anchor with the filename, and cleans up', async () => {
    const blob = new Blob(['png-bytes'], { type: 'image/png' })
    vi.stubGlobal('fetch', vi.fn(async () => ({ blob: async () => blob })))
    URL.createObjectURL = vi.fn(() => 'blob:fake')
    URL.revokeObjectURL = vi.fn()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    const ok = await downloadImage('http://core/images/x.png', 'pulsar-42.png')

    expect(ok).toBe(true)
    // no-store: an <img>-cached copy stored without CORS headers would fail
    // a cors-mode fetch, so downloads must always bypass the cache
    expect(fetch).toHaveBeenCalledWith('http://core/images/x.png', { cache: 'no-store' })
    expect(click).toHaveBeenCalledOnce()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:fake')
    expect(document.querySelector('a[download]')).toBeNull() // removed after click
    vi.unstubAllGlobals()
    click.mockRestore()
  })

  it('returns false instead of throwing on network failure', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new TypeError('network'))))
    await expect(downloadImage('http://core/x.png', 'x.png')).resolves.toBe(false)
    vi.unstubAllGlobals()
  })
})
