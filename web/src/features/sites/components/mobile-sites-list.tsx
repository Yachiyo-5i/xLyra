import { ChevronDown, FlaskConical, LoaderCircle, PencilLine, RotateCcw, Trash2 } from 'lucide-react'
import { forwardRef, type ComponentPropsWithoutRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { HoverDetails } from '@/components/common/hover-details'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { SiteBalanceDetailsContent } from '@/features/sites/components/site-balance-cell'
import { SiteHealthBadge } from '@/features/sites/components/site-health-badge'
import { SitePlanBadge } from '@/features/sites/components/site-plan-badge'
import type { Site, SiteAPIKey, SiteModel, SiteTypeInfo } from '@/features/sites/api/sites'
import {
  formatCompactTokens,
  formatDateTime,
  formatRelativeDateTime,
  formatRoutingPriority,
  formatSiteBalance,
  formatSiteTypeLabel,
  isOAuthSite,
  isSub2APIQuotaSite,
  isSiteEnableBlockedByAbnormalState,
  siteAccountEmail,
  siteBalanceDetails,
  siteDisplayUpdatedAt,
  siteModelsDisplayCount,
  siteTypeIcon,
  siteTypeIconClassName,
} from '@/features/sites/lib/site-utils'
import type { SiteUpdatedAtDisplayMode, ValidationSnapshot } from '@/features/sites/lib/types'

const EMPTY_MODELS: SiteModel[] = []
const EMPTY_API_KEYS: SiteAPIKey[] = []

export function MobileSitesList({
  items,
  abnormalItems = [],
  abnormalCollapsed = true,
  onAbnormalCollapsedChange,
  modelsMap,
  apiKeysMap,
  validationSnapshots,
  refreshingSiteIds,
  togglingSiteId,
  deletingSiteId,
  updatedAtMode,
  now,
  onUpdatedAtModeChange,
  onRefresh,
  onToggleEnabled,
  onEdit,
  onDelete,
  onOpenModels,
  onOpenAPIKeys,
  onOpenGrokAccounts,
  onOpenTest,
  onOpenUsageSplit,
  siteTypes,
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
  siteTypes: SiteTypeInfo[]
  className?: string
}) {
  const visibleItems = abnormalCollapsed ? items : [...items, ...abnormalItems]
  const abnormalStartIndex = items.length

  return (
    <div className={cn('space-y-3', className)}>
      {visibleItems.map((site, index) => (
        <div key={site.id} className="space-y-3">
          {!abnormalCollapsed && abnormalItems.length > 0 && index === abnormalStartIndex ? (
            <MobileAbnormalToggle
              count={abnormalItems.length}
              collapsed={abnormalCollapsed}
              onToggle={() => onAbnormalCollapsedChange?.(!abnormalCollapsed)}
            />
          ) : null}
          <MobileSiteCard
            site={site}
            models={modelsMap[site.id] ?? EMPTY_MODELS}
            apiKeys={apiKeysMap[site.id] ?? EMPTY_API_KEYS}
            validation={validationSnapshots[site.id] ?? site.validation}
            isRefreshing={refreshingSiteIds.includes(site.id)}
            isToggling={togglingSiteId === site.id}
            isDeleting={deletingSiteId === site.id}
            updatedAtMode={updatedAtMode}
            now={now}
            onUpdatedAtModeChange={onUpdatedAtModeChange}
            onRefresh={onRefresh}
            onToggleEnabled={onToggleEnabled}
            onEdit={onEdit}
            onDelete={onDelete}
            onOpenModels={onOpenModels}
            onOpenAPIKeys={onOpenAPIKeys}
            onOpenGrokAccounts={onOpenGrokAccounts}
            onOpenTest={onOpenTest}
            onOpenUsageSplit={onOpenUsageSplit}
            siteTypes={siteTypes}
          />
        </div>
      ))}

      {abnormalCollapsed && abnormalItems.length > 0 ? (
        <MobileAbnormalToggle
          count={abnormalItems.length}
          collapsed={abnormalCollapsed}
          onToggle={() => onAbnormalCollapsedChange?.(!abnormalCollapsed)}
        />
      ) : null}
    </div>
  )
}

function MobileSiteCard({
  site,
  models,
  apiKeys,
  validation,
  isRefreshing,
  isToggling,
  isDeleting,
  updatedAtMode,
  now,
  onUpdatedAtModeChange,
  onRefresh,
  onToggleEnabled,
  onEdit,
  onDelete,
  onOpenModels,
  onOpenAPIKeys,
  onOpenGrokAccounts,
  onOpenTest,
  onOpenUsageSplit,
  siteTypes,
}: {
  site: Site
  models: SiteModel[]
  apiKeys: SiteAPIKey[]
  validation: ValidationSnapshot | Site['validation'] | undefined
  isRefreshing: boolean
  isToggling: boolean
  isDeleting: boolean
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
  siteTypes: SiteTypeInfo[]
}) {
  const { t, i18n } = useTranslation('sites')
  const oauth = isOAuthSite(site)
  const icon = siteTypeIcon(site.site_type, siteTypes) || site.icon_url
  const modelCount = siteModelsDisplayCount(site, models, apiKeys)
  const isGrok = site.site_type === 'grok'
  const keyInfo = oauth || isGrok ? null : { enabled: apiKeys.filter((key) => key.enabled).length, total: apiKeys.length || site.api_key_count || 0 }
  const usage = site.usage
  const totalTokens = usage?.total_tokens ?? 0
  const cost = usage?.estimated_cost ?? 0
  const currency = usage?.currency?.trim().toUpperCase()
  const costPrefix = currency === 'CNY' || currency === 'RMB' ? '¥' : '$'
  const updatedValue = siteDisplayUpdatedAt(site)
  const updatedLabel = updatedAtMode === 'absolute'
    ? formatDateTime(updatedValue, i18n.language)
    : formatRelativeDateTime(updatedValue, now, i18n.language, t)
  const statusBadge = <SiteHealthBadge site={site} validation={validation} className="shrink-0" />
  const enableBlocked = isSiteEnableBlockedByAbnormalState(site, validation)

  return (
    // 站点卡片：名字独占标题行，标签放到下一行可换行，名字不再被挤成「h…」
    <Card className="p-4">
      <div className="flex items-start gap-3">
        {icon ? <img src={icon} alt="" className={cn('mt-0.5 size-8 shrink-0 rounded-inner object-contain', siteTypeIconClassName(site.site_type))} /> : null}
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-headline text-label">{site.name}</h3>
          <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
            <Badge variant="secondary">{formatSiteTypeLabel(site.site_type, siteTypes)}</Badge>
            <SitePlanBadge site={site} />
            {statusBadge}
          </div>
          <div className="mt-1.5 flex min-w-0 items-center gap-1.5 text-footnote">
            <div className="min-w-0 flex-1 truncate text-label-2">
              {oauth ? (
                siteAccountEmail(site)
              ) : site.base_url ? (
                <a href={site.base_url} target="_blank" rel="noreferrer noopener" className="transition-colors hover:text-label">{site.base_url}</a>
              ) : '-'}
            </div>
          </div>
        </div>
        <Switch
          checked={site.enabled}
          disabled={isToggling || enableBlocked}
          aria-label={t('table.ariaLabels.toggle', { name: site.name })}
          onCheckedChange={() => {
            if (enableBlocked) return
            onToggleEnabled(site)
          }}
        />
      </div>

      <div className="mt-3 grid grid-cols-3 gap-2 text-footnote">
        <MobileSiteBalance site={site} apiKeys={apiKeys} />
        <MobileMetric label={t('table.headers.priority')} value={formatRoutingPriority(site.routing_priority)} />
        <MobileMetricButton
          label={t('table.headers.usage')}
          value={usage ? formatCompactTokens(totalTokens, false) : '-'}
          secondary={usage ? `${costPrefix}${cost.toFixed(2)}` : undefined}
          onClick={() => onOpenUsageSplit(site)}
        />
        <MobileMetricButton
          label={t('table.headers.models')}
          value={t('table.resources.models', { count: modelCount })}
          onClick={() => onOpenModels(site)}
        />
        {isGrok ? (
          <MobileMetricButton
            label={t('table.headers.accounts')}
            value={t('table.resources.accounts', { count: site.grok_account_count ?? site.api_key_count ?? 0 })}
            onClick={() => onOpenGrokAccounts(site)}
          />
        ) : keyInfo ? (
          <MobileMetricButton
            label={t('table.headers.apiKeys')}
            value={t('table.resources.keys', { enabled: keyInfo.enabled, total: keyInfo.total })}
            onClick={() => onOpenAPIKeys(site)}
          />
        ) : (
          <MobileMetric label={t('table.headers.apiKeys')} value="-" />
        )}
        <button
          type="button"
          className="min-w-0 px-2 py-2 text-left transition-colors hover:text-tint"
          title={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
          aria-label={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
          onClick={() => onUpdatedAtModeChange(updatedAtMode === 'absolute' ? 'relative' : 'absolute')}
        >
          <span className="block text-caption-1 text-label-3">{t('table.headers.updatedAt')}</span>
          <span className="mt-1 block truncate text-callout font-medium text-label tabular-nums">{updatedLabel}</span>
        </button>
      </div>

      <div className="mt-3 flex items-center justify-end gap-1 border-t border-separator pt-2">
        <Button size="icon" variant="ghost" className="h-9 w-9" disabled={isRefreshing || isToggling} onClick={() => onRefresh(site.id)} aria-label={t('table.ariaLabels.refresh', { name: site.name })}>
          {isRefreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
        </Button>
        <Button size="icon" variant="ghost" className="h-9 w-9" disabled={isRefreshing || isToggling} onClick={() => onOpenTest(site)} aria-label={t('table.ariaLabels.test', { name: site.name })}>
          <FlaskConical className="h-4 w-4" />
        </Button>
        <Button size="icon" variant="ghost" className="h-9 w-9" disabled={isToggling} onClick={() => onEdit(site)} aria-label={t('table.ariaLabels.edit', { name: site.name })}>
          <PencilLine className="h-4 w-4" />
        </Button>
        <Button size="icon" variant="ghost" className="h-9 w-9 text-danger hover:text-danger" disabled={isDeleting || isToggling} onClick={() => onDelete(site)} aria-label={t('table.ariaLabels.delete', { name: site.name })}>
          {isDeleting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
        </Button>
      </div>
    </Card>
  )
}

function MobileSiteBalance({ site, apiKeys }: { site: Site; apiKeys: SiteAPIKey[] }) {
  const { t, i18n } = useTranslation('sites')
  const value = formatSiteBalance(site)
  const hasDetails = siteBalanceDetails(site, i18n.language).length > 0
    || (isSub2APIQuotaSite(site) && apiKeys.length > 0)

  if (!hasDetails) {
    return <MobileMetric label={t('table.headers.balance')} value={value} />
  }

  return (
    <HoverDetails
      asChild
      title={`${site.name} · ${t('table.headers.balance')}`}
      content={<SiteBalanceDetailsContent site={site} apiKeys={apiKeys} />}
    >
      <MobileMetricButton label={t('table.headers.balance')} value={value} />
    </HoverDetails>
  )
}

function MobileMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 px-2 py-2">
      <span className="block text-caption-1 text-label-3">{label}</span>
      <span className="mt-1 block truncate text-callout font-medium text-label tabular-nums" title={value}>{value}</span>
    </div>
  )
}

const MobileMetricButton = forwardRef<HTMLButtonElement, Omit<ComponentPropsWithoutRef<'button'>, 'children'> & { label: string; value: string; secondary?: string }>(
  ({ label, value, secondary, className, ...props }, ref) => (
    <button ref={ref} type="button" className={cn('min-w-0 px-2 py-2 text-left transition-colors active:text-label', className)} {...props}>
      <span className="block text-caption-1 text-label-3">{label}</span>
      <span className="mt-1 block truncate text-callout font-medium text-label tabular-nums" title={value}>{value}</span>
      {/* 用量拆成两行：Token 在上、费用在下，不再被截成「9.04B / $12…」 */}
      {secondary ? <span className="block truncate font-mono text-caption-1 text-label-3 tabular-nums">{secondary}</span> : null}
    </button>
  ),
)
MobileMetricButton.displayName = 'MobileMetricButton'

function MobileAbnormalToggle({ count, collapsed, onToggle }: { count: number; collapsed: boolean; onToggle: () => void }) {
  const { t } = useTranslation('sites')

  return (
    <div>
      <div className="mb-3 border-t border-separator" />
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded-inner bg-fill-4 px-3 py-2.5 text-left text-body text-label-2 transition-colors hover:text-label"
        onClick={onToggle}
      >
        <ChevronDown className={cn('h-4 w-4 shrink-0 transition-transform', collapsed && '-rotate-90')} />
        <span className="font-medium">
          {t('page.abnormalSites')}
          <span className="ml-2 text-footnote opacity-60">({count})</span>
        </span>
      </button>
    </div>
  )
}
