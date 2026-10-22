// Numbers and words the live dashboard derives from a snapshot (docs/API.md,
// Live dashboard). Every snapshot carries the whole state, so these functions
// take one snapshot and keep nothing from the previous one.

import type { ChannelSnapshot, Issue, MessageStatus } from '@/api/types'
import { MESSAGE_STATUS_ORDER } from './labels'

/**
 * The percentage shown next to "143 of 232 done". It is rounded, except that
 * it never reads 100% before every message is final (231 of 232 is 99%).
 */
export function progressPercent(done: number, total: number): number {
  if (total <= 0) return 0
  if (done >= total) return 100
  return Math.min(99, Math.round((done / total) * 100))
}

export interface Segment {
  status: MessageStatus
  count: number
  /** Exact share of the channel's messages, for the bar's width (not rounded). */
  percent: number
}

/** The segmented bar's parts in the design's order: Sent, Sending, Queued, Retrying, Failed. */
export function segments(ch: ChannelSnapshot): Segment[] {
  return MESSAGE_STATUS_ORDER.map((status) => ({
    status,
    count: ch[status],
    percent: ch.total > 0 ? (ch[status] / ch.total) * 100 : 0,
  }))
}

/** How full the rate gauge is: rate ÷ limit, between 0 and 1. */
export function gaugeFraction(rate: number, limit: number): number {
  if (limit <= 0) return 0
  return Math.min(1, Math.max(0, rate / limit))
}

const KIND_LABELS: Record<Issue['kind'], string> = {
  throttled: 'Provider busy',
  provider_error: 'Provider error',
  rejected: 'Rejected',
  no_answer: 'No answer',
}

/**
 * The sentence of a "Recent issues" row (D25 K4: the backend classifies, the
 * UI words it). A 429 names its wait, rounded to whole seconds and at least 1s.
 */
export function issueText(issue: Issue): string {
  const label = KIND_LABELS[issue.kind]
  if (issue.outcome === 'failed') return `${label} · won't retry`
  if (issue.kind === 'throttled' && issue.retry_in_ms !== null) {
    return `${label} · retrying in ${Math.max(1, Math.round(issue.retry_in_ms / 1000))}s`
  }
  return `${label} · retrying`
}

/**
 * How long before the snapshot an issue happened: "now", "3s", "2m". Measured
 * against the snapshot's own `at` (the api's clock), never the browser's, so a
 * slow or skewed browser clock cannot change it, and it stays still while the
 * dashboard reconnects.
 */
export function issueAge(issueAt: string, snapshotAt: string): string {
  const seconds = Math.floor((Date.parse(snapshotAt) - Date.parse(issueAt)) / 1000)
  if (seconds < 1) return 'now'
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m`
}

export interface Point {
  x: number
  y: number
}

export interface CubicSegment {
  from: Point
  c1: Point
  c2: Point
  to: Point
}

/**
 * A smooth curve through the points that never overshoots them: monotone
 * cubic interpolation with Steffen's tangents (as d3's curveMonotoneX). Each
 * control point stays between its segment's two values, so the curve never
 * rises above the highest sample. An ordinary spline would bulge above a run
 * of 95-100/s and draw the line over the 100/s limit the chart is there to show.
 * Points must be evenly spaced on x.
 */
export function monotoneSegments(points: Point[]): CubicSegment[] {
  const n = points.length
  if (n < 2) return []
  const h = points[1]!.x - points[0]!.x
  const slopes: number[] = []
  for (let i = 0; i < n - 1; i++) slopes.push((points[i + 1]!.y - points[i]!.y) / h)
  const tangents: number[] = []
  for (let i = 0; i < n; i++) {
    if (i === 0) tangents.push(slopes[0]!)
    else if (i === n - 1) tangents.push(slopes[n - 2]!)
    else {
      const s0 = slopes[i - 1]!
      const s1 = slopes[i]!
      tangents.push(
        (Math.sign(s0) + Math.sign(s1)) *
          Math.min(Math.abs(s0), Math.abs(s1), Math.abs(s0 + s1) / 4),
      )
    }
  }
  const out: CubicSegment[] = []
  for (let i = 0; i < n - 1; i++) {
    const a = points[i]!
    const b = points[i + 1]!
    out.push({
      from: a,
      c1: { x: a.x + h / 3, y: a.y + (tangents[i]! * h) / 3 },
      c2: { x: b.x - h / 3, y: b.y - (tangents[i + 1]! * h) / 3 },
      to: b,
    })
  }
  return out
}

/** The SVG path of monotoneSegments; a single point is drawn as a dot-sized line. */
export function monotonePath(points: Point[]): string {
  if (points.length === 0) return ''
  const first = points[0]!
  if (points.length === 1) return `M${first.x},${first.y}h0`
  const r = (v: number) => Math.round(v * 100) / 100
  return monotoneSegments(points).reduce(
    (d, s) => `${d}C${r(s.c1.x)},${r(s.c1.y)} ${r(s.c2.x)},${r(s.c2.y)} ${r(s.to.x)},${r(s.to.y)}`,
    `M${r(first.x)},${r(first.y)}`,
  )
}
