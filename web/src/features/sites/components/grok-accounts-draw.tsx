import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, KeyRound, LoaderCircle, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Draw, DrawBody, DrawContent, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { Switch } from '@/components/ui/switch'
import { ModelsDraw, type ModelsDrawItem } from '@/components/common/models-draw'
import { toast } from '@/lib/toast'
import {
  deleteGrokAccount,
  listGrokAccounts,
  pollGrokDeviceLogin,
  refreshGrokAccount,
  refreshGrokAccounts,
  sitesQueryKeys,
  startGrokDeviceLogin,
  updateGrokAccount,
  updateGrokAccountModel,
  type GrokAccount,
  type GrokDeviceStart,
  type Site,
} from '@/features/sites/api/sites'

const EMPTY_ACCOUNTS: GrokAccount[] = []

type DeviceState = {
  status: 'starting' | 'waiting' | 'error'
  purpose: 'add' | 'reauth'
  credentialId?: string
  info?: GrokDeviceStart
  error?: string
}

export function GrokAccountsDraw({
  site,
  onOpenChange,
}: {
  site: Site | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation('sites')
  const queryClient = useQueryClient()
  const [deleting, setDeleting] = useState<GrokAccount | null>(null)
  const [device, setDevice] = useState<DeviceState | null>(null)
  const [modelsAccount, setModelsAccount] = useState<GrokAccount | null>(null)
  const open = Boolean(site)
  const queryKey = site
    ? [...sitesQueryKeys.detail(site.id), 'grok-accounts']
    : [...sitesQueryKeys.all, 'grok-accounts', 'none']

  const accountsQuery = useQuery({
    queryKey,
    queryFn: () => site ? listGrokAccounts(site.id) : Promise.resolve({ items: EMPTY_ACCOUNTS }),
    enabled: open,
  })

  const updateMutation = useMutation({
    mutationFn: ({ account, enabled }: { account: GrokAccount; enabled: boolean }) => {
      if (!site) throw new Error('site required')
      return updateGrokAccount(site.id, account.credential_id, { enabled })
    },
    onSuccess: async () => {
      await refreshAccountQueries()
      toast.success(t('grokAccounts.toast.updated'))
    },
    onError: (error) => toast.error(t('grokAccounts.toast.updateFailed'), { description: error.message }),
  })

  const deleteMutation = useMutation({
    mutationFn: (account: GrokAccount) => {
      if (!site) throw new Error('site required')
      return deleteGrokAccount(site.id, account.credential_id)
    },
    onSuccess: async () => {
      setDeleting(null)
      await refreshAccountQueries()
      toast.success(t('grokAccounts.toast.deleted'))
    },
    onError: (error) => toast.error(t('grokAccounts.toast.deleteFailed'), { description: error.message }),
  })

  const refreshMutation = useMutation({
    mutationFn: (account: GrokAccount) => {
      if (!site) throw new Error('site required')
      return refreshGrokAccount(site.id, account.credential_id)
    },
    onSuccess: async (result) => {
      await refreshAccountQueries()
      if (result.account.sync_message) {
        toast.warning(t('grokAccounts.toast.refreshFailed'), { description: result.account.sync_message })
      } else {
        toast.success(t('grokAccounts.toast.refreshed'))
      }
    },
    onError: (error) => toast.error(t('grokAccounts.toast.refreshFailed'), { description: error.message }),
  })

  const refreshAllMutation = useMutation({
    mutationFn: () => {
      if (!site) throw new Error('site required')
      return refreshGrokAccounts(site.id)
    },
    onSuccess: async (result) => {
      await refreshAccountQueries()
      const failed = result.meta?.failed ?? 0
      if (failed > 0) {
        toast.warning(t('grokAccounts.toast.refreshAllPartial'), {
          description: t('grokAccounts.toast.refreshAllPartialDesc', { count: failed }),
        })
      } else {
        toast.success(t('grokAccounts.toast.refreshedAll'))
      }
    },
    onError: (error) => toast.error(t('grokAccounts.toast.refreshAllFailed'), { description: error.message }),
  })

  const updateModelMutation = useMutation({
    mutationFn: ({ credentialId, model, enabled }: { credentialId: string; model: string; enabled: boolean }) => {
      if (!site) throw new Error('site required')
      return updateGrokAccountModel(site.id, credentialId, { model, enabled })
    },
    onSuccess: async (result) => {
      setModelsAccount((current) => (current?.credential_id === result.account.credential_id ? result.account : current))
      await refreshAccountQueries()
    },
    onError: (error) => toast.error(t('grokAccounts.toast.updateFailed'), { description: error.message }),
  })

  async function refreshAccountQueries() {
    await queryClient.invalidateQueries({ queryKey })
    await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.list() })
    if (site) await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.models(site.id) })
  }

  async function startDeviceLogin(purpose: DeviceState['purpose'], credentialId?: string) {
    if (!site) return
    setDevice({ status: 'starting', purpose, credentialId })
    try {
      const result = await startGrokDeviceLogin(site.id)
      setDevice({ status: 'waiting', purpose, credentialId, info: result.device })
      window.open(result.device.verification_uri_complete || result.device.verification_uri, '_blank', 'noopener')
    } catch (error) {
      setDevice({ status: 'error', purpose, credentialId, error: (error as Error).message })
    }
  }

  useEffect(() => {
    if (!site || !device || device.status !== 'waiting' || !device.info) return
    let cancelled = false
    const deviceCode = device.info.device_code
    const intervalMs = Math.max(2, device.info.interval || 5) * 1000
    const timer = setInterval(async () => {
      try {
        const result = await pollGrokDeviceLogin(site.id, deviceCode, device.credentialId)
        if (cancelled) return
        if (result.device.status === 'complete') {
          setDevice(null)
          await refreshAccountQueries()
          toast.success(t('grokAccounts.toast.created'))
        }
      } catch (error) {
        if (cancelled) return
        setDevice((current) => current ? { ...current, status: 'error', error: (error as Error).message } : null)
      }
    }, intervalMs)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [site?.id, device?.status, device?.info?.device_code])

  const items = accountsQuery.data?.items ?? EMPTY_ACCOUNTS
  const activeModelsAccount = modelsAccount
    ? (items.find((account) => account.credential_id === modelsAccount.credential_id) ?? modelsAccount)
    : null

  return (
    <>
      <Draw open={open} onOpenChange={(next) => {
        if (!next) {
          setDeleting(null)
          setDevice(null)
        }
        onOpenChange(next)
      }}>
        <DrawContent side="right" size="wide">
          <DrawHeader className="flex flex-col items-stretch gap-3 sm:flex-row sm:items-center sm:justify-between">
            <DrawTitle>{t('grokAccounts.title')}</DrawTitle>
            <div className="flex flex-col gap-2 min-[420px]:flex-row min-[420px]:items-center">
              <Button size="sm" variant="outline" className="w-full min-[420px]:w-auto" disabled={refreshAllMutation.isPending} onClick={() => refreshAllMutation.mutate()}>
                {refreshAllMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}{t('grokAccounts.refreshAll')}
              </Button>
              <Button size="sm" className="w-full min-[420px]:w-auto" disabled={Boolean(device)} onClick={() => startDeviceLogin('add')}>
                <Plus className="h-4 w-4" />{t('grokAccounts.add')}
              </Button>
            </div>
          </DrawHeader>
          <DrawBody>
            {accountsQuery.isLoading ? (
              <p className="py-10 text-center text-sm text-muted-soft">{t('grokAccounts.loading')}</p>
            ) : accountsQuery.isError ? (
              <p className="py-10 text-center text-sm text-red-400">{accountsQuery.error.message}</p>
            ) : items.length ? (
              <div className="space-y-3">
                {items.map((account) => {
                  const refreshing = refreshMutation.isPending && refreshMutation.variables?.credential_id === account.credential_id
                  const syncBadge = grokSyncBadge(account.sync_status)
                  const reauthRequired = account.sync_status?.toLowerCase() === 'reauth_required'
                  return (
                    <div key={account.credential_id} className="section-soft space-y-3 rounded-lg p-4">
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0 space-y-1.5">
                          <span className="block truncate text-sm font-medium text-foreground">{account.masked_token || t('grokAccounts.unnamed')}</span>
                          <div className="flex flex-wrap items-center gap-1.5">
                            {account.tier ? <Badge variant="outline" className="px-1.5 py-0 text-[10px]">{account.tier}</Badge> : null}
                            <Badge variant="outline" className={`px-1.5 py-0 text-[10px] ${syncBadge.className}`}>{syncBadge.label(t)}</Badge>
                          </div>
                        </div>
                        <Switch
                          checked={account.enabled}
                          disabled={updateMutation.isPending || reauthRequired}
                          aria-label={t('grokAccounts.toggleLabel', { name: account.masked_token || t('grokAccounts.unnamed') })}
                          onCheckedChange={(enabled) => updateMutation.mutate({ account, enabled })}
                        />
                      </div>

                      {account.sync_message ? <p className="text-xs text-red-400">{account.sync_message}</p> : null}

                      <div className="flex items-center justify-between text-xs text-muted-soft">
                        <div className="flex items-center gap-3">
                          <span>-</span>
                          <button type="button" className="hover:text-foreground cursor-pointer" onClick={() => setModelsAccount(account)}>
                            {account.model_count ?? account.models?.length ?? 0} {t('grokAccounts.models')}
                          </button>
                        </div>
                        <div className="flex items-center gap-1">
                          {reauthRequired ? (
                            <Button
                              size="icon"
                              variant="ghost"
                              className="h-7 w-7 text-red-400"
                              disabled={Boolean(device)}
                              title={t('grokAccounts.reauthorize')}
                              aria-label={t('grokAccounts.reauthorize')}
                              onClick={() => startDeviceLogin('reauth', account.credential_id)}
                            >
                              <KeyRound className="h-3.5 w-3.5" />
                            </Button>
                          ) : null}
                          <Button
                            size="icon"
                            variant="ghost"
                            className="h-7 w-7"
                            disabled={refreshing}
                            title={t('grokAccounts.refresh')}
                            aria-label={t('grokAccounts.refresh')}
                            onClick={() => refreshMutation.mutate(account)}
                          >
                            {refreshing ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="h-7 w-7 text-red-500 hover:bg-red-500/10 hover:text-red-400"
                            disabled={deleteMutation.isPending}
                            title={t('grokAccounts.delete')}
                            aria-label={t('grokAccounts.delete')}
                            onClick={() => setDeleting(account)}
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
              <p className="py-10 text-center text-sm text-muted-soft">{t('grokAccounts.empty')}</p>
            )}
          </DrawBody>
        </DrawContent>
      </Draw>

      <Dialog open={Boolean(device)} onOpenChange={(next) => { if (!next) setDevice(null) }}>
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2"><DialogTitle>{t(device?.purpose === 'reauth' ? 'grokAccounts.device.reauthTitle' : 'grokAccounts.device.title')}</DialogTitle></DialogHeader>
          <DialogBody className="space-y-4 pt-0">
            {device?.status === 'starting' ? (
              <p className="flex items-center gap-2 text-sm text-muted-soft"><LoaderCircle className="h-4 w-4 animate-spin" />{t('grokAccounts.device.starting')}</p>
            ) : device?.status === 'error' ? (
              <p className="text-sm text-red-400">{device.error}</p>
            ) : device?.info ? (
              <>
                <DialogDescription className="mt-0">{t('grokAccounts.device.instructions')}</DialogDescription>
                <div className="rounded-lg border border-[hsl(var(--glass-divider))] bg-background/40 py-4 text-center">
                  <span className="font-mono text-2xl font-semibold tracking-[0.3em] text-foreground">{device.info.user_code}</span>
                </div>
                <Button variant="outline" className="w-full" onClick={() => window.open(device.info!.verification_uri_complete || device.info!.verification_uri, '_blank', 'noopener')}>
                  <ExternalLink className="h-4 w-4" />{t('grokAccounts.device.authorize')}
                </Button>
                <p className="flex items-center justify-center gap-2 text-xs text-muted-soft"><LoaderCircle className="h-3.5 w-3.5 animate-spin" />{t('grokAccounts.device.waiting')}</p>
              </>
            ) : null}
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button variant="ghost" onClick={() => setDevice(null)}>{t('grokAccounts.cancel')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(deleting)} onOpenChange={(next) => { if (!next && !deleteMutation.isPending) setDeleting(null) }}>
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2"><DialogTitle>{t('grokAccounts.deleteDialog.title')}</DialogTitle></DialogHeader>
          <DialogBody className="pt-0">
            <DialogDescription className="mt-0">{t('grokAccounts.deleteDialog.confirm', { name: deleting?.masked_token || t('grokAccounts.unnamed') })}</DialogDescription>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button variant="outline" disabled={deleteMutation.isPending} onClick={() => setDeleting(null)}>{t('grokAccounts.cancel')}</Button>
            <Button variant="destructive" disabled={deleteMutation.isPending} onClick={() => { if (deleting) deleteMutation.mutate(deleting) }}>
              {deleteMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}{t('grokAccounts.deleteDialog.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ModelsDraw
        key={activeModelsAccount?.credential_id ?? 'grok-account-models'}
        open={Boolean(activeModelsAccount)}
        title={activeModelsAccount ? `${activeModelsAccount.masked_token} ${t('grokAccounts.models')}` : t('grokAccounts.models')}
        items={(activeModelsAccount?.models ?? []).map<ModelsDrawItem>((model) => ({
          id: model.upstream_name,
          displayName: model.display_name || model.upstream_name,
          upstreamName: model.upstream_name,
          enabled: model.enabled,
        }))}
        pendingItemId={updateModelMutation.isPending ? updateModelMutation.variables?.model : undefined}
        onToggleItem={(item, enabled) => {
          if (activeModelsAccount) updateModelMutation.mutate({ credentialId: activeModelsAccount.credential_id, model: item.id, enabled })
        }}
        onOpenChange={(next) => { if (!next) setModelsAccount(null) }}
      />
    </>
  )
}

function grokSyncBadge(status?: string | null): { className: string; label: (t: (key: string) => string) => string } {
  switch ((status || '').toLowerCase()) {
    case 'synced':
      return { className: 'border-emerald-500/40 text-emerald-400', label: (t) => t('grokAccounts.sync.synced') }
    case 'failed':
      return { className: 'border-red-500/40 text-red-400', label: (t) => t('grokAccounts.sync.failed') }
    case 'reauth_required':
      return { className: 'border-red-500/40 text-red-400', label: (t) => t('grokAccounts.sync.reauthRequired') }
    case 'stale':
      return { className: 'border-amber-500/40 text-amber-400', label: (t) => t('grokAccounts.sync.stale') }
    case 'partial':
      return { className: 'border-amber-500/40 text-amber-400', label: (t) => t('grokAccounts.sync.partial') }
    default:
      return { className: 'text-muted-soft', label: (t) => t('grokAccounts.sync.unknown') }
  }
}
