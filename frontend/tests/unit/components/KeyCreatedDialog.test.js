import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import KeyCreatedDialog from '@/components/KeyCreatedDialog.vue'

enableAutoUnmount(afterEach)
afterEach(() => vi.unstubAllGlobals())

const secret = 'sk-app-test-creation-value'
describe('KeyCreatedDialog', () => {
  it('copies the whole key and dismisses only when Done is clicked', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const wrapper = mount(KeyCreatedDialog, { props: { value: secret } })
    await wrapper.get('button[autofocus]').trigger('click')
    expect(writeText).toHaveBeenCalledWith(secret)
    expect(wrapper.text()).toContain('Copied')
    await wrapper.get('dialog').trigger('cancel')
    expect(wrapper.emitted('close')).toBeUndefined()
    await wrapper.findAll('button')[1].trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('keeps the key available for manual copy if clipboard access fails', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('Denied')) } })
    const wrapper = mount(KeyCreatedDialog, { props: { value: secret } })
    await wrapper.get('button[autofocus]').trigger('click')
    expect(wrapper.get('code').text()).toBe(secret)
    expect(wrapper.get('[role="alert"]').text()).toContain('copy it manually')
  })
})
