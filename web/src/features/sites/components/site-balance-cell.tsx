import { useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatSiteBalance, siteBalanceDetails } from '@/features/sites/lib/site-utils'
import type { Site } from '@/features/sites/api/sites'

type PointerPosition = {
  x: number
  y: number
}

export function SiteBalanceCell({ site, className }: { site: Site; className?: string }) {
  const { t, i18n } = useTranslation('sites')
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<PointerPosition | null>(null)
  const value = formatSiteBalance(site)
  const details = siteBalanceDetails(site, i18n.language)
  const showTooltip = details.length > 0
  const detailValue = (detail: (typeof details)[number]) => {
    const quota = detail.valuePrefix
      ? `${t(`table.quotaDetails.${detail.valuePrefix}`)} ${detail.value}`
      : detail.value
    return detail.extra ? `${quota} · ${detail.extra}` : quota
  }
  const ariaLabel = showTooltip
    ? `${value}: ${details.map((detail) => `${detail.labelText ?? t(`table.quotaDetails.${detail.label}`)} ${detailValue(detail)}`).join(', ')}`
    : undefined

  const tooltip = open && position && showTooltip && typeof document !== 'undefined'
    ? createPortal(
        <div
          className="glass-panel-strong pointer-events-none fixed z-[160] max-w-[min(320px,calc(100vw-32px))] rounded-lg px-3 py-2 text-xs leading-5 shadow-lg"
          style={{
            left: Math.min(position.x + 12, Math.max(12, window.innerWidth - 336)),
            top: Math.min(position.y + 14, Math.max(12, window.innerHeight - 96)),
          }}
        >
          <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-left">
            {details.map((detail, index) => (
              <div key={`${detail.label}-${index}`} className="contents">
                <span className="text-muted-foreground">{detail.labelText ?? t(`table.quotaDetails.${detail.label}`)}</span>
                <span className="text-foreground tabular-nums">{detailValue(detail)}</span>
              </div>
            ))}
          </div>
        </div>,
        document.body,
      )
    : null

  return (
    <>
      <span
        className={cn('text-sm text-foreground tabular-nums', className)}
        aria-label={ariaLabel}
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
        {value}
      </span>
      {tooltip}
    </>
  )
}
