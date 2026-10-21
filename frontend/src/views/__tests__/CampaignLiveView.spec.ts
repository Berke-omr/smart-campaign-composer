import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Snapshot } from '@/api/types'
import { routes } from '@/router'
import CampaignLiveView from '../CampaignLiveView.vue'

/** EventSource stand-in: the test emits events and failures by hand. */
class FakeEventSource {
  static all: FakeEventSource[] = []
  readyState = 0
  onerror: ((ev: Event) => void) | null = null
  closed = false
  private listeners = new Map<string, ((ev: MessageEvent) => void)[]>()
  constructor(public url: string) {
    FakeEventSource.all.push(this)
  }
  addEventListener(type: string, l: (ev: MessageEvent) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), l])
  }
  close() {
    this.closed = true
    this.readyState = 2
  }
  emit(type: string, data: unknown) {
    this.readyState = 1
    for (const l of this.listeners.get(type) ?? []) {
      l(new MessageEvent(type, { data: JSON.stringify(data) }))
    }
  }
  fail() {
    this.readyState = 2
    this.onerror?.(new Event('error'))
  }
}

// The real snapshot of docs/API.md: an SMS-only campaign about 10 s into sending.
const first: Snapshot = {
  campaign_id: 13,
  status: 'sending',
  approved_at: '2026-10-21T10:16:15.197516Z',
  finished_at: null,
  at: '2026-10-21T10:16:25.255557959Z',
  total: 1226,
  done: 860,
  window: { from: '2026-10-21T10:15:54Z', to: '2026-10-21T10:16:24Z' },
  channels: {
    sms: {
      total: 1226,
      done: 860,
      queued: 266,
      sending: 51,
      retrying: 49,
      sent: 844,
      failed: 16,
      per_second: [...Array(21).fill(0), 65, 93, 93, 92, 90, 92, 92, 94, 92],
      rate: 92,
      limit: 100,
    },
  },
  recent_issues: [
    {
      at: '2026-10-21T10:16:22.042721Z',
      channel: 'sms',
      kind: 'throttled',
      code: 429,
      outcome: 'retry',
      retry_in_ms: 1284,
      attempt: 1,
      error: '429 provider busy',
    },
  ],
}

const later: Snapshot = {
  ...first,
  at: '2026-10-21T10:16:40.000Z',
  done: 1226,
  status: 'completed',
  finished_at: '2026-10-21T10:16:39.000Z',
  channels: {
    sms: {
      ...first.channels.sms!,
      done: 1226,
      queued: 0,
      sending: 0,
      retrying: 0,
      sent: 1209,
      failed: 17,
      per_second: [...Array(28).fill(80), 40, 0],
      rate: 0,
    },
  },
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  FakeEventSource.all = []
  vi.stubGlobal('EventSource', FakeEventSource)
  fetchMock = vi.fn<typeof fetch>(() =>
    Promise.resolve(
      json(200, {
        id: 13,
        name: 'New season — cart adders',
        status: 'sending',
        channels: ['sms'],
        approved_at: first.approved_at,
      }),
    ),
  )
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

async function setup() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/campaigns/13/live')
  const wrapper = mount(CampaignLiveView, {
    props: { id: 13 },
    global: { plugins: [router] },
    attachTo: document.body,
  })
  await flushPromises()
  return { wrapper, router }
}

const es = () => FakeEventSource.all[FakeEventSource.all.length - 1]!

describe('CampaignLiveView', () => {
  it('shows a snapshot: progress, states, rate, chart and issues', async () => {
    const { wrapper } = await setup()
    expect(es().url).toBe('/api/campaigns/13/events')
    es().emit('delivery.snapshot', first)
    await flushPromises()

    expect(wrapper.get('[data-testid="percent"]').text()).toBe('70%')
    expect(wrapper.get('[data-testid="done"]').text()).toBe('860 of 1,226 done')
    expect(wrapper.get('[role="progressbar"]').attributes('aria-valuetext')).toBe(
      '860 of 1,226 done',
    )
    expect(wrapper.get('[data-testid="rate"]').text()).toBe('92 msg/s')
    expect(wrapper.findAll('[data-status]').map((l) => l.text().replace(/\s+/g, ' '))).toEqual([
      'Sent 844',
      'Sending 51',
      'Queued 266',
      'Retrying 49',
      'Failed 16',
    ])
    expect(wrapper.get('svg[role="img"]').attributes('aria-label')).toContain(
      'SMS 92 per second at the end, highest 94',
    )
    // 3.2 s before the snapshot's own time.
    expect(wrapper.get('[data-testid="issue"]').text()).toContain('Provider busy · retrying in 1s')
    expect(wrapper.get('[data-testid="issue"]').text()).toContain('3s')
    expect(wrapper.get('.connection').text()).toBe('Live')
  })

  it('keeps the last numbers while reconnecting, then replaces them', async () => {
    const { wrapper } = await setup()
    es().emit('delivery.snapshot', first)
    await flushPromises()

    es().fail() // nginx answered 502: the browser gives up on this EventSource
    await flushPromises()
    expect(wrapper.get('.connection').text()).toBe('Reconnecting…')
    expect(wrapper.text()).toContain('Showing the last update from')
    expect(wrapper.get('[data-testid="done"]').text()).toBe('860 of 1,226 done')

    await vi.advanceTimersByTimeAsync(1300) // backoff, then the campaign check, then a new stream
    expect(FakeEventSource.all).toHaveLength(2)
    es().emit('delivery.snapshot', later)
    await flushPromises()
    expect(wrapper.get('.connection').text()).not.toBe('Reconnecting…')
    expect(wrapper.get('[data-testid="done"]').text()).toBe('1,226 of 1,226 done')
    // The new series replaced the old one.
    expect(wrapper.get('svg[role="img"]').attributes('aria-label')).toContain(
      'SMS 0 per second at the end, highest 80',
    )
  })

  it('ends with the final state and closes the stream on delivery.done', async () => {
    const { wrapper } = await setup()
    es().emit('delivery.snapshot', later)
    es().emit('delivery.done', { campaign_id: 13, status: 'completed' })
    await flushPromises()

    expect(es().closed).toBe(true)
    expect(wrapper.get('[data-testid="status-pill"]').text()).toBe('Completed')
    expect(wrapper.get('.connection').text()).toBe('Final')
    expect(wrapper.get('[data-testid="percent"]').text()).toBe('100%')
    expect(wrapper.text()).toContain('last 30 seconds of sending')
    await vi.advanceTimersByTimeAsync(60_000)
    expect(FakeEventSource.all).toHaveLength(1)
  })

  it('marks the numbers as old when a snapshot could not be read', async () => {
    const { wrapper } = await setup()
    es().emit('delivery.snapshot', first)
    es().emit('delivery.error', { message: 'database is restarting' })
    await flushPromises()
    expect(wrapper.text()).toContain('Waiting for fresh data')
    es().emit('delivery.snapshot', first)
    await flushPromises()
    expect(wrapper.text()).not.toContain('Waiting for fresh data')
  })

  it('sends a campaign that is not approved yet back to its draft', async () => {
    fetchMock.mockResolvedValue(json(200, { id: 13, status: 'ready', channels: ['sms'] }))
    const { router } = await setup()
    expect(router.currentRoute.value.fullPath).toBe('/campaigns/13')
    expect(FakeEventSource.all).toHaveLength(0)
  })
})
