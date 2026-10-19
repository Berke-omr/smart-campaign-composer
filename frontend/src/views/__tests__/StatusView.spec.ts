import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import StatusView from '../StatusView.vue'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('StatusView', () => {
  it('shows user counts and opt-in shares from the API', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValue(jsonResponse(200, { total: 5000, sms_opt_in: 3156, email_opt_in: 3714 }))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(StatusView)
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/stats/users', expect.anything())
    const values = wrapper.findAll('[data-testid="tile-value"]').map((el) => el.text())
    expect(values).toEqual(['5,000', '3,156', '3,714'])
    expect(wrapper.text()).toContain('63% opted in')
    expect(wrapper.text()).toContain('74% opted in')
  })

  it('shows the server error message and recovers on retry', async () => {
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse(500, { error: 'internal error' }))
      .mockResolvedValueOnce(jsonResponse(200, { total: 10, sms_opt_in: 5, email_opt_in: 7 }))
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(StatusView)
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('internal error')

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="tile-value"]').map((el) => el.text())).toEqual([
      '10',
      '5',
      '7',
    ])
  })

  it('does not divide by zero on an empty user base', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(jsonResponse(200, { total: 0, sms_opt_in: 0, email_opt_in: 0 })),
    )

    const wrapper = mount(StatusView)
    await flushPromises()

    expect(wrapper.text()).not.toContain('NaN')
    expect(wrapper.text()).toContain('— opted in')
  })
})
