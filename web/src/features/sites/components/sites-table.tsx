import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { ChevronDown, FlaskConical, LoaderCircle, PencilLine, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DataTable } from '@/components/common/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { SiteBalanceCell } from '@/features/sites/components/site-balance-cell'
import { SiteHealthBadge } from '@/features/sites/components/site-health-badge'
import { SitePlanBadge } from '@/features/sites/components/site-plan-badge'
import type { Site, SiteAPIKey, SiteModel, SiteTypeInfo } from '@/features/sites/api/sites'
import {
  formatCompactTokens,
  formatDateTime,
  formatRelativeDateTime,
  formatRoutingPriority,
  formatSiteTypeLabel,
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

export function SitesTable({
  items, modelsMap, apiKeysMap, validationSnapshots, refreshingSiteIds, togglingSiteId, deletingSiteId,
  abnormalItems = [], abnormalCollapsed = true, onAbnormalCollapsedChange,
  updatedAtMode, now, onUpdatedAtModeChange,
  onRefresh, onToggleEnabled, onEdit, onDelete, onOpenModels, onOpenAPIKeys, onOpenGrokAccounts, onOpenTest, onOpenUsageSplit, resolvedMode, siteTypes,
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
  onOpenGrokAccounts: (site: Site) => void
  onOpenTest: (site: Site) => void
  onOpenUsageSplit: (site: Site) => void
  resolvedMode: 'light' | 'dark'
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
      <tr className="border-t border-[hsl(var(--glass-divider))] bg-[hsl(var(--surface-subtle)/0.45)]">
        <td colSpan={colSpan} className="px-4 py-2.5">
          <button
            type="button"
            className="flex w-full items-center gap-2 text-left text-sm text-muted-soft transition-colors hover:text-foreground"
            onClick={() => onAbnormalCollapsedChange(!abnormalCollapsed)}
          >
            <ChevronDown className={cn('h-4 w-4 shrink-0 transition-transform', abnormalCollapsed && '-rotate-90')} />
            <span className="font-medium">
              {t('page.abnormalSites')}
              <span className="ml-2 text-xs opacity-60">({abnormalItems.length})</span>
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
        const icon = siteTypeIcon(site.site_type, siteTypes, resolvedMode) || site.icon_url
        return (
          <div className="flex items-center gap-3 min-w-0">
            {icon ? <img src={icon} alt="" className={cn('h-7 w-7 shrink-0 rounded-lg object-contain', siteTypeIconClassName(site.site_type))} /> : null}
            <div className="min-w-0 space-y-0.5">
              <div className="flex items-center gap-2">
                <span className="truncate font-medium text-foreground">{site.name}</span>
                <Badge variant="outline" className="shrink-0 text-[11px] px-1.5 py-0 select-none">{formatSiteTypeLabel(site.site_type)}</Badge>
                <SitePlanBadge site={site} />
              </div>
              <div className="text-muted-soft truncate text-xs">
                {oauth ? (
                  siteAccountEmail(site)
                ) : site.base_url ? (
                  <a href={site.base_url} target="_blank" rel="noreferrer noopener" className="hover:text-foreground transition-colors">{site.base_url}</a>
                ) : null}
              </div>
            </div>
          </div>
        )
      },
      meta: { className: 'w-[24%]', cellClassName: 'min-w-0', align: 'left' as const },
    },
    {
      id: 'health',
      header: t('table.headers.health'),
      cell: ({ row }) => {
        const value = validationSnapshots[row.original.id] ?? row.original.validation
        return <SiteHealthBadge site={row.original} validation={value} />
      },
      meta: { className: 'w-[8%]', align: 'center' as const },
    },
    {
      id: 'resources',
      header: t('table.headers.resources'),
      cell: ({ row }) => {
        const site = row.original
        const models = modelsMap[site.id] ?? EMPTY_MODELS
        const keys = apiKeysMap[site.id] ?? EMPTY_API_KEYS
        const modelCount = siteModelsDisplayCount(site, models, keys)
        const isGrok = site.site_type === 'grok'
        const keyInfo = isOAuthSite(site) || isGrok ? null : { enabled: keys.filter((key) => key.enabled).length, total: keys.length || site.api_key_count || 0 }
        return (
          <div className="flex items-center justify-center gap-1 text-xs tabular-nums whitespace-nowrap">
            <button type="button" className="hover:text-foreground text-foreground cursor-pointer" onClick={() => onOpenModels(site)}>
              {t('table.resources.models', { count: modelCount })}
            </button>
            {isGrok ? (
              <>
                <span className="text-muted-soft">/</span>
                <button type="button" className="hover:text-foreground text-foreground cursor-pointer" onClick={() => onOpenGrokAccounts(site)}>
                  {t('table.resources.accounts', { count: site.grok_account_count ?? site.api_key_count ?? 0 })}
                </button>
              </>
            ) : keyInfo ? (
              <>
                <span className="text-muted-soft">/</span>
                <button type="button" className="hover:text-foreground text-foreground cursor-pointer" onClick={() => onOpenAPIKeys(site)}>
                  {t('table.resources.keys', { enabled: keyInfo.enabled, total: keyInfo.total })}
                </button>
              </>
            ) : (
              <span className="text-muted-soft">/ -</span>
            )}
          </div>
        )
      },
      meta: { className: 'w-[13%]', align: 'center' as const },
    },
    {
      id: 'balance',
      header: t('table.headers.balance'),
      cell: ({ row }) => <SiteBalanceCell site={row.original} apiKeys={apiKeysMap[row.original.id] ?? EMPTY_API_KEYS} />,
      meta: { className: 'w-[8%]', align: 'center' as const },
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
              className="text-sm text-muted-soft transition-colors hover:text-primary"
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
            className="flex flex-col items-center gap-0.5 text-xs tabular-nums whitespace-nowrap"
            title={t('table.headers.usage')}
            onClick={() => onOpenUsageSplit(site)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                onOpenUsageSplit(site)
              }
            }}
          >
            <span className="text-foreground">{formatCompactTokens(totalTokens)}</span>
            <span className="text-muted-soft">{costPrefix}{cost.toFixed(2)}</span>
          </div>
        )
      },
      meta: { className: 'w-[8%]', align: 'center' as const },
    },
    {
      id: 'priority',
      header: t('table.headers.priority'),
      cell: ({ row }) => <span className="text-sm text-foreground tabular-nums">{formatRoutingPriority(row.original.routing_priority)}</span>,
      meta: { className: 'w-[6%]', align: 'center' as const },
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
      meta: { className: 'w-[7%]', align: 'center' as const },
    },
    {
      id: 'updated_at',
      header: () => (
        <button
          type="button"
          className="hover:text-foreground transition-colors cursor-pointer"
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
            className="text-muted-soft hover:text-foreground cursor-pointer text-sm tabular-nums"
            title={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
            aria-label={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
            onClick={() => onUpdatedAtModeChange(updatedAtMode === 'absolute' ? 'relative' : 'absolute')}
          >
            {label}
          </button>
        )
      },
      meta: { className: 'w-[10%]', align: 'center' as const },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const isRefreshing = refreshingSiteIds.includes(row.original.id)
        return (
          <div className="flex items-center gap-1">
            <Button size="icon" variant="ghost" className="h-8 w-8" disabled={isRefreshing || togglingSiteId === row.original.id} onClick={(event) => { event.stopPropagation(); onRefresh(row.original.id) }} aria-label={t('table.ariaLabels.refresh', { name: row.original.name })}>
              {isRefreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
            </Button>
            <Button size="icon" variant="ghost" className="h-8 w-8" disabled={isRefreshing || togglingSiteId === row.original.id} onClick={(event) => { event.stopPropagation(); onOpenTest(row.original) }} aria-label={t('table.ariaLabels.test', { name: row.original.name })}>
              <FlaskConical className="h-4 w-4" />
            </Button>
            <Button size="icon" variant="ghost" className="h-8 w-8" disabled={togglingSiteId === row.original.id} onClick={(event) => { event.stopPropagation(); onEdit(row.original) }} aria-label={t('table.ariaLabels.edit', { name: row.original.name })}>
              <PencilLine className="h-4 w-4" />
            </Button>
            <Button size="icon" variant="ghost" className="h-8 w-8 text-red-400 hover:text-red-300" disabled={deletingSiteId === row.original.id || togglingSiteId === row.original.id} onClick={(event) => { event.stopPropagation(); onDelete(row.original) }} aria-label={t('table.ariaLabels.delete', { name: row.original.name })}>
              {deletingSiteId === row.original.id ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
            </Button>
          </div>
        )
      },
      meta: { className: 'w-[11%]', align: 'left' as const },
    },
  ], [apiKeysMap, deletingSiteId, i18n.language, modelsMap, now, onDelete, onEdit, onOpenAPIKeys, onOpenGrokAccounts, onOpenModels, onOpenTest, onOpenUsageSplit, onRefresh, onToggleEnabled, onUpdatedAtModeChange, refreshingSiteIds, resolvedMode, siteTypes, t, togglingSiteId, updatedAtMode, validationSnapshots])

  return (
    <DataTable
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
