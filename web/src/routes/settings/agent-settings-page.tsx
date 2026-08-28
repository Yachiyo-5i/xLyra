import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle, RefreshCw, Save, Search, ServerCog, Settings2 } from 'lucide-react'
import { BrandMark } from '@/components/common/brand-mark'
import { buildModelGlyph, siteTypeIconPath } from '@/components/common/brand-utils'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { PageHeader } from '@/components/common/page-header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Draw, DrawBody, DrawContent, DrawDescription, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { SiteModelScopePicker, type SiteModelScopePatch, type SiteModelScopePolicy } from '@/features/api-keys/components/site-model-scope-picker'
import { useSiteModelScope } from '@/features/api-keys/lib/use-site-model-scope'
import { fetchAgentAvailableModels, fetchAgentHealth, fetchAgentMeta, fetchAgentRuntimeSettings, updateAgentRuntimeSettings } from '@/features/agent/api/agent'
import { listCanonicalModels, listSitesWithOAuth } from '@/features/sites/api/sites'
import { formatSiteTypeLabel } from '@/features/sites/lib/site-utils'
import { canonicalModelIconInfo, modelNameIconInfo } from '@/features/sites/lib/model-icon'
import { toast } from '@/lib/toast'

const agentSettingsKey = ['settings', 'agent'] as const

type ScopeView = 'sites' | 'models'

export function AgentSettingsPage() {
  const { t } = useTranslation(['settings', 'components'])
  const queryClient = useQueryClient()
  const health = useQuery({ queryKey: [...agentSettingsKey, 'health'], queryFn: fetchAgentHealth, retry: false, refetchInterval: 30_000 })
  const meta = useQuery({ queryKey: [...agentSettingsKey, 'meta'], queryFn: fetchAgentMeta, retry: false })
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

  const save = useMutation({
    mutationFn: () => updateAgentRuntimeSettings({
      runner_base_url: effectiveRunnerURL.trim(),
      ...(runnerToken.trim() ? { runner_token: runnerToken.trim() } : {}),
      site_policy: effectiveSitePolicy,
      model_policy: effectiveModelPolicy,
      allowed_site_ids: effectiveSitePolicy === 'allow_list' ? selectedSiteIDs : [],
      allowed_site_model_ids: effectiveModelPolicy === 'allow_list'
        ? scope.selectedSiteModelIds.filter((id) => scope.availableSiteModelIds.has(id))
        : [],
    }),
    onSuccess: async () => {
      setRunnerURL('')
      setRunnerToken('')
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
          <Button variant="outline" onClick={() => { void queryClient.invalidateQueries({ queryKey: agentSettingsKey }) }}>
            <RefreshCw className="h-4 w-4" />
            {t('settings:agent.refresh')}
          </Button>
        )}
      />

      <div className="grid gap-4 md:grid-cols-3">
        <Card className="p-5">
          <div className="flex items-center gap-2 text-sm text-muted-soft">
            <ServerCog className="h-4 w-4" />
            {t('settings:agent.status.runner')}
          </div>
          <p className="mt-3 text-lg font-semibold text-foreground">
            {health.data?.runner === 'healthy' ? t('settings:agent.status.connected') : t('settings:agent.status.disconnected')}
          </p>
          <p className="mt-1 text-xs text-muted-soft">{health.data?.agent ?? t('settings:agent.status.pending')}</p>
        </Card>
        <Card className="p-5">
          <p className="text-sm text-muted-soft">{t('settings:agent.status.version')}</p>
          <p className="mt-3 text-lg font-semibold text-foreground">{meta.data?.version ?? '—'}</p>
          <p className="mt-1 text-xs text-muted-soft">{meta.data?.package ?? ''}</p>
        </Card>
        <Card className="p-5">
          <p className="text-sm text-muted-soft">{t('settings:agent.status.modelScope')}</p>
          <p className="mt-3 text-lg font-semibold text-foreground">
            {effectiveModelPolicy === 'allow_all' ? t('settings:agent.status.allModels') : scope.validSelectedSiteModelCount}
          </p>
          <p className="mt-1 text-xs text-muted-soft">
            {effectiveSitePolicy === 'allow_all' ? t('settings:agent.status.allSites') : t('settings:agent.status.siteCount', { count: selectedSiteIDs.length })}
          </p>
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
          <DrawFooter className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setScopeOpen(false)}>
              {t('settings:agent.scope.cancel')}
            </Button>
            <Button onClick={() => save.mutate()} disabled={save.isPending || (effectiveSitePolicy === 'allow_list' && scope.siteModelsLoading)}>
              <Save className="h-4 w-4" />
              {save.isPending ? t('settings:agent.saving') : t('settings:agent.save')}
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

      <div className="flex justify-end border-t border-[hsl(var(--divider))] pt-5">
        <Button onClick={() => save.mutate()} disabled={save.isPending || (effectiveSitePolicy === 'allow_list' && scope.siteModelsLoading)}>
          <Save className="h-4 w-4" />
          {save.isPending ? t('settings:agent.saving') : t('settings:agent.save')}
        </Button>
      </div>
    </div>
  )
}
