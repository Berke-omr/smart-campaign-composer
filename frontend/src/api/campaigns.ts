import { getJSON, requestJSON } from './client'
import type {
  AppConfig,
  ApproveResponse,
  BriefExample,
  CampaignSummary,
  CampaignView,
  Channel,
  CreateCampaignRequest,
  MessageContent,
  SaveMessageResponse,
} from './types'

export function fetchConfig(): Promise<AppConfig> {
  return getJSON<AppConfig>('/api/config')
}

export async function fetchExamples(): Promise<BriefExample[]> {
  return (await getJSON<{ examples: BriefExample[] }>('/api/briefs/examples')).examples
}

export function createCampaign(req: CreateCampaignRequest): Promise<CampaignView> {
  return requestJSON<CampaignView>('POST', '/api/campaigns', req)
}

export async function fetchCampaigns(): Promise<CampaignSummary[]> {
  return (await getJSON<{ campaigns: CampaignSummary[] }>('/api/campaigns')).campaigns
}

export function fetchCampaign(id: number): Promise<CampaignView> {
  return getJSON<CampaignView>(`/api/campaigns/${id}`)
}

export function saveMessage(
  id: number,
  channel: Channel,
  content: MessageContent,
): Promise<SaveMessageResponse> {
  return requestJSON<SaveMessageResponse>(
    'PUT',
    `/api/campaigns/${id}/messages/${channel}`,
    content,
  )
}

export function approveCampaign(id: number): Promise<ApproveResponse> {
  return requestJSON<ApproveResponse>('POST', `/api/campaigns/${id}/approve`)
}

export function draftStreamURL(id: number): string {
  return `/api/campaigns/${id}/draft/stream`
}

export function eventsURL(id: number): string {
  return `/api/campaigns/${id}/events`
}
