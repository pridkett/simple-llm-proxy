import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ErrorAlert from '@/components/ErrorAlert.vue'

describe('ErrorAlert', () => {
  it('renders the title', () => {
    const wrapper = mount(ErrorAlert, { props: { title: 'Something went wrong' } })
    expect(wrapper.text()).toContain('Something went wrong')
  })

  it('renders the message when provided', () => {
    const wrapper = mount(ErrorAlert, {
      props: { title: 'Error', message: 'Network failure' },
    })
    expect(wrapper.text()).toContain('Network failure')
  })

  it('does not render message element when message is empty', () => {
    const wrapper = mount(ErrorAlert, { props: { title: 'Error', message: '' } })
    expect(wrapper.find('p').exists()).toBe(false)
  })

  it('uses default title when none provided', () => {
    const wrapper = mount(ErrorAlert, {})
    expect(wrapper.text()).toContain('An error occurred')
  })
})
