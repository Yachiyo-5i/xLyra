import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { LoaderCircle } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { StatusBadge } from '@/components/common/status-badge'
import { getRequestLog, requestQueryKeys, type RequestLogItem } from '@/features/requests/api/requests'
import {
  compactJoin,
  formatCurrency,
  formatDateTime,
  formatFailureResponse,
  formatMetricValue,
  formatPerRequestPrice,
  formatPrice,
  formatTokenMetric,
  requestCachePrice,
  requestCacheRatio,
  requestCacheTokens,
  requestCacheWriteTokens,
  formatCostMultiplier,
  requestCredentialMultiplier,
  requestCredentialName,
  requestCostFormula,
  requestFailoverFailureReason,
  requestFailoverTrace,
  type RequestFailoverChannel,
  type RequestFailoverCredentialAttempt,
  requestDownstreamTransportLabel,
  requestGroupName,
  requestGroupRatio,
  requestHasBillingDetails,
  requestIsInProgress,
  requestPhaseLabel,
  requestIsFastBilling,
  requestMappedModel,
  requestModelName,
  requestResponseModeLabel,
} from '@/features/requests/lib/request-utils'

export function RequestDetailRow({ item }: { item: RequestLogItem }) {
  return (
    <tr className="border-t border-[hsl(var(--glass-divider))]">
      <td colSpan={10} className="rounded-md bg-[hsl(var(--surface-subtle))] px-6 py-5">
        <RequestDetailContent item={item} />
      </td>
    </tr>
  )
}

export function RequestDetailContent({ item }: { item: RequestLogItem }) {
  const { t, i18n } = useTranslation('requests')
  const inProgress = requestIsInProgress(item)
  const detailQuery = useQuery({
    queryKey: requestQueryKeys.detail(item.id),
    queryFn: () => getRequestLog(item.id),
    enabled: !inProgress,
  })

  if (inProgress) {
    return (
      <div className="space-y-2.5 text-left text-sm">
        <DetailRow label={t('detail.requestId')}>
          <InlineItem label={t('detail.requestId')} value={item.request_id} tone="badge" />
        </DetailRow>
        <DetailRow label={t('detail.request')}>
          <InlineItem label={t('detail.downstreamModel')} value={requestModelName(item)} tone="badge" />
          <InlineItem label={t('detail.status')}>
            <StatusBadge status="syncing">{t('detail.inProgress')}</StatusBadge>
          </InlineItem>
          <InlineItem label={t('detail.phase')} value={requestPhaseLabel(item, t)} tone="badge" />
          <InlineItem label={t('detail.attempt')} value={item.attempt != null ? String(item.attempt) : null} tone="badge" />
          <InlineItem label={t('detail.responseMode')} value={requestResponseModeLabel(item, t)} tone="badge" />
          <InlineItem label={t('detail.site')} value={item.site.name} tone="badge" />
          <InlineItem label={t('detail.apiKey')} value={item.api_key.name} tone="badge" />
          <InlineItem label={t('detail.startedAt')} value={formatDateTime(item.started_at, i18n.language)} tone="badge" />
        </DetailRow>
      </div>
    )
  }

  if (detailQuery.isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-[hsl(var(--text-muted-soft))]">
        <LoaderCircle className="h-4 w-4 animate-spin" />
        {t('detail.loading')}
      </div>
    )
  }

  if (detailQuery.isError) {
    return (
      <div className="text-sm text-[hsl(var(--destructive))]">
        {t('detail.loadFailed', { message: detailQuery.error.message })}
      </div>
    )
  }

  const detail = detailQuery.data?.request ?? item
  const mappedModel = requestMappedModel(detail)
  const downstreamTransport = requestDownstreamTransportLabel(detail)
  const cacheTokens = requestCacheTokens(detail)
  const cacheWriteTokens = requestCacheWriteTokens(detail)
  const cachePrice = requestCachePrice(detail)
  const costFormula = requestCostFormula(detail, t)
  const currency = detail.cost_calculation?.currency ?? detail.usage.currency ?? detail.pricing?.currency
  const totalCost = detail.cost_calculation?.estimated_cost ?? detail.usage.estimated_cost
  const groupName = requestGroupName(detail)
  const fastBilling = requestIsFastBilling(detail)
  const longContext = detail.cost_calculation?.long_context === true
  const longContextInputMultiplier = detail.cost_calculation?.long_context_input_multiplier
  const longContextOutputMultiplier = detail.cost_calculation?.long_context_output_multiplier
  const failoverTrace = requestFailoverTrace(detail)
  const ratios = compactJoin([
    ratioPart(t('detail.ratioModel'), detail.pricing?.model_ratio),
    ratioPart(t('detail.ratioCompletion'), detail.pricing?.completion_ratio),
    ratioPart(t('detail.ratioCache'), requestCacheRatio(detail)),
    ratioPart(t('detail.ratioGroup'), requestGroupRatio(detail)),
  ])

  return (
    <div className="space-y-2.5 text-left text-sm">
      <DetailRow label={t('detail.requestId')}>
        <Badge variant="neutral" className="w-full max-w-full justify-start overflow-x-auto rounded-md px-2 py-0.5 text-left text-xs tracking-normal sm:w-fit">
          <span className="block w-max min-w-full whitespace-nowrap">{detail.request_id}</span>
        </Badge>
      </DetailRow>

      <DetailRow label={t('detail.request')}>
        <InlineItem label={t('detail.downstreamModel')} value={requestModelName(detail)} tone="badge" />
        <InlineItem label={t('detail.upstreamModel')} value={mappedModel} tone="badge" />
        <InlineItem label={t('detail.transport')}>
          <Badge variant={downstreamTransport === 'WS' ? 'accent' : 'neutral'} className="rounded-md px-2 py-0.5 text-xs tracking-normal">
            {downstreamTransport}
          </Badge>
        </InlineItem>
        <InlineItem label={t('detail.status')}>
          <StatusBadge status={detail.success ? 'healthy' : 'error'}>
            {detail.success ? t('detail.success') : t('detail.failure')}
          </StatusBadge>
        </InlineItem>
        <InlineItem label={t('detail.statusCode')} value={String(detail.upstream_status_code ?? detail.status_code ?? '-')} tone="badge" />
        <InlineItem label={t('detail.site')} value={detail.site.name} tone="badge" />
        <InlineItem label={t('detail.upstreamCredential')} value={requestCredentialName(detail)} tone="badge" />
        <InlineItem label={t('detail.group')} value={groupName} tone="badge" />
      </DetailRow>

      {requestHasBillingDetails(detail) ? (
        <>
          <DetailRow label={t('detail.usage')}>
            <InlineItem label={t('detail.input')} value={formatTokenMetric(detail.cost_calculation?.prompt_tokens ?? detail.usage.prompt_tokens)} tone="badge" />
            <InlineItem label={t('detail.output')} value={formatTokenMetric(detail.cost_calculation?.completion_tokens ?? detail.usage.completion_tokens)} tone="badge" />
            <InlineItem label={t('detail.cache')} value={formatTokenMetric(cacheTokens)} tone="badge" />
            {typeof cacheWriteTokens === 'number' && cacheWriteTokens > 0 ? (
              <InlineItem label={t('detail.cacheWrite')} value={formatTokenMetric(cacheWriteTokens)} tone="badge" />
            ) : null}
            <InlineItem label={t('detail.ratio')} value={ratios} tone="badge" />
          </DetailRow>

          <DetailRow label={t('detail.billing')}>
            <InlineItem label={t('detail.inputPrice')} value={formatPrice(detail.pricing?.input_value, detail.pricing?.currency)} tone="badge" />
            <InlineItem label={t('detail.outputPrice')} value={formatPrice(detail.pricing?.output_value, detail.pricing?.currency)} tone="badge" />
            <InlineItem label={t('detail.cachePrice')} value={formatPrice(cachePrice, detail.pricing?.currency)} tone="badge" />
            <InlineItem label={t('detail.perRequestPrice')} value={formatPerRequestPrice(detail.pricing?.per_request_value, detail.pricing?.currency, t)} tone="badge" />
            <InlineItem label={t('detail.credentialMultiplier')} value={formatCostMultiplier(requestCredentialMultiplier(detail))} tone="badge" />
            {fastBilling ? (
              <InlineItem label={t('detail.mode')}>
                <Badge variant="secondary" className="rounded-md px-2 py-0.5 text-xs tracking-normal">
                  fast
                </Badge>
              </InlineItem>
            ) : null}
            {longContext ? (
              <InlineItem label={t('detail.longContext')}>
                <Badge variant="warning" className="rounded-md px-2 py-0.5 text-xs tracking-normal">
                  {longContextInputMultiplier != null && longContextOutputMultiplier != null
                    ? `×${longContextInputMultiplier} / ×${longContextOutputMultiplier}`
                    : t('detail.longContextApplied')}
                </Badge>
              </InlineItem>
            ) : null}
            <InlineItem label={t('detail.totalCost')} value={formatCurrency(totalCost, currency)} tone="badge" strong />
            {costFormula !== '-' ? (
              <div className="w-full min-w-0 max-w-full basis-full pt-1">
                <div className="w-full max-w-full overflow-x-auto rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))]">
                  <code className="block w-max min-w-full whitespace-nowrap px-3 py-2 font-mono text-xs leading-6 text-[hsl(var(--text-muted-soft))]">
                    {costFormula}
                  </code>
                </div>
              </div>
            ) : null}
          </DetailRow>
        </>
      ) : null}

      <DetailRow label={t('detail.path')}>
        <div className="flex min-w-0 flex-1 flex-col gap-2">
          <PathLine label={t('detail.downstream')} value={detail.downstream_path} />
          <PathLine label={t('detail.upstream')} value={detail.upstream_path} />
        </div>
      </DetailRow>

      {failoverTrace ? (
        <DetailRow>
          <FailoverChannel
            label={t('detail.failoverDefaultChannel')}
            channel={failoverTrace.defaultChannel}
            reason={requestFailoverFailureReason(failoverTrace.defaultChannel, t)}
          />
          {failoverTrace.intermediateChannels.length > 0 ? (
            <div className="basis-full pl-3 text-xs text-[hsl(var(--text-muted-soft))]">
              {t('detail.failoverIntermediateSummary', { count: failoverTrace.intermediateChannels.length })}
            </div>
          ) : null}
          {failoverTrace.intermediateChannels.map((channel, index) => (
            <FailoverChannel
              key={`${channel.siteName}-${index}`}
              label={t('detail.failoverIntermediateChannel', { count: index + 1 })}
              channel={channel}
              reason={requestFailoverFailureReason(channel, t)}
            />
          ))}
          {failoverTrace.finalChannel ? (
            <FailoverChannel
              label={t('detail.failoverFinalChannel')}
              channel={failoverTrace.finalChannel}
              reason={requestFailoverFailureReason(failoverTrace.finalChannel, t)}
              result
            />
          ) : null}
          {failoverTrace.credentialAttempts.length > 0 ? (
            <div className="basis-full pl-3 text-xs text-[hsl(var(--text-muted-soft))]">
              {t('detail.failoverAttemptSummary', { count: failoverTrace.credentialAttempts.length })}
            </div>
          ) : null}
          {failoverTrace.credentialAttempts.map((attempt, index) => (
            <FailoverCredentialAttempt
              key={`${attempt.credentialName ?? 'credential'}-${attempt.attempt ?? index}-${index}`}
              attempt={attempt}
              reason={attempt.success ? undefined : requestFailoverFailureReason(attempt, t)}
            />
          ))}
        </DetailRow>
      ) : null}

      {!detail.success ? (
        <DetailRow label={t('detail.failureLabel')}>
          <pre className="max-h-72 min-w-0 overflow-auto rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] px-3 py-2 whitespace-pre-wrap break-all text-xs leading-6 text-[hsl(var(--text-muted-soft))]">
            {formatFailureResponse(detail.failure_response)}
          </pre>
        </DetailRow>
      ) : null}
    </div>
  )
}

function DetailRow({
  label,
  children,
}: {
  label?: string
  children: ReactNode
}) {
  return (
    <div className="min-w-0 space-y-2 border-t border-[hsl(var(--glass-divider))] py-3 first:border-t-0 first:pt-0 last:pb-0">
      {label ? <div className="text-[11px] font-medium tracking-[0.12em] text-[hsl(var(--text-muted-soft))]">{label}</div> : null}
      <div className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-2 text-foreground">
        {children}
      </div>
    </div>
  )
}

function FailoverChannel({
  label,
  channel,
  reason,
  result = false,
}: {
  label: string
  channel: RequestFailoverChannel
  reason?: string
  result?: boolean
}) {
  const { t } = useTranslation('requests')
  const channelName = compactJoin([channel.siteType, channel.siteName])

  return (
    <div className="flex min-w-0 basis-full flex-wrap items-center gap-x-3 gap-y-1 border-l-2 border-[hsl(var(--glass-divider))] py-1 pl-3">
      <span className="shrink-0 text-xs text-[hsl(var(--text-muted-soft))]">{label}</span>
      <Badge variant={channel.success ? 'success' : 'neutral'} className="max-w-full rounded-md px-2 py-0.5 text-xs tracking-normal">
        <span className="truncate">{channelName}</span>
      </Badge>
      {result ? (
        <StatusBadge status={channel.success ? 'healthy' : 'error'}>
          {channel.success ? t('detail.success') : t('detail.failure')}
        </StatusBadge>
      ) : null}
      {!channel.success ? (
        <span className="min-w-0 truncate text-xs text-[hsl(var(--destructive))]">
          {t('detail.failoverFailureReason')}: {reason}
        </span>
      ) : null}
    </div>
  )
}

function FailoverCredentialAttempt({
  attempt,
  reason,
}: {
  attempt: RequestFailoverCredentialAttempt
  reason?: string
}) {
  const { t } = useTranslation('requests')
  const credentialLabel = attempt.credentialName || attempt.credentialId || t('detail.failoverCredential')
  const channelName = compactJoin([attempt.siteType, attempt.siteName])
  const attemptLabel = attempt.attempt != null && attempt.total != null
    ? t('detail.failoverCredentialOf', { attempt: attempt.attempt, total: attempt.total })
    : attempt.attempt != null
      ? t('detail.failoverCredentialAttempt', { attempt: attempt.attempt })
      : null

  return (
    <div className="flex min-w-0 basis-full flex-wrap items-center gap-x-3 gap-y-1 border-l-2 border-[hsl(var(--glass-divider))] py-1 pl-3">
      <span className="shrink-0 text-xs text-[hsl(var(--text-muted-soft))]">{t('detail.failoverCredentialChannel')}</span>
      <Badge variant={attempt.success ? 'success' : 'neutral'} className="max-w-full rounded-md px-2 py-0.5 text-xs tracking-normal">
        <span className="truncate">{channelName}</span>
      </Badge>
      <span className="shrink-0 text-xs text-[hsl(var(--text-muted-soft))]">{t('detail.failoverCredentialAttemptLabel')}</span>
      <Badge variant={attempt.success ? 'success' : 'neutral'} className="max-w-full rounded-md px-2 py-0.5 text-xs tracking-normal">
        <span className="truncate">{credentialLabel}</span>
      </Badge>
      <StatusBadge status={attempt.success ? 'healthy' : 'error'}>
        {attempt.success ? t('detail.success') : t('detail.failure')}
      </StatusBadge>
      {attemptLabel ? <span className="text-xs text-[hsl(var(--text-muted-soft))]">{attemptLabel}</span> : null}
      {!attempt.success ? (
        <span className="min-w-0 truncate text-xs text-[hsl(var(--destructive))]">
          {t('detail.failoverFailureReason')}: {reason}
        </span>
      ) : null}
    </div>
  )
}

function InlineItem({
  label,
  value,
  children,
  strong,
  tone,
}: {
  label: string
  value?: string | null
  children?: ReactNode
  strong?: boolean
  tone?: 'text' | 'badge' | 'code'
}) {
  const content = children ?? value

  if (!content || content === '-') return null

  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span className="shrink-0 text-xs leading-5 text-[hsl(var(--text-muted-soft))]">{label}</span>
      <InlineValue strong={strong} tone={tone}>{content}</InlineValue>
    </span>
  )
}

function InlineValue({
  children,
  strong,
  tone = 'text',
}: {
  children: ReactNode
  strong?: boolean
  tone?: 'text' | 'badge' | 'code'
}) {
  if (tone === 'badge') {
    return (
      <Badge variant={strong ? 'accent' : 'neutral'} className="max-w-full rounded-md px-2 py-0.5 text-xs tracking-normal">
        <span className="truncate">{children}</span>
      </Badge>
    )
  }

  if (tone === 'code') {
    return (
      <code className="min-w-0 truncate rounded-md bg-[hsl(var(--surface-panel))] px-2 py-0.5 font-mono text-xs text-foreground">
        {children}
      </code>
    )
  }

  return (
    <span className={strong ? 'min-w-0 truncate font-medium leading-5 text-foreground' : 'min-w-0 truncate leading-5 text-foreground'}>
      {children}
    </span>
  )
}

function PathLine({ label, value }: { label: string; value?: string | null }) {
  return (
    <div className="grid min-w-0 basis-full grid-cols-[44px_minmax(0,1fr)] items-start gap-3">
      <span className="pt-1 text-xs leading-4 text-[hsl(var(--text-muted-soft))]">{label}</span>
      <Badge variant="neutral" className="w-fit max-w-full justify-start rounded-md px-2 py-0.5 text-left text-xs tracking-normal">
        <span className="min-w-0 break-all">{value || '-'}</span>
      </Badge>
    </div>
  )
}

function ratioPart(label: string, value?: number | null) {
  const formatted = formatMetricValue(value)
  return formatted ? `${label} ${formatted}` : null
}
