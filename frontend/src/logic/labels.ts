// The UI's wording for the api's codes (docs/API.md tables). One place, so a
// status never gets two names on two screens (DESIGN.md section 5).

import type { CampaignStatus, Channel, DraftState, MessageStatus, Tone } from '@/api/types'

export const CAMPAIGN_STATUS_LABELS: Record<CampaignStatus, string> = {
  draft: 'Draft',
  ready: 'Ready',
  sending: 'Sending',
  completed: 'Completed',
  failed: 'Failed',
}

export const MESSAGE_STATUS_LABELS: Record<MessageStatus, string> = {
  queued: 'Queued',
  sending: 'Sending',
  sent: 'Sent',
  retrying: 'Retrying',
  failed: 'Failed',
}

/** Message states in the order of the segmented progress bar and its legend. */
export const MESSAGE_STATUS_ORDER: readonly MessageStatus[] = [
  'sent',
  'sending',
  'queued',
  'retrying',
  'failed',
]

export const CHANNEL_LABELS: Record<Channel, string> = {
  sms: 'SMS',
  email: 'Email',
}

export const TONE_LABELS: Record<Tone, string> = {
  friendly: 'Friendly',
  professional: 'Professional',
  playful: 'Playful',
  urgent: 'Urgent',
}

/** The card label of a draft state; "fixing" names the attempt ("Fixing… · attempt 2 of 3"). */
export function draftStateLabel(state: DraftState, attempt: number, maxAttempts: number): string {
  switch (state) {
    case 'writing':
      return 'Writing…'
    case 'validating':
      return 'Validating…'
    case 'fixing':
      return `Fixing… · attempt ${attempt} of ${maxAttempts}`
    case 'valid':
      return 'Valid'
    case 'needs_manual_edit':
      return 'Needs manual edit'
  }
}
