import { useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronRight, LoaderCircle, RotateCcw, Save } from 'lucide-react'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { EmptyState } from '@/components/common/empty-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  buildModelGlyph,
  resolveThemedIconPath,
} from '@/components/common/brand-utils'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
  parsePriceDraft,
  pricingSummary,
  statusBadgeVariant,
  statusLabel,
  isNewAPISite,
  perRequestPriceHeaderLabel,
  tokenPriceHeaderLabel,
  type ModelPriceGroup,
  type PriceDraft,
  type SiteModelGroup,
} from '@/features/settings/lib/model-price-utils'

type DraftTarget =
  | { type: 'group'; key: string; group: ModelPriceGroup; draft: PriceDraft }
  | { type: 'row'; key: string; row: ModelPriceItem; draft: PriceDraft }

type ModelsPriceTableProps = {
  groups: ModelPriceGroup[]
  loading?: boolean
  pendingBulkGroupId?: string | null
  pendingSiteModelId?: string | null
  toolbarStart?: ReactNode
  toolbarControls?: ReactNode
  onBulkSave: (group: ModelPriceGroup, input: ModelPriceInput) => Promise<void>
  onRowSave: (row: ModelPriceItem, input: ModelPriceInput) => Promise<void>
  onReset?: (row: ModelPriceItem) => Promise<void>
  siteGroup?: SiteModelGroup | null
  onResetSite?: (siteId: string) => Promise<void>
}

const tableColumns =
  '36px minmax(180px,1.5fr) 120px 130px minmax(60px,0.35fr) minmax(60px,0.35fr) minmax(60px,0.35fr) minmax(60px,0.35fr) minmax(60px,0.35fr) minmax(60px,0.35fr) minmax(60px,0.35fr) minmax(60px,0.35fr) minmax(100px,0.6fr)'

export function ModelsPriceTable({
  groups,
  loading,
  pendingBulkGroupId,
  pendingSiteModelId,
  toolbarStart,
  toolbarControls,
  onBulkSave,
  onRowSave,
  onReset,
  siteGroup,
  onResetSite,
}: ModelsPriceTableProps) {
  const { t } = useTranslation('settings')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [drafts, setDrafts] = useState<Record<string, PriceDraft>>({})
  const [saving, setSaving] = useState(false)
  const [resetTarget, setResetTarget] = useState<ModelPriceItem | null>(null)
  const [resetting, setResetting] = useState(false)
  const [resetSiteTarget, setResetSiteTarget] = useState<string | null>(null)
  const dirtyCount = Object.keys(drafts).length

  const visibleTargets = useMemo(
    () => collectVisibleTargets(groups, siteGroup, drafts),
    [drafts, groups, siteGroup],
  )

  function updateDraft(
    key: string,
    fallback: PriceDraft,
    patch: Partial<PriceDraft>,
  ) {
    setDrafts((prev) => ({
      ...prev,
      [key]: {
        ...fallback,
        ...prev[key],
        ...patch,
      },
    }))
  }

  async function handleSaveAll() {
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
  }

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col gap-3 overflow-hidden">
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 shrink-0">{toolbarStart}</div>
        <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-3">
          {toolbarControls}
          <Button
            className="shrink-0"
            onClick={handleSaveAll}
            disabled={dirtyCount === 0 || saving}
          >
            {saving ? (
              <LoaderCircle className="h-4 w-4 animate-spin" />
            ) : (
              <Save className="h-4 w-4" />
            )}
            {t('modelsPrice.saveChanges')}
            {dirtyCount ? ` ${dirtyCount}` : ''}
          </Button>
        </div>
      </div>

      {siteGroup ? (
        <SiteView
          key={siteGroup.siteId}
          siteGroup={siteGroup}
          drafts={drafts}
          saving={saving}
          pendingSiteModelId={pendingSiteModelId}
          onReset={onReset}
          onResetSite={() => setResetSiteTarget(siteGroup.siteId)}
          updateDraft={updateDraft}
          t={t}
        />
      ) : groups.length === 0 ? (
        loading ? (
          <div className="text-muted-soft flex min-h-56 items-center justify-center text-sm">
            {t('modelsPrice.loading')}
          </div>
        ) : (
          <EmptyState
            title={t('modelsPrice.empty.title')}
            description={t('modelsPrice.empty.description')}
          />
        )
      ) : (
        <>
          <div
            className="grid shrink-0 border-y border-[hsl(var(--divider))] bg-[hsl(var(--surface-base))] px-3 py-3 text-xs font-medium text-muted-soft"
            style={{ gridTemplateColumns: tableColumns }}
          >
            <div />
            <div>{t('modelsPrice.headers.model')}</div>
            <div>{t('modelsPrice.headers.group')}</div>
            <div>{t('modelsPrice.headers.billing')}</div>
            <div>
              {tokenPriceHeaderLabel(t('modelsPrice.headers.inputPrice'))}
            </div>
            <div>
              {tokenPriceHeaderLabel(t('modelsPrice.headers.outputPrice'))}
            </div>
            <div>
              {tokenPriceHeaderLabel(t('modelsPrice.headers.audioOutputPrice'))}
            </div>
            <div>
              {tokenPriceHeaderLabel(t('modelsPrice.headers.cachePrice'))}
            </div>
            <div>
              {tokenPriceHeaderLabel(t('modelsPrice.headers.cacheWritePrice'))}
            </div>
            <div>
              {tokenPriceHeaderLabel(
                t('modelsPrice.headers.cacheWrite1hPrice'),
              )}
            </div>
            <div>
              {perRequestPriceHeaderLabel(
                t('modelsPrice.headers.perRequestPrice'),
                t,
              )}
            </div>
            <div>{t('modelsPrice.headers.groupRatio')}</div>
            <div>{t('modelsPrice.headers.status')}</div>
          </div>

          <div className="scrollbar-hidden min-h-0 flex-1 overflow-auto">
            <div className="divide-y divide-[hsl(var(--divider))]">
              {groups.map((group) => {
                const expanded = expandedId === group.id
                const bulkDisabled =
                  !group.canonicalModelId ||
                  group.editableSiteModelIds.length === 0
                const groupKey = groupDraftKey(group)
                const groupFallback = groupDraft(group)
                const groupDraftValue = drafts[groupKey] ?? groupFallback
                const groupPending = saving || pendingBulkGroupId === group.id

                return (
                  <div key={group.id}>
                    <div
                      className="grid cursor-pointer items-center gap-0 px-3 py-3 transition-colors hover:bg-[hsl(var(--surface-subtle)/0.35)]"
                      style={{ gridTemplateColumns: tableColumns }}
                      onClick={() => setExpandedId(expanded ? null : group.id)}
                    >
                      <button
                        type="button"
                        className="flex size-8 items-center justify-center rounded-md hover:bg-[hsl(var(--surface-subtle))]"
                        onClick={(event) => {
                          event.stopPropagation()
                          setExpandedId(expanded ? null : group.id)
                        }}
                        aria-label={
                          expanded
                            ? t('modelsPrice.ariaLabels.collapse')
                            : t('modelsPrice.ariaLabels.expand')
                        }
                      >
                        <ChevronRight
                          className={cn(
                            'h-4 w-4 text-muted-soft transition-transform',
                            expanded && 'rotate-90',
                          )}
                        />
                      </button>

                      <div className="flex min-w-0 items-center gap-3 pr-3">
                        <ProviderMark
                          provider={group.provider}
                          modelKey={group.modelKey}
                          size="lg"
                        />
                        <div className="min-w-0">
                          <div
                            className="truncate text-sm font-semibold text-foreground"
                            title={group.modelKey}
                          >
                            {group.modelKey}
                          </div>
                          <div className="text-muted-soft mt-1 flex flex-wrap items-center gap-2 text-xs">
                            <span>
                              {t('modelsPrice.subtitle.sites', {
                                count: group.uniqueSiteCount,
                              })}
                            </span>
                            <span>
                              {t('modelsPrice.subtitle.editable', {
                                count: group.editableCount,
                              })}
                            </span>
                            <span title={pricingSummary(group, t)}>
                              {pricingSummary(group, t)}
                            </span>
                          </div>
                        </div>
                      </div>

                      <GroupCell group={group} />

                      <PriceDraftCells
                        draft={groupDraftValue}
                        disabled={bulkDisabled || groupPending}
                        pending={groupPending}
                        isAudio={group.category === 'audio'}
                        disableGroupRatio={group.rows.some(
                          (row) =>
                            row.site.type.trim().toLowerCase() === 'openai',
                        )}
                        onChange={(patch) =>
                          updateDraft(groupKey, groupFallback, patch)
                        }
                      />

                      <div className="flex flex-wrap items-center gap-1.5">
                        {group.missingCount ? (
                          <Badge variant="warning">
                            {t('modelsPrice.badges.missing', {
                              count: group.missingCount,
                            })}
                          </Badge>
                        ) : null}
                        {group.manualCount ? (
                          <Badge variant="secondary">
                            {t('modelsPrice.badges.manual', {
                              count: group.manualCount,
                            })}
                          </Badge>
                        ) : null}
                        {group.syncedCount ? (
                          <Badge variant="success">
                            {t('modelsPrice.badges.synced', {
                              count: group.syncedCount,
                            })}
                          </Badge>
                        ) : null}
                        {bulkDisabled ? (
                          <Badge
                            variant="neutral"
                            title={
                              !group.canonicalModelId
                                ? t('modelsPrice.badges.noBatchTooltip')
                                : t('modelsPrice.badges.noEditableTooltip')
                            }
                          >
                            {t('modelsPrice.badges.noBatch')}
                          </Badge>
                        ) : null}
                      </div>
                    </div>

                    {expanded ? (
                      <div className="divide-y divide-[hsl(var(--divider))] bg-[hsl(var(--surface-subtle)/0.18)]">
                        {group.rows.map((row) => {
                          const rowKey = rowDraftKey(row)
                          const rowFallback = createDraftFromPricing(row)
                          const rowDraftValue = drafts[rowKey] ?? rowFallback
                          const rowPending =
                            saving || pendingSiteModelId === row.site_model_id
                          return (
                            <div key={rowKey}>
                              <div
                                className="grid items-center px-3 py-3"
                                style={{ gridTemplateColumns: tableColumns }}
                              >
                                <div />
                                <div className="min-w-0 pr-3">
                                  <div
                                    className="truncate text-sm font-medium text-foreground"
                                    title={row.site.name}
                                  >
                                    {row.site.name}
                                  </div>
                                  <div
                                    className="text-muted-soft mt-1 truncate text-xs"
                                    title={row.model.upstream_model_name}
                                  >
                                    {row.site.type} ·{' '}
                                    {row.model.upstream_model_name}
                                  </div>
                                </div>

                                <GroupCell row={row} />

                                <PriceDraftCells
                                  draft={rowDraftValue}
                                  disabled={!row.editable || rowPending}
                                  pending={rowPending}
                                  isAudio={row.model.category === 'audio'}
                                  disableGroupRatio={
                                    row.site.type.trim().toLowerCase() ===
                                    'openai'
                                  }
                                  onChange={(patch) =>
                                    updateDraft(rowKey, rowFallback, patch)
                                  }
                                />

                                <div className="flex flex-wrap items-center gap-2">
                                  <Badge
                                    variant={statusBadgeVariant(
                                      row.pricing_status,
                                    )}
                                  >
                                    {statusLabel(row.pricing_status, t)}
                                  </Badge>
                                  {row.pricing_status === 'manual' &&
                                  onReset ? (
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      className="h-6 gap-1 px-1.5 text-xs"
                                      disabled={rowPending}
                                      title={t('modelsPrice.resetTooltip')}
                                      onClick={() => setResetTarget(row)}
                                    >
                                      <RotateCcw className="h-3 w-3" />
                                      {t('modelsPrice.resetButton')}
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
                              <CredentialPricingRows row={row} />
                            </div>
                          )
                        })}
                      </div>
                    ) : null}
                  </div>
                )
              })}
            </div>
          </div>
        </>
      )}

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
          count: siteGroup?.manualCount ?? 0,
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

type SiteViewProps = {
  siteGroup: SiteModelGroup
  drafts: Record<string, PriceDraft>
  saving: boolean
  pendingSiteModelId?: string | null
  onReset?: (row: ModelPriceItem) => Promise<void>
  onResetSite: () => void
  updateDraft: (
    key: string,
    fallback: PriceDraft,
    patch: Partial<PriceDraft>,
  ) => void
  t: (key: string, options?: Record<string, unknown>) => string
}

function SiteView({
  siteGroup,
  drafts,
  saving,
  pendingSiteModelId,
  onReset,
  onResetSite,
  updateDraft,
  t,
}: SiteViewProps) {
  const [expanded, setExpanded] = useState(true)

  return (
    <>
      <div
        className="grid shrink-0 border-y border-[hsl(var(--divider))] bg-[hsl(var(--surface-base))] px-3 py-3 text-xs font-medium text-muted-soft"
        style={{ gridTemplateColumns: tableColumns }}
      >
        <div />
        <div>{t('modelsPrice.headers.model')}</div>
        <div>{t('modelsPrice.headers.group')}</div>
        <div>{t('modelsPrice.headers.billing')}</div>
        <div>{tokenPriceHeaderLabel(t('modelsPrice.headers.inputPrice'))}</div>
        <div>{tokenPriceHeaderLabel(t('modelsPrice.headers.outputPrice'))}</div>
        <div>
          {tokenPriceHeaderLabel(t('modelsPrice.headers.audioOutputPrice'))}
        </div>
        <div>{tokenPriceHeaderLabel(t('modelsPrice.headers.cachePrice'))}</div>
        <div>
          {tokenPriceHeaderLabel(t('modelsPrice.headers.cacheWritePrice'))}
        </div>
        <div>
          {tokenPriceHeaderLabel(t('modelsPrice.headers.cacheWrite1hPrice'))}
        </div>
        <div>
          {perRequestPriceHeaderLabel(
            t('modelsPrice.headers.perRequestPrice'),
            t,
          )}
        </div>
        <div>{t('modelsPrice.headers.groupRatio')}</div>
        <div>{t('modelsPrice.headers.status')}</div>
      </div>

      <div className="scrollbar-hidden min-h-0 flex-1 overflow-auto">
        <div className="divide-y divide-[hsl(var(--divider))]">
          <div>
            <div
              className="flex cursor-pointer items-center gap-3 px-3 py-3 transition-colors hover:bg-[hsl(var(--surface-subtle)/0.35)]"
              onClick={() => setExpanded(!expanded)}
            >
              <button
                type="button"
                className="flex size-8 shrink-0 items-center justify-center rounded-md hover:bg-[hsl(var(--surface-subtle))]"
                onClick={(event) => {
                  event.stopPropagation()
                  setExpanded(!expanded)
                }}
                aria-label={
                  expanded
                    ? t('modelsPrice.ariaLabels.collapse')
                    : t('modelsPrice.ariaLabels.expand')
                }
              >
                <ChevronRight
                  className={cn(
                    'h-4 w-4 text-muted-soft transition-transform',
                    expanded && 'rotate-90',
                  )}
                />
              </button>

              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-semibold text-foreground">
                  {siteGroup.siteName}
                </div>
                <div className="text-muted-soft mt-1 flex flex-wrap items-center gap-2 text-xs">
                  <span>
                    {t('modelsPrice.siteView.summary', {
                      total: siteGroup.totalCount,
                      manual: siteGroup.manualCount,
                    })}
                  </span>
                </div>
              </div>

              <div className="flex shrink-0 items-center gap-2">
                <Badge variant="success">
                  {t('modelsPrice.badges.synced', {
                    count: siteGroup.totalCount - siteGroup.manualCount,
                  })}
                </Badge>
                {siteGroup.manualCount > 0 ? (
                  <Badge variant="secondary">
                    {t('modelsPrice.badges.manual', {
                      count: siteGroup.manualCount,
                    })}
                  </Badge>
                ) : null}
                {siteGroup.manualCount > 0 ? (
                  <Button
                    variant="outline"
                    size="sm"
                    className="gap-1.5"
                    onClick={(event) => {
                      event.stopPropagation()
                      onResetSite()
                    }}
                  >
                    <RotateCcw className="h-3.5 w-3.5" />
                    {t('modelsPrice.resetAllButton')}
                  </Button>
                ) : null}
              </div>
            </div>

            {expanded ? (
              <div className="divide-y divide-[hsl(var(--divider))] bg-[hsl(var(--surface-subtle)/0.18)]">
                {siteGroup.rows.length === 0 ? (
                  <div className="px-3 py-10">
                    <EmptyState
                      title={t('modelsPrice.empty.title')}
                      description={t('modelsPrice.empty.description')}
                    />
                  </div>
                ) : (
                  siteGroup.rows.map((row) => {
                    const rowKey = siteRowDraftKey(row)
                    const rowFallback = createDraftFromPricing(row)
                    const rowDraftValue = drafts[rowKey] ?? rowFallback
                    const rowPending =
                      saving || pendingSiteModelId === row.site_model_id
                    return (
                      <div key={rowKey}>
                        <div
                          className="grid items-center px-3 py-3"
                          style={{ gridTemplateColumns: tableColumns }}
                        >
                          <div />
                          <div className="flex min-w-0 items-center gap-3 pr-3">
                            <ProviderMark
                              provider={row.model.provider}
                              modelKey={row.displayKey}
                              size="sm"
                            />
                            <div className="min-w-0">
                              <div
                                className="truncate text-sm font-medium text-foreground"
                                title={row.displayKey}
                              >
                                {row.displayKey}
                              </div>
                              <div
                                className="text-muted-soft mt-0.5 truncate text-xs"
                                title={row.model.upstream_model_name}
                              >
                                {row.model.upstream_model_name}
                              </div>
                            </div>
                          </div>

                          <GroupCell row={row} />

                          <PriceDraftCells
                            draft={rowDraftValue}
                            disabled={!row.editable || rowPending}
                            pending={rowPending}
                            isAudio={row.model.category === 'audio'}
                            disableGroupRatio={
                              row.site.type.trim().toLowerCase() === 'openai'
                            }
                            onChange={(patch) =>
                              updateDraft(rowKey, rowFallback, patch)
                            }
                          />

                          <div className="flex flex-wrap items-center gap-2">
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
                                onClick={() => onReset(row)}
                              >
                                <RotateCcw className="h-3 w-3" />
                              </Button>
                            ) : null}
                          </div>
                        </div>
                        <CredentialPricingRows row={row} />
                      </div>
                    )
                  })
                )}
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </>
  )
}

function CredentialPricingRows({ row }: { row: ModelPriceItem }) {
  const { t } = useTranslation('settings')
  if (!row.credential_pricings?.length) return null
  return (
    <div className="border-t border-[hsl(var(--glass-divider))] bg-[hsl(var(--surface-base)/0.45)]">
      {row.credential_pricings.map((credential) => (
        <div
          key={`${row.site_model_id}:${credential.credential_id}`}
          className="grid items-center px-3 py-2.5 text-xs"
          style={{ gridTemplateColumns: tableColumns }}
        >
          <div />
          <div className="min-w-0 pr-3 pl-3">
            <div
              className="truncate font-medium text-foreground"
              title={credential.credential_name}
            >
              {credential.credential_name}
            </div>
          </div>
          <div>
            <Badge variant="neutral">API Key</Badge>
          </div>
          <CredentialPricingCells credential={credential} />
          <div>
            <Badge
              variant={
                credential.credential_usable && credential.model_enabled
                  ? 'success'
                  : 'warning'
              }
            >
              {credential.credential_usable && credential.model_enabled
                ? t('common:status.enabled')
                : t('common:status.disabled')}
            </Badge>
          </div>
        </div>
      ))}
    </div>
  )
}

function CredentialPricingCells({
  credential,
}: {
  credential: ModelPriceCredentialPricing
}) {
  return (
    <>
      <div className="pr-2 text-muted-soft">
        {credential.billing_type || '-'}
      </div>
      <CredentialPriceValue value={credential.input_value} />
      <CredentialPriceValue value={credential.output_value} />
      <CredentialPriceValue value={credential.audio_output_value} />
      <CredentialPriceValue value={credential.cache_input_value} />
      <CredentialPriceValue value={credential.create_cache_input_value} />
      <CredentialPriceValue value={credential.create_cache_1h_input_value} />
      <CredentialPriceValue value={credential.per_request_value} />
      <div className="pr-2 font-medium tabular-nums text-foreground">
        x{credential.group_ratio}
      </div>
    </>
  )
}

function CredentialPriceValue({ value }: { value?: number | null }) {
  return (
    <div className="pr-2 tabular-nums text-muted-soft">
      {typeof value === 'number'
        ? value.toLocaleString(undefined, { maximumFractionDigits: 6 })
        : '-'}
    </div>
  )
}

type GroupCellProps = {
  group?: ModelPriceGroup
  row?: ModelPriceItem
}

function ProviderMark({
  provider,
  modelKey,
  size = 'sm',
}: {
  provider?: string | null
  modelKey: string
  size?: 'sm' | 'lg'
}) {
  const entry = getProviderEntry(provider)
  const resolvedMode = useResolvedTheme()
  const label = entry?.name ?? provider ?? modelKey
  const boxClassName = size === 'lg' ? 'size-9 rounded-md' : 'size-6 rounded-md'
  const imageClassName = size === 'lg' ? 'size-6' : 'size-4'
  const textClassName = size === 'lg' ? 'text-xs font-semibold' : 'text-[11px]'

  return (
    <span
      className={cn(
        'flex shrink-0 items-center justify-center',
        !entry
          ? '[background-color:hsl(var(--primary)/0.18)] text-primary'
          : 'bg-[hsl(var(--surface-subtle))]',
        boxClassName,
      )}
    >
      {entry?.iconPath ? (
        <img
          src={resolveThemedIconPath(entry.iconPath, label, resolvedMode)}
          alt={label}
          className={cn('object-contain', imageClassName)}
        />
      ) : (
        <span
          className={cn(
            !entry ? 'text-primary' : 'text-foreground',
            textClassName,
          )}
        >
          {buildModelGlyph(modelKey)}
        </span>
      )}
    </span>
  )
}

function GroupCell({ group, row }: GroupCellProps) {
  if (row) {
    const groupName = isNewAPISite(row.site.type)
      ? row.pricing?.group_name?.trim()
      : ''
    return (
      <div className="pr-2">
        {groupName ? (
          <Badge
            variant="secondary"
            className="max-w-full truncate"
            title={groupName}
          >
            {groupName}
          </Badge>
        ) : (
          <span className="text-muted-soft text-xs">-</span>
        )}
      </div>
    )
  }

  const groupNames = [
    ...new Set(
      (group?.rows ?? [])
        .filter((item) => isNewAPISite(item.site.type))
        .map((item) => item.pricing?.group_name?.trim())
        .filter((value): value is string => Boolean(value)),
    ),
  ]

  return (
    <div className="flex min-w-0 flex-wrap gap-1 pr-2">
      {groupNames.length ? (
        <Badge variant="secondary" title={groupNames.join('、')}>
          {groupNames.length}
        </Badge>
      ) : (
        <span className="text-muted-soft text-xs">-</span>
      )}
    </div>
  )
}

type PriceDraftCellsProps = {
  draft: PriceDraft
  disabled?: boolean
  pending?: boolean
  isAudio?: boolean
  disableGroupRatio?: boolean
  onChange: (patch: Partial<PriceDraft>) => void
}

function PriceDraftCells({
  draft,
  disabled,
  pending,
  isAudio,
  disableGroupRatio,
  onChange,
}: PriceDraftCellsProps) {
  const { t } = useTranslation('settings')
  const isTokens = draft.billingType === 'tokens'
  const controlDisabled = disabled || pending

  return (
    <>
      <div className="pr-2" onClick={(event) => event.stopPropagation()}>
        <Select
          value={draft.billingType}
          onValueChange={(value) =>
            onChange({ billingType: value as PriceDraft['billingType'] })
          }
          disabled={controlDisabled}
        >
          <SelectTrigger
            className={cn(
              'h-9 px-2 text-xs',
              controlDisabled && 'cursor-not-allowed opacity-55',
            )}
          >
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
      </div>
      <PriceInputCell
        value={draft.inputValue}
        placeholder="$ / 1M"
        disabled={controlDisabled || !isTokens}
        onChange={(value) => onChange({ inputValue: value })}
      />
      <PriceInputCell
        value={draft.outputValue}
        placeholder="$ / 1M"
        disabled={controlDisabled || !isTokens}
        onChange={(value) => onChange({ outputValue: value })}
      />
      <PriceInputCell
        value={draft.audioOutputValue}
        placeholder="$ / 1M"
        disabled={controlDisabled || !isTokens || !isAudio}
        onChange={(value) => onChange({ audioOutputValue: value })}
      />
      <PriceInputCell
        value={draft.cacheInputValue}
        placeholder="$ / 1M"
        disabled={controlDisabled || !isTokens}
        onChange={(value) => onChange({ cacheInputValue: value })}
      />
      <PriceInputCell
        value={draft.createCacheInputValue}
        placeholder="$ / 1M"
        disabled={controlDisabled || !isTokens}
        onChange={(value) => onChange({ createCacheInputValue: value })}
      />
      <PriceInputCell
        value={draft.createCache1hInputValue}
        placeholder="$ / 1M"
        disabled={controlDisabled || !isTokens}
        onChange={(value) => onChange({ createCache1hInputValue: value })}
      />
      <PriceInputCell
        value={draft.perRequestValue}
        placeholder={`$ ${t('modelsPrice.perRequestSuffix')}`}
        disabled={controlDisabled || isTokens}
        onChange={(value) => onChange({ perRequestValue: value })}
      />
      <PriceInputCell
        value={draft.groupRatio}
        placeholder="1"
        disabled={controlDisabled || disableGroupRatio}
        onChange={(value) => onChange({ groupRatio: value })}
      />
    </>
  )
}

type PriceInputCellProps = {
  value: string
  placeholder: string
  disabled?: boolean
  onChange: (value: string) => void
}

function PriceInputCell({
  value,
  placeholder,
  disabled,
  onChange,
}: PriceInputCellProps) {
  return (
    <div className="pr-2" onClick={(event) => event.stopPropagation()}>
      <Input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        inputMode="decimal"
        className="h-9 px-2 text-xs"
      />
    </div>
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
