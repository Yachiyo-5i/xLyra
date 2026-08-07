import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { DataTable } from '@/components/common/data-table'
import { EmptyState } from '@/components/common/empty-state'
import { Switch } from '@/components/ui/switch'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { APIKeyActionsMenu } from '@/features/api-keys/components/api-key-actions-menu'
import { APIKeyQuotaCell } from '@/features/api-keys/components/api-key-quota-cell'
import { APIKeyCopyMenu } from '@/features/api-keys/components/api-key-copy-menu'
import {
  formatDateTime,
  formatModelPolicy,
  formatRateLimitSummary,
  formatRelativeDateTime,
  formatSitePolicy,
  hasResettableQuota,
  isAPIKeyActive,
} from '@/features/api-keys/lib/api-key-utils'
import type { TimeDisplayMode } from '@/features/api-keys/lib/types'

export function DownstreamAPIKeysTable({
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
}) {
  const { t, i18n } = useTranslation('api-keys')

  const columns = useMemo<ColumnDef<DownstreamAPIKey>[]>(
    () => [
      {
        id: 'key',
        header: t('table.headers.key'),
        cell: ({ row }) => (
          <div className="min-w-0 space-y-1">
            <div className="truncate font-medium text-foreground" title={row.original.name}>
              {row.original.name}
            </div>
            <APIKeyCopyMenu apiKey={row.original} />
          </div>
        ),
        meta: {
          className: 'w-[15%]',
          cellClassName: 'min-w-0',
        },
      },
      {
        id: 'models',
        header: t('table.headers.models'),
        cell: ({ row }) => (
          <div className="flex items-center justify-center gap-1 text-xs tabular-nums whitespace-nowrap">
            <button
              type="button"
              className="hover:text-foreground text-foreground cursor-pointer"
              onClick={() => onShowModels(row.original)}
            >
              {formatModelPolicy(row.original, t)}
            </button>
          </div>
        ),
        meta: {
          className: 'w-[12%]',
          align: 'center',
        },
      },
      {
        id: 'sites',
        header: t('table.headers.sites'),
        cell: ({ row }) => (
          <span className="text-muted-soft text-sm">{formatSitePolicy(row.original, t)}</span>
        ),
        meta: {
          className: 'w-[10%]',
          align: 'center',
        },
      },
      {
        id: 'quota',
        header: t('table.headers.quota'),
        cell: ({ row }) => <APIKeyQuotaCell apiKey={row.original} />,
        meta: {
          className: 'w-[18%]',
        },
      },
      {
        id: 'rate_limit',
        header: t('table.headers.rateLimit'),
        cell: ({ row }) => {
          const enabled = row.original.rate_limit?.status === 'enabled'
          return (
            <span className={enabled ? 'text-foreground text-sm tabular-nums' : 'text-muted-soft text-sm'}>
              {formatRateLimitSummary(row.original, t)}
            </span>
          )
        },
        meta: {
          className: 'w-[12%]',
          align: 'center',
        },
      },
      {
        id: 'last_used_at',
        header: () => <button type="button" className="hover:text-foreground transition-colors cursor-pointer" onClick={onToggleLastUsedMode}>{t('table.headers.lastUsed')}</button>,
        cell: ({ row }) => {
          const val = row.original.last_used_at
          if (!val) return <span className="text-muted-soft text-sm">-</span>
          const label = lastUsedMode === 'absolute' ? formatDateTime(val) : formatRelativeDateTime(val, now, i18n.language, t)
          return <button type="button" className="text-muted-soft hover:text-foreground cursor-pointer text-sm tabular-nums" onClick={onToggleLastUsedMode}>{label}</button>
        },
        meta: {
          className: 'w-[12%]',
          align: 'center',
        },
      },
      {
        id: 'expires_at',
        header: t('table.headers.expiresAt'),
        cell: ({ row }) => (
          <span className="text-muted-soft text-sm">
            {row.original.expires_at ? formatDateTime(row.original.expires_at) : t('table.permanent')}
          </span>
        ),
        meta: {
          className: 'w-[12%]',
          align: 'center',
        },
      },
      {
        id: 'enabled',
        header: t('table.headers.status'),
        cell: ({ row }) => {
          const active = isAPIKeyActive(row.original)
          const toggling = togglingKeyId === row.original.id
          return (
            <Switch
              checked={active}
              disabled={toggling}
              onCheckedChange={() => onToggleKey(row.original)}
            />
          )
        },
        meta: {
          className: 'w-[7%]',
          align: 'center',
        },
      },
      {
        id: 'actions',
        header: '',
        cell: ({ row }) => {
          const deleting = deletingKeyId === row.original.id

          return (
            <APIKeyActionsMenu
              busy={deleting}
              resetDisabled={!hasResettableQuota(row.original)}
              onEdit={() => onEditKey(row.original)}
              onReset={() => onResetQuota(row.original)}
              onDelete={() => onDeleteKey(row.original)}
            />
          )
        },
        meta: {
          className: 'w-[5%]',
        },
      },
    ],
    [t, i18n.language, deletingKeyId, lastUsedMode, now, onDeleteKey, onEditKey, onResetQuota, onShowModels, onToggleKey, onToggleLastUsedMode, togglingKeyId],
  )

  return (
    <DataTable
      columns={columns}
      data={apiKeys}
      getRowId={(row) => row.id}
      emptyState={<EmptyState title={t('table.empty.title')} description={t('table.empty.description')} />}
      hideHeaderWhenEmpty
    />
  )
}
