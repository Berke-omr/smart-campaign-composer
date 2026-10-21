import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Values } from '@/api/types'
import MessageEditor from '../MessageEditor.vue'

const longest: Values = {
  first_name: 'Muhammed Emin',
  last_name: 'Montgomery-Fitzgerald',
  city: 'İstanbul',
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>

beforeEach(() => {
  fetchMock = vi.fn<typeof fetch>()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

function editor(channel: 'sms' | 'email', initial: Record<string, string>) {
  return mount(MessageEditor, {
    props: { campaignId: 5, channel, initial, longest },
    attachTo: document.body,
  })
}

describe('MessageEditor', () => {
  it('inserts a variable at the caret and keeps typing there', async () => {
    const e = editor('sms', { text: 'Merhaba , hoş geldin' })
    const ta = e.get('textarea').element as HTMLTextAreaElement
    ta.setSelectionRange(8, 8)
    await e.get('textarea').trigger('select')

    await e.get('button[aria-label="Insert first_name"]').trigger('click')
    expect(ta.value).toBe('Merhaba {{first_name}}, hoş geldin')
    expect(ta.selectionStart).toBe(22)
    expect(document.activeElement).toBe(ta)
  })

  it('updates the worst-case counter and the previews as you type', async () => {
    const e = editor('sms', { text: '' })
    await e.get('textarea').setValue('a'.repeat(147) + '{{first_name}}')
    expect(e.get('[data-testid="counter"]').text()).toBe('Near limit160 / 160')
    await e.get('textarea').setValue('a'.repeat(148) + '{{first_name}}')
    expect(e.get('[data-testid="counter"]').text()).toBe('1 over limit161 / 160')

    await e.get('textarea').setValue('Merhaba {{first_name}} {{city}}')
    const previews = e.findAll('.preview-text').map((p) => p.text())
    expect(previews).toEqual(['Merhaba Ayşe İstanbul', 'Merhaba Muhammed Emin İstanbul'])
  })

  it("saves through the backend's validator and reports the stored message", async () => {
    const saved = { message: { state: 'valid', source: 'edited' }, status: 'ready' }
    fetchMock.mockResolvedValue(json(200, saved))
    const e = editor('email', { subject: 'Konu', body: 'Gövde' })
    await e.get('input').setValue('{{first_name}}, sepetin seni bekliyor')
    await e.get('form').trigger('submit')
    await flushPromises()

    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/campaigns/5/messages/email')
    expect(init?.method).toBe('PUT')
    expect(JSON.parse(String(init?.body))).toEqual({
      subject: '{{first_name}}, sepetin seni bekliyor',
      body: 'Gövde',
    })
    expect(e.emitted('saved')).toEqual([[saved]])
  })

  it('shows a rejected edit next to its field and keeps the text', async () => {
    const message =
      'text is 163 characters with the longest values in this audience filled in; the limit is 160. Shorten it by at least 3 characters.'
    fetchMock.mockResolvedValue(
      json(422, {
        error: 'invalid content',
        report: { valid: false, errors: [{ field: 'text', code: 'too_long', message }] },
      }),
    )
    const e = editor('sms', { text: 'a'.repeat(163) })
    await e.get('form').trigger('submit')
    await flushPromises()

    const ta = e.get('textarea')
    expect(ta.attributes('aria-invalid')).toBe('true')
    const errorsId = ta
      .attributes('aria-describedby')!
      .split(' ')
      .find((id) => id.endsWith('errors'))!
    expect(document.getElementById(errorsId)!.textContent).toContain(message)
    expect(e.get('[role="alert"]').text()).toContain('The previous version is kept.')
    expect((ta.element as HTMLTextAreaElement).value).toBe('a'.repeat(163))
    expect(e.emitted('saved')).toBeUndefined()
  })
})
