import { useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, FlaskConical, KeyRound, LoaderCircle, Pencil, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
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
import { cn } from '@/lib/utils'

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
    <Card className={cn('flex min-w-0 flex-col', compact ? 'gap-4 p-4' : 'gap-4 p-5')}>
      {/* 标题行：图标 + 名称/套餐 + 启用开关；下一行是状态与账号 */}
      <div className="flex min-w-0 items-start gap-3">
        {source?.iconPath ? <img src={source.iconPath} alt="" className="size-9 shrink-0 rounded-inner object-contain" /> : null}
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h3 className="min-w-0 truncate text-headline text-label">{providerLabel(connection.provider)}</h3>
            {planType ? <Badge variant="neutral" className="shrink-0">{planType}</Badge> : null}
          </div>
          <div className="mt-1.5 flex min-w-0 items-center gap-2">
            <StatusBadge
              status={effectiveStatus === 'connected' ? 'healthy' : effectiveStatus === 'error' ? 'error' : 'warning'}
              className="shrink-0"
            >
              {connectionStatusLabel(effectiveStatus, t)}
            </StatusBadge>
            {refreshWarning ? (
              <OAuthRefreshWarningBadge label={t('card.nonRefreshable')} message={refreshWarning} />
            ) : null}
            <span className="min-w-0 truncate text-footnote text-label-3" title={connection.email ?? connection.account_id ?? undefined}>
              {connection.email ?? connection.account_id ?? '-'}
            </span>
          </div>
        </div>
        {routeEnabled !== null && boundSite?.id ? (
          <Switch
            className="shrink-0"
            checked={routeEnabled}
            disabled={toggling || enableBlocked}
            onCheckedChange={(checked) => {
              if (checked && enableBlocked) return
              onToggleEnabled(checked)
            }}
          />
        ) : null}
      </div>

      {/* 指标在上（桌面 3×2，移动端 2×3），额度在下 */}
      <div className={cn('grid items-start gap-x-4 gap-y-3', compact ? 'grid-cols-2' : 'grid-cols-3')}>
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
      </div>

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

      <div className="mt-auto flex items-center justify-end gap-0.5 border-t border-separator pt-3">
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
    </Card>
  )
}

/** 卡片里的一项指标：小标签 + 值，可选第二行次值；可点击的指标点值会变成强调色 */
function CardMetric({
  label,
  value,
  secondary,
  title,
  disabled,
  onClick,
}: {
  label: string
  value: string
  secondary?: string
  title?: string
  disabled?: boolean
  onClick?: () => void
}) {
  const body = (
    <>
      <span className="block truncate text-caption-1 text-label-3">{label}</span>
      <span className="mt-0.5 block truncate text-footnote font-medium text-label tabular-nums transition-colors group-hover/metric:text-tint group-disabled/metric:text-label" title={value}>
        {value || '-'}
      </span>
      {secondary ? <span className="block truncate text-caption-1 text-label-3 tabular-nums">{secondary}</span> : null}
    </>
  )
  if (!onClick) return <div className="min-w-0">{body}</div>
  return (
    <button
      type="button"
      className="group/metric min-w-0 text-left disabled:cursor-not-allowed disabled:opacity-60"
      disabled={disabled}
      title={title}
      aria-label={title ? `${label} ${value}: ${title}` : undefined}
      onClick={onClick}
    >
      {body}
    </button>
  )
}

function CardAction({
  label,
  className,
  disabled,
  onClick,
  children,
}: {
  label: string
  className?: string
  disabled?: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      className={cn('text-label-2 hover:text-label', className)}
      disabled={disabled}
      onClick={onClick}
      title={label}
      aria-label={label}
    >
      {children}
    </Button>
  )
}
