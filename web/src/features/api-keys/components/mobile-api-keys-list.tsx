import { LoaderCircle, PencilLine, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { APIKeyCopyMenu } from '@/features/api-keys/components/api-key-copy-menu'
import { APIKeyQuotaCell } from '@/features/api-keys/components/api-key-quota-cell'
import {
  formatDateTime,
  formatModelPolicy,
  formatRateLimitSummary,
  formatRelativeDateTime,
  formatSitePolicy,
  isAPIKeyActive,
} from '@/features/api-keys/lib/api-key-utils'
import type { TimeDisplayMode } from '@/features/api-keys/lib/types'

export function MobileAPIKeysList({
  apiKeys,
  lastUsedMode,
  now,
  togglingKeyId,
  deletingKeyId,
  onToggleKey,
  onEditKey,
  onDeleteKey,
  onResetQuota,
  onShowModels,
  onToggleLastUsedMode,
  className,
}: {
  apiKeys: DownstreamAPIKey[]
  lastUsedMode: TimeDisplayMode
  now: number
  togglingKeyId?: string
  deletingKeyId?: string
  onToggleKey: (apiKey: DownstreamAPIKey) => void
  onEditKey: (apiKey: DownstreamAPIKey) => void
  onDeleteKey: (apiKey: DownstreamAPIKey) => void
  onResetQuota: (apiKey: DownstreamAPIKey) => void
  onShowModels: (apiKey: DownstreamAPIKey) => void
  onToggleLastUsedMode: () => void
  className?: string
}) {
  return (
    <div className={cn('space-y-3', className)}>
      {apiKeys.map((apiKey) => (
        <MobileAPIKeyCard
          key={apiKey.id}
          apiKey={apiKey}
          lastUsedMode={lastUsedMode}
          now={now}
          isToggling={togglingKeyId === apiKey.id}
          isDeleting={deletingKeyId === apiKey.id}
          onToggleKey={onToggleKey}
          onEditKey={onEditKey}
          onDeleteKey={onDeleteKey}
          onResetQuota={onResetQuota}
          onShowModels={onShowModels}
          onToggleLastUsedMode={onToggleLastUsedMode}
        />
      ))}
    </div>
  )
}

function MobileAPIKeyCard({
  apiKey,
  lastUsedMode,
  now,
  isToggling,
  isDeleting,
  onToggleKey,
  onEditKey,
  onDeleteKey,
  onResetQuota,
  onShowModels,
  onToggleLastUsedMode,
}: {
  apiKey: DownstreamAPIKey
  lastUsedMode: TimeDisplayMode
  now: number
  isToggling: boolean
  isDeleting: boolean
  onToggleKey: (apiKey: DownstreamAPIKey) => void
  onEditKey: (apiKey: DownstreamAPIKey) => void
  onDeleteKey: (apiKey: DownstreamAPIKey) => void
  onResetQuota: (apiKey: DownstreamAPIKey) => void
  onShowModels: (apiKey: DownstreamAPIKey) => void
  onToggleLastUsedMode: () => void
}) {
  const { t, i18n } = useTranslation('api-keys')
  const active = isAPIKeyActive(apiKey)
  const lastUsedLabel = apiKey.last_used_at
    ? lastUsedMode === 'absolute'
      ? formatDateTime(apiKey.last_used_at)
      : formatRelativeDateTime(apiKey.last_used_at, now, i18n.language, t)
    : '-'

  return (
    <article className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] p-3 shadow-[var(--button-secondary-shadow)]">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h3 className="min-w-0 truncate text-base font-semibold text-foreground">{apiKey.name}</h3>
            <Badge
              variant={active ? 'success' : 'neutral'}
              className="shrink-0 px-1.5 py-0 text-[11px]"
            >
              {active ? t('workspace.status.active') : t('workspace.status.disabled')}
            </Badge>
          </div>
          <APIKeyCopyMenu apiKey={apiKey} triggerClassName="mt-1.5" />
        </div>
        <Switch
          checked={active}
          disabled={isToggling}
          onCheckedChange={() => onToggleKey(apiKey)}
          aria-label={t('table.headers.status')}
        />
      </div>

      <div className="mt-3 grid grid-cols-2 gap-2 text-xs">
        <MobileMetricButton
          label={t('table.headers.models')}
          value={formatModelPolicy(apiKey, t)}
          onClick={() => onShowModels(apiKey)}
        />
        <MobileMetric label={t('table.headers.sites')} value={formatSitePolicy(apiKey, t)} />
        <MobileQuotaMetric apiKey={apiKey} />
        <MobileMetric label={t('table.headers.rateLimit')} value={formatRateLimitSummary(apiKey, t)} />
        <button
          type="button"
          className="min-w-0 self-start px-2 py-2 text-left transition-colors hover:text-primary"
          onClick={onToggleLastUsedMode}
        >
          <span className="block text-[11px] text-muted-soft">{t('table.headers.lastUsed')}</span>
          <span className="mt-1 block truncate font-medium text-foreground tabular-nums">{lastUsedLabel}</span>
        </button>
        <MobileMetric
          label={t('table.headers.expiresAt')}
          value={apiKey.expires_at ? formatDateTime(apiKey.expires_at) : t('table.permanent')}
        />
      </div>

      <div className="mt-3 flex items-center justify-end gap-1 border-t border-[hsl(var(--glass-divider))] pt-2">
        <Button
          size="icon"
          variant="ghost"
          className="h-8 w-8 text-foreground/60 hover:text-foreground"
          disabled={isDeleting}
          onClick={() => onEditKey(apiKey)}
          aria-label={t('table.edit')}
          title={t('table.edit')}
        >
          <PencilLine className="h-4 w-4" />
        </Button>
        <Button
          size="icon"
          variant="ghost"
          className="h-8 w-8 text-foreground/60 hover:text-foreground"
          disabled={isDeleting || !hasResettableQuota(apiKey)}
          onClick={() => onResetQuota(apiKey)}
          aria-label={t('table.resetQuota')}
          title={t('table.resetQuota')}
        >
          <RotateCcw className="h-4 w-4" />
        </Button>
        <Button
          size="icon"
          variant="ghost"
          className="h-8 w-8 text-destructive hover:text-destructive"
          disabled={isDeleting}
          onClick={() => onDeleteKey(apiKey)}
          aria-label={t('table.delete')}
          title={t('table.delete')}
        >
          {isDeleting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
        </Button>
      </div>
    </article>
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

function MobileMetricButton({ label, value, onClick }: { label: string; value: string; onClick: () => void }) {
  return (
    <button
      type="button"
      className="min-w-0 self-start px-2 py-2 text-left transition-colors hover:text-primary"
      onClick={onClick}
    >
      <span className="block text-[11px] text-muted-soft">{label}</span>
      <span className="mt-1 block truncate font-medium text-foreground tabular-nums" title={value}>{value}</span>
    </button>
  )
}

function MobileQuotaMetric({ apiKey }: { apiKey: DownstreamAPIKey }) {
  const { t } = useTranslation('api-keys')

  return (
    <div className="min-w-0 px-2 py-2">
      <span className="block text-[11px] text-muted-soft">{t('table.headers.quota')}</span>
      <div className="mt-1"><APIKeyQuotaCell apiKey={apiKey} truncateValues={false} compactValues /></div>
    </div>
  )
}

function hasResettableQuota(apiKey: DownstreamAPIKey) {
  return (!apiKey.quota_daily_unlimited && apiKey.quota_daily_limit != null)
    || (!apiKey.quota_weekly_unlimited && apiKey.quota_weekly_limit != null)
}
