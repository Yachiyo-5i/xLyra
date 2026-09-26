import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ChevronDown, LoaderCircle, Search, Trash2 } from 'lucide-react'
import { ModelsDraw } from '@/components/common/models-draw'
import { BrandMark } from '@/components/common/brand-mark'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/lib/toast'
import {
  createManualSiteModel,
  deleteSiteModel,
  listCanonicalModels,
  listSiteAPIKeys,
  listSiteModels,
  sitesQueryKeys,
  updateSiteAPIKeyModelStatus,
  updateSiteModelStatus,
  updateSiteModelsStatus,
  type Site,
  type SiteAPIKey,
  type SiteAPIKeyModel,
  type SiteModel,
} from '@/features/sites/api/sites'
import { replaceAPIKey } from '@/features/sites/lib/site-cache'
import { modelNameIconInfo, siteModelIconInfo } from '@/features/sites/lib/model-icon'
import { buildDisplayModelItems } from '@/features/sites/lib/site-model-items'
import { formatEndpointTypeLabel } from '@/features/models/lib/model-helpers'
import {
  apiKeyModels,
  siteAPIKeyGroupBadgeVariant,
} from '@/features/sites/lib/site-utils'
import { useMobileLayout } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'

const EMPTY_MODELS: SiteModel[] = []
const EMPTY_API_KEYS: SiteAPIKey[] = []

export function SiteModelsDraw({
  site,
  models,
  apiKeys,
  title,
  onModelsChanged,
  onOpenChange,
}: {
  site: Site | null
  models: SiteModel[]
  apiKeys: SiteAPIKey[]
  title?: string
  onModelsChanged?: () => void
  onOpenChange: (open: boolean) => void
}) {
  const queryClient = useQueryClient()
  const { t } = useTranslation(['sites', 'components'])
  const isMobile = useMobileLayout()
  const open = Boolean(site)
  const [addOpen, setAddOpen] = useState(false)
  const [upstreamModelName, setUpstreamModelName] = useState('')
  const [canonicalModelId, setCanonicalModelId] = useState('')
  const [endpointTypes, setEndpointTypes] = useState<string[]>([])
  const [siteCredentialIds, setSiteCredentialIds] = useState<string[]>([])
  const [enabled, setEnabled] = useState(true)
  const [deleteTarget, setDeleteTarget] = useState<SiteModel | null>(null)
  const [manualManage, setManualManage] = useState(false)
  const [search, setSearch] = useState('')
  const [collapsedKeyIds, setCollapsedKeyIds] = useState<Record<string, boolean>>({})
  const modelsQuery = useQuery({
    queryKey: site ? sitesQueryKeys.models(site.id) : [...sitesQueryKeys.all, 'models', 'none'],
    queryFn: async () => {
      if (!site) return { items: EMPTY_MODELS }
      return listSiteModels(site.id)
    },
    enabled: open,
    initialData: models.length ? { items: models } : undefined,
  })
  const apiKeysQueryKey = site
    ? [...sitesQueryKeys.detail(site.id), 'api-keys']
    : [...sitesQueryKeys.all, 'api-keys', 'models-none']
  const apiKeysQuery = useQuery({
    queryKey: apiKeysQueryKey,
    queryFn: async () => {
      if (!site) return { items: EMPTY_API_KEYS }
      return listSiteAPIKeys(site.id)
    },
    enabled: open,
    initialData: apiKeys.length ? { items: apiKeys } : undefined,
  })
  const canonicalModelsQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'canonical-models'],
    queryFn: listCanonicalModels,
    enabled: open,
  })
  const updateMutation = useMutation({
    mutationFn: ({ modelId, enabled }: { modelId: string; enabled: boolean }) => {
      if (!site) throw new Error('site required')
      return updateSiteModelStatus(site.id, modelId, { enabled })
    },
    onSuccess: (result) => {
      if (!site) return
      queryClient.setQueryData(sitesQueryKeys.models(site.id), (current: { items?: SiteModel[] } | undefined) => ({
        items: (current?.items ?? []).map((model) => model.id === result.model.id ? result.model : model),
      }))
      onModelsChanged?.()
    },
    onError: (error) => toast.error(t('models.toast.updateFailed'), { description: error.message }),
  })
  const bulkUpdateMutation = useMutation({
    mutationFn: ({ modelIds, enabled }: { modelIds: string[]; enabled: boolean }) => {
      if (!site) throw new Error('site required')
      return updateSiteModelsStatus(site.id, { modelIds, enabled })
    },
    onSuccess: (result) => {
      if (!site) return
      queryClient.setQueryData(sitesQueryKeys.models(site.id), (current: { items?: SiteModel[] } | undefined) => ({
        items: replaceModels(current?.items ?? [], result.items),
      }))
      onModelsChanged?.()
    },
    onError: (error) => toast.error(t('models.toast.updateFailed'), { description: error.message }),
  })
  const updateKeyModelMutation = useMutation({
    mutationFn: ({
      apiKeyId,
      model,
      enabled,
      siteModelId,
    }: {
      apiKeyId: string
      model: string
      enabled: boolean
      siteModelId?: string
    }) => {
      if (!site) throw new Error('site required')
      return updateSiteAPIKeyModelStatus(site.id, apiKeyId, { model, enabled, siteModelId })
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(
        apiKeysQueryKey,
        (current: { items: SiteAPIKey[] } | undefined) => replaceAPIKey(current, result.api_key),
      )
      if (site) {
        await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.models(site.id) })
      }
      onModelsChanged?.()
    },
    onError: (error) => toast.error(t('models.toast.updateFailed'), { description: error.message }),
  })
  const bulkUpdateKeyModelMutation = useMutation({
    mutationFn: async ({
      items,
      enabled,
    }: {
      items: { apiKeyId: string; model: string; siteModelId?: string }[]
      enabled: boolean
    }) => {
      if (!site) throw new Error('site required')
      const latestByKey = new Map<string, SiteAPIKey>()
      let firstError: Error | null = null
      for (const item of items) {
        try {
          const result = await updateSiteAPIKeyModelStatus(site.id, item.apiKeyId, {
            model: item.model,
            enabled,
            siteModelId: item.siteModelId,
          })
          latestByKey.set(item.apiKeyId, result.api_key)
        } catch (error) {
          if (!firstError) firstError = error instanceof Error ? error : new Error(String(error))
        }
      }
      if (!latestByKey.size) throw firstError ?? new Error('no models to update')
      return { latestByKey, firstError }
    },
    onSuccess: async ({ latestByKey, firstError }) => {
      for (const apiKey of latestByKey.values()) {
        queryClient.setQueryData(
          apiKeysQueryKey,
          (current: { items: SiteAPIKey[] } | undefined) => replaceAPIKey(current, apiKey),
        )
      }
      if (site) {
        await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.models(site.id) })
      }
      onModelsChanged?.()
      if (firstError) {
        toast.error(t('models.toast.updateFailed'), { description: firstError.message })
      }
    },
    onError: (error) => toast.error(t('models.toast.updateFailed'), { description: error.message }),
  })
  const createMutation = useMutation({
    mutationFn: () => {
      if (!site) throw new Error('site required')
      return createManualSiteModel(site.id, {
        upstreamModelName,
        canonicalModelId,
        supportedEndpointTypes: endpointTypes,
        siteCredentialIds,
        enabled,
      })
    },
    onSuccess: async (result) => {
      if (!site) return
      queryClient.setQueryData(sitesQueryKeys.models(site.id), (current: { items?: SiteModel[] } | undefined) => ({
        items: upsertModel(current?.items ?? [], result.model),
      }))
      await queryClient.invalidateQueries({ queryKey: apiKeysQueryKey })
      resetAddForm()
      setAddOpen(false)
      setManualManage(false)
      onModelsChanged?.()
      toast.success(t('models.toast.createSuccess'))
    },
    onError: (error) => toast.error(t('models.toast.createFailed'), { description: error.message }),
  })
  const deleteMutation = useMutation({
    mutationFn: ({ modelId }: { modelId: string }) => {
      if (!site) throw new Error('site required')
      return deleteSiteModel(site.id, modelId)
    },
    onSuccess: async (_, variables) => {
      if (!site) return
      queryClient.setQueryData(sitesQueryKeys.models(site.id), (current: { items?: SiteModel[] } | undefined) => ({
        items: (current?.items ?? []).filter((model) => model.id !== variables.modelId),
      }))
      await queryClient.invalidateQueries({ queryKey: apiKeysQueryKey })
      setDeleteTarget(null)
      onModelsChanged?.()
      toast.success(t('models.toast.deleteSuccess'))
    },
    onError: (error) => toast.error(t('models.toast.deleteFailed'), { description: error.message }),
  })

  const items = modelsQuery.data?.items ?? EMPTY_MODELS
  const liveAPIKeys = apiKeysQuery.data?.items ?? apiKeys
  const modelsByID = useMemo(() => new Map(items.map((model) => [model.id, model])), [items])
  const hasManualModels = useMemo(() => items.some(isManualSiteModel), [items])
  const manualManageActive = manualManage && hasManualModels && open
  const dialogItems = useMemo(() => buildDisplayModelItems(site, items, liveAPIKeys, canonicalModelsQuery.data?.items).map((item) => {
    const model = modelsByID.get(item.id)
    if (!manualManageActive || !model || !isManualSiteModel(model)) return item
    const deleting = deleteMutation.isPending && deleteMutation.variables?.modelId === model.id
    return {
      ...item,
      leadingAction: (
        <Button
          type="button"
          size="icon"
          variant="ghost"
          className="h-6 w-6 text-red-500 hover:bg-red-500/10 hover:text-red-400"
          disabled={deleteMutation.isPending}
          title={t('models.delete.action')}
          aria-label={t('models.delete.ariaLabel', { name: model.upstream_model_name || model.display_name })}
          onClick={() => setDeleteTarget(model)}
        >
          {deleting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
        </Button>
      ),
    }
  }), [canonicalModelsQuery.data?.items, deleteMutation.isPending, deleteMutation.variables?.modelId, items, liveAPIKeys, manualManageActive, modelsByID, site, t])
  const activeCanonicalModels = useMemo(
    () => (canonicalModelsQuery.data?.items ?? []).filter((model) => model.status === 'active'),
    [canonicalModelsQuery.data?.items],
  )
  const sortedAPIKeys = useMemo(
    () => [...liveAPIKeys].sort((left, right) => {
      const priority = (right.routing_priority ?? 1) - (left.routing_priority ?? 1)
      if (priority !== 0) return priority
      return left.id.localeCompare(right.id)
    }),
    [liveAPIKeys],
  )
  const showCredentialPicker = site?.supports_multiple_api_keys === true && sortedAPIKeys.length > 0
  const showKeyGroups = site?.supports_multiple_api_keys === true && sortedAPIKeys.length > 1
  const credentialOptions = useMemo<MultiSelectOption[]>(
    () => sortedAPIKeys.map((apiKey) => ({
      value: apiKey.id,
      label: apiKey.name || apiKey.key || apiKey.id,
      description: apiKey.enabled ? (apiKey.group ?? apiKey.key) : t('models.keyDisabled'),
    })),
    [sortedAPIKeys, t],
  )
  const canCreate = Boolean(
    upstreamModelName.trim() &&
    canonicalModelId &&
    endpointTypes.length &&
    (!showCredentialPicker || siteCredentialIds.length),
  )
  const canonicalMap = useMemo(
    () => new Map((canonicalModelsQuery.data?.items ?? []).map((model) => [model.id, model])),
    [canonicalModelsQuery.data?.items],
  )
  const keyword = search.trim().toLowerCase()
  const keyGroups = useMemo(() => {
    if (!showKeyGroups) return []
    return sortedAPIKeys.map((apiKey) => {
      const modelsForKey = apiKeyModels(apiKey).flatMap((item) => {
        const name = item.name.trim()
        if (!name) return []
        if (keyword && !name.toLowerCase().includes(keyword) && !(matchSiteModel(items, item)?.display_name ?? '').toLowerCase().includes(keyword)) {
          return []
        }
        const siteModel = matchSiteModel(items, item)
        return [{
          name,
          enabled: item.enabled,
          siteModel,
          icon: siteModel ? siteModelIconInfo(siteModel, canonicalMap, site) : modelNameIconInfo(name),
        }]
      })
      return { apiKey, models: modelsForKey }
    }).filter((group) => group.models.length > 0)
  }, [canonicalMap, items, keyword, showKeyGroups, site, sortedAPIKeys])
  const visibleKeyModelItems = useMemo(
    () => keyGroups.flatMap((group) => group.models.map((model) => ({
      apiKeyId: group.apiKey.id,
      model: model.name,
      enabled: model.enabled,
      siteModelId: model.siteModel?.id,
    }))),
    [keyGroups],
  )
  const keyModelPendingId = updateKeyModelMutation.isPending
    ? `${updateKeyModelMutation.variables?.apiKeyId}:${updateKeyModelMutation.variables?.model}`
    : undefined
  const keyModelBulkPending = bulkUpdateKeyModelMutation.isPending
  const canBulkEnableKeyModels = visibleKeyModelItems.some((item) => !item.enabled)
  const canBulkDisableKeyModels = visibleKeyModelItems.some((item) => item.enabled)
  const nestedOpen = addOpen || Boolean(deleteTarget)
  const panelTitle = title ?? t('models.title')

  function resetAddForm() {
    setUpstreamModelName('')
    setCanonicalModelId('')
    setEndpointTypes(defaultManualEndpointTypes(site))
    setSiteCredentialIds(showCredentialPicker ? sortedAPIKeys.filter((apiKey) => apiKey.enabled).map((apiKey) => apiKey.id) : [])
    setEnabled(true)
  }

  function openAddForm() {
    setEndpointTypes(defaultManualEndpointTypes(site))
    setSiteCredentialIds(showCredentialPicker ? sortedAPIKeys.filter((apiKey) => apiKey.enabled).map((apiKey) => apiKey.id) : [])
    setEnabled(true)
    setAddOpen(true)
  }

  function handleShellOpenChange(nextOpen: boolean) {
    if (!nextOpen && !isMobile && nestedOpen) {
      closeNested()
      return
    }
    if (!nextOpen) {
      setManualManage(false)
      setSearch('')
      setCollapsedKeyIds({})
    }
    onOpenChange(nextOpen)
  }

  function toggleKeyGroup(apiKeyId: string) {
    setCollapsedKeyIds((current) => ({ ...current, [apiKeyId]: !current[apiKeyId] }))
  }

  function closeNested() {
    if (addOpen && !createMutation.isPending) {
      setAddOpen(false)
      resetAddForm()
      return
    }
    if (deleteTarget && !deleteMutation.isPending) setDeleteTarget(null)
  }

  const toolbarAction = (
    <div className="grid grid-cols-2 gap-2 sm:flex">
      <Button
        type="button"
        size="sm"
        variant={manualManageActive ? 'default' : 'secondary'}
        disabled={!hasManualModels || modelsQuery.isLoading}
        onClick={() => setManualManage((value) => !value)}
      >
        {manualManageActive ? t('models.manageManual.done') : t('models.manageManual.action')}
      </Button>
      <Button type="button" size="sm" variant="secondary" onClick={openAddForm}>
        {t('models.add.action')}
      </Button>
    </div>
  )

  const addAndDeleteDialogs = (
    <>
      <Dialog open={addOpen} onOpenChange={(nextOpen) => {
        setAddOpen(nextOpen)
        if (!nextOpen && !createMutation.isPending) resetAddForm()
      }}>
        <DialogContent
          overlayClassName="z-[60]"
          size="md"
          className="z-[60]"
          onOpenAutoFocus={(event) => event.preventDefault()}
        >
          <DialogHeader className="px-6 py-5">
            <DialogTitle>{t('models.add.action')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="space-y-5 px-6 py-6">
            <div className="space-y-2.5">
              <span className="block text-sm font-medium text-foreground">{t('models.add.upstreamName')}</span>
              <Input
                value={upstreamModelName}
                onChange={(event) => setUpstreamModelName(event.target.value)}
                placeholder={t('models.add.upstreamPlaceholder')}
              />
            </div>
            <div className="space-y-2.5">
              <span className="block text-sm font-medium text-foreground">{t('models.add.canonicalModel')}</span>
              <Select value={canonicalModelId} onValueChange={setCanonicalModelId}>
                <SelectTrigger>
                  <SelectValue placeholder={t('models.add.canonicalPlaceholder')} />
                </SelectTrigger>
                <SelectContent>
                  {activeCanonicalModels.map((model) => (
                    <SelectItem
                      key={model.id}
                      value={model.id}
                      textValue={`${model.model_key} ${model.provider}`}
                    >
                      <span className="truncate">{model.model_key}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2.5">
              <span className="block text-sm font-medium text-foreground">{t('models.add.endpointTypes')}</span>
              <MultiSelect
                value={endpointTypes}
                options={MANUAL_ENDPOINT_TYPE_OPTIONS}
                placeholder={t('models.add.endpointPlaceholder')}
                searchPlaceholder={t('models.add.searchPlaceholder')}
                emptyText={t('models.add.emptyOptions')}
                onChange={setEndpointTypes}
              />
            </div>
            {showCredentialPicker ? (
              <div className="space-y-2.5">
                <span className="block text-sm font-medium text-foreground">{t('models.add.apiKeys')}</span>
                <MultiSelect
                  value={siteCredentialIds}
                  options={credentialOptions}
                  placeholder={t('models.add.apiKeysPlaceholder')}
                  searchPlaceholder={t('models.add.searchPlaceholder')}
                  emptyText={t('models.add.emptyOptions')}
                  onChange={setSiteCredentialIds}
                />
              </div>
            ) : null}
            <label className="flex min-h-12 items-center justify-between gap-4 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-field))] px-4">
              <span className="text-sm font-medium text-foreground">{t('models.add.enabled')}</span>
              <Switch checked={enabled} onCheckedChange={setEnabled} aria-label={t('models.add.enabled')} />
            </label>
          </DialogBody>
          <DialogFooter
            className="gap-4 px-6 py-5"
            cancel={(
              <Button type="button" variant="ghost" onClick={() => { resetAddForm(); setAddOpen(false) }}>
                {t('models.add.cancel')}
              </Button>
            )}
            confirm={(
              <Button
                type="button"
                disabled={!canCreate || createMutation.isPending}
                onClick={() => createMutation.mutate()}
              >
                {createMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                {t('models.add.submit')}
              </Button>
            )}
          />
        </DialogContent>
      </Dialog>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(nextOpen) => {
        if (!nextOpen && !deleteMutation.isPending) setDeleteTarget(null)
      }}>
        <DialogContent
          overlayClassName="z-[60]"
          size="sm"
          className="z-[60]"
        >
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('models.delete.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pt-0">
            <DialogDescription className="mt-0">
              {deleteTarget
                ? t('models.delete.description', { name: deleteTarget.upstream_model_name || deleteTarget.display_name })
                : t('models.delete.descriptionDefault')}
            </DialogDescription>
          </DialogBody>
          <DialogFooter
            className="border-t-0 pt-2"
            cancel={(
              <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleteMutation.isPending}>
                {t('models.delete.cancel')}
              </Button>
            )}
            confirm={(
              <Button
                variant="destructive"
                disabled={deleteMutation.isPending || !deleteTarget}
                onClick={() => {
                  if (deleteTarget) deleteMutation.mutate({ modelId: deleteTarget.id })
                }}
              >
                {deleteMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                {t('models.delete.confirm')}
              </Button>
            )}
          />
        </DialogContent>
      </Dialog>
    </>
  )

  const modelsList = (
    <ModelsDraw
      key={site?.id ?? 'site-models'}
      open={open}
      title={panelTitle}
      items={dialogItems}
      loading={modelsQuery.isLoading}
      pendingItemId={updateMutation.isPending ? updateMutation.variables?.modelId : undefined}
      bulkPending={bulkUpdateMutation.isPending}
      toolbarAction={toolbarAction}
      shell={isMobile ? 'draw' : 'dialog'}
      dialogSize="md"
      dismissLocked={!isMobile && nestedOpen}
      onToggleItem={(item, nextEnabled) => {
        if (!item.id || item.id.startsWith('unmapped:')) return
        updateMutation.mutate({ modelId: item.id, enabled: nextEnabled })
      }}
      onBulkToggleItems={(bulkItems, nextEnabled) => {
        const modelIds = bulkItems
          .filter((item) => item.id && !item.id.startsWith('unmapped:') && item.enabled !== nextEnabled)
          .map((item) => item.id)
        if (modelIds.length) bulkUpdateMutation.mutate({ modelIds, enabled: nextEnabled })
      }}
      onOpenChange={handleShellOpenChange}
    />
  )

  if (isMobile || !showKeyGroups) {
    return (
      <>
        {modelsList}
        {addAndDeleteDialogs}
      </>
    )
  }

  return (
    <>
      <Dialog open={open} onOpenChange={handleShellOpenChange}>
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
            closeNested()
          }}
        >
          <DialogHeader>
            <DialogTitle>{panelTitle}</DialogTitle>
          </DialogHeader>
          <DialogBody className="flex min-h-0 flex-1 flex-col overflow-hidden">
            <div className="shrink-0 space-y-3 pb-4">
              <div className="relative min-w-0">
                <Search className="text-foreground/40 pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
                <Input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={t('components:modelsDraw.searchPlaceholder')}
                  className="pl-10"
                />
              </div>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <div className="grid grid-cols-2 gap-2 sm:flex">
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={keyModelBulkPending || Boolean(keyModelPendingId) || !canBulkEnableKeyModels}
                    onClick={() => {
                      const itemsToUpdate = visibleKeyModelItems.filter((item) => !item.enabled)
                      if (itemsToUpdate.length) bulkUpdateKeyModelMutation.mutate({ items: itemsToUpdate, enabled: true })
                    }}
                  >
                    {t('components:modelsDraw.enableAll')}
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={keyModelBulkPending || Boolean(keyModelPendingId) || !canBulkDisableKeyModels}
                    onClick={() => {
                      const itemsToUpdate = visibleKeyModelItems.filter((item) => item.enabled)
                      if (itemsToUpdate.length) bulkUpdateKeyModelMutation.mutate({ items: itemsToUpdate, enabled: false })
                    }}
                  >
                    {t('components:modelsDraw.disableAll')}
                  </Button>
                </div>
                <div className="sm:shrink-0">{toolbarAction}</div>
              </div>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              {modelsQuery.isLoading || apiKeysQuery.isLoading ? (
                <p className="py-10 text-center text-sm text-muted-soft">{t('components:modelsDraw.loading')}</p>
              ) : keyGroups.length ? (
                keyGroups.map((group) => {
                  const keyName = group.apiKey.name || t('apiKeys.defaultKey')
                  const collapsed = Boolean(collapsedKeyIds[group.apiKey.id])
                  return (
                    <section key={group.apiKey.id}>
                      <button
                        type="button"
                        className="sticky top-0 z-10 flex w-full items-center gap-2 bg-[hsl(var(--dialog-surface))] py-2.5 pl-3 pr-3 text-left backdrop-blur-[40px] backdrop-saturate-150"
                        aria-expanded={!collapsed}
                        aria-label={collapsed ? t('models.expandKey', { name: keyName }) : t('models.collapseKey', { name: keyName })}
                        onClick={() => toggleKeyGroup(group.apiKey.id)}
                      >
                        <span className="min-w-0 truncate font-medium text-foreground">{keyName}</span>
                        {group.apiKey.enabled ? null : (
                          <Badge variant="warning">{t('models.keyDisabled')}</Badge>
                        )}
                        {group.apiKey.group ? (
                          <Badge variant={siteAPIKeyGroupBadgeVariant(group.apiKey.group)}>
                            {group.apiKey.group}
                          </Badge>
                        ) : null}
                        <APIKeySyncBadge apiKey={group.apiKey} />
                        <ChevronDown
                          className={cn(
                            'ml-auto h-4 w-4 shrink-0 text-muted-soft transition-transform',
                            collapsed && '-rotate-90',
                          )}
                        />
                      </button>
                      {collapsed ? null : (
                        <table className="w-full table-fixed border-collapse text-left text-sm">
                          <tbody>
                            {group.models.map((model) => {
                              const pending = keyModelPendingId === `${group.apiKey.id}:${model.name}`
                              const deleting = Boolean(
                                model.siteModel
                                && deleteMutation.isPending
                                && deleteMutation.variables?.modelId === model.siteModel.id,
                              )
                              return (
                                <tr key={`${group.apiKey.id}:${model.name}`} className="border-t border-[hsl(var(--glass-divider))] first:border-t-0">
                                  <td className="min-w-0 px-1 py-3">
                                    <div className="flex min-w-0 items-center gap-2">
                                      {manualManageActive && model.siteModel && isManualSiteModel(model.siteModel) ? (
                                        <Button
                                          type="button"
                                          size="icon"
                                          variant="ghost"
                                          className="h-6 w-6 shrink-0 text-red-500 hover:bg-red-500/10 hover:text-red-400"
                                          disabled={deleteMutation.isPending}
                                          title={t('models.delete.action')}
                                          aria-label={t('models.delete.ariaLabel', { name: model.name })}
                                          onClick={() => setDeleteTarget(model.siteModel ?? null)}
                                        >
                                          {deleting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
                                        </Button>
                                      ) : null}
                                      <BrandMark
                                        iconPath={model.icon.iconPath}
                                        label={model.icon.label}
                                        fallback={model.icon.fallback}
                                        fallbackText={model.icon.fallbackText}
                                        size="sm"
                                      />
                                      <button
                                        type="button"
                                        className="block min-w-0 max-w-full cursor-pointer truncate text-left font-medium text-foreground"
                                        title={t('components:modelsDraw.copyName')}
                                        onClick={() => copyToClipboard(model.name, t('components:modelsDraw.copied'), t('components:modelsDraw.copyFailed'))}
                                      >
                                        {model.name}
                                      </button>
                                    </div>
                                  </td>
                                  <td className="w-[7.5rem] px-1 py-3 text-right">
                                    <Switch
                                      checked={model.enabled}
                                      disabled={keyModelBulkPending || pending}
                                      aria-label={t('components:modelsDraw.toggleLabel', { name: model.name })}
                                      onCheckedChange={(checked) => {
                                        updateKeyModelMutation.mutate({
                                          apiKeyId: group.apiKey.id,
                                          model: model.name,
                                          enabled: checked,
                                          siteModelId: model.siteModel?.id,
                                        })
                                      }}
                                    />
                                  </td>
                                </tr>
                              )
                            })}
                          </tbody>
                        </table>
                      )}
                    </section>
                  )
                })
              ) : (
                <p className="py-10 text-center text-sm text-muted-soft">{t('components:modelsDraw.noModels')}</p>
              )}
            </div>
          </DialogBody>
        </DialogContent>
      </Dialog>
      {addAndDeleteDialogs}
    </>
  )
}

function APIKeySyncBadge({ apiKey }: { apiKey: SiteAPIKey }) {
  const { t } = useTranslation('sites')
  const status = apiKey.sync_status?.trim().toLowerCase()
  if (!status) return null
  const variant = apiKeySyncBadgeVariant(status)
  if (!variant) return null
  return <Badge variant={variant}>{t(`apiKeys.sync.${syncBadgeKey(status)}`)}</Badge>
}

function apiKeySyncBadgeVariant(status: string): 'success' | 'warning' | 'info' | 'outline' | null {
  switch (status) {
    case 'synced':
      return 'success'
    case 'failed':
    case 'partial':
    case 'stale':
      return 'warning'
    case 'pending':
    case 'syncing':
      return 'info'
    default:
      return 'outline'
  }
}

function syncBadgeKey(status: string) {
  switch (status) {
    case 'synced':
    case 'failed':
    case 'partial':
    case 'stale':
    case 'pending':
    case 'syncing':
      return status
    default:
      return 'unknown'
  }
}

function matchSiteModel(models: SiteModel[], item: SiteAPIKeyModel) {
  if (item.site_model_id) {
    const matched = models.find((model) => model.id === item.site_model_id)
    if (matched) return matched
  }
  const name = item.name.trim()
  return models.find((model) => model.upstream_model_name === name || model.display_name === name)
}

function replaceModels(current: SiteModel[], updated: SiteModel[]) {
  if (!current.length) return updated
  const updatedById = new Map(updated.map((model) => [model.id, model]))
  return current.map((model) => updatedById.get(model.id) ?? model)
}

function upsertModel(current: SiteModel[], model: SiteModel) {
  if (!current.some((item) => item.id === model.id)) {
    return [...current, model]
  }
  return current.map((item) => item.id === model.id ? model : item)
}

function isManualSiteModel(model: SiteModel) {
  return model.capabilities?.manual === true || model.capabilities?.source === 'manual'
}

const MANUAL_ENDPOINT_TYPES = [
  'openai',
  'openai-response',
  'openai-image',
  'openai-embedding',
  'openai-audio-speech',
  'anthropic-messages',
  'google-gemini',
]

const MANUAL_ENDPOINT_TYPE_OPTIONS: MultiSelectOption[] = MANUAL_ENDPOINT_TYPES.map((value) => ({
  value,
  label: formatEndpointTypeLabel(value),
  description: value,
}))

function defaultManualEndpointTypes(site: Site | null) {
  switch (site?.site_type) {
    case 'anthropic':
    case 'claude_code':
      return ['anthropic-messages']
    case 'google':
      return ['google-gemini']
    case 'codex':
    case 'antigravity':
      return ['openai-response']
    default:
      return ['openai']
  }
}
