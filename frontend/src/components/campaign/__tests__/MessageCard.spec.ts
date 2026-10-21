import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { Report, Values } from '@/api/types'
import type { ChannelDraft } from '@/logic/draft'
import MessageCard from '../MessageCard.vue'

const longest: Values = {
  first_name: 'Muhammed Emin',
  last_name: 'Montgomery-Fitzgerald',
  city: 'İstanbul',
}

function draft(over: Partial<ChannelDraft>): ChannelDraft {
  return {
    state: 'writing',
    attempt: 1,
    maxAttempts: 3,
    fields: {},
    textAttempt: 1,
    problems: [],
    report: null,
    ...over,
  }
}

function card(channel: 'sms' | 'email', d: ChannelDraft) {
  return mount(MessageCard, { props: { channel, draft: d, longest } })
}

describe('MessageCard', () => {
  it.each([
    ['pending', 1, 'Waiting to start…'],
    ['writing', 1, 'Writing…'],
    ['validating', 1, 'Validating…'],
    ['fixing', 2, 'Fixing… · attempt 2 of 3'],
    ['valid', 1, 'Valid'],
    ['needs_manual_edit', 3, 'Needs manual edit'],
  ] as const)('labels the %s state (attempt %i) "%s"', (state, attempt, label) => {
    const c = card('sms', draft({ state, attempt, fields: { text: 'Merhaba' } }))
    expect(c.get('[data-testid="state"]').text()).toBe(label)
    expect(c.get('[data-testid="state"]').attributes('aria-live')).toBe('polite')
  })

  it('shows variables as pills and counts the worst case, not the template', () => {
    const text = 'Merhaba {{first_name}}, sepetin seni bekliyor!'
    const c = card('sms', draft({ state: 'valid', fields: { text } }))
    const pill = c.get('.token')
    expect(pill.text()).toBe('variable first_name')
    expect(c.text()).not.toContain('{{first_name}}')
    // 46 characters as written; 45 with "Muhammed Emin" (13) in place of the 14-character placeholder.
    expect(c.get('[data-testid="counter"]').text()).toBe('45 / 160')
  })

  it('says near limit and over limit in words', () => {
    const near = card('sms', draft({ fields: { text: 'a'.repeat(144) } }))
    expect(near.get('[data-testid="counter"]').text()).toBe('Near limit144 / 160')
    const over = card('sms', draft({ fields: { text: 'a'.repeat(150) + '{{first_name}}' } }))
    expect(over.get('[data-testid="counter"]').text()).toBe('3 over limit163 / 160')
  })

  it('shows the email subject with a counter and the body without one', () => {
    const c = card(
      'email',
      draft({
        state: 'valid',
        fields: { subject: '{{first_name}}, %30 indirim', body: 'Merhaba' },
      }),
    )
    expect(c.text()).toContain('Subject')
    expect(c.text()).toContain('Body')
    // "Muhammed Emin" (13) + ", %30 indirim" (13)
    expect(c.findAll('[data-testid="counter"]').map((x) => x.text())).toEqual(['26 / 80'])
  })

  it('keeps showing skeleton lines where the text is still streaming', () => {
    const c = card('email', draft({ state: 'writing', fields: { subject: 'Konu', body: 'Merh' } }))
    expect(c.findAll('.skeleton')).toHaveLength(1)
    expect(c.findAll('.cursor')).toHaveLength(1)
    const valid = card(
      'email',
      draft({ state: 'valid', fields: { subject: 'Konu', body: 'Merhaba' } }),
    )
    expect(valid.findAll('.skeleton')).toHaveLength(0)
    expect(valid.findAll('.cursor')).toHaveLength(0)
  })

  it('explains a manual edit next to the field and in the card', () => {
    const report = {
      valid: false,
      errors: [
        { field: 'text', code: 'too_long', message: 'text is 200 characters; the limit is 160.' },
      ],
      lengths: [],
    } as unknown as Report
    const c = card(
      'sms',
      draft({
        state: 'needs_manual_edit',
        attempt: 3,
        fields: { text: 'x'.repeat(200) },
        problems: ['text is 200 characters; the limit is 160.'],
        report,
      }),
    )
    expect(c.get('.field-errors').text()).toBe('text is 200 characters; the limit is 160.')
    expect(c.get('.manual').text()).toContain('This message needs a manual edit')
    expect(c.get('[data-testid="counter"]').text()).toBe('40 over limit200 / 160')
  })

  it('says when there is no text at all', () => {
    const c = card('sms', draft({ state: 'needs_manual_edit', attempt: 2, fields: {} }))
    expect(c.text()).toContain('No text yet.')
    expect(c.find('[data-testid="counter"]').exists()).toBe(false)
  })
})
