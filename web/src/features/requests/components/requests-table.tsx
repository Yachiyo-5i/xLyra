import { Fragment, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type RefObject, useRef, useState } from 'react'
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
import {
  readRequestsTableColumnWidthsPreference,
  writeRequestsTableColumnWidthsPreference,
} from '@/features/requests/lib/request-preferences'
import {
  REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS,
  resizeRequestTableColumnBoundary,
  type RequestTableColumnWidths,
} from '@/features/requests/lib/request-table-columns'
import {
  formatCurrency,
  formatDateTime,
  formatInteger,
  formatTokenCompact,
  requestReasoningEffort,
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
  { id: 'expand', label: 'table.headers.expand', className: 'px-4 py-3 font-medium' },
  { id: 'time', label: 'table.headers.time', className: 'px-4 py-3 font-medium' },
  { id: 'model', label: 'table.headers.model', className: 'px-4 py-3 font-medium' },
  { id: 'api-key', label: 'table.headers.apiKey', className: 'px-4 py-3 font-medium' },
  { id: 'site', label: 'table.headers.site', className: 'px-4 py-3 font-medium' },
  { id: 'status', label: 'table.headers.status', className: 'px-1 py-3 font-medium text-center' },
  { id: 'latency', label: 'table.headers.latency', className: 'px-4 py-3 font-medium' },
  { id: 'input', label: 'table.headers.input', className: 'px-4 py-3 font-medium text-right' },
  { id: 'output', label: 'table.headers.output', className: 'px-4 py-3 font-medium text-right' },
  { id: 'cost', label: 'table.headers.cost', className: 'px-4 py-3 font-medium text-right' },
] as const

type ColumnResizeState = {
  boundaryIndex: number
  pointerID: number
  startX: number
  tableWidth: number
  widths: RequestTableColumnWidths
}

export function RequestsTable({
  items,
  expandedId,
  onExpandedIdChange,
  scrollContainerRef,
  className,
}: RequestsTableProps) {
  const { t, i18n } = useTranslation('requests')
  const [columnWidths, setColumnWidths] = useState<RequestTableColumnWidths>(() => readRequestsTableColumnWidthsPreference())
  const columnWidthsRef = useRef(columnWidths)
  const resizeStateRef = useRef<ColumnResizeState | null>(null)
  const tableRef = useRef<HTMLTableElement>(null)

  function updateColumnWidths(widths: RequestTableColumnWidths) {
    columnWidthsRef.current = widths
    setColumnWidths(widths)
  }

  function resizeColumnBoundary(boundaryIndex: number, deltaPercent: number) {
    const widths = resizeRequestTableColumnBoundary(columnWidthsRef.current, boundaryIndex, deltaPercent)
    updateColumnWidths(widths)
    return widths
  }

  function handleColumnResizerPointerDown(boundaryIndex: number, event: ReactPointerEvent<HTMLSpanElement>) {
    if (event.button !== 0) return

    const tableWidth = tableRef.current?.getBoundingClientRect().width ?? 0
    if (tableWidth <= 0) return

    event.preventDefault()
    event.stopPropagation()
    event.currentTarget.setPointerCapture(event.pointerId)
    resizeStateRef.current = {
      boundaryIndex,
      pointerID: event.pointerId,
      startX: event.clientX,
      tableWidth,
      widths: columnWidthsRef.current,
    }
  }

  function handleColumnResizerPointerMove(event: ReactPointerEvent<HTMLSpanElement>) {
    const resizeState = resizeStateRef.current
    if (!resizeState || resizeState.pointerID !== event.pointerId) return

    event.preventDefault()
    event.stopPropagation()
    const deltaPercent = ((event.clientX - resizeState.startX) / resizeState.tableWidth) * 100
    const widths = resizeRequestTableColumnBoundary(resizeState.widths, resizeState.boundaryIndex, deltaPercent)
    updateColumnWidths(widths)
  }

  function handleColumnResizerPointerEnd(event: ReactPointerEvent<HTMLSpanElement>) {
    const resizeState = resizeStateRef.current
    if (!resizeState || resizeState.pointerID !== event.pointerId) return

    event.preventDefault()
    event.stopPropagation()
    resizeStateRef.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    writeRequestsTableColumnWidthsPreference(columnWidthsRef.current)
  }

  function handleColumnResizerKeyDown(boundaryIndex: number, event: KeyboardEvent<HTMLSpanElement>) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return

    event.preventDefault()
    event.stopPropagation()
    const direction = event.key === 'ArrowLeft' ? -1 : 1
    const widths = resizeColumnBoundary(boundaryIndex, direction * (event.shiftKey ? 5 : 1))
    writeRequestsTableColumnWidthsPreference(widths)
  }

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
      <table ref={tableRef} className="w-full table-fixed border-collapse text-left">
        <colgroup>
          {columnWidths.map((width, index) => (
            <col key={requestTableHeaders[index].id} style={{ width: `${width}%` }} />
          ))}
        </colgroup>
        <thead className="sticky -top-6 z-10 bg-[hsl(var(--surface-base))] shadow-[0_1px_0_hsl(var(--glass-divider))] lg:-top-8">
          <tr className="text-faint text-xs uppercase tracking-[0.16em]">
            {requestTableHeaders.map((header, index) => {
              const boundaryIndex = index - 1
              const canResize = boundaryIndex >= 0
              const maximumWidth = canResize
                ? 100 - REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS.reduce((total, minimum, minimumIndex) => minimumIndex === boundaryIndex ? total : total + minimum, 0)
                : 0
              const headerLabel = t(header.label)
              const resizerColumnLabel = canResize ? t(requestTableHeaders[boundaryIndex].label) : ''
              return (
                <th key={header.id} className={cn('relative', header.id === 'status' && 'overflow-hidden', header.className)}>
                  {header.id === 'expand' ? <span className="sr-only">{headerLabel}</span> : headerLabel}
                  {canResize ? (
                    <span
                      role="separator"
                      aria-orientation="vertical"
                      aria-label={t('table.resizeColumn', { column: resizerColumnLabel })}
                      aria-valuenow={columnWidths[boundaryIndex]}
                      aria-valuemin={REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS[boundaryIndex]}
                      aria-valuemax={maximumWidth}
                      tabIndex={0}
                      className="absolute -left-2 top-0 z-20 h-full w-4 cursor-col-resize touch-none select-none outline-none after:absolute after:inset-y-2 after:left-1/2 after:w-px after:-translate-x-1/2 after:bg-[hsl(var(--glass-divider))] after:opacity-50 hover:after:bg-primary hover:after:opacity-100 focus-visible:after:bg-primary focus-visible:after:opacity-100"
                      style={{ cursor: 'col-resize' }}
                      onPointerDown={(event) => handleColumnResizerPointerDown(boundaryIndex, event)}
                      onPointerMove={handleColumnResizerPointerMove}
                      onPointerUp={handleColumnResizerPointerEnd}
                      onPointerCancel={handleColumnResizerPointerEnd}
                      onKeyDown={(event) => handleColumnResizerKeyDown(boundaryIndex, event)}
                    />
                  ) : null}
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {items.map((item) => {
            const expanded = expandedId === item.id
            const cacheTokens = requestCacheTokens(item)
            const cacheWriteTokens = requestCacheWriteTokens(item)
            const reasoningEffort = requestReasoningEffort(item)
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
                        <div className="min-w-0 space-y-1">
                          <RequestModelMapping item={item} className="text-sm font-medium text-foreground" />
                          {reasoningEffort ? (
                            <div className="min-w-0 text-xs text-foreground">
                              <span className="block truncate" title={reasoningEffort}>{reasoningEffort}</span>
                            </div>
                          ) : null}
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
                      <td className="overflow-hidden px-1 py-4 align-middle text-center">
                        <div className="flex flex-col items-center gap-1 text-xs leading-4">
                          <div className="flex flex-wrap items-center justify-center gap-1.5">
                            <StatusBadge status={item.success ? 'healthy' : 'error'} className="shrink-0 px-2 py-0.5 text-xs leading-4">
                              {item.success ? t('table.success') : t('table.failure')}
                            </StatusBadge>
                            <Badge
                              variant={requestResponseModeVariant(item)}
                              className="shrink-0 px-2 py-0.5 text-xs font-medium tracking-wide"
                            >
                              {requestResponseModeLabel(item, t)}
                            </Badge>
                            {requestHasFailover(item) ? (
                              <Badge variant="warning" className="shrink-0 px-2 py-0.5 text-xs leading-4">
                                {t('table.failover')}
                              </Badge>
                            ) : null}
                          </div>
                          <span className="shrink-0 text-xs text-muted-soft">
                            {item.upstream_status_code ?? item.status_code ?? '-'}
                          </span>
                        </div>
                      </td>
                      <td className="px-4 py-4 align-middle">
                        <RequestTiming item={item} />
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
