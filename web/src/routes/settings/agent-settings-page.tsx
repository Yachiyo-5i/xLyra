import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ArrowUpCircle, Bot, LoaderCircle, RefreshCw, Save, Search, ServerCog, Settings2 } from 'lucide-react'
import { BrandMark } from '@/components/common/brand-mark'
import { buildModelGlyph, siteTypeIconPath } from '@/components/common/brand-utils'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { PageHeader } from '@/components/common/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Draw, DrawBody, DrawContent, DrawDescription, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { SiteModelScopePicker, type SiteModelScopePatch, type SiteModelScopePolicy } from '@/features/api-keys/components/site-model-scope-picker'
import { useSiteModelScope } from '@/features/api-keys/lib/use-site-model-scope'
import { fetchAgentAvailableModels, fetchAgentHealth, fetchAgentRuntimeSettings, fetchAgentVersion, updateAgentRunnerSettings, updateAgentScopeSettings, clearAgentRunnerSettings, upgradeAgent, type AgentHealth } from '@/features/agent/api/agent'
import { agentSettingsKey } from '@/features/agent/lib/use-agent-availability'
import { listCanonicalModels, listSitesWithOAuth } from '@/features/sites/api/sites'
import { formatSiteTypeLabel } from '@/features/sites/lib/site-utils'
import { canonicalModelIconInfo, modelNameIconInfo } from '@/features/sites/lib/model-icon'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'

type ScopeView = 'sites' | 'models'

/** Component status indicator: dot + label (healthy / unhealthy / unreachable / unknown). */
function StatusIndicator({ state, className = 'text-sm' }: { state?: string | null; className?: string }) {
  const { t } = useTranslation(['settings'])
  const tone = statusToneClass(state)
  const text = state === 'healthy'
    ? t('settings:agent.status.stateHealthy')
    : state === 'unhealthy'
      ? t('settings:agent.status.stateUnhealthy')
      : state === 'unreachable'
        ? t('settings:agent.status.stateUnreachable')
        : t('settings:agent.status.stateUnknown')
  return (
    <span className={`flex items-center gap-2 font-medium text-foreground ${className}`}>
      <span className={`inline-block h-2 w-2 rounded-full ${tone}`} />
      {text}
    </span>
  )
}

function statusToneClass(state?: string | null): string {
  if (state === 'healthy') return 'bg-emerald-500'
  if (state === 'unhealthy') return 'bg-red-500'
  if (state === 'unreachable') return 'bg-amber-500'
  return 'bg-foreground/30'
}

/** Header status lights for Runner / Agent; a hover card shows both states. */
function HeaderStatusLights({ health }: { health: AgentHealth | null }) {
  return (
    <div className="group relative">
      <div className="flex cursor-default items-center gap-4">
        {(['runner', 'agent'] as const).map((key) => (
          <span key={key} className="flex items-center gap-1.5 text-xs text-muted-soft">
            <span className={`inline-block h-2 w-2 rounded-full ${statusToneClass(health?.[key])}`} />
            {key === 'runner' ? 'Runner' : 'Agent'}
          </span>
        ))}
      </div>
      <div className="pointer-events-none invisible absolute right-0 top-full z-30 mt-2 w-44 rounded-xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--dialog-surface))] p-3 opacity-0 shadow-[var(--shadow-dialog)] backdrop-blur-[40px] transition-opacity duration-150 group-hover:visible group-hover:opacity-100">
        {(['runner', 'agent'] as const).map((key) => (
          <div key={key} className="flex items-center justify-between gap-3 text-xs first:mt-0 mt-2">
            <span className="text-muted-soft">{key === 'runner' ? 'Runner' : 'Agent'}</span>
            <StatusIndicator state={health?.[key]} className="text-xs" />
          </div>
        ))}
      </div>
    </div>
  )
}

export function AgentSettingsPage() {
  const { t } = useTranslation(['settings', 'components'])
  const queryClient = useQueryClient()
  const health = useQuery({ queryKey: [...agentSettingsKey, 'health'], queryFn: fetchAgentHealth, retry: false, refetchInterval: 30_000 })
  const versionQuery = useQuery({
    queryKey: [...agentSettingsKey, 'version'],
    queryFn: () => fetchAgentVersion(false),
    retry: false,
    // Poll the upgrade state machine every 2s while upgrading; otherwise no polling (latest is cached 1h runner-side).
    refetchInterval: (query) => {
      const upgrade = query.state.data?.upgrade
      return upgrade && upgrade.status !== 'failed' ? 2_000 : false
    },
  })
  const runtime = useQuery({ queryKey: [...agentSettingsKey, 'runtime'], queryFn: fetchAgentRuntimeSettings, retry: false })
  const sitesQuery = useQuery({ queryKey: [...agentSettingsKey, 'sites'], queryFn: listSitesWithOAuth, retry: false })
  const canonicalQuery = useQuery({ queryKey: [...agentSettingsKey, 'canonical-models'], queryFn: listCanonicalModels, retry: false })
  const available = useQuery({ queryKey: [...agentSettingsKey, 'available-models'], queryFn: fetchAgentAvailableModels, retry: false })
  const [runnerURL, setRunnerURL] = useState('')
  const [runnerToken, setRunnerToken] = useState('')
  const [sitePolicy, setSitePolicy] = useState<SiteModelScopePolicy | null>(null)
  const [modelPolicy, setModelPolicy] = useState<SiteModelScopePolicy | null>(null)
  const [siteIDs, setSiteIDs] = useState<string[] | null>(null)
  const [siteModelIDs, setSiteModelIDs] = useState<string[] | null>(null)
  const [autoSelect, setAutoSelect] = useState<boolean | null>(null)
  const [scopeOpen, setScopeOpen] = useState(false)
  const [scopeView, setScopeView] = useState<ScopeView | null>(null)
  const [viewSearch, setViewSearch] = useState('')
  const [upgradeDialog, setUpgradeDialog] = useState<{ force: boolean } | null>(null)
  const [clearRunnerOpen, setClearRunnerOpen] = useState(false)
  const [refreshingStatus, setRefreshingStatus] = useState(false)

  const versionInfo = versionQuery.data ?? null
  const upgradeState = versionInfo?.upgrade ?? null
  const upgrading = Boolean(upgradeState && upgradeState.status !== 'failed')
  const agentVersionText = versionInfo?.current ?? health.data?.agent_version
    ?? (health.data ? t('settings:agent.status.agentSelfManaged') : '—')

  // Force one registry check on page open (runner caches the result for 1h).
  useEffect(() => {
    void fetchAgentVersion(true).then((fresh) => {
      if (fresh) queryClient.setQueryData([...agentSettingsKey, 'version'], fresh)
    })
  }, [queryClient])

  async function refreshStatus() {
    setRefreshingStatus(true)
    try {
      const [fresh] = await Promise.all([fetchAgentVersion(true), health.refetch()])
      if (fresh) queryClient.setQueryData([...agentSettingsKey, 'version'], fresh)
    } finally {
      setRefreshingStatus(false)
    }
  }

  // Toast once when the upgrade state machine leaves in-progress: null means success, failed means failure.
  const upgradeTargetRef = useRef<string | null>(null)
  useEffect(() => {
    if (upgradeState && upgradeState.status !== 'failed') {
      upgradeTargetRef.current = upgradeState.target
      return
    }
    const target = upgradeTargetRef.current
    if (!target) return
    upgradeTargetRef.current = null
    if (upgradeState?.status === 'failed') {
      toast.error(t('settings:agent.upgrade.failed'), { description: upgradeState.error })
    } else {
      toast.success(t('settings:agent.upgrade.succeeded', { version: target }))
      void queryClient.invalidateQueries({ queryKey: [...agentSettingsKey, 'health'] })
    }
  }, [upgradeState, queryClient, t])

  const upgrade = useMutation({
    mutationFn: (force: boolean) => upgradeAgent({ force }),
    onSuccess: () => {
      setUpgradeDialog(null)
      toast.success(t('settings:agent.upgrade.started'))
      void queryClient.invalidateQueries({ queryKey: [...agentSettingsKey, 'version'] })
    },
    onError: (error) => {
      // Sessions were running at submit time: escalate to the force-confirm dialog.
      if (error instanceof APIError && error.code === 'agent_busy') {
        setUpgradeDialog({ force: true })
        return
      }
      setUpgradeDialog(null)
      toast.error(t('settings:agent.upgrade.startFailed'), { description: error.message })
    },
  })

  function openUpgradeConfirm() {
    setUpgradeDialog({ force: (versionInfo?.active_runs ?? 0) > 0 })
  }

  const sites = useMemo(() => sitesQuery.data?.items ?? [], [sitesQuery.data])
  const canonicalModels = useMemo(() => canonicalQuery.data?.items ?? [], [canonicalQuery.data])
  const canonicalById = useMemo(() => new Map(canonicalModels.map((model) => [model.id, model])), [canonicalModels])
  const effectiveRunnerURL = runnerURL || runtime.data?.runner_base_url || ''
  const effectiveSitePolicy = sitePolicy ?? runtime.data?.site_policy ?? 'allow_all'
  const effectiveModelPolicy = effectiveSitePolicy === 'allow_list' ? 'allow_list' : modelPolicy ?? runtime.data?.model_policy ?? 'allow_all'
  const selectedSiteIDs = siteIDs ?? runtime.data?.allowed_site_ids ?? []
  const storedSiteModelIDs = siteModelIDs ?? runtime.data?.allowed_site_model_ids ?? []
  const effectiveAutoSelect = autoSelect ?? storedSiteModelIDs.length === 0

  const scope = useSiteModelScope({
    sites,
    sitePolicy: effectiveSitePolicy,
    modelPolicy: effectiveModelPolicy,
    siteIds: selectedSiteIDs,
    siteModelIds: storedSiteModelIDs,
    autoSelectSiteModels: effectiveAutoSelect,
  })

  const availableSites = useMemo(() => [...(available.data ?? [])].sort((left, right) => left.site_name.localeCompare(right.site_name, 'zh-CN')), [available.data])
  const selectedSiteModelIDSet = useMemo(() => new Set(scope.selectedSiteModelIds), [scope.selectedSiteModelIds])
  const viewKeyword = viewSearch.trim().toLowerCase()
  const viewSites = (effectiveSitePolicy === 'allow_list' ? availableSites.filter((site) => selectedSiteIDs.includes(site.site_id)) : availableSites)
    .filter((site) => site.enabled)
    .filter((site) => `${site.site_name} ${site.site_type}`.toLowerCase().includes(viewKeyword))
  const viewModels = availableSites
    .filter((site) => site.enabled && (effectiveSitePolicy !== 'allow_list' || selectedSiteIDs.includes(site.site_id)))
    .flatMap((site) => site.models.map((model) => ({ site, model })))
    .filter(({ model }) => effectiveModelPolicy !== 'allow_list' || selectedSiteModelIDSet.has(model.id))
    .filter(({ site, model }) => `${site.site_name} ${model.display_name} ${model.upstream_model_name} ${model.model_key ?? ''}`.toLowerCase().includes(viewKeyword))

  // Runner connection and site/model scope are saved separately (the settings endpoint is a partial update).
  const saveRunner = useMutation({
    mutationFn: () => updateAgentRunnerSettings({
      runner_base_url: effectiveRunnerURL.trim(),
      ...(runnerToken.trim() ? { runner_token: runnerToken.trim() } : {}),
    }),
    onSuccess: async () => {
      setRunnerURL('')
      setRunnerToken('')
      await queryClient.invalidateQueries({ queryKey: agentSettingsKey })
      toast.success(t('settings:agent.saveSuccess'))
    },
    onError: (error) => toast.error(t('settings:agent.saveFailed'), { description: error.message }),
  })

  const clearRunner = useMutation({
    mutationFn: clearAgentRunnerSettings,
    onSuccess: async () => {
      setRunnerURL('')
      setRunnerToken('')
      setClearRunnerOpen(false)
      await queryClient.invalidateQueries({ queryKey: agentSettingsKey })
      toast.success(t('settings:agent.runner.cleared'))
    },
    onError: (error) => toast.error(t('settings:agent.runner.clearFailed'), { description: error.message }),
  })

  const saveScope = useMutation({
    mutationFn: () => updateAgentScopeSettings({
      site_policy: effectiveSitePolicy,
      model_policy: effectiveModelPolicy,
      allowed_site_ids: effectiveSitePolicy === 'allow_list' ? selectedSiteIDs : [],
      allowed_site_model_ids: effectiveModelPolicy === 'allow_list'
        ? scope.selectedSiteModelIds.filter((id) => scope.availableSiteModelIds.has(id))
        : [],
    }),
    onSuccess: async () => {
      setSitePolicy(null)
      setModelPolicy(null)
      setSiteIDs(null)
      setSiteModelIDs(null)
      setAutoSelect(null)
      setScopeOpen(false)
      await queryClient.invalidateQueries({ queryKey: agentSettingsKey })
      toast.success(t('settings:agent.saveSuccess'))
    },
    onError: (error) => toast.error(t('settings:agent.saveFailed'), { description: error.message }),
  })

  if (runtime.isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-soft">
        <LoaderCircle className="h-4 w-4 animate-spin" />
        {t('settings:agent.loading')}
      </div>
    )
  }

  function handleScopeChange(patch: SiteModelScopePatch) {
    if (patch.sitePolicy !== undefined) setSitePolicy(patch.sitePolicy)
    if (patch.modelPolicy !== undefined) setModelPolicy(patch.modelPolicy)
    if (patch.siteIds !== undefined) setSiteIDs(patch.siteIds)
    if (patch.siteModelIds !== undefined) setSiteModelIDs(patch.siteModelIds)
    if (patch.autoSelectSiteModels !== undefined) setAutoSelect(patch.autoSelectSiteModels)
  }

  function openView(view: ScopeView) {
    setViewSearch('')
    setScopeView(view)
  }

  return (
    <div className="max-w-5xl space-y-7">
      <PageHeader
        eyebrow={t('settings:agent.eyebrow')}
        title={t('settings:agent.title')}
        description={t('settings:agent.description')}
        actions={(
          <>
            <HeaderStatusLights health={health.data ?? null} />
            <Button variant="outline" size="sm" onClick={() => void refreshStatus()} disabled={refreshingStatus}>
              <RefreshCw className={`h-3.5 w-3.5 ${refreshingStatus ? 'animate-spin' : ''}`} />
              {t('settings:agent.status.refresh')}
            </Button>
          </>
        )}
      />

      <div className="grid gap-4 md:grid-cols-2">
        <Card className="p-5">
          <div className="flex items-center gap-2 text-sm text-muted-soft">
            <ServerCog className="h-4 w-4" />
            Runner
          </div>
          {/* Fixed-height action slot keeps the card stable when actions appear/disappear. */}
          <div className="mt-3 flex h-9 items-center justify-between gap-3">
            <span className="text-lg font-semibold text-foreground">{health.data?.version ?? '—'}</span>
          </div>
        </Card>
        <Card className="p-5">
          <div className="flex items-center gap-2 text-sm text-muted-soft">
            <Bot className="h-4 w-4" />
            Agent
          </div>
          <div className="mt-3 flex h-9 items-center justify-between gap-3">
            <span className="flex min-w-0 items-center gap-2 text-lg font-semibold text-foreground">
              {agentVersionText}
              {versionInfo?.update_available && versionInfo.latest && !upgrading ? (
                <Badge variant="info">{t('settings:agent.upgrade.available', { version: versionInfo.latest })}</Badge>
              ) : null}
            </span>
            {versionInfo?.managed ? (
              upgrading || upgrade.isPending ? (
                <Button size="sm" variant="outline" disabled>
                  <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                  {t('settings:agent.upgrade.upgrading')}
                </Button>
              ) : upgradeState?.status === 'failed' ? (
                <Button size="sm" variant="outline" onClick={openUpgradeConfirm} title={upgradeState.error}>
                  <ArrowUpCircle className="h-3.5 w-3.5" />
                  {t('settings:agent.upgrade.retry')}
                </Button>
              ) : versionInfo.update_available ? (
                <Button size="sm" variant="outline" onClick={openUpgradeConfirm}>
                  <ArrowUpCircle className="h-3.5 w-3.5" />
                  {t('settings:agent.upgrade.action')}
                </Button>
              ) : null
            ) : null}
          </div>
        </Card>
      </div>

      <section className="space-y-5 border-t border-[hsl(var(--divider))] pt-6">
        <div>
          <h4 className="text-base font-semibold text-foreground">{t('settings:agent.runner.title')}</h4>
          <p className="mt-1 text-sm text-muted-soft">{t('settings:agent.runner.description')}</p>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <FormField label={t('settings:agent.runner.urlLabel')}>
            <Input
              value={effectiveRunnerURL}
              onChange={(event) => setRunnerURL(event.target.value)}
              placeholder={t('settings:agent.runner.urlPlaceholder')}
              className="font-mono text-xs"
            />
          </FormField>
          <FormField
            label={t('settings:agent.runner.tokenLabel')}
            description={runtime.data?.runner_token_configured ? t('settings:agent.runner.tokenConfigured') : t('settings:agent.runner.tokenNotConfigured')}
          >
            <Input
              type="password"
              value={runnerToken}
              onChange={(event) => setRunnerToken(event.target.value)}
              placeholder={runtime.data?.runner_token_configured ? t('settings:agent.runner.tokenReplacePlaceholder') : t('settings:agent.runner.tokenPlaceholder')}
              autoComplete="new-password"
              className="font-mono text-xs"
            />
          </FormField>
        </div>
        <div className="flex justify-end gap-3">
          <Button
            variant="outline"
            onClick={() => setClearRunnerOpen(true)}
            disabled={clearRunner.isPending || (!runtime.data?.runner_base_url && !runtime.data?.runner_token_configured && !runnerURL && !runnerToken)}
            className="text-destructive hover:text-destructive"
          >
            {t('settings:agent.runner.clear')}
          </Button>
          <Button onClick={() => saveRunner.mutate()} disabled={saveRunner.isPending || !effectiveRunnerURL.trim()}>
            <Save className="h-4 w-4" />
            {saveRunner.isPending ? t('settings:agent.saving') : t('settings:agent.save')}
          </Button>
        </div>
      </section>

      <section className="space-y-5 border-t border-[hsl(var(--divider))] pt-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h4 className="text-base font-semibold text-foreground">{t('settings:agent.scope.title')}</h4>
            <p className="mt-1 text-sm text-muted-soft">{t('settings:agent.scope.description')}</p>
          </div>
          <Button variant="outline" onClick={() => setScopeOpen(true)}>
            <Settings2 className="h-4 w-4" />
            {t('settings:agent.scope.configure')}
          </Button>
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Card className="cursor-pointer p-5" role="button" tabIndex={0} onClick={() => openView('sites')} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); openView('sites') } }}>
            <p className="text-sm text-muted-soft">{t('settings:agent.scope.availableSites')}</p>
            <p className="mt-2 text-lg font-semibold text-foreground">
              {effectiveSitePolicy === 'allow_all' ? t('settings:agent.status.allSites') : t('settings:agent.status.siteCount', { count: selectedSiteIDs.length })}
            </p>
            <p className="mt-1 text-xs text-muted-soft">{t('settings:agent.scope.availableSitesHint')}</p>
          </Card>
          <Card className="cursor-pointer p-5" role="button" tabIndex={0} onClick={() => openView('models')} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); openView('models') } }}>
            <p className="text-sm text-muted-soft">{t('settings:agent.scope.availableModels')}</p>
            <p className="mt-2 text-lg font-semibold text-foreground">
              {effectiveModelPolicy === 'allow_all' ? t('settings:agent.status.allModels') : t('settings:agent.status.modelCount', { count: scope.validSelectedSiteModelCount })}
            </p>
            <p className="mt-1 text-xs text-muted-soft">{t('settings:agent.scope.availableModelsHint')}</p>
          </Card>
        </div>
      </section>

      <Draw open={scopeOpen} onOpenChange={setScopeOpen}>
        <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
          <DrawHeader>
            <DrawTitle>{t('settings:agent.scope.title')}</DrawTitle>
            <DrawDescription>{t('settings:agent.scope.description')}</DrawDescription>
          </DrawHeader>
          <DrawBody className="space-y-4">
            <SiteModelScopePicker
              scope={scope}
              sitesLoading={sitesQuery.isLoading}
              canonicalModels={canonicalModels}
              canonicalModelsLoading={canonicalQuery.isLoading}
              sitePolicy={effectiveSitePolicy}
              modelPolicy={modelPolicy ?? runtime.data?.model_policy ?? 'allow_all'}
              siteIds={selectedSiteIDs}
              siteModelIds={storedSiteModelIDs}
              onChange={handleScopeChange}
            />
          </DrawBody>
          <DrawFooter>
            <Button onClick={() => saveScope.mutate()} disabled={saveScope.isPending || (effectiveSitePolicy === 'allow_list' && scope.siteModelsLoading)}>
              {saveScope.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {saveScope.isPending ? t('settings:agent.saving') : t('settings:agent.save')}
            </Button>
            <Button variant="ghost" onClick={() => setScopeOpen(false)} disabled={saveScope.isPending}>
              {t('settings:agent.scope.cancel')}
            </Button>
          </DrawFooter>
        </DrawContent>
      </Draw>

      <Draw open={scopeView !== null} onOpenChange={(open) => { if (!open) setScopeView(null) }}>
        <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
          <DrawHeader>
            <DrawTitle>{scopeView === 'sites' ? t('settings:agent.scope.availableSites') : t('settings:agent.scope.availableModels')}</DrawTitle>
          </DrawHeader>
          <DrawBody className="space-y-4">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-foreground/40" />
              <Input
                value={viewSearch}
                onChange={(event) => setViewSearch(event.target.value)}
                placeholder={scopeView === 'sites' ? t('settings:agent.scope.searchSites') : t('settings:agent.scope.searchModels')}
                className="pl-9"
              />
            </div>
            <div className="overflow-y-auto rounded-lg border border-[hsl(var(--glass-border))]">
              {scopeView === 'sites' ? (
                viewSites.length ? viewSites.map((site) => (
                  <div key={site.site_id} className="flex items-center gap-3 border-t border-[hsl(var(--glass-divider))] px-4 py-3 first:border-t-0">
                    <BrandMark
                      iconPath={siteTypeIconPath(site.site_type)}
                      label={formatSiteTypeLabel(site.site_type)}
                      fallback={site.site_name}
                      fallbackText={buildModelGlyph(site.site_name)}
                      size="sm"
                    />
                    <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground" title={site.site_name}>{site.site_name}</span>
                    <Badge variant="neutral" className="max-w-[40%] truncate">{formatSiteTypeLabel(site.site_type)}</Badge>
                  </div>
                )) : <div className="py-10 text-center text-sm text-muted-soft">{t('settings:agent.scope.noSites')}</div>
              ) : (
                viewModels.length ? viewModels.map(({ site, model }) => {
                  const canonical = model.canonical_model_id ? canonicalById.get(model.canonical_model_id) : undefined
                  const icon = canonical
                    ? canonicalModelIconInfo(canonical, model.display_name || model.upstream_model_name)
                    : modelNameIconInfo([model.display_name, model.upstream_model_name, model.model_key], model.model_key || model.upstream_model_name)
                  return (
                    <div key={model.id} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-4 border-t border-[hsl(var(--glass-divider))] px-4 py-3 first:border-t-0">
                      <div className="flex min-w-0 items-center gap-3">
                        <BrandMark
                          iconPath={icon.iconPath}
                          label={icon.label}
                          fallback={icon.fallback}
                          fallbackText={icon.fallbackText}
                          size="sm"
                        />
                        <button
                          type="button"
                          className="min-w-0 max-w-full truncate bg-transparent p-0 text-left text-sm font-medium text-foreground"
                          title={model.upstream_model_name}
                          onClick={() => copyToClipboard(model.model_key || model.upstream_model_name, t('components:modelsDraw.copied'), t('components:modelsDraw.copyFailed'))}
                        >
                          {model.model_key || model.upstream_model_name}
                        </button>
                      </div>
                      <Badge variant="neutral" className="max-w-full truncate" title={site.site_name}>{site.site_name}</Badge>
                    </div>
                  )
                }) : <div className="py-10 text-center text-sm text-muted-soft">{t('settings:agent.scope.noModels')}</div>
              )}
            </div>
          </DrawBody>
        </DrawContent>
      </Draw>

      <Dialog open={clearRunnerOpen} onOpenChange={(open) => { if (!open) setClearRunnerOpen(false) }}>
        <DialogContent className="w-[min(92vw,440px)]">
          <DialogHeader>
            <DialogTitle>{t('settings:agent.runner.clearTitle')}</DialogTitle>
            <DialogDescription>{t('settings:agent.runner.clearDescription')}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setClearRunnerOpen(false)} disabled={clearRunner.isPending}>
              {t('settings:agent.runner.clearCancel')}
            </Button>
            <Button variant="destructive" disabled={clearRunner.isPending} onClick={() => clearRunner.mutate()}>
              {clearRunner.isPending ? t('settings:agent.runner.clearing') : t('settings:agent.runner.clearConfirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={upgradeDialog !== null} onOpenChange={(open) => { if (!open) setUpgradeDialog(null) }}>
        <DialogContent className="w-[min(92vw,440px)]">
          <DialogHeader>
            <DialogTitle>
              {upgradeDialog?.force ? t('settings:agent.upgrade.forceTitle') : t('settings:agent.upgrade.title')}
            </DialogTitle>
            <DialogDescription>
              {upgradeDialog?.force
                ? t('settings:agent.upgrade.forceDescription', { count: versionInfo?.active_runs ?? 0 })
                : t('settings:agent.upgrade.description', { current: agentVersionText, target: versionInfo?.latest ?? '' })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setUpgradeDialog(null)}>
              {t('settings:agent.upgrade.cancel')}
            </Button>
            <Button
              variant={upgradeDialog?.force ? 'destructive' : 'default'}
              disabled={upgrade.isPending}
              onClick={() => upgrade.mutate(Boolean(upgradeDialog?.force))}
            >
              {upgrade.isPending
                ? t('settings:agent.upgrade.starting')
                : upgradeDialog?.force
                  ? t('settings:agent.upgrade.forceConfirm')
                  : t('settings:agent.upgrade.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
