import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle, Plus, Search, Trash2 } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Draw,
  DrawBody,
  DrawContent,
  DrawFooter,
  DrawHeader,
  DrawTitle,
} from '@/components/ui/draw'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import { useMobileLayout } from '@/hooks/use-media-query'
import {
  getProviderCatalogEntry,
} from '@/lib/brands'
import {
  archiveCanonicalModel,
  bindSiteModelCanonical,
  createCanonicalModel,
  createCanonicalModelAlias,
  deleteCanonicalModelAlias,
  getCanonicalModelMatrix,
  listCanonicalModels,
  modelsQueryKeys,
  type CanonicalModelItem,
  type CanonicalModelMatrixRow,
  type Site,
  type SiteModel,
  buildUnmatchedSiteModels,
  canonicalModelSearchText,
  isManualCreatedCanonicalModel,
  replaceSiteModelInMap,
  type CreateCanonicalModelDraft,
  type UnmatchedSiteModel,
} from '../api/models'
import { formatMatchSource } from '../lib/model-helpers'
import { sitesQueryKeys } from '@/features/sites/api/sites'

type ModelCatalogSurfaceProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  sites: Site[]
  modelsMap: Record<string, SiteModel[]>
}

function toDefaultDisplayName(modelKey: string): string {
  return Array.from(modelKey)
    .map((char, index, chars) => (
      char === '.' && /\p{N}/u.test(chars[index - 1] ?? '') && /\p{N}/u.test(chars[index + 1] ?? '')
        ? char
        : char.replace(/[^\p{L}\p{N}]/gu, ' ')
    ))
    .join('')
    .replace(/\s+/g, ' ')
    .trim()
    .toUpperCase()
}

function shouldSyncDisplayName(current: CreateCanonicalModelDraft): boolean {
  const currentDisplayName = current.displayName.trim()

  return !currentDisplayName || currentDisplayName === toDefaultDisplayName(current.modelKey)
}

export function ModelCatalogSurface({
  open,
  onOpenChange,
  sites,
  modelsMap,
}: ModelCatalogSurfaceProps) {
  const { t } = useTranslation('models')
  const isMobile = useMobileLayout()
  const queryClient = useQueryClient()
  const [modelSearch, setModelSearch] = useState('')
  const [unmatchedSearch, setUnmatchedSearch] = useState('')
  const [selectedModelId, setSelectedModelId] = useState<string | null>(null)
  const [aliasInput, setAliasInput] = useState('')
  const [createDraft, setCreateDraft] = useState<CreateCanonicalModelDraft | null>(null)
  const [archiveTarget, setArchiveTarget] = useState<CanonicalModelItem | null>(null)

  const canonicalModelsQuery = useQuery({
    queryKey: modelsQueryKeys.canonical(),
    queryFn: listCanonicalModels,
    enabled: open,
  })
  const activeCanonicalModels = useMemo(
    () => (canonicalModelsQuery.data?.items ?? [])
      .filter((model) => model.status === 'active')
      .filter((model) => model.site_model_count > 0 || isManualCreatedCanonicalModel(model)),
    [canonicalModelsQuery.data?.items],
  )
  const filteredCanonicalModels = useMemo(() => {
    const keyword = modelSearch.trim().toLowerCase()
    if (!keyword) return activeCanonicalModels

    return activeCanonicalModels.filter((model) => canonicalModelSearchText(model).includes(keyword))
  }, [activeCanonicalModels, modelSearch])
  const selectedModel = selectedModelId
    ? activeCanonicalModels.find((model) => model.id === selectedModelId) ?? activeCanonicalModels[0] ?? null
    : activeCanonicalModels[0] ?? null
  const visibleSelectedModel = isMobile
    ? filteredCanonicalModels.find((model) => model.id === selectedModel?.id) ?? filteredCanonicalModels[0] ?? null
    : selectedModel
  const activeModelId = visibleSelectedModel?.id ?? ''
  const matrixQuery = useQuery({
    queryKey: activeModelId ? modelsQueryKeys.canonicalMatrix(activeModelId) : [...modelsQueryKeys.canonical(), 'matrix', 'none'],
    queryFn: () => getCanonicalModelMatrix(activeModelId),
    enabled: open && Boolean(activeModelId),
  })
  const canCreateSearchedModel = useMemo(() => {
    const keyword = modelSearch.trim().toLowerCase()
    if (!keyword) return false

    return !activeCanonicalModels.some((model) =>
      model.model_key.toLowerCase() === keyword ||
      model.display_name.toLowerCase() === keyword ||
      model.aliases.some((alias) => alias.alias.toLowerCase() === keyword),
    )
  }, [activeCanonicalModels, modelSearch])
  const unmatchedModels = useMemo(
    () => buildUnmatchedSiteModels(sites, modelsMap, unmatchedSearch),
    [modelsMap, sites, unmatchedSearch],
  )

  const addAliasMutation = useMutation({
    mutationFn: ({ modelId, alias }: { modelId: string; alias: string }) => createCanonicalModelAlias(modelId, alias),
    onSuccess: async (_result, variables) => {
      setAliasInput('')
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonical() })
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonicalMatrix(variables.modelId) })
      toast.success(t('catalog.toast.aliasAdded'))
    },
    onError: (error) => {
      toast.error(t('catalog.toast.aliasAddFailed'), { description: error.message })
    },
  })
  const createModelMutation = useMutation({
    mutationFn: async (draft: CreateCanonicalModelDraft) => {
      const created = await createCanonicalModel({
        modelKey: draft.modelKey,
        displayName: draft.displayName || draft.modelKey,
      })

      if (draft.target) {
        await createCanonicalModelAlias(created.model.id, draft.target.model.upstream_model_name)
        const bound = await bindSiteModelCanonical(draft.target.model.id, created.model.id)

        return {
          model: created.model,
          boundModel: bound.model,
        }
      }

      return {
        model: created.model,
        boundModel: null,
      }
    },
    onSuccess: async (result) => {
      if (result.boundModel) {
        queryClient.setQueriesData<Record<string, SiteModel[]>>(
          { queryKey: [...sitesQueryKeys.all, 'models-marketplace'] },
          (current) => replaceSiteModelInMap(current, result.boundModel!),
        )
      }

      setSelectedModelId(result.model.id)
      setCreateDraft(null)
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonical() })
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonicalMatrix(result.model.id) })
      toast.success(result.boundModel ? t('catalog.toast.modelCreatedBound') : t('catalog.toast.modelCreated'))
    },
    onError: (error) => {
      toast.error(t('catalog.toast.modelCreateFailed'), { description: error.message })
    },
  })
  const deleteAliasMutation = useMutation({
    mutationFn: ({ modelId, aliasId }: { modelId: string; aliasId: string }) =>
      deleteCanonicalModelAlias(modelId, aliasId),
    onSuccess: async (_result, variables) => {
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonical() })
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonicalMatrix(variables.modelId) })
      toast.success(t('catalog.toast.aliasDeleted'))
    },
    onError: (error) => {
      toast.error(t('catalog.toast.aliasDeleteFailed'), { description: error.message })
    },
  })
  const archiveModelMutation = useMutation({
    mutationFn: (modelId: string) => archiveCanonicalModel(modelId),
    onSuccess: async (_result, modelId) => {
      if (selectedModelId === modelId) {
        setSelectedModelId(null)
      }
      setArchiveTarget(null)
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonical() })
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonicalMatrix(modelId) })
      toast.success(t('catalog.toast.modelArchived'))
    },
    onError: (error) => {
      toast.error(t('catalog.toast.modelArchiveFailed'), { description: error.message })
    },
  })
  const bindMutation = useMutation({
    mutationFn: ({ siteModelId, canonicalModelId }: { siteModelId: string; canonicalModelId: string | null }) =>
      bindSiteModelCanonical(siteModelId, canonicalModelId),
    onSuccess: async (result, variables) => {
      queryClient.setQueriesData<Record<string, SiteModel[]>>(
        { queryKey: [...sitesQueryKeys.all, 'models-marketplace'] },
        (current) => replaceSiteModelInMap(current, result.model),
      )
      await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonical() })
      if (variables.canonicalModelId) {
        await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonicalMatrix(variables.canonicalModelId) })
      }
      if (activeModelId && activeModelId !== variables.canonicalModelId) {
        await queryClient.invalidateQueries({ queryKey: modelsQueryKeys.canonicalMatrix(activeModelId) })
      }
      toast.success(variables.canonicalModelId ? t('catalog.toast.modelBound') : t('catalog.toast.modelUnbound'))
    },
    onError: (error) => {
      toast.error(t('catalog.toast.modelBindFailed'), { description: error.message })
    },
  })

  function handleAddAlias() {
    const alias = aliasInput.trim()
    if (!visibleSelectedModel || !alias) return

    addAliasMutation.mutate({
      modelId: visibleSelectedModel.id,
      alias,
    })
  }

  function openCreateModelSurface(target: UnmatchedSiteModel) {
    setCreateDraft({
      modelKey: '',
      displayName: '',
      target,
    })
  }

  function openCreateEmptyModelSurface() {
    const value = modelSearch.trim()
    setCreateDraft({
      modelKey: value,
      displayName: toDefaultDisplayName(value),
    })
  }

  function handleCreateDraftModelKeyChange(modelKey: string) {
    setCreateDraft((current) => {
      if (!current) return current

      return {
        ...current,
        modelKey,
        displayName: shouldSyncDisplayName(current)
          ? toDefaultDisplayName(modelKey)
          : current.displayName,
      }
    })
  }

  function handleCreateModel() {
    if (!createDraft?.modelKey.trim()) return

    createModelMutation.mutate({
      ...createDraft,
      modelKey: createDraft.modelKey.trim(),
      displayName: createDraft.displayName.trim(),
    })
  }

  const unmatchedSection = (
    <div className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-soft))] p-4">
      <div className="mb-3 flex items-center justify-between gap-3">
        <h4 className="text-sm font-medium text-foreground">{t('catalog.unmatched')}</h4>
        <Input
          value={unmatchedSearch}
          onChange={(event) => setUnmatchedSearch(event.target.value)}
          placeholder={t('catalog.searchUnmatched')}
          className="h-9 max-w-[280px]"
        />
      </div>

      <div className="max-h-56 space-y-2 overflow-y-auto pr-1">
        {unmatchedModels.length ? (
          unmatchedModels.map((item) => {
            const binding = bindMutation.isPending && bindMutation.variables?.siteModelId === item.model.id

            return (
              <div
                key={item.model.id}
                className="flex items-center justify-between gap-3 rounded-md bg-[hsl(var(--surface-subtle))] px-3 py-2"
              >
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium text-foreground">
                    {item.model.display_name || item.model.upstream_model_name}
                  </div>
                  <div className="text-muted-soft mt-1 truncate text-xs">{item.siteName}</div>
                </div>
                <div className="ml-auto flex shrink-0 items-center justify-end gap-2">
                  {visibleSelectedModel ? (
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => bindMutation.mutate({
                        siteModelId: item.model.id,
                        canonicalModelId: visibleSelectedModel.id,
                      })}
                      disabled={binding}
                    >
                      {binding ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                      {t('catalog.bind')}
                    </Button>
                  ) : null}
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => openCreateModelSurface(item)}
                    disabled={createModelMutation.isPending}
                  >
                    {t('catalog.create')}
                  </Button>
                </div>
              </div>
            )
          })
        ) : (
          <div className="text-muted-soft py-8 text-center text-sm">{t('catalog.noUnmatched')}</div>
        )}
      </div>
    </div>
  )

  const mobileUnmatchedSection = (
    <div className="space-y-3 rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-soft))] p-3">
      <div className="flex items-center justify-between gap-3">
        <h4 className="text-sm font-semibold text-foreground">{t('catalog.unmatched')}</h4>
        <Badge variant="secondary" className="border-transparent">{unmatchedModels.length}</Badge>
      </div>
      <Input
        value={unmatchedSearch}
        onChange={(event) => setUnmatchedSearch(event.target.value)}
        placeholder={t('catalog.searchUnmatched')}
        className="h-9"
      />

      <div className="space-y-2">
        {unmatchedModels.length ? (
          unmatchedModels.map((item) => {
            const binding = bindMutation.isPending && bindMutation.variables?.siteModelId === item.model.id

            return (
              <div
                key={item.model.id}
                className="rounded-xl bg-[hsl(var(--surface-subtle))] px-3 py-2.5"
              >
                <div className="min-w-0">
                  <div className="line-clamp-2 break-all text-sm font-medium text-foreground">
                    {item.model.display_name || item.model.upstream_model_name}
                  </div>
                  <div className="text-muted-soft mt-1 truncate text-xs">{item.siteName}</div>
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  {visibleSelectedModel ? (
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() => bindMutation.mutate({
                        siteModelId: item.model.id,
                        canonicalModelId: visibleSelectedModel.id,
                      })}
                      disabled={binding}
                    >
                      {binding ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                      {t('catalog.bind')}
                    </Button>
                  ) : null}
                  <Button
                    size="sm"
                    variant="outline"
                    className={visibleSelectedModel ? undefined : 'col-span-2'}
                    onClick={() => openCreateModelSurface(item)}
                    disabled={createModelMutation.isPending}
                  >
                    {t('catalog.create')}
                  </Button>
                </div>
              </div>
            )
          })
        ) : (
          <div className="text-muted-soft py-7 text-center text-sm">{t('catalog.noUnmatched')}</div>
        )}
      </div>
    </div>
  )

  const createModelSurface = isMobile ? (
    <Draw
      open={Boolean(createDraft)}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !createModelMutation.isPending) {
          setCreateDraft(null)
        }
      }}
    >
      <DrawContent side="bottom">
        <DrawHeader>
          <DrawTitle>{t('catalog.createSurface.title')}</DrawTitle>
        </DrawHeader>
        <DrawBody className="space-y-4">
          <div className="space-y-2">
            <span className="block text-sm font-medium">{t('catalog.createSurface.modelKey')}</span>
            <Input
              aria-label={t('catalog.createSurface.modelKey')}
              value={createDraft?.modelKey ?? ''}
              onChange={(event) => handleCreateDraftModelKeyChange(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  handleCreateModel()
                }
              }}
            />
          </div>
          <div className="space-y-2">
            <span className="block text-sm font-medium">{t('catalog.createSurface.displayName')}</span>
            <Input
              aria-label={t('catalog.createSurface.displayName')}
              value={createDraft?.displayName ?? ''}
              onChange={(event) => {
                setCreateDraft((current) => current ? { ...current, displayName: event.target.value } : current)
              }}
            />
          </div>
          {createDraft?.target ? (
            <div className="rounded-xl bg-[hsl(var(--surface-subtle))] px-3 py-2 text-sm text-muted-soft">
              {createDraft.target.model.display_name || createDraft.target.model.upstream_model_name}
            </div>
          ) : null}
        </DrawBody>
        <DrawFooter className="grid grid-cols-2 gap-2">
          <Button
            variant="ghost"
            onClick={() => setCreateDraft(null)}
            disabled={createModelMutation.isPending}
          >
            {t('catalog.createSurface.cancel')}
          </Button>
          <Button
            onClick={handleCreateModel}
            disabled={!createDraft?.modelKey.trim() || createModelMutation.isPending}
          >
            {createModelMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {t('catalog.createSurface.save')}
          </Button>
        </DrawFooter>
      </DrawContent>
    </Draw>
  ) : null

  const archiveConfirmDialog = (
    <Dialog
      open={Boolean(archiveTarget)}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !archiveModelMutation.isPending) {
          setArchiveTarget(null)
        }
      }}
    >
      <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
        <DialogHeader className="border-b-0 pb-2">
          <DialogTitle>{t('catalog.archiveDialog.title')}</DialogTitle>
        </DialogHeader>
        <DialogBody className="pt-0">
          <DialogDescription className="mt-0">
            {archiveTarget
              ? t('catalog.archiveDialog.confirm', { model: archiveTarget.display_name })
              : t('catalog.archiveDialog.confirmDefault')}
          </DialogDescription>
        </DialogBody>
        <DialogFooter className="border-t-0 pt-2">
          <Button
            variant="outline"
            onClick={() => setArchiveTarget(null)}
            disabled={archiveModelMutation.isPending}
          >
            {t('catalog.archiveDialog.cancel')}
          </Button>
          <Button
            variant="destructive"
            onClick={() => {
              if (archiveTarget) {
                archiveModelMutation.mutate(archiveTarget.id)
              }
            }}
            disabled={!archiveTarget || archiveModelMutation.isPending}
          >
            {archiveModelMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {t('catalog.archiveDialog.confirmAction')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )

  if (isMobile) {
    return (
      <>
        <Draw open={open} onOpenChange={onOpenChange}>
          <DrawContent
            side="bottom"
            size="wide"
            className="max-h-[92dvh]"
            onOpenAutoFocus={(event) => event.preventDefault()}
          >
            <DrawHeader className="pb-3">
              <DrawTitle>{t('catalog.title')}</DrawTitle>
            </DrawHeader>
            <DrawBody className="space-y-3 px-4 py-4">
              <div className="sticky top-0 z-10 -mx-1 space-y-2 rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.58)] p-2 shadow-[0_14px_28px_rgba(0,0,0,0.10)] backdrop-blur-[28px] backdrop-saturate-150">
                <label className="relative block">
                  <Search className="text-muted-soft pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
                  <Input
                    value={modelSearch}
                    onChange={(event) => setModelSearch(event.target.value)}
                    placeholder={t('catalog.searchStandard')}
                    className="h-10 pl-9 pr-11"
                  />
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    className="absolute right-1 top-1/2 h-8 w-8 !-translate-y-1/2 hover:!-translate-y-1/2 active:!-translate-y-1/2"
                    onClick={openCreateEmptyModelSurface}
                    disabled={!canCreateSearchedModel || createModelMutation.isPending}
                    aria-label={t('catalog.createStandardLabel')}
                  >
                    <Plus className="h-4 w-4" />
                  </Button>
                </label>
                <div className="max-h-36 space-y-1.5 overflow-y-auto pr-1">
                  {canonicalModelsQuery.isLoading ? (
                    <div className="space-y-1.5">
                      <Skeleton className="h-10 w-full rounded-md" />
                      <Skeleton className="h-10 w-full rounded-md" />
                    </div>
                  ) : filteredCanonicalModels.length ? (
                    filteredCanonicalModels.map((model) => {
                      const active = visibleSelectedModel?.id === model.id

                      return (
                        <button
                          key={model.id}
                          type="button"
                          onClick={() => setSelectedModelId(model.id)}
                          className={cn(
                            'flex h-10 w-full items-center justify-between gap-3 rounded-md px-3 text-left transition-colors',
                            active
                              ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                              : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                          )}
                        >
                          <span className="min-w-0 truncate text-sm font-medium" title={model.display_name}>
                            {model.display_name}
                          </span>
                          <span className="shrink-0 rounded-full bg-[hsl(var(--surface-subtle))] px-2 py-0.5 text-xs text-muted-soft">
                            {model.site_model_count}
                          </span>
                        </button>
                      )
                    })
                  ) : (
                    <div className="text-muted-soft px-3 py-2 text-center text-sm">{t('catalog.noStandardModels')}</div>
                  )}
                </div>
              </div>

              {canonicalModelsQuery.isLoading ? (
                <CatalogSkeleton />
              ) : visibleSelectedModel ? (
                (() => {
                  const model = visibleSelectedModel
                  const providerName = getProviderCatalogEntry(model.provider)?.name ?? model.provider

                  return (
                    <article
                      key={model.id}
                      className={cn(
                        'overflow-hidden rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-soft))]',
                        'shadow-[0_18px_34px_rgba(0,0,0,0.10)]',
                      )}
                    >
                      <div className="flex w-full items-start justify-between gap-3 px-3.5 py-3 text-left">
                        <div className="min-w-0">
                          <div className="line-clamp-2 break-all text-sm font-semibold text-foreground">
                            {model.display_name}
                          </div>
                          <div className="text-muted-soft mt-1 line-clamp-1 break-all text-xs">
                            {model.model_key}
                          </div>
                        </div>
                        <div className="flex shrink-0 flex-col items-end gap-1.5">
                          <Badge variant="secondary" className="border-transparent">{providerName}</Badge>
                          <div className="flex items-center gap-1.5">
                            <span className="rounded-full bg-[hsl(var(--surface-subtle))] px-2 py-0.5 text-xs text-muted-soft">
                              {model.site_model_count}
                            </span>
                            {isManualCreatedCanonicalModel(model) && model.site_model_count === 0 ? (
                              <Button
                                type="button"
                                size="icon"
                                variant="ghost"
                                className="h-7 w-7"
                                onClick={() => setArchiveTarget(model)}
                                disabled={archiveModelMutation.isPending && archiveModelMutation.variables === model.id}
                                aria-label={t('catalog.archiveModelLabel', { model: model.display_name })}
                              >
                                {archiveModelMutation.isPending && archiveModelMutation.variables === model.id
                                  ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                                  : <Trash2 className="h-3.5 w-3.5" />}
                              </Button>
                            ) : null}
                          </div>
                        </div>
                      </div>

                      <div className="space-y-3 border-t border-[hsl(var(--glass-divider))] px-3.5 pb-3.5 pt-3">
                        <div className="space-y-3 rounded-2xl bg-[hsl(var(--surface-subtle))] p-3">
                          <div className="flex items-center justify-between gap-3">
                            <h4 className="text-sm font-semibold text-foreground">{t('catalog.alias')}</h4>
                            <Badge variant="secondary" className="border-transparent">
                              {model.aliases.length}
                            </Badge>
                          </div>
                          <div className="flex gap-2">
                            <Input
                              value={aliasInput}
                              onChange={(event) => setAliasInput(event.target.value)}
                              onKeyDown={(event) => {
                                if (event.key === 'Enter') {
                                  handleAddAlias()
                                }
                              }}
                              placeholder={t('catalog.addAliasPlaceholder')}
                              className="h-9 min-w-0"
                            />
                            <Button
                              size="sm"
                              onClick={handleAddAlias}
                              disabled={!aliasInput.trim() || addAliasMutation.isPending}
                            >
                              {addAliasMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                              {t('catalog.addAlias')}
                            </Button>
                          </div>
                          <div className="flex flex-wrap gap-2">
                            {model.aliases.length ? (
                              model.aliases.map((alias) => {
                                const deleting = deleteAliasMutation.isPending &&
                                  deleteAliasMutation.variables?.aliasId === alias.id

                                return (
                                  <span
                                    key={alias.id}
                                    className="inline-flex max-w-full items-center gap-1.5 rounded-full bg-[hsl(var(--surface-soft))] px-2.5 py-1 text-xs text-foreground"
                                  >
                                    <span className="truncate">{alias.alias}</span>
                                    <button
                                      type="button"
                                      className="text-muted-soft hover:text-foreground"
                                      onClick={() => deleteAliasMutation.mutate({ modelId: model.id, aliasId: alias.id })}
                                      disabled={deleting}
                                      aria-label={t('catalog.deleteAliasLabel', { alias: alias.alias })}
                                    >
                                      {deleting ? <LoaderCircle className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
                                    </button>
                                  </span>
                                )
                              })
                            ) : (
                              <span className="text-muted-soft text-sm">{t('catalog.noAliases')}</span>
                            )}
                          </div>
                        </div>

                        <MobileCatalogMatrixList
                          rows={matrixQuery.data?.items ?? []}
                          loading={matrixQuery.isLoading}
                          pendingSiteModelId={bindMutation.isPending ? bindMutation.variables?.siteModelId : undefined}
                          onUnbind={(siteModelId) => bindMutation.mutate({ siteModelId, canonicalModelId: null })}
                          t={t}
                        />

                        {mobileUnmatchedSection}
                      </div>
                    </article>
                  )
                })()
              ) : (
                <div className="text-muted-soft px-3 py-8 text-center text-sm">{t('catalog.noStandardModels')}</div>
              )}
            </DrawBody>
          </DrawContent>
        </Draw>

        {createModelSurface}
        {archiveConfirmDialog}
      </>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(94vw,1180px)] overflow-hidden">
        <DialogHeader>
          <DialogTitle>{t('catalog.title')}</DialogTitle>
        </DialogHeader>
        <DialogBody className="grid max-h-[72vh] min-h-[560px] grid-cols-[300px_minmax(0,1fr)] gap-5 overflow-hidden">
          <aside className="flex min-h-0 flex-col gap-3">
            <div className="relative shrink-0">
              <Search className="text-muted-soft pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
              <Input
                value={modelSearch}
                onChange={(event) => setModelSearch(event.target.value)}
                placeholder={t('catalog.searchStandard')}
                className="pl-9 pr-11"
              />
              <Button
                type="button"
                size="icon"
                variant="ghost"
                className="absolute right-1 top-1/2 h-8 w-8 !-translate-y-1/2 hover:!-translate-y-1/2 active:!-translate-y-1/2"
                onClick={openCreateEmptyModelSurface}
                disabled={!canCreateSearchedModel || createModelMutation.isPending}
                aria-label={t('catalog.createStandardLabel')}
              >
                <Plus className="h-4 w-4" />
              </Button>
            </div>

            <div className="min-h-0 space-y-2 overflow-y-auto pr-1">
              {canonicalModelsQuery.isLoading ? (
                <CatalogSkeleton />
              ) : filteredCanonicalModels.length ? (
                filteredCanonicalModels.map((model) => {
                  const active = selectedModel?.id === model.id
                  const canArchive = isManualCreatedCanonicalModel(model) && model.site_model_count === 0
                  const archiving = archiveModelMutation.isPending && archiveModelMutation.variables === model.id

                  return (
                    <div
                      key={model.id}
                      className={cn(
                        'flex w-full items-center gap-2 rounded-md px-3 py-2.5 text-left transition-colors',
                        active
                          ? 'bg-[hsl(var(--surface-subtle))] text-foreground'
                          : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                      )}
                    >
                      <button
                        type="button"
                        onClick={() => setSelectedModelId(model.id)}
                        className="min-w-0 flex-1 text-left"
                      >
                        <span className="block truncate text-sm font-medium" title={model.display_name}>
                          {model.display_name}
                        </span>
                      </button>
                      <span className="shrink-0 rounded-full bg-[hsl(var(--surface-subtle))] px-2 py-0.5 text-xs">
                        {model.site_model_count}
                      </span>
                      {canArchive ? (
                        <Button
                          type="button"
                          size="icon"
                          variant="ghost"
                          className="h-7 w-7 shrink-0"
                          onClick={() => setArchiveTarget(model)}
                          disabled={archiving}
                          aria-label={t('catalog.archiveModelLabel', { model: model.display_name })}
                        >
                          {archiving ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                        </Button>
                      ) : null}
                    </div>
                  )
                })
              ) : (
                <div className="text-muted-soft px-3 py-8 text-center text-sm">{t('catalog.noStandardModels')}</div>
              )}
            </div>
          </aside>

          <section className="min-h-0 overflow-y-auto pr-1">
            {selectedModel ? (
              <div className="space-y-5">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0">
                    <h3 className="truncate text-base font-semibold text-foreground">{selectedModel.display_name}</h3>
                    <div className="text-muted-soft mt-1 truncate text-sm">{selectedModel.model_key}</div>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Badge variant="secondary" className="border-transparent">{getProviderCatalogEntry(selectedModel.provider)?.name ?? selectedModel.provider}</Badge>
                    {isManualCreatedCanonicalModel(selectedModel) && selectedModel.site_model_count === 0 ? (
                      <Button
                        type="button"
                        size="icon"
                        variant="ghost"
                        className="h-8 w-8"
                        onClick={() => setArchiveTarget(selectedModel)}
                        disabled={archiveModelMutation.isPending && archiveModelMutation.variables === selectedModel.id}
                        aria-label={t('catalog.archiveModelLabel', { model: selectedModel.display_name })}
                      >
                        {archiveModelMutation.isPending && archiveModelMutation.variables === selectedModel.id
                          ? <LoaderCircle className="h-4 w-4 animate-spin" />
                          : <Trash2 className="h-4 w-4" />}
                      </Button>
                    ) : null}
                  </div>
                </div>

                <div className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-soft))] p-4">
                  <div className="mb-3 flex items-center justify-between gap-3">
                    <h4 className="text-sm font-medium text-foreground">{t('catalog.alias')}</h4>
                    <div className="flex min-w-0 max-w-[420px] flex-1 gap-2">
                      <Input
                        value={aliasInput}
                        onChange={(event) => setAliasInput(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === 'Enter') {
                            handleAddAlias()
                          }
                        }}
                        placeholder={t('catalog.addAliasPlaceholder')}
                        className="h-9"
                      />
                      <Button
                        size="sm"
                        onClick={handleAddAlias}
                        disabled={!aliasInput.trim() || addAliasMutation.isPending}
                      >
                        {addAliasMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Plus className="h-4 w-4" />}
                        {t('catalog.addAlias')}
                      </Button>
                    </div>
                  </div>

                  <div className="flex flex-wrap gap-2">
                    {selectedModel.aliases.length ? (
                      selectedModel.aliases.map((alias) => {
                        const deleting = deleteAliasMutation.isPending &&
                          deleteAliasMutation.variables?.aliasId === alias.id

                        return (
                          <span
                            key={alias.id}
                            className="inline-flex max-w-full items-center gap-1.5 rounded-full bg-[hsl(var(--surface-subtle))] px-2.5 py-1 text-xs text-foreground"
                          >
                            <span className="truncate">{alias.alias}</span>
                            <button
                              type="button"
                              className="text-muted-soft hover:text-foreground"
                              onClick={() => deleteAliasMutation.mutate({ modelId: selectedModel.id, aliasId: alias.id })}
                              disabled={deleting}
                              aria-label={t('catalog.deleteAliasLabel', { alias: alias.alias })}
                            >
                              {deleting ? <LoaderCircle className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
                            </button>
                          </span>
                        )
                      })
                    ) : (
                      <span className="text-muted-soft text-sm">{t('catalog.noAliases')}</span>
                    )}
                  </div>
                </div>

                <CatalogMatrixTable
                  rows={matrixQuery.data?.items ?? []}
                  loading={matrixQuery.isLoading}
                  pendingSiteModelId={bindMutation.isPending ? bindMutation.variables?.siteModelId : undefined}
                  onUnbind={(siteModelId) => bindMutation.mutate({ siteModelId, canonicalModelId: null })}
                  t={t}
                />

                {unmatchedSection}
              </div>
            ) : (
              <div className="space-y-5">
                {unmatchedSection}
              </div>
            )}
          </section>
        </DialogBody>
      </DialogContent>

      <Dialog
        open={Boolean(createDraft)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen && !createModelMutation.isPending) {
            setCreateDraft(null)
          }
        }}
      >
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader>
            <DialogTitle>{t('catalog.createSurface.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="space-y-4">
            <div className="space-y-2">
              <span className="block text-sm font-medium">{t('catalog.createSurface.modelKey')}</span>
              <Input
                aria-label={t('catalog.createSurface.modelKey')}
                value={createDraft?.modelKey ?? ''}
                onChange={(event) => handleCreateDraftModelKeyChange(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    handleCreateModel()
                  }
                }}
              />
            </div>
            <div className="space-y-2">
              <span className="block text-sm font-medium">{t('catalog.createSurface.displayName')}</span>
              <Input
                aria-label={t('catalog.createSurface.displayName')}
                value={createDraft?.displayName ?? ''}
                onChange={(event) => {
                  setCreateDraft((current) => current ? { ...current, displayName: event.target.value } : current)
                }}
              />
            </div>
            {createDraft?.target ? (
              <div className="rounded-md bg-[hsl(var(--surface-subtle))] px-3 py-2 text-sm text-muted-soft">
                {createDraft.target.model.display_name || createDraft.target.model.upstream_model_name}
              </div>
            ) : null}
          </DialogBody>
          <div className="flex justify-end gap-3 border-t border-[hsl(var(--glass-divider))] px-6 py-4">
            <Button
              variant="ghost"
              onClick={() => setCreateDraft(null)}
              disabled={createModelMutation.isPending}
            >
              {t('catalog.createSurface.cancel')}
            </Button>
            <Button
              onClick={handleCreateModel}
              disabled={!createDraft?.modelKey.trim() || createModelMutation.isPending}
            >
              {createModelMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {t('catalog.createSurface.save')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
      {archiveConfirmDialog}
    </Dialog>
  )
}

function CatalogSkeleton() {
  return (
    <>
      <Skeleton className="h-14 w-full" />
      <Skeleton className="h-14 w-full" />
      <Skeleton className="h-14 w-full" />
    </>
  )
}

type TFunction = (key: string, options?: Record<string, unknown>) => string

function MobileCatalogMatrixList({
  rows,
  loading,
  pendingSiteModelId,
  onUnbind,
  t,
}: {
  rows: CanonicalModelMatrixRow[]
  loading: boolean
  pendingSiteModelId?: string
  onUnbind: (siteModelId: string) => void
  t: TFunction
}) {
  return (
    <div className="space-y-2 rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-soft))] p-3">
      <div className="flex items-center justify-between gap-3">
        <h4 className="text-sm font-semibold text-foreground">{t('catalog.headers.model')}</h4>
        <Badge variant="secondary" className="border-transparent">{rows.length}</Badge>
      </div>

      {loading ? (
        <div className="text-muted-soft py-7 text-center text-sm">{t('catalog.loading')}</div>
      ) : rows.length ? (
        rows.map((row) => {
          const pending = pendingSiteModelId === row.site_model_id
          const source = formatMatchSource(row.canonical_match_source, t)

          return (
            <div
              key={row.site_model_id}
              className="rounded-xl bg-[hsl(var(--surface-subtle))] px-3 py-2.5"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="truncate text-sm font-semibold text-foreground" title={row.site_name}>
                    {row.site_name}
                  </div>
                  <div
                    className="text-muted-soft mt-1 line-clamp-2 break-all text-xs"
                    title={row.display_name || row.upstream_model_name}
                  >
                    {row.display_name || row.upstream_model_name}
                  </div>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-8 shrink-0 px-2"
                  onClick={() => onUnbind(row.site_model_id)}
                  disabled={pending}
                >
                  {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                  {t('catalog.unbind')}
                </Button>
              </div>

              <div className="mt-2 flex flex-wrap gap-2">
                <span className="rounded-full bg-[hsl(var(--surface-soft))] px-2 py-0.5 text-xs text-muted-soft">
                  {source}
                </span>
                <span className="rounded-full bg-[hsl(var(--surface-soft))] px-2 py-0.5 text-xs text-muted-soft">
                  {row.canonical_match_confidence}%
                </span>
              </div>
            </div>
          )
        })
      ) : (
        <div className="text-muted-soft py-7 text-center text-sm">{t('catalog.noLinkedModels')}</div>
      )}
    </div>
  )
}

function CatalogMatrixTable({
  rows,
  loading,
  pendingSiteModelId,
  onUnbind,
  t,
}: {
  rows: CanonicalModelMatrixRow[]
  loading: boolean
  pendingSiteModelId?: string
  onUnbind: (siteModelId: string) => void
  t: TFunction
}) {
  return (
    <div className="overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-soft))]">
      <table className="w-full table-fixed border-collapse text-left text-sm">
        <thead className="bg-[hsl(var(--surface-subtle))] text-faint text-xs uppercase tracking-[0.16em]">
          <tr>
            <th className="w-[13%] px-4 py-3 font-medium">{t('catalog.headers.site')}</th>
            <th className="w-[57%] px-4 py-3 font-medium">{t('catalog.headers.model')}</th>
            <th className="w-[9%] px-4 py-3 font-medium">{t('catalog.headers.source')}</th>
            <th className="w-[11%] px-4 py-3 font-medium">{t('catalog.headers.confidence')}</th>
            <th className="w-[10%] px-4 py-3 font-medium"></th>
          </tr>
        </thead>
        <tbody>
          {loading ? (
            <tr>
              <td colSpan={5} className="px-4 py-10 text-center text-sm text-muted-soft">{t('catalog.loading')}</td>
            </tr>
          ) : rows.length ? (
            rows.map((row) => {
              const pending = pendingSiteModelId === row.site_model_id

              return (
                <tr key={row.site_model_id} className="border-t border-[hsl(var(--glass-divider))]">
                  <td className="px-4 py-3">
                    <div className="truncate font-medium text-foreground" title={row.site_name}>{row.site_name}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div
                      className="line-clamp-2 break-all text-foreground"
                      title={row.display_name || row.upstream_model_name}
                    >
                      {row.display_name || row.upstream_model_name}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-muted-soft" title={formatMatchSource(row.canonical_match_source, t)}>
                    {formatMatchSource(row.canonical_match_source, t)}
                  </td>
                  <td className="px-4 py-3 text-muted-soft">{row.canonical_match_confidence}%</td>
                  <td className="px-4 py-3 text-right">
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => onUnbind(row.site_model_id)}
                      disabled={pending}
                    >
                      {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                      {t('catalog.unbind')}
                    </Button>
                  </td>
                </tr>
              )
            })
          ) : (
            <tr>
              <td colSpan={5} className="px-4 py-10 text-center text-sm text-muted-soft">{t('catalog.noLinkedModels')}</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
