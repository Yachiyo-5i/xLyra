import { LoaderCircle, PencilLine, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { CardAction, CardFooter, CardHeading, CardMetric, CardMetrics, ItemCard } from '@/components/ui/card'
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
  hasResettableQuota,
  isAPIKeyActive,
  isAPIKeyExpired,
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
  onRotateKey,
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
  onRotateKey: (apiKey: DownstreamAPIKey) => void
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
          onRotateKey={onRotateKey}
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
  onRotateKey,
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
  onRotateKey: (apiKey: DownstreamAPIKey) => void
  onResetQuota: (apiKey: DownstreamAPIKey) => void
  onShowModels: (apiKey: DownstreamAPIKey) => void
  onToggleLastUsedMode: () => void
}) {
  const { t, i18n } = useTranslation('api-keys')
  const active = isAPIKeyActive(apiKey)
  const expired = isAPIKeyExpired(apiKey)
  const lastUsedLabel = apiKey.last_used_at
    ? lastUsedMode === 'absolute'
      ? formatDateTime(apiKey.last_used_at)
      : formatRelativeDateTime(apiKey.last_used_at, now, i18n.language, t)
    : '-'

  return (
    <ItemCard>
      <CardHeading
        title={apiKey.name}
        meta={<APIKeyCopyMenu apiKey={apiKey} triggerClassName="-mt-0.5" />}
        trailing={(
          <Switch
            checked={active}
            disabled={isToggling || expired}
            title={expired ? t('table.expiredToggleHint') : undefined}
            onCheckedChange={() => onToggleKey(apiKey)}
            aria-label={t('table.headers.status')}
          />
        )}
      />

      <CardMetrics>
        <CardMetric
          label={t('table.headers.models')}
          value={formatModelPolicy(apiKey, t)}
          onClick={() => onShowModels(apiKey)}
        />
        <CardMetric label={t('table.headers.sites')} value={formatSitePolicy(apiKey, t)} />
        <div className="min-w-0">
          <span className="block truncate text-caption-1 text-label-3">{t('table.headers.quota')}</span>
          <div className="mt-0.5"><APIKeyQuotaCell apiKey={apiKey} truncateValues={false} compactValues /></div>
        </div>
        <CardMetric label={t('table.headers.rateLimit')} value={formatRateLimitSummary(apiKey, t)} />
        <CardMetric label={t('table.headers.lastUsed')} value={lastUsedLabel} onClick={onToggleLastUsedMode} />
        <CardMetric
          label={t('table.headers.expiresAt')}
          value={apiKey.expires_at ? formatDateTime(apiKey.expires_at) : t('table.permanent')}
          valueClassName={expired ? 'text-danger' : undefined}
          title={expired ? t('table.expired') : undefined}
        />
      </CardMetrics>

      <CardFooter>
        <CardAction label={t('table.edit')} disabled={isDeleting} onClick={() => onEditKey(apiKey)}>
          <PencilLine />
        </CardAction>
        <CardAction
          label={apiKey.key_kind === 'custom' ? t('table.rotateCustomDisabled') : t('table.rotate')}
          disabled={isDeleting || apiKey.key_kind === 'custom'}
          onClick={() => onRotateKey(apiKey)}
        >
          <RefreshCw />
        </CardAction>
        <CardAction
          label={t('table.resetQuota')}
          disabled={isDeleting || !hasResettableQuota(apiKey)}
          onClick={() => onResetQuota(apiKey)}
        >
          <RotateCcw />
        </CardAction>
        <CardAction
          label={t('table.delete')}
          className="text-danger hover:text-danger"
          disabled={isDeleting}
          onClick={() => onDeleteKey(apiKey)}
        >
          {isDeleting ? <LoaderCircle className="animate-spin" /> : <Trash2 />}
        </CardAction>
      </CardFooter>
    </ItemCard>
  )
}
