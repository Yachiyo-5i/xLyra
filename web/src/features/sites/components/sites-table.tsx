import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { ChevronDown, FlaskConical, LoaderCircle, PencilLine, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DataTable } from '@/components/common/data-table'
import { defaultTableColumnWidths } from '@/lib/table-column-widths'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { SiteBalanceCell } from '@/features/sites/components/site-balance-cell'
import { SiteHealthBadge } from '@/features/sites/components/site-health-badge'
import { SitePlanBadge } from '@/features/sites/components/site-plan-badge'
import { ProviderUnavailableBadge } from '@/features/sites/components/provider-unavailable-badge'
import { SiteTypeBadge } from '@/features/sites/components/site-type-badge'
import { SiteTypeIcon } from '@/features/sites/components/site-type-icon'
import type { Site, SiteAPIKey, SiteModel, SiteTypeInfo } from '@/features/sites/api/sites'
import {
  formatCompactTokens,
  formatDateTime,
  formatRelativeDateTime,
  formatRoutingPriority,
  isAccountLoginSiteType,
  siteAccountCounts,
  isOAuthSite,
  isSiteEnableBlockedByAbnormalState,
  siteAccountEmail,
  siteDisplayUpdatedAt,
  siteModelsDisplayCount,
  siteTypeIcon,
  siteTypeIconClassName,
} from '@/features/sites/lib/site-utils'
import type { SiteUpdatedAtDisplayMode, ValidationSnapshot } from '@/features/sites/lib/types'

const EMPTY_MODELS: SiteModel[] = []
const EMPTY_API_KEYS: SiteAPIKey[] = []

const SITES_COLUMN_SIZING = {
  storageKey: 'xlyra:sites:table-column-widths:v1',
  defaultWidths: defaultTableColumnWidths([24, 8, 13, 8, 8, 6, 7, 10, 11]),
  minimumWidths: [10, 5, 7, 5, 5, 4, 5, 7, 7],
}

export function SitesTable({
  items, modelsMap, apiKeysMap, validationSnapshots, refreshingSiteIds, togglingSiteId, deletingSiteId,
  abnormalItems = [], abnormalCollapsed = true, onAbnormalCollapsedChange,
  updatedAtMode, now, onUpdatedAtModeChange,
  onRefresh, onToggleEnabled, onEdit, onDelete, onOpenModels, onOpenAPIKeys, onOpenAccounts, onOpenTest, onOpenUsageSplit, siteTypes,
  className,
}: {
  items: Site[]
  abnormalItems?: Site[]
  abnormalCollapsed?: boolean
  onAbnormalCollapsedChange?: (collapsed: boolean) => void
  modelsMap: Record<string, SiteModel[]>
  apiKeysMap: Record<string, SiteAPIKey[]>
  validationSnapshots: Record<string, ValidationSnapshot>
  refreshingSiteIds: string[]
  togglingSiteId: string | null | undefined
  deletingSiteId: string | null | undefined
  updatedAtMode: SiteUpdatedAtDisplayMode
  now: number
  onUpdatedAtModeChange: (mode: SiteUpdatedAtDisplayMode) => void
  onRefresh: (id: string) => void
  onToggleEnabled: (site: Site) => void
  onEdit: (site: Site) => void
  onDelete: (site: Site) => void
  onOpenModels: (site: Site) => void
  onOpenAPIKeys: (site: Site) => void
  onOpenAccounts: (site: Site) => void
  onOpenTest: (site: Site) => void
  onOpenUsageSplit: (site: Site) => void
  siteTypes: SiteTypeInfo[]
  className?: string
}) {
  const { t, i18n } = useTranslation('sites')
  const tableItems = useMemo(
    () => abnormalCollapsed ? items : [...items, ...abnormalItems],
    [abnormalCollapsed, abnormalItems, items],
  )
  const abnormalStartIndex = items.length

  function renderAbnormalGroupRow(colSpan: number) {
    if (!abnormalItems.length || !onAbnormalCollapsedChange) return null
    return (
      <tr className="border-t border-separator bg-fill-4">
        <td colSpan={colSpan} className="px-4 py-2.5">
          <button
            type="button"
            className="flex w-full items-center gap-2 text-left text-body text-label-2 transition-colors hover:text-label"
            onClick={() => onAbnormalCollapsedChange(!abnormalCollapsed)}
          >
            <ChevronDown className={cn('h-4 w-4 shrink-0 transition-transform', abnormalCollapsed && '-rotate-90')} />
            <span className="font-medium">
              {t('page.abnormalSites')}
              <span className="ml-2 text-footnote opacity-60">({abnormalItems.length})</span>
            </span>
          </button>
        </td>
      </tr>
    )
  }

  const columns = useMemo<ColumnDef<Site>[]>(() => [
    {
      accessorKey: 'name',
      header: t('table.headers.site'),
      cell: ({ row }) => {
        const site = row.original
        const oauth = isOAuthSite(site)
        const icon = siteTypeIcon(site.site_type, siteTypes) || site.icon_url
        return (
          <div className="flex items-center gap-3 min-w-0">
            <SiteTypeIcon
              src={icon}
              name={site.name}
              empty="hidden"
              muted={site.provider_status === 'unavailable'}
              className="h-7 w-7 shrink-0 rounded-inner"
              imageClassName={siteTypeIconClassName(site.site_type)}
            />
            <div className="min-w-0 space-y-0.5">
              <div className="flex items-center gap-2">
                <span className="truncate font-medium text-label">{site.name}</span>
                <SiteTypeBadge siteType={site.site_type} siteTypes={siteTypes} className="shrink-0 select-none" />
                <ProviderUnavailableBadge status={site.provider_status} />
                <SitePlanBadge site={site} />
              </div>
              <div className="text-label-2 truncate text-footnote">
                {oauth ? (
                  siteAccountEmail(site)
                ) : site.base_url ? (
                  <a href={site.base_url} target="_blank" rel="noreferrer noopener" className="hover:text-label transition-colors">{site.base_url}</a>
                ) : null}
              </div>
            </div>
          </div>
        )
      },
      meta: { cellClassName: 'min-w-0', align: 'left' as const },
    },
    {
      id: 'health',
      header: t('table.headers.health'),
      cell: ({ row }) => {
        const value = validationSnapshots[row.original.id] ?? row.original.validation
        return <SiteHealthBadge site={row.original} validation={value} />
      },
      meta: { align: 'center' as const },
    },
    {
      id: 'resources',
      header: t('table.headers.resources'),
      cell: ({ row }) => {
        const site = row.original
        const models = modelsMap[site.id] ?? EMPTY_MODELS
        const keys = apiKeysMap[site.id] ?? EMPTY_API_KEYS
        const modelCount = siteModelsDisplayCount(site, models, keys)
        const isGrok = isAccountLoginSiteType(site.site_type, siteTypes)
        const keyInfo = isOAuthSite(site) || isGrok ? null : { enabled: keys.filter((key) => key.enabled).length, total: keys.length || site.api_key_count || 0 }
        return (
          <div className="flex items-center justify-center gap-1 text-footnote tabular-nums whitespace-nowrap">
            <button type="button" className="hover:text-label text-label cursor-pointer" onClick={() => onOpenModels(site)}>
              {t('table.resources.models', { count: modelCount })}
            </button>
            {isGrok ? (
              <>
                <span className="text-label-2">/</span>
                <button type="button" className="hover:text-label text-label cursor-pointer" onClick={() => onOpenAccounts(site)}>
                  {t('table.resources.accounts', siteAccountCounts(site))}
                </button>
              </>
            ) : keyInfo ? (
              <>
                <span className="text-label-2">/</span>
                <button type="button" className="hover:text-label text-label cursor-pointer" onClick={() => onOpenAPIKeys(site)}>
                  {t('table.resources.keys', { enabled: keyInfo.enabled, total: keyInfo.total })}
                </button>
              </>
            ) : (
              <span className="text-label-2">/ -</span>
            )}
          </div>
        )
      },
      meta: { align: 'center' as const },
    },
    {
      id: 'balance',
      header: t('table.headers.balance'),
      cell: ({ row }) => <SiteBalanceCell site={row.original} apiKeys={apiKeysMap[row.original.id] ?? EMPTY_API_KEYS} />,
      meta: { align: 'center' as const, cellClassName: 'whitespace-nowrap' },
    },
    {
      id: 'usage',
      header: t('table.headers.usage'),
      cell: ({ row }) => {
        const site = row.original
        const usage = row.original.usage
        if (!usage) {
          return (
            <span
              role="button"
              tabIndex={0}
              className="text-body text-label-2 transition-colors hover:text-tint"
              onClick={() => onOpenUsageSplit(site)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  onOpenUsageSplit(site)
                }
              }}
            >
              -
            </span>
          )
        }
        const totalTokens = usage.total_tokens ?? 0
        const cost = usage.estimated_cost ?? 0
        const currency = usage.currency?.trim().toUpperCase()
        const costPrefix = currency === 'CNY' || currency === 'RMB' ? '¥' : '$'
        return (
          <div
            role="button"
            tabIndex={0}
            className="flex flex-col items-center gap-0.5 text-footnote tabular-nums whitespace-nowrap"
            title={t('table.headers.usage')}
            onClick={() => onOpenUsageSplit(site)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                onOpenUsageSplit(site)
              }
            }}
          >
            <span className="text-label">{formatCompactTokens(totalTokens)}</span>
            <span className="text-label-2">{costPrefix}{cost.toFixed(2)}</span>
          </div>
        )
      },
      meta: { align: 'center' as const },
    },
    {
      id: 'priority',
      header: t('table.headers.priority'),
      cell: ({ row }) => <span className="text-body text-label tabular-nums">{formatRoutingPriority(row.original.routing_priority)}</span>,
      meta: { align: 'center' as const },
    },
    {
      id: 'routing',
      header: t('table.headers.routing'),
      cell: ({ row }) => {
        const site = row.original
        const enableBlocked = isSiteEnableBlockedByAbnormalState(site, validationSnapshots[site.id] ?? site.validation)
        return (
          <Switch
            checked={site.enabled}
            disabled={togglingSiteId === site.id || enableBlocked}
            aria-label={t('table.ariaLabels.toggle', { name: site.name })}
            onCheckedChange={() => {
              if (enableBlocked) return
              onToggleEnabled(site)
            }}
          />
        )
      },
      meta: { align: 'center' as const },
    },
    {
      id: 'updated_at',
      header: () => (
        <button
          type="button"
          className="hover:text-label transition-colors cursor-pointer"
          title={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
          aria-label={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
          onClick={() => onUpdatedAtModeChange(updatedAtMode === 'absolute' ? 'relative' : 'absolute')}
        >
          {t('table.headers.updatedAt')}
        </button>
      ),
      cell: ({ row }) => {
        const value = siteDisplayUpdatedAt(row.original)
        const label = updatedAtMode === 'absolute'
          ? formatDateTime(value, i18n.language)
          : formatRelativeDateTime(value, now, i18n.language, t)
        return (
          <button
            type="button"
            className="text-label-2 hover:text-label cursor-pointer text-body tabular-nums"
            title={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
            aria-label={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
            onClick={() => onUpdatedAtModeChange(updatedAtMode === 'absolute' ? 'relative' : 'absolute')}
          >
            {label}
          </button>
        )
      },
      meta: { align: 'center' as const, resizeLabel: t('table.headers.updatedAt') },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const isRefreshing = refreshingSiteIds.includes(row.original.id)
        return (
          <div className="flex items-center gap-0.5">
            <Button size="icon-sm" variant="ghost" className="size-7" disabled={isRefreshing || togglingSiteId === row.original.id} onClick={(event) => { event.stopPropagation(); onRefresh(row.original.id) }} aria-label={t('table.ariaLabels.refresh', { name: row.original.name })}>
              {isRefreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
            </Button>
            <Button size="icon-sm" variant="ghost" className="size-7" disabled={isRefreshing || togglingSiteId === row.original.id} onClick={(event) => { event.stopPropagation(); onOpenTest(row.original) }} aria-label={t('table.ariaLabels.test', { name: row.original.name })}>
              <FlaskConical className="h-4 w-4" />
            </Button>
            <Button size="icon-sm" variant="ghost" className="size-7" disabled={togglingSiteId === row.original.id} onClick={(event) => { event.stopPropagation(); onEdit(row.original) }} aria-label={t('table.ariaLabels.edit', { name: row.original.name })}>
              <PencilLine className="h-4 w-4" />
            </Button>
            <Button size="icon-sm" variant="ghost" className="size-7 text-danger hover:text-danger" disabled={deletingSiteId === row.original.id || togglingSiteId === row.original.id} onClick={(event) => { event.stopPropagation(); onDelete(row.original) }} aria-label={t('table.ariaLabels.delete', { name: row.original.name })}>
              {deletingSiteId === row.original.id ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
            </Button>
          </div>
        )
      },
      // 操作列固定在右侧：表格横向滚动时编辑 / 删除等按钮始终可见
      meta: { align: 'left' as const, className: 'table-sticky-end', cellClassName: 'px-2', headerClassName: 'px-2' },
    },
  ], [apiKeysMap, deletingSiteId, i18n.language, modelsMap, now, onDelete, onEdit, onOpenAPIKeys, onOpenAccounts, onOpenModels, onOpenTest, onOpenUsageSplit, onRefresh, onToggleEnabled, onUpdatedAtModeChange, refreshingSiteIds, siteTypes, t, togglingSiteId, updatedAtMode, validationSnapshots])

  return (
    <DataTable
      columnSizing={SITES_COLUMN_SIZING}
      stickyHeader="container"
      columns={columns}
      data={tableItems}
      getRowId={(site) => site.id}
      className={className}
      renderRowBefore={(_site, index, colSpan) => (
        !abnormalCollapsed && abnormalItems.length > 0 && index === abnormalStartIndex
          ? renderAbnormalGroupRow(colSpan)
          : null
      )}
      renderBodyAppend={(colSpan) => (
        abnormalCollapsed && abnormalItems.length > 0 ? renderAbnormalGroupRow(colSpan) : null
      )}
    />
  )
}
