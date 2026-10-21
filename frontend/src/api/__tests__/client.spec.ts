import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, isUnreachable, requestJSON } from '../client'

function respond(status: number, body: string, contentType = 'application/json'): Response {
  return new Response(body, { status, headers: { 'Content-Type': contentType } })
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('requestJSON', () => {
  it('sends JSON and returns the parsed answer', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(respond(201, '{"id":7}'))
    vi.stubGlobal('fetch', fetchMock)

    await expect(requestJSON('POST', '/api/campaigns', { brief: 'x' })).resolves.toEqual({ id: 7 })
    const [url, init] = fetchMock.mock.calls[0]!
    expect(url).toBe('/api/campaigns')
    expect(init?.method).toBe('POST')
    expect(init?.body).toBe('{"brief":"x"}')
    expect(new Headers(init?.headers).get('Content-Type')).toBe('application/json')
  })

  it('keeps the whole body of a 422 so a validation report can be shown', async () => {
    const body = {
      error: 'invalid content',
      report: { valid: false, errors: [{ field: 'text', code: 'too_long' }] },
    }
    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockResolvedValue(respond(422, JSON.stringify(body))),
    )

    const err = await requestJSON('PUT', '/api/campaigns/1/messages/sms', { text: 'x' }).catch(
      (e: unknown) => e,
    )
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(422)
    expect((err as ApiError).message).toBe('invalid content')
    expect((err as ApiError).body).toEqual(body)
    expect(isUnreachable(err)).toBe(false)
  })

  it("treats nginx's 502 page and a failed network as an unreachable server", async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(respond(502, '<html>Bad Gateway</html>', 'text/html')),
    )
    const bad = await requestJSON('GET', '/api/config').catch((e: unknown) => e)
    expect((bad as ApiError).status).toBe(502)
    expect((bad as ApiError).message).toBe('Request failed with status 502')
    expect(isUnreachable(bad)).toBe(true)

    vi.stubGlobal(
      'fetch',
      vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch')),
    )
    const down = await requestJSON('GET', '/api/config').catch((e: unknown) => e)
    expect((down as ApiError).status).toBe(0)
    expect(isUnreachable(down)).toBe(true)
  })
})
