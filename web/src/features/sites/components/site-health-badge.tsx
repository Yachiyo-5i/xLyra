import { useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { cn } from '@/lib/utils'
import { isSiteAbnormal } from '@/features/sites/lib/site-utils'
import type { Site } from '@/features/sites/api/sites'
import type { ValidationSnapshot } from '@/features/sites/lib/types'

type PointerPosition = {
  x: number
  y: number
}

type SiteHealthBadgeProps = {
  site: Site
  validation?: ValidationSnapshot | Site['validation']
  className?: string
}

export function SiteHealthBadge({ site, validation, className }: SiteHealthBadgeProps) {
  const { t } = useTranslation('sites')
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<PointerPosition | null>(null)
  const health = siteHealthStatus(site, validation, t)
  const showTooltip = health.status === 'error' && Boolean(health.message)

  const tooltip = open && position && showTooltip && typeof document !== 'undefined'
    ? createPortal(
        <div
          className="glass-panel-strong pointer-events-none fixed z-[160] max-w-[min(380px,calc(100vw-32px))] rounded-lg px-3 py-2 text-xs leading-5 text-foreground shadow-lg"
          style={{
            left: Math.min(position.x + 12, Math.max(12, window.innerWidth - 396)),
            top: Math.min(position.y + 14, Math.max(12, window.innerHeight - 120)),
          }}
        >
          <div className="font-medium text-red-300">{t('table.health.abnormal')}</div>
          <div className="break-words text-muted-foreground">{health.message}</div>
        </div>,
        document.body,
      )
    : null

  return (
    <>
      <StatusBadge
        status={health.status}
        className={cn(showTooltip && 'cursor-help', className)}
        aria-label={showTooltip ? `${health.label}: ${health.message}` : health.label}
        tabIndex={showTooltip ? 0 : undefined}
        onMouseEnter={(event) => {
          if (!showTooltip) return
          setOpen(true)
          setPosition({ x: event.clientX, y: event.clientY })
        }}
        onMouseMove={(event) => {
          if (!showTooltip) return
          setPosition({ x: event.clientX, y: event.clientY })
        }}
        onMouseLeave={() => setOpen(false)}
        onFocus={(event) => {
          if (!showTooltip) return
          const rect = event.currentTarget.getBoundingClientRect()
          setOpen(true)
          setPosition({ x: rect.left + rect.width / 2, y: rect.bottom })
        }}
        onBlur={() => setOpen(false)}
      >
        {health.label}
      </StatusBadge>
      {tooltip}
    </>
  )
}

function siteHealthStatus(site: Site, validation: ValidationSnapshot | Site['validation'] | undefined, t: (key: string) => string): {
  status: 'healthy' | 'error' | 'idle'
  label: string
  message?: string
} {
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
