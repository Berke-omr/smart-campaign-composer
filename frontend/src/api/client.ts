// Thin fetch wrapper: JSON in/out, and non-2xx responses become ApiError with
// the server's {"error": "..."} message when there is one.

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export async function getJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { Accept: 'application/json', ...init?.headers },
  })
  if (!res.ok) {
    let message = `Request failed with status ${res.status}`
    try {
      const body = (await res.json()) as { error?: unknown }
      if (typeof body.error === 'string') message = body.error
    } catch {
      // Body was not JSON (e.g. a proxy error page); keep the generic message.
    }
    throw new ApiError(res.status, message)
  }
  return (await res.json()) as T
}
