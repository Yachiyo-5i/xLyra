import { HoverDetails } from '@/components/common/hover-details'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { accountBalanceDetails, formatDateTime, formatSiteBalance, isSub2APIQuotaSite, siteBalanceDetails, sub2APIKeyQuotaDetails } from '@/features/sites/lib/site-utils'
import type { Site, SiteAPIKey } from '@/features/sites/api/sites'

function getBalanceDetails(site: Site, apiKeys: SiteAPIKey[], language: string) {
  const details = siteBalanceDetails(site, language)
  const accountBalance = site.quota_probe?.probe_type === 'deepseek' || site.quota_probe?.probe_type === 'moonshot'
  const detailKeys = isSub2APIQuotaSite(site) || accountBalance ? apiKeys : []
  const keyDetails = detailKeys.map((apiKey) => ({
    apiKey,
    details: accountBalance ? accountBalanceDetails(apiKey.quota_probe?.entries) : sub2APIKeyQuotaDetails(apiKey.quota_probe, language),
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

  return (
    <HoverDetails
      asChild
      disabled={!showTooltip}
      title={t('table.headers.balance')}
      description={site.name}
      accessibleDescription={tooltipDescription}
      contentClassName="w-[440px]"
      content={<SiteBalanceDetailsContent site={site} apiKeys={apiKeys} />}
    >
      <span className={cn('text-sm text-foreground tabular-nums', className)}>{value}</span>
    </HoverDetails>
  )
}
