import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { CampaignView, MessageView } from '@/api/types'
import ConfirmSendModal from '@/components/campaign/ConfirmSendModal.vue'
import { routes } from '@/router'
import CampaignDraftView from '../CampaignDraftView.vue'

const report = (lengths: object[]) => ({
  valid: true,
  errors: [],
  lengths,
  longest_values: {},
  preview: { sample_user: {}, sample: {}, worst_case: {} },
})

const sms: MessageView = {
  state: 'valid',
  source: 'generated',
  attempts: 1,
  message: { text: 'Merhaba {{first_name}}, sepetin seni bekliyor!' },
  report: report([{ field: 'text', template_length: 46, worst_case_length: 45, limit: 160 }]),
  problems: [],
  updated_at: '',
} as unknown as MessageView

const email: MessageView = {
  ...sms,
  message: { subject: '{{first_name}}, %30 indirim', body: 'Merhaba {{first_name}},' },
} as MessageView

// A ready campaign with both messages stored, so no draft stream is opened.
const campaign = {
  id: 3,
  name: 'Cart reminder — İstanbul',
  status: 'ready',
  brief: 'Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul’daki kullanıcılara…',
  tone: 'friendly',
  channels: ['sms', 'email'],
  rule: { match: 'all', conditions: [] },
  unsupported_criteria: [],
  audience: {
    total: 177,
    sms_reachable: 99,
    email_reachable: 133,
    sms_excluded: 78,
    email_excluded: 44,
    channel_messages: 232,
    description: { summary: '', lines: ['City is İstanbul'] },
    longest_values: {
      sms: { first_name: 'Muhammed Emin', last_name: 'Montgomery-Fitzgerald', city: 'İstanbul' },
      email: { first_name: 'Muhammed Emin', last_name: 'Montgomery-Fitzgerald', city: 'İstanbul' },
    },
  },
  messages: { sms, email },
} as unknown as CampaignView

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>
let approve: () => void = () => {}

beforeEach(() => {
  fetchMock = vi.fn<typeof fetch>((input, init) => {
    if (init?.method === 'POST') {
      // Held until the test lets it answer, like a slow network.
      return new Promise((resolve) => {
        approve = () =>
          resolve(
            json(200, {
              campaign_id: 3,
              status: 'sending',
              approved_at: '',
              deliveries: { sms: 99, email: 133, total: 232 },
            }),
          )
      })
    }
    return Promise.resolve(json(200, campaign))
  })
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('EventSource', vi.fn())
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

async function setup() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/campaigns/3')
  const wrapper = mount(CampaignDraftView, {
    props: { id: 3 },
    global: { plugins: [router] },
    attachTo: document.body,
  })
  await flushPromises()
  return { wrapper, router }
}

const button = (w: Awaited<ReturnType<typeof setup>>['wrapper'], text: string) =>
  w.findAll('button').find((b) => b.text() === text)!

describe('CampaignDraftView', () => {
  it('shows stored messages without opening a draft stream', async () => {
    const { wrapper } = await setup()
    expect(EventSource).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-testid="state"]').map((s) => s.text())).toEqual([
      'Valid',
      'Valid',
    ])
    expect(wrapper.get('[aria-current="step"]').text()).toBe('Review')
  })

  it('blocks approval while a message is being edited', async () => {
    const { wrapper } = await setup()
    expect(button(wrapper, 'Review & approve').attributes('disabled')).toBeUndefined()
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Edit')!
      .trigger('click')
    expect(button(wrapper, 'Review & approve').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('Save or cancel your edit first.')
  })

  it('confirms with the final numbers and approves once, however often Send is clicked', async () => {
    const { wrapper, router } = await setup()
    await button(wrapper, 'Review & approve').trigger('click')
    const dialog = wrapper.get('dialog')
    expect(dialog.attributes('open')).toBeDefined()
    expect(dialog.text()).toContain('177 people')
    expect(dialog.findAll('[data-testid="confirm-channel"]').map((c) => c.text())).toEqual([
      expect.stringContaining('SMS · 99 messages · 78 without consent left out'),
      expect.stringContaining('Email · 133 messages · 44 without consent left out'),
    ])
    expect(dialog.get('[data-testid="confirm-total"]').text()).toBe('232 messages in total')
    expect(dialog.text()).toContain("This can't be undone")
    expect(wrapper.get('[aria-current="step"]').text()).toBe('Send')

    const send = button(wrapper, 'Send messages')
    await send.trigger('click')
    await send.trigger('click')
    await send.trigger('click')
    expect(send.attributes('disabled')).toBeDefined()
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)

    approve()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/campaigns/3/live')
  })

  it('approves once even if the dialog confirms twice before the button updates', async () => {
    const { wrapper } = await setup()
    await button(wrapper, 'Review & approve').trigger('click')
    const modal = wrapper.findComponent(ConfirmSendModal)
    modal.vm.$emit('confirm')
    modal.vm.$emit('confirm')
    await flushPromises()
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
    approve()
    await flushPromises()
  })

  it('keeps an approval error in the dialog', async () => {
    fetchMock.mockImplementation((input, init) =>
      Promise.resolve(
        init?.method === 'POST'
          ? json(422, {
              error: 'the sms message no longer fits this audience; edit it and approve again',
              channel: 'sms',
              report: {
                errors: [{ field: 'text', code: 'too_long', message: 'text is 170 characters' }],
              },
            })
          : json(200, campaign),
      ),
    )
    const { wrapper, router } = await setup()
    await button(wrapper, 'Review & approve').trigger('click')
    await button(wrapper, 'Send messages').trigger('click')
    await flushPromises()

    const alert = wrapper.get('dialog [role="alert"]')
    expect(alert.text()).toContain('The sms message no longer fits this audience')
    expect(alert.text()).toContain('text is 170 characters')
    expect(wrapper.get('dialog').attributes('open')).toBeDefined()
    expect(router.currentRoute.value.fullPath).toBe('/campaigns/3')
  })
})
