import { describe, expect, it } from 'vitest'
import type { CampaignView, ChannelResult, DraftState, Report } from '@/api/types'
import {
  applyDraftEvent,
  initialProgress,
  needsDrafting,
  type DraftEvent,
  type DraftProgress,
} from '../draft'

const report = { valid: true, errors: [], lengths: [] } as unknown as Report

function view(messages: CampaignView['messages']): CampaignView {
  return { channels: ['sms', 'email'], messages } as CampaignView
}

const start = (): DraftProgress => initialProgress(view({ sms: null, email: null }))

function run(events: DraftEvent[], from = start()): DraftProgress {
  return events.reduce(applyDraftEvent, from)
}

const state = (
  channel: 'sms' | 'email',
  s: DraftState,
  attempt: number,
  extra = {},
): DraftEvent => ({
  type: 'draft.state',
  data: { channel, attempt, state: s, max_attempts: 3, ...extra },
})
const delta = (
  channel: 'sms' | 'email',
  attempt: number,
  field: 'text' | 'subject' | 'body',
  value: string,
): DraftEvent => ({ type: 'draft.delta', data: { channel, attempt, field, value } })

describe('applyDraftEvent', () => {
  it('replaces a field with each delta instead of appending', () => {
    const p = run([
      state('sms', 'writing', 1),
      delta('sms', 1, 'text', 'Mer'),
      delta('sms', 1, 'text', 'Merhaba'),
      delta('sms', 1, 'text', 'Merhaba {{first_name}}'),
    ])
    expect(p.channels.sms!.fields.text).toBe('Merhaba {{first_name}}')
  })

  it('keeps the old text while fixing, then the new attempt replaces all of it', () => {
    let p = run([
      state('email', 'writing', 1),
      delta('email', 1, 'subject', 'Eski konu'),
      delta('email', 1, 'body', 'Eski gövde'),
      state('email', 'validating', 1),
      state('email', 'fixing', 2, { problems: ['subject uses unknown variable {{name}}'] }),
    ])
    expect(p.channels.email).toMatchObject({
      state: 'fixing',
      attempt: 2,
      problems: ['subject uses unknown variable {{name}}'],
      fields: { subject: 'Eski konu', body: 'Eski gövde' },
    })

    p = run([delta('email', 2, 'subject', 'Yeni')], p)
    expect(p.channels.email!.fields).toEqual({ subject: 'Yeni' })
  })

  it('ignores a late delta of an attempt that was already replaced', () => {
    const p = run([
      state('sms', 'writing', 1),
      delta('sms', 1, 'text', 'eski'),
      state('sms', 'fixing', 2),
      delta('sms', 2, 'text', 'yeni'),
      delta('sms', 1, 'text', 'eski metin'),
    ])
    expect(p.channels.sms!.fields.text).toBe('yeni')
  })

  it('starts a channel from nothing when it is written again after a reconnect', () => {
    const p = run([
      state('sms', 'writing', 1),
      delta('sms', 1, 'text', 'yarım kalan'),
      state('sms', 'writing', 1),
    ])
    expect(p.channels.sms!.fields).toEqual({})
  })

  it('lets SMS become valid while the email is still being written', () => {
    const result: ChannelResult = {
      channel: 'sms',
      state: 'valid',
      attempts: 1,
      message: { text: 'Merhaba {{first_name}}' },
      report,
      problems: [],
    }
    const p = run([
      state('sms', 'writing', 1),
      state('email', 'writing', 1),
      delta('email', 1, 'subject', 'Konu'),
      state('sms', 'validating', 1),
      state('sms', 'valid', 1, { result }),
    ])
    expect(p.channels.sms).toMatchObject({
      state: 'valid',
      fields: { text: 'Merhaba {{first_name}}' },
      report,
    })
    expect(p.channels.email).toMatchObject({ state: 'writing', fields: { subject: 'Konu' } })
  })

  it('follows the 200-character SMS through three attempts to a manual edit', () => {
    const steps: [DraftState, number][] = [
      ['writing', 1],
      ['validating', 1],
      ['fixing', 2],
      ['validating', 2],
      ['fixing', 3],
      ['validating', 3],
    ]
    let p = start()
    for (const [s, a] of steps) {
      p = run([state('sms', s, a), delta('sms', a, 'text', `deneme ${a}`)], p)
      expect(p.channels.sms).toMatchObject({ state: s, attempt: a })
    }
    p = run([
      state('sms', 'needs_manual_edit', 3, {
        result: {
          channel: 'sms',
          state: 'needs_manual_edit',
          attempts: 3,
          message: { text: 'x'.repeat(200) },
          report,
          problems: ['text is 200 characters'],
        },
      }),
    ])
    expect(p.channels.sms).toMatchObject({
      state: 'needs_manual_edit',
      attempt: 3,
      fields: { text: 'x'.repeat(200) },
      problems: ['text is 200 characters'],
    })
  })

  it('shows no text when the model never produced a message', () => {
    const p = run([
      state('sms', 'writing', 1),
      state('sms', 'fixing', 2),
      state('sms', 'needs_manual_edit', 2, {
        result: {
          channel: 'sms',
          state: 'needs_manual_edit',
          attempts: 2,
          message: null,
          report: null,
          problems: ['the model declined twice'],
        },
      }),
    ])
    expect(p.channels.sms).toMatchObject({ fields: {}, report: null, state: 'needs_manual_edit' })
  })

  it('records errors and the end of the stream', () => {
    const p = run([
      { type: 'draft.error', data: { channel: 'sms', message: 'could not save' } },
      { type: 'draft.done', data: { campaign_id: 3, status: 'ready' } },
    ])
    expect(p.errors).toEqual(['could not save'])
    expect(p.done).toEqual({ campaign_id: 3, status: 'ready' })
  })
})

describe('initialProgress', () => {
  it('shows stored messages as final and the others as pending', () => {
    const v = view({
      sms: {
        state: 'valid',
        source: 'edited',
        attempts: 1,
        message: { text: 'Merhaba' },
        report,
        problems: [],
        updated_at: '',
      },
      email: null,
    })
    const p = initialProgress(v)
    expect(p.channels.sms).toMatchObject({ state: 'valid', fields: { text: 'Merhaba' } })
    expect(p.channels.email!.state).toBe('pending')
    expect(needsDrafting(v)).toBe(true)
    expect(needsDrafting(view({ sms: v.messages.sms, email: v.messages.sms }))).toBe(false)
  })
})
