import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { useColorScheme } from '@/composables/useColorScheme.js'

enableAutoUnmount(afterEach)
afterEach(() => vi.unstubAllGlobals())

function preference(dark) {
  const media = new EventTarget()
  media.matches = dark
  vi.stubGlobal('matchMedia', vi.fn(() => media))
  return media
}

const Probe = {
  setup: useColorScheme,
  template: '<span>{{ isDark ? "dark" : "light" }}</span>',
}

describe('browser color preference', () => {
  it.each([true, false])('uses the initial browser preference: %s', dark => {
    preference(dark)
    const wrapper = mount(Probe)
    expect(window.matchMedia).toHaveBeenCalledWith('(prefers-color-scheme: dark)')
    expect(wrapper.text()).toBe(dark ? 'dark' : 'light')
  })

  it('responds to changes in both directions without storing a preference', async () => {
    const media = preference(false)
    const wrapper = mount(Probe)
    media.matches = true
    media.dispatchEvent(new Event('change'))
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toBe('dark')
    media.matches = false
    media.dispatchEvent(new Event('change'))
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toBe('light')
    expect(localStorage.length).toBe(0)
  })

  it('removes its listener when unmounted', () => {
    const media = preference(true)
    const add = vi.spyOn(media, 'addEventListener')
    const remove = vi.spyOn(media, 'removeEventListener')
    const wrapper = mount(Probe)
    const listener = add.mock.calls[0][1]
    wrapper.unmount()
    expect(remove).toHaveBeenCalledWith('change', listener)
  })

  it('falls back to light if matchMedia is unavailable', () => {
    vi.stubGlobal('matchMedia', undefined)
    expect(mount(Probe).text()).toBe('light')
  })
})
