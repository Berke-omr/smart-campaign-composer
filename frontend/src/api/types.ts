// Shapes of the api's JSON, as documented in docs/API.md. Field names stay in
// the api's snake_case so a response can be compared with the docs line by line.

export type Channel = 'sms' | 'email'
export const CHANNELS: readonly Channel[] = ['sms', 'email']

export type Tone = 'friendly' | 'professional' | 'playful' | 'urgent'
export const TONES: readonly Tone[] = ['friendly', 'professional', 'playful', 'urgent']

export type CampaignStatus = 'draft' | 'ready' | 'sending' | 'completed' | 'failed'
export type MessageStatus = 'queued' | 'sending' | 'sent' | 'retrying' | 'failed'
export type DraftState = 'writing' | 'validating' | 'fixing' | 'valid' | 'needs_manual_edit'

export type Variable = 'first_name' | 'last_name' | 'city'
export const VARIABLES: readonly Variable[] = ['first_name', 'last_name', 'city']

/** One value per variable (a recipient, the sample user, or an audience's longest values). */
export type Values = Partial<Record<Variable, string>>

export interface AppConfig {
  llm: { mode: 'mock' | 'anthropic'; model: string | null }
}

export interface BriefExample {
  id: string
  title: string
  brief: string
  channels: Channel[]
  tone: Tone
}

export interface Reach {
  total: number
  sms_reachable: number
  email_reachable: number
  sms_excluded: number
  email_excluded: number
  channel_messages: number
}

export interface Audience extends Reach {
  description: { summary: string; lines: string[] }
}

export type Field = 'text' | 'subject' | 'body'

/** SMS uses text; email uses subject and body. Templates keep their {{variables}}. */
export interface MessageContent {
  text?: string
  subject?: string
  body?: string
}

export interface FieldError {
  field: Field
  code: 'empty' | 'too_long' | 'unknown_variable' | 'malformed_variable' | 'multiline' | 'emoji'
  message: string
}

export interface FieldLength {
  field: Field
  template_length: number
  worst_case_length: number
  limit: number
}

export interface Report {
  valid: boolean
  errors: FieldError[]
  lengths: FieldLength[]
  longest_values: Values
  preview: { sample_user: Values; sample: MessageContent; worst_case: MessageContent }
}

export interface MessageView {
  state: 'valid' | 'needs_manual_edit'
  source: 'generated' | 'edited'
  attempts: number
  message: MessageContent | null
  report: Report | null
  problems: string[]
  updated_at: string
}

export interface CampaignView {
  id: number
  name: string
  status: CampaignStatus
  brief: string
  tone: Tone
  channels: Channel[]
  audience: Audience
  unsupported_criteria: string[]
  as_of: string
  llm: AppConfig['llm']
  messages: Partial<Record<Channel, MessageView | null>>
  approved_at: string | null
  finished_at: string | null
  created_at: string
  updated_at: string
}

export interface CampaignSummary {
  id: number
  name: string
  status: CampaignStatus
  channels: Channel[]
  audience: Reach
  created_at: string
}

export interface CreateCampaignRequest {
  brief: string
  channels: Channel[]
  tone: Tone
}

export interface SaveMessageResponse {
  message: MessageView
  status: CampaignStatus
}

export interface ApproveResponse {
  campaign_id: number
  status: CampaignStatus
  approved_at: string
  deliveries: { sms: number; email: number; total: number }
}

// ---- Draft stream (GET /api/campaigns/{id}/draft/stream) ----

export interface ChannelResult {
  channel: Channel
  state: 'valid' | 'needs_manual_edit'
  attempts: number
  message: MessageContent | null
  report: Report | null
  problems: string[]
}

export interface DraftStateEvent {
  channel: Channel
  attempt: number
  state: DraftState
  max_attempts?: number
  problems?: string[]
  result?: ChannelResult
}

export interface DraftDeltaEvent {
  channel: Channel
  attempt: number
  field: Field
  /** The field's whole text so far (omitted by the api when empty). Replace, don't append. */
  value?: string
}

export interface DraftErrorEvent {
  channel?: Channel
  message: string
}

export interface DraftDoneEvent {
  campaign_id: number
  status: CampaignStatus
}

// ---- Live dashboard (GET /api/campaigns/{id}/events) ----

export interface ChannelSnapshot {
  total: number
  done: number
  queued: number
  sending: number
  retrying: number
  sent: number
  failed: number
  /** 30 values, oldest first: provider calls started in each second of the window. */
  per_second: number[]
  rate: number
  limit: number
}

export interface Issue {
  at: string
  channel: Channel
  kind: 'throttled' | 'provider_error' | 'rejected' | 'no_answer'
  code: number | null
  outcome: 'retry' | 'failed'
  retry_in_ms: number | null
  attempt: number
  error: string
}

export interface Snapshot {
  campaign_id: number
  status: CampaignStatus
  approved_at: string | null
  finished_at: string | null
  at: string
  total: number
  done: number
  window: { from: string; to: string }
  channels: Partial<Record<Channel, ChannelSnapshot>>
  recent_issues: Issue[]
}

export interface DeliveryDoneEvent {
  campaign_id: number
  status: CampaignStatus
}
