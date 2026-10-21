import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { focusManager, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, LoaderCircle, Plus, RotateCcw, Search, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router-dom'
import { downstreamAPIKeyQueryKeys } from '@/features/api-keys/api/api-keys'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { MobileFilterBar } from '@/components/common/mobile-filter-bar'
import { PageHeader } from '@/components/common/page-header'
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
import { Input } from '@/components/ui/input'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { toast } from '@/lib/toast'
import {
  completeClaudeCodeOAuth,
  completeOAuthFromCallback,
  exportOAuthConnection,
  importOAuthAccounts,
  listOAuthConnections,
  oauthQueryKeys,
  refreshOAuthConnection,
  startAntigravityOAuth,
  startClaudeCodeOAuth,
  startCodexOAuth,
  type ImportOAuthAccountsInput,
  type OAuthConnectionListItem,
  type OAuthProvider,
} from '@/features/oauth/api/oauth'
import { OAuthConnectionCard } from '@/features/oauth/components/oauth-connection-card'
import { OAuthCreateDraw } from '@/features/oauth/components/oauth-create-draw'
import { OAuthDisclaimerDialog } from '@/features/oauth/components/oauth-disclaimer-dialog'
import { OAuthImportSheet } from '@/features/oauth/components/oauth-import-sheet'
import { cn } from '@/lib/utils'
import { OAuthWorkspaceSkeleton } from '@/features/oauth/components/oauth-workspace-skeleton'
import { SiteModelTestSheet } from '@/features/sites/components/site-model-test-sheet'
import { ChannelSplitDialog } from '@/features/usage/components/channel-split-dialog'
import {
  DEFAULT_CREATE_STATE,
  readOAuthUpdatedAtMode,
  generateOAuthSiteName,
  generateOAuthSiteSlug,
  saveOAuthUpdatedAtMode,
  oauthConnectionSearchText,
  oauthConnectionEffectiveStatus,
  getOAuthSources,
  oauthPageURL,
  parseOAuthCallbackInput,
  providerLabel,
  sortOAuthConnectionsForDisplay,
  type OAuthUpdatedAtDisplayMode,
} from '@/features/oauth/lib/oauth-utils'
import type { OAuthCreateState, ImportResult } from '@/features/oauth/lib/types'
import { deleteSite, listSites, sitesQueryKeys, updateSiteEnabled, type Site, type SiteModel } from '@/features/sites/api/sites'
import { listSiteGroups, siteGroupQueryKeys, type SiteGroup } from '@/features/settings/api/site-groups'
import type { ChannelSplitTarget } from '@/features/usage/api/channel-split'
import { useMobileLayout } from '@/hooks/use-media-query'

const EMPTY_CONNECTIONS: OAuthConnectionListItem[] = []
const EMPTY_SITE_MODELS: SiteModel[] = []
const EMPTY_SITE_GROUPS: SiteGroup[] = []
const EMPTY_SITES: Site[] = []

type OAuthDisclaimerAction =
  | { type: 'create' }
  | { type: 'import' }
  | { type: 'reauth'; item: OAuthConnectionListItem }

const PROVIDER_GROUP_ORDER: string[] = ['codex', 'claude_code', 'antigravity']

type StartOAuthFlowInput = {
  source: OAuthProvider
  site?: {
    id?: string | null
    name?: string | null
    slug?: string | null
  } | null
  proxyId?: string | null
  mode?: 'create' | 'reauth'
}

export function OAuthWorkspace() {
  const { t } = useTranslation('oauth')
  const isMobile = useMobileLayout()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [search, setSearch] = useState('')
  const [providerFilter, setProviderFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [createOpen, setCreateOpen] = useState(false)
  const [disclaimer, setDisclaimer] = useState<OAuthDisclaimerAction | null>(null)
  const [createState, setCreateState] = useState<OAuthCreateState>(DEFAULT_CREATE_STATE)
  const [createMode, setCreateMode] = useState<'create' | 'reauth'>('create')
  const [createProxyId, setCreateProxyId] = useState('direct')
  const [deleteTarget, setDeleteTarget] = useState<OAuthConnectionListItem | null>(null)
  const [importSheetOpen, setImportSheetOpen] = useState(false)
  const [importResult, setImportResult] = useState<ImportResult | null>(null)
  const [testSite, setTestSite] = useState<Site | null>(null)
  const [usageSplitTarget, setUsageSplitTarget] = useState<ChannelSplitTarget | null>(null)
  const [errorCollapsed, setErrorCollapsed] = useState(true)
  const [collapsedProviderGroups, setCollapsedProviderGroups] = useState<Record<string, boolean>>({})
  const [updatedAtMode, setUpdatedAtMode] = useState<OAuthUpdatedAtDisplayMode>(readOAuthUpdatedAtMode)
  const [now, setNow] = useState(() => Date.now())

  const connectionsQuery = useQuery({
    queryKey: oauthQueryKeys.connections(),
    queryFn: listOAuthConnections,
    refetchOnWindowFocus: 'always',
  })
  const siteGroupsQuery = useQuery({
    queryKey: siteGroupQueryKeys.list(),
    queryFn: listSiteGroups,
    refetchOnWindowFocus: 'always',
  })
  const sitesQuery = useQuery({
    queryKey: sitesQueryKeys.list('all', 'with_requests'),
    queryFn: () => listSites({ oauth: 'all', deleted: 'with_requests' }),
    refetchOnWindowFocus: 'always',
  })

  const connections = useMemo(
    () => sortOAuthConnectionsForDisplay(connectionsQuery.data?.items ?? EMPTY_CONNECTIONS),
    [connectionsQuery.data?.items],
  )
  const siteGroups = siteGroupsQuery.data?.items ?? EMPTY_SITE_GROUPS
  // 来源 Tab 只列出已添加过连接的类型，按分组顺序排列
  const availableProviders = useMemo(() => {
    const present = new Set(connections.map((item) => item.provider))
    return [
      ...PROVIDER_GROUP_ORDER.filter((provider) => present.has(provider)),
      ...[...present].filter((provider) => !PROVIDER_GROUP_ORDER.includes(provider)),
    ]
  }, [connections])
  const sourceTabsRef = useRef<HTMLDivElement>(null)
  const [sourceTabsOverflowing, setSourceTabsOverflowing] = useState(false)
  useLayoutEffect(() => {
    const element = sourceTabsRef.current
    if (!element) return
    const update = () => {
      const list = element.querySelector('[role="tablist"]')
      if (!list) return
      // 当前是否已留出 32px 淡出区：留出时内容区要扣掉它，避免切换内边距时来回抖动
      const reserved = element.classList.contains('pr-8') ? 32 : 0
      setSourceTabsOverflowing(list.scrollWidth > element.clientWidth - reserved + 0.5)
    }
    update()
    const observer = new ResizeObserver(update)
    observer.observe(element)
    if (element.parentElement) observer.observe(element.parentElement)
    return () => observer.disconnect()
  }, [availableProviders, isMobile])
  // 选中的来源被删光后自动回到「全部」
  const effectiveProviderFilter = providerFilter !== 'all' && availableProviders.includes(providerFilter) ? providerFilter : 'all'
  const filteredConnections = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return connections.filter((item) => {
      const matchSearch = !keyword || oauthConnectionSearchText(item).includes(keyword)
      const matchProvider = effectiveProviderFilter === 'all' || item.provider === effectiveProviderFilter
      const matchStatus = statusFilter === 'all' || oauthConnectionEffectiveStatus(item) === statusFilter
      return matchSearch && matchProvider && matchStatus
    })
  }, [connections, search, effectiveProviderFilter, statusFilter])

  const normalConnections = useMemo(
    () => filteredConnections.filter((item) => {
      const status = oauthConnectionEffectiveStatus(item)
      return status === 'connected' || status === 'pending_sync'
    }),
    [filteredConnections],
  )
  const errorConnections = useMemo(
    () => filteredConnections.filter((item) => {
      const status = oauthConnectionEffectiveStatus(item)
      return status !== 'connected' && status !== 'pending_sync'
    }),
    [filteredConnections],
  )
  const normalConnectionGroups = useMemo(() => {
    const grouped = new Map<string, OAuthConnectionListItem[]>()
    for (const item of normalConnections) {
      const list = grouped.get(item.provider)
      if (list) list.push(item)
      else grouped.set(item.provider, [item])
    }
    const orderedProviders = [
      ...PROVIDER_GROUP_ORDER.filter((provider) => grouped.has(provider)),
      ...[...grouped.keys()].filter((provider) => !PROVIDER_GROUP_ORDER.includes(provider)),
    ]
    return orderedProviders.map((provider) => ({ provider, items: grouped.get(provider) ?? [] }))
  }, [normalConnections])
  const hasPendingSyncConnections = connections.some((item) => item.status === 'pending_sync')

  const startMutation = useMutation({
    mutationFn: async (input: StartOAuthFlowInput) => {
      const redirectURL = oauthPageURL()
      const payload = {
        siteID: input.site?.id,
        siteName: input.site?.name ?? generateOAuthSiteName(input.source),
        siteSlug: input.site?.slug ?? generateOAuthSiteSlug(input.source),
        proxyId: input.proxyId,
        redirectURL,
        failureRedirectURL: redirectURL,
      }
      if (input.source === 'antigravity') return startAntigravityOAuth(payload)
      if (input.source === 'claude_code') return startClaudeCodeOAuth(payload)
      return startCodexOAuth(payload)
    },
    onSuccess: (result) => {
      setCreateState((current) => ({
        ...current,
        authorize: result,
      }))
      toast.success(t('workspace.toast.linkGenerated'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.linkFailed'), {
        description: error.message,
      })
    },
  })

  const completeMutation = useMutation({
    mutationFn: async (rawCallback: string) => {
      const parsed = parseOAuthCallbackInput(rawCallback, t)

      if (parsed.kind === 'redirect_result') {
        return {
          connectionId: parsed.connectionId,
          siteId: parsed.siteId,
          message: parsed.message,
          status: parsed.status,
        }
      }

      if (parsed.kind === 'callback_error') {
        throw new Error(parsed.message)
      }

      if (parsed.kind === 'authorization_result') {
        if (createState.authorize?.provider !== 'claude_code' || !createState.authorize.session_id) {
          throw new Error(t('callback.missingClaudeCodeSession'))
        }
        const result = await completeClaudeCodeOAuth(createState.authorize.session_id, parsed.authorizationResult, createProxyId === 'direct' ? '' : createProxyId)
        return {
          connectionId: result.connection_id ?? null,
          siteId: result.site_id ?? null,
          message: result.message ?? '',
          status: result.status === 'partial' ? 'partial' as const : 'success' as const,
        }
      }

      const provider = createState.authorize?.provider ?? inferOAuthCallbackProvider(parsed.callbackURL)
      if (provider === 'claude_code') {
        if (!createState.authorize?.session_id) {
          throw new Error(t('callback.missingClaudeCodeSession'))
        }
        const authorizationResult = claudeCodeAuthorizationResultFromURL(parsed.callbackURL)
        const result = await completeClaudeCodeOAuth(createState.authorize.session_id, authorizationResult, createProxyId === 'direct' ? '' : createProxyId)
        return {
          connectionId: result.connection_id ?? null,
          siteId: result.site_id ?? null,
          message: result.message ?? '',
          status: result.status === 'partial' ? 'partial' as const : 'success' as const,
        }
      }
      const result = await completeOAuthFromCallback(provider, parsed.callbackURL, createProxyId === 'direct' ? '' : createProxyId)
      return {
        connectionId: result.connection_id ?? null,
        siteId: result.site_id ?? null,
        message: result.message ?? '',
        status: result.status === 'partial' ? 'partial' as const : 'success' as const,
      }
    },
    onSuccess: async (result) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: oauthQueryKeys.connections() }),
        queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all }),
        queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() }),
      ])

      if (result.connectionId) {
        await queryClient.invalidateQueries({ queryKey: oauthQueryKeys.detail(result.connectionId) })
      } else {
        await queryClient.invalidateQueries({ queryKey: oauthQueryKeys.all })
      }

      setCreateOpen(false)
      setCreateState(DEFAULT_CREATE_STATE)
      toast.success(result.status === 'partial' ? t('workspace.toast.authPartial') : t('workspace.toast.authSuccess'), {
        description: result.message || undefined,
      })
    },
    onError: (error) => {
      toast.error(t('workspace.toast.callbackFailed'), {
        description: error.message,
      })
    },
  })

  const refreshMutation = useMutation({
    mutationFn: refreshOAuthConnection,
    onSuccess: async (result, connectionId) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: oauthQueryKeys.connections() }),
        queryClient.invalidateQueries({ queryKey: oauthQueryKeys.detail(connectionId) }),
        queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all }),
        queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() }),
      ])
      if (result.refresh?.site?.id) {
        await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.detail(result.refresh.site.id) })
      }
      toast.success(t('workspace.toast.refreshed'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.refreshFailed'), {
        description: error.message,
      })
    },
  })

  const toggleEnabledMutation = useMutation({
    mutationFn: ({ siteId, enabled }: { siteId: string; enabled: boolean }) => updateSiteEnabled(siteId, { enabled }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: oauthQueryKeys.connections() }),
        queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all }),
        queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() }),
      ])
    },
    onError: (error) => {
      toast.error(t('workspace.toast.statusFailed'), { description: error.message })
    },
  })

  const refreshAllMutation = useMutation({
    mutationFn: async () => {
      const ids = connections.map((connection) => connection.id)
      await Promise.allSettled(ids.map((id) => refreshOAuthConnection(id)))
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: oauthQueryKeys.all })
      await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all })
      await queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() })
      toast.success(t('workspace.toast.refreshAllDone'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.refreshAllFailed'), { description: error.message })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: ({ siteId }: { siteId: string; connectionId: string }) => deleteSite(siteId),
    onSuccess: (_, { siteId, connectionId }) => {
      queryClient.setQueryData<Awaited<ReturnType<typeof listOAuthConnections>>>(
        oauthQueryKeys.connections(),
        (current) => {
          if (!current) return current
          const items = current.items.filter((item) => item.id !== connectionId)
          if (items.length === current.items.length) return current
          return {
            ...current,
            items,
            meta: current.meta
              ? {
                  ...current.meta,
                  count: typeof current.meta.count === 'number'
                    ? Math.max(0, current.meta.count - 1)
                    : current.meta.count,
                }
              : current.meta,
          }
        },
      )
      queryClient.removeQueries({ queryKey: oauthQueryKeys.detail(connectionId) })
      queryClient.removeQueries({ queryKey: sitesQueryKeys.detail(siteId) })
      queryClient.removeQueries({ queryKey: sitesQueryKeys.models(siteId) })
      for (const oauth of ['exclude', 'all', 'only'] as const) {
        queryClient.setQueryData(
          sitesQueryKeys.list(oauth),
          (current: { items: Site[]; meta: { count: number } } | undefined) => {
            if (!current) return current
            const items = current.items.filter((site) => site.id !== siteId)
            return items.length === current.items.length
              ? current
              : { ...current, items, meta: { ...current.meta, count: items.length } }
          },
        )
      }
      setDeleteTarget(null)
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: oauthQueryKeys.connections(), exact: true }),
        queryClient.invalidateQueries({ queryKey: [...sitesQueryKeys.all, 'list'] }),
        queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list(), exact: true }),
        queryClient.invalidateQueries({ queryKey: downstreamAPIKeyQueryKeys.list(), exact: true }),
        queryClient.invalidateQueries({ queryKey: [...sitesQueryKeys.all, 'canonical-models'] }),
        queryClient.invalidateQueries({ queryKey: [...sitesQueryKeys.all, 'routes-matrix'] }),
        queryClient.invalidateQueries({ queryKey: [...sitesQueryKeys.all, 'all-pricings'], exact: true }),
        queryClient.invalidateQueries({ queryKey: ['settings', 'model-prices'], exact: true }),
      ])
      toast.success(t('workspace.toast.deleted'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.deleteFailed'), { description: error.message })
    },
  })

  const exportMutation = useMutation({
    mutationFn: exportOAuthConnection,
    onSuccess: ({ blob, filename }) => {
      downloadBlob(blob, filename ?? 'codex-oauth.json')
      toast.success(t('workspace.toast.exported'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.exportFailed'), { description: error.message })
    },
  })

  const importMutation = useMutation({
    mutationFn: (input: ImportOAuthAccountsInput) => importOAuthAccounts(input),
    onSuccess: async (result) => {
      setImportResult(result)
      const importedConnectionIds = result.items
        .map((item) => item.connection_id)
        .filter((connectionId): connectionId is string => Boolean(connectionId))
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: oauthQueryKeys.all }),
        queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all }),
        queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() }),
        ...importedConnectionIds.map((connectionId) => (
          queryClient.invalidateQueries({ queryKey: oauthQueryKeys.detail(connectionId) })
        )),
      ])
      toast.success(t('workspace.toast.importDone', { accepted: result.meta.accepted ?? result.meta.succeeded, queued: result.meta.queued ?? result.meta.succeeded, failed: result.meta.failed }))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.importFailed'), { description: error.message })
    },
  })

  useEffect(() => {
    saveOAuthUpdatedAtMode(updatedAtMode)
  }, [updatedAtMode])

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => focusManager.subscribe((focused) => {
    if (focused) setNow(Date.now())
  }), [])

  useEffect(() => {
    if (!hasPendingSyncConnections) return
    const timer = window.setInterval(() => {
      void queryClient.invalidateQueries({ queryKey: oauthQueryKeys.all })
      void queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all })
    }, 3_000)
    return () => window.clearInterval(timer)
  }, [hasPendingSyncConnections, queryClient])

  useEffect(() => {
    const status = searchParams.get('status')
    const message = searchParams.get('message')

    if (!status) return

    void Promise.all([
      queryClient.invalidateQueries({ queryKey: oauthQueryKeys.connections() }),
      queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all }),
      queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() }),
    ]).then(() => {
      if (status === 'success') {
        toast.success(t('workspace.toast.oauthSuccess'))
      } else if (status === 'partial') {
        toast.success(t('workspace.toast.oauthPartial'), {
          description: message || t('workspace.toast.oauthPartialDesc'),
        })
      } else {
        toast.error(t('workspace.toast.oauthFailed'), {
          description: message || t('workspace.toast.oauthFailedDesc'),
        })
      }
      setSearchParams({}, { replace: true })
    })
  }, [queryClient, searchParams, setSearchParams, t])

  function confirmDisclaimer() {
    const action = disclaimer
    setDisclaimer(null)
    if (!action) return
    if (action.type === 'create') {
      setCreateMode('create')
      setCreateProxyId('direct')
      setCreateOpen(true)
      return
    }
    if (action.type === 'import') {
      setImportResult(null)
      setImportSheetOpen(true)
      return
    }
    startReconnectOAuth(action.item)
  }

  function startReconnectOAuth(item: OAuthConnectionListItem) {
    if (item.provider !== 'codex' && item.provider !== 'antigravity' && item.provider !== 'claude_code') {
      toast.error(t('workspace.toast.linkFailed'), { description: item.provider })
      return
    }
    const siteID = item.site?.id ?? item.site_id
    if (!siteID) {
      toast.error(t('workspace.toast.linkFailed'), { description: t('workspace.toast.reauthMissingSite') })
      return
    }
    setCreateMode('reauth')
    setCreateOpen(true)
    setCreateProxyId(item.site?.proxy_id || 'direct')
    setCreateState({
      ...DEFAULT_CREATE_STATE,
      source: item.provider,
      authorize: null,
    })
    startMutation.mutate({
      source: item.provider,
      site: {
        id: siteID,
        name: item.site?.name,
        slug: item.site?.slug,
      },
      proxyId: item.site?.proxy_id || null,
      mode: 'reauth',
    })
  }

  function toggleProviderGroup(provider: string) {
    setCollapsedProviderGroups((current) => ({ ...current, [provider]: !current[provider] }))
  }

  function renderConnectionCard(item: OAuthConnectionListItem, mobile = false) {
    const groupLabel = siteGroupLabelForSite(item.site?.id ?? item.site_id, siteGroups)
    const cardProps = {
      item,
      groupLabel,
      refreshing: refreshAllMutation.isPending || (refreshMutation.isPending && refreshMutation.variables === item.id),
      toggling: toggleEnabledMutation.isPending && toggleEnabledMutation.variables?.siteId === item.site_id,
      deleting: deleteMutation.isPending && deleteMutation.variables?.connectionId === item.id,
      exporting: exportMutation.isPending && exportMutation.variables === item.id,
      onRefresh: () => refreshMutation.mutate(item.id),
      onExport: () => exportMutation.mutate(item.id),
      onOpenTest: (site: Site) => setTestSite(site),
      onOpenUsageSplit: () => setUsageSplitTarget({ type: 'oauth', id: item.id }),
      onToggleEnabled: (enabled: boolean) => {
        if (item.site_id) toggleEnabledMutation.mutate({ siteId: item.site_id, enabled })
      },
      onDelete: () => setDeleteTarget(item),
      onReconnect: () => setDisclaimer({ type: 'reauth', item }),
      updatedAtMode,
      now,
      onUpdatedAtModeChange: setUpdatedAtMode,
    }

    return <OAuthConnectionCard key={item.id} {...cardProps} compact={mobile} />
  }

  const deleteTargetPending = deleteMutation.isPending
    && deleteMutation.variables?.connectionId === deleteTarget?.id

  const overlayContent = (
    <>
      <OAuthCreateDraw
        open={createOpen}
        state={createState}
        startPending={startMutation.isPending}
        completePending={completeMutation.isPending}
        onOpenChange={(open) => {
          setCreateOpen(open)
          if (!open) {
            setCreateState(DEFAULT_CREATE_STATE)
            setCreateMode('create')
            setCreateProxyId('direct')
          }
        }}
        onStateChange={setCreateState}
        startSource={startMutation.variables?.source ?? null}
        title={createMode === 'reauth' ? t('create.reauthTitle') : undefined}
        proxyId={createProxyId}
        onProxyIdChange={setCreateProxyId}
        onGenerate={(source) => {
          setCreateMode('create')
          setCreateState((current) => ({
            ...current,
            source,
            authorize: null,
          }))
          startMutation.mutate({ source, proxyId: createProxyId === 'direct' ? null : createProxyId, mode: 'create' })
        }}
        onSubmitCallback={() => completeMutation.mutate(createState.callbackInput)}
      />

      <OAuthImportSheet
        open={importSheetOpen}
        pending={importMutation.isPending}
        result={importResult}
        onOpenChange={setImportSheetOpen}
        onImport={(input) => importMutation.mutate(input)}
      />

      <SiteModelTestSheet
        site={testSite}
        models={EMPTY_SITE_MODELS}
        onOpenChange={(open) => {
          if (!open) setTestSite(null)
        }}
      />

      {usageSplitTarget ? (
        <ChannelSplitDialog
          key={`${usageSplitTarget.type}:${usageSplitTarget.id}`}
          open
          initialTarget={usageSplitTarget}
          sites={sitesQuery.data?.items ?? EMPTY_SITES}
          oauthConnections={connections}
          onOpenChange={(open) => {
            if (!open) setUsageSplitTarget(null)
          }}
        />
      ) : null}

      <OAuthDisclaimerDialog
        open={Boolean(disclaimer)}
        onOpenChange={(next) => { if (!next) setDisclaimer(null) }}
        onConfirm={confirmDisclaimer}
      />

      <Dialog open={Boolean(deleteTarget)} onOpenChange={(next) => { if (!next && !deleteTargetPending) setDeleteTarget(null) }}>
        <DialogContent presentation="center" className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2"><DialogTitle>{t('workspace.deleteDialog.title')}</DialogTitle></DialogHeader>
          <DialogBody className="pt-0 text-body text-label-2">
            <DialogDescription className="mt-0">
              {deleteTarget ? t('workspace.deleteDialog.confirm', { provider: providerLabel(deleteTarget.provider) }) : t('workspace.deleteDialog.confirmDefault')}
            </DialogDescription>
          </DialogBody>
          <DialogFooter
            className="border-t-0 pt-2"
            cancel={<Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleteTargetPending}>{t('workspace.deleteDialog.cancel')}</Button>}
            confirm={(
              <Button variant="destructive" onClick={() => { if (deleteTarget?.site_id) deleteMutation.mutate({ siteId: deleteTarget.site_id, connectionId: deleteTarget.id }) }} disabled={deleteTargetPending}>
                {deleteTargetPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}{t('workspace.deleteDialog.delete')}
              </Button>
            )}
          />
        </DialogContent>
      </Dialog>
    </>
  )

  if (connectionsQuery.isLoading) {
    return <OAuthWorkspaceSkeleton />
  }

  if (connectionsQuery.isError) {
    return (
      <ErrorState
        title={t('workspace.loadFailed')}
        description={connectionsQuery.error.message}
      />
    )
  }

  const oauthSources = getOAuthSources(t)
  const statusSelect = (
    <Select value={statusFilter} onValueChange={setStatusFilter}>
      <SelectTrigger variant="filter" filterLabel={t('workspace.statusFilter.label')} active={statusFilter !== 'all'}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent searchable={false} widthMode="content">
        <SelectItem value="all">{t('workspace.statusFilter.all')}</SelectItem>
        <SelectItem value="connected">{t('workspace.statusFilter.connected')}</SelectItem>
        <SelectItem value="pending_sync">{t('workspace.statusFilter.pendingSync')}</SelectItem>
        <SelectItem value="reconnect_required">{t('workspace.statusFilter.reconnectRequired')}</SelectItem>
        <SelectItem value="expired">{t('workspace.statusFilter.expired')}</SelectItem>
        <SelectItem value="error">{t('workspace.statusFilter.error')}</SelectItem>
      </SelectContent>
    </Select>
  )
  // 与站点页一致的来源横条：类型多时横向滚动、右侧淡出；状态筛选不随之滚动——桌面紧跟在横条右侧，移动端固定在这一行最右
  const sourceControls = (
    <div className="flex min-w-0 flex-1 items-center gap-3">
      <div
        ref={sourceTabsRef}
        className={cn(
          'scrollbar-hidden min-w-0 overflow-x-auto',
          // 只有放不下时才留出右侧淡出区，放得下时状态筛选紧贴横条
          sourceTabsOverflowing && 'pr-8 [mask-image:linear-gradient(to_right,#000_calc(100%-40px),transparent)]',
        )}
      >
        <Tabs value={effectiveProviderFilter} onValueChange={setProviderFilter}>
          <TabsList className="inline-flex w-max">
            <TabsTrigger value="all">{t('workspace.provider.all')}</TabsTrigger>
            {availableProviders.map((provider) => {
              const source = oauthSources.find((item) => item.value === provider)
              return (
                <TabsTrigger key={provider} value={provider}>
                  <span className="flex items-center gap-1.5">
                    {source?.iconPath ? <img src={source.iconPath} alt="" className="size-4 rounded object-contain" /> : null}
                    {source?.label ?? providerLabel(provider)}
                  </span>
                </TabsTrigger>
              )
            })}
          </TabsList>
        </Tabs>
      </div>
      <div className={cn('shrink-0', isMobile && 'ml-auto')}>{statusSelect}</div>
    </div>
  )

  if (isMobile) {
    return (
      <div className="relative space-y-4 pb-20">
        <PageHeader
          eyebrow={t('workspace.pageHeader.eyebrow')}
          title={t('workspace.pageHeader.title')}
          description={t('workspace.pageHeader.description')}
        />

        <MobileFilterBar
          search={{ value: search, onChange: (event) => setSearch(event.target.value), placeholder: t('workspace.search') }}
          actions={
            <>
            <Button
              type="button"
              variant="secondary"
              size="icon"
              className="rounded-full"
              aria-label={t('workspace.import')}
              onClick={() => setDisclaimer({ type: 'import' })}
            >
              <Upload />
            </Button>
            <Button
              type="button"
              variant="secondary"
              size="icon"
              className="rounded-full"
              aria-label={t('workspace.refreshAll')}
              disabled={refreshAllMutation.isPending || !connections.length}
              onClick={() => refreshAllMutation.mutate()}
            >
              {refreshAllMutation.isPending ? <LoaderCircle className="animate-spin" /> : <RotateCcw />}
            </Button>
            </>
          }
          segments={sourceControls}
          summary={
            <>
              <span className="tabular-nums">{t('common:list.count', { count: filteredConnections.length })}</span>
              {search || effectiveProviderFilter !== 'all' || statusFilter !== 'all' ? (
                <button
                  type="button"
                  className="text-label-2 transition-colors active:text-label"
                  onClick={() => {
                    setSearch('')
                    setProviderFilter('all')
                    setStatusFilter('all')
                  }}
                >
                  {t('common:list.clearFilters')}
                </button>
              ) : null}
            </>
          }
        />

        {filteredConnections.length ? (
          <div className="space-y-4">
            {normalConnections.length > 0 ? (
              <div className="space-y-3">
                {normalConnectionGroups.map(({ provider, items }) => {
                  const collapsed = Boolean(collapsedProviderGroups[provider])
                  return (
                    <div key={provider}>
                      <GroupHeader label={providerLabel(provider)} count={items.length} collapsed={collapsed} onClick={() => toggleProviderGroup(provider)} />
                      {!collapsed ? (
                        <div className="mt-3 space-y-3">
                          {items.map((item) => renderConnectionCard(item, true))}
                        </div>
                      ) : null}
                    </div>
                  )
                })}
              </div>
            ) : statusFilter === 'all' ? (
              <EmptyState title={t('workspace.empty.title')} description={t('workspace.empty.description')} />
            ) : null}

            {errorConnections.length > 0 ? (
              <div>
                <div className="mb-3 border-t border-separator" />
                <GroupHeader label={t('workspace.errorAccounts')} count={errorConnections.length} collapsed={errorCollapsed} onClick={() => setErrorCollapsed((v) => !v)} />
                {!errorCollapsed ? (
                  <div className="mt-3 space-y-3">
                    {errorConnections.map((item) => renderConnectionCard(item, true))}
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>
        ) : statusFilter !== 'all' ? (
          <EmptyState title={t('workspace.noMatch.title')} description={t('workspace.noMatch.description')} />
        ) : (
          <EmptyState title={t('workspace.empty.title')} description={t('workspace.empty.description')} />
        )}

        <Button
          className="fixed bottom-[calc(env(safe-area-inset-bottom,0px)+6.5rem)] right-5 z-20 size-14 rounded-full p-0 shadow-[0_12px_32px_rgb(0_0_0/0.45)] [&_svg]:size-6"
          onClick={() => setDisclaimer({ type: 'create' })}
          aria-label={t('workspace.createNew')}
        >
          <Plus className="h-5 w-5" />
        </Button>

        {overlayContent}
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-5 overflow-hidden">
      <PageHeader
        eyebrow={t('workspace.pageHeader.eyebrow')}
        title={t('workspace.pageHeader.title')}
        description={t('workspace.pageHeader.description')}
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex-1 flex items-center gap-3 min-w-0">
          <div className="relative w-52 shrink-0">
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('workspace.search')}
              className="pl-10"
            />
            <Search className="text-label-3 absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 pointer-events-none z-10" />
          </div>

          {sourceControls}
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <Button
            variant="outline"
            onClick={() => refreshAllMutation.mutate()}
            disabled={refreshAllMutation.isPending}
          >
            {refreshAllMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
            {t('workspace.refreshAll')}
          </Button>
          <Button variant="outline" onClick={() => setDisclaimer({ type: 'import' })}>
            <Upload className="h-4 w-4" />
            {t('workspace.import')}
          </Button>
          <Button onClick={() => setDisclaimer({ type: 'create' })}>
            <Plus className="h-4 w-4" />
            {t('workspace.createNew')}
          </Button>
        </div>
      </div>

      <div className="scrollbar-hidden min-h-0 flex-1 overflow-auto">
        {filteredConnections.length ? (
          <div className="space-y-4">
            {normalConnections.length > 0 ? (
              <div className="space-y-4">
                {normalConnectionGroups.map(({ provider, items }) => {
                  const collapsed = Boolean(collapsedProviderGroups[provider])
                  return (
                    <div key={provider}>
                      <GroupHeader label={providerLabel(provider)} count={items.length} collapsed={collapsed} onClick={() => toggleProviderGroup(provider)} />
                      {!collapsed ? (
                        <div className="mt-2 grid grid-cols-1 gap-4 md:grid-cols-2">
                          {items.map((item) => renderConnectionCard(item))}
                        </div>
                      ) : null}
                    </div>
                  )
                })}
              </div>
            ) : statusFilter === 'all' ? (
              <EmptyState title={t('workspace.empty.title')} description={t('workspace.empty.description')} />
            ) : null}

            {errorConnections.length > 0 ? (
              <div>
                <div className="mb-4 border-t border-separator" />
                <GroupHeader label={t('workspace.errorAccounts')} count={errorConnections.length} collapsed={errorCollapsed} onClick={() => setErrorCollapsed((v) => !v)} />
                {!errorCollapsed ? (
                  <div className="mt-2 grid grid-cols-1 gap-4 md:grid-cols-2">
                    {errorConnections.map((item) => (
                      renderConnectionCard(item)
                    ))}
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>
        ) : statusFilter !== 'all' ? (
          <EmptyState title={t('workspace.noMatch.title')} description={t('workspace.noMatch.description')} />
        ) : (
          <EmptyState title={t('workspace.empty.title')} description={t('workspace.empty.description')} />
        )}
      </div>

      {overlayContent}
    </div>
  )
}

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.append(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

function siteGroupLabelForSite(siteId: string | null | undefined, groups: SiteGroup[]) {
  if (!siteId) return '-'
  const names = groups
    .filter((group) => group.sites.some((item) => item.site_id === siteId))
    .map((group) => group.name)
  if (names.length === 0) return '-'
  if (names.length <= 2) return names.join(' / ')
  return `${names.slice(0, 2).join(' / ')} +${names.length - 2}`
}

function inferOAuthCallbackProvider(callbackURL: string): OAuthProvider {
  try {
    const parsed = new URL(callbackURL)
    if (parsed.pathname.toLowerCase().includes('antigravity')) return 'antigravity'
  } catch {
    // Fall through to the Codex loopback path used by OpenAI OAuth.
  }
  return 'codex'
}

function claudeCodeAuthorizationResultFromURL(callbackURL: string) {
  const parsed = new URL(callbackURL)
  const code = parsed.searchParams.get('code')?.trim()
  const state = parsed.searchParams.get('state')?.trim()
  if (!code || !state) {
    throw new Error('Claude Code callback URL must contain code and state')
  }
  return `${code}#${state}`
}

/** 分组标题：可折叠，标题 + 数量，不带底色 */
function GroupHeader({
  label,
  count,
  collapsed,
  onClick,
}: {
  label: string
  count: number
  collapsed: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      className="flex items-center gap-1.5 py-1 text-left text-headline text-label transition-colors hover:text-label-2"
      aria-expanded={!collapsed}
      onClick={onClick}
    >
      <ChevronDown className={cn('size-4 shrink-0 text-label-3 transition-transform', collapsed && '-rotate-90')} />
      {label}
      <span className="text-footnote font-normal text-label-3 tabular-nums">{count}</span>
    </button>
  )
}
