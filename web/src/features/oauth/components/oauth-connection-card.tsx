import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, FlaskConical, KeyRound, LoaderCircle, Pencil, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { CardAction, CardFooter, CardHeading, CardMetric, CardMetrics, ItemCard } from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'
import {
  getOAuthConnection,
  oauthQueryKeys,
  type OAuthConnectionListItem,
} from '@/features/oauth/api/oauth'
import {
  connectionStatusLabel,
  formatOAuthUpdatedAt,
  formatUsageParts,
  getOAuthSources,
  oauthConnectionEffectiveStatus,
  oauthConnectionEnableBlockedByStatus,
  oauthConnectionErrorMessage,
  oauthEnabledModelCount,
  oauthRefreshWarning,
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
  compact = false,
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
  /** 移动端：指标两列、内边距更紧凑 */
  compact?: boolean
}) {
  const { t, i18n } = useTranslation('oauth')
  const queryClient = useQueryClient()
  const detailQuery = useQuery({
    queryKey: oauthQueryKeys.detail(item.id),
    queryFn: () => getOAuthConnection(item.id),
    staleTime: 30_000,
    refetchOnWindowFocus: 'always',
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

  const usage = formatUsageParts(connection.site?.usage)
  const actionsBusy = refreshing || toggling

  return (
    <ItemCard className={compact ? undefined : 'p-5'}>
      <CardHeading
        leading={source?.iconPath ? <img src={source.iconPath} alt="" className="size-9 shrink-0 rounded-inner object-contain" /> : null}
        title={providerLabel(connection.provider)}
        tags={(
          <>
            {planType ? <Badge variant="neutral" className="shrink-0">{planType}</Badge> : null}
            <StatusBadge
              status={effectiveStatus === 'connected' ? 'healthy' : effectiveStatus === 'error' ? 'error' : 'warning'}
              className="shrink-0"
            >
              {connectionStatusLabel(effectiveStatus, t)}
            </StatusBadge>
            {refreshWarning ? (
              <OAuthRefreshWarningBadge label={t('card.nonRefreshable')} message={refreshWarning} />
            ) : null}
          </>
        )}
        meta={<span title={connection.email ?? connection.account_id ?? undefined}>{connection.email ?? connection.account_id ?? '-'}</span>}
        trailing={routeEnabled !== null && boundSite?.id ? (
          <Switch
            checked={routeEnabled}
            disabled={toggling || enableBlocked}
            onCheckedChange={(checked) => {
              if (checked && enableBlocked) return
              onToggleEnabled(checked)
            }}
          />
        ) : null}
      />

      {/* 指标在上（桌面 3×2，移动端 2×3），额度在下 */}
      <CardMetrics columns={compact ? 2 : 3}>
        <CardMetric label={t('card.site')} value={connection.site?.name ?? '-'} />
        <CardMetric label={t('card.group')} value={groupLabel} />
        <CardMetric
          label={t('card.usage')}
          value={usage.main}
          secondary={usage.secondary}
          disabled={!connection.site_id && !boundSite?.id}
          title={t('card.actions.usageSplit')}
          onClick={onOpenUsageSplit}
        />
        <CardMetric label={t('card.weight')} value={connection.site?.routing_priority != null ? connection.site.routing_priority.toFixed(1) : '-'} />
        <CardMetric
          label={t('card.models')}
          value={detailQuery.isError ? t('card.modelsFailed') : detailQuery.isLoading ? t('card.modelsLoading') : t('card.modelsCount', { count: enabledModelCount })}
          disabled={!boundSite?.id}
          onClick={() => {
            if (boundSite?.id) setModelsSite(boundSite)
          }}
        />
        <CardMetric
          label={t('card.updatedAt')}
          value={updatedAtLabel}
          title={updatedAtMode === 'relative' ? t('card.updatedAtMode.showAbsolute') : t('card.updatedAtMode.showRelative')}
          onClick={() => onUpdatedAtModeChange(nextUpdatedAtMode)}
        />
      </CardMetrics>

      {effectiveStatus !== 'connected' && connectionError ? (
        <OAuthErrorPreview label={t('card.errorInfo')} message={connectionError} />
      ) : (
        <QuotaPanel
          connectionId={connection.id}
          provider={connection.provider}
          quota={quota}
          estimate={detail?.quota_estimate}
          loading={detailQuery.isLoading}
          resetCreditsData={detail?.reset_credits}
        />
      )}

      <CardFooter>
        {effectiveStatus === 'reconnect_required' ? (
          <CardAction
            label={t('card.actions.reconnect')}
            className="text-warning hover:text-warning"
            disabled={actionsBusy || !boundSite?.id}
            onClick={onReconnect}
          >
            <KeyRound />
          </CardAction>
        ) : null}
        <CardAction label={t('card.actions.refresh')} disabled={actionsBusy} onClick={onRefresh}>
          {refreshing ? <LoaderCircle className="animate-spin" /> : <RotateCcw />}
        </CardAction>
        {connection.provider === 'codex' ? (
          <CardAction label={t('card.actions.export')} disabled={exporting || toggling} onClick={onExport}>
            {exporting ? <LoaderCircle className="animate-spin" /> : <Download />}
          </CardAction>
        ) : null}
        <CardAction
          label={t('card.actions.test')}
          disabled={!boundSite || actionsBusy}
          onClick={() => { if (boundSite) onOpenTest(boundSite) }}
        >
          <FlaskConical />
        </CardAction>
        <CardAction label={t('card.actions.edit')} disabled={!connection.site || toggling} onClick={() => setEditOpen(true)}>
          <Pencil />
        </CardAction>
        <CardAction
          label={t('card.actions.delete')}
          className="text-danger hover:text-danger"
          disabled={deleting || toggling}
          onClick={onDelete}
        >
          {deleting ? <LoaderCircle className="animate-spin" /> : <Trash2 />}
        </CardAction>
      </CardFooter>

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
    </ItemCard>
  )
}
