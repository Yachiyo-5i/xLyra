import { ChevronDown, FlaskConical, LoaderCircle, PencilLine, RotateCcw, Trash2 } from 'lucide-react'
import { forwardRef, type ComponentPropsWithoutRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Draw, DrawBody, DrawContent, DrawDescription, DrawHeader, DrawTitle, DrawTrigger } from '@/components/ui/draw'
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
  resolvedMode,
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
  resolvedMode: 'light' | 'dark'
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
            resolvedMode={resolvedMode}
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
  resolvedMode,
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
  resolvedMode: 'light' | 'dark'
  siteTypes: SiteTypeInfo[]
}) {
  const { t, i18n } = useTranslation('sites')
  const oauth = isOAuthSite(site)
  const icon = siteTypeIcon(site.site_type, siteTypes, resolvedMode) || site.icon_url
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
  const statusBadge = <SiteHealthBadge site={site} validation={validation} className="shrink-0 px-1.5 py-0 text-[11px]" />
  const enableBlocked = isSiteEnableBlockedByAbnormalState(site, validation)

  return (
    <article className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] p-3 shadow-[var(--button-secondary-shadow)]">
      <div className="flex items-start gap-3">
        {icon ? <img src={icon} alt="" className={cn('mt-0.5 h-8 w-8 shrink-0 rounded-lg object-contain', siteTypeIconClassName(site.site_type))} /> : null}
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h3 className="min-w-0 truncate text-base font-semibold text-foreground">{site.name}</h3>
            <Badge variant="outline" className="shrink-0 px-1.5 py-0 text-[11px]">{formatSiteTypeLabel(site.site_type, siteTypes)}</Badge>
            <SitePlanBadge site={site} />
            {statusBadge}
          </div>
          <div className="mt-1.5 flex min-w-0 items-center gap-1.5 text-xs">
            <div className="min-w-0 flex-1 truncate text-muted-soft">
              {oauth ? (
                siteAccountEmail(site)
              ) : site.base_url ? (
                <a href={site.base_url} target="_blank" rel="noreferrer noopener" className="transition-colors hover:text-foreground">{site.base_url}</a>
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

      <div className="mt-3 grid grid-cols-3 gap-2 text-xs">
        <MobileSiteBalance site={site} apiKeys={apiKeys} />
        <MobileMetric label={t('table.headers.priority')} value={formatRoutingPriority(site.routing_priority)} />
        <MobileMetricButton
          label={t('table.headers.usage')}
          value={usage ? `${formatCompactTokens(totalTokens, false)} / ${costPrefix}${cost.toFixed(2)}` : '-'}
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
          className="min-w-0 px-2 py-2 text-left transition-colors hover:text-primary"
          title={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
          aria-label={updatedAtMode === 'absolute' ? t('table.updatedAtMode.showRelative') : t('table.updatedAtMode.showAbsolute')}
          onClick={() => onUpdatedAtModeChange(updatedAtMode === 'absolute' ? 'relative' : 'absolute')}
        >
          <span className="block text-[11px] text-muted-soft">{t('table.headers.updatedAt')}</span>
          <span className="mt-1 block truncate font-medium text-foreground tabular-nums">{updatedLabel}</span>
        </button>
      </div>

      <div className="mt-3 flex items-center justify-end gap-1 border-t border-[hsl(var(--glass-divider))] pt-2">
        <Button size="icon" variant="ghost" className="h-9 w-9" disabled={isRefreshing || isToggling} onClick={() => onRefresh(site.id)} aria-label={t('table.ariaLabels.refresh', { name: site.name })}>
          {isRefreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
        </Button>
        <Button size="icon" variant="ghost" className="h-9 w-9" disabled={isRefreshing || isToggling} onClick={() => onOpenTest(site)} aria-label={t('table.ariaLabels.test', { name: site.name })}>
          <FlaskConical className="h-4 w-4" />
        </Button>
        <Button size="icon" variant="ghost" className="h-9 w-9" disabled={isToggling} onClick={() => onEdit(site)} aria-label={t('table.ariaLabels.edit', { name: site.name })}>
          <PencilLine className="h-4 w-4" />
        </Button>
        <Button size="icon" variant="ghost" className="h-9 w-9 text-red-400 hover:text-red-300" disabled={isDeleting || isToggling} onClick={() => onDelete(site)} aria-label={t('table.ariaLabels.delete', { name: site.name })}>
          {isDeleting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
        </Button>
      </div>
    </article>
  )
}

function MobileSiteBalance({ site, apiKeys }: { site: Site; apiKeys: SiteAPIKey[] }) {
  const { t } = useTranslation('sites')
  const value = formatSiteBalance(site)

  if (!isSub2APIQuotaSite(site) || apiKeys.length === 0) {
    return <MobileMetric label={t('table.headers.balance')} value={value} />
  }

  return (
    <Draw>
      <DrawTrigger asChild>
        <MobileMetricButton label={t('table.headers.balance')} value={value} />
      </DrawTrigger>
      <DrawContent side="bottom">
        <DrawHeader>
          <DrawTitle>{site.name} · {t('table.headers.balance')}</DrawTitle>
          <DrawDescription className="sr-only">{site.name}</DrawDescription>
        </DrawHeader>
        <DrawBody className="text-sm leading-6">
          <SiteBalanceDetailsContent site={site} apiKeys={apiKeys} />
        </DrawBody>
      </DrawContent>
    </Draw>
  )
}

function MobileMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 px-2 py-2">
      <span className="block text-[11px] text-muted-soft">{label}</span>
      <span className="mt-1 block truncate font-medium text-foreground tabular-nums" title={value}>{value}</span>
    </div>
  )
}

const MobileMetricButton = forwardRef<HTMLButtonElement, Omit<ComponentPropsWithoutRef<'button'>, 'children'> & { label: string; value: string }>(
  ({ label, value, className, ...props }, ref) => (
    <button ref={ref} type="button" className={cn('min-w-0 px-2 py-2 text-left transition-colors hover:text-primary', className)} {...props}>
      <span className="block text-[11px] text-muted-soft">{label}</span>
      <span className="mt-1 block truncate font-medium text-foreground tabular-nums" title={value}>{value}</span>
    </button>
  ),
)
MobileMetricButton.displayName = 'MobileMetricButton'

function MobileAbnormalToggle({ count, collapsed, onToggle }: { count: number; collapsed: boolean; onToggle: () => void }) {
  const { t } = useTranslation('sites')

  return (
    <div>
      <div className="mb-3 border-t border-[hsl(var(--glass-border))]" />
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded-lg bg-[hsl(var(--surface-subtle)/0.6)] px-3 py-2.5 text-left text-sm text-muted-soft transition-colors hover:text-foreground"
        onClick={onToggle}
      >
        <ChevronDown className={cn('h-4 w-4 shrink-0 transition-transform', collapsed && '-rotate-90')} />
        <span className="font-medium">
          {t('page.abnormalSites')}
          <span className="ml-2 text-xs opacity-60">({count})</span>
        </span>
      </button>
    </div>
  )
}
