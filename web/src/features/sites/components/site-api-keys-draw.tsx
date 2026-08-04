import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  LoaderCircle,
  PencilLine,
  Plus,
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
import { Switch } from '@/components/ui/switch'
import { toast } from '@/lib/toast'
import {
  createSiteAPIKey,
  deleteSiteAPIKey,
  listSiteAPIKeys,
  revealSiteAPIKey,
  refreshSiteAPIKey,
  sitesQueryKeys,
  updateSiteAPIKeyModelStatus,
  updateSiteAPIKeyConfig,
  updateSiteAPIKeySecret,
  updateSiteAPIKeyStatus,
  type Site,
  type SiteAPIKey,
} from '@/features/sites/api/sites'
import { routeQueryKeys } from '@/features/routes/api/routes'
import {
  apiKeyModels,
  canCompleteAPIKey,
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
import {
  SiteAPIKeyFormFields,
} from '@/features/sites/components/site-api-key-form'
import {
  DEFAULT_API_KEY_FORM_DRAFT,
  parseSiteAPIKeyForm,
  type APIKeyFormDraft,
} from '@/features/sites/components/site-api-key-form-data'

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
  const [modelsAPIKey, setModelsAPIKey] = useState<SiteAPIKey | null>(null)
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
      toast.success(t('apiKeys.toast.created'))
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
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      })
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.all })
      await invalidatePricingViews()
      toast.success(t('apiKeys.toast.updated'))
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
    }: {
      apiKeyId: string
      model: string
      enabled: boolean
    }) => {
      if (!site) throw new Error('site required')
      return updateSiteAPIKeyModelStatus(site.id, apiKeyId, { model, enabled })
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
  const canAddAPIKey = site ? canAddOfficialAPIKey(site) : false

  return (
    <>
      <Draw
        open={open}
        onOpenChange={(next) => {
          if (!next) {
            setModelsAPIKey(null)
            setAddingAPIKey(false)
            setConfiguringAPIKey(null)
            setEditingAPIKey(null)
            setDeletingAPIKey(null)
            setNewAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
            setSecretInput('')
          }
          onOpenChange(next)
        }}
      >
        <DrawContent side="right" size="wide">
          <DrawHeader className="flex items-center justify-between gap-3">
            <DrawTitle>{t('apiKeys.title')}</DrawTitle>
            {canAddAPIKey ? (
              <Button size="sm" onClick={() => setAddingAPIKey(true)}>
                <Plus className="h-4 w-4" />
                {t('apiKeys.add')}
              </Button>
            ) : null}
          </DrawHeader>
          <DrawBody>
            {apiKeysQuery.isLoading ? (
              <p className="text-muted-soft text-center text-sm py-10">
                {t('apiKeys.loading')}
              </p>
            ) : items.length ? (
              <div className="space-y-3">
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
                  const syncBadge = apiKeySyncBadge(item.sync_status)

                  return (
                    <div
                      key={item.id}
                      className="section-soft rounded-lg p-4 space-y-3"
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
                          {item.sync_status ? (
                            <Badge
                              variant="outline"
                              className={`shrink-0 px-1.5 py-0 text-[10px] ${syncBadge.className}`}
                            >
                              {syncBadge.label(t)}
                            </Badge>
                          ) : null}
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
          </DrawBody>
        </DrawContent>
      </Draw>

      <Draw
        open={addingAPIKey}
        onOpenChange={(next) => {
          if (!next && !createAPIKeyMutation.isPending) {
            setAddingAPIKey(false)
            setNewAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
          }
        }}
      >
        <DrawContent side="right" size="wide">
          <DrawHeader>
            <DrawTitle>{t('apiKeys.addTitle')}</DrawTitle>
          </DrawHeader>
          <DrawBody className="space-y-4">
            <SiteAPIKeyFormFields
              draft={newAPIKeyDraft}
              onChange={setNewAPIKeyDraft}
              showAPIKey
              showCostMultiplier={supportsCostMultiplier}
              t={t}
            />
          </DrawBody>
          <DrawFooter>
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
          </DrawFooter>
        </DrawContent>
      </Draw>

      <Draw
        open={Boolean(configuringAPIKey)}
        onOpenChange={(next) => {
          if (!next && !updateConfigMutation.isPending)
            setConfiguringAPIKey(null)
        }}
      >
        <DrawContent side="right" size="wide">
          <DrawHeader>
            <DrawTitle>{t('apiKeys.editConfig')}</DrawTitle>
          </DrawHeader>
          <DrawBody>
            <SiteAPIKeyFormFields
              draft={configDraft}
              onChange={setConfigDraft}
              showAPIKey={false}
              showCostMultiplier={supportsCostMultiplier}
              t={t}
            />
          </DrawBody>
          <DrawFooter>
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
          </DrawFooter>
        </DrawContent>
      </Draw>

      <Draw
        open={Boolean(editingAPIKey)}
        onOpenChange={(next) => {
          if (!next && !updateSecretMutation.isPending) {
            setEditingAPIKey(null)
            setSecretInput('')
          }
        }}
      >
        <DrawContent side="right" size="wide">
          <DrawHeader>
            <DrawTitle>{t('apiKeys.title')}</DrawTitle>
          </DrawHeader>
          <DrawBody className="space-y-2">
            <span className="block text-sm font-medium">ApiKey</span>
            <Input
              value={secretInput}
              autoComplete="off"
              onChange={(event) => setSecretInput(event.target.value)}
            />
          </DrawBody>
          <DrawFooter>
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
          </DrawFooter>
        </DrawContent>
      </Draw>

      <Dialog
        open={Boolean(deletingAPIKey)}
        onOpenChange={(next) => {
          if (!next && !deleteMutation.isPending) setDeletingAPIKey(null)
        }}
      >
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
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
        items={buildAPIKeyModelItems(modelsAPIKey)}
        backLabel={t('apiKeys.backToKeys')}
        onBack={() => setModelsAPIKey(null)}
        pendingItemId={
          updateModelMutation.isPending &&
          updateModelMutation.variables?.apiKeyId === modelsAPIKey?.id
            ? updateModelMutation.variables.model
            : undefined
        }
        bulkPending={bulkUpdateModelMutation.isPending}
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
          if (!next) setModelsAPIKey(null)
        }}
      />
    </>
  )
}

function formatAPIKeyNumber(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 4 }).format(
    value,
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

function buildAPIKeyModelItems(apiKey: SiteAPIKey | null): ModelsDrawItem[] {
  if (!apiKey) return []
  const models = apiKeyModels(apiKey)
  if (!models.length) return []

  return models.map((model) => {
    const name = model.name.trim()
    return {
      id: name,
      displayName: name,
      enabled: model.enabled,
      icon: modelNameIconInfo(name),
    }
  })
}
