import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Link, LoaderCircle, Plus, RefreshCw, RotateCcw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toast } from '@/lib/toast'
import {
  createDownstreamAPIKey,
  deleteDownstreamAPIKey,
  downstreamAPIKeyQueryKeys,
  listDownstreamAPIKeys,
  resetDownstreamAPIKeyQuota,
  rotateDownstreamAPIKey,
  updateDownstreamAPIKey,
  type APIKeyUpsertInput,
  type DownstreamAPIKey,
  type QuotaResetScope,
} from '@/features/api-keys/api/api-keys'
import { APIKeyFormDraw } from '@/features/api-keys/components/api-key-form-draw'
import { APIKeyModelsDraw } from '@/features/api-keys/components/api-key-models-draw'
import { APIKeysSkeleton } from '@/features/api-keys/components/api-keys-skeleton'
import { DownstreamAPIKeysTable } from '@/features/api-keys/components/downstream-api-keys-table'
import { MobileAPIKeysList } from '@/features/api-keys/components/mobile-api-keys-list'
import {
  apiKeySearchText,
  isAPIKeyActive,
  readLastUsedMode,
  saveLastUsedMode,
  sortAPIKeysForDisplay,
  sortCanonicalModels,
  toUpdateInput,
} from '@/features/api-keys/lib/api-key-utils'
import type { StatusFilter, TimeDisplayMode } from '@/features/api-keys/lib/types'
import {
  listCanonicalModels,
  listSitesWithOAuth,
  sitesQueryKeys,
  type CanonicalModelItem,
  type Site,
} from '@/features/sites/api/sites'
import { listSiteGroups, siteGroupQueryKeys, type SiteGroup } from '@/features/settings/api/site-groups'
import { useMobileLayout } from '@/hooks/use-media-query'

const EMPTY_API_KEYS: DownstreamAPIKey[] = []
const EMPTY_MODELS: CanonicalModelItem[] = []
const EMPTY_SITES: Site[] = []
const EMPTY_SITE_GROUPS: SiteGroup[] = []

export function DownstreamAPIKeysWorkspace() {
  const { t } = useTranslation('api-keys')
  const queryClient = useQueryClient()
  const isMobile = useMobileLayout()
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [formOpen, setFormOpen] = useState(false)
  const [editingKey, setEditingKey] = useState<DownstreamAPIKey | null>(null)
  const [modelsKey, setModelsKey] = useState<DownstreamAPIKey | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<DownstreamAPIKey | null>(null)
  const [rotateTarget, setRotateTarget] = useState<DownstreamAPIKey | null>(null)
  const [rotatedKey, setRotatedKey] = useState<{ name: string; key: string } | null>(null)
  const [resetTarget, setResetTarget] = useState<DownstreamAPIKey | null>(null)
  const [resetScopes, setResetScopes] = useState<QuotaResetScope[]>([])
  const [lastUsedMode, setLastUsedMode] = useState<TimeDisplayMode>(readLastUsedMode)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [])
  useEffect(() => { saveLastUsedMode(lastUsedMode) }, [lastUsedMode])

  const apiKeysQuery = useQuery({
    queryKey: downstreamAPIKeyQueryKeys.list(),
    queryFn: listDownstreamAPIKeys,
  })
  const canonicalModelsQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'canonical-models'],
    queryFn: listCanonicalModels,
  })
  const sitesQuery = useQuery({
    queryKey: sitesQueryKeys.listWithOAuth(),
    queryFn: listSitesWithOAuth,
  })
  const siteGroupsQuery = useQuery({
    queryKey: siteGroupQueryKeys.list(),
    queryFn: listSiteGroups,
  })

  const apiKeys = apiKeysQuery.data?.items ?? EMPTY_API_KEYS
  const canonicalModels = useMemo(
    () => sortCanonicalModels((canonicalModelsQuery.data?.items ?? EMPTY_MODELS).filter((model) => model.status === 'active')),
    [canonicalModelsQuery.data?.items],
  )
  const sites = sitesQuery.data?.items ?? EMPTY_SITES
  const siteGroups = siteGroupsQuery.data?.items ?? EMPTY_SITE_GROUPS

  const filteredAPIKeys = useMemo(() => {
    const keyword = search.trim().toLowerCase()

    const filtered = apiKeys.filter((apiKey) => {
      const matchesStatus =
        statusFilter === 'all' ||
        (statusFilter === 'active' ? isAPIKeyActive(apiKey) : !isAPIKeyActive(apiKey))
      const matchesSearch = !keyword || apiKeySearchText(apiKey).includes(keyword)

      return matchesStatus && matchesSearch
    })
    return sortAPIKeysForDisplay(filtered)
  }, [apiKeys, search, statusFilter])

  const saveMutation = useMutation({
    mutationFn: async (input: APIKeyUpsertInput & { id?: string }) => {
      if (input.id) {
        return (await updateDownstreamAPIKey(input.id, input)).api_key
      }

      return (await createDownstreamAPIKey(input)).api_key
    },
    onSuccess: (result, variables) => {
      queryClient.setQueryData<Awaited<ReturnType<typeof listDownstreamAPIKeys>>>(
        downstreamAPIKeyQueryKeys.list(),
        (current) => {
          if (!current) return current
          const nextResult = { ...result, key: null }
          const existingIndex = current.items.findIndex((item) => item.id === result.id)
          const items = existingIndex >= 0
            ? current.items.map((item) => (item.id === result.id ? nextResult : item))
            : [...current.items, nextResult]
          return { ...current, items, meta: { ...current.meta, count: items.length } }
        },
      )
      setFormOpen(false)
      setEditingKey(null)
      toast.success(variables.id ? t('workspace.toast.keyUpdated') : t('workspace.toast.keyCreated'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.saveFailed'), { description: error.message })
    },
  })

  const toggleMutation = useMutation({
    mutationFn: (apiKey: DownstreamAPIKey) => updateDownstreamAPIKey(apiKey.id, toUpdateInput(apiKey, {
      status: isAPIKeyActive(apiKey) ? 'disabled' : 'active',
    })),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: downstreamAPIKeyQueryKeys.list() })
    },
    onError: (error) => {
      toast.error(t('workspace.toast.statusFailed'), { description: error.message })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteDownstreamAPIKey,
    onSuccess: async () => {
      setDeleteTarget(null)
      await queryClient.invalidateQueries({ queryKey: downstreamAPIKeyQueryKeys.list() })
      toast.success(t('workspace.toast.keyDeleted'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.deleteFailed'), { description: error.message })
    },
  })

  const resetQuotaMutation = useMutation({
    mutationFn: ({ apiKeyId, scopes }: { apiKeyId: string; scopes: QuotaResetScope[] }) => (
      resetDownstreamAPIKeyQuota(apiKeyId, scopes)
    ),
    onSuccess: async () => {
      setResetTarget(null)
      await queryClient.invalidateQueries({ queryKey: downstreamAPIKeyQueryKeys.list() })
      toast.success(t('workspace.toast.quotaReset'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.quotaResetFailed'), { description: error.message })
    },
  })

  const rotateMutation = useMutation({
    mutationFn: (apiKeyId: string) => rotateDownstreamAPIKey(apiKeyId),
    onSuccess: async (result) => {
      setRotateTarget(null)
      setRotatedKey({ name: result.api_key.name, key: result.key })
      await queryClient.invalidateQueries({ queryKey: downstreamAPIKeyQueryKeys.list() })
      toast.success(t('workspace.toast.keyRotated'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.rotateFailed'), { description: error.message })
    },
  })

  if (apiKeysQuery.isLoading) {
    return <APIKeysSkeleton />
  }

  if (apiKeysQuery.isError) {
    return (
      <ErrorState
        title={t('workspace.loadFailed')}
        description={apiKeysQuery.error.message}
      />
    )
  }

  function openCreateKey() {
    setEditingKey(null)
    setFormOpen(true)
  }

  function openEditKey(apiKey: DownstreamAPIKey) {
    setEditingKey(apiKey)
    setFormOpen(true)
  }

  function openResetQuota(apiKey: DownstreamAPIKey) {
    const totalEnabled = !apiKey.quota_unlimited && apiKey.quota_limit != null
    const dailyEnabled = !apiKey.quota_daily_unlimited && apiKey.quota_daily_limit != null
    const weeklyEnabled = !apiKey.quota_weekly_unlimited && apiKey.quota_weekly_limit != null
    setResetTarget(apiKey)
    setResetScopes([
      ...(totalEnabled ? ['total' as const] : []),
      ...(weeklyEnabled ? ['weekly' as const] : []),
      ...(dailyEnabled ? ['daily' as const] : []),
    ])
  }

  const listProps = {
    apiKeys: filteredAPIKeys,
    lastUsedMode,
    now,
    togglingKeyId: toggleMutation.isPending ? toggleMutation.variables?.id : undefined,
    deletingKeyId: deleteMutation.isPending ? deleteMutation.variables : undefined,
    onToggleKey: (apiKey: DownstreamAPIKey) => toggleMutation.mutate(apiKey),
    onEditKey: openEditKey,
    onDeleteKey: setDeleteTarget,
    onRotateKey: setRotateTarget,
    onResetQuota: openResetQuota,
    onShowModels: setModelsKey,
    onToggleLastUsedMode: () => setLastUsedMode((current) => (current === 'absolute' ? 'relative' : 'absolute')),
  }

  const dialogs = (
    <>
      {formOpen ? (
        <APIKeyFormDraw
          open={formOpen}
          initialKey={editingKey}
          canonicalModels={canonicalModels}
          canonicalModelsLoading={canonicalModelsQuery.isLoading}
          sites={sites}
          sitesLoading={sitesQuery.isLoading}
          siteGroups={siteGroups}
          siteGroupsLoading={siteGroupsQuery.isLoading}
          pending={saveMutation.isPending && (saveMutation.variables?.id ?? null) === (editingKey?.id ?? null)}
          onOpenChange={(open) => {
            if (!open && !saveMutation.isPending) {
              setFormOpen(false)
              setEditingKey(null)
            }
          }}
          onSubmit={(values) => {
            saveMutation.mutate({
              ...values,
              id: editingKey?.id,
            })
          }}
        />
      ) : null}

      <APIKeyModelsDraw
        apiKey={modelsKey}
        canonicalModels={canonicalModels}
        onOpenChange={(open) => {
          if (!open) setModelsKey(null)
        }}
      />

      <Dialog
        open={Boolean(resetTarget)}
        onOpenChange={(open) => {
          if (!open && !resetQuotaMutation.isPending) {
            setResetTarget(null)
          }
        }}
      >
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('workspace.resetQuotaDialog.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="space-y-5 pt-0">
            <DialogDescription className="mt-0">
              {resetTarget ? t('workspace.resetQuotaDialog.description', { name: resetTarget.name }) : null}
            </DialogDescription>
            <div className="grid gap-3 rounded-lg border border-[hsl(var(--glass-border))] p-3">
              {resetTarget && !resetTarget.quota_unlimited && resetTarget.quota_limit != null ? (
                <Checkbox
                  checked={resetScopes.includes('total')}
                  disabled={resetQuotaMutation.isPending}
                  onCheckedChange={(checked) => setResetScopes((current) => (
                    checked ? [...current.filter((scope) => scope !== 'total'), 'total'] : current.filter((scope) => scope !== 'total')
                  ))}
                  label={t('workspace.resetQuotaDialog.total')}
                  description={t('workspace.resetQuotaDialog.totalDescription', { used: resetTarget.quota_total_used ?? resetTarget.quota_used })}
                />
              ) : null}
              {resetTarget && !resetTarget.quota_weekly_unlimited && resetTarget.quota_weekly_limit != null ? (
                <Checkbox
                  checked={resetScopes.includes('weekly')}
                  disabled={resetQuotaMutation.isPending}
                  onCheckedChange={(checked) => setResetScopes((current) => (
                    checked ? [...current.filter((scope) => scope !== 'weekly'), 'weekly'] : current.filter((scope) => scope !== 'weekly')
                  ))}
                  label={t('workspace.resetQuotaDialog.weekly')}
                  description={t('workspace.resetQuotaDialog.weeklyDescription', { used: resetTarget.quota_weekly_used ?? 0 })}
                />
              ) : null}
              {resetTarget && !resetTarget.quota_daily_unlimited && resetTarget.quota_daily_limit != null ? (
                <Checkbox
                  checked={resetScopes.includes('daily')}
                  disabled={resetQuotaMutation.isPending}
                  onCheckedChange={(checked) => setResetScopes((current) => (
                    checked ? [...current.filter((scope) => scope !== 'daily'), 'daily'] : current.filter((scope) => scope !== 'daily')
                  ))}
                  label={t('workspace.resetQuotaDialog.daily')}
                  description={t('workspace.resetQuotaDialog.dailyDescription', { used: resetTarget.quota_daily_used ?? 0 })}
                />
              ) : null}
            </div>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button
              variant="outline"
              onClick={() => setResetTarget(null)}
              disabled={resetQuotaMutation.isPending}
            >
              {t('workspace.resetQuotaDialog.cancel')}
            </Button>
            <Button
              onClick={() => {
                if (resetTarget && resetScopes.length > 0) {
                  resetQuotaMutation.mutate({ apiKeyId: resetTarget.id, scopes: resetScopes })
                }
              }}
              disabled={resetQuotaMutation.isPending || resetScopes.length === 0}
            >
              {resetQuotaMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
              {t('workspace.resetQuotaDialog.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(rotateTarget)}
        onOpenChange={(open) => {
          if (!open && !rotateMutation.isPending) {
            setRotateTarget(null)
          }
        }}
      >
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('workspace.rotateDialog.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pt-0">
            <DialogDescription className="mt-0">
              {rotateTarget ? t('workspace.rotateDialog.description', { name: rotateTarget.name }) : null}
            </DialogDescription>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button
              variant="outline"
              onClick={() => setRotateTarget(null)}
              disabled={rotateMutation.isPending}
            >
              {t('workspace.rotateDialog.cancel')}
            </Button>
            <Button
              onClick={() => {
                if (rotateTarget) {
                  rotateMutation.mutate(rotateTarget.id)
                }
              }}
              disabled={rotateMutation.isPending}
            >
              {rotateMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              {t('workspace.rotateDialog.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(rotatedKey)}
        onOpenChange={(open) => {
          if (!open) {
            setRotatedKey(null)
          }
        }}
      >
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('workspace.rotateResultDialog.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="min-w-0 space-y-4 pt-0">
            <DialogDescription className="mt-0">
              {rotatedKey ? t('workspace.rotateResultDialog.description', { name: rotatedKey.name }) : null}
            </DialogDescription>
            <div className="flex items-start gap-2 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-3 py-2.5">
              <code className="min-w-0 flex-1 break-all font-mono text-sm leading-6 text-foreground">{rotatedKey?.key}</code>
              <Button
                size="icon"
                variant="ghost"
                className="h-8 w-8 shrink-0"
                aria-label={t('workspace.rotateResultDialog.copy')}
                title={t('workspace.rotateResultDialog.copy')}
                onClick={() => {
                  if (rotatedKey) {
                    void copyToClipboard(rotatedKey.key, t('table.copySuccess'), t('table.copyFailed'))
                  }
                }}
              >
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button onClick={() => setRotatedKey(null)}>
              {t('workspace.rotateResultDialog.close')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) {
            setDeleteTarget(null)
          }
        }}
      >
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('workspace.deleteDialog.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pt-0">
            <DialogDescription className="mt-0">
              {deleteTarget ? t('workspace.deleteDialog.confirm', { name: deleteTarget.name }) : t('workspace.deleteDialog.confirmDefault')}
            </DialogDescription>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button
              variant="outline"
              onClick={() => setDeleteTarget(null)}
              disabled={deleteMutation.isPending}
            >
              {t('workspace.deleteDialog.cancel')}
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (deleteTarget) {
                  deleteMutation.mutate(deleteTarget.id)
                }
              }}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {t('workspace.deleteDialog.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )

  if (isMobile) {
    return (
      <div className="relative space-y-4 pb-2">
        <div>
          <PageHeader
            eyebrow={t('workspace.eyebrow')}
            title={t('workspace.title')}
            description={t('workspace.description')}
          />
        </div>

        <div className="sticky top-0 z-20 -mx-1">
          <div className="rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-2 shadow-[0_18px_38px_rgba(0,0,0,0.10)] backdrop-blur-[32px] backdrop-saturate-150">
            <APIKeyEndpointCopy className="mb-2 w-full" />
            <div className="flex items-center gap-2">
              <div className="relative min-w-0 flex-1">
                <Input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={t('workspace.search')}
                  className="pl-10"
                />
                <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-foreground/40" />
              </div>
              <Button
                variant="secondary"
                size="icon"
                className="h-10 w-10 shrink-0 bg-[hsl(var(--card)/0.34)] backdrop-blur-[24px] hover:bg-[hsl(var(--card)/0.46)]"
                onClick={() => apiKeysQuery.refetch()}
                disabled={apiKeysQuery.isFetching}
                aria-label={t('workspace.refreshAll')}
              >
                {apiKeysQuery.isFetching ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
              </Button>
            </div>

            <div className="mt-2">
              <Select value={statusFilter} onValueChange={(value) => setStatusFilter(value as StatusFilter)}>
                <SelectTrigger variant="filter" filterLabel={t('table.headers.status')} active={statusFilter !== 'all'}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent searchable={false} widthMode="content">
                  <SelectItem value="all">{t('workspace.status.all')}</SelectItem>
                  <SelectItem value="active">{t('workspace.status.active')}</SelectItem>
                  <SelectItem value="disabled">{t('workspace.status.disabled')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </div>

        {filteredAPIKeys.length ? (
          <MobileAPIKeysList {...listProps} />
        ) : (
          <EmptyState
            title={t('table.empty.title')}
            description={t('table.empty.description')}
            action={!apiKeys.length ? <Button onClick={openCreateKey}>{t('workspace.createKey')}</Button> : undefined}
          />
        )}

        <Button
          className="fixed bottom-[calc(env(safe-area-inset-bottom,0px)+6rem)] right-5 z-20 h-12 w-12 rounded-full p-0 shadow-[0_18px_34px_rgba(15,23,42,0.22)]"
          onClick={openCreateKey}
          aria-label={t('workspace.createKey')}
        >
          <Plus className="h-5 w-5" />
        </Button>

        {dialogs}
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow={t('workspace.eyebrow')}
        title={t('workspace.title')}
        description={t('workspace.description')}
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex-1 flex items-center gap-3 min-w-0">
          <APIKeyEndpointCopy />
          <div className="relative w-52 shrink-0">
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('workspace.search')}
              className="pl-10"
            />
            <Search className="text-foreground/40 absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 pointer-events-none z-10" />
          </div>

          <Select value={statusFilter} onValueChange={(value) => setStatusFilter(value as StatusFilter)}>
            <SelectTrigger variant="filter" filterLabel={t('table.headers.status')} active={statusFilter !== 'all'}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent searchable={false} widthMode="content">
              <SelectItem value="all">{t('workspace.status.all')}</SelectItem>
              <SelectItem value="active">{t('workspace.status.active')}</SelectItem>
              <SelectItem value="disabled">{t('workspace.status.disabled')}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            onClick={() => apiKeysQuery.refetch()}
            disabled={apiKeysQuery.isFetching}
          >
            {apiKeysQuery.isFetching ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
            {t('workspace.refreshAll')}
          </Button>
          <Button
            onClick={() => {
              openCreateKey()
            }}
          >
            <Plus className="h-4 w-4" />
            {t('workspace.createKey')}
          </Button>
        </div>
      </div>

      <DownstreamAPIKeysTable
        {...listProps}
      />

      {dialogs}
    </div>
  )
}

function APIKeyEndpointCopy({ className }: { className?: string }) {
  const { t } = useTranslation('api-keys')
  const endpoint = currentGatewayEndpoint()

  return (
    <button
      type="button"
      className={`glass-field inline-flex h-10 min-w-0 items-center gap-2 rounded-md px-3 text-left text-sm text-foreground transition-all duration-200 hover:bg-[hsl(var(--surface-soft-hover))] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))] ${className ?? 'max-w-[min(24rem,38vw)] shrink-0'}`}
      title={t('workspace.endpointCopyTitle')}
      aria-label={t('workspace.endpointCopyTitle')}
      onClick={() => {
        void copyToClipboard(endpoint, t('workspace.endpointCopied'), t('workspace.endpointCopyFailed'))
      }}
    >
      <Link className="h-4 w-4 shrink-0 text-foreground/50" />
      <code className="min-w-0 truncate font-mono text-xs text-foreground md:text-sm">{endpoint}</code>
      <Copy className="h-4 w-4 shrink-0 text-foreground/45" />
    </button>
  )
}

function currentGatewayEndpoint() {
  if (typeof window === 'undefined') return '/v1'
  return `${window.location.protocol}//${window.location.host}/v1`
}
