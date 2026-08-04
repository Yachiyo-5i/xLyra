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
import { QuotaPanel } from '@/features/oauth/components/oauth-quota-panel'
import { OAuthEditSheet } from '@/features/oauth/components/oauth-edit-sheet'
import { OAuthRefreshWarningBadge } from '@/features/oauth/components/oauth-refresh-warning-badge'
import { sitesQueryKeys, type Site } from '@/features/sites/api/sites'
import { SiteModelsDraw } from '@/features/sites/components/site-models-draw'
import { OAuthErrorPreview } from '@/features/oauth/components/oauth-error-preview'
import { isSiteEnableBlockedByAbnormalState } from '@/features/sites/lib/site-utils'

export function OAuthConnectionCard({
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
}: {
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
}) {
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
    <div className="flex flex-col gap-3 overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] p-4">
      <div className="flex min-h-0 min-w-0 items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          {source?.iconPath ? <img src={source.iconPath} alt="" className="h-10 w-10 shrink-0 rounded-md object-contain" /> : null}
          <div className="min-w-0 space-y-1">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="truncate text-base font-semibold text-foreground">{providerLabel(connection.provider)}</span>
              {planType ? <Badge variant={planBadgeVariant(planType)} className="shrink-0">{planType}</Badge> : null}
              <StatusBadge status={effectiveStatus === 'connected' ? 'healthy' : effectiveStatus === 'error' ? 'error' : 'warning'}>
                {connectionStatusLabel(effectiveStatus, t)}
              </StatusBadge>
              {refreshWarning ? (
                <OAuthRefreshWarningBadge label={t('card.nonRefreshable')} message={refreshWarning} />
              ) : null}
            </div>
            <div className="truncate text-xs text-muted-soft">{connection.email ?? connection.account_id ?? '-'}</div>
          </div>
        </div>
        <div className="shrink-0">
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
      </div>

      <div className="grid grid-cols-3 gap-3">
        <CompactInfo label={t('card.site')} value={connection.site?.name ?? '-'} />
        <CompactInfo label={t('card.group')} value={groupLabel} />
        <CompactInfoButton
          label={t('card.usage')}
          value={formatUsage(connection.site?.usage)}
          disabled={!connection.site_id && !boundSite?.id}
          title={t('card.actions.usageSplit')}
          ariaLabel={t('card.actions.usageSplit')}
          onClick={onOpenUsageSplit}
        />
        <CompactInfo label={t('card.weight')} value={connection.site?.routing_priority != null ? connection.site.routing_priority.toFixed(1) : '-'} />
        <CompactInfoButton
          label={t('card.models')}
          value={detailQuery.isError ? t('card.modelsFailed') : detailQuery.isLoading ? t('card.modelsLoading') : t('card.modelsCount', { count: enabledModelCount })}
          disabled={!boundSite?.id}
          onClick={() => {
            if (boundSite?.id) setModelsSite(boundSite)
          }}
        />
        <CompactInfoButton
          label={t('card.updatedAt')}
          value={updatedAtLabel}
          title={updatedAtMode === 'relative' ? t('card.updatedAtMode.showAbsolute') : t('card.updatedAtMode.showRelative')}
          ariaLabel={updatedAtMode === 'relative' ? t('card.updatedAtMode.showAbsolute') : t('card.updatedAtMode.showRelative')}
          onClick={() => onUpdatedAtModeChange(nextUpdatedAtMode)}
        />
      </div>

      {effectiveStatus !== 'connected' && connectionError ? (
        <div className="py-1">
          <OAuthErrorPreview label={t('card.errorInfo')} message={connectionError} />
        </div>
      ) : (
        <QuotaPanel connectionId={connection.id} provider={connection.provider} quota={quota} loading={detailQuery.isLoading} resetCreditsData={detail?.reset_credits} />
      )}

      <div className="flex items-center justify-end gap-1 border-t border-[hsl(var(--glass-divider))] pt-2">
        {effectiveStatus === 'reconnect_required' ? (
          <Button
            className="size-9 shrink-0 p-0"
            size="icon"
            variant="ghost"
            disabled={refreshing || toggling || !boundSite?.id}
            onClick={onReconnect}
            title={t('card.actions.reconnect')}
          >
            <KeyRound className="h-4 w-4" />
          </Button>
        ) : null}
        <Button
          className="size-9 shrink-0 p-0"
          size="icon"
          variant="ghost"
          disabled={refreshing || toggling}
          onClick={onRefresh}
          title={t('card.actions.refresh')}
        >
          {refreshing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
        </Button>
        {connection.provider === 'codex' ? (
          <Button
            className="size-9 shrink-0 p-0"
            size="icon"
            variant="ghost"
            disabled={exporting || toggling}
            onClick={onExport}
            title={t('card.actions.export')}
          >
            {exporting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Download className="h-4 w-4" />}
          </Button>
        ) : null}
        <Button
          className="size-9 shrink-0 p-0"
          size="icon"
          variant="ghost"
          disabled={!boundSite || refreshing || toggling}
          onClick={() => { if (boundSite) onOpenTest(boundSite) }}
          title={t('card.actions.test')}
        >
          <FlaskConical className="h-4 w-4" />
        </Button>
        <Button
          className="size-9 shrink-0 p-0"
          size="icon"
          variant="ghost"
          disabled={!connection.site || toggling}
          onClick={() => setEditOpen(true)}
          title={t('card.actions.edit')}
        >
          <Pencil className="h-4 w-4" />
        </Button>
        <Button
          className="size-9 shrink-0 p-0 text-red-400 hover:text-red-300"
          size="icon"
          variant="ghost"
          disabled={deleting || toggling}
          onClick={onDelete}
          title={t('card.actions.delete')}
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
    </div>
  )
}

function CompactInfo({
  label,
  value,
  title,
}: {
  label: string
  value: string
  title?: string
}) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-soft">{label}</div>
      <div className="truncate text-sm font-medium text-foreground" title={title ?? value}>
        {value || '-'}
      </div>
    </div>
  )
}

function CompactInfoButton({
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
      className="min-w-0 text-left disabled:cursor-not-allowed disabled:opacity-60"
      disabled={disabled}
      title={title}
      aria-label={ariaLabel}
      onClick={onClick}
    >
      <div className="text-xs text-muted-soft">{label}</div>
      <div className="truncate text-sm font-medium text-foreground transition-colors hover:text-primary" title={value}>
        {value || '-'}
      </div>
    </button>
  )
}
