import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ComposerView from '../ComposerView.vue'
import { routes } from '@/router'

const examples = {
  examples: [
    {
      id: 'new-season-sms',
      title: 'New season — SMS',
      brief: 'Son 60 günde sepete ürün ekleyen kadın kullanıcılara yeni sezon ürünlerini duyur.',
      channels: ['sms'],
      tone: 'playful',
    },
  ],
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>
/** Answers for every request except the recent-campaigns list, in order. */
let answers: (Response | Error)[]
let recent: unknown[]

/** The calls other than GET /api/campaigns (the recent list). */
const calls = () =>
  fetchMock.mock.calls.filter(([url, init]) => !(url === '/api/campaigns' && !init?.body))

async function setup() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/')
  const wrapper = mount(ComposerView, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return { wrapper, router }
}

beforeEach(() => {
  answers = []
  recent = []
  fetchMock = vi.fn<typeof fetch>((url, init) => {
    if (url === '/api/campaigns' && !init?.body) {
      return Promise.resolve(json(200, { campaigns: recent }))
    }
    const next = answers.shift() ?? json(500, { error: 'internal error' })
    return next instanceof Error ? Promise.reject(next) : Promise.resolve(next)
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('ComposerView', () => {
  it('sends the brief, the channels and the tone, then opens the draft', async () => {
    answers = [json(201, { id: 7 })]
    const { wrapper, router } = await setup()
    const generate = wrapper.get('button[type="submit"]')
    expect(generate.attributes('disabled')).toBeDefined()

    await wrapper.get('textarea').setValue('İstanbul’daki kullanıcılara %30 indirim')
    await wrapper.get('select').setValue('urgent')
    expect(generate.attributes('disabled')).toBeUndefined()
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const [url, init] = calls()[0]!
    expect(url).toBe('/api/campaigns')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({
      brief: 'İstanbul’daki kullanıcılara %30 indirim',
      channels: ['sms', 'email'],
      tone: 'urgent',
    })
    expect(router.currentRoute.value.fullPath).toBe('/campaigns/7')
  })

  it('needs at least one channel', async () => {
    const { wrapper } = await setup()
    await wrapper.get('textarea').setValue('Bir brief')
    const toggles = wrapper.findAll('button[aria-pressed]')
    expect(toggles.map((t) => [t.text(), t.attributes('aria-pressed')])).toEqual([
      ['SMS', 'true'],
      ['Email', 'true'],
    ])
    await toggles[0]!.trigger('click')
    await toggles[1]!.trigger('click')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('Choose at least one channel.')
  })

  it('counts the brief limit in characters, not UTF-16 units', async () => {
    const { wrapper } = await setup()
    await wrapper.get('textarea').setValue('𝟑'.repeat(2000)) // 4,000 UTF-16 units
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('textarea').setValue('𝟑'.repeat(2001))
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('The brief is 2,001 characters; the limit is 2,000.')
  })

  it('fills the form from the brief library', async () => {
    answers = [json(200, examples)]
    const { wrapper } = await setup()
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Brief library')!
      .trigger('click')
    await flushPromises()
    expect(calls()[0]![0]).toBe('/api/briefs/examples')

    await wrapper.get('.example').trigger('click')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe(
      examples.examples[0]!.brief,
    )
    expect(
      wrapper.findAll('button[aria-pressed]').map((t) => t.attributes('aria-pressed')),
    ).toEqual(['true', 'false'])
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('playful')
    expect(wrapper.get('dialog').attributes('open')).toBeUndefined()
  })

  it('shows why no audience could be built, and keeps the brief', async () => {
    answers = [
      json(422, {
        error: 'could not build an audience from this brief (3 attempts)',
        details: ['condition 1: unknown field "gender"'],
      }),
    ]
    const { wrapper, router } = await setup()
    await wrapper.get('textarea').setValue('kadın kullanıcılar')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain('Could not build an audience from this brief (3 attempts)')
    expect(alert.text()).toContain('condition 1: unknown field "gender"')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe(
      'kadın kullanıcılar',
    )
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it('offers to try again when the server cannot be reached', async () => {
    answers = [new TypeError('Failed to fetch'), json(201, { id: 9 })]
    const { wrapper, router } = await setup()
    await wrapper.get('textarea').setValue('Bir brief')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain("Can't reach the server")
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Try again')!
      .trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/campaigns/9')
  })

  it('lists the four most recent campaigns, each opening the right screen', async () => {
    const summary = (id: number, status: string) => ({
      id,
      name: `Campaign ${id}`,
      status,
      channels: ['sms'],
      audience: { total: 1000 + id },
      created_at: '',
    })
    recent = [
      summary(5, 'sending'),
      summary(4, 'completed'),
      summary(3, 'ready'),
      summary(2, 'draft'),
      summary(1, 'failed'),
    ]
    const { wrapper } = await setup()
    const cards = wrapper.findAll('[data-testid="campaign-card"]')
    expect(cards.map((c) => c.attributes('href'))).toEqual([
      '/campaigns/5/live',
      '/campaigns/4/live',
      '/campaigns/3',
      '/campaigns/2',
    ])
    expect(cards[0]!.text()).toContain('1,005 people')
    expect(cards[0]!.text()).toContain('In progress')
    expect(cards[0]!.text()).not.toMatch(/\d+%/)
    expect(cards[1]!.text()).toContain('100% done')
    expect(cards[2]!.text()).toContain('Not sent yet')
    expect(wrapper.findAll('a').some((a) => a.text() === 'All campaigns')).toBe(true)
  })
})
