import { describe, expect, it } from 'vitest'
import type { Issue, Snapshot } from '@/api/types'
import {
  gaugeFraction,
  issueAge,
  issueText,
  monotonePath,
  monotoneSegments,
  progressPercent,
  segments,
} from '../snapshot'

// The real snapshot in docs/API.md (an SMS-only campaign about 10 s into sending).
const snapshot: Snapshot = {
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
      per_second: [
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 65, 93, 93, 92, 90, 92, 92,
        94, 92,
      ],
      rate: 92,
      limit: 100,
    },
  },
  recent_issues: [
    {
      at: '2026-10-21T10:16:25.042721Z',
      channel: 'sms',
      kind: 'rejected',
      code: 400,
      outcome: 'failed',
      retry_in_ms: null,
      attempt: 1,
      error: '400 invalid recipient',
    },
    {
      at: '2026-10-21T10:16:24.981641Z',
      channel: 'sms',
      kind: 'no_answer',
      code: null,
      outcome: 'retry',
      retry_in_ms: 579,
      attempt: 1,
      error: 'no answer within 3s',
    },
    {
      at: '2026-10-21T10:16:24.713263Z',
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

function issue(kind: Issue['kind'], outcome: Issue['outcome'], retry: number | null): Issue {
  return { ...snapshot.recent_issues[0]!, kind, outcome, retry_in_ms: retry }
}

describe('progressPercent', () => {
  it.each([
    [860, 1226, 70],
    [143, 232, 62],
    [231, 232, 99],
    [232, 232, 100],
    [0, 232, 0],
    [0, 0, 0],
  ])('%i of %i reads %i percent', (done, total, percent) => {
    expect(progressPercent(done, total)).toBe(percent)
  })
})

describe('segments', () => {
  it('lists all five states in the design order with exact shares', () => {
    const s = segments(snapshot.channels.sms!)
    expect(s.map((x) => [x.status, x.count])).toEqual([
      ['sent', 844],
      ['sending', 51],
      ['queued', 266],
      ['retrying', 49],
      ['failed', 16],
    ])
    expect(s.reduce((sum, x) => sum + x.percent, 0)).toBeCloseTo(100, 10)
    expect(s[4]!.percent).toBeCloseTo((16 / 1226) * 100, 10)
  })

  it('keeps zero counts (the legend always shows five numbers)', () => {
    const empty = {
      ...snapshot.channels.sms!,
      total: 0,
      sent: 0,
      sending: 0,
      queued: 0,
      retrying: 0,
      failed: 0,
    }
    expect(segments(empty).map((x) => x.percent)).toEqual([0, 0, 0, 0, 0])
  })
})

describe('gaugeFraction', () => {
  it('is rate over limit, kept between 0 and 1', () => {
    expect(gaugeFraction(92, 100)).toBe(0.92)
    expect(gaugeFraction(0, 100)).toBe(0)
    expect(gaugeFraction(130, 100)).toBe(1)
    expect(gaugeFraction(5, 0)).toBe(0)
  })
})

describe('issueText', () => {
  it('words the three issues of the real snapshot', () => {
    expect(snapshot.recent_issues.map(issueText)).toEqual([
      "Rejected · won't retry",
      'No answer · retrying',
      'Provider busy · retrying in 1s',
    ])
  })

  it.each([
    ['throttled', 'retry', 2000, 'Provider busy · retrying in 2s'],
    ['throttled', 'retry', 1600, 'Provider busy · retrying in 2s'],
    ['throttled', 'retry', 300, 'Provider busy · retrying in 1s'],
    ['provider_error', 'retry', 900, 'Provider error · retrying'],
    ['throttled', 'failed', null, "Provider busy · won't retry"],
    ['provider_error', 'failed', null, "Provider error · won't retry"],
    ['no_answer', 'failed', null, "No answer · won't retry"],
  ] as const)('%s + %s (%s ms) reads "%s"', (kind, outcome, retry, text) => {
    expect(issueText(issue(kind, outcome, retry))).toBe(text)
  })
})

describe('issueAge', () => {
  it("is measured against the snapshot's own time", () => {
    const at = snapshot.at
    expect(snapshot.recent_issues.map((i) => issueAge(i.at, at))).toEqual(['now', 'now', 'now'])
    expect(issueAge('2026-10-21T10:16:23.900Z', at)).toBe('1s')
    expect(issueAge('2026-10-21T10:16:22.000Z', at)).toBe('3s')
    expect(issueAge('2026-10-21T10:15:10.000Z', at)).toBe('1m')
    // An issue stamped a little after the snapshot (two clocks) still reads "now".
    expect(issueAge('2026-10-21T10:16:26.000Z', at)).toBe('now')
  })
})

describe('monotoneSegments', () => {
  // Deterministic pseudo-random series between 0 and 100, like a sending speed.
  function series(seed: number): number[] {
    let x = seed
    return Array.from({ length: 30 }, () => {
      x = (x * 1103515245 + 12345) % 2147483648
      return x % 3 === 0 ? 100 : x % 101
    })
  }

  it('never draws above the highest sample (the curve stays inside each segment)', () => {
    for (let seed = 1; seed <= 200; seed++) {
      const points = series(seed).map((y, i) => ({ x: i * 10, y }))
      for (const s of monotoneSegments(points)) {
        const lo = Math.min(s.from.y, s.to.y)
        const hi = Math.max(s.from.y, s.to.y)
        for (const c of [s.c1, s.c2]) {
          expect(c.y).toBeGreaterThanOrEqual(lo - 1e-9)
          expect(c.y).toBeLessThanOrEqual(hi + 1e-9)
        }
      }
    }
  })

  it('passes through every sample', () => {
    const points = [0, 65, 93, 93, 100, 100, 92].map((y, i) => ({ x: i * 20, y }))
    const segs = monotoneSegments(points)
    expect(segs.map((s) => s.from)).toEqual(points.slice(0, -1))
    expect(segs.map((s) => s.to)).toEqual(points.slice(1))
    expect(monotonePath(points.slice(0, 2))).toMatch(/^M0,0C/)
  })
})
