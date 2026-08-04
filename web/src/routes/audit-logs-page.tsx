import { useMemo, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { Filter, Hash, LoaderCircle, MapPin, RotateCcw, Search, Shield, UserRound, X } from 'lucide-react'
import { localeFromLanguage } from '@/lib/locale'
import { DataTable } from '@/components/common/data-table'
import { EmptyState } from '@/components/common/empty-state'
import { PageHeader } from '@/components/common/page-header'
import { StatusBadge } from '@/components/common/status-badge'
import { TableToolbar } from '@/components/common/table-toolbar'
import { Button } from '@/components/ui/button'
import { DatePicker } from '@/components/ui/date-picker'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
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
      meta: { className: 'w-[18%]' },
    },
    {
      accessorKey: 'actor_type',
      header: t('headers.actor'),
      cell: ({ row }) => <StatusBadge status="idle">{formatActorType(row.original.actor_type)}</StatusBadge>,
      meta: { className: 'w-[13%]' },
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
      meta: { className: 'w-[29%]', cellClassName: 'min-w-0' },
    },
    {
      accessorKey: 'success',
      header: t('headers.result'),
      cell: ({ row }) => (
        <StatusBadge status={row.original.success ? 'success' : 'warning'}>
          {row.original.success ? t('filters.success') : row.original.error_code || t('filters.failure')}
        </StatusBadge>
      ),
      meta: { className: 'w-[10%]' },
    },
    {
      accessorKey: 'ip_address',
      header: t('headers.ip'),
      cell: ({ row }) => formatIPAddress(row.original.ip_address),
      meta: { className: 'w-[13%]' },
    },
    {
      accessorKey: 'request_id',
      header: t('headers.requestId'),
      cell: ({ row }) => (
        <div className="truncate font-mono text-xs" title={row.original.request_id}>
          {row.original.request_id || '-'}
        </div>
      ),
      meta: { className: 'w-[17%]', cellClassName: 'min-w-0' },
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
      className={isMobile ? 'mt-3 justify-end border-t border-[hsl(var(--glass-divider))] pb-2 pt-3' : 'shrink-0 justify-end'}
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

        <div className="sticky top-0 z-20 -mx-1">
          <div className="rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-2 shadow-[0_18px_38px_rgba(0,0,0,0.10)] backdrop-blur-[32px] backdrop-saturate-150">
            <section>
              <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2">
                <label className="relative min-w-0">
                  <Search className="text-muted-soft pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
                  <Input
                    value={action}
                    onChange={(event) => {
                      setAction(event.target.value)
                      setPage(1)
                    }}
                    placeholder={t('filters.searchAction')}
                    className="h-10 pl-10"
                  />
                </label>
                <Button
                  type="button"
                  variant={hasFilters ? 'secondary' : 'outline'}
                  size="icon"
                  aria-label={t('mobile.filters')}
                  onClick={() => setMobileFiltersOpen(true)}
                >
                  <Filter className="h-4 w-4" />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  aria-label={t('mobile.refresh')}
                  disabled={auditQuery.isFetching}
                  onClick={() => auditQuery.refetch()}
                >
                  {auditQuery.isFetching ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
                </Button>
              </div>
              <div className="mt-2 flex min-w-0 items-center gap-2 overflow-x-auto pb-0.5">
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
              </div>
            </section>
          </div>
        </div>

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
          <DrawContent side="bottom">
            <DrawHeader>
              <DrawTitle>{t('mobile.filters')}</DrawTitle>
            </DrawHeader>
            <DrawBody className="space-y-4">
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-2">
                  <label className="text-sm font-medium text-foreground">{t('filters.dateFrom')}</label>
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
                  <label className="text-sm font-medium text-foreground">{t('filters.dateTo')}</label>
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
            <DrawFooter className="grid grid-cols-2 gap-3">
              <Button type="button" variant="outline" onClick={clearFilters} disabled={!hasFilters}>
                <X className="h-4 w-4" />
                {t('filters.clearFilters')}
              </Button>
              <Button type="button" onClick={() => setMobileFiltersOpen(false)}>
                {t('mobile.done')}
              </Button>
            </DrawFooter>
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
          <Button type="button" variant="outline" onClick={clearFilters}>
            <X className="h-4 w-4" />
            {t('filters.clearFilters')}
          </Button>
        ) : null}
      />

      <DataTable
        columns={columns}
        data={items}
        getRowId={(item) => item.id}
        emptyState={
          <EmptyState
            title={auditQuery.isFetching ? t('empty.loading') : t('empty.noData')}
            description={t('empty.description')}
          />
        }
        className="min-h-0 flex-1 [&>div]:scrollbar-hidden [&>div]:h-full [&>div]:overflow-auto [&_table]:min-w-[1060px] [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
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
  return (
    <article className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] p-3 shadow-[var(--button-secondary-shadow)]">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="truncate text-base font-semibold text-foreground" title={actionLabel}>{actionLabel}</div>
          <div className="mt-1.5 flex min-w-0 items-center gap-2">
            <StatusBadge status="idle" className="shrink-0 px-1.5 py-0 text-[11px]">{actorLabel}</StatusBadge>
            <span className="min-w-0 truncate text-xs text-muted-soft">{timeLabel}</span>
          </div>
        </div>
        <StatusBadge status={item.success ? 'success' : 'warning'} className="shrink-0">
          {item.success ? t('filters.success') : item.error_code || t('filters.failure')}
        </StatusBadge>
      </div>

      <div className="mt-3 grid grid-cols-2 gap-2 text-xs">
        <MobileField icon={UserRound} label={t('headers.actor')} value={actorLabel} />
        <MobileField icon={Shield} label={t('headers.result')} value={item.success ? t('filters.success') : item.error_code || t('filters.failure')} />
        <MobileField icon={MapPin} label={t('headers.ip')} value={ipLabel} />
        <MobileField icon={Hash} label={t('headers.requestId')} value={item.request_id || '-'} mono />
      </div>
    </article>
  )
}

function MobileField({
  icon: Icon,
  label,
  mono = false,
  value,
}: {
  icon: typeof UserRound
  label: string
  mono?: boolean
  value: string
}) {
  return (
    <div className="min-w-0 px-1 py-1">
      <span className="flex items-center gap-1.5 text-[11px] text-muted-soft">
        <Icon className="h-3.5 w-3.5" />
        {label}
      </span>
      <span className={`mt-1 block truncate font-medium text-foreground ${mono ? 'font-mono text-[11px]' : 'tabular-nums'}`} title={value}>{value}</span>
    </div>
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
