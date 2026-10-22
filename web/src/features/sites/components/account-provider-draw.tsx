import { useEffect, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, KeyRound, LoaderCircle, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  AccountHeaderActions,
  AccountList,
  AccountModelsDialog,
  AccountRow,
  AccountsListDialog,
} from '@/features/sites/components/account-dialogs'
import {
  accountCopyPrefix,
  accountOperationFlags,
  accountUIText,
  fillAccountText,
  loginOpenURL,
  loginPollIntervalMs,
  loginPollOutcome,
} from '@/features/sites/components/account-provider-login'
import type { AccountCopyPrefix, AccountLoginKind } from '@/features/sites/components/account-provider-login'
import { grokRowView, pluginRowView } from '@/features/sites/components/account-provider-rows'
import { SiteTypeIcon } from '@/features/sites/components/site-type-icon'
import { isBuiltinAccountSite } from '@/features/sites/lib/site-utils'
import { useNestedDialogClassNames } from '@/features/sites/components/use-nested-dialog'
import { AccountImportDialog } from '@/features/sites/components/account-import-dialog'
import { cn } from '@/lib/utils'
import { toast } from '@/lib/toast'
import {
  deleteAccount,
  listAccountRegions,
  listAccounts,
  pollAccountLogin,
  refreshAccount,
  refreshAccounts,
  sitesQueryKeys,
  startAccountLogin,
  updateAccount,
  updateAccountModel,
  type AccountLoginStart,
  type AccountModelUpdate,
  type AccountRegion,
  type Site,
  type SiteAccount,
  type SiteAccountCapabilities,
} from '@/features/sites/api/sites'

const EMPTY_ACCOUNTS: SiteAccount[] = []
const EMPTY_REGIONS: AccountRegion[] = []

type LoginState = {
  status: 'starting' | 'waiting' | 'error'
  purpose: 'add' | 'reauth'
  credentialId?: string
  info?: AccountLoginStart
  error?: string
}

export function AccountLoginDialog({
  login,
  kind,
  onClose,
  copyPrefix,
  labels,
  showVerificationURL = false,
}: {
  login: LoginState | null
  kind: AccountLoginKind
  onClose: () => void
  copyPrefix?: AccountCopyPrefix
  labels?: {
    title?: string
    starting?: string
    instructions?: string
    authorize?: string
    waiting?: string
    cancel?: string
  }
  showVerificationURL?: boolean
}) {
  const { t } = useTranslation('sites')
  const prefix = copyPrefix ?? (kind === 'custom_poll' ? 'accounts' : 'grokAccounts')
  const section = prefix === 'grokAccounts' ? 'device' : 'login'
  const info = login?.info
  const openURL = info ? loginOpenURL(info) : undefined
  return (
    <NestedDialog open={Boolean(login)} onOpenChange={(next) => { if (!next) onClose() }}>
      <DialogHeader className="border-b-0 pb-2">
        <DialogTitle>{labels?.title ?? t(login?.purpose === 'reauth' ? `${prefix}.${section}.reauthTitle` : `${prefix}.${section}.title`)}</DialogTitle>
      </DialogHeader>
      <DialogBody className="space-y-4 pt-0">
        {login?.status === 'starting' ? (
          <p className="flex items-center gap-2 text-body text-label-2"><LoaderCircle className="h-4 w-4 animate-spin" />{labels?.starting ?? t(`${prefix}.${section}.starting`)}</p>
        ) : login?.status === 'error' ? (
          <p className="text-body text-danger">{login.error}</p>
        ) : info ? (
          <>
            <DialogDescription className="mt-0">{labels?.instructions ?? t(`${prefix}.${section}.instructions`)}</DialogDescription>
            {kind === 'oauth2_device_code' ? (
              <div className="rounded-inner border border-separator bg-background/40 py-4 text-center">
                <span className="font-mono text-2xl font-semibold tracking-[0.3em] text-label">{info.user_code}</span>
                {showVerificationURL && openURL ? (
                  <p className="mt-3 break-all px-4 font-mono text-footnote text-label-2">{openURL}</p>
                ) : null}
              </div>
            ) : null}
            <Button variant="secondary" className="w-full" onClick={() => window.open(openURL, '_blank', 'noopener')}>
              <ExternalLink className="h-4 w-4" />{labels?.authorize ?? t(`${prefix}.${section}.authorize`)}
            </Button>
            <p className="flex items-center justify-center gap-2 text-footnote text-label-2"><LoaderCircle className="h-3.5 w-3.5 animate-spin" />{labels?.waiting ?? t(`${prefix}.${section}.waiting`)}</p>
          </>
        ) : null}
      </DialogBody>
      <DialogFooter className="border-t-0 pt-2">
        <Button variant="secondary" onClick={onClose}>{labels?.cancel ?? t(`${prefix}.cancel`)}</Button>
      </DialogFooter>
    </NestedDialog>
  )
}

export function AccountRegionDialog({
  open,
  copyPrefix,
  regions,
  selectedId,
  loading,
  error,
  confirmDisabled,
  onSelect,
  onConfirm,
  onClose,
  labels,
}: {
  open: boolean
  copyPrefix: AccountCopyPrefix
  regions: { id: string; label: string; hint?: string }[]
  selectedId: string
  loading?: boolean
  error?: string
  confirmDisabled?: boolean
  onSelect: (id: string) => void
  onConfirm: () => void
  onClose: () => void
  labels?: {
    title?: string
    description?: string
    loading?: string
    cancel?: string
    continue?: string
  }
}) {
  const { t } = useTranslation('sites')
  return (
    <NestedDialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
      <DialogHeader className="border-b-0 pb-2"><DialogTitle>{labels?.title ?? t(`${copyPrefix}.regionDialog.title`)}</DialogTitle></DialogHeader>
      <DialogBody className="space-y-3 pt-0">
        <DialogDescription className="mt-0">{labels?.description ?? t(`${copyPrefix}.regionDialog.description`)}</DialogDescription>
        {loading ? (
          <p className="flex items-center gap-2 text-body text-label-2"><LoaderCircle className="h-4 w-4 animate-spin" />{labels?.loading ?? t(`${copyPrefix}.loading`)}</p>
        ) : error ? (
          <p className="text-body text-danger">{error}</p>
        ) : (
          <div role="radiogroup" className="space-y-2">
            {regions.map((region) => {
              const selected = selectedId === region.id
              return (
                <button
                  key={region.id}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  onClick={() => onSelect(region.id)}
                  className={cn(
                    'w-full rounded-inner border px-3 py-2.5 text-left transition-colors',
                    selected ? 'border-tint bg-tint/5' : 'border-separator bg-elevated-1 hover:border-tint/50 hover:bg-fill-3',
                  )}
                >
                  <span className="block text-body font-medium text-label">{region.label}</span>
                  {region.hint ? <span className="block text-footnote text-label-2">{region.hint}</span> : null}
                </button>
              )
            })}
          </div>
        )}
      </DialogBody>
      <DialogFooter
        className="border-t-0 pt-2"
        cancel={<Button variant="secondary" onClick={onClose}>{labels?.cancel ?? t(`${copyPrefix}.cancel`)}</Button>}
        confirm={(
          <Button disabled={confirmDisabled} onClick={onConfirm}>
            {labels?.continue ?? t(`${copyPrefix}.regionDialog.continue`)}
          </Button>
        )}
      />
    </NestedDialog>
  )
}

function NestedDialog({
  open,
  onOpenChange,
  children,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  children: ReactNode
}) {
  const nestedZ = useNestedDialogClassNames()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {typeof document === 'undefined' ? children : (
        <DialogContent presentation="center" size="sm" overlayClassName={nestedZ.overlayClassName} className={nestedZ.className}>
          {children}
        </DialogContent>
      )}
    </Dialog>
  )
}

export function AccountProviderDraw({
  site,
  capabilities,
  onOpenChange,
  iconURL,
  typeLabel,
}: {
  site: Site | null
  capabilities: SiteAccountCapabilities | null
  onOpenChange: (open: boolean) => void
  iconURL?: string | null
  typeLabel?: string | null
}) {
  const { t, i18n } = useTranslation('sites')
  const queryClient = useQueryClient()
  const [deleting, setDeleting] = useState<SiteAccount | null>(null)
  const [login, setLogin] = useState<LoginState | null>(null)
  const [modelsAccount, setModelsAccount] = useState<SiteAccount | null>(null)
  const [importOpen, setImportOpen] = useState(false)
  const [regionPickOpen, setRegionPickOpen] = useState(false)
  const [pickedRegion, setPickedRegion] = useState('')
  const open = Boolean(site)
  const builtin = !site || isBuiltinAccountSite(site.site_type)
  const prefix: AccountCopyPrefix = builtin ? accountCopyPrefix(site?.site_type) : 'accounts'
  const loginKind = capabilities?.login_kind ?? 'oauth2_device_code'
  const supportsRegions = Boolean(capabilities?.supports_regions)
  const supportsImport = Boolean(capabilities?.supports_import)
  const operationFlags = accountOperationFlags(builtin ? undefined : capabilities?.ui?.operations, supportsImport)
  const texts = builtin ? undefined : capabilities?.ui?.texts
  function accountLabel(suffix: string, textKey: string, vars?: Record<string, string | number>) {
    if (!builtin) {
      const custom = accountUIText(texts, textKey, i18n.language)
      if (custom) return fillAccountText(custom, vars)
    }
    return vars ? t(`${prefix}.${suffix}`, vars) : t(`${prefix}.${suffix}`)
  }
  const nestedOpen = Boolean(login) || Boolean(deleting) || Boolean(modelsAccount) || importOpen || regionPickOpen
  const queryKey = site ? sitesQueryKeys.accounts(site.id) : [...sitesQueryKeys.all, 'accounts', 'none']

  const accountsQuery = useQuery({
    queryKey,
    queryFn: () => site ? listAccounts(site.id) : Promise.resolve({ items: EMPTY_ACCOUNTS, meta: { count: 0 } }),
    enabled: open,
  })

  const regionsQuery = useQuery({
    queryKey: site ? sitesQueryKeys.accountRegions(site.id) : [...sitesQueryKeys.all, 'accounts', 'regions', 'none'],
    queryFn: () => site ? listAccountRegions(site.id) : Promise.resolve({ items: EMPTY_REGIONS, meta: { count: 0, default: '' } }),
    enabled: open && supportsRegions,
    staleTime: Infinity,
  })
  const regions = regionsQuery.data?.items ?? EMPTY_REGIONS
  const defaultRegion = regionsQuery.data?.meta?.default ?? regions[0]?.id ?? ''

  function regionLabel(id: string) {
    const region = regions.find((item) => item.id === id)
    const fallback = region ? (i18n.language.startsWith('en') ? region.label_en : region.label) : id
    return t(`${prefix}.region.${id}`, { defaultValue: fallback })
  }

  const updateMutation = useMutation({
    mutationFn: ({ account, enabled }: { account: SiteAccount; enabled: boolean }) => {
      if (!site) throw new Error('site required')
      return updateAccount(site.id, account.credential_id, { enabled })
    },
    onSuccess: async () => {
      await refreshAccountQueries()
      toast.success(t(`${prefix}.toast.updated`))
    },
    onError: (error) => toast.error(t(`${prefix}.toast.updateFailed`), { description: error.message }),
  })

  const deleteMutation = useMutation({
    mutationFn: (account: SiteAccount) => {
      if (!site) throw new Error('site required')
      return deleteAccount(site.id, account.credential_id)
    },
    onSuccess: async () => {
      setDeleting(null)
      await refreshAccountQueries()
      toast.success(t(`${prefix}.toast.deleted`))
    },
    onError: (error) => toast.error(t(`${prefix}.toast.deleteFailed`), { description: error.message }),
  })

  const refreshMutation = useMutation({
    mutationFn: (account: SiteAccount) => {
      if (!site) throw new Error('site required')
      return refreshAccount(site.id, account.credential_id)
    },
    onSuccess: async (result) => {
      await refreshAccountQueries()
      if (result.account.sync_message) {
        toast.warning(t(`${prefix}.toast.refreshFailed`), { description: result.account.sync_message })
      } else {
        toast.success(t(`${prefix}.toast.refreshed`))
      }
    },
    onError: (error) => toast.error(t(`${prefix}.toast.refreshFailed`), { description: error.message }),
  })

  const refreshAllMutation = useMutation({
    mutationFn: () => {
      if (!site) throw new Error('site required')
      return refreshAccounts(site.id)
    },
    onSuccess: async (result) => {
      await refreshAccountQueries()
      const failed = result.meta?.failed ?? 0
      if (failed > 0) {
        toast.warning(t(`${prefix}.toast.refreshAllPartial`), {
          description: t(`${prefix}.toast.refreshAllPartialDesc`, { count: failed }),
        })
      } else {
        toast.success(t(`${prefix}.toast.refreshedAll`))
      }
    },
    onError: (error) => toast.error(t(`${prefix}.toast.refreshAllFailed`), { description: error.message }),
  })

  const updateModelMutation = useMutation({
    mutationFn: ({ credentialId, update }: { credentialId: string; update: AccountModelUpdate }) => {
      if (!site) throw new Error('site required')
      return updateAccountModel(site.id, credentialId, update)
    },
    onSuccess: async (result) => {
      setModelsAccount((current) => (current?.credential_id === result.account.credential_id ? result.account : current))
      await refreshAccountQueries()
    },
    onError: (error) => toast.error(t(`${prefix}.toast.updateFailed`), { description: error.message }),
  })

  const bulkModelMutation = useMutation({
    mutationFn: async ({ credentialId, models, enabled }: { credentialId: string; models: string[]; enabled: boolean }) => {
      if (!site) throw new Error('site required')
      let latest: Awaited<ReturnType<typeof updateAccountModel>> | null = null
      let firstError: Error | null = null
      for (const model of models) {
        try {
          latest = await updateAccountModel(site.id, credentialId, { model, enabled })
        } catch (error) {
          firstError ??= error instanceof Error ? error : new Error(String(error))
        }
      }
      if (!latest) throw firstError ?? new Error('no models to update')
      return { latest, firstError }
    },
    onSuccess: async ({ latest, firstError }) => {
      setModelsAccount((current) => (current?.credential_id === latest.account.credential_id ? latest.account : current))
      await refreshAccountQueries()
      if (firstError) toast.error(t(`${prefix}.toast.updateFailed`), { description: firstError.message })
    },
    onError: (error) => toast.error(t(`${prefix}.toast.updateFailed`), { description: error.message }),
  })

  async function refreshAccountQueries() {
    await queryClient.invalidateQueries({ queryKey })
    await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.list() })
    if (site) await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.models(site.id) })
  }

  async function startLogin(purpose: LoginState['purpose'], credentialId?: string, region?: string) {
    if (!site) return
    setLogin({ status: 'starting', purpose, credentialId })
    try {
      const result = await startAccountLogin(site.id, region)
      setLogin({ status: 'waiting', purpose, credentialId, info: result.login })
      window.open(loginOpenURL(result.login), '_blank', 'noopener')
    } catch (error) {
      setLogin({ status: 'error', purpose, credentialId, error: (error as Error).message })
    }
  }

  useEffect(() => {
    if (!site || !login || login.status !== 'waiting' || !login.info || !login.info.session) return
    let cancelled = false
    const info = login.info
    const session = info.session
    const credentialId = login.credentialId
    const intervalMs = loginPollIntervalMs(info.kind, info.interval)
    const timer = setInterval(async () => {
      try {
        const result = await pollAccountLogin(site.id, session, credentialId)
        if (cancelled) return
        const outcome = loginPollOutcome(result.login)
        if (outcome === 'complete') {
          setLogin(null)
          await refreshAccountQueries()
          toast.success(t(`${prefix}.toast.created`))
        } else if (outcome === 'error') {
          setLogin((current) => current ? { ...current, status: 'error', error: result.login.status } : null)
        }
      } catch (error) {
        if (cancelled) return
        setLogin((current) => current ? { ...current, status: 'error', error: (error as Error).message } : null)
      }
    }, intervalMs)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [site?.id, login?.status, login?.info?.session])

  const items = accountsQuery.data?.items ?? EMPTY_ACCOUNTS
  const activeModelsAccount = modelsAccount
    ? (items.find((account) => account.credential_id === modelsAccount.credential_id) ?? modelsAccount)
    : null
  const regionOptions = regions.map((region) => {
    const hint = t(`${prefix}.regionDialog.${region.id}Hint`, { defaultValue: '' })
    return { id: region.id, label: regionLabel(region.id), hint: hint || undefined }
  })
  const titleText = accountLabel('title', 'title')
  const title = builtin ? titleText : (
    <span className="inline-flex min-w-0 items-center gap-2">
      <SiteTypeIcon
        src={iconURL}
        name={typeLabel || titleText}
        muted={site?.provider_status === 'unavailable'}
        className="h-5 w-5 shrink-0 rounded-control"
      />
      <span className="truncate">{titleText}</span>
    </span>
  )
  const loginLabels = builtin ? undefined : {
    title: accountLabel(login?.purpose === 'reauth' ? 'login.reauthTitle' : 'login.title', login?.purpose === 'reauth' ? 'login_reauth_title' : 'login_title'),
    starting: accountLabel('login.starting', 'login_starting'),
    instructions: accountLabel('login.instructions', 'login_instructions'),
    authorize: accountLabel('login.authorize', 'login_authorize'),
    waiting: accountLabel('login.waiting', 'login_waiting'),
    cancel: accountLabel('cancel', 'cancel'),
  }
  const regionLabels = builtin ? undefined : {
    title: accountLabel('regionDialog.title', 'region_title'),
    description: accountLabel('regionDialog.description', 'region_description'),
    continue: accountLabel('regionDialog.continue', 'region_continue'),
    cancel: accountLabel('cancel', 'cancel'),
    loading: accountLabel('loading', 'loading'),
  }

  return (
    <>
      <AccountsListDialog
        open={open}
        title={title}
        nestedOpen={nestedOpen}
        onCloseInnermost={() => {
          if (importOpen) setImportOpen(false)
          else if (regionPickOpen) setRegionPickOpen(false)
          else if (login) setLogin(null)
          else if (deleting) setDeleting(null)
          else if (modelsAccount) setModelsAccount(null)
        }}
        onOpenChange={(next) => {
          if (!next) {
            setDeleting(null)
            setLogin(null)
            setModelsAccount(null)
            setImportOpen(false)
            setRegionPickOpen(false)
          }
          onOpenChange(next)
        }}
        headerActions={(
          <AccountHeaderActions
            addLabel={accountLabel('add', 'add')}
            addDisabled={Boolean(login)}
            onAdd={operationFlags.add ? () => {
              if (supportsRegions) {
                setPickedRegion((current) => current || defaultRegion)
                setRegionPickOpen(true)
                return
              }
              void startLogin('add')
            } : undefined}
            importLabel={operationFlags.import ? accountLabel('import', 'import') : undefined}
            importDisabled={Boolean(login)}
            onImport={operationFlags.import ? () => setImportOpen(true) : undefined}
            refreshAllLabel={accountLabel('refreshAll', 'refresh_all')}
            refreshingAll={refreshAllMutation.isPending}
            showRefreshAll={operationFlags.refresh}
            onRefreshAll={() => refreshAllMutation.mutate()}
          />
        )}
      >
        {accountsQuery.isLoading ? (
          <p className="py-10 text-center text-body text-label-2">{accountLabel('loading', 'loading')}</p>
        ) : accountsQuery.isError ? (
          <p className="py-10 text-center text-body text-danger">{accountsQuery.error.message}</p>
        ) : items.length ? (
          <AccountList>
            {items.map((account) => {
              const refreshing = refreshMutation.isPending && refreshMutation.variables?.credential_id === account.credential_id
              const syncBadge = accountSyncBadge(prefix, account.sync_status)
              const reauthRequired = account.sync_status?.toLowerCase() === 'reauth_required'
              const name = account.masked_token || accountLabel('unnamed', 'unnamed')
              const view = builtin
                ? grokRowView(account)
                : pluginRowView(account, capabilities?.ui?.row_pills, i18n.language)
              const refreshLabel = accountLabel('refresh', 'refresh')
              const deleteLabel = accountLabel('delete', 'delete')
              const reauthorizeLabel = accountLabel('reauthorize', 'reauthorize')
              return (
                <AccountRow
                  key={account.credential_id}
                  name={name}
                  badges={<Badge variant="secondary" className={`shrink-0 px-1.5 py-0 text-[10px] ${syncBadge.className}`}>{syncBadge.label(t)}</Badge>}
                  enabled={account.enabled}
                  toggleDisabled={updateMutation.isPending || reauthRequired}
                  toggleLabel={accountLabel('toggleLabel', 'toggle_label', { name })}
                  showToggle={operationFlags.toggle}
                  showModels={operationFlags.models}
                  onToggle={(enabled) => updateMutation.mutate({ account, enabled })}
                  pills={view.pills}
                  detail={view.detail}
                  message={account.sync_message || undefined}
                  modelCountLabel={`${account.model_count ?? account.models?.length ?? 0} ${t('accountModels.available')}`}
                  onOpenModels={() => setModelsAccount(account)}
                  actions={(
                    <>
                      {operationFlags.reauth && reauthRequired ? (
                        <Button
                          size="icon"
                          variant="ghost"
                          className="h-7 w-7 text-danger"
                          disabled={Boolean(login)}
                          title={reauthorizeLabel}
                          aria-label={reauthorizeLabel}
                          onClick={() => {
                            const region = supportsRegions ? accountRegionId(account) : undefined
                            void startLogin('reauth', account.credential_id, region)
                          }}
                        >
                          <KeyRound className="h-3.5 w-3.5" />
                        </Button>
                      ) : null}
                      {operationFlags.refresh ? (
                        <Button
                          size="icon"
                          variant="ghost"
                          className="h-7 w-7"
                          disabled={refreshing}
                          title={refreshLabel}
                          aria-label={refreshLabel}
                          onClick={() => refreshMutation.mutate(account)}
                        >
                          {refreshing ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}
                        </Button>
                      ) : null}
                      {operationFlags.delete ? (
                        <Button
                          size="icon"
                          variant="ghost"
                          className="h-7 w-7 text-danger hover:bg-danger/10 hover:text-danger"
                          disabled={deleteMutation.isPending}
                          title={deleteLabel}
                          aria-label={deleteLabel}
                          onClick={() => setDeleting(account)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      ) : null}
                    </>
                  )}
                />
              )
            })}
          </AccountList>
        ) : (
          <p className="py-10 text-center text-body text-label-2">{accountLabel('empty', 'empty')}</p>
        )}
      </AccountsListDialog>

      {supportsRegions ? (
        <AccountRegionDialog
          open={regionPickOpen}
          copyPrefix={prefix}
          regions={regionOptions}
          selectedId={pickedRegion || defaultRegion}
          loading={regionsQuery.isLoading}
          error={regionsQuery.isError ? regionsQuery.error.message : undefined}
          confirmDisabled={!regions.length || Boolean(login)}
          labels={regionLabels}
          onSelect={setPickedRegion}
          onConfirm={() => {
            const region = pickedRegion || defaultRegion
            setRegionPickOpen(false)
            void startLogin('add', undefined, region)
          }}
          onClose={() => setRegionPickOpen(false)}
        />
      ) : null}

      {site && operationFlags.import ? (
        <AccountImportDialog
          open={importOpen}
          siteId={site.id}
          regionLabel={regionLabel}
          texts={texts}
          language={i18n.language}
          onOpenChange={setImportOpen}
          onImported={refreshAccountQueries}
        />
      ) : null}

      <AccountLoginDialog
        login={login}
        kind={loginKind}
        onClose={() => setLogin(null)}
        copyPrefix={builtin ? undefined : 'accounts'}
        labels={loginLabels}
        showVerificationURL={!builtin && loginKind === 'oauth2_device_code'}
      />

      <NestedDialog open={Boolean(deleting)} onOpenChange={(next) => { if (!next && !deleteMutation.isPending) setDeleting(null) }}>
        <DialogHeader className="border-b-0 pb-2"><DialogTitle>{accountLabel('deleteDialog.title', 'delete_title')}</DialogTitle></DialogHeader>
        <DialogBody className="pt-0">
          <DialogDescription className="mt-0">{accountLabel('deleteDialog.confirm', 'delete_confirm', { name: deleting?.masked_token || accountLabel('unnamed', 'unnamed') })}</DialogDescription>
        </DialogBody>
        <DialogFooter
          className="border-t-0 pt-2"
          cancel={<Button variant="secondary" disabled={deleteMutation.isPending} onClick={() => setDeleting(null)}>{accountLabel('cancel', 'cancel')}</Button>}
          confirm={(
            <Button variant="destructive" disabled={deleteMutation.isPending} onClick={() => { if (deleting) deleteMutation.mutate(deleting) }}>
              {deleteMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}{accountLabel('deleteDialog.delete', 'delete_delete')}
            </Button>
          )}
        />
      </NestedDialog>

      <AccountModelsDialog
        key={activeModelsAccount?.credential_id ?? 'account-models'}
        open={Boolean(activeModelsAccount)}
        title={activeModelsAccount ? `${activeModelsAccount.masked_token} ${t('accountModels.available')}` : t('accountModels.available')}
        models={activeModelsAccount?.models ?? []}
        backLabel={t('accountModels.backToAccounts')}
        pendingModel={updateModelMutation.isPending ? updateModelMutation.variables?.update.model : undefined}
        bulkPending={bulkModelMutation.isPending}
        protocolPending={updateModelMutation.isPending && Boolean(updateModelMutation.variables?.update.endpointMode)}
        onBack={() => setModelsAccount(null)}
        onClose={() => setModelsAccount(null)}
        onToggleModel={(model, enabled) => {
          if (!activeModelsAccount) return
          updateModelMutation.mutate({ credentialId: activeModelsAccount.credential_id, update: { model: model.upstream_name, enabled } })
        }}
        onBulkToggle={(models, enabled) => {
          if (!activeModelsAccount) return
          bulkModelMutation.mutate({ credentialId: activeModelsAccount.credential_id, models, enabled })
        }}
        onSaveProtocol={(update) => {
          if (!activeModelsAccount) return
          updateModelMutation.mutate({ credentialId: activeModelsAccount.credential_id, update })
        }}
      />
    </>
  )
}

function accountRegionId(account: SiteAccount) {
  if ('region' in account && typeof account.region === 'string' && account.region) return account.region
  const fromFields = account.fields?.region
  return typeof fromFields === 'string' && fromFields ? fromFields : undefined
}

function accountSyncBadge(prefix: AccountCopyPrefix, status?: string | null): { className: string; label: (t: (key: string) => string) => string } {
  switch ((status || '').toLowerCase()) {
    case 'synced':
      return { className: 'border-success/40 text-success', label: (t) => t(`${prefix}.sync.synced`) }
    case 'failed':
      return { className: 'border-danger/40 text-danger', label: (t) => t(`${prefix}.sync.failed`) }
    case 'reauth_required':
      return { className: 'border-danger/40 text-danger', label: (t) => t(`${prefix}.sync.reauthRequired`) }
    case 'stale':
      return { className: 'border-warning/40 text-warning', label: (t) => t(`${prefix}.sync.stale`) }
    case 'partial':
      return { className: 'border-warning/40 text-warning', label: (t) => t(`${prefix}.sync.partial`) }
    default:
      return { className: 'text-label-2', label: (t) => t(`${prefix}.sync.unknown`) }
  }
}
