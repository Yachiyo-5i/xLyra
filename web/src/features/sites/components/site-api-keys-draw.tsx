import { useEffect, useState, type ReactNode } from 'react'
import { ErrorDetails } from '@/components/common/error-details'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  LoaderCircle,
  PencilLine,
  RefreshCw,
  Settings2,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import {
  ModelsDraw,
  type ModelsDrawItem,
} from '@/components/common/models-draw'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Draw,
  DrawBody,
  DrawContent,
  DrawFooter,
  DrawHeader,
  DrawTitle,
} from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/lib/toast'
import {
  createSiteAPIKey,
  deleteSiteAPIKey,
  listSiteAPIKeys,
  listSiteModels,
  revealSiteAPIKey,
  refreshSiteAPIKey,
  sitesQueryKeys,
  updateSiteAPIKeyModelStatus,
  updateSiteAPIKeyConfig,
  updateSiteAPIKeySecret,
  updateSiteAPIKeyStatus,
  type Site,
  type SiteAPIKey,
  type SiteAPIKeyModel,
  type SiteModel,
} from '@/features/sites/api/sites'
import { routeQueryKeys } from '@/features/routes/api/routes'
import {
  apiKeyModels,
  canCompleteAPIKey,
  accountBalanceDetails,
  formatAPIKeyValue,
  formatDisplayQuota,
  formatProbeAmount,
  isNewAPISite,
  siteAPIKeyGroupBadgeVariant,
} from '@/features/sites/lib/site-utils'
import {
  removeAPIKey,
  replaceAPIKey,
  upsertAPIKey,
} from '@/features/sites/lib/site-cache'
import { modelNameIconInfo } from '@/features/sites/lib/model-icon'
import { formatEndpointTypeLabel, upstreamEndpointTypes } from '@/features/models/lib/model-helpers'
import {
  SiteAPIKeyFormFields,
} from '@/features/sites/components/site-api-key-form'
import {
  DEFAULT_API_KEY_FORM_DRAFT,
  parseSiteAPIKeyForm,
  type APIKeyFormDraft,
} from '@/features/sites/components/site-api-key-form-data'
import { useMobileLayout } from '@/hooks/use-media-query'

const EMPTY_API_KEYS: SiteAPIKey[] = []

export function SiteAPIKeysDraw({
  site,
  onOpenChange,
}: {
  site: Site | null
  onOpenChange: (open: boolean) => void
}) {
  const queryClient = useQueryClient()
  const { t } = useTranslation('sites')
  const isMobile = useMobileLayout()
  const [modelsAPIKey, setModelsAPIKey] = useState<SiteAPIKey | null>(null)
  const [modelProtocolTarget, setModelProtocolTarget] = useState<SiteAPIKeyModel | null>(null)
  const [modelProtocolMode, setModelProtocolMode] = useState<'inherit' | 'allowlist' | 'disabled'>('inherit')
  const [modelProtocolTypes, setModelProtocolTypes] = useState<string[]>([])
  const [addingSiteModelOpen, setAddingSiteModelOpen] = useState(false)
  const [selectedSiteModel, setSelectedSiteModel] = useState<SiteModel | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const [addingAPIKey, setAddingAPIKey] = useState(false)
  const [configuringAPIKey, setConfiguringAPIKey] = useState<SiteAPIKey | null>(
    null,
  )
  const [editingAPIKey, setEditingAPIKey] = useState<SiteAPIKey | null>(null)
  const [deletingAPIKey, setDeletingAPIKey] = useState<SiteAPIKey | null>(null)
  const [newAPIKeyDraft, setNewAPIKeyDraft] = useState(
    DEFAULT_API_KEY_FORM_DRAFT,
  )
  const [configDraft, setConfigDraft] = useState<APIKeyFormDraft>(
    DEFAULT_API_KEY_FORM_DRAFT,
  )
  const [secretInput, setSecretInput] = useState('')
  const open = Boolean(site)
  const supportsCostMultiplier =
    site?.supports_api_key_cost_multiplier === true

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [])

  const queryKey = site
    ? [...sitesQueryKeys.detail(site.id), 'api-keys']
    : [...sitesQueryKeys.all, 'api-keys', 'none']
  const apiKeysQuery = useQuery({
    queryKey,
    queryFn: async () => {
      if (!site) return { items: EMPTY_API_KEYS }
      return listSiteAPIKeys(site.id)
    },
    enabled: open,
  })
  const siteModelsQuery = useQuery({
    queryKey: site ? [...sitesQueryKeys.models(site.id), 'api-key-options'] : ['sites', 'models', 'none'],
    queryFn: () => (site ? listSiteModels(site.id) : Promise.resolve({ items: [] as SiteModel[] })),
    enabled: open && Boolean(modelsAPIKey),
  })
  const invalidatePricingViews = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['settings', 'model-prices'] }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'all-pricings'],
      }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'models-marketplace-api-keys'],
      }),
    ])
  }
  const createAPIKeyMutation = useMutation({
    mutationFn: (input: {
      apiKey: string
      name: string
      routingPriority: number
      upstreamCostMultiplier?: number
    }) => {
      if (!site) throw new Error('site required')
      return createSiteAPIKey(site.id, input)
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(
        queryKey,
        (current: { items: SiteAPIKey[] } | undefined) =>
          upsertAPIKey(current, result.api_key),
      )
      setAddingAPIKey(false)
      setNewAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
      if (site) {
        await queryClient.invalidateQueries({
          queryKey: sitesQueryKeys.models(site.id),
        })
      }
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      })
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.all })
      await invalidatePricingViews()
      toast.success(t('apiKeys.toast.syncQueued'))
    },
    onError: (error) =>
      toast.error(t('apiKeys.toast.createFailed'), {
        description: error.message,
      }),
  })
  const updateMutation = useMutation({
    mutationFn: ({
      apiKeyId,
      enabled,
    }: {
      apiKeyId: string
      enabled: boolean
    }) => {
      if (!site) throw new Error('site required')
      return updateSiteAPIKeyStatus(site.id, apiKeyId, { enabled })
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(
        queryKey,
        (current: { items: SiteAPIKey[] } | undefined) =>
          replaceAPIKey(current, result.api_key),
      )
      setModelsAPIKey((current) =>
        current?.id === result.api_key.id ? result.api_key : current,
      )
      if (site) {
        await queryClient.invalidateQueries({
          queryKey: sitesQueryKeys.models(site.id),
        })
      }
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      })
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.all })
      await invalidatePricingViews()
    },
    onError: (error) =>
      toast.error(t('apiKeys.toast.updateFailed'), {
        description: error.message,
      }),
  })
  const updateConfigMutation = useMutation({
    mutationFn: (input: {
      apiKeyId: string
      name: string
      routingPriority: number
      upstreamCostMultiplier?: number
    }) => {
      if (!site) throw new Error('site required')
      return updateSiteAPIKeyConfig(site.id, input.apiKeyId, input)
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(
        queryKey,
        (current: { items: SiteAPIKey[] } | undefined) =>
          replaceAPIKey(current, result.api_key),
      )
      setModelsAPIKey((current) =>
        current?.id === result.api_key.id ? result.api_key : current,
      )
      setConfiguringAPIKey(null)
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.all })
      await invalidatePricingViews()
      toast.success(t('apiKeys.toast.updated'))
    },
    onError: (error) =>
      toast.error(t('apiKeys.toast.updateFailed'), {
        description: error.message,
      }),
  })
  const updateSecretMutation = useMutation({
    mutationFn: ({
      apiKeyId,
      apiKey,
    }: {
      apiKeyId: string
      apiKey: string
    }) => {
      if (!site) throw new Error('site required')
      return updateSiteAPIKeySecret(site.id, apiKeyId, { apiKey })
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(
        queryKey,
        (current: { items: SiteAPIKey[] } | undefined) =>
          replaceAPIKey(current, result.api_key),
      )
      setModelsAPIKey((current) =>
        current?.id === result.api_key.id ? result.api_key : current,
      )
      setEditingAPIKey(null)
      setSecretInput('')
      onOpenChange(false)
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      })
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.all })
      await invalidatePricingViews()
      toast.success(t('apiKeys.toast.syncQueued'))
    },
    onError: (error) =>
      toast.error(t('apiKeys.toast.updateFailed'), {
        description: error.message,
      }),
  })
  const deleteMutation = useMutation({
    mutationFn: ({ apiKeyId }: { apiKeyId: string }) => {
      if (!site) throw new Error('site required')
      return deleteSiteAPIKey(site.id, apiKeyId)
    },
    onSuccess: async (_, variables) => {
      queryClient.setQueryData(
        queryKey,
        (current: { items: SiteAPIKey[] } | undefined) =>
          removeAPIKey(current, variables.apiKeyId),
      )
      setModelsAPIKey((current) =>
        current?.id === variables.apiKeyId ? null : current,
      )
      setDeletingAPIKey(null)
      if (site) {
        await queryClient.invalidateQueries({
          queryKey: sitesQueryKeys.models(site.id),
        })
      }
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      })
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.all })
      await invalidatePricingViews()
      toast.success(t('apiKeys.toast.deleted'))
    },
    onError: (error) =>
      toast.error(t('apiKeys.toast.deleteFailed'), {
        description: error.message,
      }),
  })
  async function applyAPIKeyModelUpdate(
    result: Awaited<ReturnType<typeof updateSiteAPIKeyModelStatus>>,
  ) {
    queryClient.setQueryData(
      queryKey,
      (current: { items: SiteAPIKey[] } | undefined) =>
        replaceAPIKey(current, result.api_key),
    )
    setModelsAPIKey((current) =>
      current?.id === result.api_key.id ? result.api_key : current,
    )
    if (site) {
      await queryClient.invalidateQueries({
        queryKey: sitesQueryKeys.models(site.id),
      })
    }
    await queryClient.invalidateQueries({
      queryKey: [...sitesQueryKeys.all, 'canonical-models'],
    })
    await queryClient.invalidateQueries({
      queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
    })
    await queryClient.invalidateQueries({ queryKey: routeQueryKeys.all })
    await invalidatePricingViews()
  }
  const updateModelMutation = useMutation({
    mutationFn: ({
      apiKeyId,
      model,
      enabled,
      siteModelId,
      endpointMode,
      endpointTypes,
    }: {
      apiKeyId: string
      model: string
      enabled?: boolean
      siteModelId?: string
      endpointMode?: 'inherit' | 'allowlist' | 'disabled'
      endpointTypes?: string[]
    }) => {
      if (!site) throw new Error('site required')
      return updateSiteAPIKeyModelStatus(site.id, apiKeyId, { model, enabled, siteModelId, endpointMode, endpointTypes })
    },
    onSuccess: applyAPIKeyModelUpdate,
    onError: (error) =>
      toast.error(t('apiKeys.toast.modelToggleFailed'), {
        description: error.message,
      }),
  })
  const bulkUpdateModelMutation = useMutation({
    mutationFn: async ({
      apiKeyId,
      models,
      enabled,
    }: {
      apiKeyId: string
      models: string[]
      enabled: boolean
    }) => {
      if (!site) throw new Error('site required')
      let latest: Awaited<
        ReturnType<typeof updateSiteAPIKeyModelStatus>
      > | null = null
      let firstError: Error | null = null
      for (const model of models) {
        try {
          latest = await updateSiteAPIKeyModelStatus(site.id, apiKeyId, {
            model,
            enabled,
          })
        } catch (error) {
          if (!firstError) {
            firstError =
              error instanceof Error ? error : new Error(String(error))
          }
        }
      }
      if (!latest) throw firstError ?? new Error('no models to update')
      return { latest, firstError }
    },
    onSuccess: async ({ latest, firstError }) => {
      await applyAPIKeyModelUpdate(latest)
      if (firstError) {
        toast.error(t('apiKeys.toast.modelToggleFailed'), {
          description: firstError.message,
        })
      }
    },
    onError: (error) =>
      toast.error(t('apiKeys.toast.modelToggleFailed'), {
        description: error.message,
      }),
  })
  const refreshMutation = useMutation({
    mutationFn: ({ apiKeyId }: { apiKeyId: string }) => {
      if (!site) throw new Error('site required')
      return refreshSiteAPIKey(site.id, apiKeyId)
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(
        queryKey,
        (current: { items: SiteAPIKey[] } | undefined) =>
          replaceAPIKey(current, result.api_key),
      )
      setModelsAPIKey((current) =>
        current?.id === result.api_key.id ? result.api_key : current,
      )
      if (site) {
        await queryClient.invalidateQueries({
          queryKey: sitesQueryKeys.models(site.id),
        })
      }
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      })
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.all })
      await invalidatePricingViews()
      if (result.api_key.message) {
        toast.warning(t('apiKeys.toast.refreshFailed'), {
          description: result.api_key.message,
        })
      } else {
        toast.success(t('apiKeys.toast.refreshed'))
      }
    },
    onError: (error) =>
      toast.error(t('apiKeys.toast.refreshFailed'), {
        description: error.message,
      }),
  })

  const items = [...(apiKeysQuery.data?.items ?? EMPTY_API_KEYS)].sort(
    (left, right) => {
      const priority =
        (right.routing_priority ?? 1) - (left.routing_priority ?? 1)
      if (priority !== 0) return priority
      return left.id.localeCompare(right.id)
    },
  )
  const protocolOptions = (() => {
    const siteModel = siteModelsQuery.data?.items.find((item) => item.id === modelProtocolTarget?.site_model_id)
    const siteTypes = endpointTypesFromCapabilities(siteModel?.capabilities)
    return (modelProtocolTarget?.supported_endpoint_types ?? []).filter((value) => siteTypes.length === 0 || siteTypes.includes(value))
  })()
  const assignableSiteModels = (siteModelsQuery.data?.items ?? []).filter((model) => (
    !apiKeyModels(modelsAPIKey).some((item) => item.site_model_id === model.id)
  ))
  const canAddAPIKey = site ? canAddOfficialAPIKey(site) : false
  const nestedOpen = addingAPIKey
    || Boolean(configuringAPIKey)
    || Boolean(editingAPIKey)
    || Boolean(deletingAPIKey)
    || Boolean(modelsAPIKey)
    || addingSiteModelOpen
    || Boolean(modelProtocolTarget)

  function closeInnermostNested() {
    if (addingSiteModelOpen) {
      setAddingSiteModelOpen(false)
      setSelectedSiteModel(null)
      return
    }
    if (modelProtocolTarget && !updateModelMutation.isPending) {
      setModelProtocolTarget(null)
      return
    }
    if (modelsAPIKey) {
      setModelsAPIKey(null)
      return
    }
    if (deletingAPIKey && !deleteMutation.isPending) {
      setDeletingAPIKey(null)
      return
    }
    if (addingAPIKey && !createAPIKeyMutation.isPending) {
      setAddingAPIKey(false)
      setNewAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
      return
    }
    if (configuringAPIKey && !updateConfigMutation.isPending) {
      setConfiguringAPIKey(null)
      return
    }
    if (editingAPIKey && !updateSecretMutation.isPending) {
      setEditingAPIKey(null)
      setSecretInput('')
    }
  }

  function handleMainOpenChange(next: boolean) {
    if (!next && !isMobile && nestedOpen) {
      closeInnermostNested()
      return
    }
    if (!next) {
      setModelsAPIKey(null)
      setAddingAPIKey(false)
      setConfiguringAPIKey(null)
      setEditingAPIKey(null)
      setDeletingAPIKey(null)
      setAddingSiteModelOpen(false)
      setSelectedSiteModel(null)
      setModelProtocolTarget(null)
      setNewAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
      setSecretInput('')
    }
    onOpenChange(next)
  }

  const headerActions = canAddAPIKey ? (
    <Button size="sm" onClick={() => setAddingAPIKey(true)}>
      {t('apiKeys.add')}
    </Button>
  ) : null

  const listBody = (
    <>
      {apiKeysQuery.isLoading ? (
        <p className="text-muted-soft text-center text-sm py-10">
          {t('apiKeys.loading')}
        </p>
      ) : items.length ? (
        <div className={isMobile ? 'space-y-3' : 'divide-y divide-[hsl(var(--glass-divider))]'}>
          {items.map((item) => {
            const ensureSKPrefix = site ? isNewAPISite(site) : false
                  const displayKey = formatAPIKeyValue(item.key, {
                    ensureSKPrefix,
                  })
                  const canCopy = !item.secret_missing
                  const remaining = item.usage?.data?.total_available ?? null
                  const total = item.usage?.data?.total_granted ?? null
                  const unlimited = item.usage?.data?.unlimited_quota === true
                  const quotaText = unlimited
                    ? '∞ / ∞'
                    : remaining !== null || total !== null
                      ? `${remaining === null ? '-' : '$' + formatDisplayQuota(remaining)} / ${total === null ? '-' : '$' + formatDisplayQuota(total)}`
                      : null
                  const openCodeGoUsage = site?.site_type === 'opencode_go' ? item.usage : undefined
                  const openCodeGoResetAt = openCodeGoUsage?.reset_at
                    ? new Date(openCodeGoUsage.reset_at).toLocaleString()
                    : '-'
                  const openCodeGoLimitActive = openCodeGoUsage?.available === false && (
                    !openCodeGoUsage.reset_at || new Date(openCodeGoUsage.reset_at).getTime() > now
                  )
                  const openCodeGoQuotaText = openCodeGoLimitActive
                    ? t('apiKeys.openCodeGoLimited', {
                        window: openCodeGoUsage.limit_name || '-',
                        reset: openCodeGoResetAt,
                      })
                    : site?.site_type === 'opencode_go'
                      ? t('apiKeys.openCodeGoPlan')
                      : null
                  const probe = item.quota_probe
                  const balanceDetails = site?.quota_probe?.probe_type === 'deepseek' || site?.quota_probe?.probe_type === 'moonshot'
                    ? accountBalanceDetails(probe?.entries).filter((detail) => detail.label === 'accountBalance')
                    : []
                  const probeEntry = probe?.entries?.length
                    ? (probe.entries.find(
                        (entry) => entry.label === 'balance',
                      ) ?? probe.entries[0])
                    : undefined
                  const probeText = probeEntry
                    ? probeEntry.unlimited
                      ? probeEntry.used != null
                        ? `$${formatProbeAmount(probeEntry.used)} / ∞`
                        : '∞'
                      : probeEntry.remaining != null
                        ? `$${formatProbeAmount(probeEntry.remaining)}${probeEntry.limit != null ? ' / $' + formatProbeAmount(probeEntry.limit) : ''}`
                        : null
                    : null
                  const probeFailed = Boolean(probe && probe.status !== 'ok')

            return (
              <div
                key={item.id}
                className={isMobile ? 'section-soft space-y-3 rounded-lg p-4' : 'space-y-3 py-4 first:pt-0 last:pb-0'}
              >
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex items-center gap-2 min-w-0">
                          <span className="truncate font-medium text-sm text-foreground">
                            {item.name || t('apiKeys.defaultKey')}
                          </span>
                          {item.group ? (
                            <Badge
                              variant={siteAPIKeyGroupBadgeVariant(item.group)}
                              className="shrink-0 text-[10px] px-1.5 py-0"
                            >
                              {item.group}
                            </Badge>
                          ) : null}
                          {item.sync_status ? <APIKeySyncStatusBadge apiKey={item} /> : null}
                        </div>
                        <Switch
                          checked={item.enabled}
                          disabled={
                            updateMutation.isPending &&
                            updateMutation.variables?.apiKeyId === item.id
                          }
                          aria-label={t('apiKeys.toggleLabel', {
                            name: item.name,
                          })}
                          onCheckedChange={(checked) =>
                            updateMutation.mutate({
                              apiKeyId: item.id,
                              enabled: checked,
                            })
                          }
                        />
                      </div>

                      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-soft">
                        {site?.supports_multiple_api_keys ? (
                          <>
                            <Badge variant="neutral">
                              {t('apiKeys.routingPriority')}:{' '}
                              {formatAPIKeyNumber(item.routing_priority ?? 1)}
                            </Badge>
                            {supportsCostMultiplier ? (
                              <Badge variant="neutral">
                                {t('apiKeys.costMultiplier')}:{' '}
                                {formatAPIKeyNumber(
                                  item.upstream_cost_multiplier ?? 1,
                                )}
                                x
                              </Badge>
                            ) : null}
                          </>
                        ) : null}
                        {item.upstream_name &&
                        item.upstream_name !== item.name ? (
                          <span className="truncate">
                            {t('apiKeys.upstreamName')}: {item.upstream_name}
                          </span>
                        ) : null}
                      </div>

                      <button
                        type="button"
                        className="block w-full cursor-pointer rounded-md bg-[hsl(var(--surface-field))] px-3 py-2 text-left font-mono text-xs text-foreground truncate hover:bg-[hsl(var(--surface-soft-hover))] transition-colors"
                        title={
                          canCopy
                            ? t('apiKeys.copyTooltip')
                            : t('apiKeys.noCopyTooltip')
                        }
                        onClick={() => {
                          if (!canCopy || !site) {
                            toast.warning(t('apiKeys.noCopyTooltip'), {
                              description: t('apiKeys.noCopyTooltip'),
                            })
                            return
                          }
                          void revealSiteAPIKey(site.id, item.id)
                            .then((revealed) => {
                              const copyKey = revealed.copy_key?.trim()
                              if (!copyKey) {
                                toast.warning(t('apiKeys.noCopyTooltip'), {
                                  description: t('apiKeys.noCopyTooltip'),
                                })
                                return
                              }
                              return copyToClipboard(
                                copyKey,
                                t('apiKeys.copied'),
                              )
                            })
                            .catch(() => {
                              toast.warning(t('apiKeys.noCopyTooltip'), {
                                description: t('apiKeys.noCopyTooltip'),
                              })
                            })
                        }}
                      >
                        {displayKey}
                      </button>

                      <div className="flex items-center justify-between text-xs text-muted-soft">
                        <div className="flex min-w-0 flex-wrap items-center gap-3">
                          {probeFailed ? (
                            <span className="text-red-400" title={probe?.error}>
                              {t('apiKeys.quotaProbeFailed')}
                            </span>
                          ) : balanceDetails.length > 0 ? (
                            <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1">
                              {balanceDetails.map((detail, index) => (
                                <div key={`${detail.label}-${index}`} className="contents">
                                  <span>{t(`table.quotaDetails.${detail.label}`)}</span>
                                  <span className="text-foreground tabular-nums">{detail.value}</span>
                                </div>
                              ))}
                            </div>
                          ) : (openCodeGoQuotaText ?? probeText ?? quotaText) ? (
                            <span className="tabular-nums whitespace-normal" title={site?.site_type === 'opencode_go' ? t('apiKeys.openCodeGoUnavailable') : undefined}>
                              {openCodeGoQuotaText ?? probeText ?? quotaText}
                            </span>
                          ) : (
                            <span>-</span>
                          )}
                          <button
                            type="button"
                            className="hover:text-foreground cursor-pointer"
                            onClick={() => setModelsAPIKey(item)}
                          >
                            {item.models.length} {t('apiKeys.modelsTitle')}
                          </button>
                        </div>
                        <div className="flex items-center gap-1">
                          {canCompleteAPIKey(item) ? (
                            <Button
                              size="sm"
                              variant="ghost"
                              className="h-7 text-xs"
                              onClick={() => {
                                setEditingAPIKey(item)
                                setSecretInput('')
                              }}
                            >
                              <PencilLine className="h-3 w-3 mr-1" />
                              {t('apiKeys.complete')}
                            </Button>
                          ) : null}
                          {site?.supports_multiple_api_keys ? (
                            <Button
                              size="icon"
                              variant="ghost"
                              className="h-7 w-7"
                              title={t('apiKeys.editConfig')}
                              aria-label={t('apiKeys.editConfigLabel', {
                                name: item.name || t('apiKeys.defaultKey'),
                              })}
                              onClick={() => {
                                setConfiguringAPIKey(item)
                                setConfigDraft({
                                  name: item.display_name ?? item.name ?? '',
                                  apiKey: '',
                                  routingPriority: String(
                                    item.routing_priority ?? 1,
                                  ),
                                  upstreamCostMultiplier: String(
                                    item.upstream_cost_multiplier ?? 1,
                                  ),
                                })
                              }}
                            >
                              <Settings2 className="h-3.5 w-3.5" />
                            </Button>
                          ) : null}
                          <Button
                            size="icon"
                            variant="ghost"
                            className="h-7 w-7"
                            disabled={
                              refreshMutation.isPending &&
                              refreshMutation.variables?.apiKeyId === item.id
                            }
                            title={t('apiKeys.refresh')}
                            aria-label={t('apiKeys.refreshLabel', {
                              name: item.name || t('apiKeys.defaultKey'),
                            })}
                            onClick={() =>
                              refreshMutation.mutate({ apiKeyId: item.id })
                            }
                          >
                            {refreshMutation.isPending &&
                            refreshMutation.variables?.apiKeyId === item.id ? (
                              <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                            ) : (
                              <RefreshCw className="h-3.5 w-3.5" />
                            )}
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="h-7 w-7 text-red-500 hover:bg-red-500/10 hover:text-red-400"
                            disabled={deleteMutation.isPending}
                            title={t('apiKeys.delete')}
                            aria-label={t('apiKeys.deleteLabel', {
                              name: item.name || t('apiKeys.defaultKey'),
                            })}
                            onClick={() => setDeletingAPIKey(item)}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </div>
              </div>
            )
          })}
        </div>
      ) : (
        <p className="text-muted-soft text-center text-sm py-10">
          {t('apiKeys.noKey')}
        </p>
      )}
    </>
  )

  const addForm = (
    <SiteAPIKeyFormFields
      draft={newAPIKeyDraft}
      onChange={setNewAPIKeyDraft}
      showAPIKey
      showCostMultiplier={supportsCostMultiplier}
      t={t}
    />
  )
  const addFooter = (
    <>
      <Button
        onClick={() => {
          const values = parseSiteAPIKeyForm(
            newAPIKeyDraft,
            {
              includeCostMultiplier: supportsCostMultiplier,
              requireAPIKey: true,
            },
          )
          if (!values?.apiKey) return
          createAPIKeyMutation.mutate({
            ...values,
            apiKey: values.apiKey,
          })
        }}
        disabled={
          !parseSiteAPIKeyForm(
            newAPIKeyDraft,
            {
              includeCostMultiplier: supportsCostMultiplier,
              requireAPIKey: true,
            },
          ) ||
          createAPIKeyMutation.isPending
        }
      >
        {createAPIKeyMutation.isPending ? (
          <LoaderCircle className="h-4 w-4 animate-spin" />
        ) : null}
        {t('apiKeys.save')}
      </Button>
      <Button
        variant="ghost"
        onClick={() => {
          setAddingAPIKey(false)
          setNewAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
        }}
        disabled={createAPIKeyMutation.isPending}
      >
        {t('apiKeys.cancel')}
      </Button>
    </>
  )

  const configForm = (
    <SiteAPIKeyFormFields
      draft={configDraft}
      onChange={setConfigDraft}
      showAPIKey={false}
      showCostMultiplier={supportsCostMultiplier}
      t={t}
    />
  )
  const configFooter = (
    <>
      <Button
        onClick={() => {
          const values = parseSiteAPIKeyForm(
            configDraft,
            {
              includeCostMultiplier: supportsCostMultiplier,
              requireAPIKey: false,
            },
          )
          if (!configuringAPIKey || !values) return
          updateConfigMutation.mutate({
            apiKeyId: configuringAPIKey.id,
            ...values,
          })
        }}
        disabled={
          !parseSiteAPIKeyForm(configDraft, {
            includeCostMultiplier: supportsCostMultiplier,
            requireAPIKey: false,
          }) ||
          updateConfigMutation.isPending
        }
      >
        {updateConfigMutation.isPending ? (
          <LoaderCircle className="h-4 w-4 animate-spin" />
        ) : null}
        {t('apiKeys.save')}
      </Button>
      <Button
        variant="ghost"
        onClick={() => setConfiguringAPIKey(null)}
        disabled={updateConfigMutation.isPending}
      >
        {t('apiKeys.cancel')}
      </Button>
    </>
  )

  const secretForm = (
    <>
      <span className="block text-sm font-medium">ApiKey</span>
      <Input
        value={secretInput}
        autoComplete="off"
        onChange={(event) => setSecretInput(event.target.value)}
      />
    </>
  )
  const secretFooter = (
    <>
      <Button
        onClick={() => {
          const value = secretInput.trim()
          if (!editingAPIKey || !value) return
          updateSecretMutation.mutate({
            apiKeyId: editingAPIKey.id,
            apiKey: value,
          })
        }}
        disabled={!secretInput.trim() || updateSecretMutation.isPending}
      >
        {updateSecretMutation.isPending ? (
          <LoaderCircle className="h-4 w-4 animate-spin" />
        ) : null}
        {t('apiKeys.save')}
      </Button>
      <Button
        variant="ghost"
        onClick={() => {
          setEditingAPIKey(null)
          setSecretInput('')
        }}
        disabled={updateSecretMutation.isPending}
      >
        {t('apiKeys.cancel')}
      </Button>
    </>
  )

  return (
    <>
      {isMobile ? (
        <Draw open={open} onOpenChange={handleMainOpenChange}>
          <DrawContent side="right">
            <DrawHeader className="flex items-center justify-between gap-3">
              <DrawTitle>{t('apiKeys.title')}</DrawTitle>
              {headerActions}
            </DrawHeader>
            <DrawBody>
              {listBody}
            </DrawBody>
          </DrawContent>
        </Draw>
      ) : (
        <Dialog open={open} onOpenChange={handleMainOpenChange}>
          <DialogContent
            size="md"
            onOpenAutoFocus={(event) => event.preventDefault()}
            onPointerDownOutside={(event) => {
              if (nestedOpen) event.preventDefault()
            }}
            onInteractOutside={(event) => {
              if (nestedOpen) event.preventDefault()
            }}
            onEscapeKeyDown={(event) => {
              if (!nestedOpen) return
              event.preventDefault()
              handleMainOpenChange(false)
            }}
          >
            <DialogHeader className="flex flex-row items-center justify-between gap-3">
              <DialogTitle>{t('apiKeys.title')}</DialogTitle>
              {headerActions}
            </DialogHeader>
            <DialogBody className="min-h-0 flex-1 overflow-y-auto">
              {listBody}
            </DialogBody>
          </DialogContent>
        </Dialog>
      )}

      {isMobile ? (
        <>
          <NestedDraw
            open={addingAPIKey}
            title={t('apiKeys.addTitle')}
            bodyClassName="space-y-4"
            onOpenChange={(next) => {
              if (!next && !createAPIKeyMutation.isPending) {
                setAddingAPIKey(false)
                setNewAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
              }
            }}
            footer={addFooter}
          >
            {addForm}
          </NestedDraw>
          <NestedDraw
            open={Boolean(configuringAPIKey)}
            title={t('apiKeys.editConfig')}
            onOpenChange={(next) => {
              if (!next && !updateConfigMutation.isPending) setConfiguringAPIKey(null)
            }}
            footer={configFooter}
          >
            {configForm}
          </NestedDraw>
          <NestedDraw
            open={Boolean(editingAPIKey)}
            title={t('apiKeys.title')}
            bodyClassName="space-y-2"
            onOpenChange={(next) => {
              if (!next && !updateSecretMutation.isPending) {
                setEditingAPIKey(null)
                setSecretInput('')
              }
            }}
            footer={secretFooter}
          >
            {secretForm}
          </NestedDraw>
        </>
      ) : (
        <>
          <NestedEditorDialog
            open={addingAPIKey}
            title={t('apiKeys.addTitle')}
            bodyClassName="space-y-4"
            onOpenChange={(next) => {
              if (!next && !createAPIKeyMutation.isPending) {
                setAddingAPIKey(false)
                setNewAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
              }
            }}
            footer={addFooter}
          >
            {addForm}
          </NestedEditorDialog>
          <NestedEditorDialog
            open={Boolean(configuringAPIKey)}
            title={t('apiKeys.editConfig')}
            onOpenChange={(next) => {
              if (!next && !updateConfigMutation.isPending) setConfiguringAPIKey(null)
            }}
            footer={configFooter}
          >
            {configForm}
          </NestedEditorDialog>
          <NestedEditorDialog
            open={Boolean(editingAPIKey)}
            title={t('apiKeys.title')}
            bodyClassName="space-y-2"
            onOpenChange={(next) => {
              if (!next && !updateSecretMutation.isPending) {
                setEditingAPIKey(null)
                setSecretInput('')
              }
            }}
            footer={secretFooter}
          >
            {secretForm}
          </NestedEditorDialog>
        </>
      )}

      <Dialog
        open={Boolean(deletingAPIKey)}
        onOpenChange={(next) => {
          if (!next && !deleteMutation.isPending) setDeletingAPIKey(null)
        }}
      >
        <DialogContent
          size="sm"
          overlayClassName={isMobile ? undefined : 'z-[60]'}
          className={isMobile ? undefined : 'z-[60]'}
        >
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('apiKeys.deleteDialog.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pt-0">
            <DialogDescription className="mt-0">
              {deletingAPIKey
                ? t('apiKeys.deleteDialog.confirm', {
                    name: deletingAPIKey.name || t('apiKeys.defaultKey'),
                  })
                : t('apiKeys.deleteDialog.confirmDefault')}
            </DialogDescription>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button
              variant="outline"
              onClick={() => setDeletingAPIKey(null)}
              disabled={deleteMutation.isPending}
            >
              {t('apiKeys.cancel')}
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (!deletingAPIKey) return
                deleteMutation.mutate({ apiKeyId: deletingAPIKey.id })
              }}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : null}
              {t('apiKeys.deleteDialog.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ModelsDraw
        key={modelsAPIKey?.id ?? 'api-key-models'}
        open={Boolean(modelsAPIKey)}
        title={
          modelsAPIKey
            ? `${modelsAPIKey.name} ${t('apiKeys.modelsTitle')}`
            : t('apiKeys.modelsTitle')
        }
        items={buildAPIKeyModelItems(modelsAPIKey, siteModelsQuery.data?.items ?? [], t, (model) => {
          setModelProtocolTarget(model)
          const mode = model.endpoint_override?.mode ?? 'inherit'
          setModelProtocolMode(mode)
          setModelProtocolTypes(mode === 'allowlist'
            ? (model.endpoint_override?.endpoint_types ?? [])
            : mode === 'disabled'
              ? []
              : endpointTypesForAPIKeyModel(model, siteModelsQuery.data?.items ?? []))
        })}
        backLabel={t('apiKeys.backToKeys')}
        onBack={() => setModelsAPIKey(null)}
        pendingItemId={
          updateModelMutation.isPending &&
          updateModelMutation.variables?.apiKeyId === modelsAPIKey?.id
            ? updateModelMutation.variables.model
            : undefined
        }
        bulkPending={bulkUpdateModelMutation.isPending}
        shell={isMobile ? 'draw' : 'dialog'}
        nested={!isMobile}
        dialogSize="md"
        dismissLocked={addingSiteModelOpen || Boolean(modelProtocolTarget)}
        onToggleItem={(item, enabled) => {
          if (!modelsAPIKey) return
          updateModelMutation.mutate({
            apiKeyId: modelsAPIKey.id,
            model: item.id,
            enabled,
          })
        }}
        onBulkToggleItems={(items, enabled) => {
          if (!modelsAPIKey) return
          const models = items
            .filter((item) => item.enabled !== enabled)
            .map((item) => item.id)
          if (!models.length) return
          bulkUpdateModelMutation.mutate({
            apiKeyId: modelsAPIKey.id,
            models,
            enabled,
          })
        }}
        onOpenChange={(next) => {
          if (!next) {
            if (addingSiteModelOpen) {
              setAddingSiteModelOpen(false)
              setSelectedSiteModel(null)
              return
            }
            if (modelProtocolTarget && !updateModelMutation.isPending) {
              setModelProtocolTarget(null)
              return
            }
            setModelsAPIKey(null)
          }
        }}
        toolbarAction={modelsAPIKey ? (
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              setSelectedSiteModel(null)
              setAddingSiteModelOpen(true)
            }}
          >
            {t('apiKeys.addModel')}
          </Button>
        ) : null}
      />
      <Dialog
        open={addingSiteModelOpen}
        onOpenChange={(next) => {
          if (next) return
          setAddingSiteModelOpen(false)
          setSelectedSiteModel(null)
        }}
      >
        <DialogContent size="sm" overlayClassName="z-[60]" className="z-[60]">
          <DialogHeader>
            <DialogTitle>{t('apiKeys.addSiteModelTitle')}</DialogTitle>
            <DialogDescription>{t('apiKeys.addSiteModelDescription')}</DialogDescription>
          </DialogHeader>
          <DialogBody className="min-h-0 flex-1 space-y-1 overflow-y-auto">
            {assignableSiteModels.length ? assignableSiteModels.map((model) => {
              const selected = selectedSiteModel?.id === model.id
              return (
                <button
                  key={model.id}
                  type="button"
                  className={`w-full rounded-lg px-3 py-2.5 text-left transition-colors ${selected ? 'bg-[hsl(var(--surface-subtle))]' : 'hover:bg-[hsl(var(--surface-subtle))]'}`}
                  onClick={() => setSelectedSiteModel(model)}
                >
                  <div className="truncate text-sm font-medium text-foreground">{model.display_name || model.upstream_model_name}</div>
                  {model.display_name && model.display_name !== model.upstream_model_name ? (
                    <div className="truncate text-xs text-muted-soft">{model.upstream_model_name}</div>
                  ) : null}
                </button>
              )
            }) : (
              <p className="py-8 text-center text-sm text-muted-soft">{t('apiKeys.addSiteModelEmpty')}</p>
            )}
          </DialogBody>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => {
                setAddingSiteModelOpen(false)
                setSelectedSiteModel(null)
              }}
            >
              {t('apiKeys.cancel')}
            </Button>
            <Button
              disabled={!selectedSiteModel || !modelsAPIKey || updateModelMutation.isPending}
              onClick={() => {
                if (!selectedSiteModel || !modelsAPIKey) return
                updateModelMutation.mutate({
                  apiKeyId: modelsAPIKey.id,
                  model: selectedSiteModel.upstream_model_name,
                  enabled: true,
                  siteModelId: selectedSiteModel.id,
                  endpointMode: 'inherit',
                  endpointTypes: [],
                })
                setAddingSiteModelOpen(false)
                setSelectedSiteModel(null)
              }}
            >
              {t('apiKeys.addModel')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={Boolean(modelProtocolTarget)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen && !updateModelMutation.isPending) setModelProtocolTarget(null)
        }}
      >
        <DialogContent size="sm" overlayClassName="z-[60]" className="z-[60]">
          <DialogHeader>
            <DialogTitle>{t('apiKeys.protocol.title')}</DialogTitle>
            <DialogDescription>
              {modelProtocolTarget
                ? t('apiKeys.protocol.namedDescription', { name: modelProtocolTarget.name })
                : t('apiKeys.protocol.description')}
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="min-h-0 flex-1 space-y-4 overflow-y-auto">
            <p className="text-sm text-muted-soft">
              {modelProtocolMode === 'disabled'
                ? t('apiKeys.protocol.modeDisabled')
                : modelProtocolMode === 'allowlist'
                  ? t('apiKeys.protocol.modeAllowlist')
                  : t('apiKeys.protocol.modeInherit')}
            </p>
            {protocolOptions.length ? (
              <div className="divide-y divide-[hsl(var(--glass-divider))]">
                {protocolOptions.map((endpointType) => {
                  const checked = modelProtocolTypes.includes(endpointType)
                  return (
                    <button
                      key={endpointType}
                      type="button"
                      className="flex w-full items-center justify-between gap-3 py-3 text-left first:pt-0 last:pb-0"
                      onClick={() => {
                        setModelProtocolMode('allowlist')
                        setModelProtocolTypes((current) => (
                          checked
                            ? current.filter((item) => item !== endpointType)
                            : current.includes(endpointType) ? current : [...current, endpointType]
                        ))
                      }}
                    >
                      <span className="min-w-0">
                        <span className="block text-sm font-medium text-foreground">{formatEndpointTypeLabel(endpointType)}</span>
                        <span className="block text-xs text-muted-soft">{endpointType}</span>
                      </span>
                      <Checkbox
                        checked={checked}
                        className="pointer-events-none"
                        ariaLabel={formatEndpointTypeLabel(endpointType)}
                        onCheckedChange={() => undefined}
                      />
                    </button>
                  )
                })}
              </div>
            ) : (
              <p className="text-sm text-muted-soft">{t('apiKeys.protocol.empty')}</p>
            )}
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setModelProtocolTarget(null)}>
              {t('apiKeys.cancel')}
            </Button>
            <Button
              disabled={!modelProtocolTarget?.site_model_id || updateModelMutation.isPending || (modelProtocolMode === 'allowlist' && modelProtocolTypes.length === 0)}
              onClick={() => {
                if (!modelProtocolTarget || !modelsAPIKey || !modelProtocolTarget.site_model_id) return
                updateModelMutation.mutate({
                  apiKeyId: modelsAPIKey.id,
                  model: modelProtocolTarget.name,
                  enabled: modelProtocolTarget.enabled,
                  siteModelId: modelProtocolTarget.site_model_id,
                  endpointMode: modelProtocolMode,
                  endpointTypes: modelProtocolTypes,
                })
                setModelProtocolTarget(null)
              }}
            >
              {updateModelMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {t('apiKeys.save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function NestedDraw({
  open,
  title,
  bodyClassName,
  footer,
  children,
  onOpenChange,
}: {
  open: boolean
  title: string
  bodyClassName?: string
  footer: ReactNode
  children: ReactNode
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Draw open={open} onOpenChange={onOpenChange}>
      <DrawContent side="right">
        <DrawHeader>
          <DrawTitle>{title}</DrawTitle>
        </DrawHeader>
        <DrawBody className={bodyClassName}>{children}</DrawBody>
        <DrawFooter>{footer}</DrawFooter>
      </DrawContent>
    </Draw>
  )
}

function NestedEditorDialog({
  open,
  title,
  bodyClassName,
  footer,
  children,
  onOpenChange,
}: {
  open: boolean
  title: string
  bodyClassName?: string
  footer: ReactNode
  children: ReactNode
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="sm"
        overlayClassName="z-[60]"
        className="z-[60]"
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <DialogBody className={`min-h-0 flex-1 overflow-y-auto ${bodyClassName ?? ''}`}>
          {children}
        </DialogBody>
        <DialogFooter>{footer}</DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function formatAPIKeyNumber(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(
    value,
  )
}

function APIKeySyncStatusBadge({ apiKey }: { apiKey: SiteAPIKey }) {
  const { t } = useTranslation('sites')
  const status = apiKey.sync_status?.trim().toLowerCase()
  const message = ['failed', 'partial', 'stale'].includes(status ?? '') ? apiKey.message?.trim() : undefined
  const syncBadge = apiKeySyncBadge(status)
  return (
    <ErrorDetails
      asChild
      message={message}
      description={apiKey.name || t('apiKeys.defaultKey')}
    >
      <Badge variant="outline" className={`shrink-0 px-1.5 py-0 text-[10px] ${syncBadge.className}`}>
        {syncBadge.label(t)}
      </Badge>
    </ErrorDetails>
  )
}

function apiKeySyncBadge(status?: string | null): {
  className: string
  label: (t: (key: string) => string) => string
} {
  switch ((status || '').toLowerCase()) {
    case 'synced':
      return {
        className: 'border-emerald-500/40 text-emerald-400',
        label: (t) => t('apiKeys.sync.synced'),
      }
    case 'failed':
      return {
        className: 'border-red-500/40 text-red-400',
        label: (t) => t('apiKeys.sync.failed'),
      }
    case 'partial':
      return {
        className: 'border-amber-500/40 text-amber-400',
        label: (t) => t('apiKeys.sync.partial'),
      }
    case 'stale':
      return {
        className: 'border-red-500/40 text-red-400',
        label: (t) => t('apiKeys.sync.stale'),
      }
    case 'pending':
      return {
        className: 'border-sky-500/40 text-sky-400',
        label: (t) => t('apiKeys.sync.pending'),
      }
    case 'syncing':
      return {
        className: 'border-sky-500/40 text-sky-400',
        label: (t) => t('apiKeys.sync.syncing'),
      }
    default:
      return {
        className: 'text-muted-soft',
        label: (t) => t('apiKeys.sync.unknown'),
      }
  }
}

function canAddOfficialAPIKey(site: Site): boolean {
  return site.supports_multiple_api_keys === true
}

function buildAPIKeyModelItems(
  apiKey: SiteAPIKey | null,
  siteModels: SiteModel[],
  t: (key: string, options?: { name: string }) => string,
  onConfigure: (model: SiteAPIKeyModel) => void,
): ModelsDrawItem[] {
  if (!apiKey) return []
  const models = apiKeyModels(apiKey)
  if (!models.length) return []
  return models.map((model) => {
    const name = model.name.trim()
    const siteModel = siteModels.find((item) => item.id === model.site_model_id)
    const siteTypes = endpointTypesFromCapabilities(siteModel?.capabilities)
    const protocolTypes = upstreamEndpointTypes(model, siteTypes)
    return {
      id: name,
      displayName: name,
      upstreamName: protocolTypes.join(' '),
      protocols: protocolTypes.map((endpointType) => ({ label: formatEndpointTypeLabel(endpointType), enabled: true })),
      enabled: model.enabled,
      icon: modelNameIconInfo(name),
      trailingAction: model.site_model_id ? (
        <Button type="button" size="icon" variant="ghost" className="h-7 w-7" title={t('apiKeys.protocol.configure')} aria-label={t('apiKeys.protocol.configureLabel', { name })} onClick={() => onConfigure(model)}>
          <Settings2 className="h-4 w-4" />
        </Button>
      ) : undefined,
    }
  })
}

function endpointTypesForAPIKeyModel(model: SiteAPIKeyModel, siteModels: SiteModel[]): string[] {
  const siteModel = siteModels.find((item) => item.id === model.site_model_id)
  const siteTypes = endpointTypesFromCapabilities(siteModel?.capabilities)
  return upstreamEndpointTypes(model, siteTypes)
}

function endpointTypesFromCapabilities(capabilities?: Record<string, unknown>): string[] {
  const direct = capabilities?.supported_endpoint_types
  if (Array.isArray(direct)) return uniqueEndpointTypes(direct.filter((value): value is string => typeof value === 'string'))
  const nested = capabilities?.raw
  if (nested && typeof nested === 'object' && Array.isArray((nested as Record<string, unknown>).supported_endpoint_types)) {
    return uniqueEndpointTypes(((nested as Record<string, unknown>).supported_endpoint_types as unknown[]).filter((value): value is string => typeof value === 'string'))
  }
  return []
}

function uniqueEndpointTypes(values: string[]): string[] {
  return [...new Set(values.map((value) => normalizeEndpointType(value)).filter(Boolean))]
}

function normalizeEndpointType(value: string): string {
  switch (value.trim().toLowerCase()) {
    case 'chat':
    case 'completions':
    case 'openai-chat':
    case 'openai-completions':
      return 'openai'
    case 'responses':
    case 'openai-responses':
      return 'openai-response'
    case 'messages':
    case 'anthropic-message':
      return 'anthropic-messages'
    case 'gemini':
      return 'google-gemini'
    default:
      return value.trim().toLowerCase()
  }
}
