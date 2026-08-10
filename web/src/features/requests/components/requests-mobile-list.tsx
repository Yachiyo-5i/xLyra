import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { EmptyState } from '@/components/common/empty-state'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import type { RequestLogItem } from '@/features/requests/api/requests'
import { RequestDetailContent } from '@/features/requests/components/request-detail-row'
import { RequestModelMapping } from '@/features/requests/components/request-model-mapping'
import { RequestTiming } from '@/features/requests/components/request-timing'
import {
  formatCurrency,
  formatDateTime,
  formatInteger,
  formatTokenCompact,
  requestReasoningEffort,
  requestCacheTokens,
  requestCacheWriteTokens,
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
        const reasoningEffort = requestReasoningEffort(item)

        return (
          <Card key={item.id} className="overflow-hidden rounded-lg p-0">
            <button
              type="button"
              className="w-full p-4 text-left"
              onClick={() => onExpandedIdChange(expanded ? null : item.id)}
            >
              <div className="min-w-0 space-y-3">
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0 space-y-1">
                    <RequestModelMapping item={item} inline className="text-sm font-semibold text-foreground" />
                    {reasoningEffort ? (
                      <div className="flex min-w-0 items-center gap-2 text-xs">
                        <span className="truncate text-foreground" title={reasoningEffort}>{reasoningEffort}</span>
                        <span className="shrink-0 text-muted-soft">{formatDateTime(item.created_at, i18n.language)}</span>
                      </div>
                    ) : (
                      <div className="text-muted-soft text-xs">
                        {formatDateTime(item.created_at, i18n.language)}
                      </div>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <StatusBadge status={item.success ? 'healthy' : 'error'}>
                      {item.success ? t('table.success') : t('table.failure')}
                    </StatusBadge>
                    <span className="flex size-6 items-center justify-center text-muted-soft">
                      {expanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                    </span>
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
                  <MobileField label={t('table.headers.site')} value={item.site.name ?? '-'} />
                  <MobileField
                    label={t('table.headers.apiKey')}
                    value={item.is_test ? t('table.test') : item.api_key.name ?? '-'}
                  />
                  <RequestTiming item={item} className="pr-2" />
                  <MobileField label={t('table.headers.cost')} value={formatCurrency(item.usage.estimated_cost, item.usage.currency)} />
                </div>

                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <Badge
                    variant={requestResponseModeVariant(item)}
                    className="px-2.5 py-1 text-xs font-medium tracking-wide"
                  >
                    {requestResponseModeLabel(item, t)}
                  </Badge>
                  <Badge variant="neutral" className="px-2.5 py-1 text-xs tracking-normal">
                    {t('mobile.statusCode', { code: statusCode })}
                  </Badge>
                  <Badge variant="neutral" className="px-2.5 py-1 text-xs tracking-normal">
                    {t('mobile.tokens', {
                      input: formatInteger(item.usage.prompt_tokens),
                      output: formatInteger(item.usage.completion_tokens),
                    })}
                  </Badge>
                  {(hasCacheRead || hasCacheWrite) ? (
                    <Badge variant="neutral" className="px-2.5 py-1 text-xs tracking-normal">
                      {hasCacheRead && hasCacheWrite
                        ? t('table.cacheReadWrite', { read: formatTokenCompact(cacheTokens), write: formatTokenCompact(cacheWriteTokens) })
                        : hasCacheRead
                          ? t('table.cache', { tokens: formatTokenCompact(cacheTokens) })
                          : t('table.cacheWrite', { tokens: formatTokenCompact(cacheWriteTokens) })
                      }
                    </Badge>
                  ) : null}
                </div>
              </div>
            </button>

            {expanded ? (
              <div className="border-t border-[hsl(var(--glass-divider))] bg-[hsl(var(--surface-subtle))] px-4 py-4">
                <RequestDetailContent item={item} />
              </div>
            ) : null}
          </Card>
        )
      })}
    </div>
  )
}

function MobileField({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-muted-soft">{label}</div>
      <div className="truncate font-medium text-foreground" title={value}>
        {value}
      </div>
    </div>
  )
}
