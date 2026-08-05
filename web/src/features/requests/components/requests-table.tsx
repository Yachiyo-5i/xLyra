import { Fragment, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { EmptyState } from '@/components/common/empty-state'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { RequestLogItem } from '@/features/requests/api/requests'
import { RequestDetailRow } from '@/features/requests/components/request-detail-row'
import { RequestModelMapping } from '@/features/requests/components/request-model-mapping'
import {
  formatCurrency,
  formatDateTime,
  formatInteger,
  formatLatency,
  formatTokenCompact,
  requestCacheTokens,
  requestCacheWriteTokens,
  requestResponseModeLabel,
  requestResponseModeVariant,
} from '@/features/requests/lib/request-utils'

type RequestsTableProps = {
  items: RequestLogItem[]
  expandedId: string | null
  onExpandedIdChange: (id: string | null) => void
  scrollContainerRef?: RefObject<HTMLDivElement | null>
  className?: string
}

export function RequestsTable({
  items,
  expandedId,
  onExpandedIdChange,
  scrollContainerRef,
  className,
}: RequestsTableProps) {
  const { t, i18n } = useTranslation('requests')

  if (!items.length) {
    return (
      <div ref={scrollContainerRef} className={className}>
        <EmptyState title={t('table.empty.title')} description={t('table.empty.description')} />
      </div>
    )
  }

  return (
    <div
      ref={scrollContainerRef}
      className={cn('rounded-lg', className)}
    >
      <table className="w-full table-fixed border-collapse text-left">
        <thead className="sticky -top-6 z-10 bg-[hsl(var(--surface-base))] shadow-[0_1px_0_hsl(var(--glass-divider))] lg:-top-8">
          <tr className="text-faint text-xs uppercase tracking-[0.16em]">
            <th className="w-[4%] px-4 py-3 font-medium" />
            <th className="w-[12%] px-4 py-3 font-medium">{t('table.headers.time')}</th>
            <th className="w-[15%] px-4 py-3 font-medium">{t('table.headers.model')}</th>
            <th className="w-[12%] px-4 py-3 font-medium">{t('table.headers.apiKey')}</th>
            <th className="w-[10%] px-4 py-3 font-medium">{t('table.headers.site')}</th>
            <th className="w-[9%] px-4 py-3 font-medium text-center">{t('table.headers.status')}</th>
            <th className="w-[10%] px-4 py-3 font-medium text-right">{t('table.headers.latency')}</th>
            <th className="w-[10%] px-4 py-3 font-medium text-right">{t('table.headers.input')}</th>
            <th className="w-[8%] px-4 py-3 font-medium text-right">{t('table.headers.output')}</th>
            <th className="w-[10%] px-4 py-3 font-medium text-right">{t('table.headers.cost')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => {
            const expanded = expandedId === item.id
            const cacheTokens = requestCacheTokens(item)
            const cacheWriteTokens = requestCacheWriteTokens(item)
            const hasCacheRead = typeof cacheTokens === 'number' && cacheTokens > 0
            const hasCacheWrite = typeof cacheWriteTokens === 'number' && cacheWriteTokens > 0

            return (
              <Fragment key={item.id}>
                <tr
                  className="cursor-pointer border-t border-[hsl(var(--glass-divider))] [&>td]:transition-colors hover:[&>td]:bg-[hsl(var(--surface-subtle))] hover:[&>td:first-child]:rounded-l-md hover:[&>td:last-child]:rounded-r-md"
                  onClick={() => onExpandedIdChange(expanded ? null : item.id)}
                >
                      <td className="px-4 py-4 align-middle">
                        {expanded ? (
                          <ChevronDown className="text-muted-soft h-4 w-4" />
                        ) : (
                          <ChevronRight className="text-muted-soft h-4 w-4" />
                        )}
                      </td>
                      <td className="px-4 py-4 align-middle text-sm text-foreground">
                        {formatDateTime(item.created_at, i18n.language)}
                      </td>
                      <td className="px-4 py-4 align-middle">
                        <div className="min-w-0">
                          <RequestModelMapping item={item} className="text-sm font-medium text-foreground" />
                        </div>
                      </td>
                      <td className="px-4 py-4 align-middle">
                        <div className="flex min-w-0 items-center gap-2">
                          {item.is_test ? (
                            <Badge variant="secondary" className="shrink-0 px-2 py-0.5 text-xs">
                              {t('table.test')}
                            </Badge>
                          ) : (
                            <div className="truncate text-sm text-foreground" title={item.api_key.name ?? '-'}>
                              {item.api_key.name ?? '-'}
                            </div>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-4 align-middle">
                        <div className="truncate text-sm text-foreground" title={item.site.name ?? '-'}>
                          {item.site.name ?? '-'}
                        </div>
                      </td>
                      <td className="px-4 py-4 align-middle text-center">
                        <div className="flex flex-col items-center gap-2">
                          <StatusBadge status={item.success ? 'healthy' : 'error'}>
                            {item.success ? t('table.success') : t('table.failure')}
                          </StatusBadge>
                          <span className="text-muted-soft text-xs">
                            {item.upstream_status_code ?? item.status_code ?? '-'}
                          </span>
                        </div>
                      </td>
                      <td className="px-4 py-4 align-middle text-right text-sm text-foreground">
                        <div className="flex flex-col items-end gap-2">
                          <Badge
                            variant={requestResponseModeVariant(item)}
                            className="px-2.5 py-1 text-xs font-medium tracking-wide"
                          >
                            {requestResponseModeLabel(item, t)}
                          </Badge>
                          <span>{formatLatency(item.latency_ms)}</span>
                        </div>
                      </td>
                      <td className="px-4 py-4 align-middle text-right text-sm text-foreground">
                        <div>{formatInteger(item.usage.prompt_tokens)}</div>
                        {(hasCacheRead || hasCacheWrite) ? (
                          <div className="text-muted-soft whitespace-nowrap text-xs">
                            {hasCacheRead && hasCacheWrite
                              ? t('table.cacheReadWrite', { read: formatTokenCompact(cacheTokens), write: formatTokenCompact(cacheWriteTokens) })
                              : hasCacheRead
                                ? t('table.cache', { tokens: formatTokenCompact(cacheTokens) })
                                : t('table.cacheWrite', { tokens: formatTokenCompact(cacheWriteTokens) })
                            }
                          </div>
                        ) : null}
                      </td>
                      <td className="px-4 py-4 align-middle text-right text-sm text-foreground">
                        {formatInteger(item.usage.completion_tokens)}
                      </td>
                      <td className="px-4 py-4 align-middle text-right text-sm text-foreground">
                        {formatCurrency(item.usage.estimated_cost, item.usage.currency)}
                      </td>
                </tr>
                {expanded ? (
                  <RequestDetailRow item={item} />
                ) : null}
              </Fragment>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
