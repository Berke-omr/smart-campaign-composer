import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { CampaignView } from '@/api/types'
import AudienceCard from '../AudienceCard.vue'

// The case's sample audience (DESIGN.md shared data; PLAN.md P2: 177 / 99 / 133).
function campaign(
  over: Partial<CampaignView> = {},
  audience: Partial<CampaignView['audience']> = {},
) {
  return {
    channels: ['sms', 'email'],
    rule: { match: 'all', conditions: [] },
    unsupported_criteria: [],
    audience: {
      total: 177,
      sms_reachable: 99,
      email_reachable: 133,
      sms_excluded: 78,
      email_excluded: 44,
      channel_messages: 232,
      description: {
        summary: 'Users matching all of the following',
        lines: [
          'City is İstanbul',
          'Added to cart in the last 30 days',
          'No purchase in the last 30 days',
        ],
      },
      longest_values: {},
      ...audience,
    },
    ...over,
  } as CampaignView
}

describe('AudienceCard', () => {
  it('shows the conditions, the people and each channel with its exclusions', () => {
    const c = mount(AudienceCard, { props: { campaign: campaign() } })
    expect(c.findAll('.condition').map((x) => x.text())).toEqual([
      'City is İstanbul',
      'Added to cart in the last 30 days',
      'No purchase in the last 30 days',
    ])
    expect(c.findAll('.joiner').map((x) => x.text())).toEqual(['and', 'and'])
    expect(c.get('[data-testid="people"]').text()).toBe('177 people')
    expect(c.findAll('.reach-row').map((x) => x.text())).toEqual([
      'SMS · 99 reachable · 78 without consent',
      'Email · 133 reachable · 44 without consent',
    ])
    // Messages are not people: 99 + 133.
    expect(c.get('[data-testid="channel-messages"]').text()).toBe('232 messages in total')
  })

  it('joins "any" rules with "or" and lists only the selected channels', () => {
    const c = mount(AudienceCard, {
      props: {
        campaign: campaign(
          { channels: ['sms'], rule: { match: 'any', conditions: [] } },
          {
            channel_messages: 99,
            description: { summary: '', lines: ['City is İzmir', 'City is Ankara'] },
          },
        ),
      },
    })
    expect(c.findAll('.joiner').map((x) => x.text())).toEqual(['or'])
    expect(c.findAll('.reach-row')).toHaveLength(1)
  })

  it('warns that part of the brief was not applied', () => {
    const c = mount(AudienceCard, {
      props: { campaign: campaign({ unsupported_criteria: ['kadın kullanıcılar'] }) },
    })
    const banner = c.get('[role="status"]')
    expect(banner.text()).toContain('Some of the brief could not be targeted')
    expect(banner.text()).toContain('Not applied: kadın kullanıcılar.')
  })

  it('says so when nobody matches', () => {
    const c = mount(AudienceCard, {
      props: {
        campaign: campaign(
          {},
          {
            total: 0,
            sms_reachable: 0,
            email_reachable: 0,
            sms_excluded: 0,
            email_excluded: 0,
            channel_messages: 0,
          },
        ),
      },
    })
    expect(c.get('[data-testid="people"]').text()).toBe('0 people')
    expect(c.text()).toContain('No people match this audience.')
    expect(c.get('[data-testid="channel-messages"]').text()).toBe('0 messages in total')
  })
})
