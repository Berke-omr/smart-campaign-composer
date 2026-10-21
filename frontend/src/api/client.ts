// Thin fetch wrapper: JSON in and out. A non-2xx response becomes an ApiError
// that keeps the server's message and its whole JSON body (a 422 carries
// `details` or a validation `report`, docs/API.md).

export class ApiError extends Error {
  constructor(
    /** HTTP status; 0 when the server could not be reached at all. */
    public readonly status: number,
    message: string,
    /** The parsed JSON body of the error response, if it had one. */
    public readonly body: unknown = null,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/**
 * True when the request never got an answer from the api: the network failed,
 * or nginx answered for an api that is down or restarting (502/503/504).
 */
export function isUnreachable(e: unknown): boolean {
  return (
    e instanceof ApiError &&
    (e.status === 0 || e.status === 502 || e.status === 503 || e.status === 504)
  )
}

export async function requestJSON<T>(
  method: 'GET' | 'POST' | 'PUT',
  path: string,
  body?: unknown,
): Promise<T> {
  const init: RequestInit = { method, headers: { Accept: 'application/json' } }
  if (body !== undefined) {
    init.headers = { Accept: 'application/json', 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    throw new ApiError(0, "Can't reach the server")
  }
  if (!res.ok) {
    let message = `Request failed with status ${res.status}`
    let parsed: unknown = null
    try {
      parsed = await res.json()
      const error = (parsed as { error?: unknown }).error
      if (typeof error === 'string') message = error
    } catch {
      // Body was not JSON (e.g. nginx's 502 page); keep the generic message.
    }
    throw new ApiError(res.status, message, parsed)
  }
  return (await res.json()) as T
}

export function getJSON<T>(path: string): Promise<T> {
  return requestJSON<T>('GET', path)
}
