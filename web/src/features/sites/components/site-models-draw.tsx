import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle, Trash2 } from 'lucide-react'
import { ModelsDraw } from '@/components/common/models-draw'
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
  listSiteModels,
  sitesQueryKeys,
  updateSiteModelStatus,
  updateSiteModelsStatus,
  type Site,
  type SiteAPIKey,
  type SiteModel,
} from '@/features/sites/api/sites'
import { buildDisplayModelItems } from '@/features/sites/lib/site-model-items'
import { formatEndpointTypeLabel } from '@/features/models/lib/model-helpers'

const EMPTY_MODELS: SiteModel[] = []

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
  const { t } = useTranslation('sites')
  const open = Boolean(site)
  const [addOpen, setAddOpen] = useState(false)
  const [upstreamModelName, setUpstreamModelName] = useState('')
  const [canonicalModelId, setCanonicalModelId] = useState('')
  const [endpointTypes, setEndpointTypes] = useState<string[]>([])
  const [siteCredentialIds, setSiteCredentialIds] = useState<string[]>([])
  const [enabled, setEnabled] = useState(true)
  const [deleteTarget, setDeleteTarget] = useState<SiteModel | null>(null)
  const [manualManage, setManualManage] = useState(false)
  const modelsQuery = useQuery({
    queryKey: site ? sitesQueryKeys.models(site.id) : [...sitesQueryKeys.all, 'models', 'none'],
    queryFn: async () => {
      if (!site) return { items: EMPTY_MODELS }
      return listSiteModels(site.id)
    },
    enabled: open,
    initialData: models.length ? { items: models } : undefined,
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
    onSuccess: (result) => {
      if (!site) return
      queryClient.setQueryData(sitesQueryKeys.models(site.id), (current: { items?: SiteModel[] } | undefined) => ({
        items: upsertModel(current?.items ?? [], result.model),
      }))
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
    onSuccess: (_, variables) => {
      if (!site) return
      queryClient.setQueryData(sitesQueryKeys.models(site.id), (current: { items?: SiteModel[] } | undefined) => ({
        items: (current?.items ?? []).filter((model) => model.id !== variables.modelId),
      }))
      setDeleteTarget(null)
      onModelsChanged?.()
      toast.success(t('models.toast.deleteSuccess'))
    },
    onError: (error) => toast.error(t('models.toast.deleteFailed'), { description: error.message }),
  })

  const items = modelsQuery.data?.items ?? EMPTY_MODELS
  const modelsByID = useMemo(() => new Map(items.map((model) => [model.id, model])), [items])
  const hasManualModels = useMemo(() => items.some(isManualSiteModel), [items])
  const manualManageActive = manualManage && hasManualModels && open
  const dialogItems = useMemo(() => buildDisplayModelItems(site, items, apiKeys, canonicalModelsQuery.data?.items).map((item) => {
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
  }), [apiKeys, canonicalModelsQuery.data?.items, deleteMutation.isPending, deleteMutation.variables?.modelId, items, manualManageActive, modelsByID, site, t])
  const activeCanonicalModels = useMemo(
    () => (canonicalModelsQuery.data?.items ?? []).filter((model) => model.status === 'active'),
    [canonicalModelsQuery.data?.items],
  )
  const enabledAPIKeys = useMemo(() => apiKeys.filter((apiKey) => apiKey.enabled), [apiKeys])
  const credentialOptions = useMemo<MultiSelectOption[]>(
    () => enabledAPIKeys.map((apiKey) => ({
      value: apiKey.id,
      label: apiKey.name || apiKey.key || apiKey.id,
      description: apiKey.group ?? apiKey.key,
    })),
    [enabledAPIKeys],
  )
  const isNewAPI = site?.site_type === 'newapi'
  const canCreate = Boolean(
    upstreamModelName.trim() &&
    canonicalModelId &&
    endpointTypes.length &&
    (!isNewAPI || siteCredentialIds.length),
  )

  function resetAddForm() {
    setUpstreamModelName('')
    setCanonicalModelId('')
    setEndpointTypes(defaultManualEndpointTypes(site))
    setSiteCredentialIds(isNewAPI ? enabledAPIKeys.map((apiKey) => apiKey.id) : [])
    setEnabled(true)
  }

  function openAddForm() {
    setEndpointTypes(defaultManualEndpointTypes(site))
    setSiteCredentialIds(isNewAPI ? enabledAPIKeys.map((apiKey) => apiKey.id) : [])
    setEnabled(true)
    setAddOpen(true)
  }

  return (
    <>
      <ModelsDraw
        key={site?.id ?? 'site-models'}
        open={open}
        title={title ?? t('models.title')}
        items={dialogItems}
        loading={modelsQuery.isLoading}
        pendingItemId={updateMutation.isPending ? updateMutation.variables?.modelId : undefined}
        bulkPending={bulkUpdateMutation.isPending}
        toolbarAction={
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
        }
        onToggleItem={(item, enabled) => {
          if (!item.id || item.id.startsWith('unmapped:')) return
          updateMutation.mutate({ modelId: item.id, enabled })
        }}
        onBulkToggleItems={(bulkItems, enabled) => {
          const modelIds = bulkItems
            .filter((item) => item.id && !item.id.startsWith('unmapped:') && item.enabled !== enabled)
            .map((item) => item.id)
          if (modelIds.length) bulkUpdateMutation.mutate({ modelIds, enabled })
        }}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setManualManage(false)
          onOpenChange(nextOpen)
        }}
      />
      <Dialog open={addOpen} onOpenChange={(nextOpen) => {
        setAddOpen(nextOpen)
        if (!nextOpen && !createMutation.isPending) resetAddForm()
      }}>
        <DialogContent className="w-[min(92vw,560px)] overflow-visible rounded-3xl">
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
            {isNewAPI ? (
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
          <DialogFooter className="gap-4 px-6 py-5">
            <Button type="button" variant="ghost" onClick={() => { resetAddForm(); setAddOpen(false) }}>
              {t('models.add.cancel')}
            </Button>
            <Button
              type="button"
              disabled={!canCreate || createMutation.isPending}
              onClick={() => createMutation.mutate()}
            >
              {createMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {t('models.add.submit')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={Boolean(deleteTarget)} onOpenChange={(nextOpen) => {
        if (!nextOpen && !deleteMutation.isPending) setDeleteTarget(null)
      }}>
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
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
          <DialogFooter className="border-t-0 pt-2">
            <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleteMutation.isPending}>
              {t('models.delete.cancel')}
            </Button>
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
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
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
