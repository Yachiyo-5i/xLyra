import { ChevronDown, FlaskConical, LoaderCircle, PencilLine, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { CardAction, CardFooter, CardHeading, CardMetric, CardMetrics, ItemCard } from '@/components/ui/card'
import { HoverDetails } from '@/components/common/hover-details'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { SiteBalanceDetailsContent } from '@/features/sites/components/site-balance-cell'
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
  formatSiteBalance,
  isAccountLoginSiteType,
  siteAccountCounts,
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
  onOpenAccounts,
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
  onOpenAccounts: (site: Site) => void
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
            onOpenAccounts={onOpenAccounts}
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
  onOpenAccounts,
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
  onOpenAccounts: (site: Site) => void
  onOpenTest: (site: Site) => void
  onOpenUsageSplit: (site: Site) => void
  siteTypes: SiteTypeInfo[]
}) {
  const { t, i18n } = useTranslation('sites')
  const oauth = isOAuthSite(site)
  const icon = siteTypeIcon(site.site_type, siteTypes) || site.icon_url
  const modelCount = siteModelsDisplayCount(site, models, apiKeys)
  const isGrok = isAccountLoginSiteType(site.site_type, siteTypes)
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
    <ItemCard>
      <CardHeading
        leading={(
          <SiteTypeIcon
            src={icon}
            name={site.name}
            empty="hidden"
            muted={site.provider_status === 'unavailable'}
            className="mt-0.5 size-8 shrink-0 rounded-inner"
            imageClassName={siteTypeIconClassName(site.site_type)}
          />
        )}
        title={site.name}
        tags={(
          <>
            <SiteTypeBadge siteType={site.site_type} siteTypes={siteTypes} />
            <ProviderUnavailableBadge status={site.provider_status} />
            <SitePlanBadge site={site} />
            {statusBadge}
          </>
        )}
        meta={oauth ? (
          siteAccountEmail(site)
        ) : site.base_url ? (
          <a href={site.base_url} target="_blank" rel="noreferrer noopener" className="transition-colors hover:text-label">{site.base_url}</a>
        ) : '-'}
        trailing={(
          <Switch
            checked={site.enabled}
            disabled={isToggling || enableBlocked}
            aria-label={t('table.ariaLabels.toggle', { name: site.name })}
            onCheckedChange={() => {
              if (enableBlocked) return
              onToggleEnabled(site)
            }}
          />
        )}
      />

      <CardMetrics columns={3}>
        <MobileSiteBalance site={site} apiKeys={apiKeys} />
        <CardMetric label={t('table.headers.priority')} value={formatRoutingPriority(site.routing_priority)} />
        <CardMetric
          label={t('table.headers.usage')}
          value={usage ? formatCompactTokens(totalTokens, false) : '-'}
          secondary={usage ? `${costPrefix}${cost.toFixed(2)}` : undefined}
          onClick={() => onOpenUsageSplit(site)}
        />
        <CardMetric
          label={t('table.headers.models')}
          value={t('table.resources.models', { count: modelCount })}
          onClick={() => onOpenModels(site)}
        />
        {isGrok ? (
          <CardMetric
            label={t('table.headers.accounts')}
            value={t('table.resources.accounts', siteAccountCounts(site))}
            onClick={() => onOpenAccounts(site)}
          />
        ) : keyInfo ? (
          <CardMetric
            label={t('table.headers.apiKeys')}
            value={t('table.resources.keys', { enabled: keyInfo.enabled, total: keyInfo.total })}
            onClick={() => onOpenAPIKeys(site)}
          />
        ) : (
          <CardMetric label={t('table.headers.apiKeys')} value="-" />
        )}
        <CardMetric
          label={t('table.headers.updatedAt')}
          value={updatedLabel}
          title={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
          onClick={() => onUpdatedAtModeChange(updatedAtMode === 'absolute' ? 'relative' : 'absolute')}
        />
      </CardMetrics>

      <CardFooter>
        <CardAction label={t('table.ariaLabels.refresh', { name: site.name })} disabled={isRefreshing || isToggling} onClick={() => onRefresh(site.id)}>
          {isRefreshing ? <LoaderCircle className="animate-spin" /> : <RotateCcw />}
        </CardAction>
        <CardAction label={t('table.ariaLabels.test', { name: site.name })} disabled={isRefreshing || isToggling} onClick={() => onOpenTest(site)}>
          <FlaskConical />
        </CardAction>
        <CardAction label={t('table.ariaLabels.edit', { name: site.name })} disabled={isToggling} onClick={() => onEdit(site)}>
          <PencilLine />
        </CardAction>
        <CardAction label={t('table.ariaLabels.delete', { name: site.name })} className="text-danger hover:text-danger" disabled={isDeleting || isToggling} onClick={() => onDelete(site)}>
          {isDeleting ? <LoaderCircle className="animate-spin" /> : <Trash2 />}
        </CardAction>
      </CardFooter>
    </ItemCard>
  )
}

function MobileSiteBalance({ site, apiKeys }: { site: Site; apiKeys: SiteAPIKey[] }) {
  const { t, i18n } = useTranslation('sites')
  const value = formatSiteBalance(site)
  const hasDetails = siteBalanceDetails(site, i18n.language).length > 0
    || (isSub2APIQuotaSite(site) && apiKeys.length > 0)

  if (!hasDetails) {
    return <CardMetric label={t('table.headers.balance')} value={value} />
  }

  return (
    <HoverDetails
      asChild
      title={`${site.name} · ${t('table.headers.balance')}`}
      content={<SiteBalanceDetailsContent site={site} apiKeys={apiKeys} />}
    >
      <CardMetric interactive label={t('table.headers.balance')} value={value} />
    </HoverDetails>
  )
}

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
