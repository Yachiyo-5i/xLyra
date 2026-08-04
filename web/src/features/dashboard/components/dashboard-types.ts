import type { ReactNode } from 'react'

export type DashboardRange = '7d' | '30d' | '90d'

export type DashboardSeries = {
  key: string
  name: string
  color?: string
}

export type DashboardTrendDatum = {
  date: string
  [key: string]: string | number
}

export type SiteCostDatum = {
  site: string
  cost: number
}

export type UptimeStatus = 'healthy' | 'degraded' | 'down' | 'idle'

type SiteUptimeBucket = {
  time: string
  status: UptimeStatus
}

export type SiteUptimeItem = {
  id: string
  name: string
  siteType: string
  uptime: string
  buckets: SiteUptimeBucket[]
}

export type CooldownItem = {
  id: string
  scope: 'site' | 'model' | 'credential'
  name: string
  target?: string
  reason: string
  remaining: string
  severity?: 'warning' | 'error'
  siteId?: string
  siteModelId?: string | null
  siteCredentialId?: string | null
  source?: string | null
}

export type DashboardRiskItem = {
  id: string
  title: string
  value?: string
  detail: string
  tone?: 'neutral' | 'warning' | 'error'
  badgeLabel?: string
  actionPath?: string
  actionLabel?: string
  icon?: ReactNode
}
