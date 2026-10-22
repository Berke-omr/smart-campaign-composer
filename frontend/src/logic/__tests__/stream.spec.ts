import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  backoffDelay,
  openStream,
  type ConnectionState,
  type EventSourceLike,
  type StreamOptions,
} from '../stream'

const CONNECTING = 0
const OPEN = 1
const CLOSED = 2

/** A stand-in for the browser's EventSource that the test drives by hand. */
class FakeEventSource implements EventSourceLike {
  readyState = CONNECTING
  onerror: ((ev: Event) => void) | null = null
  closed = false
  private listeners = new Map<string, ((ev: MessageEvent) => void)[]>()

  constructor(public readonly url: string) {}

  addEventListener(type: string, listener: (ev: MessageEvent) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener])
  }

  close() {
    this.closed = true
    this.readyState = CLOSED
  }

  emit(type: string, data: unknown) {
    this.readyState = OPEN
    for (const l of this.listeners.get(type) ?? []) {
      l(new MessageEvent(type, { data: JSON.stringify(data) }))
    }
  }

  /** The browser lost an open stream and is reconnecting by itself. */
  dropAndRetry() {
    this.readyState = CONNECTING
    this.onerror?.(new Event('error'))
  }

  /** The browser gave up for good (a 502 from nginx, a 404, a 409). */
  fail() {
    this.readyState = CLOSED
    this.onerror?.(new Event('error'))
  }
}

let sources: FakeEventSource[]
let states: ConnectionState[]
let received: unknown[]

function open(extra: Partial<StreamOptions> = {}) {
  return openStream({
    url: '/api/campaigns/1/events',
    handlers: { 'delivery.snapshot': (d) => received.push(d) },
    doneEvent: 'delivery.done',
    onState: (s) => states.push(s),
    random: () => 0.5, // no jitter: delays are exactly 1 s, 2 s, 4 s …
    createEventSource: (url) => {
      const es = new FakeEventSource(url)
      sources.push(es)
      return es
    },
    ...extra,
  })
}

const last = () => sources[sources.length - 1]!
const lastState = () => states[states.length - 1]

beforeEach(() => {
  vi.useFakeTimers()
  sources = []
  states = []
  received = []
})

afterEach(() => {
  vi.useRealTimers()
})

describe('openStream', () => {
  it('passes parsed events on and reports the connection as live', () => {
    open()
    expect(states).toEqual(['connecting'])
    last().emit('delivery.snapshot', { done: 1 })
    last().emit('delivery.snapshot', { done: 2 })
    expect(received).toEqual([{ done: 1 }, { done: 2 }])
    expect(lastState()).toBe('live')
  })

  it('closes the stream on the done event and never reopens it', async () => {
    const done: unknown[] = []
    open({ handlers: { 'delivery.done': (d) => done.push(d) } })
    last().emit('delivery.done', { campaign_id: 1, status: 'completed' })
    expect(done).toEqual([{ campaign_id: 1, status: 'completed' }])
    expect(last().closed).toBe(true)
    expect(lastState()).toBe('closed')

    last().fail()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sources).toHaveLength(1)
  })

  it('leaves a dropped stream to the browser while it is reconnecting by itself', async () => {
    open()
    last().emit('delivery.snapshot', {})
    last().dropAndRetry()
    expect(lastState()).toBe('reconnecting')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(sources).toHaveLength(1)
  })

  it('reopens the stream itself after the browser gave up, with growing waits', async () => {
    const check = vi.fn<() => Promise<'retry'>>(async () => 'retry')
    open({ check })
    last().fail() // e.g. nginx answered 502 while the api restarts
    expect(lastState()).toBe('reconnecting')

    await vi.advanceTimersByTimeAsync(999)
    expect(sources).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(check).toHaveBeenCalledTimes(1)
    expect(sources).toHaveLength(2)

    last().fail()
    await vi.advanceTimersByTimeAsync(1999)
    expect(sources).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(sources).toHaveLength(3)

    // A snapshot arrives: the next failure starts from 1 s again.
    last().emit('delivery.snapshot', { done: 5 })
    expect(lastState()).toBe('live')
    last().fail()
    await vi.advanceTimersByTimeAsync(1000)
    expect(sources).toHaveLength(4)
    expect(sources.slice(0, 3).every((s) => s.readyState === CLOSED)).toBe(true)
  })

  it('stops when the check says the stream cannot come back', async () => {
    open({ check: async () => 'stop' })
    last().fail()
    await vi.advanceTimersByTimeAsync(1000)
    expect(sources).toHaveLength(1)
    expect(lastState()).toBe('closed')
  })

  it('treats silence longer than stallMs as a lost connection', async () => {
    open({ stallMs: 5000 })
    last().emit('delivery.snapshot', {})
    await vi.advanceTimersByTimeAsync(4999)
    expect(sources).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(sources[0]!.closed).toBe(true)
    expect(lastState()).toBe('reconnecting')
    await vi.advanceTimersByTimeAsync(1000)
    expect(sources).toHaveLength(2)
  })

  it('reconnects at once on "Try now", and only once', async () => {
    let answer: (v: 'retry') => void = () => {}
    const check = vi.fn<() => Promise<'retry'>>(() => new Promise<'retry'>((r) => (answer = r)))
    const stream = open({ check })
    last().fail()
    stream.retryNow()
    stream.retryNow() // a second click while the first check is still waiting
    answer('retry')
    await vi.advanceTimersByTimeAsync(0)
    expect(sources).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sources).toHaveLength(2)
  })

  it('ignores events of a connection it already replaced', async () => {
    open({ stallMs: 5000 })
    const first = last()
    await vi.advanceTimersByTimeAsync(6000)
    expect(sources).toHaveLength(2)
    first.emit('delivery.snapshot', { stale: true })
    expect(received).toEqual([])
  })

  it('does nothing more after close', async () => {
    const stream = open({ stallMs: 5000 })
    stream.close()
    expect(last().closed).toBe(true)
    last().fail()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sources).toHaveLength(1)
  })
})

describe('backoffDelay', () => {
  const b = { firstMs: 1000, maxMs: 10_000, jitter: 0.2 }
  it('doubles up to the cap', () => {
    expect([0, 1, 2, 3, 4, 5].map((n) => backoffDelay(n, b, () => 0.5))).toEqual([
      1000, 2000, 4000, 8000, 10_000, 10_000,
    ])
  })
  it('spreads by the jitter both ways', () => {
    expect(backoffDelay(0, b, () => 0)).toBe(800)
    expect(backoffDelay(0, b, () => 1)).toBe(1200)
  })
})
