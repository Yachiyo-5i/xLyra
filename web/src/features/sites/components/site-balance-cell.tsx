import { useEffect, useId, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatDateTime, formatSiteBalance, isSub2APIQuotaSite, siteBalanceDetails, sub2APIKeyQuotaDetails } from '@/features/sites/lib/site-utils'
import type { Site, SiteAPIKey } from '@/features/sites/api/sites'

type PointerPosition = {
  x: number
  y: number
}

function getBalanceDetails(site: Site, apiKeys: SiteAPIKey[], language: string) {
  const details = siteBalanceDetails(site, language)
  const sub2APIKeys = isSub2APIQuotaSite(site) ? apiKeys : []
  const keyDetails = sub2APIKeys.map((apiKey) => ({
    apiKey,
    details: sub2APIKeyQuotaDetails(apiKey.quota_probe, language),
  }))

  return { details, keyDetails }
}

export function SiteBalanceDetailsContent({ site, apiKeys = [] }: { site: Site; apiKeys?: SiteAPIKey[] }) {
  const { t, i18n } = useTranslation('sites')
  const { details, keyDetails } = getBalanceDetails(site, apiKeys, i18n.language)
  const detailValue = (detail: (typeof details)[number]) => {
    const quota = detail.valuePrefix
      ? `${t(`table.quotaDetails.${detail.valuePrefix}`)} ${detail.value}`
      : detail.value
    return detail.extra ? `${quota} · ${detail.extra}` : quota
  }
  const probeFailureText = (probe: SiteAPIKey['quota_probe']) => {
    if (probe?.status !== 'error') return ''
    const fetchedAt = probe.fetched_at ? formatDateTime(probe.fetched_at, i18n.language, 'h23') : ''
    return fetchedAt
      ? t('table.quotaDetails.probeFailedAt', { time: fetchedAt })
      : t('table.quotaDetails.probeFailed')
  }

  if (keyDetails.length === 0) {
    return (
      <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-left">
        {details.map((detail, index) => (
          <div key={`${detail.label}-${index}`} className="contents">
            <span className="text-muted-foreground">{detail.labelText ?? t(`table.quotaDetails.${detail.label}`)}</span>
            <span className="text-foreground tabular-nums">{detailValue(detail)}</span>
          </div>
        ))}
      </div>
    )
  }

  return (
    <div className="divide-y divide-[hsl(var(--glass-divider))] text-left">
      {keyDetails.map(({ apiKey, details: apiKeyDetails }) => {
        const probe = apiKey.quota_probe
        const expiresAt = probe?.expires_at ? formatDateTime(probe.expires_at, i18n.language, 'h23') : ''
        const failure = probeFailureText(probe)
        const keyName = apiKey.name && apiKey.name !== apiKey.key ? apiKey.name : ''
        const plan = apiKeyDetails.some((detail) => detail.label === 'accountBalance') ? undefined : probe?.plan
        return (
          <div key={apiKey.id} className="py-2 first:pt-0 last:pb-0">
            <div className="flex min-w-0 items-center justify-between gap-3">
              <span className="truncate text-foreground">{keyName || apiKey.key}</span>
              {apiKey.group ? <span className="shrink-0 text-muted-foreground">{apiKey.group}</span> : null}
            </div>
            {keyName ? <div className="truncate font-mono text-muted-foreground">{apiKey.key}</div> : null}
            {failure ? <div className="mt-1 text-amber-400">{failure}</div> : null}
            {plan || expiresAt || apiKeyDetails.length > 0 ? (
              <div className="mt-1 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-0.5">
                {plan ? (
                  <>
                    <span className="text-muted-foreground">{t('table.quotaDetails.plan')}</span>
                    <span className="text-foreground">{plan}</span>
                  </>
                ) : null}
                {apiKeyDetails.map((detail, index) => (
                  <div key={`${detail.label}-${index}`} className="contents">
                    <span className="text-muted-foreground">{detail.labelText ?? t(`table.quotaDetails.${detail.label}`)}</span>
                    <span className="text-foreground tabular-nums">{detailValue(detail)}</span>
                  </div>
                ))}
                {expiresAt ? (
                  <>
                    <span className="text-muted-foreground">{t('table.quotaDetails.expiresAt')}</span>
                    <span className="text-foreground tabular-nums">{expiresAt}</span>
                  </>
                ) : null}
              </div>
            ) : !failure ? <div className="mt-1 text-muted-foreground">{t('table.quotaDetails.noQuota')}</div> : null}
          </div>
        )
      })}
    </div>
  )
}

export function SiteBalanceCell({ site, apiKeys = [], className }: { site: Site; apiKeys?: SiteAPIKey[]; className?: string }) {
  const { t, i18n } = useTranslation('sites')
  const tooltipId = useId()
  const closeTimer = useRef<number | null>(null)
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<PointerPosition | null>(null)
  const value = formatSiteBalance(site)
  const { details, keyDetails } = getBalanceDetails(site, apiKeys, i18n.language)
  const showTooltip = keyDetails.length > 0 || details.length > 0
  const detailValue = (detail: (typeof details)[number]) => {
    const quota = detail.valuePrefix
      ? `${t(`table.quotaDetails.${detail.valuePrefix}`)} ${detail.value}`
      : detail.value
    return detail.extra ? `${quota} · ${detail.extra}` : quota
  }
  const probeFailureText = (probe: SiteAPIKey['quota_probe']) => {
    if (probe?.status !== 'error') return ''
    const fetchedAt = probe.fetched_at ? formatDateTime(probe.fetched_at, i18n.language, 'h23') : ''
    return fetchedAt
      ? t('table.quotaDetails.probeFailedAt', { time: fetchedAt })
      : t('table.quotaDetails.probeFailed')
  }
  const tooltipDescription = showTooltip && keyDetails.length === 0
    ? `${value}: ${details.map((detail) => `${detail.labelText ?? t(`table.quotaDetails.${detail.label}`)} ${detailValue(detail)}`).join(', ')}`
    : keyDetails.length > 0
      ? `${value}: ${keyDetails.map(({ apiKey, details: apiKeyDetails }) => {
          const probe = apiKey.quota_probe
          const expiresAt = probe?.expires_at ? formatDateTime(probe.expires_at, i18n.language, 'h23') : ''
          const values = [apiKey.name && apiKey.name !== apiKey.key ? `${apiKey.name}, ${apiKey.key}` : apiKey.key]
          const plan = apiKeyDetails.some((detail) => detail.label === 'accountBalance') ? undefined : probe?.plan
          if (apiKey.group) values.push(apiKey.group)
          const failure = probeFailureText(probe)
          if (failure) values.push(failure)
          if (plan) values.push(`${t('table.quotaDetails.plan')} ${plan}`)
          values.push(...apiKeyDetails.map((detail) => `${detail.labelText ?? t(`table.quotaDetails.${detail.label}`)} ${detailValue(detail)}`))
          if (expiresAt) values.push(`${t('table.quotaDetails.expiresAt')} ${expiresAt}`)
          if (!failure && !plan && !expiresAt && apiKeyDetails.length === 0) values.push(t('table.quotaDetails.noQuota'))
          return values.join(', ')
        }).join('; ')}`
      : undefined

  useEffect(() => () => {
    if (closeTimer.current !== null) window.clearTimeout(closeTimer.current)
  }, [])

  const cancelClose = () => {
    if (closeTimer.current === null) return
    window.clearTimeout(closeTimer.current)
    closeTimer.current = null
  }
  const scheduleClose = () => {
    cancelClose()
    closeTimer.current = window.setTimeout(() => {
      setOpen(false)
      closeTimer.current = null
    }, 150)
  }

  const tooltip = open && position && showTooltip && typeof document !== 'undefined'
    ? createPortal(
        <div
          id={tooltipId}
          role="tooltip"
          className="glass-panel-strong fixed z-[160] max-h-[min(70vh,520px)] w-[min(440px,calc(100vw-32px))] overflow-y-auto overscroll-contain rounded-lg px-3 py-2 text-xs leading-5 shadow-lg"
          style={{
            left: Math.min(position.x + 12, Math.max(12, window.innerWidth - 456)),
            top: position.y <= window.innerHeight / 2 ? position.y + 14 : undefined,
            bottom: position.y > window.innerHeight / 2 ? window.innerHeight - position.y + 14 : undefined,
          }}
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
        >
          <SiteBalanceDetailsContent site={site} apiKeys={apiKeys} />
        </div>,
        document.body,
      )
    : null

  return (
    <>
      <span
        className={cn('text-sm text-foreground tabular-nums', className)}
        aria-describedby={showTooltip ? tooltipId : undefined}
        tabIndex={showTooltip ? 0 : undefined}
        onMouseEnter={(event) => {
          if (!showTooltip) return
          cancelClose()
          setOpen(true)
          setPosition({ x: event.clientX, y: event.clientY })
        }}
        onMouseMove={(event) => {
          if (!showTooltip) return
          setPosition({ x: event.clientX, y: event.clientY })
        }}
        onMouseLeave={scheduleClose}
        onFocus={(event) => {
          if (!showTooltip) return
          cancelClose()
          const rect = event.currentTarget.getBoundingClientRect()
          setOpen(true)
          setPosition({ x: rect.left + rect.width / 2, y: rect.bottom })
        }}
        onBlur={() => {
          cancelClose()
          setOpen(false)
        }}
      >
        {value}
      </span>
      {!open && showTooltip ? <span id={tooltipId} role="tooltip" className="sr-only">{tooltipDescription}</span> : null}
      {tooltip}
    </>
  )
}
