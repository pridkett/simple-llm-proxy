import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusBadge from '@/components/StatusBadge.vue'

describe('StatusBadge', () => {
  it('renders "Healthy" for status healthy', () => {
    const wrapper = mount(StatusBadge, { props: { status: 'healthy' } })
    expect(wrapper.text()).toBe('Healthy')
  })

  it('renders "Cooldown" for status cooldown', () => {
    const wrapper = mount(StatusBadge, { props: { status: 'cooldown' } })
    expect(wrapper.text()).toBe('Cooldown')
  })

  it('renders "Unknown" for unrecognised status', () => {
    const wrapper = mount(StatusBadge, { props: { status: 'foobar' } })
    expect(wrapper.text()).toBe('Unknown')
  })

  it('applies green classes for healthy status', () => {
    const wrapper = mount(StatusBadge, { props: { status: 'healthy' } })
    const span = wrapper.find('span')
    expect(span.classes()).toContain('bg-green-50')
    expect(span.classes()).toContain('text-green-700')
  })

  it('applies red classes for cooldown status', () => {
    const wrapper = mount(StatusBadge, { props: { status: 'cooldown' } })
    const span = wrapper.find('span')
    expect(span.classes()).toContain('bg-red-50')
    expect(span.classes()).toContain('text-red-700')
  })

  it('applies gray classes for unknown status', () => {
    const wrapper = mount(StatusBadge, { props: { status: 'unknown' } })
    const span = wrapper.find('span')
    expect(span.classes()).toContain('bg-gray-100')
  })
})
