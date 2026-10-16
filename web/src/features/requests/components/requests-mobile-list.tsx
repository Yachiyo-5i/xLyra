import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/common/empty-state'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import type { RequestLogItem } from '@/features/requests/api/requests'
import { RequestDetailContent } from '@/features/requests/components/request-detail-row'
import { RequestModelMapping } from '@/features/requests/components/request-model-mapping'
import { requestTimingFields } from '@/features/requests/lib/request-timing-tones'
import { requestFailoverBadgeClassName } from '@/features/requests/lib/request-badge-styles'
import {
  formatCurrency,
  formatDateTime,
  formatInteger,
  formatTokenCompact,
  requestCacheTokens,
  requestCacheWriteTokens,
  requestHasFailover,
  requestResponseModeLabel,
  requestResponseModeVariant,
} from '@/features/requests/lib/request-utils'
import { cn } from '@/lib/utils'

type RequestsMobileListProps = {
  items: RequestLogItem[]
  expandedId: string | null
  onExpandedIdChange: (id: string | null) => void
  className?: string
}

export function RequestsMobileList({
  items,
  expandedId,
  onExpandedIdChange,
  className,
}: RequestsMobileListProps) {
  const { t, i18n } = useTranslation('requests')

  if (!items.length) {
    return (
      <div className={cn('px-1 py-4', className)}>
        <EmptyState title={t('table.empty.title')} description={t('table.empty.description')} />
      </div>
    )
  }

  return (
    <div className={cn('space-y-3', className)}>
      {items.map((item) => {
        const expanded = expandedId === item.id
        const cacheTokens = requestCacheTokens(item)
        const cacheWriteTokens = requestCacheWriteTokens(item)
        const hasCacheRead = typeof cacheTokens === 'number' && cacheTokens > 0
        const hasCacheWrite = typeof cacheWriteTokens === 'number' && cacheWriteTokens > 0
        const statusCode = item.upstream_status_code ?? item.status_code ?? '-'

        return (
          <Card key={item.id} className="overflow-hidden p-0">
            {/* 与 OAuth / API 密钥卡片同一结构：标题（模型 + 时间）→ 两列指标；点卡片展开详情 */}
            <button
              type="button"
              className="flex w-full flex-col gap-4 p-4 text-left"
              aria-expanded={expanded}
              onClick={() => onExpandedIdChange(expanded ? null : item.id)}
            >
              <div className="flex w-full min-w-0 items-start gap-3">
                <div className="min-w-0 flex-1">
                  <RequestModelMapping item={item} inline className="text-headline text-label" />
                  <div className="mt-1 text-footnote text-label-3 tabular-nums">
                    {formatDateTime(item.created_at, i18n.language)}
                    <span className="px-1.5">·</span>
                    {statusCode}
                  </div>
                </div>
                <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
                  {requestHasFailover(item) ? (
                    <Badge variant="neutral" className={requestFailoverBadgeClassName}>
                      {t('table.failover')}
                    </Badge>
                  ) : null}
                  <Badge variant={requestResponseModeVariant(item)}>{requestResponseModeLabel(item, t)}</Badge>
                  <StatusBadge status={item.success ? 'healthy' : 'error'}>
                    {item.success ? t('table.success') : t('table.failure')}
                  </StatusBadge>
                </div>
              </div>

              <div className="grid w-full grid-cols-2 items-start gap-x-4 gap-y-3">
                <MobileField label={t('table.headers.site')} value={item.site.name ?? '-'} />
                <MobileField
                  label={t('table.headers.apiKey')}
                  value={item.is_test ? t('table.test') : item.api_key.name ?? '-'}
                />
                {requestTimingFields(item, t).map((field) => (
                  <MobileField key={field.label} label={field.label} value={field.value} valueClassName={field.className} />
                ))}
                <MobileField
                  label={t('table.headers.tokens')}
                  value={t('mobile.tokens', {
                    input: formatInteger(item.usage.prompt_tokens),
                    output: formatInteger(item.usage.completion_tokens),
                  })}
                  secondary={(hasCacheRead || hasCacheWrite)
                    ? hasCacheRead && hasCacheWrite
                      ? t('table.cacheReadWrite', { read: formatTokenCompact(cacheTokens), write: formatTokenCompact(cacheWriteTokens) })
                      : hasCacheRead
                        ? t('table.cache', { tokens: formatTokenCompact(cacheTokens) })
                        : t('table.cacheWrite', { tokens: formatTokenCompact(cacheWriteTokens) })
                    : undefined}
                />
                <MobileField label={t('table.headers.cost')} value={formatCurrency(item.usage.estimated_cost, item.usage.currency)} />
              </div>
            </button>

            {expanded ? (
              <div className="border-t border-separator bg-fill-4 px-4 py-4">
                <RequestDetailContent item={item} />
              </div>
            ) : null}
          </Card>
        )
      })}
    </div>
  )
}

function MobileField({
  label,
  value,
  secondary,
  valueClassName,
}: {
  label: string
  value: string
  secondary?: string
  valueClassName?: string
}) {
  return (
    <div className="min-w-0">
      <div className="truncate text-caption-1 text-label-3">{label}</div>
      <div className={cn('mt-0.5 truncate text-footnote font-medium tabular-nums text-label', valueClassName)} title={value}>
        {value}
      </div>
      {secondary ? <div className="truncate text-caption-1 tabular-nums text-label-3">{secondary}</div> : null}
    </div>
  )
}
