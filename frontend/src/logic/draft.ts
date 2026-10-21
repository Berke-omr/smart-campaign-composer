// What each message card shows while the drafts are written: a reducer over
// the draft stream's events (docs/API.md, GET /api/campaigns/{id}/draft/stream).
//
// Rules from the contract and the design:
//   - draft.delta carries a field's whole text so far: it replaces, never appends;
//   - "fixing" keeps the last text on screen until the new attempt's first delta,
//     which then replaces all of the channel's text (DESIGN.md, message cards);
//   - "writing" starts a channel from nothing (also after a reconnect, when the
//     api writes an unfinished channel again);
//   - a final state carries the result: the stored message and its report.

import type {
  CampaignView,
  Channel,
  ChannelResult,
  DraftDeltaEvent,
  DraftDoneEvent,
  DraftErrorEvent,
  DraftState,
  DraftStateEvent,
  Field,
  MessageContent,
  MessageView,
  Report,
} from '@/api/types'
import { fieldsOf } from './content'

export const MAX_ATTEMPTS = 3

export interface ChannelDraft {
  /** "pending" until the stream says anything about the channel. */
  state: DraftState | 'pending'
  attempt: number
  maxAttempts: number
  /** The text on screen, per field. */
  fields: Partial<Record<Field, string>>
  /** The attempt the text on screen belongs to. */
  textAttempt: number
  /** What a fixing attempt corrects, or why a manual edit is needed. */
  problems: string[]
  /** The backend's validation of the final message; null until then or without one. */
  report: Report | null
}

export interface DraftProgress {
  channels: Partial<Record<Channel, ChannelDraft>>
  errors: string[]
  done: DraftDoneEvent | null
}

export function isFinal(state: ChannelDraft['state']): boolean {
  return state === 'valid' || state === 'needs_manual_edit'
}

function pending(): ChannelDraft {
  return {
    state: 'pending',
    attempt: 0,
    maxAttempts: MAX_ATTEMPTS,
    fields: {},
    textAttempt: 0,
    problems: [],
    report: null,
  }
}

function fieldsFrom(
  channel: Channel,
  message: MessageContent | null,
): Partial<Record<Field, string>> {
  if (!message) return {}
  const fields: Partial<Record<Field, string>> = {}
  for (const f of fieldsOf(channel)) fields[f] = message[f] ?? ''
  return fields
}

function finalDraft(
  channel: Channel,
  r: Pick<ChannelResult, 'state' | 'attempts' | 'message' | 'report' | 'problems'>,
): ChannelDraft {
  return {
    state: r.state,
    attempt: r.attempts,
    maxAttempts: MAX_ATTEMPTS,
    fields: fieldsFrom(channel, r.message),
    textAttempt: r.attempts,
    problems: r.problems,
    report: r.report,
  }
}

/** A stored message as a finished card. */
export function fromMessageView(channel: Channel, m: MessageView): ChannelDraft {
  return finalDraft(channel, m)
}

/** The cards before the stream: stored channels are final, the others pending. */
export function initialProgress(view: CampaignView): DraftProgress {
  const channels: DraftProgress['channels'] = {}
  for (const ch of view.channels) {
    const m = view.messages[ch]
    channels[ch] = m ? fromMessageView(ch, m) : pending()
  }
  return { channels, errors: [], done: null }
}

/** True when some selected channel has no stored message yet, i.e. the stream must run. */
export function needsDrafting(view: CampaignView): boolean {
  return view.channels.some((ch) => !view.messages[ch])
}

export type DraftEvent =
  | { type: 'draft.state'; data: DraftStateEvent }
  | { type: 'draft.delta'; data: DraftDeltaEvent }
  | { type: 'draft.error'; data: DraftErrorEvent }
  | { type: 'draft.done'; data: DraftDoneEvent }

/** Applies one stream event and returns the new progress (the old one is not modified). */
export function applyDraftEvent(p: DraftProgress, ev: DraftEvent): DraftProgress {
  switch (ev.type) {
    case 'draft.state': {
      const d = ev.data
      if (d.result) {
        return withChannel(p, d.channel, finalDraft(d.channel, d.result))
      }
      const cur = p.channels[d.channel] ?? pending()
      const next: ChannelDraft = {
        ...cur,
        state: d.state,
        attempt: d.attempt,
        maxAttempts: d.max_attempts ?? cur.maxAttempts,
        problems: d.problems ?? [],
        report: null,
      }
      if (d.state === 'writing') {
        next.fields = {}
        next.textAttempt = d.attempt
      }
      return withChannel(p, d.channel, next)
    }
    case 'draft.delta': {
      const d = ev.data
      const cur = p.channels[d.channel] ?? pending()
      if (d.attempt < cur.textAttempt) return p // a late delta of an attempt already replaced
      const fields = d.attempt > cur.textAttempt ? {} : { ...cur.fields }
      fields[d.field] = d.value ?? ''
      return withChannel(p, d.channel, { ...cur, fields, textAttempt: d.attempt })
    }
    case 'draft.error':
      return { ...p, errors: [...p.errors, ev.data.message] }
    case 'draft.done':
      return { ...p, done: ev.data }
  }
}

function withChannel(p: DraftProgress, channel: Channel, d: ChannelDraft): DraftProgress {
  return { ...p, channels: { ...p.channels, [channel]: d } }
}
