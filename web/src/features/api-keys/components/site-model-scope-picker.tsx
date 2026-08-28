import { useMemo, useState } from 'react'
import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { BrandMark } from '@/components/common/brand-mark'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { MultiSelect } from '@/components/ui/multi-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { formatSiteTypeLabel, mergeModelKeys, modelProviderLabel } from '@/features/api-keys/lib/api-key-utils'
import { type SiteModelScope, type SiteModelScopePatch, type SiteModelScopePolicy } from '@/features/api-keys/lib/use-site-model-scope'
import { siteModelIconInfo } from '@/features/sites/lib/model-icon'
import type { CanonicalModelItem } from '@/features/sites/api/sites'
import type { SiteGroup } from '@/features/settings/api/site-groups'

export type { SiteModelScopePatch, SiteModelScopePolicy } from '@/features/api-keys/lib/use-site-model-scope'

export function SiteModelScopePicker({
  scope,
  sitesLoading,
  siteGroups,
  siteGroupsLoading,
  canonicalModels,
  canonicalModelsLoading,
  sitePolicy,
  modelPolicy,
  siteIds,
  siteGroupIds = [],
  siteModelIds,
  onChange,
}: {
  scope: SiteModelScope
  sitesLoading?: boolean
  siteGroups?: SiteGroup[]
  siteGroupsLoading?: boolean
  canonicalModels: CanonicalModelItem[]
  canonicalModelsLoading?: boolean
  sitePolicy: SiteModelScopePolicy
  modelPolicy: SiteModelScopePolicy
  siteIds: string[]
  siteGroupIds?: string[]
  siteModelIds: string[]
  onChange: (patch: SiteModelScopePatch) => void
}) {
  const { t } = useTranslation('api-keys')
  const [siteSearch, setSiteSearch] = useState('')
  const [siteTypeFilter, setSiteTypeFilter] = useState('all')
  const [showDisabledSites, setShowDisabledSites] = useState(true)
  const [modelSearch, setModelSearch] = useState('')
  const [modelBrand, setModelBrand] = useState('all')

  const {
    sortedSites,
    enabledSites,
    inheritedSiteIdSet,
    selectedDirectSiteIdSet,
    effectiveSiteIds,
    effectiveModelPolicy,
    siteModelsLoading,
    siteModelRows,
    selectedSiteModelIds,
    validSelectedSiteModelCount,
  } = scope

  const showSiteGroups = siteGroups !== undefined
  const siteGroupOptions = useMemo(() => (siteGroups ?? []).filter((group) => group.enabled).map((group) => ({
    value: group.id,
    label: group.name,
    description: t('form.fields.groupSiteCount', { count: group.sites.length }),
  })), [siteGroups, t])
  const visibleSiteOptions = useMemo(() => {
    if (sitePolicy !== 'allow_list') return enabledSites
    return sortedSites.filter((site) => (
      site.enabled ||
      showDisabledSites ||
      selectedDirectSiteIdSet.has(site.id) ||
      inheritedSiteIdSet.has(site.id)
    ))
  }, [enabledSites, inheritedSiteIdSet, selectedDirectSiteIdSet, showDisabledSites, sitePolicy, sortedSites])

  const siteTypeItems = useMemo(() => {
    const map = new Map<string, number>()
    for (const site of visibleSiteOptions) {
      const label = formatSiteTypeLabel(site.site_type)
      map.set(label, (map.get(label) ?? 0) + 1)
    }
    return [...map.entries()]
      .sort(([left], [right]) => left.localeCompare(right, 'zh-CN'))
      .map(([label, count]) => ({ label, count }))
  }, [visibleSiteOptions])

  const filteredSites = useMemo(() => {
    const keyword = siteSearch.trim().toLowerCase()
    return visibleSiteOptions.filter((site) => {
      const matchesType = siteTypeFilter === 'all' || formatSiteTypeLabel(site.site_type) === siteTypeFilter
      const matchesSearch = !keyword || `${site.name} ${site.slug} ${site.site_type}`.toLowerCase().includes(keyword)
      return matchesType && matchesSearch
    })
  }, [siteSearch, siteTypeFilter, visibleSiteOptions])
  const hasSelectableFilteredSites = useMemo(
    () => filteredSites.some((site) => site.enabled && !selectedDirectSiteIdSet.has(site.id)),
    [filteredSites, selectedDirectSiteIdSet],
  )
  const hasClearableFilteredSites = useMemo(
    () => filteredSites.some((site) => selectedDirectSiteIdSet.has(site.id)),
    [filteredSites, selectedDirectSiteIdSet],
  )

  const canonicalById = useMemo(
    () => new Map(canonicalModels.map((model) => [model.id, model])),
    [canonicalModels],
  )

  const modelBrandItems = useMemo(() => {
    const counts = new Map<string, number>()
    for (const { site, model } of siteModelRows) {
      const canonical = model.canonical_model_id ? canonicalById.get(model.canonical_model_id) : undefined
      const provider = canonical ? modelProviderLabel(canonical) : formatSiteTypeLabel(site.site_type)
      counts.set(provider, (counts.get(provider) ?? 0) + 1)
    }
    return [...counts.entries()]
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([provider, count]) => ({ provider, count }))
  }, [canonicalById, siteModelRows])

  const filteredModels = useMemo(() => {
    const keyword = modelSearch.trim().toLowerCase()
    return siteModelRows.filter(({ site, model }) => {
      const canonical = model.canonical_model_id ? canonicalById.get(model.canonical_model_id) : undefined
      const provider = canonical ? modelProviderLabel(canonical) : formatSiteTypeLabel(site.site_type)
      const matchesBrand = modelBrand === 'all' || provider === modelBrand
      const text = `${model.upstream_model_name} ${model.display_name} ${canonical?.model_key ?? ''} ${site.name} ${site.slug} ${site.site_type}`.toLowerCase()
      return matchesBrand && (!keyword || text.includes(keyword))
    })
  }, [canonicalById, modelBrand, modelSearch, siteModelRows])

  const filteredModelKeys = useMemo(
    () => filteredModels.map(({ model }) => model.id),
    [filteredModels],
  )
  const hasSelectableFilteredModels = useMemo(
    () => filteredModelKeys.some((id) => !selectedSiteModelIds.includes(id)),
    [filteredModelKeys, selectedSiteModelIds],
  )
  const hasClearableFilteredModels = useMemo(
    () => filteredModelKeys.some((id) => selectedSiteModelIds.includes(id)),
    [filteredModelKeys, selectedSiteModelIds],
  )

  function handleSitePolicyChange(nextPolicy: SiteModelScopePolicy) {
    onChange({
      sitePolicy: nextPolicy,
      modelPolicy: nextPolicy === 'allow_list' ? 'allow_list' : modelPolicy,
      siteModelIds: nextPolicy === 'allow_list' ? [] : siteModelIds,
      autoSelectSiteModels: nextPolicy === 'allow_list',
    })
  }

  function selectAllSites() {
    const selectableSiteIds = filteredSites.filter((site) => site.enabled).map((site) => site.id)
    onChange({ siteIds: mergeModelKeys(siteIds, selectableSiteIds) })
  }

  function clearAllSites() {
    const filteredIds = new Set(filteredSites.map((site) => site.id))
    onChange({ siteIds: siteIds.filter((id) => !filteredIds.has(id)) })
  }

  function selectFilteredModels() {
    onChange({ autoSelectSiteModels: false, siteModelIds: mergeModelKeys(selectedSiteModelIds, filteredModelKeys) })
  }

  function clearFilteredModels() {
    onChange({
      autoSelectSiteModels: false,
      siteModelIds: selectedSiteModelIds.filter((item) => !filteredModelKeys.includes(item)),
    })
  }

  return (
    <>
      <FormField label={t('form.fields.siteAccess')}>
        <Select value={sitePolicy} onValueChange={(value) => handleSitePolicyChange(value as SiteModelScopePolicy)}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent searchable={false}>
            <SelectItem value="allow_all">{t('form.fields.sitePolicyAll')}</SelectItem>
            <SelectItem value="allow_list">{t('form.fields.sitePolicyList')}</SelectItem>
          </SelectContent>
        </Select>
      </FormField>

      {sitePolicy === 'allow_list' ? (
        <>
          {showSiteGroups ? (
            <FormField label={t('form.fields.siteGroups')} description={t('form.fields.siteGroupsHint')}>
              <MultiSelect
                value={siteGroupIds}
                options={siteGroupOptions}
                placeholder={t('form.fields.siteGroupsPlaceholder')}
                searchPlaceholder={t('form.fields.siteGroupsSearch')}
                emptyText={t('form.fields.noSiteGroups')}
                disabled={siteGroupsLoading}
                onChange={(nextSiteGroupIds) => onChange({ siteGroupIds: nextSiteGroupIds, modelPolicy: 'allow_list' })}
              />
            </FormField>
          ) : null}

          <FormField
            label={t('form.fields.extraSites')}
            description={showSiteGroups ? t('form.fields.extraSitesHint') : undefined}
          >
            <div className="space-y-3">
              {showSiteGroups ? (
                <div className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-3 py-2 text-xs leading-5 text-muted-soft">
                  {t('form.fields.customSiteCount', { count: effectiveSiteIds.length, direct: siteIds.length, inherited: scope.inheritedSiteIds.length })}
                </div>
              ) : null}
              <div className="space-y-3 rounded-lg border border-[hsl(var(--glass-border))] p-3">
                <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_180px]">
                  <div className="relative">
                    <Search className="text-foreground/40 pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
                    <Input
                      value={siteSearch}
                      onChange={(event) => setSiteSearch(event.target.value)}
                      placeholder={t('form.fields.siteSearch')}
                      className="h-9 pl-9"
                    />
                  </div>
                  <Select value={siteTypeFilter} onValueChange={setSiteTypeFilter}>
                    <SelectTrigger className="h-9">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent searchable={false}>
                      <SelectItem value="all">{t('form.fields.siteTypeAll')}</SelectItem>
                      {siteTypeItems.map((item) => (
                        <SelectItem key={item.label} value={item.label}>
                          {item.label} ({item.count})
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <Checkbox
                    checked={showDisabledSites}
                    onCheckedChange={setShowDisabledSites}
                    label={t('form.fields.showDisabledSites')}
                  />
                  <div className="ml-auto flex items-center gap-2">
                    <Button size="sm" variant="outline" className="h-8 border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]" onClick={selectAllSites} disabled={!hasSelectableFilteredSites}>
                      {t('form.fields.selectAll')}
                    </Button>
                    <Button size="sm" variant="outline" className="h-8 border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]" onClick={clearAllSites} disabled={!hasClearableFilteredSites}>
                      {t('form.fields.deselectAll')}
                    </Button>
                  </div>
                </div>
                <div className="max-h-40 space-y-1 overflow-y-auto pr-1">
                  {sitesLoading ? (
                    <>
                      <Skeleton className="h-9 w-full" />
                      <Skeleton className="h-9 w-full" />
                    </>
                  ) : filteredSites.length ? (
                    filteredSites.map((site) => {
                      const checked = siteIds.includes(site.id)
                      const inherited = !checked && inheritedSiteIdSet.has(site.id)
                      const disabled = inherited || (!site.enabled && !checked)
                      return (
                        <label
                          key={site.id}
                          className={`flex items-center gap-3 rounded-md px-2 py-2 hover:bg-[hsl(var(--surface-subtle))] ${disabled ? 'cursor-default opacity-70' : 'cursor-pointer'}`}
                        >
                          <Checkbox
                            checked={checked || inherited}
                            disabled={disabled}
                            onCheckedChange={(nextChecked) => {
                              onChange({
                                siteIds: nextChecked && site.enabled
                                  ? [...siteIds, site.id]
                                  : siteIds.filter((id) => id !== site.id),
                                modelPolicy: 'allow_list',
                              })
                            }}
                            ariaLabel={site.name}
                          />
                          <span className="min-w-0 flex-1 truncate text-sm text-foreground">{site.name}</span>
                          {!site.enabled ? <span className="text-muted-soft shrink-0 text-xs">{t('form.fields.disabledSite')}</span> : null}
                          {inherited ? <span className="text-primary shrink-0 text-xs">{t('form.fields.inheritedFromGroup')}</span> : null}
                          <span className="text-muted-soft shrink-0 text-xs">{formatSiteTypeLabel(site.site_type)}</span>
                        </label>
                      )
                    })
                  ) : (
                    <div className="text-muted-soft py-6 text-center text-sm">{t('form.fields.noSites')}</div>
                  )}
                </div>
              </div>
            </div>
          </FormField>
        </>
      ) : null}

      <FormField label={t('form.fields.modelAccess')}>
        <Select
          value={effectiveModelPolicy}
          disabled={sitePolicy === 'allow_list'}
          onValueChange={(value) => onChange({ modelPolicy: value as SiteModelScopePolicy })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent searchable={false}>
            <SelectItem value="allow_all">{t('form.fields.modelPolicyAll')}</SelectItem>
            <SelectItem value="allow_list">{t('form.fields.modelPolicyList')}</SelectItem>
          </SelectContent>
        </Select>
      </FormField>

      {sitePolicy === 'allow_list' ? (
        <div className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-3 py-2 text-xs leading-5 text-muted-soft">
          {siteModelsLoading
            ? t('form.fields.modelScopeSummaryLoading', { siteCount: effectiveSiteIds.length })
            : t('form.fields.modelScopeSummary', { siteCount: effectiveSiteIds.length, modelCount: siteModelRows.length })}
        </div>
      ) : null}

      {effectiveModelPolicy === 'allow_list' ? (
        <div className="space-y-3 rounded-lg border border-[hsl(var(--glass-border))] p-3">
          {sitePolicy === 'allow_list' && effectiveSiteIds.length === 0 ? (
            <div className="text-muted-soft py-6 text-center text-sm">{t('form.fields.selectSitesFirst')}</div>
          ) : (
            <>
              <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_180px]">
                <div className="relative">
                  <Search className="text-foreground/40 pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
                  <Input
                    value={modelSearch}
                    onChange={(event) => setModelSearch(event.target.value)}
                    placeholder={t('form.fields.modelSearch')}
                    className="h-9 pl-9"
                  />
                </div>
                <Select value={modelBrand} onValueChange={setModelBrand}>
                  <SelectTrigger className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent searchable={false}>
                    <SelectItem value="all">{t('form.fields.brandAll')}</SelectItem>
                    {modelBrandItems.map((item) => (
                      <SelectItem key={item.provider} value={item.provider}>
                        {item.provider} ({item.count})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="text-muted-soft text-xs">{t('form.fields.selectedCount', { count: validSelectedSiteModelCount })}</div>
                <div className="flex items-center gap-2">
                  <Button size="sm" variant="outline" className="h-8 border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]" onClick={selectFilteredModels} disabled={!hasSelectableFilteredModels}>
                    {t('form.fields.selectAll')}
                  </Button>
                  <Button size="sm" variant="outline" className="h-8 border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]" onClick={clearFilteredModels} disabled={!hasClearableFilteredModels}>
                    {t('form.fields.deselectAll')}
                  </Button>
                </div>
              </div>
              <div className="max-h-56 space-y-1 overflow-y-auto pr-1">
                {canonicalModelsLoading || siteModelsLoading ? (
                  <>
                    <Skeleton className="h-9 w-full" />
                    <Skeleton className="h-9 w-full" />
                    <Skeleton className="h-9 w-full" />
                  </>
                ) : filteredModels.length ? (
                  filteredModels.map(({ site, model }) => {
                    const checked = selectedSiteModelIds.includes(model.id)
                    const canonical = canonicalById.get(model.canonical_model_id ?? '')
                    const icon = siteModelIconInfo(model, canonicalById, site)

                    return (
                      <label
                        key={model.id}
                        className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-2 hover:bg-[hsl(var(--surface-subtle))]"
                      >
                        <Checkbox
                          checked={checked}
                          onCheckedChange={(nextChecked) => {
                            onChange({
                              autoSelectSiteModels: false,
                              siteModelIds: nextChecked
                                ? mergeModelKeys(selectedSiteModelIds, [model.id])
                                : selectedSiteModelIds.filter((item) => item !== model.id),
                            })
                          }}
                          ariaLabel={model.upstream_model_name}
                        />
                        <BrandMark
                          iconPath={icon.iconPath}
                          label={icon.label}
                          fallback={icon.fallback}
                          fallbackText={icon.fallbackText}
                          size="sm"
                        />
                        <span className="min-w-0 flex-1 truncate text-sm text-foreground" title={`${site.name} / ${model.upstream_model_name}`}>
                          {site.name} / {model.upstream_model_name}
                        </span>
                        <span className="text-muted-soft shrink-0 text-xs">{canonical?.model_key ?? model.display_name}</span>
                      </label>
                    )
                  })
                ) : (
                  <div className="text-muted-soft py-8 text-center text-sm">{t('form.fields.noModels')}</div>
                )}
              </div>
            </>
          )}
        </div>
      ) : null}
    </>
  )
}
