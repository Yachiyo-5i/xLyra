import { useMemo, useState, type ReactNode } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { CalendarDays, Hash, LoaderCircle, MapPin, RotateCcw, Shield, UserRound, X } from 'lucide-react'
import { localeFromLanguage } from '@/lib/locale'
import { cn } from '@/lib/utils'
import { DataTable } from '@/components/common/data-table'
import { defaultTableColumnWidths } from '@/lib/table-column-widths'
import { EmptyState } from '@/components/common/empty-state'
import { MobileFilterBar } from '@/components/common/mobile-filter-bar'
import { PageHeader } from '@/components/common/page-header'
import { StatusBadge } from '@/components/common/status-badge'
import { TableToolbar } from '@/components/common/table-toolbar'
import { Button } from '@/components/ui/button'
import { CardHeading, CardMetric, CardMetrics, ItemCard } from '@/components/ui/card'
import { filterTriggerActiveClass, filterTriggerClass } from '@/components/ui/field-styles'
import { DatePicker } from '@/components/ui/date-picker'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { PaginationControls } from '@/components/ui/pagination'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { auditQueryKeys, listAuditLogs, type AuditLogItem } from '@/features/auth/api/profile'
import { useMobileLayout } from '@/hooks/use-media-query'

const PAGE_SIZE_OPTIONS = [50, 100, 200]

type TFunction = (key: string, options?: Record<string, unknown>) => string

const AUDIT_COLUMN_SIZING = {
  storageKey: 'xlyra:audit:table-column-widths:v1',
  defaultWidths: defaultTableColumnWidths([18, 13, 29, 10, 13, 17]),
  minimumWidths: [9, 7, 12, 6, 7, 9],
}

/** 日期胶囊上显示的短日期：MM-DD */
function formatShortDate(date: Date) {
  return `${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

export function AuditLogsPage() {
  const { t, i18n } = useTranslation('audit')
  const isMobile = useMobileLayout()
  const [action, setAction] = useState('')
  const [actorType, setActorType] = useState('all')
  const [success, setSuccess] = useState<'all' | 'true' | 'false'>('all')
  const [dateFrom, setDateFrom] = useState<Date | undefined>()
  const [dateTo, setDateTo] = useState<Date | undefined>()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [mobileFiltersOpen, setMobileFiltersOpen] = useState(false)

  const filters = useMemo(() => ({
    action: action.trim() || undefined,
    actorType,
    success,
    dateFrom: toRFC3339(dateFrom, 'start'),
    dateTo: toRFC3339(dateTo, 'end'),
    page,
    pageSize,
  }), [action, actorType, dateFrom, dateTo, page, pageSize, success])

  const auditQuery = useQuery({
    queryKey: auditQueryKeys.list(filters),
    queryFn: () => listAuditLogs(filters),
    placeholderData: keepPreviousData,
  })

  const items = auditQuery.data?.items ?? []
  const total = auditQuery.data?.meta?.total ?? 0
  const hasFilters = Boolean(action.trim() || actorType !== 'all' || success !== 'all' || dateFrom || dateTo)

  const dateTimeLocale = localeFromLanguage(i18n.language)

  const columns = useMemo<ColumnDef<AuditLogItem>[]>(() => [
    {
      accessorKey: 'created_at',
      header: t('headers.time'),
      cell: ({ row }) => <span className="tabular-nums">{formatDateTime(row.original.created_at, dateTimeLocale)}</span>,
    },
    {
      accessorKey: 'actor_type',
      header: t('headers.actor'),
      cell: ({ row }) => <StatusBadge status="idle">{formatActorType(row.original.actor_type)}</StatusBadge>,
    },
    {
      accessorKey: 'action',
      header: t('headers.action'),
      cell: ({ row }) => {
        const label = formatAction(row.original, t)
        return (
          <div className="truncate" title={label}>
            {label}
          </div>
        )
      },
      meta: { cellClassName: 'min-w-0' },
    },
    {
      accessorKey: 'success',
      header: t('headers.result'),
      cell: ({ row }) => (
        <StatusBadge status={row.original.success ? 'success' : 'warning'}>
          {row.original.success ? t('filters.success') : row.original.error_code || t('filters.failure')}
        </StatusBadge>
      ),
    },
    {
      accessorKey: 'ip_address',
      header: t('headers.ip'),
      cell: ({ row }) => formatIPAddress(row.original.ip_address),
    },
    {
      accessorKey: 'request_id',
      header: t('headers.requestId'),
      cell: ({ row }) => (
        <div className="truncate font-mono text-footnote" title={row.original.request_id}>
          {row.original.request_id || '-'}
        </div>
      ),
      meta: { cellClassName: 'min-w-0' },
    },
  ], [t, dateTimeLocale])

  function clearFilters() {
    setAction('')
    setActorType('all')
    setSuccess('all')
    setDateFrom(undefined)
    setDateTo(undefined)
    setPage(1)
  }

  const pagination = (
    <PaginationControls
      page={page}
      pageSize={pageSize}
      totalItems={total}
      pageSizeOptions={PAGE_SIZE_OPTIONS}
      showPageSize={!isMobile}
      showDetails={!isMobile}
      onPageChange={setPage}
      onPageSizeChange={(nextPageSize) => {
        setPageSize(nextPageSize)
        setPage(1)
      }}
      disabled={auditQuery.isFetching}
      className={isMobile ? 'mt-3 justify-end border-t border-separator pb-2 pt-3' : 'shrink-0 justify-end'}
    />
  )

  if (isMobile) {
    return (
      <div className="relative space-y-4 pb-2">
        <PageHeader
          eyebrow={t('page.eyebrow')}
          title={t('page.title')}
          description={t('page.description')}
        />

        <MobileFilterBar
          search={{
            value: action,
            onChange: (event) => {
              setAction(event.target.value)
              setPage(1)
            },
            placeholder: t('filters.searchAction'),
          }}
          actions={
            <Button
              type="button"
              variant="secondary"
              size="icon"
              className="rounded-full"
              aria-label={t('mobile.refresh')}
              disabled={auditQuery.isFetching}
              onClick={() => auditQuery.refetch()}
            >
              {auditQuery.isFetching ? <LoaderCircle className="animate-spin" /> : <RotateCcw />}
            </Button>
          }
          filters={
            <>
            <Select value={actorType} onValueChange={(value) => { setActorType(value); setPage(1) }}>
              <SelectTrigger variant="filter" filterLabel={t('headers.actor')} active={actorType !== 'all'}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent searchable={false} widthMode="content">
                <SelectItem value="all">{t('filters.allActors')}</SelectItem>
                <SelectItem value="session">Session</SelectItem>
                <SelectItem value="access_token">Access Token</SelectItem>
                <SelectItem value="system">System</SelectItem>
              </SelectContent>
            </Select>
            <Select value={success} onValueChange={(value) => { setSuccess(value as 'all' | 'true' | 'false'); setPage(1) }}>
              <SelectTrigger variant="filter" filterLabel={t('headers.result')} active={success !== 'all'}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent searchable={false} widthMode="content">
                <SelectItem value="all">{t('filters.allResults')}</SelectItem>
                <SelectItem value="true">{t('filters.success')}</SelectItem>
                <SelectItem value="false">{t('filters.failure')}</SelectItem>
              </SelectContent>
            </Select>
            {/* 日期范围需要两个日期选择器：胶囊点开底部抽屉 */}
            <button
              type="button"
              className={cn(filterTriggerClass, (dateFrom || dateTo) && filterTriggerActiveClass)}
              onClick={() => setMobileFiltersOpen(true)}
            >
              <CalendarDays className="size-3.5" />
              {dateFrom || dateTo
                ? `${dateFrom ? formatShortDate(dateFrom) : '…'} → ${dateTo ? formatShortDate(dateTo) : '…'}`
                : t('filters.date')}
            </button>
            </>
          }
          summary={
            <>
              <span className="tabular-nums">{t('mobile.total', { count: auditQuery.data?.meta.total ?? items.length })}</span>
              {hasFilters ? (
                <button type="button" className="text-label-2 transition-colors active:text-label" onClick={clearFilters}>
                  {t('filters.clearFilters')}
                </button>
              ) : null}
            </>
          }
        />

        {items.length ? (
          <div className="space-y-3">
            {items.map((item) => (
              <MobileAuditCard
                key={item.id}
                item={item}
                actionLabel={formatAction(item, t)}
                actorLabel={formatActorType(item.actor_type)}
                timeLabel={formatDateTime(item.created_at, dateTimeLocale)}
                ipLabel={formatIPAddress(item.ip_address)}
                t={t}
              />
            ))}
          </div>
        ) : (
          <EmptyState
            title={auditQuery.isFetching ? t('empty.loading') : t('empty.noData')}
            description={t('empty.description')}
          />
        )}

        {pagination}

        <Draw open={mobileFiltersOpen} onOpenChange={setMobileFiltersOpen}>
          <DrawContent>
            <DrawHeader>
              <DrawTitle>{t('mobile.filters')}</DrawTitle>
            </DrawHeader>
            <DrawBody className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-2">
                  <label className="text-body font-medium text-label">{t('filters.dateFrom')}</label>
                  <DatePicker
                    value={dateFrom}
                    onValueChange={(date) => {
                      setDateFrom(date)
                      setPage(1)
                    }}
                    placeholder={t('filters.dateFrom')}
                    disableFutureDates
                    className="w-full"
                    triggerClassName="h-10"
                  />
                </div>
                <div className="space-y-2">
                  <label className="text-body font-medium text-label">{t('filters.dateTo')}</label>
                  <DatePicker
                    value={dateTo}
                    onValueChange={(date) => {
                      setDateTo(date)
                      setPage(1)
                    }}
                    placeholder={t('filters.dateTo')}
                    disableFutureDates
                    className="w-full"
                    triggerClassName="h-10"
                  />
                </div>
              </div>
            </DrawBody>
            <DrawFooter
              cancel={(
                <Button type="button" variant="secondary" onClick={clearFilters} disabled={!hasFilters}>
                  <X className="h-4 w-4" />
                  {t('filters.clearFilters')}
                </Button>
              )}
              confirm={(
                <Button type="button" onClick={() => setMobileFiltersOpen(false)}>
                  {t('mobile.done')}
                </Button>
              )}
            />
          </DrawContent>
        </Draw>
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-5 overflow-hidden">
      <PageHeader
        eyebrow={t('page.eyebrow')}
        title={t('page.title')}
        description={t('page.description')}
      />

      <TableToolbar
        searchValue={action}
        onSearchChange={(event) => {
          setAction(event.target.value)
          setPage(1)
        }}
        searchPlaceholder={t('filters.searchAction')}
        searchClassName="flex-none md:w-52"
        filtersClassName="flex min-w-0 flex-1 flex-wrap items-center gap-3 md:flex md:auto-cols-auto"
        filters={(
          <>
            <Select value={actorType} onValueChange={(value) => { setActorType(value); setPage(1) }}>
              <SelectTrigger variant="filter" filterLabel={t('headers.actor')} active={actorType !== 'all'}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent searchable={false} widthMode="content">
                <SelectItem value="all">{t('filters.allActors')}</SelectItem>
                <SelectItem value="session">Session</SelectItem>
                <SelectItem value="access_token">Access Token</SelectItem>
                <SelectItem value="system">System</SelectItem>
              </SelectContent>
            </Select>
            <Select value={success} onValueChange={(value) => { setSuccess(value as 'all' | 'true' | 'false'); setPage(1) }}>
              <SelectTrigger variant="filter" filterLabel={t('headers.result')} active={success !== 'all'}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent searchable={false} widthMode="content">
                <SelectItem value="all">{t('filters.allResults')}</SelectItem>
                <SelectItem value="true">{t('filters.success')}</SelectItem>
                <SelectItem value="false">{t('filters.failure')}</SelectItem>
              </SelectContent>
            </Select>
            <DatePicker
              value={dateFrom}
              onValueChange={(date) => {
                setDateFrom(date)
                setPage(1)
              }}
              placeholder={t('filters.dateFrom')}
              disableFutureDates
              className="w-full md:w-56"
              triggerClassName="h-10"
            />
            <DatePicker
              value={dateTo}
              onValueChange={(date) => {
                setDateTo(date)
                setPage(1)
              }}
              placeholder={t('filters.dateTo')}
              disableFutureDates
              className="w-full md:w-56"
              triggerClassName="h-10"
            />
          </>
        )}
        actions={hasFilters ? (
          <Button type="button" variant="secondary" onClick={clearFilters}>
            <X className="h-4 w-4" />
            {t('filters.clearFilters')}
          </Button>
        ) : null}
      />

      <DataTable
        columnSizing={AUDIT_COLUMN_SIZING}
        stickyHeader="container"
        columns={columns}
        data={items}
        getRowId={(item) => item.id}
        emptyState={
          <EmptyState
            title={auditQuery.isFetching ? t('empty.loading') : t('empty.noData')}
            description={t('empty.description')}
          />
        }
        className="min-h-0 flex-1 [&>div]:scrollbar-hidden [&>div]:h-full [&>div]:overflow-auto [&_table]:min-w-[1060px]"
      />

      {pagination}
    </div>
  )
}

function MobileAuditCard({
  actionLabel,
  actorLabel,
  ipLabel,
  item,
  timeLabel,
  t,
}: {
  actionLabel: string
  actorLabel: string
  ipLabel: string
  item: AuditLogItem
  timeLabel: string
  t: TFunction
}) {
  const resultLabel = item.success ? t('filters.success') : item.error_code || t('filters.failure')
  return (
    <ItemCard>
      <CardHeading
        title={<span title={actionLabel}>{actionLabel}</span>}
        tags={(
          <>
            <StatusBadge status="idle" className="shrink-0 px-1.5 py-0 text-[11px]">{actorLabel}</StatusBadge>
            <span className="min-w-0 truncate text-footnote text-label-2">{timeLabel}</span>
          </>
        )}
        trailing={<StatusBadge status={item.success ? 'success' : 'warning'}>{resultLabel}</StatusBadge>}
      />

      <CardMetrics>
        <CardMetric label={<MetricLabel icon={UserRound}>{t('headers.actor')}</MetricLabel>} value={actorLabel} />
        <CardMetric label={<MetricLabel icon={Shield}>{t('headers.result')}</MetricLabel>} value={resultLabel} />
        <CardMetric label={<MetricLabel icon={MapPin}>{t('headers.ip')}</MetricLabel>} value={ipLabel} />
        <CardMetric
          label={<MetricLabel icon={Hash}>{t('headers.requestId')}</MetricLabel>}
          value={item.request_id || '-'}
          valueClassName="font-mono text-caption-1"
        />
      </CardMetrics>
    </ItemCard>
  )
}

function MetricLabel({ icon: Icon, children }: { icon: typeof UserRound; children: ReactNode }) {
  return (
    <>
      <Icon className="size-3.5 shrink-0" />
      {children}
    </>
  )
}

function toRFC3339(value: Date | undefined, edge: 'start' | 'end') {
  if (!value) return undefined
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined
  if (edge === 'start') {
    date.setHours(0, 0, 0, 0)
  } else {
    date.setHours(23, 59, 59, 999)
  }
  return date.toISOString()
}

function formatDateTime(value?: string | null, locale = 'zh-CN') {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date)
}


function formatAction(item: AuditLogItem, t: TFunction) {
  const actionKey = `actions.${item.action}`
  const translated = t(actionKey)
  if (translated !== actionKey) return translated

  if (item.action === 'admin_api.mutation') {
    return formatMutationAction(item, t)
  }

  return item.action || '-'
}

function formatActorType(value?: string | null) {
  switch (value) {
    case 'session':
      return 'Session'
    case 'access_token':
      return 'Access Token'
    case 'system':
      return 'System'
    default:
      return value || '-'
  }
}

function formatMutationAction(item: AuditLogItem, t: TFunction) {
  const metadata = asMetadataRecord(item.metadata)
  const method = getMetadataString(metadata, 'method').toUpperCase()
  const path = getMetadataString(metadata, 'path') || item.resource_id
  const target = formatMutationTarget(path, t)

  if (!method && !target) return t('mutation.default')
  if (method === 'DELETE') return t('mutation.delete', { target })
  if (method === 'POST') {
    if (path.includes('/refresh')) return t('mutation.refresh', { target })
    if (path.includes('/sync')) return t('mutation.sync', { target })
    return t('mutation.create', { target })
  }
  if (method === 'PUT' || method === 'PATCH') {
    if (path.includes('/enabled')) return t('mutation.toggleStatus', { target })
    if (path.includes('/pricing') || path.includes('/price')) return t('mutation.updatePrice', { target })
    return t('mutation.update', { target })
  }

  return t('mutation.modify', { target })
}

function formatMutationTarget(path: string, t: TFunction) {
  if (!path) return t('targets.default')
  if (path.includes('/api/v1/sites')) return t('targets.sites')
  if (path.includes('/api/v1/site-models')) return t('targets.siteModels')
  if (path.includes('/api/v1/site-pricings')) return t('targets.sitePricings')
  if (path.includes('/api/v1/model-prices')) return t('targets.modelPrices')
  if (path.includes('/api/v1/api-keys')) return t('targets.apiKeys')
  if (path.includes('/api/v1/oauth')) return t('targets.oauth')
  if (path.includes('/api/v1/routes')) return t('targets.routes')
  if (path.includes('/api/v1/settings')) return t('targets.settings')
  if (path.includes('/api/v1/profile')) return t('targets.profile')
  if (path.includes('/api/v1/newapi')) return t('targets.newapi')
  return t('targets.default')
}

function asMetadataRecord(value: unknown) {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return undefined
}

function getMetadataString(metadata: Record<string, unknown> | undefined, key: string) {
  const value = metadata?.[key]
  return typeof value === 'string' ? value.trim() : ''
}

function formatIPAddress(value?: string | null) {
  if (!value) return '-'
  return value
}
