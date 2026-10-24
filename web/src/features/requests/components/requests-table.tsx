import { Fragment, useRef, type RefObject } from 'react'
import { DataTableHeader, TableColumnResizeHandle } from '@/components/common/data-table-header'
import { useTableColumnSizing } from '@/hooks/use-table-column-sizing'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { EmptyState } from '@/components/common/empty-state'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { RequestLogItem } from '@/features/requests/api/requests'
import { RequestDetailRow } from '@/features/requests/components/request-detail-row'
import { RequestModelMapping } from '@/features/requests/components/request-model-mapping'
import { RequestTiming } from '@/features/requests/components/request-timing'
import { requestFailoverBadgeClassName } from '@/features/requests/lib/request-badge-styles'
import { REQUEST_TABLE_COLUMN_SIZING } from '@/features/requests/lib/request-table-columns'
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

type RequestsTableProps = {
  items: RequestLogItem[]
  expandedId: string | null
  onExpandedIdChange: (id: string | null) => void
  scrollContainerRef?: RefObject<HTMLDivElement | null>
  className?: string
}

const requestTableHeaders = [
  { id: 'expand', label: 'table.headers.expand', className: 'px-4 py-2' },
  { id: 'time', label: 'table.headers.time', className: 'px-4 py-2' },
  { id: 'model', label: 'table.headers.model', className: 'px-4 py-2' },
  { id: 'api-key', label: 'table.headers.apiKey', className: 'px-4 py-2' },
  { id: 'site', label: 'table.headers.site', className: 'px-4 py-2' },
  { id: 'status', label: 'table.headers.status', className: 'px-4 py-2' },
  { id: 'latency', label: 'table.headers.latency', className: 'px-4 py-2' },
  { id: 'input', label: 'table.headers.input', className: 'px-4 py-2 text-right' },
  { id: 'output', label: 'table.headers.output', className: 'px-4 py-2 text-right' },
  { id: 'cost', label: 'table.headers.cost', className: 'px-4 py-2 text-right' },
] as const

export function RequestsTable({
  items,
  expandedId,
  onExpandedIdChange,
  scrollContainerRef,
  className,
}: RequestsTableProps) {
  const { t, i18n } = useTranslation('requests')
  const tableRef = useRef<HTMLTableElement>(null)
  const sizing = useTableColumnSizing(tableRef, REQUEST_TABLE_COLUMN_SIZING)
  if (!items.length) {
    return (
      <div ref={scrollContainerRef} className={className}>
        <EmptyState title={t('table.empty.title')} description={t('table.empty.description')} />
      </div>
    )
  }

  return (
    <div ref={scrollContainerRef} className={className}>
      <table ref={tableRef} className="w-full table-fixed border-collapse text-left">
        <colgroup>
          {sizing.widths.map((width, index) => (
            <col key={requestTableHeaders[index].id} style={{ width: `${width}%` }} />
          ))}
        </colgroup>
        <DataTableHeader sticky="container">
          <tr className="text-footnote font-semibold text-label-2">
            {requestTableHeaders.map((header, index) => {
              const boundaryIndex = index - 1
              const canResize = boundaryIndex >= 0
              const headerLabel = t(header.label)
              const resizerColumnLabel = canResize ? t(requestTableHeaders[boundaryIndex].label) : ''
              return (
                <th key={header.id} className={cn('relative h-9 whitespace-nowrap', header.className)}>
                  {header.id === 'expand' ? <span className="sr-only">{headerLabel}</span> : headerLabel}
                  {canResize ? (
                    <TableColumnResizeHandle sizing={sizing} index={boundaryIndex} label={resizerColumnLabel} />
                  ) : null}
                </th>
              )
            })}
          </tr>
        </DataTableHeader>
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
                  className="cursor-pointer border-t border-separator [&>td]:transition-colors hover:[&>td]:bg-fill-4"
                  onClick={() => onExpandedIdChange(expanded ? null : item.id)}
                >
                      <td className="px-4 py-4 align-middle">
                        {expanded ? (
                          <ChevronDown className="text-label-2 h-4 w-4" />
                        ) : (
                          <ChevronRight className="text-label-2 h-4 w-4" />
                        )}
                      </td>
                      <td className="px-4 py-4 align-middle text-body text-label">
                        {formatDateTime(item.created_at, i18n.language)}
                      </td>
                      <td className="px-4 py-4 align-middle">
                        <RequestModelMapping item={item} className="text-body font-medium text-label" />
                      </td>
                      <td className="px-4 py-4 align-middle">
                        <div className="flex min-w-0 items-center gap-2">
                          {item.is_test ? (
                            <Badge variant="secondary" className="shrink-0">
                              {t('table.test')}
                            </Badge>
                          ) : (
                            <div className="truncate text-body text-label" title={item.api_key.name ?? '-'}>
                              {item.api_key.name ?? '-'}
                            </div>
                          )}
                        </div>
                      </td>
                      <td className="px-4 py-4 align-middle">
                        <div className="truncate text-body text-label" title={item.site.name ?? '-'}>
                          {item.site.name ?? '-'}
                        </div>
                      </td>
                      <td className="overflow-hidden px-4 py-4 align-middle">
                        <div className="flex flex-col items-start gap-1 text-footnote leading-4">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <StatusBadge status={item.success ? 'healthy' : 'error'} className="shrink-0">
                              {item.success ? t('table.success') : t('table.failure')}
                            </StatusBadge>
                            <Badge
                              variant={requestResponseModeVariant(item)}
                              className="shrink-0"
                            >
                              {requestResponseModeLabel(item, t)}
                            </Badge>
                          </div>
                          <div className="flex shrink-0 items-center gap-1.5 text-footnote">
                            {requestHasFailover(item) ? (
                              <Badge
                                variant="neutral"
                                className={requestFailoverBadgeClassName}
                              >
                                {t('table.failover')}
                              </Badge>
                            ) : null}
                            <span className="text-label-2">{item.upstream_status_code ?? item.status_code ?? '-'}</span>
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-4 align-middle">
                        <RequestTiming item={item} />
                      </td>
                      <td className="px-4 py-4 align-middle text-right text-body text-label">
                        <div>{formatInteger(item.usage.prompt_tokens)}</div>
                        {(hasCacheRead || hasCacheWrite) ? (
                          <div className="text-label-2 whitespace-nowrap text-footnote">
                            {hasCacheRead && hasCacheWrite
                              ? t('table.cacheReadWrite', { read: formatTokenCompact(cacheTokens), write: formatTokenCompact(cacheWriteTokens) })
                              : hasCacheRead
                                ? t('table.cache', { tokens: formatTokenCompact(cacheTokens) })
                                : t('table.cacheWrite', { tokens: formatTokenCompact(cacheWriteTokens) })
                            }
                          </div>
                        ) : null}
                      </td>
                      <td className="px-4 py-4 align-middle text-right text-body text-label">
                        {formatInteger(item.usage.completion_tokens)}
                      </td>
                      <td className="whitespace-nowrap px-4 py-4 align-middle text-right text-body tabular-nums text-label">
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
