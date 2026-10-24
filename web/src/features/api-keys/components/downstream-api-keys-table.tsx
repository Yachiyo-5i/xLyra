import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { DataTable } from '@/components/common/data-table'
import { defaultTableColumnWidths } from '@/lib/table-column-widths'
import { EmptyState } from '@/components/common/empty-state'
import { Switch } from '@/components/ui/switch'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { APIKeyActionsMenu } from '@/features/api-keys/components/api-key-actions-menu'
import { APIKeyQuotaCell } from '@/features/api-keys/components/api-key-quota-cell'
import { APIKeyCopyMenu } from '@/features/api-keys/components/api-key-copy-menu'
import {
  enabledSiteGroupIds,
  enabledSiteIds,
  formatDateTime,
  formatModelPolicy,
  formatRateLimitSummary,
  formatRelativeDateTime,
  hasResettableQuota,
  isAPIKeyActive,
  isAPIKeyExpired,
} from '@/features/api-keys/lib/api-key-utils'
import type { TimeDisplayMode } from '@/features/api-keys/lib/types'

const API_KEYS_COLUMN_SIZING = {
  storageKey: 'xlyra:api-keys:table-column-widths:v4',
  // 名称与额度给足宽度（额度可能有 总/周/日 三行），短值列收窄
  defaultWidths: defaultTableColumnWidths([17, 12, 8, 23, 10, 10, 10, 6, 5]),
  minimumWidths: [8, 6, 4, 10, 5, 5, 5, 4, 3],
}

export function DownstreamAPIKeysTable({
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
  const { t, i18n } = useTranslation('api-keys')

  const columns = useMemo<ColumnDef<DownstreamAPIKey>[]>(
    () => [
      {
        id: 'key',
        header: t('table.headers.key'),
        cell: ({ row }) => (
          <div className="min-w-0 space-y-1">
            <div className="truncate font-medium text-label" title={row.original.name}>
              {row.original.name}
            </div>
            <APIKeyCopyMenu apiKey={row.original} />
          </div>
        ),
        meta: {
          cellClassName: 'min-w-0',
        },
      },
      {
        id: 'scope',
        header: t('table.headers.scope'),
        cell: ({ row }) => {
          const apiKey = row.original
          const siteCount = enabledSiteIds(apiKey).length
          const groupCount = enabledSiteGroupIds(apiKey).length
          const showSiteCount = apiKey.site_policy === 'allow_list' && siteCount > 0
          const showGroupCount = apiKey.site_policy === 'allow_list' && groupCount > 0
          return (
            <button
              type="button"
              className="inline-flex cursor-pointer flex-col items-start gap-0.5 text-left text-label-2 transition-colors hover:text-label"
              onClick={() => onShowModels(row.original)}
            >
              <span className="text-footnote tabular-nums">
                {formatModelPolicy(row.original, t)}
              </span>
              {showSiteCount && (
                <span className="text-footnote tabular-nums">
                  {t('table.policy.selectedSites', { count: siteCount })}
                </span>
              )}
              {showGroupCount && (
                <span className="text-footnote tabular-nums">
                  {t('table.policy.selectedGroups', { count: groupCount })}
                </span>
              )}
            </button>
          )
        },
      },
      {
        id: 'billing_multiplier',
        header: t('table.headers.billingMultiplier'),
        cell: ({ row }) => {
          const val = row.original.billing_multiplier
          if (!val || val === 1) return <span className="text-label-2 text-body">×1</span>
          return <span className="text-body tabular-nums text-label">×{val}</span>
        },
      },
      {
        id: 'quota',
        header: t('table.headers.quota'),
        cell: ({ row }) => <APIKeyQuotaCell apiKey={row.original} />,
      },
      {
        id: 'rate_limit',
        header: t('table.headers.rateLimit'),
        cell: ({ row }) => {
          const enabled = row.original.rate_limit?.status === 'enabled'
          return (
            <span className={enabled ? 'text-label text-body tabular-nums' : 'text-label-2 text-body'}>
              {formatRateLimitSummary(row.original, t)}
            </span>
          )
        },
      },
      {
        id: 'last_used_at',
        header: () => <button type="button" className="hover:text-label transition-colors cursor-pointer" onClick={onToggleLastUsedMode}>{t('table.headers.lastUsed')}</button>,
        cell: ({ row }) => {
          const val = row.original.last_used_at
          if (!val) return <span className="text-label-2 text-body">-</span>
          const label = lastUsedMode === 'absolute' ? formatDateTime(val) : formatRelativeDateTime(val, now, i18n.language, t)
          return <button type="button" className="text-label-2 hover:text-label cursor-pointer text-body tabular-nums" onClick={onToggleLastUsedMode}>{label}</button>
        },
        meta: {
          resizeLabel: t('table.headers.lastUsed'),
        },
      },
      {
        id: 'expires_at',
        header: t('table.headers.expiresAt'),
        cell: ({ row }) => {
          const expired = isAPIKeyExpired(row.original)
          return (
            <span
              className={expired ? 'text-body text-danger' : 'text-label-2 text-body'}
              title={expired ? t('table.expired') : undefined}
            >
              {row.original.expires_at ? formatDateTime(row.original.expires_at) : t('table.permanent')}
            </span>
          )
        },
      },
      {
        id: 'enabled',
        header: t('table.headers.status'),
        cell: ({ row }) => {
          const active = isAPIKeyActive(row.original)
          const expired = isAPIKeyExpired(row.original)
          const toggling = togglingKeyId === row.original.id
          return (
            <Switch
              checked={active}
              disabled={toggling || expired}
              title={expired ? t('table.expiredToggleHint') : undefined}
              onCheckedChange={() => onToggleKey(row.original)}
            />
          )
        },
        meta: {
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
              rotateDisabled={row.original.key_kind === 'custom'}
              onEdit={() => onEditKey(row.original)}
              onRotate={() => onRotateKey(row.original)}
              onReset={() => onResetQuota(row.original)}
              onDelete={() => onDeleteKey(row.original)}
            />
          )
        },
        meta: {
          className: 'table-sticky-end',
          cellClassName: 'px-2',
          headerClassName: 'px-2',
        },
      },
    ],
    [t, i18n.language, deletingKeyId, lastUsedMode, now, onDeleteKey, onEditKey, onResetQuota, onRotateKey, onShowModels, onToggleKey, onToggleLastUsedMode, togglingKeyId],
  )

  return (
    <DataTable
      columnSizing={API_KEYS_COLUMN_SIZING}
      stickyHeader="container"
      columns={columns}
      data={apiKeys}
      className={className}
      getRowId={(row) => row.id}
      emptyState={<EmptyState title={t('table.empty.title')} description={t('table.empty.description')} />}
      hideHeaderWhenEmpty
    />
  )
}
