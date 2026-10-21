import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import StatusPill from '../StatusPill.vue'
import type { CampaignStatus, MessageStatus } from '@/api/types'

describe('StatusPill', () => {
  // The design's exact labels (DESIGN.md section 5): never "Delivered" or "Awaiting approval".
  const campaign: [CampaignStatus, string][] = [
    ['draft', 'Draft'],
    ['ready', 'Ready'],
    ['sending', 'Sending'],
    ['completed', 'Completed'],
    ['failed', 'Failed'],
  ]
  const message: [MessageStatus, string][] = [
    ['queued', 'Queued'],
    ['sending', 'Sending'],
    ['sent', 'Sent'],
    ['retrying', 'Retrying'],
    ['failed', 'Failed'],
  ]

  it.each(campaign)('campaign status %s reads "%s" in its own colors', (status, label) => {
    const pill = mount(StatusPill, { props: { kind: 'campaign', status } })
    expect(pill.text()).toBe(label)
    expect(pill.attributes('style')).toContain(`--color-campaign-${status}-ink`)
  })

  it.each(message)('message state %s reads "%s" in its own colors', (status, label) => {
    const pill = mount(StatusPill, { props: { kind: 'message', status } })
    expect(pill.text()).toBe(label)
    expect(pill.attributes('style')).toContain(`--color-message-${status}-ink`)
  })
})
