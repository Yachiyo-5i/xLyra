import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, RotateCcw } from 'lucide-react'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { EmptyState } from '@/components/common/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  buildModelGlyph,
  resolveThemedIconPath,
} from '@/components/common/brand-utils'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import { providerCatalog } from '@/lib/brands'
import { useResolvedTheme } from '@/hooks/theme-context'
import type {
  ModelPriceCredentialPricing,
  ModelPriceInput,
  ModelPriceItem,
} from '@/features/settings/api/model-prices'
import {
  DEFAULT_PRICE_DRAFT,
  createDraftFromPricing,
  isNewAPISite,
  parsePriceDraft,
  pricingSummary,
  statusBadgeVariant,
  statusLabel,
  perRequestPriceHeaderLabel,
  tokenPriceHeaderLabel,
  type ModelPriceGroup,
  type PriceDraft,
  type SiteModelGroup,
} from '@/features/settings/lib/model-price-utils'

type DraftTarget =
  | { type: 'group'; key: string; group: ModelPriceGroup; draft: PriceDraft }
  | { type: 'row'; key: string; row: ModelPriceItem; draft: PriceDraft }

type MobileModelsPriceListProps = {
  groups: ModelPriceGroup[]
  loading?: boolean
  pendingBulkGroupId?: string | null
  pendingSiteModelId?: string | null
  siteGroup?: SiteModelGroup | null
  onSaveStateChange?: (state: MobileModelsPriceSaveState) => void
  onBulkSave: (group: ModelPriceGroup, input: ModelPriceInput) => Promise<void>
  onRowSave: (row: ModelPriceItem, input: ModelPriceInput) => Promise<void>
  onReset?: (row: ModelPriceItem) => Promise<void>
  onResetSite?: (siteId: string) => Promise<void>
}

export type MobileModelsPriceListHandle = {
  saveAll: () => void
}

export type MobileModelsPriceSaveState = {
  dirtyCount: number
  saving: boolean
}

export const MobileModelsPriceList = forwardRef<
  MobileModelsPriceListHandle,
  MobileModelsPriceListProps
>(function MobileModelsPriceList(
  {
    groups,
    loading,
    pendingBulkGroupId,
    pendingSiteModelId,
    siteGroup,
    onSaveStateChange,
    onBulkSave,
    onRowSave,
    onReset,
    onResetSite,
  },
  ref,
) {
  const { t } = useTranslation('settings')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [collapsedSiteId, setCollapsedSiteId] = useState<string | null>(null)
  const [drafts, setDrafts] = useState<Record<string, PriceDraft>>({})
  const [saving, setSaving] = useState(false)
  const [resetTarget, setResetTarget] = useState<ModelPriceItem | null>(null)
  const [resetSiteTarget, setResetSiteTarget] = useState<string | null>(null)
  const [resetting, setResetting] = useState(false)
  const dirtyCount = Object.keys(drafts).length
  const siteGroupId = siteGroup?.siteId
  const siteExpanded = !siteGroupId || collapsedSiteId !== siteGroupId
  const visibleTargets = useMemo(
    () => collectVisibleTargets(groups, siteGroup, drafts),
    [drafts, groups, siteGroup],
  )

  function updateDraft(
    key: string,
    fallback: PriceDraft,
    patch: Partial<PriceDraft>,
  ) {
    setDrafts((previous) => ({
      ...previous,
      [key]: {
        ...fallback,
        ...previous[key],
        ...patch,
      },
    }))
  }

  const handleSaveAll = useCallback(async () => {
    if (dirtyCount === 0) return

    const dirtyTargets = visibleTargets.filter((target) => drafts[target.key])
    const parsedTargets = dirtyTargets.map((target) => ({
      target,
      parsed: parsePriceDraft(target.draft, t),
    }))
    const invalid = parsedTargets.find((item) => !item.parsed.ok)
    if (invalid?.parsed.ok === false) {
      toast.error(invalid.parsed.message)
      return
    }

    setSaving(true)
    try {
      for (const item of parsedTargets) {
        if (!item.parsed.ok) continue
        if (item.target.type === 'group') {
          await onBulkSave(item.target.group, item.parsed.input)
        }
      }
      for (const item of parsedTargets) {
        if (!item.parsed.ok) continue
        if (item.target.type === 'row') {
          await onRowSave(item.target.row, item.parsed.input)
        }
      }
      setDrafts({})
      toast.success(t('modelsPrice.savedSuccess'), {
        description: t('modelsPrice.savedDescription', {
          count: parsedTargets.length,
        }),
      })
    } finally {
      setSaving(false)
    }
  }, [dirtyCount, drafts, onBulkSave, onRowSave, t, visibleTargets])

  useImperativeHandle(
    ref,
    () => ({
      saveAll: handleSaveAll,
    }),
    [handleSaveAll],
  )

  useEffect(() => {
    onSaveStateChange?.({ dirtyCount, saving })
  }, [dirtyCount, onSaveStateChange, saving])

  if (loading && groups.length === 0) {
    return (
      <p className="px-1 py-8 text-sm text-muted-soft">
        {t('modelsPrice.loading')}
      </p>
    )
  }

  if (!loading && groups.length === 0) {
    return (
      <EmptyState
        title={t('modelsPrice.empty.title')}
        description={t('modelsPrice.empty.description')}
      />
    )
  }

  if (siteGroup) {
    return (
      <div className="space-y-3">
        <article className="overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] shadow-[var(--button-secondary-shadow)]">
          <button
            type="button"
            className="flex w-full items-start gap-3 p-3 text-left focus:outline-none focus-visible:outline-none focus-visible:ring-0"
            onClick={() =>
              setCollapsedSiteId(siteExpanded ? (siteGroupId ?? null) : null)
            }
            aria-label={
              siteExpanded
                ? t('modelsPrice.ariaLabels.collapse')
                : t('modelsPrice.ariaLabels.expand')
            }
          >
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-2">
                <h3
                  className="min-w-0 flex-1 truncate text-base font-semibold text-foreground"
                  title={siteGroup.siteName}
                >
                  {siteGroup.siteName}
                </h3>
                <ChevronDown
                  className={cn(
                    'h-4 w-4 shrink-0 text-muted-soft transition-transform',
                    siteExpanded && 'rotate-180',
                  )}
                />
              </div>
              <p className="mt-1 truncate text-xs text-muted-soft">
                {t('modelsPrice.siteView.summary', {
                  total: siteGroup.totalCount,
                  manual: siteGroup.manualCount,
                })}
              </p>
            </div>
          </button>

          <div className="mb-3 grid grid-cols-3 gap-2 px-3 text-xs">
            <Metric
              label={t('modelsPrice.subtitle.sites', { count: 1 })}
              value={t('modelsPrice.siteView.modelCount', {
                count: siteGroup.totalCount,
              })}
            />
            <Metric
              label={t('modelsPrice.statusLabels.synced')}
              value={String(siteGroup.totalCount - siteGroup.manualCount)}
            />
            <Metric
              label={t('modelsPrice.statusLabels.manual')}
              value={String(siteGroup.manualCount)}
              tone={siteGroup.manualCount ? 'warning' : undefined}
            />
          </div>

          {siteGroup.manualCount > 0 && onResetSite ? (
            <div className="flex justify-end px-3 pb-3">
              <Button
                variant="outline"
                size="sm"
                className="gap-1.5"
                onClick={() => setResetSiteTarget(siteGroup.siteId)}
              >
                <RotateCcw className="h-3.5 w-3.5" />
                {t('modelsPrice.resetAllButton')}
              </Button>
            </div>
          ) : null}

          {siteExpanded ? (
            <div className="border-t border-[hsl(var(--glass-divider))] bg-[hsl(var(--surface-subtle)/0.12)]">
              {siteGroup.rows.length === 0 ? (
                <div className="px-3 py-6">
                  <EmptyState
                    title={t('modelsPrice.empty.title')}
                    description={t('modelsPrice.empty.description')}
                  />
                </div>
              ) : (
                <div className="divide-y divide-[hsl(var(--glass-divider))] px-3">
                  {siteGroup.rows.map((row) => {
                    const rowKey = siteRowDraftKey(row)
                    const rowFallback = createDraftFromPricing(row)
                    const rowDraftValue = drafts[rowKey] ?? rowFallback
                    const rowPending =
                      saving || pendingSiteModelId === row.site_model_id

                    return (
                      <div key={rowKey} className="py-3">
                        <div className="flex items-start justify-between gap-3">
                          <div className="flex min-w-0 items-center gap-2">
                            <ProviderMark
                              provider={row.model.provider}
                              modelKey={row.displayKey}
                            />
                            <div className="min-w-0">
                              <p
                                className="truncate text-sm font-semibold text-foreground"
                                title={row.displayKey}
                              >
                                {row.displayKey}
                              </p>
                              <span
                                className="mt-0.5 block min-w-0 truncate text-xs text-muted-soft"
                                title={row.model.upstream_model_name}
                              >
                                {row.model.upstream_model_name}
                              </span>
                              {isNewAPISite(row.site.type) &&
                              row.pricing?.group_name ? (
                                <Badge
                                  variant="secondary"
                                  className="mt-1 max-w-[8rem] truncate rounded-full px-2 py-0 text-[11px]"
                                  title={row.pricing.group_name}
                                >
                                  {row.pricing.group_name}
                                </Badge>
                              ) : null}
                            </div>
                          </div>
                          <div className="flex shrink-0 flex-wrap justify-end gap-1.5">
                            <Badge
                              variant={statusBadgeVariant(row.pricing_status)}
                            >
                              {statusLabel(row.pricing_status, t)}
                            </Badge>
                            {row.pricing_status === 'manual' && onReset ? (
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-6 gap-1 px-1.5 text-xs"
                                disabled={rowPending}
                                title={t('modelsPrice.resetTooltip')}
                                onClick={() => setResetTarget(row)}
                              >
                                <RotateCcw className="h-3 w-3" />
                              </Button>
                            ) : null}
                            {!row.editable ? (
                              <Badge
                                variant="neutral"
                                title={row.edit_reason || undefined}
                              >
                                {t('modelsPrice.readOnly')}
                              </Badge>
                            ) : null}
                          </div>
                        </div>
                        <div className="mt-2">
                          <PriceEditor
                            draft={rowDraftValue}
                            disabled={!row.editable || rowPending}
                            pending={rowPending}
                            variant="compact"
                            isAudio={row.model.category === 'audio'}
                            disableGroupRatio={
                              row.site.type.trim().toLowerCase() === 'openai'
                            }
                            onChange={(patch) =>
                              updateDraft(rowKey, rowFallback, patch)
                            }
                          />
                        </div>
                        <MobileCredentialPricings
                          items={row.credential_pricings}
                        />
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          ) : null}
        </article>

        <ConfirmDialog
          open={resetTarget !== null}
          title={t('modelsPrice.resetDialogTitle')}
          description={t('modelsPrice.resetConfirm')}
          confirmLabel={t('modelsPrice.resetButton')}
          pending={resetting}
          destructive
          onCancel={() => setResetTarget(null)}
          onConfirm={async () => {
            if (!resetTarget || !onReset) return
            setResetting(true)
            try {
              await onReset(resetTarget)
            } finally {
              setResetting(false)
              setResetTarget(null)
            }
          }}
        />
        <ConfirmDialog
          open={resetSiteTarget !== null}
          title={t('modelsPrice.resetAllDialogTitle')}
          description={t('modelsPrice.resetAllConfirm', {
            count: siteGroup.manualCount,
          })}
          confirmLabel={t('modelsPrice.resetAllButton')}
          pending={resetting}
          destructive
          onCancel={() => setResetSiteTarget(null)}
          onConfirm={async () => {
            if (!resetSiteTarget || !onResetSite) return
            setResetting(true)
            try {
              await onResetSite(resetSiteTarget)
            } finally {
              setResetting(false)
              setResetSiteTarget(null)
            }
          }}
        />
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {groups.map((group) => {
        const expanded = expandedId === group.id
        const bulkDisabled =
          !group.canonicalModelId || group.editableSiteModelIds.length === 0
        const groupKey = groupDraftKey(group)
        const groupFallback = groupDraft(group)
        const groupDraftValue = drafts[groupKey] ?? groupFallback
        const groupPending = saving || pendingBulkGroupId === group.id

        return (
          <article
            key={group.id}
            className="overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] shadow-[var(--button-secondary-shadow)]"
          >
            <button
              type="button"
              className="flex w-full items-start gap-3 p-3 text-left focus:outline-none focus-visible:outline-none focus-visible:ring-0"
              onClick={() => setExpandedId(expanded ? null : group.id)}
              aria-label={
                expanded
                  ? t('modelsPrice.ariaLabels.collapse')
                  : t('modelsPrice.ariaLabels.expand')
              }
            >
              <ProviderMark
                provider={group.provider}
                modelKey={group.modelKey}
              />
              <div className="min-w-0 flex-1">
                <div className="flex min-w-0 items-center gap-2">
                  <h3
                    className="min-w-0 flex-1 truncate text-base font-semibold text-foreground"
                    title={group.modelKey}
                  >
                    {group.modelKey}
                  </h3>
                  <ChevronDown
                    className={cn(
                      'h-4 w-4 shrink-0 text-muted-soft transition-transform',
                      expanded && 'rotate-180',
                    )}
                  />
                </div>
                <p
                  className="mt-1 truncate text-xs text-muted-soft"
                  title={pricingSummary(group, t)}
                >
                  {pricingSummary(group, t)}
                </p>
              </div>
            </button>

            <div className="mb-3 grid grid-cols-3 gap-2 px-3 text-xs">
              <Metric
                label={t('modelsPrice.subtitle.sites', {
                  count: group.uniqueSiteCount,
                })}
                value={t('modelsPrice.subtitle.editable', {
                  count: group.editableCount,
                })}
              />
              <Metric
                label={t('modelsPrice.statusLabels.missing')}
                value={String(group.missingCount)}
                tone={group.missingCount ? 'warning' : undefined}
              />
              <Metric
                label={t('modelsPrice.statusLabels.manual')}
                value={String(group.manualCount)}
              />
            </div>

            {expanded ? (
              <div className="border-t border-[hsl(var(--glass-divider))] bg-[hsl(var(--surface-subtle)/0.12)]">
                <div className="p-3">
                  <div className="rounded-lg bg-[hsl(var(--card)/0.36)] p-3">
                    <div className="mb-2 flex items-center justify-between gap-3">
                      <p className="text-sm font-semibold text-foreground">
                        {t('modelsPrice.batchEdit')}
                      </p>
                      <span className="shrink-0 text-xs text-muted-soft">
                        {t('modelsPrice.subtitle.editable', {
                          count: group.editableCount,
                        })}
                      </span>
                    </div>
                    <PriceEditor
                      draft={groupDraftValue}
                      disabled={bulkDisabled || groupPending}
                      pending={groupPending}
                      variant="compact"
                      isAudio={group.category === 'audio'}
                      disableGroupRatio={group.rows.some(
                        (row) =>
                          row.site.type.trim().toLowerCase() === 'openai',
                      )}
                      onChange={(patch) =>
                        updateDraft(groupKey, groupFallback, patch)
                      }
                    />
                    {bulkDisabled ? (
                      <p className="mt-2 text-xs text-muted-soft">
                        {!group.canonicalModelId
                          ? t('modelsPrice.badges.noBatchTooltip')
                          : t('modelsPrice.badges.noEditableTooltip')}
                      </p>
                    ) : null}
                  </div>
                </div>

                <div className="px-3 pb-3">
                  <div className="divide-y divide-[hsl(var(--glass-divider))] rounded-lg bg-[hsl(var(--card)/0.18)] px-3">
                    {group.rows.map((row) => {
                      const rowKey = rowDraftKey(row)
                      const rowFallback = createDraftFromPricing(row)
                      const rowDraftValue = drafts[rowKey] ?? rowFallback
                      const rowPending =
                        saving || pendingSiteModelId === row.site_model_id

                      return (
                        <div key={rowKey} className="py-3">
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <p
                                className="truncate text-sm font-semibold text-foreground"
                                title={row.site.name}
                              >
                                {row.site.name}
                              </p>
                              <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1.5 text-xs text-muted-soft">
                                <span
                                  className="min-w-0 truncate"
                                  title={row.model.upstream_model_name}
                                >
                                  {row.site.type} ·{' '}
                                  {row.model.upstream_model_name}
                                </span>
                                {isNewAPISite(row.site.type) &&
                                row.pricing?.group_name ? (
                                  <Badge
                                    variant="secondary"
                                    className="max-w-[8rem] truncate rounded-full px-2 py-0 text-[11px]"
                                    title={row.pricing.group_name}
                                  >
                                    {row.pricing.group_name}
                                  </Badge>
                                ) : null}
                              </div>
                            </div>
                            <div className="flex shrink-0 flex-wrap justify-end gap-1.5">
                              <Badge
                                variant={statusBadgeVariant(row.pricing_status)}
                              >
                                {statusLabel(row.pricing_status, t)}
                              </Badge>
                              {!row.editable ? (
                                <Badge
                                  variant="neutral"
                                  title={row.edit_reason || undefined}
                                >
                                  {t('modelsPrice.readOnly')}
                                </Badge>
                              ) : null}
                            </div>
                          </div>
                          <div className="mt-2">
                            <PriceEditor
                              draft={rowDraftValue}
                              disabled={!row.editable || rowPending}
                              pending={rowPending}
                              variant="compact"
                              isAudio={row.model.category === 'audio'}
                              disableGroupRatio={
                                row.site.type.trim().toLowerCase() === 'openai'
                              }
                              onChange={(patch) =>
                                updateDraft(rowKey, rowFallback, patch)
                              }
                            />
                          </div>
                          <MobileCredentialPricings
                            items={row.credential_pricings}
                          />
                        </div>
                      )
                    })}
                  </div>
                </div>
              </div>
            ) : null}
          </article>
        )
      })}
    </div>
  )
})

function Metric({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone?: 'warning'
}) {
  return (
    <div className="min-w-0 px-2 py-2">
      <span className="block truncate text-[11px] text-muted-soft">
        {label}
      </span>
      <span
        className={cn(
          'mt-1 block truncate font-semibold tabular-nums text-foreground',
          tone === 'warning' && 'text-amber-600',
        )}
      >
        {value}
      </span>
    </div>
  )
}

function MobileCredentialPricings({
  items,
}: {
  items?: ModelPriceCredentialPricing[]
}) {
  const { t } = useTranslation('settings')
  if (!items?.length) return null
  return (
    <div className="mt-3 space-y-2 border-t border-[hsl(var(--glass-divider))] pt-3">
      {items.map((item) => (
        <div
          key={item.credential_id}
          className="rounded-md bg-[hsl(var(--surface-subtle)/0.58)] px-3 py-2.5"
        >
          <div className="flex items-center justify-between gap-3">
            <div className="min-w-0">
              <div
                className="truncate text-xs font-medium text-foreground"
                title={item.credential_name}
              >
                {item.credential_name}
              </div>
              <div className="mt-0.5 text-[11px] text-muted-soft">
                x{item.group_ratio}
              </div>
            </div>
            <Badge
              variant={
                item.credential_usable && item.model_enabled
                  ? 'success'
                  : 'warning'
              }
            >
              {item.credential_usable && item.model_enabled
                ? t('common:status.enabled')
                : t('common:status.disabled')}
            </Badge>
          </div>
          <div className="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-[11px] text-muted-soft">
            <MobileCredentialValue
              label={t('modelsPrice.headers.inputPrice')}
              value={item.input_value ?? item.per_request_value}
            />
            <MobileCredentialValue
              label={t('modelsPrice.headers.outputPrice')}
              value={item.output_value ?? item.audio_output_value}
            />
          </div>
        </div>
      ))}
    </div>
  )
}

function MobileCredentialValue({
  label,
  value,
}: {
  label: string
  value?: number | null
}) {
  return (
    <div className="flex min-w-0 justify-between gap-2">
      <span className="truncate">{label}</span>
      <span className="shrink-0 tabular-nums text-foreground">
        {typeof value === 'number'
          ? value.toLocaleString(undefined, { maximumFractionDigits: 6 })
          : '-'}
      </span>
    </div>
  )
}

function PriceEditor({
  draft,
  disabled,
  pending,
  variant = 'default',
  isAudio,
  disableGroupRatio,
  onChange,
}: {
  draft: PriceDraft
  disabled?: boolean
  pending?: boolean
  variant?: 'default' | 'compact'
  isAudio?: boolean
  disableGroupRatio?: boolean
  onChange: (patch: Partial<PriceDraft>) => void
}) {
  const { t } = useTranslation('settings')
  const isTokens = draft.billingType === 'tokens'
  const controlDisabled = disabled || pending
  const compact = variant === 'compact'

  return (
    <div className="space-y-2">
      {compact ? (
        <div className="grid grid-cols-3 gap-2">
          <span className="min-w-0">
            <span className="mb-1 block truncate text-[11px] text-muted-soft">
              {t('modelsPrice.headers.billing')}
            </span>
            <Select
              value={draft.billingType}
              onValueChange={(value) =>
                onChange({ billingType: value as PriceDraft['billingType'] })
              }
              disabled={controlDisabled}
            >
              <SelectTrigger className="h-9 w-full px-3">
                <SelectValue />
              </SelectTrigger>
              <SelectContent searchable={false}>
                <SelectItem value="tokens">
                  {t('modelsPrice.billingLabels.tokens')}
                </SelectItem>
                <SelectItem value="per_request">
                  {t('modelsPrice.billingLabels.perRequest')}
                </SelectItem>
              </SelectContent>
            </Select>
          </span>
        </div>
      ) : (
        <Select
          value={draft.billingType}
          onValueChange={(value) =>
            onChange({ billingType: value as PriceDraft['billingType'] })
          }
          disabled={controlDisabled}
        >
          <SelectTrigger className="h-10 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent searchable={false}>
            <SelectItem value="tokens">
              {t('modelsPrice.billingLabels.tokens')}
            </SelectItem>
            <SelectItem value="per_request">
              {t('modelsPrice.billingLabels.perRequest')}
            </SelectItem>
          </SelectContent>
        </Select>
      )}

      {isTokens ? (
        <div className="grid grid-cols-3 gap-2">
          <PriceInput
            label={tokenPriceHeaderLabel(t('modelsPrice.headers.inputPrice'))}
            value={draft.inputValue}
            disabled={controlDisabled}
            compact={compact}
            onChange={(value) => onChange({ inputValue: value })}
          />
          <PriceInput
            label={tokenPriceHeaderLabel(t('modelsPrice.headers.outputPrice'))}
            value={draft.outputValue}
            disabled={controlDisabled}
            compact={compact}
            onChange={(value) => onChange({ outputValue: value })}
          />
          {isAudio ? (
            <PriceInput
              label={tokenPriceHeaderLabel(
                t('modelsPrice.headers.audioOutputPrice'),
              )}
              value={draft.audioOutputValue}
              disabled={controlDisabled}
              compact={compact}
              onChange={(value) => onChange({ audioOutputValue: value })}
            />
          ) : null}
          <PriceInput
            label={tokenPriceHeaderLabel(t('modelsPrice.headers.cachePrice'))}
            value={draft.cacheInputValue}
            disabled={controlDisabled}
            compact={compact}
            onChange={(value) => onChange({ cacheInputValue: value })}
          />
          <PriceInput
            label={tokenPriceHeaderLabel(
              t('modelsPrice.headers.cacheWritePrice'),
            )}
            value={draft.createCacheInputValue}
            disabled={controlDisabled}
            compact={compact}
            onChange={(value) => onChange({ createCacheInputValue: value })}
          />
          <PriceInput
            label={tokenPriceHeaderLabel(
              t('modelsPrice.headers.cacheWrite1hPrice'),
            )}
            value={draft.createCache1hInputValue}
            disabled={controlDisabled}
            compact={compact}
            onChange={(value) => onChange({ createCache1hInputValue: value })}
          />
        </div>
      ) : (
        <PriceInput
          label={perRequestPriceHeaderLabel(
            t('modelsPrice.headers.perRequestPrice'),
            t,
          )}
          value={draft.perRequestValue}
          disabled={controlDisabled}
          compact={compact}
          onChange={(value) => onChange({ perRequestValue: value })}
        />
      )}

      <PriceInput
        label={t('modelsPrice.headers.groupRatio')}
        value={draft.groupRatio}
        disabled={controlDisabled || disableGroupRatio}
        compact={compact}
        onChange={(value) => onChange({ groupRatio: value })}
      />
    </div>
  )
}

function PriceInput({
  label,
  value,
  disabled,
  compact,
  onChange,
}: {
  label: string
  value: string
  disabled?: boolean
  compact?: boolean
  onChange: (value: string) => void
}) {
  return (
    <label className="min-w-0">
      <span className="mb-1 block truncate text-[11px] text-muted-soft">
        {label}
      </span>
      <Input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        inputMode="decimal"
        placeholder="$"
        className={cn('px-2 text-sm', compact ? 'h-9' : 'h-10')}
      />
    </label>
  )
}

function ProviderMark({
  provider,
  modelKey,
}: {
  provider?: string | null
  modelKey: string
}) {
  const entry = getProviderEntry(provider)
  const resolvedMode = useResolvedTheme()
  const label = entry?.name ?? provider ?? modelKey

  return (
    <span
      className={cn(
        'mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-md',
        !entry
          ? 'bg-[hsl(var(--primary)/0.18)] text-primary'
          : 'bg-[hsl(var(--surface-subtle))]',
      )}
    >
      {entry?.iconPath ? (
        <img
          src={resolveThemedIconPath(entry.iconPath, label, resolvedMode)}
          alt={label}
          className="size-6 object-contain"
        />
      ) : (
        <span
          className={cn(
            !entry ? 'text-primary' : 'text-foreground',
            'text-xs font-semibold',
          )}
        >
          {buildModelGlyph(modelKey)}
        </span>
      )}
    </span>
  )
}

function collectVisibleTargets(
  groups: ModelPriceGroup[],
  siteGroup: SiteModelGroup | null | undefined,
  drafts: Record<string, PriceDraft>,
): DraftTarget[] {
  if (siteGroup) {
    return siteGroup.rows.map((row) => {
      const key = siteRowDraftKey(row)
      return {
        type: 'row',
        key,
        row,
        draft: drafts[key] ?? createDraftFromPricing(row),
      }
    })
  }

  return groups.flatMap((group) => {
    const groupKey = groupDraftKey(group)
    const targets: DraftTarget[] = [
      {
        type: 'group',
        key: groupKey,
        group,
        draft: drafts[groupKey] ?? groupDraft(group),
      },
    ]

    for (const row of group.rows) {
      const key = rowDraftKey(row)
      targets.push({
        type: 'row',
        key,
        row,
        draft: drafts[key] ?? createDraftFromPricing(row),
      })
    }

    return targets
  })
}

function groupDraft(group: ModelPriceGroup): PriceDraft {
  if (group.missingCount > 0) return DEFAULT_PRICE_DRAFT
  const pricedRow = group.rows.find((row) => row.pricing)
  return pricedRow ? createDraftFromPricing(pricedRow) : DEFAULT_PRICE_DRAFT
}

function groupDraftKey(group: ModelPriceGroup) {
  return `group:${group.id}`
}

function rowDraftKey(row: ModelPriceItem) {
  return `row:${row.site_model_id}:${row.pricing?.group_name ?? 'default'}:${row.pricing_id ?? 'missing'}`
}

function siteRowDraftKey(row: ModelPriceItem) {
  return `site-row:${row.site_model_id}:${row.pricing?.group_name ?? 'default'}:${row.pricing_id ?? 'missing'}`
}

function getProviderEntry(provider?: string | null) {
  if (!provider) return undefined
  const normalized = provider.trim().toLowerCase()
  return providerCatalog.find(
    (item) => item.id === normalized || item.name.toLowerCase() === normalized,
  )
}
