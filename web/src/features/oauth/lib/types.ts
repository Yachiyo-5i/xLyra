import type { OAuthProvider, StartCodexOAuthResponse } from '@/features/oauth/api/oauth'

export type OAuthSourceItem = {
  value: OAuthProvider
  label: string
  subtitle: string
  iconPath: string
  enabled: boolean
}

export type OAuthCreateState = {
  source: OAuthProvider
  authorize?: StartCodexOAuthResponse | null
  callbackInput: string
}

type ImportAccountResult = {
  email: string
  provider: string
  connection_id?: string
  site_id?: string
  site_name?: string
  status: 'queued' | 'failed' | string
  refresh_ok?: boolean | null
  refreshable?: boolean | null
  token_mode?: string | null
  warning?: string
  error?: string
}

export type ImportResult = {
  items: ImportAccountResult[]
  meta: {
    total: number
    accepted?: number
    queued?: number
    succeeded: number
    failed: number
  }
}

export type OAuthQuotaWindowLike = {
  name?: string | null
  display_name?: string | null
  used_percent?: number | null
  remaining_percent?: number | null
  reset_at?: number | string | null
  reset_time?: string | null
}
