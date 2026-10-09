import { useCallback, useMemo, useRef, useState, type ChangeEvent, type DragEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { FlaskConical, KeyRound, LoaderCircle, Package, RefreshCw, Trash2, Upload } from 'lucide-react'
import { DataTable } from '@/components/common/data-table'
import { useMobileLayout } from '@/hooks/use-media-query'
import { EmptyState } from '@/components/common/empty-state'
import { PageHeader } from '@/components/common/page-header'
import { JSPluginSiteBindingSection } from '@/features/settings/components/js-plugins/js-plugin-site-binding'
import { useSiteBinding } from '@/features/settings/components/js-plugins/use-site-binding'
import {
  MobileBuiltinCard,
  MobileCardList,
  MobileRowList,
  MobileUploadedCard,
  MobileVersionCard,
} from '@/features/settings/components/js-plugins/js-plugins-mobile'
import { TableToolbar } from '@/components/common/table-toolbar'
import { defaultTableColumnWidths } from '@/lib/table-column-widths'
import { FormField } from '@/components/ui/form-field'
import { StatusBadge } from '@/components/common/status-badge'
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  bindJSPluginProtocolSlug,
  GLOBAL_KINDS,
  SITE_BOUND_KINDS,
  createJSPluginTrustedKey,
  deleteJSPluginTrustedKey,
  deleteJSPluginVersion,
  disableJSPlugin,
  enableJSPluginVersion,
  getJSPlugin,
  JS_PLUGIN_MAX_PACKAGE_BYTES,
  jsPluginQueryKeys,
  listBuiltinJSPlugins,
  listJSPluginTrustedKeys,
  listUploadedJSPlugins,
  tryJSPlugin,
  uploadJSPlugin,
  type JSPluginBuiltin,
  type JSPluginListItem,
  type JSPluginMetrics24h,
  type JSPluginTrust,
  type JSPluginTrustedKey,
  type JSPluginVersion,
} from '@/features/settings/api/js-plugins'
import { listSites, sitesQueryKeys } from '@/features/sites/api/sites'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'

function versionStatusBadge(status: string) {
  switch (status) {
    case 'enabled':
      return <StatusBadge status="success">{status}</StatusBadge>
    case 'broken':
      return <StatusBadge status="error">{status}</StatusBadge>
    case 'disabled':
      return <StatusBadge status="disabled">{status}</StatusBadge>
    default:
      return <StatusBadge status="idle">{status}</StatusBadge>
  }
}

const KIND_LABEL_KEYS: Record<string, string> = {
  quota_probe: 'quotaProbe',
  protocol: 'protocol',
  model_list: 'modelList',
  credential_check: 'credentialCheck',
  site_detect: 'siteDetect',
  error_classifier: 'errorClassifier',
  model_metadata: 'modelMetadata',
  pricing_parse: 'pricingParse',
}

function kindLabel(t: TFunction, kind: string) {
  const key = KIND_LABEL_KEYS[kind]
  if (key) return t(`settings:jsPlugins.kind.${key}`)
  return kind || '—'
}

function builtinDetail(plugin: JSPluginBuiltin, t: TFunction) {
  if (plugin.replaces) {
    return t('settings:jsPlugins.builtinReplaces', { type: plugin.replaces })
  }
  if (plugin.protocol) {
    return t('settings:jsPlugins.builtinProtocol', { name: plugin.protocol })
  }
  return '—'
}

function manifestString(manifest: Record<string, unknown> | undefined, key: string) {
  const value = manifest?.[key]
  return typeof value === 'string' && value.trim() ? value.trim() : ''
}

function shortSha256(sha?: string) {
  if (!sha) return '—'
  if (sha.length <= 14) return sha
  return `${sha.slice(0, 8)}…${sha.slice(-6)}`
}

function builtinDisplayName(plugin: JSPluginBuiltin) {
  return (plugin.name?.trim() || plugin.id).trim()
}

function formatMetrics24h(t: TFunction, metrics?: JSPluginMetrics24h) {
  if (!metrics || metrics.calls <= 0) {
    return t('settings:jsPlugins.metricsEmpty')
  }
  const rate = (metrics.error_rate * 100).toFixed(1)
  return t('settings:jsPlugins.metricsSummary', {
    errors: metrics.errors,
    calls: metrics.calls,
    rate,
  })
}

type BuiltinKindFilter = 'all' | 'quota_probe' | 'protocol'

const BUILTIN_COLUMN_SIZING = {
  storageKey: 'xlyra:js-plugins:builtin-table-column-widths:v1',
  defaultWidths: defaultTableColumnWidths([22, 28, 10, 14, 26]),
  minimumWidths: [10, 12, 6, 8, 10],
}

const UPLOADED_COLUMN_SIZING = {
  storageKey: 'xlyra:js-plugins:uploaded-table-column-widths:v1',
  defaultWidths: defaultTableColumnWidths([18, 22, 12, 12, 16, 20]),
  minimumWidths: [8, 10, 6, 6, 8, 12],
}

export function JSPluginsSettingsWorkspace() {
  const { t, i18n } = useTranslation(['settings', 'common'])
  const queryClient = useQueryClient()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [managePlugin, setManagePlugin] = useState<JSPluginListItem | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ pluginId: string; version: string } | null>(null)
  const [enableConfirm, setEnableConfirm] = useState<{
    pluginId: string
    version: string
    trust: JSPluginTrust
    signer?: string
  } | null>(null)
  const [enableConfirmed, setEnableConfirmed] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploadDragging, setUploadDragging] = useState(false)
  const [trustedKeysOpen, setTrustedKeysOpen] = useState(false)
  const [trustedKeyAddOpen, setTrustedKeyAddOpen] = useState(false)
  const [trustedKeyName, setTrustedKeyName] = useState('')
  const [trustedKeyPublic, setTrustedKeyPublic] = useState('')
  const [builtinSearch, setBuiltinSearch] = useState('')
  const [builtinKindFilter, setBuiltinKindFilter] = useState<BuiltinKindFilter>('all')
  // Last trial run of each version, shown in its row until the dialog closes.
  const [trials, setTrials] = useState<Record<string, TrialState>>({})
  const [protocolSlug, setProtocolSlug] = useState('')
  const isMobile = useMobileLayout()

  const builtinsQuery = useQuery({
    queryKey: jsPluginQueryKeys.builtins(),
    queryFn: ({ signal }) => listBuiltinJSPlugins(signal),
  })
  const uploadedQuery = useQuery({
    queryKey: jsPluginQueryKeys.uploaded(),
    queryFn: ({ signal }) => listUploadedJSPlugins(signal),
  })
  const detailQuery = useQuery({
    queryKey: managePlugin ? jsPluginQueryKeys.detail(managePlugin.id) : ['settings', 'js-plugins', 'detail', 'none'],
    queryFn: ({ signal }) => getJSPlugin(managePlugin!.id, signal),
    enabled: managePlugin != null,
  })
  const sitesQuery = useQuery({
    queryKey: sitesQueryKeys.listWithOAuth(),
    queryFn: async () => {
      const result = await listSites({ oauth: 'all' })
      return result.items ?? []
    },
    enabled:
      managePlugin != null &&
      (managePlugin.kind === 'quota_probe' || (SITE_BOUND_KINDS as readonly string[]).includes(managePlugin.kind ?? '')),
  })

  const invalidateAll = () => {
    void queryClient.invalidateQueries({ queryKey: jsPluginQueryKeys.all })
  }

  const uploadMutation = useMutation({
    mutationFn: uploadJSPlugin,
    onSuccess: (row) => {
      toast.success(t('settings:jsPlugins.uploadSuccess', { id: row.plugin_id, version: row.version }))
      setUploadOpen(false)
      setUploadDragging(false)
      invalidateAll()
    },
    onError: (error: unknown) => {
      if (!(error instanceof APIError)) {
        toast.error(t('settings:jsPlugins.uploadFailed'))
        return
      }
      switch (error.code) {
        case 'js_plugin_version_conflict':
          toast.error(t('settings:jsPlugins.uploadVersionConflict'))
          return
        case 'request_body_too_large':
          toast.error(t('settings:jsPlugins.uploadTooLarge'))
          return
        case 'js_plugin_package_invalid':
          toast.error(t('settings:jsPlugins.uploadInvalid', { message: error.message }))
          return
        default:
          toast.error(t('settings:jsPlugins.uploadFailed'))
      }
    },
  })

  const enableMutation = useMutation({
    mutationFn: ({
      pluginId,
      version,
      confirmUntrusted,
    }: {
      pluginId: string
      version: string
      confirmUntrusted?: boolean
    }) =>
      enableJSPluginVersion(pluginId, version, { confirm_untrusted: confirmUntrusted }),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.enableSuccess'))
      setEnableConfirm(null)
      setEnableConfirmed(false)
      invalidateAll()
      if (managePlugin) {
        void queryClient.invalidateQueries({ queryKey: jsPluginQueryKeys.detail(managePlugin.id) })
      }
    },
    onError: (error: unknown, variables) => {
      if (error instanceof APIError && error.code === 'js_plugin_confirm_required') {
        const row = detailQuery.data?.versions.find((item) => item.version === variables.version)
        setEnableConfirm({
          pluginId: variables.pluginId,
          version: variables.version,
          trust: row?.signer ? 'untrusted_signer' : 'unsigned',
          signer: row?.signer,
        })
        setEnableConfirmed(false)
        return
      }
      if (error instanceof APIError && error.code === 'js_plugin_kind_not_connected') {
        toast.error(t('settings:jsPlugins.kindNotConnected'))
        return
      }
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.enableFailed'))
    },
  })

  const addTrustedKeyMutation = useMutation({
    mutationFn: () =>
      createJSPluginTrustedKey({
        name: trustedKeyName.trim(),
        public_key: trustedKeyPublic.trim(),
      }),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.trustedKeyAddSuccess'))
      setTrustedKeyName('')
      setTrustedKeyPublic('')
      setTrustedKeyAddOpen(false)
      void queryClient.invalidateQueries({ queryKey: jsPluginQueryKeys.trustedKeys() })
      invalidateAll()
    },
    onError: (error: unknown) => {
      if (error instanceof APIError && error.code === 'js_plugin_trusted_key_exists') {
        toast.error(t('settings:jsPlugins.trustedKeyExists'))
        return
      }
      if (error instanceof APIError && error.code === 'js_plugin_trusted_key_invalid') {
        toast.error(error.message)
        return
      }
      toast.error(t('settings:jsPlugins.trustedKeyAddFailed'))
    },
  })

  const deleteTrustedKeyMutation = useMutation({
    mutationFn: (id: string) => deleteJSPluginTrustedKey(id),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.trustedKeyDeleteSuccess'))
      void queryClient.invalidateQueries({ queryKey: jsPluginQueryKeys.trustedKeys() })
      invalidateAll()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.trustedKeyDeleteFailed'))
    },
  })

  const disableMutation = useMutation({
    mutationFn: (pluginId: string) => disableJSPlugin(pluginId),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.disableSuccess'))
      invalidateAll()
      if (managePlugin) {
        void queryClient.invalidateQueries({ queryKey: jsPluginQueryKeys.detail(managePlugin.id) })
      }
    },
    onError: (error: unknown) => {
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.disableFailed'))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: ({ pluginId, version }: { pluginId: string; version: string }) =>
      deleteJSPluginVersion(pluginId, version),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.deleteSuccess'))
      setDeleteTarget(null)
      invalidateAll()
      if (managePlugin) {
        void queryClient.invalidateQueries({ queryKey: jsPluginQueryKeys.detail(managePlugin.id) })
      }
    },
    onError: (error: unknown) => {
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.deleteFailed'))
    },
  })

  const tryMutation = useMutation({
    mutationFn: ({ pluginId, version }: { pluginId: string; version: string }) => tryJSPlugin(pluginId, version),
    onSuccess: (result, { pluginId, version }) => {
      setTrials((current) => ({
        ...current,
        [trialKey(pluginId, version)]: { ok: result.ok, durationUs: result.duration_us, error: result.error },
      }))
    },
    onError: (error: unknown, { pluginId, version }) => {
      setTrials((current) => ({
        ...current,
        [trialKey(pluginId, version)]: {
          ok: false,
          error: error instanceof APIError ? error.message : t('settings:jsPlugins.tryFailed'),
        },
      }))
    },
  })

  const bindProtocolMutation = useMutation({
    mutationFn: ({ pluginId, version, slug }: { pluginId: string; version: string; slug: string }) =>
      bindJSPluginProtocolSlug(pluginId, version, slug),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.bindProtocolSuccess'))
      setProtocolSlug('')
    },
    onError: (error: unknown) => {
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.bindProtocolFailed'))
    },
  })

  const busy =
    uploadMutation.isPending ||
    enableMutation.isPending ||
    disableMutation.isPending ||
    deleteMutation.isPending ||
    tryMutation.isPending ||
    bindProtocolMutation.isPending ||
    addTrustedKeyMutation.isPending ||
    deleteTrustedKeyMutation.isPending

  const submitUploadFile = (file: File | null | undefined) => {
    if (!file || uploadMutation.isPending) return
    if (!/\.(xlp|zip)$/i.test(file.name)) {
      toast.error(t('settings:jsPlugins.uploadWrongType'))
      return
    }
    if (file.size > JS_PLUGIN_MAX_PACKAGE_BYTES) {
      toast.error(t('settings:jsPlugins.uploadTooLarge'))
      return
    }
    uploadMutation.mutate(file)
  }

  const onFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    submitUploadFile(file)
  }

  const onUploadDrop = (event: DragEvent) => {
    event.preventDefault()
    setUploadDragging(false)
    const file = event.dataTransfer.files?.[0]
    submitUploadFile(file)
  }

  const uploadedUnavailable =
    uploadedQuery.isError &&
    uploadedQuery.error instanceof APIError &&
    uploadedQuery.error.code === 'js_plugin_unavailable'

  const trustedKeysQuery = useQuery({
    queryKey: jsPluginQueryKeys.trustedKeys(),
    queryFn: ({ signal }) => listJSPluginTrustedKeys(signal),
    enabled: !uploadedUnavailable,
  })

  const trustedKeyCount = trustedKeysQuery.data?.length ?? 0

  const refresh = () => {
    void builtinsQuery.refetch()
    void uploadedQuery.refetch()
    if (!uploadedUnavailable) {
      void trustedKeysQuery.refetch()
    }
  }

  const openManage = useCallback((plugin: JSPluginListItem) => {
    setManagePlugin(plugin)
    setProtocolSlug('')
  }, [])

  const enabledVersionRow = (detailQuery.data?.versions ?? []).find((row) => row.status === 'enabled')
  const bindsToSites =
    managePlugin != null &&
    (managePlugin.kind === 'quota_probe' || (SITE_BOUND_KINDS as readonly string[]).includes(managePlugin.kind ?? ''))
  const siteBinding = useSiteBinding(
    managePlugin && enabledVersionRow && bindsToSites
      ? { pluginId: managePlugin.id, version: enabledVersionRow.version, kind: managePlugin.kind ?? '' }
      : null,
    sitesQuery.data ?? [],
  )
  const closeManage = () => {
    setManagePlugin(null)
    setProtocolSlug('')
    setTrials({})
    siteBinding.reset()
  }

  const filteredBuiltins = useMemo(() => {
    const query = builtinSearch.trim().toLowerCase()
    const items = [...(builtinsQuery.data ?? [])].sort((a, b) =>
      builtinDisplayName(a).localeCompare(builtinDisplayName(b), i18n.language, { sensitivity: 'base' }),
    )
    return items.filter((plugin) => {
      if (builtinKindFilter !== 'all' && plugin.kind !== builtinKindFilter) {
        return false
      }
      if (!query) return true
      const haystack = [
        builtinDisplayName(plugin),
        plugin.id,
        plugin.kind,
        plugin.replaces ?? '',
        plugin.protocol ?? '',
        plugin.description ?? '',
      ]
        .join(' ')
        .toLowerCase()
      return haystack.includes(query)
    })
  }, [builtinKindFilter, builtinSearch, builtinsQuery.data, i18n.language])

  const builtinColumns = useMemo<ColumnDef<JSPluginBuiltin>[]>(
    () => [
      {
        id: 'name',
        header: t('settings:jsPlugins.columns.name'),
        cell: ({ row }) => (
          <div className="truncate font-medium text-foreground" title={builtinDisplayName(row.original)}>
            {builtinDisplayName(row.original)}
          </div>
        ),
        meta: { cellClassName: 'min-w-0' },
      },
      {
        id: 'id',
        header: t('settings:jsPlugins.columns.id'),
        cell: ({ row }) => (
          <div className="truncate font-mono text-xs text-muted-soft" title={row.original.id}>
            {row.original.id}
          </div>
        ),
        meta: { cellClassName: 'min-w-0' },
      },
      {
        id: 'version',
        header: t('settings:jsPlugins.columns.version'),
        cell: ({ row }) => <span className="text-sm tabular-nums">{row.original.version}</span>,
        meta: { align: 'center' },
      },
      {
        id: 'kind',
        header: t('settings:jsPlugins.columns.kind'),
        cell: ({ row }) => <span className="text-sm">{kindLabel(t, row.original.kind)}</span>,
        meta: { align: 'center' },
      },
      {
        id: 'role',
        header: t('settings:jsPlugins.columns.role'),
        cell: ({ row }) => (
          <div className="truncate text-sm text-muted-soft" title={builtinDetail(row.original, t)}>
            {builtinDetail(row.original, t)}
          </div>
        ),
        meta: { cellClassName: 'min-w-0' },
      },
    ],
    [t],
  )

  const disablePlugin = disableMutation.mutate

  const uploadedColumns = useMemo<ColumnDef<JSPluginListItem>[]>(
    () => [
      {
        id: 'name',
        header: t('settings:jsPlugins.columns.name'),
        cell: ({ row }) => (
          <div className="truncate font-medium text-foreground" title={row.original.name}>
            {row.original.name || '—'}
          </div>
        ),
        meta: { cellClassName: 'min-w-0' },
      },
      {
        id: 'id',
        header: t('settings:jsPlugins.columns.id'),
        cell: ({ row }) => (
          <div className="truncate font-mono text-xs text-muted-soft" title={row.original.id}>
            {row.original.id}
          </div>
        ),
        meta: { cellClassName: 'min-w-0' },
      },
      {
        id: 'kind',
        header: t('settings:jsPlugins.columns.kind'),
        cell: ({ row }) => <span className="text-sm">{kindLabel(t, row.original.kind ?? '')}</span>,
        meta: { align: 'center' },
      },
      {
        id: 'enabled_version',
        header: t('settings:jsPlugins.columns.enabledVersion'),
        cell: ({ row }) =>
          row.original.enabled_version ? (
            <StatusBadge status="success">{row.original.enabled_version}</StatusBadge>
          ) : (
            <span className="text-sm text-muted-soft">—</span>
          ),
        meta: { align: 'center' },
      },
      {
        id: 'metrics_24h',
        header: t('settings:jsPlugins.columns.metrics24h'),
        cell: ({ row }) => (
          <span className="text-sm tabular-nums text-muted-soft">{formatMetrics24h(t, row.original.metrics_24h)}</span>
        ),
        meta: { align: 'center' },
      },
      {
        id: 'actions',
        header: '',
        cell: ({ row }) => (
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => openManage(row.original)}>
              {t('settings:jsPlugins.manageVersions')}
            </Button>
            {row.original.enabled_version ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={busy}
                onClick={() => disablePlugin(row.original.id)}
              >
                {t('settings:jsPlugins.disable')}
              </Button>
            ) : null}
          </div>
        ),
        meta: { align: 'right' },
      },
    ],
    [t, busy, openManage, disablePlugin],
  )

  const requestEnable = (row: JSPluginVersion) => {
    if (row.trust && row.trust !== 'trusted') {
      setEnableConfirm({
        pluginId: row.plugin_id,
        version: row.version,
        trust: row.trust,
        signer: row.signer,
      })
      setEnableConfirmed(false)
      return
    }
    enableMutation.mutate({ pluginId: row.plugin_id, version: row.version })
  }

  return (
    <div className="max-w-5xl space-y-7">
      <PageHeader
        eyebrow={t('settings:jsPlugins.eyebrow')}
        title={t('settings:jsPlugins.title')}
        description={t('settings:jsPlugins.description')}
        actions={
          <Button type="button" variant="outline" size="sm" onClick={refresh} disabled={busy}>
            <RefreshCw
              className={cn(
                'mr-2 h-4 w-4',
                (builtinsQuery.isFetching || uploadedQuery.isFetching || trustedKeysQuery.isFetching) && 'animate-spin',
              )}
            />
            {t('common:actions.refresh')}
          </Button>
        }
      />

      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <Package className="h-4 w-4 text-muted-soft" />
          <h3 className="text-sm font-semibold text-foreground">{t('settings:jsPlugins.builtinsTitle')}</h3>
        </div>
        <p className="text-sm text-muted-soft">{t('settings:jsPlugins.builtinsHint')}</p>
        <TableToolbar
          searchValue={builtinSearch}
          onSearchChange={(event) => setBuiltinSearch(event.target.value)}
          searchPlaceholder={t('settings:jsPlugins.builtinSearchPlaceholder')}
          searchClassName="flex-none md:w-52"
          filtersClassName="flex min-w-0 flex-1 flex-wrap items-center gap-3 md:flex md:auto-cols-auto"
          filters={(
            <Select
              value={builtinKindFilter}
              onValueChange={(value) => setBuiltinKindFilter(value as BuiltinKindFilter)}
            >
              <SelectTrigger
                variant="filter"
                filterLabel={t('settings:jsPlugins.builtinKindFilter.label')}
                active={builtinKindFilter !== 'all'}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent searchable={false} widthMode="content">
                <SelectItem value="all">{t('settings:jsPlugins.builtinKindFilter.all')}</SelectItem>
                <SelectItem value="quota_probe">{t('settings:jsPlugins.kind.quotaProbe')}</SelectItem>
                <SelectItem value="protocol">{t('settings:jsPlugins.kind.protocol')}</SelectItem>
              </SelectContent>
            </Select>
          )}
        />
        {isMobile && !builtinsQuery.isError && filteredBuiltins.length > 0 ? (
          <MobileCardList>
            {filteredBuiltins.map((plugin) => (
              <MobileBuiltinCard
                key={plugin.id}
                name={builtinDisplayName(plugin)}
                id={plugin.id}
                version={plugin.version}
                kind={kindLabel(t, plugin.kind)}
                detail={builtinDetail(plugin, t)}
                labels={{ version: t('settings:jsPlugins.columns.version'), kind: t('settings:jsPlugins.columns.kind') }}
              />
            ))}
          </MobileCardList>
        ) : (
          <DataTable
            columnSizing={BUILTIN_COLUMN_SIZING}
            columns={builtinColumns}
            data={builtinsQuery.isError ? [] : filteredBuiltins}
            getRowId={(plugin) => plugin.id}
            emptyState={
              <EmptyState
                title={
                  builtinsQuery.isLoading
                    ? t('settings:jsPlugins.loading')
                    : builtinsQuery.isError
                      ? t('settings:jsPlugins.loadFailed')
                      : t('settings:jsPlugins.builtinFilterEmpty')
                }
                description={t('settings:jsPlugins.builtinFilterEmptyHint')}
              />
            }
            hideHeaderWhenEmpty
          />
        )}
      </section>

      <section className="space-y-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 space-y-1">
            <h3 className="text-sm font-semibold text-foreground">{t('settings:jsPlugins.uploadedTitle')}</h3>
            <p className="text-sm text-muted-soft">{t('settings:jsPlugins.uploadedHint')}</p>
          </div>
          {!uploadedUnavailable ? (
            <div className="flex shrink-0 flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={busy}
                onClick={() => setTrustedKeysOpen(true)}
              >
                <KeyRound className="mr-2 h-4 w-4" />
                {t('settings:jsPlugins.trustedKeysButton', { count: trustedKeyCount })}
              </Button>
              <Button type="button" size="sm" onClick={() => setUploadOpen(true)} disabled={busy}>
                <Upload className="mr-2 h-4 w-4" />
                {t('settings:jsPlugins.upload')}
              </Button>
            </div>
          ) : null}
        </div>
        {isMobile && !uploadedUnavailable && (uploadedQuery.data ?? []).length > 0 ? (
          <MobileCardList>
            {(uploadedQuery.data ?? []).map((plugin) => (
              <MobileUploadedCard
                key={plugin.id}
                name={plugin.name || plugin.id}
                id={plugin.id}
                kind={kindLabel(t, plugin.kind ?? '')}
                enabledVersion={
                  plugin.enabled_version ? (
                    <StatusBadge status="success">{plugin.enabled_version}</StatusBadge>
                  ) : (
                    <span className="text-muted-soft">—</span>
                  )
                }
                metrics={formatMetrics24h(t, plugin.metrics_24h)}
                labels={{
                  kind: t('settings:jsPlugins.columns.kind'),
                  enabledVersion: t('settings:jsPlugins.columns.enabledVersion'),
                  metrics: t('settings:jsPlugins.columns.metrics24h'),
                }}
                actions={
                  <>
                    <Button type="button" variant="outline" size="sm" onClick={() => openManage(plugin)}>
                      {t('settings:jsPlugins.manageVersions')}
                    </Button>
                    {plugin.enabled_version ? (
                      <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => disablePlugin(plugin.id)}>
                        {t('settings:jsPlugins.disable')}
                      </Button>
                    ) : null}
                  </>
                }
              />
            ))}
          </MobileCardList>
        ) : (
          <DataTable
            columnSizing={UPLOADED_COLUMN_SIZING}
            columns={uploadedColumns}
            data={uploadedUnavailable ? [] : uploadedQuery.data ?? []}
            getRowId={(plugin) => plugin.id}
            emptyState={
              <EmptyState
                title={
                  uploadedUnavailable
                    ? t('settings:jsPlugins.unavailable')
                    : uploadedQuery.isLoading
                      ? t('settings:jsPlugins.loading')
                      : t('settings:jsPlugins.emptyUploaded')
                }
                description={t('settings:jsPlugins.emptyUploadedHint')}
              />
            }
            hideHeaderWhenEmpty
          />
        )}
      </section>

      <Dialog
        open={managePlugin != null}
        onOpenChange={(open) => {
          if (!open) closeManage()
        }}
      >
        <DialogContent size="lg">
          <DialogHeader>
            <DialogTitle>{managePlugin?.name || t('settings:jsPlugins.versionsDialogTitle')}</DialogTitle>
            <DialogDescription className="space-y-1">
              <span className="block font-mono text-xs">{managePlugin?.id}</span>
              {managePlugin?.kind ? (
                <span className="block text-xs">{kindLabel(t, managePlugin.kind)}</span>
              ) : null}
              {managePlugin?.enabled_version && managePlugin.metrics_24h ? (
                <span className="block text-xs text-muted-soft">
                  {t('settings:jsPlugins.metricsLabel')}: {formatMetrics24h(t, managePlugin.metrics_24h)}
                </span>
              ) : null}
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="space-y-4">
            {detailQuery.isLoading ? (
              <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-soft">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                {t('settings:jsPlugins.loading')}
              </div>
            ) : detailQuery.isError ? (
              <p className="text-sm text-destructive">{t('settings:jsPlugins.loadFailed')}</p>
            ) : (
              <>
                {isMobile ? (
                  <MobileRowList>
                    {(detailQuery.data?.versions ?? []).map((row) => (
                      <VersionCard
                        key={row.version}
                        row={row}
                        busy={busy}
                        trial={trials[trialKey(row.plugin_id, row.version)]}
                        tryPending={tryMutation.isPending && tryMutation.variables?.version === row.version}
                        onEnable={() => requestEnable(row)}
                        onTry={() => tryMutation.mutate({ pluginId: row.plugin_id, version: row.version })}
                        onDelete={() => setDeleteTarget({ pluginId: row.plugin_id, version: row.version })}
                        t={t}
                      />
                    ))}
                  </MobileRowList>
                ) : (
                  <table className="w-full min-w-full border-collapse text-left text-sm">
                    <thead>
                      <tr className="border-b border-[hsl(var(--glass-border))] text-muted-soft">
                        <th className="px-3 py-2 font-medium">{t('settings:jsPlugins.columns.version')}</th>
                        <th className="px-3 py-2 font-medium">{t('settings:jsPlugins.columns.status')}</th>
                        <th className="px-3 py-2 font-medium">{t('settings:jsPlugins.columns.package')}</th>
                        <th className="px-3 py-2 font-medium">{t('settings:jsPlugins.columns.selftest')}</th>
                        <th className="px-3 py-2 text-right font-medium">{t('settings:jsPlugins.columns.actions')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(detailQuery.data?.versions ?? []).map((row) => (
                        <VersionRow
                          key={row.version}
                          row={row}
                          busy={busy}
                          trial={trials[trialKey(row.plugin_id, row.version)]}
                        tryPending={tryMutation.isPending && tryMutation.variables?.version === row.version}
                          onEnable={() => requestEnable(row)}
                          onTry={() => tryMutation.mutate({ pluginId: row.plugin_id, version: row.version })}
                          onDelete={() => setDeleteTarget({ pluginId: row.plugin_id, version: row.version })}
                          t={t}
                        />
                      ))}
                    </tbody>
                  </table>
                )}

                {enabledVersionRow && managePlugin?.kind === 'protocol' ? (
                  <section className="space-y-3 border-t border-[hsl(var(--glass-divider))] pt-4">
                    <FormField
                      label={t('settings:jsPlugins.bindProtocolTitle')}
                      description={t('settings:jsPlugins.bindProtocolHint')}
                    >
                      <div className="flex flex-wrap gap-2">
                        <Input
                          className="max-w-xs font-mono text-sm"
                          placeholder={t('settings:jsPlugins.protocolSlugPlaceholder')}
                          value={protocolSlug}
                          onChange={(e) => setProtocolSlug(e.target.value)}
                        />
                        <Button
                          type="button"
                          size="sm"
                          disabled={busy || !protocolSlug.trim()}
                          onClick={() =>
                            bindProtocolMutation.mutate({
                              pluginId: managePlugin.id,
                              version: enabledVersionRow.version,
                              slug: protocolSlug.trim(),
                            })
                          }
                        >
                          {t('settings:jsPlugins.bindProtocol')}
                        </Button>
                      </div>
                    </FormField>
                  </section>
                ) : null}

                {siteBinding.applicable ? (
                  <JSPluginSiteBindingSection
                    binding={siteBinding}
                    sites={sitesQuery.data ?? []}
                    loading={sitesQuery.isLoading}
                  />
                ) : null}

                {enabledVersionRow && managePlugin && (GLOBAL_KINDS as readonly string[]).includes(managePlugin.kind ?? '') ? (
                  <p className="border-t border-[hsl(var(--glass-divider))] pt-4 text-sm text-muted-soft">
                    {t(`settings:jsPlugins.globalKindHint.${managePlugin.kind}`)}
                  </p>
                ) : null}
              </>
            )}
          </DialogBody>
          <DialogFooter
            cancel={
              <Button type="button" variant="outline" onClick={closeManage}>
                {siteBinding.applicable ? t('common:actions.cancel') : t('common:actions.close')}
              </Button>
            }
            confirm={
              siteBinding.applicable ? (
                <Button type="button" disabled={!siteBinding.canApply} onClick={siteBinding.apply}>
                  {siteBinding.applying ? (
                    <LoaderCircle className="h-4 w-4 animate-spin" />
                  ) : (
                    t('settings:jsPlugins.siteBinding.apply')
                  )}
                </Button>
              ) : undefined
            }
          />
        </DialogContent>
      </Dialog>

      <Dialog
        open={trustedKeysOpen}
        onOpenChange={(open) => {
          setTrustedKeysOpen(open)
          if (!open) setTrustedKeyAddOpen(false)
        }}
      >
        <DialogContent size="md">
          <DialogHeader>
            <DialogTitle>{t('settings:jsPlugins.trustedKeysDialogTitle')}</DialogTitle>
            <DialogDescription>{t('settings:jsPlugins.trustedKeysHint')}</DialogDescription>
          </DialogHeader>
          <DialogBody className="space-y-4">
            {trustedKeysQuery.isLoading ? (
              <div className="flex items-center justify-center gap-2 py-6 text-sm text-muted-soft">
                <LoaderCircle className="h-4 w-4 animate-spin" />
                {t('settings:jsPlugins.loading')}
              </div>
            ) : (trustedKeysQuery.data ?? []).length === 0 ? (
              <p className="text-sm text-muted-soft">{t('settings:jsPlugins.trustedKeysEmpty')}</p>
            ) : (
              <ul className="divide-y divide-[hsl(var(--glass-divider))] text-sm">
                {(trustedKeysQuery.data ?? []).map((key: JSPluginTrustedKey) => (
                  <li
                    key={key.id}
                    className="flex flex-wrap items-center justify-between gap-2 py-3"
                  >
                    <div className="min-w-0">
                      <div className="font-medium">{key.name}</div>
                      <div className="truncate font-mono text-xs text-muted-soft" title={key.fingerprint}>
                        {key.fingerprint}
                      </div>
                    </div>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={busy}
                      onClick={() => deleteTrustedKeyMutation.mutate(key.id)}
                    >
                      {t('common:actions.delete')}
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setTrustedKeysOpen(false)}>
              {t('common:actions.close')}
            </Button>
            <Button type="button" size="sm" disabled={busy} onClick={() => setTrustedKeyAddOpen(true)}>
              {t('settings:jsPlugins.trustedKeyAdd')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={uploadOpen}
        onOpenChange={(open) => {
          setUploadOpen(open)
          if (!open) setUploadDragging(false)
        }}
      >
        <DialogContent size="md">
          <DialogHeader>
            <DialogTitle>{t('settings:jsPlugins.uploadDialogTitle')}</DialogTitle>
            <DialogDescription>{t('settings:jsPlugins.uploadedHint')}</DialogDescription>
          </DialogHeader>
          <DialogBody>
            <div
              role="button"
              tabIndex={0}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  fileInputRef.current?.click()
                }
              }}
              onDrop={onUploadDrop}
              onDragOver={(event) => {
                event.preventDefault()
                setUploadDragging(true)
              }}
              onDragLeave={(event) => {
                event.preventDefault()
                setUploadDragging(false)
              }}
              onClick={() => !uploadMutation.isPending && fileInputRef.current?.click()}
              className={cn(
                'flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed px-4 py-10 text-center transition-colors',
                uploadMutation.isPending && 'pointer-events-none opacity-70',
                uploadDragging
                  ? 'border-[hsl(var(--accent))] bg-[hsl(var(--accent))]/5'
                  : 'border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] hover:border-[hsl(var(--accent))]/50 hover:bg-[hsl(var(--surface-subtle))]',
              )}
            >
              {uploadMutation.isPending ? (
                <LoaderCircle className="h-8 w-8 animate-spin text-muted-soft" />
              ) : (
                <Upload className="h-8 w-8 text-muted-soft" />
              )}
              <div className="text-sm font-medium text-foreground">{t('settings:jsPlugins.uploadDropzone')}</div>
              <div className="text-xs text-muted-soft">{t('settings:jsPlugins.uploadFileHint')}</div>
              <input
                ref={fileInputRef}
                type="file"
                accept=".xlp,application/zip,application/octet-stream"
                className="hidden"
                onChange={onFileChange}
              />
            </div>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setUploadOpen(false)} disabled={uploadMutation.isPending}>
              {t('common:actions.cancel')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={trustedKeyAddOpen}
        onOpenChange={(open) => {
          setTrustedKeyAddOpen(open)
          if (!open) {
            setTrustedKeyName('')
            setTrustedKeyPublic('')
          }
        }}
      >
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{t('settings:jsPlugins.trustedKeyAddDialogTitle')}</DialogTitle>
            <DialogDescription>{t('settings:jsPlugins.trustedKeysHint')}</DialogDescription>
          </DialogHeader>
          <DialogBody className="space-y-4">
            <FormField
              label={t('settings:jsPlugins.trustedKeyNameLabel')}
              htmlFor="js-plugin-trusted-key-name"
              required
            >
              <Input
                id="js-plugin-trusted-key-name"
                placeholder={t('settings:jsPlugins.trustedKeyNamePlaceholder')}
                value={trustedKeyName}
                onChange={(e) => setTrustedKeyName(e.target.value)}
              />
            </FormField>
            <FormField
              label={t('settings:jsPlugins.trustedKeyPublicLabel')}
              description={t('settings:jsPlugins.trustedKeyPublicHint')}
              htmlFor="js-plugin-trusted-key-public"
              required
            >
              <Input
                id="js-plugin-trusted-key-public"
                className="font-mono text-xs"
                placeholder={t('settings:jsPlugins.trustedKeyPublicPlaceholder')}
                value={trustedKeyPublic}
                onChange={(e) => setTrustedKeyPublic(e.target.value)}
              />
            </FormField>
          </DialogBody>
          <DialogFooter
            cancel={
              <Button type="button" variant="outline" onClick={() => setTrustedKeyAddOpen(false)}>
                {t('common:actions.cancel')}
              </Button>
            }
            confirm={
              <Button
                type="button"
                disabled={busy || !trustedKeyName.trim() || !trustedKeyPublic.trim() || addTrustedKeyMutation.isPending}
                onClick={() => addTrustedKeyMutation.mutate()}
              >
                {addTrustedKeyMutation.isPending ? (
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                ) : (
                  t('common:actions.save')
                )}
              </Button>
            }
          />
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget != null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('settings:jsPlugins.deleteConfirmTitle')}</DialogTitle>
            <DialogDescription>
              {t('settings:jsPlugins.deleteConfirmBody', {
                id: deleteTarget?.pluginId,
                version: deleteTarget?.version,
              })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDeleteTarget(null)}>
              {t('common:actions.cancel')}
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleteMutation.isPending || !deleteTarget}
              onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}
            >
              {deleteMutation.isPending ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <Trash2 className="mr-2 h-4 w-4" />
              )}
              {t('settings:jsPlugins.uninstall')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={enableConfirm != null}
        onOpenChange={(open) => {
          if (!open) {
            setEnableConfirm(null)
            setEnableConfirmed(false)
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {enableConfirm?.trust === 'untrusted_signer'
                ? t('settings:jsPlugins.untrustedTitle')
                : t('settings:jsPlugins.unsignedTitle')}
            </DialogTitle>
            <DialogDescription>
              {enableConfirm?.trust === 'untrusted_signer'
                ? t('settings:jsPlugins.untrustedBody')
                : t('settings:jsPlugins.unsignedBody')}
              {enableConfirm?.signer ? (
                <span className="mt-2 block font-mono text-xs">{enableConfirm.signer}</span>
              ) : null}
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            <label className="flex items-start gap-3 text-sm">
              <Checkbox checked={enableConfirmed} onCheckedChange={(v) => setEnableConfirmed(v === true)} />
              <span>{t('settings:jsPlugins.enableConfirm')}</span>
            </label>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setEnableConfirm(null)}>
              {t('common:actions.cancel')}
            </Button>
            <Button
              type="button"
              disabled={!enableConfirmed || !enableConfirm || enableMutation.isPending}
              onClick={() =>
                enableConfirm &&
                enableMutation.mutate({
                  pluginId: enableConfirm.pluginId,
                  version: enableConfirm.version,
                  confirmUntrusted: true,
                })
              }
            >
              {t('settings:jsPlugins.enable')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

    </div>
  )
}

type VersionRowProps = {
  row: JSPluginVersion
  trial?: TrialState
  busy: boolean
  tryPending: boolean
  onEnable: () => void
  onTry: () => void
  onDelete: () => void
  t: TFunction
}

type TrialState = { ok: boolean; durationUs?: number; error?: string }

function trialKey(pluginId: string, version: string) {
  return `${pluginId}@${version}`
}

// A fixture run takes well under a millisecond, so milliseconds alone would
// always read 0. Show microseconds up to 1 ms, then milliseconds to the microsecond.
function formatDurationUs(us: number) {
  if (us < 1000) return `${us} µs`
  if (us < 1_000_000) return `${(us / 1000).toFixed(3)} ms`
  return `${(us / 1_000_000).toFixed(3)} s`
}

function TrialStatus({ trial, t }: { trial: TrialState; t: TFunction }) {
  return (
    <div className="space-y-1">
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge status={trial.ok ? 'success' : 'error'}>
          {trial.ok ? t('settings:jsPlugins.trialPassed') : t('settings:jsPlugins.trialFailed')}
        </StatusBadge>
        {trial.durationUs != null ? (
          <span className="font-mono tabular-nums text-muted-soft">{formatDurationUs(trial.durationUs)}</span>
        ) : null}
      </div>
      {trial.error ? <p className="whitespace-pre-wrap break-words text-destructive">{trial.error}</p> : null}
    </div>
  )
}

function versionTrustBadge(row: JSPluginVersion, t: TFunction) {
  if (row.trust === 'trusted') return <StatusBadge status="success">{t('settings:jsPlugins.trustTrusted')}</StatusBadge>
  if (row.trust === 'untrusted_signer') {
    return <StatusBadge status="warning">{t('settings:jsPlugins.trustUntrusted')}</StatusBadge>
  }
  if (row.signed) return <StatusBadge status="idle">{t('settings:jsPlugins.signed')}</StatusBadge>
  return <StatusBadge status="idle">{t('settings:jsPlugins.unsigned')}</StatusBadge>
}

function selftestSummary(row: JSPluginVersion, t: TFunction) {
  const ok = row.selftest?.ok
  const error = row.selftest?.error
  if (ok === true) return t('settings:jsPlugins.selftestOk')
  return error || (ok === false ? t('settings:jsPlugins.selftestFailed') : '—')
}

function VersionActions({ row, busy, tryPending, onEnable, onTry, onDelete, t }: VersionRowProps) {
  return (
    <>
      <Button type="button" size="sm" variant="outline" disabled={busy || tryPending} onClick={onTry}>
        {tryPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <FlaskConical className="mr-1 h-3.5 w-3.5" />}
        {t('settings:jsPlugins.try')}
      </Button>
      {row.status !== 'enabled' ? (
        <Button type="button" size="sm" disabled={busy || row.status === 'broken'} onClick={onEnable}>
          {t('settings:jsPlugins.enable')}
        </Button>
      ) : null}
      {row.status !== 'enabled' ? (
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onDelete}>
          {t('settings:jsPlugins.uninstall')}
        </Button>
      ) : null}
    </>
  )
}

function VersionRow(props: VersionRowProps) {
  const { row, t } = props
  const manifestName = manifestString(row.manifest, 'name')
  const manifestKind = manifestString(row.manifest, 'kind')

  return (
    <tr className="border-b border-[hsl(var(--glass-border))]/60 last:border-0">
      <td className="px-3 py-2">
        <div className="font-mono text-sm">{row.version}</div>
        {manifestName ? <div className="text-xs text-muted-soft">{manifestName}</div> : null}
        {manifestKind ? <div className="text-xs text-muted-soft">{kindLabel(t, manifestKind)}</div> : null}
      </td>
      <td className="px-3 py-2">{versionStatusBadge(row.status)}</td>
      <td className="px-3 py-2 text-xs">
        <div>{versionTrustBadge(row, t)}</div>
        <div className="mt-1 font-mono text-muted-soft" title={row.package_sha256}>
          {shortSha256(row.package_sha256)}
        </div>
      </td>
      <td className="max-w-[180px] px-3 py-2 text-xs text-muted-soft" title={row.selftest?.error ?? undefined}>
        <span className="line-clamp-2">{selftestSummary(row, t)}</span>
        {props.trial ? (
          <div className="mt-2">
            <TrialStatus trial={props.trial} t={t} />
          </div>
        ) : null}
      </td>
      <td className="px-3 py-2 text-right">
        <div className="flex flex-wrap justify-end gap-2">
          <VersionActions {...props} />
        </div>
      </td>
    </tr>
  )
}

function VersionCard(props: VersionRowProps) {
  const { row, t } = props
  const manifestName = manifestString(row.manifest, 'name')
  const manifestKind = manifestString(row.manifest, 'kind')

  return (
    <MobileVersionCard
      version={row.version}
      name={manifestName || undefined}
      kind={manifestKind ? kindLabel(t, manifestKind) : undefined}
      status={versionStatusBadge(row.status)}
      trust={versionTrustBadge(row, t)}
      sha={shortSha256(row.package_sha256)}
      selftest={selftestSummary(row, t)}
      trial={props.trial ? <TrialStatus trial={props.trial} t={t} /> : undefined}
      labels={{
        package: t('settings:jsPlugins.columns.package'),
        selftest: t('settings:jsPlugins.columns.selftest'),
        trial: t('settings:jsPlugins.columns.trial'),
      }}
      actions={<VersionActions {...props} />}
    />
  )
}
