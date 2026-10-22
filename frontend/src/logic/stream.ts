// A server-sent-events stream that comes back by itself (DECISIONS.md D25,
// docs/API.md "Reconnecting").
//
// EventSource handles the easy case: when an open stream simply ends (the api
// shut down), it reconnects after the server's `retry: 3000`. But when an
// answer is not 200 (nginx's 502 while the api restarts, or a 404/409), it
// gives up for good: readyState becomes CLOSED and it never tries again. It
// does not tell us the status code either. So on CLOSED we wait (backoff with
// jitter), let the caller check what happened (e.g. GET the campaign), and
// open a new EventSource unless the caller says stop.
//
// The stream is over only after its done event (draft.done, delivery.done);
// then we close it ourselves, because EventSource would reopen a stream that
// ends. Optionally, silence for longer than `stallMs` counts as a lost
// connection (a laptop waking up, a half-open TCP connection).

/** What the screen shows about the connection. */
export type ConnectionState = 'connecting' | 'live' | 'reconnecting' | 'closed'

/** The part of EventSource we use; tests pass a fake. */
export interface EventSourceLike {
  readonly readyState: number
  onerror: ((ev: Event) => void) | null
  addEventListener(type: string, listener: (ev: MessageEvent) => void): void
  close(): void
}

export type EventSourceFactory = (url: string) => EventSourceLike

/** EventSource.CLOSED; the constant is not available in every test environment. */
const CLOSED = 2

export interface Backoff {
  firstMs: number
  maxMs: number
  /** ± share of the delay, so many browsers do not come back at the same instant. */
  jitter: number
}

export const DEFAULT_BACKOFF: Backoff = { firstMs: 1000, maxMs: 10_000, jitter: 0.2 }

/** Delay before reconnect number `failures` (0-based): 1, 2, 4, 8, 10, 10 … s, ± jitter. */
export function backoffDelay(failures: number, b: Backoff, random: () => number): number {
  const base = Math.min(b.maxMs, b.firstMs * 2 ** failures)
  return Math.round(base * (1 + b.jitter * (2 * random() - 1)))
}

export interface StreamOptions {
  url: string
  /** Handlers of named events, called with the event's parsed JSON. */
  handlers: Record<string, (data: unknown) => void>
  /** The event that ends the stream for good. */
  doneEvent: string
  onState: (state: ConnectionState) => void
  /** After the browser gave up (CLOSED): "retry" opens the stream again, "stop" ends it. */
  check?: () => Promise<'retry' | 'stop'>
  /** Reconnect when no event arrived for this long; 0 or absent turns it off. */
  stallMs?: number
  backoff?: Backoff
  createEventSource?: EventSourceFactory
  random?: () => number
}

export interface StreamHandle {
  /** Stops the stream and every timer; nothing is reopened afterwards. */
  close(): void
  /** Skips the remaining wait and reconnects now (the "Try now" button). */
  retryNow(): void
}

export function openStream(o: StreamOptions): StreamHandle {
  const create = o.createEventSource ?? ((url: string) => new EventSource(url))
  const backoff = o.backoff ?? DEFAULT_BACKOFF
  const random = o.random ?? Math.random
  const names = new Set([...Object.keys(o.handlers), o.doneEvent])

  let source: EventSourceLike | null = null
  let failures = 0
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let stallTimer: ReturnType<typeof setTimeout> | null = null
  let stopped = false
  // Bumped by every scheduled or forced reconnect, so a check still awaiting
  // its answer cannot open a second connection after "Try now".
  let generation = 0

  function clearTimers() {
    if (retryTimer) clearTimeout(retryTimer)
    if (stallTimer) clearTimeout(stallTimer)
    retryTimer = stallTimer = null
  }

  function dropSource() {
    source?.close()
    source = null
  }

  function stop() {
    stopped = true
    clearTimers()
    dropSource()
  }

  function armStall() {
    if (!o.stallMs) return
    if (stallTimer) clearTimeout(stallTimer)
    stallTimer = setTimeout(() => {
      stallTimer = null
      if (stopped) return
      dropSource()
      o.onState('reconnecting')
      scheduleReconnect()
    }, o.stallMs)
  }

  function connect() {
    if (stopped) return
    const es = create(o.url)
    source = es
    for (const name of names) {
      es.addEventListener(name, (ev) => {
        if (source !== es || stopped) return // a connection we already replaced
        failures = 0
        armStall()
        o.onState('live')
        let data: unknown
        try {
          data = JSON.parse(ev.data as string)
        } catch {
          return // not our JSON; ignore the event rather than break the screen
        }
        o.handlers[name]?.(data)
        if (name === o.doneEvent && !stopped) {
          stop()
          o.onState('closed')
        }
      })
    }
    es.onerror = () => {
      if (source !== es || stopped) return
      o.onState('reconnecting')
      if (es.readyState === CLOSED) {
        source = null
        scheduleReconnect()
      }
      // CONNECTING: the browser is already reconnecting (the server's retry: 3000).
    }
    armStall()
  }

  async function reconnect(gen: number) {
    retryTimer = null
    if (stopped || gen !== generation) return
    if (o.check) {
      const verdict = await o.check().catch(() => 'retry' as const)
      if (stopped || gen !== generation) return
      if (verdict === 'stop') {
        stop()
        o.onState('closed')
        return
      }
    }
    connect()
  }

  function scheduleReconnect() {
    if (stallTimer) clearTimeout(stallTimer)
    stallTimer = null
    const delay = backoffDelay(failures, backoff, random)
    failures++
    const gen = ++generation
    retryTimer = setTimeout(() => void reconnect(gen), delay)
  }

  o.onState('connecting')
  connect()

  return {
    close: stop,
    retryNow() {
      if (stopped) return
      clearTimers()
      dropSource()
      failures = 0
      void reconnect(++generation)
    },
  }
}
