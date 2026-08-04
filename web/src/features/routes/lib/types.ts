import type { RouteCandidateItem } from '@/features/routes/api/routes'
import type { CanonicalModelMatrixRow } from '@/features/sites/api/sites'

export type SortMode = 'default' | 'success_asc' | 'success_desc' | 'candidates_asc' | 'candidates_desc'

export type FilterItem = {
  key: string
  label: string
  count: number
  iconPath?: string
  fallbackText?: string
}

export type RouteChannelRow = {
  id: string
  kind: 'api_key' | 'site_model'
  siteId: string
  siteName: string
  siteType: string
  siteEnabled: boolean
  siteModelId: string
  siteModelEnabled: boolean
  upstreamName: string
  apiKeyId?: string
  apiKeyName?: string
  apiKeyEnabled?: boolean
  apiKeyRoutingPriority?: number
  apiKeyUpstreamCostMultiplier?: number
  apiKeySecretMissing?: boolean
  apiKeyModelName?: string
  apiKeyModelEnabled?: boolean
  groupName?: string
  current?: boolean
  cooling?: boolean
  candidate?: RouteCandidateItem
  pricing?: CanonicalModelMatrixRow['pricing'][number] | RouteCandidateItem['pricing']
  enabled: boolean
  routable: boolean
  toggleDisabled?: boolean
}

export type PendingChannelState = {
  id: string
  enabled: boolean
}
