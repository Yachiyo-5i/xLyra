import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, FlaskConical, KeyRound, LoaderCircle, Pencil, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  getOAuthConnection,
  oauthQueryKeys,
  type OAuthConnectionListItem,
} from '@/features/oauth/api/oauth'
import { OAuthEditSheet } from '@/features/oauth/components/oauth-edit-sheet'
import { OAuthErrorPreview } from '@/features/oauth/components/oauth-error-preview'
import { QuotaPanel } from '@/features/oauth/components/oauth-quota-panel'
import { OAuthRefreshWarningBadge } from '@/features/oauth/components/oauth-refresh-warning-badge'
import {
  connectionStatusLabel,
  formatOAuthUpdatedAt,
  formatUsage,
  getOAuthSources,
  oauthConnectionEffectiveStatus,
  oauthConnectionEnableBlockedByStatus,
  oauthConnectionErrorMessage,
  oauthEnabledModelCount,
  oauthRefreshWarning,
  planBadgeVariant,
  providerLabel,
  type OAuthUpdatedAtDisplayMode,
} from '@/features/oauth/lib/oauth-utils'
import { sitesQueryKeys, type Site } from '@/features/sites/api/sites'
import { SiteModelsDraw } from '@/features/sites/components/site-models-draw'
import { isSiteEnableBlockedByAbnormalState } from '@/features/sites/lib/site-utils'

type MobileOAuthConnectionCardProps = {
  item: OAuthConnectionListItem
  groupLabel: string
  refreshing: boolean
  toggling: boolean
  deleting: boolean
  exporting: boolean
  onRefresh: () => void
  onExport: () => void
  onOpenTest: (site: Site) => void
  onOpenUsageSplit: () => void
  onToggleEnabled: (enabled: boolean) => void
  onDelete: () => void
  onReconnect: () => void
  updatedAtMode: OAuthUpdatedAtDisplayMode
  now: number
  onUpdatedAtModeChange: (mode: OAuthUpdatedAtDisplayMode) => void
}

export function MobileOAuthConnectionCard({
  item,
  groupLabel,
  refreshing,
  toggling,
  deleting,
  exporting,
  onRefresh,
  onExport,
  onOpenTest,
  onOpenUsageSplit,
  onToggleEnabled,
  onDelete,
  onReconnect,
  updatedAtMode,
  now,
  onUpdatedAtModeChange,
}: MobileOAuthConnectionCardProps) {
  const { t, i18n } = useTranslation('oauth')
  const queryClient = useQueryClient()
  const detailQuery = useQuery({
    queryKey: oauthQueryKeys.detail(item.id),
    queryFn: () => getOAuthConnection(item.id),
    staleTime: 30_000,
  })
  const detail = detailQuery.data?.connection
  const connection = detail ?? item
  const sources = getOAuthSources(t)
  const source = sources.find((sourceItem) => sourceItem.value === connection.provider)
  const models = detail?.models
  const quota = detail?.quota
  const planType = detail?.plan_type
  const boundSite = connection.site
  const routeEnabled = boundSite ? boundSite.enabled : null
  const [modelsSite, setModelsSite] = useState<Site | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const updatedAtValue = connection.last_sync_at ?? connection.last_refresh_at
  const updatedAtLabel = formatOAuthUpdatedAt(updatedAtValue, updatedAtMode, now, i18n.language, t)
  const nextUpdatedAtMode = updatedAtMode === 'relative' ? 'absolute' : 'relative'
  const refreshWarning = oauthRefreshWarning(connection, t)
  const enabledModelCount = oauthEnabledModelCount(models)
  const effectiveStatus = oauthConnectionEffectiveStatus(connection)
  const connectionError = oauthConnectionErrorMessage(connection)
  const enableBlocked = Boolean(boundSite && !boundSite.enabled && (
    oauthConnectionEnableBlockedByStatus(connection) ||
    isSiteEnableBlockedByAbnormalState(boundSite)
  ))

  function handleModelsChanged() {
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: oauthQueryKeys.connections() }),
      queryClient.invalidateQueries({ queryKey: oauthQueryKeys.detail(connection.id) }),
    ])
  }

  return (
    <article className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] p-3 shadow-[var(--button-secondary-shadow)]">
      <div className="flex items-start gap-3">
        {source?.iconPath ? <img src={source.iconPath} alt="" className="mt-0.5 h-8 w-8 shrink-0 rounded-lg object-contain" /> : null}
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h3 className="min-w-0 truncate text-base font-semibold text-foreground">{providerLabel(connection.provider)}</h3>
            {planType ? <Badge variant={planBadgeVariant(planType)} className="shrink-0 px-1.5 py-0 text-[11px]">{planType}</Badge> : null}
            <StatusBadge status={effectiveStatus === 'connected' ? 'healthy' : effectiveStatus === 'error' ? 'error' : 'warning'} className="shrink-0 px-1.5 py-0 text-[11px]">
              {connectionStatusLabel(effectiveStatus, t)}
            </StatusBadge>
            {refreshWarning ? (
              <OAuthRefreshWarningBadge label={t('card.nonRefreshable')} message={refreshWarning} />
            ) : null}
          </div>
          <div className="mt-1.5 min-w-0 truncate text-xs text-muted-soft">
            {connection.email ?? connection.account_id ?? '-'}
          </div>
        </div>
        {routeEnabled !== null && boundSite?.id ? (
          <Switch
            checked={routeEnabled}
            disabled={toggling || enableBlocked}
            onCheckedChange={(checked) => {
              if (checked && enableBlocked) return
              onToggleEnabled(checked)
            }}
          />
        ) : null}
      </div>

      <div className="mt-3 grid grid-cols-3 gap-2 text-xs">
        <MobileMetric label={t('card.site')} value={connection.site?.name ?? '-'} />
        <MobileMetric label={t('card.group')} value={groupLabel} />
        <MobileMetricButton
          label={t('card.usage')}
          value={formatUsage(connection.site?.usage)}
          disabled={!connection.site_id && !boundSite?.id}
          title={t('card.actions.usageSplit')}
          ariaLabel={t('card.actions.usageSplit')}
          onClick={onOpenUsageSplit}
        />
        <MobileMetric label={t('card.weight')} value={connection.site?.routing_priority != null ? connection.site.routing_priority.toFixed(1) : '-'} />
        <MobileMetricButton
          label={t('card.models')}
          value={detailQuery.isError ? t('card.modelsFailed') : detailQuery.isLoading ? t('card.modelsLoading') : t('card.modelsCount', { count: enabledModelCount })}
          disabled={!boundSite?.id}
          onClick={() => {
            if (boundSite?.id) setModelsSite(boundSite)
          }}
        />
        <MobileMetricButton
          label={t('card.updatedAt')}
          value={updatedAtLabel}
          title={updatedAtMode === 'relative' ? t('card.updatedAtMode.showAbsolute') : t('card.updatedAtMode.showRelative')}
          ariaLabel={updatedAtMode === 'relative' ? t('card.updatedAtMode.showAbsolute') : t('card.updatedAtMode.showRelative')}
          onClick={() => onUpdatedAtModeChange(nextUpdatedAtMode)}
        />
      </div>

      {effectiveStatus !== 'connected' && connectionError ? (
        <div className="mt-3">
          <OAuthErrorPreview label={t('card.errorInfo')} message={connectionError} compact />
        </div>
      ) : (
        <div className="mt-3">
          <QuotaPanel connectionId={connection.id} provider={connection.provider} quota={quota} loading={detailQuery.isLoading} resetCreditsData={detail?.reset_credits} />
        </div>
      )}

      <div className="mt-3 flex items-center justify-end gap-1 border-t border-[hsl(var(--glass-divider))] pt-2">
        {effectiveStatus === 'reconnect_required' ? (
          <Button
            className="h-9 w-9"
            size="icon"
            variant="ghost"
            disabled={refreshing || toggling || !boundSite?.id}
            onClick={onReconnect}
            aria-label={t('card.actions.reconnect')}
          >
            <KeyRound className="h-4 w-4" />
          </Button>
        ) : null}
        <Button
          className="h-9 w-9"
          size="icon"
          variant="ghost"
          disabled={refreshing || toggling}
          onClick={onRefresh}
          aria-label={t('card.actions.refresh')}
        >
          {refreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
        </Button>
        {connection.provider === 'codex' ? (
          <Button
            className="h-9 w-9"
            size="icon"
            variant="ghost"
            disabled={exporting || toggling}
            onClick={onExport}
            aria-label={t('card.actions.export')}
          >
            {exporting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
          </Button>
        ) : null}
        <Button
          className="h-9 w-9"
          size="icon"
          variant="ghost"
          disabled={!boundSite || refreshing || toggling}
          onClick={() => { if (boundSite) onOpenTest(boundSite) }}
          aria-label={t('card.actions.test')}
        >
          <FlaskConical className="h-4 w-4" />
        </Button>
        <Button
          className="h-9 w-9"
          size="icon"
          variant="ghost"
          disabled={!connection.site || toggling}
          onClick={() => setEditOpen(true)}
          aria-label={t('card.actions.edit')}
        >
          <Pencil className="h-4 w-4" />
        </Button>
        <Button
          className="h-9 w-9 text-red-400 hover:text-red-300"
          size="icon"
          variant="ghost"
          disabled={deleting || toggling}
          onClick={onDelete}
          aria-label={t('card.actions.delete')}
        >
          {deleting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
        </Button>
      </div>

      <SiteModelsDraw
        site={modelsSite}
        models={[]}
        apiKeys={[]}
        title={t('card.modelsDrawTitle', { provider: providerLabel(connection.provider) })}
        onModelsChanged={handleModelsChanged}
        onOpenChange={(open) => { if (!open) setModelsSite(null) }}
      />

      <OAuthEditSheet
        open={editOpen}
        site={connection.site ?? null}
        email={connection.email}
        onOpenChange={setEditOpen}
        onSaved={() => {
          void Promise.all([
            queryClient.invalidateQueries({ queryKey: oauthQueryKeys.connections() }),
            queryClient.invalidateQueries({ queryKey: oauthQueryKeys.detail(item.id) }),
            boundSite?.id ? queryClient.invalidateQueries({ queryKey: sitesQueryKeys.detail(boundSite.id) }) : Promise.resolve(),
          ])
        }}
      />
    </article>
  )
}

function MobileMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 px-1 py-1">
      <span className="block text-[11px] text-muted-soft">{label}</span>
      <span className="mt-1 block truncate font-medium text-foreground tabular-nums" title={value}>{value}</span>
    </div>
  )
}

function MobileMetricButton({
  ariaLabel,
  disabled,
  label,
  onClick,
  title,
  value,
}: {
  ariaLabel?: string
  disabled?: boolean
  label: string
  onClick: () => void
  title?: string
  value: string
}) {
  return (
    <button
      type="button"
      className="min-w-0 px-1 py-1 text-left transition-colors hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
      disabled={disabled}
      onClick={onClick}
      title={title}
      aria-label={ariaLabel}
    >
      <span className="block text-[11px] text-muted-soft">{label}</span>
      <span className="mt-1 block truncate font-medium text-foreground tabular-nums" title={value}>{value}</span>
    </button>
  )
}
