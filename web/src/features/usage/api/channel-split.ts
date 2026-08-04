import { apiFetch } from '@/lib/http'

export type ChannelSplitSummary = {
  site_id: string
  site_ids: string[]
  site_name: string
  site_names: string[]
  site_slug: string
  site_type: string
  target_label: string
  oauth_connection_id?: string | null
  oauth_provider?: string | null
  oauth_account?: string | null
  range_all: boolean
  date_from: string
  date_to: string
  range_start: string
  range_end: string
  timezone: string
  generated_at: string
  request_count: number
  success_count: number
  failure_count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated_cost: number
  currency: string
  api_key_count: number
}

export type ChannelSplitItem = {
  api_key_id: string
  api_key_name: string
  masked_key: string
  request_count: number
  success_count: number
  failure_count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated_cost: number
  currency: string
  token_share: number
  cost_share: number
}

export type ChannelSplitResponse = {
  summary: ChannelSplitSummary
  items: ChannelSplitItem[]
}

export type ChannelSplitTarget =
  | { type: 'oauth'; id: string; siteIds?: string[] }
  | { type: 'site'; id: string }

export type ChannelSplitInput = {
  siteIds: string[]
  oauthConnectionId?: string
  range?: 'all'
  dateFrom?: string
  dateTo?: string
}

export const channelSplitQueryKeys = {
  all: ['channel-split'] as const,
  detail: (input: ChannelSplitInput) => [...channelSplitQueryKeys.all, input] as const,
}

export async function getChannelSplit(input: ChannelSplitInput) {
  const params = new URLSearchParams()
  for (const siteId of input.siteIds) {
    params.append('site_id', siteId)
  }
  if (input.oauthConnectionId) {
    params.set('oauth_connection_id', input.oauthConnectionId)
  }
  if (input.range === 'all') {
    params.set('range', 'all')
  } else {
    if (input.dateFrom) params.set('date_from', input.dateFrom)
    if (input.dateTo) params.set('date_to', input.dateTo)
  }

  return apiFetch<ChannelSplitResponse>(`/api/v1/requests/channel-split?${params.toString()}`)
}
