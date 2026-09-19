import { HoverDetails } from '@/components/common/hover-details'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { isSiteAbnormal } from '@/features/sites/lib/site-utils'
import type { Site } from '@/features/sites/api/sites'
import type { ValidationSnapshot } from '@/features/sites/lib/types'

type SiteHealthBadgeProps = {
  site: Site
  validation?: ValidationSnapshot | Site['validation']
  className?: string
}

export function SiteHealthBadge({ site, validation, className }: SiteHealthBadgeProps) {
  const { t } = useTranslation('sites')
  const health = siteHealthStatus(site, validation, t)
  return (
    <HoverDetails
      asChild
      disabled={health.status !== 'error' || !health.message}
      title={health.label}
      titleClassName="text-red-300"
      content={<div className="whitespace-pre-wrap break-words text-muted-foreground">{health.message}</div>}
    >
      <StatusBadge status={health.status} className={className}>{health.label}</StatusBadge>
    </HoverDetails>
  )
}

function siteHealthStatus(site: Site, validation: ValidationSnapshot | Site['validation'] | undefined, t: (key: string) => string): {
  status: 'healthy' | 'error' | 'syncing' | 'idle'
  label: string
  message?: string
} {
  const syncStatus = site.sync_state?.status?.trim().toLowerCase()
  if (syncStatus === 'pending') {
    return { status: 'syncing', label: t('table.health.pending') }
  }
  if (syncStatus === 'syncing') {
    return { status: 'syncing', label: t('table.health.syncing') }
  }

  if (isSiteAbnormal(site, validation)) {
    const message =
      (validation && validation.ok === false ? stringValue(validation.message) : undefined) ??
      stringValue(site.sync_state?.validation_message) ??
      stringValue(site.sync_state?.message) ??
      stringValue(site.status)
    return { status: 'error', label: t('table.health.abnormal'), message }
  }

  if (validation) {
    return { status: 'healthy', label: t('table.health.healthy') }
  }

  return { status: 'idle', label: t('table.health.unchecked') }
}

function stringValue(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}
