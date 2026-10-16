import type { ReactNode } from 'react'
import { LoaderCircle, PencilLine, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
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
    <Card className="flex flex-col gap-4 p-4">
      {/* 与 OAuth 卡片同一结构：名称 + 密钥 + 开关 → 指标两列 → 底部操作 */}
      <div className="flex min-w-0 items-start gap-3">
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-headline text-label">{apiKey.name}</h3>
          <APIKeyCopyMenu apiKey={apiKey} triggerClassName="mt-1" />
        </div>
        <Switch
          className="shrink-0"
          checked={active}
          disabled={isToggling || expired}
          title={expired ? t('table.expiredToggleHint') : undefined}
          onCheckedChange={() => onToggleKey(apiKey)}
          aria-label={t('table.headers.status')}
        />
      </div>

      <div className="grid grid-cols-2 items-start gap-x-4 gap-y-3">
        <MobileMetric
          label={t('table.headers.models')}
          value={formatModelPolicy(apiKey, t)}
          onClick={() => onShowModels(apiKey)}
        />
        <MobileMetric label={t('table.headers.sites')} value={formatSitePolicy(apiKey, t)} />
        <div className="min-w-0">
          <span className="block text-caption-1 text-label-3">{t('table.headers.quota')}</span>
          <div className="mt-0.5"><APIKeyQuotaCell apiKey={apiKey} truncateValues={false} compactValues /></div>
        </div>
        <MobileMetric label={t('table.headers.rateLimit')} value={formatRateLimitSummary(apiKey, t)} />
        <MobileMetric label={t('table.headers.lastUsed')} value={lastUsedLabel} onClick={onToggleLastUsedMode} />
        <MobileMetric
          label={t('table.headers.expiresAt')}
          value={apiKey.expires_at ? formatDateTime(apiKey.expires_at) : t('table.permanent')}
          valueClassName={expired ? 'text-danger' : undefined}
          valueTitle={expired ? t('table.expired') : undefined}
        />
      </div>

      <div className="flex items-center justify-end gap-0.5 border-t border-separator pt-3">
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
      </div>
    </Card>
  )
}

/** 卡片里的一项指标：小标签 + 值；可点击时值悬停变为强调色 */
function MobileMetric({
  label,
  value,
  valueClassName,
  valueTitle,
  onClick,
}: {
  label: string
  value: string
  valueClassName?: string
  valueTitle?: string
  onClick?: () => void
}) {
  const body = (
    <>
      <span className="block truncate text-caption-1 text-label-3">{label}</span>
      <span
        className={cn('mt-0.5 block truncate text-footnote font-medium text-label tabular-nums transition-colors group-hover/metric:text-tint', valueClassName)}
        title={valueTitle ?? value}
      >
        {value}
      </span>
    </>
  )
  if (!onClick) return <div className="min-w-0">{body}</div>
  return (
    <button type="button" className="group/metric min-w-0 text-left" onClick={onClick}>
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
