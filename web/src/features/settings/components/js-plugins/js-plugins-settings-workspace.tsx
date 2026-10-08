import { useRef, useState, type ChangeEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { FlaskConical, LoaderCircle, Package, RefreshCw, Trash2, Upload } from 'lucide-react'
import { PageHeader } from '@/components/common/page-header'
import { StatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
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
  bindJSPluginSiteQuotaProbe,
  deleteJSPluginVersion,
  disableJSPlugin,
  enableJSPluginVersion,
  getJSPlugin,
  jsPluginQueryKeys,
  listBuiltinJSPlugins,
  listUploadedJSPlugins,
  tryJSPlugin,
  uploadJSPlugin,
  type JSPluginBuiltin,
  type JSPluginListItem,
  type JSPluginMetrics24h,
  type JSPluginTryResult,
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

function kindLabel(t: TFunction, kind: string) {
  if (kind === 'quota_probe') return t('settings:jsPlugins.kind.quotaProbe')
  if (kind === 'protocol') return t('settings:jsPlugins.kind.protocol')
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

export function JSPluginsSettingsWorkspace() {
  const { t } = useTranslation(['settings', 'common'])
  const queryClient = useQueryClient()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [managePlugin, setManagePlugin] = useState<JSPluginListItem | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<{ pluginId: string; version: string } | null>(null)
  const [unsignedEnable, setUnsignedEnable] = useState<{ pluginId: string; version: string } | null>(null)
  const [unsignedConfirmed, setUnsignedConfirmed] = useState(false)
  const [tryResult, setTryResult] = useState<JSPluginTryResult | null>(null)
  const [protocolSlug, setProtocolSlug] = useState('')
  const [bindSiteId, setBindSiteId] = useState('')

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
    enabled: managePlugin != null && managePlugin.kind === 'quota_probe',
  })

  const invalidateAll = () => {
    void queryClient.invalidateQueries({ queryKey: jsPluginQueryKeys.all })
  }

  const uploadMutation = useMutation({
    mutationFn: uploadJSPlugin,
    onSuccess: (row) => {
      toast.success(t('settings:jsPlugins.uploadSuccess', { id: row.plugin_id, version: row.version }))
      invalidateAll()
    },
    onError: (error: unknown) => {
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.uploadFailed'))
    },
  })

  const enableMutation = useMutation({
    mutationFn: ({
      pluginId,
      version,
      confirmUnsigned,
    }: {
      pluginId: string
      version: string
      confirmUnsigned?: boolean
    }) => enableJSPluginVersion(pluginId, version, { confirm_unsigned: confirmUnsigned }),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.enableSuccess'))
      setUnsignedEnable(null)
      setUnsignedConfirmed(false)
      invalidateAll()
      if (managePlugin) {
        void queryClient.invalidateQueries({ queryKey: jsPluginQueryKeys.detail(managePlugin.id) })
      }
    },
    onError: (error: unknown, variables) => {
      if (error instanceof APIError && error.code === 'js_plugin_unsigned') {
        setUnsignedEnable({ pluginId: variables.pluginId, version: variables.version })
        return
      }
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.enableFailed'))
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
    onSuccess: (result) => {
      setTryResult(result)
      if (result.ok) {
        toast.success(t('settings:jsPlugins.trySuccess'))
      } else {
        toast.error(result.error || t('settings:jsPlugins.tryFailed'))
      }
    },
    onError: (error: unknown) => {
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.tryFailed'))
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

  const bindQuotaMutation = useMutation({
    mutationFn: ({ pluginId, version, siteId }: { pluginId: string; version: string; siteId: string }) =>
      bindJSPluginSiteQuotaProbe(pluginId, version, siteId),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.bindQuotaSuccess'))
    },
    onError: (error: unknown) => {
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.bindQuotaFailed'))
    },
  })

  const busy =
    uploadMutation.isPending ||
    enableMutation.isPending ||
    disableMutation.isPending ||
    deleteMutation.isPending ||
    tryMutation.isPending ||
    bindProtocolMutation.isPending ||
    bindQuotaMutation.isPending

  const onPickFile = () => fileInputRef.current?.click()

  const onFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    uploadMutation.mutate(file)
  }

  const refresh = () => {
    void builtinsQuery.refetch()
    void uploadedQuery.refetch()
  }

  const uploadedUnavailable =
    uploadedQuery.isError &&
    uploadedQuery.error instanceof APIError &&
    uploadedQuery.error.code === 'js_plugin_unavailable'

  const openManage = (plugin: JSPluginListItem) => {
    setManagePlugin(plugin)
    setProtocolSlug('')
    setBindSiteId('')
  }

  const enabledVersionRow = (detailQuery.data?.versions ?? []).find((row) => row.status === 'enabled')

  return (
    <div className="max-w-5xl space-y-7">
      <PageHeader
        eyebrow={t('settings:jsPlugins.eyebrow')}
        title={t('settings:jsPlugins.title')}
        description={t('settings:jsPlugins.description')}
        actions={
          <>
            <Button type="button" variant="outline" size="sm" onClick={refresh} disabled={busy}>
              <RefreshCw
                className={cn('mr-2 h-4 w-4', (builtinsQuery.isFetching || uploadedQuery.isFetching) && 'animate-spin')}
              />
              {t('common:actions.refresh')}
            </Button>
            <Button type="button" size="sm" onClick={onPickFile} disabled={busy || uploadedUnavailable}>
              {uploadMutation.isPending ? (
                <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
              ) : (
                <Upload className="mr-2 h-4 w-4" />
              )}
              {t('settings:jsPlugins.upload')}
            </Button>
            <input
              ref={fileInputRef}
              type="file"
              accept=".xlp,application/zip,application/octet-stream"
              className="hidden"
              onChange={onFileChange}
            />
          </>
        }
      />

      <section className="space-y-3">
        <div className="flex items-center gap-2">
          <Package className="h-4 w-4 text-muted-soft" />
          <h3 className="text-sm font-semibold text-foreground">{t('settings:jsPlugins.builtinsTitle')}</h3>
        </div>
        <p className="text-sm text-muted-soft">{t('settings:jsPlugins.builtinsHint')}</p>
        <Card className="overflow-hidden border-[hsl(var(--glass-border))] bg-[hsl(var(--glass-surface))]">
          {builtinsQuery.isLoading ? (
            <div className="flex items-center justify-center gap-2 p-8 text-sm text-muted-soft">
              <LoaderCircle className="h-4 w-4 animate-spin" />
              {t('settings:jsPlugins.loading')}
            </div>
          ) : builtinsQuery.isError ? (
            <p className="p-6 text-sm text-destructive">{t('settings:jsPlugins.loadFailed')}</p>
          ) : (
            <table className="w-full min-w-full border-collapse text-left text-sm">
              <thead>
                <tr className="border-b border-[hsl(var(--glass-border))] text-muted-soft">
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.name')}</th>
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.id')}</th>
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.version')}</th>
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.kind')}</th>
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.role')}</th>
                </tr>
              </thead>
              <tbody>
                {(builtinsQuery.data ?? []).map((plugin) => (
                  <tr key={plugin.id} className="border-b border-[hsl(var(--glass-border))]/60 last:border-0">
                    <td className="px-4 py-3 font-medium">{plugin.name || plugin.id}</td>
                    <td className="px-4 py-3 font-mono text-xs text-muted-soft">{plugin.id}</td>
                    <td className="px-4 py-3">{plugin.version}</td>
                    <td className="px-4 py-3">{kindLabel(t, plugin.kind)}</td>
                    <td className="px-4 py-3 text-muted-soft">{builtinDetail(plugin, t)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold text-foreground">{t('settings:jsPlugins.uploadedTitle')}</h3>
        <p className="text-sm text-muted-soft">{t('settings:jsPlugins.uploadedHint')}</p>
        <Card className="overflow-hidden border-[hsl(var(--glass-border))] bg-[hsl(var(--glass-surface))]">
          {uploadedUnavailable ? (
            <p className="p-6 text-sm text-muted-soft">{t('settings:jsPlugins.unavailable')}</p>
          ) : uploadedQuery.isLoading ? (
            <div className="flex items-center justify-center gap-2 p-8 text-sm text-muted-soft">
              <LoaderCircle className="h-4 w-4 animate-spin" />
              {t('settings:jsPlugins.loading')}
            </div>
          ) : (uploadedQuery.data ?? []).length === 0 ? (
            <p className="p-6 text-sm text-muted-soft">{t('settings:jsPlugins.emptyUploaded')}</p>
          ) : (
            <table className="w-full min-w-full border-collapse text-left text-sm">
              <thead>
                <tr className="border-b border-[hsl(var(--glass-border))] text-muted-soft">
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.name')}</th>
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.id')}</th>
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.kind')}</th>
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.enabledVersion')}</th>
                  <th className="px-4 py-3 font-medium">{t('settings:jsPlugins.columns.metrics24h')}</th>
                  <th className="px-4 py-3 text-right font-medium">{t('settings:jsPlugins.columns.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {(uploadedQuery.data ?? []).map((plugin) => (
                  <tr key={plugin.id} className="border-b border-[hsl(var(--glass-border))]/60 last:border-0">
                    <td className="px-4 py-3 font-medium">{plugin.name || '—'}</td>
                    <td className="px-4 py-3 font-mono text-xs">{plugin.id}</td>
                    <td className="px-4 py-3">{kindLabel(t, plugin.kind ?? '')}</td>
                    <td className="px-4 py-3">
                      {plugin.enabled_version ? (
                        <StatusBadge status="success">{plugin.enabled_version}</StatusBadge>
                      ) : (
                        <span className="text-muted-soft">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-xs text-muted-soft">{formatMetrics24h(t, plugin.metrics_24h)}</td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex flex-wrap justify-end gap-2">
                        <Button type="button" variant="outline" size="sm" onClick={() => openManage(plugin)}>
                          {t('settings:jsPlugins.manageVersions')}
                        </Button>
                        {plugin.enabled_version ? (
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            disabled={busy}
                            onClick={() => disableMutation.mutate(plugin.id)}
                          >
                            {t('settings:jsPlugins.disable')}
                          </Button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </section>

      <Dialog
        open={managePlugin != null}
        onOpenChange={(open) => {
          if (!open) {
            setManagePlugin(null)
            setProtocolSlug('')
            setBindSiteId('')
          }
        }}
      >
        <DialogContent className="max-w-3xl">
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
                        tryPending={tryMutation.isPending && tryMutation.variables?.version === row.version}
                        onEnable={() => enableMutation.mutate({ pluginId: row.plugin_id, version: row.version })}
                        onTry={() => tryMutation.mutate({ pluginId: row.plugin_id, version: row.version })}
                        onDelete={() => setDeleteTarget({ pluginId: row.plugin_id, version: row.version })}
                        t={t}
                      />
                    ))}
                  </tbody>
                </table>

                {enabledVersionRow && managePlugin?.kind === 'protocol' ? (
                  <div className="rounded-lg border border-[hsl(var(--glass-border))] p-4 space-y-3">
                    <p className="text-sm font-medium">{t('settings:jsPlugins.bindProtocolTitle')}</p>
                    <p className="text-xs text-muted-soft">{t('settings:jsPlugins.bindProtocolHint')}</p>
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
                  </div>
                ) : null}

                {enabledVersionRow && managePlugin?.kind === 'quota_probe' ? (
                  <div className="rounded-lg border border-[hsl(var(--glass-border))] p-4 space-y-3">
                    <p className="text-sm font-medium">{t('settings:jsPlugins.bindQuotaTitle')}</p>
                    <p className="text-xs text-muted-soft">{t('settings:jsPlugins.bindQuotaHint')}</p>
                    <div className="flex flex-wrap items-end gap-2">
                      <div className="min-w-[220px] flex-1 space-y-1">
                        <span className="text-xs font-medium">{t('settings:jsPlugins.bindQuotaSite')}</span>
                        <Select value={bindSiteId} onValueChange={setBindSiteId}>
                          <SelectTrigger>
                            <SelectValue placeholder={t('settings:jsPlugins.bindQuotaSitePlaceholder')} />
                          </SelectTrigger>
                          <SelectContent>
                            {(sitesQuery.data ?? []).map((site) => (
                              <SelectItem key={site.id} value={site.id}>
                                {site.name} ({site.slug})
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                      <Button
                        type="button"
                        size="sm"
                        disabled={busy || !bindSiteId}
                        onClick={() =>
                          bindQuotaMutation.mutate({
                            pluginId: managePlugin.id,
                            version: enabledVersionRow.version,
                            siteId: bindSiteId,
                          })
                        }
                      >
                        {t('settings:jsPlugins.bindQuota')}
                      </Button>
                    </div>
                  </div>
                ) : null}
              </>
            )}
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setManagePlugin(null)}>
              {t('common:actions.close')}
            </Button>
          </DialogFooter>
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
        open={unsignedEnable != null}
        onOpenChange={(open) => {
          if (!open) {
            setUnsignedEnable(null)
            setUnsignedConfirmed(false)
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('settings:jsPlugins.unsignedTitle')}</DialogTitle>
            <DialogDescription>{t('settings:jsPlugins.unsignedBody')}</DialogDescription>
          </DialogHeader>
          <DialogBody>
            <label className="flex items-start gap-3 text-sm">
              <Checkbox checked={unsignedConfirmed} onCheckedChange={(v) => setUnsignedConfirmed(v === true)} />
              <span>{t('settings:jsPlugins.unsignedConfirm')}</span>
            </label>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setUnsignedEnable(null)}>
              {t('common:actions.cancel')}
            </Button>
            <Button
              type="button"
              disabled={!unsignedConfirmed || !unsignedEnable || enableMutation.isPending}
              onClick={() =>
                unsignedEnable &&
                enableMutation.mutate({
                  pluginId: unsignedEnable.pluginId,
                  version: unsignedEnable.version,
                  confirmUnsigned: true,
                })
              }
            >
              {t('settings:jsPlugins.enable')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={tryResult != null} onOpenChange={(open) => !open && setTryResult(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('settings:jsPlugins.tryDialogTitle')}</DialogTitle>
            <DialogDescription className="font-mono text-xs">
              {tryResult?.plugin_id}@{tryResult?.version}
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="space-y-2 text-sm">
            <p>
              {tryResult?.ok ? (
                <StatusBadge status="success">{t('settings:jsPlugins.tryOk')}</StatusBadge>
              ) : (
                <StatusBadge status="error">{t('settings:jsPlugins.tryFailed')}</StatusBadge>
              )}
            </p>
            {tryResult?.duration_ms != null ? (
              <p className="text-muted-soft">{t('settings:jsPlugins.tryDuration', { ms: tryResult.duration_ms })}</p>
            ) : null}
            {tryResult?.error ? <p className="text-destructive text-xs whitespace-pre-wrap">{tryResult.error}</p> : null}
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setTryResult(null)}>
              {t('common:actions.close')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function VersionRow({
  row,
  busy,
  tryPending,
  onEnable,
  onTry,
  onDelete,
  t,
}: {
  row: JSPluginVersion
  busy: boolean
  tryPending: boolean
  onEnable: () => void
  onTry: () => void
  onDelete: () => void
  t: TFunction
}) {
  const selftestOk = row.selftest?.ok
  const selftestError = row.selftest?.error
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
        <div>
          {row.signed ? (
            <StatusBadge status="success">{t('settings:jsPlugins.signed')}</StatusBadge>
          ) : (
            <StatusBadge status="idle">{t('settings:jsPlugins.unsigned')}</StatusBadge>
          )}
        </div>
        <div className="mt-1 font-mono text-muted-soft" title={row.package_sha256}>
          {shortSha256(row.package_sha256)}
        </div>
      </td>
      <td className="max-w-[180px] px-3 py-2 text-xs text-muted-soft" title={selftestError ?? undefined}>
        <span className="line-clamp-2">
          {selftestOk === true
            ? t('settings:jsPlugins.selftestOk')
            : selftestError || (selftestOk === false ? t('settings:jsPlugins.selftestFailed') : '—')}
        </span>
      </td>
      <td className="px-3 py-2 text-right">
        <div className="flex flex-wrap justify-end gap-2">
          <Button type="button" size="sm" variant="outline" disabled={busy || tryPending} onClick={onTry}>
            {tryPending ? (
              <LoaderCircle className="h-4 w-4 animate-spin" />
            ) : (
              <FlaskConical className="mr-1 h-3.5 w-3.5" />
            )}
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
        </div>
      </td>
    </tr>
  )
}
