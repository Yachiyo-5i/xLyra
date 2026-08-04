import type { ModelRule } from '@/features/api-keys/api/api-keys'

export type APIKeyFormValues = {
  name: string
  enabled: boolean
  useCustomKey: boolean
  customKey: string
  sitePolicy: 'allow_all' | 'allow_list'
  siteIds: string[]
  siteGroupIds: string[]
  modelPolicy: 'allow_all' | 'allow_list'
  siteModelIds: string[]
  modelMappings: ModelRule[]
  imageBridgeEnabled: boolean
  imageBridgeModel: string
  imageBridgeSiteId: string
  quotaLimit: string
  quotaDailyLimit: string
  quotaWeeklyLimit: string
  rateLimitEnabled: boolean
  rpmLimit: string
  tpmLimit: string
  expiresPermanent: boolean
  expiresAt?: Date
}

export type StatusFilter = 'all' | 'active' | 'disabled'
export type TimeDisplayMode = 'relative' | 'absolute'
