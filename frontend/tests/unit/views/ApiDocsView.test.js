import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ApiDocsView from '@/views/ApiDocsView.vue'

describe('ApiDocsView', () => {
  let appendChildSpy
  let originalRedoc

  beforeEach(() => {
    originalRedoc = window.Redoc
    // Reset any previous Redoc mock
    delete window.Redoc
    appendChildSpy = vi.spyOn(document.head, 'appendChild').mockImplementation((el) => {
      // Simulate the script loading by triggering onload
      if (el.tagName === 'SCRIPT') {
        window.Redoc = { init: vi.fn() }
        el.onload?.()
      }
      return el
    })
  })

  afterEach(() => {
    window.Redoc = originalRedoc
    appendChildSpy.mockRestore()
    // Remove injected script if any
    const script = document.getElementById('redoc-standalone-script')
    if (script) script.remove()
  })

  it('mounts without throwing', () => {
    expect(() => mount(ApiDocsView)).not.toThrow()
  })

  it('injects the ReDoc CDN script when Redoc is not already loaded', async () => {
    mount(ApiDocsView)
    await flushPromises()

    const injectedScript = appendChildSpy.mock.calls
      .map(([el]) => el)
      .find((el) => el.tagName === 'SCRIPT')

    expect(injectedScript).toBeDefined()
    expect(injectedScript.src).toContain('redoc.standalone.js')
  })

  it('calls Redoc.init with /openapi.json after script loads', async () => {
    mount(ApiDocsView)
    await flushPromises()

    expect(window.Redoc.init).toHaveBeenCalledWith(
      '/openapi.json',
      expect.any(Object),
      expect.any(Object),
    )
  })

  it('does not inject a second script when Redoc is already loaded', async () => {
    window.Redoc = { init: vi.fn() }
    mount(ApiDocsView)
    await flushPromises()

    const scriptCalls = appendChildSpy.mock.calls
      .map(([el]) => el)
      .filter((el) => el.tagName === 'SCRIPT')

    expect(scriptCalls).toHaveLength(0)
    expect(window.Redoc.init).toHaveBeenCalled()
  })

  it('clears the container on unmount', async () => {
    window.Redoc = { init: vi.fn() }
    const wrapper = mount(ApiDocsView)
    await flushPromises()

    // Patch innerHTML setter to detect clearing
    const container = wrapper.element
    const spy = vi.spyOn(container, 'innerHTML', 'set')
    wrapper.unmount()

    expect(spy).toHaveBeenCalledWith('')
  })
})
