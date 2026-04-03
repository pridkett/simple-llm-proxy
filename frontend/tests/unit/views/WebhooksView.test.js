import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHashHistory } from 'vue-router'
import { nextTick } from 'vue'

// Mock the api client
vi.mock('@/api/client.js', () => ({
  api: {
    webhooks: vi.fn(),
    createWebhook: vi.fn(),
    updateWebhook: vi.fn(),
    deleteWebhook: vi.fn(),
  },
}))

import WebhooksView from '@/views/WebhooksView.vue'
import { api } from '@/api/client.js'
import LoadingSpinner from '@/components/LoadingSpinner.vue'
import ErrorAlert from '@/components/ErrorAlert.vue'

const mockWebhooksData = {
  webhooks: [
    {
      id: -1,
      url: 'https://yaml-hook.example.com/webhook',
      events: ['provider_failover', 'budget_exhausted'],
      enabled: true,
      source: 'yaml',
      read_only: true,
    },
    {
      id: 5,
      url: 'https://ui-hook.example.com/webhook',
      events: ['pool_cooldown'],
      enabled: true,
      source: 'ui',
      read_only: false,
      created_at: '2026-03-31T14:30:00Z',
    },
  ],
}

function makeRouter() {
  return createRouter({
    history: createWebHashHistory(),
    routes: [{ path: '/webhooks', component: WebhooksView }],
  })
}

describe('WebhooksView', () => {
  beforeEach(() => {
    vi.mocked(api.webhooks).mockReset()
    vi.mocked(api.createWebhook).mockReset()
    vi.mocked(api.updateWebhook).mockReset()
    vi.mocked(api.deleteWebhook).mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows LoadingSpinner while fetching', async () => {
    // Never resolve to keep it in loading state
    vi.mocked(api.webhooks).mockReturnValue(new Promise(() => {}))
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await nextTick()
    expect(wrapper.findComponent(LoadingSpinner).exists()).toBe(true)
  })

  it('renders webhook table with URL, events, source badge after successful fetch', async () => {
    vi.mocked(api.webhooks).mockResolvedValue(mockWebhooksData)
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await flushPromises()

    // Both webhook URLs should be in the table
    expect(wrapper.text()).toContain('yaml-hook.example.com')
    expect(wrapper.text()).toContain('ui-hook.example.com')
    // Events should be visible
    expect(wrapper.text()).toContain('provider_failover')
    expect(wrapper.text()).toContain('pool_cooldown')
    // Source badges
    expect(wrapper.text()).toContain('YAML')
    expect(wrapper.text()).toContain('UI')
  })

  it('YAML webhooks show "YAML" badge and no Edit or Delete buttons', async () => {
    vi.mocked(api.webhooks).mockResolvedValue(mockWebhooksData)
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await flushPromises()

    // Find all table rows
    const rows = wrapper.findAll('tr')
    // Find the YAML webhook row (contains yaml-hook.example.com)
    const yamlRow = rows.find((r) => r.text().includes('yaml-hook.example.com'))
    expect(yamlRow).toBeDefined()
    expect(yamlRow.text()).toContain('YAML')

    // YAML row should NOT have Edit or Delete buttons
    const buttons = yamlRow.findAll('button')
    const editBtn = buttons.find((b) => b.text().trim() === 'Edit')
    const deleteBtn = buttons.find((b) => b.text().trim() === 'Delete')
    expect(editBtn).toBeUndefined()
    expect(deleteBtn).toBeUndefined()
  })

  it('UI webhooks show "UI" badge with Edit and Delete buttons visible', async () => {
    vi.mocked(api.webhooks).mockResolvedValue(mockWebhooksData)
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await flushPromises()

    // Find the UI webhook row
    const rows = wrapper.findAll('tr')
    const uiRow = rows.find((r) => r.text().includes('ui-hook.example.com'))
    expect(uiRow).toBeDefined()
    expect(uiRow.text()).toContain('UI')

    // UI row should have Edit and Delete buttons
    const buttons = uiRow.findAll('button')
    const editBtn = buttons.find((b) => b.text().trim() === 'Edit')
    const deleteBtn = buttons.find((b) => b.text().trim() === 'Delete')
    expect(editBtn).toBeDefined()
    expect(deleteBtn).toBeDefined()
  })

  it('shows ErrorAlert when fetch fails', async () => {
    vi.mocked(api.webhooks).mockRejectedValue(new Error('server error'))
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.findComponent(ErrorAlert).exists()).toBe(true)
  })

  it('shows empty state "No webhooks configured" when webhooks array is empty', async () => {
    vi.mocked(api.webhooks).mockResolvedValue({ webhooks: [] })
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('No webhooks configured')
  })

  it('clicking "Add Webhook" button expands inline form with URL, Events, Secret, Enabled fields', async () => {
    vi.mocked(api.webhooks).mockResolvedValue(mockWebhooksData)
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await flushPromises()

    // Find and click "Add Webhook" button
    const addBtn = wrapper.findAll('button').find((b) => b.text().trim() === 'Add Webhook')
    expect(addBtn).toBeDefined()
    await addBtn.trigger('click')
    await nextTick()

    // Form should appear with URL input, event checkboxes, secret input, enabled checkbox
    const inputs = wrapper.findAll('input')
    const urlInput = inputs.find((i) => i.attributes('placeholder') === 'https://example.com/webhook')
    expect(urlInput).toBeDefined()

    // Event checkboxes
    expect(wrapper.text()).toContain('Provider Failover')
    expect(wrapper.text()).toContain('Budget Exhausted')
    expect(wrapper.text()).toContain('Pool Cooldown')

    // Secret field (password type)
    const secretInput = inputs.find((i) => i.attributes('type') === 'password')
    expect(secretInput).toBeDefined()

    // Save and Discard buttons
    expect(wrapper.text()).toContain('Save Webhook')
    expect(wrapper.text()).toContain('Discard Changes')
  })

  it('clicking "Delete" replaces button with "Delete?" confirmation and Yes/No buttons', async () => {
    vi.mocked(api.webhooks).mockResolvedValue(mockWebhooksData)
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await flushPromises()

    // Find the UI webhook row and its Delete button
    const rows = wrapper.findAll('tr')
    const uiRow = rows.find((r) => r.text().includes('ui-hook.example.com'))
    const deleteBtn = uiRow.findAll('button').find((b) => b.text().trim() === 'Delete')
    expect(deleteBtn).toBeDefined()

    await deleteBtn.trigger('click')
    await nextTick()

    // After clicking delete, confirmation should appear (consistent with TeamsView/KeysView)
    expect(wrapper.text()).toContain('Delete?')
    expect(wrapper.text()).toContain('Yes')
    expect(wrapper.text()).toContain('No')
  })

  it('"Add Webhook" button exists in page header', async () => {
    vi.mocked(api.webhooks).mockResolvedValue(mockWebhooksData)
    const router = makeRouter()
    await router.push('/webhooks')
    const wrapper = mount(WebhooksView, { global: { plugins: [router] } })
    await flushPromises()

    // Page header should contain heading and Add Webhook button
    expect(wrapper.text()).toContain('Webhooks')
    const addBtn = wrapper.findAll('button').find((b) => b.text().trim() === 'Add Webhook')
    expect(addBtn).toBeDefined()
  })
})
