import { useMemo, useState, type ReactNode } from 'react'
import { useQueries } from '@tanstack/react-query'
import { ChevronDown, LoaderCircle, Plus, Search, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { DateTimePicker } from '@/components/ui/date-time-picker'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { MultiSelect } from '@/components/ui/multi-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/lib/toast'
import type { APIKeyUpsertInput, DownstreamAPIKey, ModelRule } from '@/features/api-keys/api/api-keys'
import {
  dateToRFC3339,
  buildMappingModelKeys,
  formValuesFromAPIKey,
  formatSiteTypeLabel,
  mergeModelKeys,
  modelProviderLabel,
} from '@/features/api-keys/lib/api-key-utils'
import type { APIKeyFormValues } from '@/features/api-keys/lib/types'
import {
  listSiteModels,
  sitesQueryKeys,
  type CanonicalModelItem,
  type Site,
  type SiteModel,
} from '@/features/sites/api/sites'
import { siteModelItems } from '@/features/sites/lib/site-cache'
import { sortSitesByName } from '@/features/sites/lib/site-utils'
import type { SiteGroup } from '@/features/settings/api/site-groups'
import type { OAuthConnectionListItem } from '@/features/oauth/api/oauth'

const CUSTOM_API_KEY_PATTERN = /^[A-Za-z0-9-]+$/

const MODEL_KEY_PREFIXES = ['models/', 'model/', 'openai/', 'anthropic/', 'google/', 'gemini/', 'deepseek/', 'xai/', 'moonshotai/', 'nvidia/']

function normalizeMappingModelKeyBase(value: string) {
  let normalized = value.trim().toLowerCase()
  for (const prefix of MODEL_KEY_PREFIXES) {
    if (normalized.startsWith(prefix)) normalized = normalized.slice(prefix.length)
  }
  normalized = normalized.replaceAll('+', ' plus ')
  normalized = normalized.replace(/[^a-z0-9.]+/g, '-')
  return normalized.replace(/^[-.]+/, '')
}

function normalizeMappingModelKey(value: string) {
  return normalizeMappingModelKeyBase(value).replace(/[-.]+$/, '')
}

function mappingPatternIdentity(pattern: string) {
  const trimmed = pattern.trim()
  if (!trimmed) return ''
  if (trimmed === '*') return '*'
  if (trimmed.endsWith('*')) {
    const rawPrefix = trimmed.slice(0, -1)
    if (rawPrefix.includes('*')) return ''
    const prefix = normalizeMappingModelKeyBase(rawPrefix)
    return prefix ? `wildcard:${prefix}` : ''
  }
  if (trimmed.includes('*')) return ''
  const normalized = normalizeMappingModelKey(trimmed)
  return normalized ? `exact:${normalized}` : ''
}

export function APIKeyFormDraw({
  open,
  initialKey,
  canonicalModels,
  canonicalModelsLoading,
  sites,
  sitesLoading,
  siteGroups,
  siteGroupsLoading,
  oauthConnections,
  oauthConnectionsLoading,
  pending,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  initialKey: DownstreamAPIKey | null
  canonicalModels: CanonicalModelItem[]
  canonicalModelsLoading: boolean
  sites: Site[]
  sitesLoading: boolean
  siteGroups: SiteGroup[]
  siteGroupsLoading: boolean
  oauthConnections: OAuthConnectionListItem[]
  oauthConnectionsLoading: boolean
  pending: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (input: APIKeyUpsertInput) => void
}) {
  const { t } = useTranslation('api-keys')
  const [values, setValues] = useState<APIKeyFormValues>(() => formValuesFromAPIKey(initialKey))
  const [autoSelectSiteModels, setAutoSelectSiteModels] = useState(() => {
    const initialValues = formValuesFromAPIKey(initialKey)
    return initialValues.sitePolicy === 'allow_list' && initialValues.siteModelIds.length === 0
  })
  const [siteSearch, setSiteSearch] = useState('')
  const [siteTypeFilter, setSiteTypeFilter] = useState('all')
  const [showDisabledSites, setShowDisabledSites] = useState(true)
  const [modelSearch, setModelSearch] = useState('')
  const [modelBrand, setModelBrand] = useState('all')
  const [advancedExpanded, setAdvancedExpanded] = useState(false)
  const [mappingEntries, setMappingEntries] = useState<ModelRule[]>(() =>
    formValuesFromAPIKey(initialKey).modelMappings.map((rule) => ({ ...rule })),
  )
  const duplicateMappingKeys = useMemo(() => {
    const seen = new Map<string, number>()
    for (const rule of mappingEntries) {
      const identity = mappingPatternIdentity(rule.pattern)
      if (identity) seen.set(identity, (seen.get(identity) ?? 0) + 1)
    }
    const dupes = new Set<string>()
    for (const [identity, count] of seen) {
      if (count > 1) dupes.add(identity)
    }
    return dupes
  }, [mappingEntries])

  const sortedSites = useMemo(
    () => sortSitesByName(sites),
    [sites],
  )
  const enabledSites = useMemo(
    () => sortedSites.filter((s) => s.enabled),
    [sortedSites],
  )
  const sitesById = useMemo(() => new Map(sortedSites.map((site) => [site.id, site])), [sortedSites])
  const siteGroupOptions = useMemo(() => siteGroups.filter((group) => group.enabled).map((group) => ({
    value: group.id,
    label: group.name,
    description: t('form.fields.groupSiteCount', { count: group.sites.length }),
  })), [siteGroups, t])
  const autoResetOAuthOptions = useMemo(
    () => oauthConnections
      .filter((connection) => (
        (connection.provider === 'codex' || connection.provider === 'claude_code') &&
        (connection.status === 'connected' || connection.status === 'pending_sync')
      ))
      .sort((left, right) => (left.email ?? left.account_id ?? '').localeCompare(right.email ?? right.account_id ?? '')),
    [oauthConnections],
  )
  const inheritedSiteIds = useMemo(() => {
    if (values.sitePolicy !== 'allow_list') return []
    const ids = new Set<string>()
    const selectedGroups = new Set(values.siteGroupIds)
    for (const group of siteGroups) {
      if (!selectedGroups.has(group.id) || !group.enabled) continue
      for (const site of group.sites) {
        if (sitesById.has(site.site_id)) ids.add(site.site_id)
      }
    }
    return [...ids]
  }, [siteGroups, sitesById, values.siteGroupIds, values.sitePolicy])
  const selectedDirectSiteIdSet = useMemo(() => new Set(values.siteIds), [values.siteIds])
  const effectiveSiteIds = useMemo(() => {
    if (values.sitePolicy !== 'allow_list') return enabledSites.map((site) => site.id)
    const ids = new Set([...inheritedSiteIds, ...values.siteIds])
    return sortedSites.filter((site) => ids.has(site.id)).map((site) => site.id)
  }, [enabledSites, inheritedSiteIds, sortedSites, values.siteIds, values.sitePolicy])
  const inheritedSiteIdSet = useMemo(() => new Set(inheritedSiteIds), [inheritedSiteIds])
  const visibleSiteOptions = useMemo(() => {
    if (values.sitePolicy !== 'allow_list') return enabledSites
    return sortedSites.filter((site) => (
      site.enabled ||
      showDisabledSites ||
      selectedDirectSiteIdSet.has(site.id) ||
      inheritedSiteIdSet.has(site.id)
    ))
  }, [enabledSites, inheritedSiteIdSet, selectedDirectSiteIdSet, showDisabledSites, sortedSites, values.sitePolicy])

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

  const effectiveModelPolicy = values.sitePolicy === 'allow_list' ? 'allow_list' : values.modelPolicy
  const requestedSiteModelIds = useMemo(() => {
    const ids = new Set<string>()
    if (effectiveModelPolicy === 'allow_list' || advancedExpanded) {
      for (const siteID of effectiveSiteIds) ids.add(siteID)
    }
    if (values.imageBridgeEnabled) {
      for (const site of enabledSites) ids.add(site.id)
    }
    return [...ids]
  }, [advancedExpanded, effectiveModelPolicy, effectiveSiteIds, enabledSites, values.imageBridgeEnabled])
  const siteModelQueries = useQueries({
    queries: requestedSiteModelIds.map((siteID) => ({
      queryKey: sitesQueryKeys.models(siteID),
      queryFn: async () => {
        try {
          return await listSiteModels(siteID)
        } catch {
          return { items: [] as SiteModel[] }
        }
      },
    })),
  })
  const siteModelsMap = useMemo(() => {
    const result: Record<string, SiteModel[]> = {}
    requestedSiteModelIds.forEach((siteID, index) => {
      result[siteID] = siteModelItems(siteModelQueries[index]?.data)
    })
    return result
  }, [requestedSiteModelIds, siteModelQueries])
  const loadingSiteModelIds = useMemo(() => new Set(
    requestedSiteModelIds.filter((_, index) => siteModelQueries[index]?.isLoading),
  ), [requestedSiteModelIds, siteModelQueries])
  const siteModelsLoading = effectiveSiteIds.some((siteID) => loadingSiteModelIds.has(siteID))
  const bridgeSiteModelsLoading = values.imageBridgeEnabled && enabledSites.some((site) => loadingSiteModelIds.has(site.id))
  const mappingModelsLoading = advancedExpanded && (sitesLoading || canonicalModelsLoading || siteModelsLoading)
  const saveDisabled = pending || (values.sitePolicy === 'allow_list' && siteModelsLoading)

  const siteModelRows = useMemo(() => {
    return effectiveSiteIds.flatMap((siteId) => {
      const site = sitesById.get(siteId)
      return (siteModelsMap[siteId] ?? [])
        .filter((model) => model.status === 'active')
        .map((model) => ({ site, model }))
        .filter((item): item is { site: Site; model: SiteModel } => Boolean(item.site))
    })
  }, [effectiveSiteIds, siteModelsMap, sitesById])
  const availableSiteModelIds = useMemo(
    () => new Set(siteModelRows.map(({ model }) => model.id)),
    [siteModelRows],
  )
  const allSiteModelIds = useMemo(
    () => siteModelRows.map(({ model }) => model.id),
    [siteModelRows],
  )
  const selectedSiteModelIds = useMemo(() => {
    if (values.sitePolicy === 'allow_list' && autoSelectSiteModels) {
      return allSiteModelIds
    }
    return values.siteModelIds
  }, [allSiteModelIds, autoSelectSiteModels, values.siteModelIds, values.sitePolicy])
  const validSelectedSiteModelCount = useMemo(
    () => selectedSiteModelIds.filter((id) => availableSiteModelIds.has(id)).length,
    [availableSiteModelIds, selectedSiteModelIds],
  )

  const modelBrandItems = useMemo(() => {
    const canonicalById = new Map(canonicalModels.map((model) => [model.id, model]))
    const counts = new Map<string, number>()
    for (const { site, model } of siteModelRows) {
      const canonical = model.canonical_model_id ? canonicalById.get(model.canonical_model_id) : undefined
      const provider = canonical ? modelProviderLabel(canonical) : formatSiteTypeLabel(site.site_type)
      counts.set(provider, (counts.get(provider) ?? 0) + 1)
    }
    return [...counts.entries()]
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([provider, count]) => ({ provider, count }))
  }, [canonicalModels, siteModelRows])

  const filteredModels = useMemo(() => {
    const keyword = modelSearch.trim().toLowerCase()
    const canonicalById = new Map(canonicalModels.map((model) => [model.id, model]))
    return siteModelRows.filter(({ site, model }) => {
      const canonical = model.canonical_model_id ? canonicalById.get(model.canonical_model_id) : undefined
      const provider = canonical ? modelProviderLabel(canonical) : formatSiteTypeLabel(site.site_type)
      const matchesBrand = modelBrand === 'all' || provider === modelBrand
      const text = `${model.upstream_model_name} ${model.display_name} ${canonical?.model_key ?? ''} ${site.name} ${site.slug} ${site.site_type}`.toLowerCase()
      return matchesBrand && (!keyword || text.includes(keyword))
    })
  }, [canonicalModels, modelBrand, modelSearch, siteModelRows])

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
  const bridgeImageRows = useMemo(() => {
    const canonicalById = new Map(canonicalModels.map((model) => [model.id, model.model_key]))
    const rows: { siteId: string; modelKey: string }[] = []
    for (const site of enabledSites) {
      for (const model of siteModelsMap[site.id] ?? []) {
        if (model.status !== 'active') continue
        const modelKey = (model.canonical_model_id ? canonicalById.get(model.canonical_model_id) : undefined)
          ?? model.upstream_model_name ?? ''
        if (!modelKey) continue
        const text = `${modelKey} ${model.upstream_model_name ?? ''}`.toLowerCase()
        if (!text.includes('image')) continue
        rows.push({ siteId: site.id, modelKey })
      }
    }
    return rows
  }, [canonicalModels, enabledSites, siteModelsMap])
  const imageBridgeModelKeys = useMemo(() => {
    const keys = [...new Set(bridgeImageRows.map((row) => row.modelKey))].sort()
    const current = values.imageBridgeModel.trim()
    if (current && !keys.includes(current)) {
      return [current, ...keys]
    }
    return keys
  }, [bridgeImageRows, values.imageBridgeModel])
  const imageBridgeSites = useMemo(() => {
    const model = values.imageBridgeModel.trim()
    if (!model) return []
    const siteIds = new Set(bridgeImageRows.filter((row) => row.modelKey === model).map((row) => row.siteId))
    return enabledSites.filter((site) => siteIds.has(site.id))
  }, [bridgeImageRows, enabledSites, values.imageBridgeModel])

  const mappingModelKeys = useMemo(() => {
    return buildMappingModelKeys(
      canonicalModels,
      siteModelRows.map(({ model }) => model),
      selectedSiteModelIds,
      mappingEntries.map((rule) => rule.target),
    )
  }, [canonicalModels, mappingEntries, selectedSiteModelIds, siteModelRows])

  function selectFilteredModels() {
    setAutoSelectSiteModels(false)
    setValues((current) => ({
      ...current,
      siteModelIds: mergeModelKeys(selectedSiteModelIds, filteredModelKeys),
    }))
  }

  function clearFilteredModels() {
    setAutoSelectSiteModels(false)
    setValues((current) => ({
      ...current,
      siteModelIds: selectedSiteModelIds.filter((item) => !filteredModelKeys.includes(item)),
    }))
  }

  function selectAllSites() {
    const selectableSiteIds = filteredSites.filter((site) => site.enabled).map((site) => site.id)
    setValues((current) => ({
      ...current,
      siteIds: mergeModelKeys(current.siteIds, selectableSiteIds),
    }))
  }

  function clearAllSites() {
    const filteredIds = new Set(filteredSites.map((site) => site.id))
    setValues((current) => ({ ...current, siteIds: current.siteIds.filter((id) => !filteredIds.has(id)) }))
  }

  function handleSubmit() {
    const name = values.name.trim()
    if (!name) {
      toast.error(t('form.validation.nameRequired'))
      return
    }
    const customKey = values.customKey
    if (!initialKey && values.useCustomKey) {
      if (!customKey) {
        toast.error(t('form.validation.customKeyRequired'))
        return
      }
      if (customKey.trim() !== customKey) {
        toast.error(t('form.validation.customKeyWhitespace'))
        return
      }
      if (!CUSTOM_API_KEY_PATTERN.test(customKey)) {
        toast.error(t('form.validation.customKeyFormat'))
        return
      }
    }

    const parsedQuotaLimit = parseQuotaLimit(values.quotaLimit)
    if (parsedQuotaLimit === false) {
      toast.error(t('form.validation.quotaInvalid'))
      return
    }
    const quotaTotalUsed = initialKey?.quota_total_used ?? initialKey?.quota_used ?? 0
    if (parsedQuotaLimit != null && quotaTotalUsed > 0 && parsedQuotaLimit < quotaTotalUsed) {
      toast.error(t('form.validation.quotaTooLow', { used: quotaTotalUsed }))
      return
    }
    if (parsedQuotaLimit == null && values.autoResetOAuthConnectionId) {
      toast.error(t('form.validation.autoResetQuotaRequired'))
      return
    }

    const parsedQuotaDailyLimit = parseQuotaLimit(values.quotaDailyLimit)
    const quotaDailyUsed = initialKey?.quota_daily_used ?? 0
    if (parsedQuotaDailyLimit === false) {
      toast.error(t('form.validation.quotaDailyInvalid'))
      return
    }
    if (parsedQuotaDailyLimit != null && parsedQuotaDailyLimit < quotaDailyUsed) {
      toast.error(t('form.validation.quotaDailyTooLow', { used: quotaDailyUsed }))
      return
    }

    const parsedQuotaWeeklyLimit = parseQuotaLimit(values.quotaWeeklyLimit)
    const quotaWeeklyUsed = initialKey?.quota_weekly_used ?? 0
    if (parsedQuotaWeeklyLimit === false) {
      toast.error(t('form.validation.quotaWeeklyInvalid'))
      return
    }
    if (parsedQuotaWeeklyLimit != null && parsedQuotaWeeklyLimit < quotaWeeklyUsed) {
      toast.error(t('form.validation.quotaWeeklyTooLow', { used: quotaWeeklyUsed }))
      return
    }

    const parsedRPMLimit = parsePositiveIntegerLimit(values.rpmLimit)
    const parsedTPMLimit = parsePositiveIntegerLimit(values.tpmLimit)
    if (values.rateLimitEnabled && !parsedRPMLimit && !parsedTPMLimit) {
      toast.error(t('form.validation.rateLimitRequired'))
      return
    }
    if (parsedRPMLimit === false) {
      toast.error(t('form.validation.rpmInvalid'))
      return
    }
    if (parsedTPMLimit === false) {
      toast.error(t('form.validation.tpmInvalid'))
      return
    }

    const expiresAt = values.expiresPermanent ? null : dateToRFC3339(values.expiresAt)
    if (!values.expiresPermanent && !expiresAt) {
      toast.error(t('form.validation.expiresRequired'))
      return
    }
    if (expiresAt && new Date(expiresAt).getTime() <= Date.now()) {
      toast.error(t('form.validation.expiresPast'))
      return
    }

    const modelMappings: ModelRule[] = []
    const mappingIdentities = new Set<string>()
    for (const rule of mappingEntries) {
      const pattern = rule.pattern.trim()
      const target = rule.target.trim()
      if (!pattern || !target) continue
      const identity = mappingPatternIdentity(pattern)
      if (mappingIdentities.has(identity)) {
        toast.error(t('form.validation.mappingDuplicate', { key: pattern }))
        return
      }
      mappingIdentities.add(identity)
      modelMappings.push({ pattern, target, mode: rule.mode ?? 'hard' })
    }

    if (values.imageBridgeEnabled && !values.imageBridgeModel.trim()) {
      toast.error(t('form.validation.imageBridgeModelRequired'))
      return
    }

    const submittedSiteModelIds = effectiveModelPolicy === 'allow_list'
      ? selectedSiteModelIds.filter((id) => availableSiteModelIds.has(id))
      : []

    onSubmit({
      name,
      customKey: !initialKey && values.useCustomKey ? customKey : undefined,
      status: values.enabled ? 'active' : 'disabled',
      sitePolicy: values.sitePolicy,
      siteIds: values.sitePolicy === 'allow_list' ? values.siteIds : [],
      siteGroupIds: values.sitePolicy === 'allow_list' ? values.siteGroupIds : [],
      modelPolicy: effectiveModelPolicy,
      siteModelIds: submittedSiteModelIds,
      modelMappings,
      imageToolBridge: {
        enabled: values.imageBridgeEnabled,
        model: values.imageBridgeModel.trim(),
        site_id: values.imageBridgeSiteId !== 'auto' ? values.imageBridgeSiteId : null,
        max_calls: null,
      },
      quotaLimit: parsedQuotaLimit,
      quotaUnlimited: parsedQuotaLimit == null,
      quotaDailyLimit: parsedQuotaDailyLimit,
      quotaDailyUnlimited: parsedQuotaDailyLimit == null,
      quotaWeeklyLimit: parsedQuotaWeeklyLimit,
      quotaWeeklyUnlimited: parsedQuotaWeeklyLimit == null,
      autoResetOAuthConnectionId: values.autoResetOAuthConnectionId || null,
      rateLimit: {
        status: values.rateLimitEnabled ? 'enabled' : 'disabled',
        rpm_limit: parsedRPMLimit || null,
        tpm_limit: parsedTPMLimit || null,
      },
      expiresAt,
    })
  }

  const customKeyInputError = !initialKey && values.useCustomKey && values.customKey && !CUSTOM_API_KEY_PATTERN.test(values.customKey)
    ? t('form.validation.customKeyFormat')
    : undefined

  return (
    <Draw open={open} onOpenChange={onOpenChange}>
      <DrawContent
        side="right"
        size="wide"
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        <DrawHeader>
          <DrawTitle>{initialKey ? t('form.editTitle') : t('form.createTitle')}</DrawTitle>
        </DrawHeader>
        <DrawBody className="space-y-0">
          <FormSection title={t('form.sections.basic')}>
            <FormField label={t('form.fields.name')} required>
              <Input
                value={values.name}
                onChange={(event) => setValues((current) => ({ ...current, name: event.target.value }))}
                placeholder={t('form.fields.namePlaceholder')}
              />
            </FormField>

            <Switch
              checked={values.enabled}
              onCheckedChange={(checked) => setValues((current) => ({ ...current, enabled: checked }))}
              label={t('form.fields.enabled')}
            />

            {!initialKey ? (
              <>
                <Switch
                  checked={values.useCustomKey}
                  onCheckedChange={(checked) => setValues((current) => ({ ...current, useCustomKey: checked }))}
                  label={t('form.fields.useCustomKey')}
                />

                {values.useCustomKey ? (
                  <FormField label={t('form.fields.customKey')} error={customKeyInputError} required>
                    <Input
                      value={values.customKey}
                      onChange={(event) => setValues((current) => ({ ...current, customKey: event.target.value }))}
                      placeholder={t('form.fields.customKeyPlaceholder')}
                      autoComplete="off"
                      aria-invalid={Boolean(customKeyInputError)}
                      spellCheck={false}
                    />
                  </FormField>
                ) : null}
              </>
            ) : null}
          </FormSection>

          <FormSection title={t('form.sections.access')} divided>
            <FormField label={t('form.fields.siteAccess')}>
              <Select
                value={values.sitePolicy}
                onValueChange={(value) => {
                  const sitePolicy = value as 'allow_all' | 'allow_list'
                  setAutoSelectSiteModels(sitePolicy === 'allow_list')
                  setValues((current) => ({
                    ...current,
                    sitePolicy,
                    modelPolicy: sitePolicy === 'allow_list' ? 'allow_list' : current.modelPolicy,
                    siteModelIds: sitePolicy === 'allow_list' ? [] : current.siteModelIds,
                  }))
                }}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent searchable={false}>
                  <SelectItem value="allow_all">{t('form.fields.sitePolicyAll')}</SelectItem>
                  <SelectItem value="allow_list">{t('form.fields.sitePolicyList')}</SelectItem>
                </SelectContent>
              </Select>
            </FormField>

            {values.sitePolicy === 'allow_list' ? (
              <>
                <FormField label={t('form.fields.siteGroups')} description={t('form.fields.siteGroupsHint')}>
                  <MultiSelect
                    value={values.siteGroupIds}
                    options={siteGroupOptions}
                    placeholder={t('form.fields.siteGroupsPlaceholder')}
                    searchPlaceholder={t('form.fields.siteGroupsSearch')}
                    emptyText={t('form.fields.noSiteGroups')}
                    disabled={siteGroupsLoading}
                    onChange={(siteGroupIds) => {
                      setValues((current) => ({ ...current, siteGroupIds, modelPolicy: 'allow_list' }))
                    }}
                  />
                </FormField>

                <FormField label={t('form.fields.extraSites')} description={t('form.fields.extraSitesHint')}>
                  <div className="space-y-3">
                    <div className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-3 py-2 text-xs leading-5 text-muted-soft">
                      {t('form.fields.customSiteCount', { count: effectiveSiteIds.length, direct: values.siteIds.length, inherited: inheritedSiteIds.length })}
                    </div>
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
                            const checked = values.siteIds.includes(site.id)
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
                                    setValues((current) => ({
                                      ...current,
                                      siteIds: nextChecked && site.enabled
                                        ? [...current.siteIds, site.id]
                                        : current.siteIds.filter((id) => id !== site.id),
                                      modelPolicy: 'allow_list',
                                    }))
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
                disabled={values.sitePolicy === 'allow_list'}
                onValueChange={(value) => setValues((current) => ({
                  ...current,
                  modelPolicy: value as 'allow_all' | 'allow_list',
                }))}
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

            {values.sitePolicy === 'allow_list' ? (
              <div className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-3 py-2 text-xs leading-5 text-muted-soft">
                {siteModelsLoading
                  ? t('form.fields.modelScopeSummaryLoading', { siteCount: effectiveSiteIds.length })
                  : t('form.fields.modelScopeSummary', { siteCount: effectiveSiteIds.length, modelCount: siteModelRows.length })}
              </div>
            ) : null}

          {effectiveModelPolicy === 'allow_list' ? (
            <div className="space-y-3 rounded-lg border border-[hsl(var(--glass-border))] p-3">
              {values.sitePolicy === 'allow_list' && effectiveSiteIds.length === 0 ? (
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
                        const canonical = canonicalModels.find((item) => item.id === model.canonical_model_id)

                        return (
                          <label
                            key={model.id}
                            className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-2 hover:bg-[hsl(var(--surface-subtle))]"
                          >
                            <Checkbox
                              checked={checked}
                              onCheckedChange={(nextChecked) => {
                                setAutoSelectSiteModels(false)
                                setValues((current) => ({
                                  ...current,
                                  siteModelIds: nextChecked
                                    ? mergeModelKeys(selectedSiteModelIds, [model.id])
                                    : selectedSiteModelIds.filter((item) => item !== model.id),
                                }))
                              }}
                              ariaLabel={model.upstream_model_name}
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
          </FormSection>

          <FormSection title={t('form.sections.expires')} divided>
            <Switch
              checked={values.expiresPermanent}
              onCheckedChange={(checked) => setValues((current) => ({ ...current, expiresPermanent: checked }))}
              label={t('form.fields.neverExpire')}
            />

            {!values.expiresPermanent ? (
              <FormField label={t('form.fields.expiresDate')} required>
                <DateTimePicker
                  value={values.expiresAt}
                  onValueChange={(date) => setValues((current) => {
                    // First day selection starts at 00:00; keep the previous
                    // end-of-day default so picking only a date behaves as before.
                    if (date && !current.expiresAt && date.getHours() === 0 && date.getMinutes() === 0) {
                      const endOfDay = new Date(date)
                      endOfDay.setHours(23, 59, 0, 0)
                      return { ...current, expiresAt: endOfDay }
                    }
                    return { ...current, expiresAt: date }
                  })}
                  placeholder={t('form.fields.pickDate')}
                  disablePastDates
                  minuteStep={1}
                />
              </FormField>
            ) : null}
          </FormSection>

          <FormSection title={t('form.sections.quota')} divided>
            <FormField
              label={t('form.fields.quotaLimit')}
              description={t('form.fields.quotaDesc', { used: initialKey?.quota_total_used ?? initialKey?.quota_used ?? 0 })}
            >
              <QuotaAmountInput
                value={values.quotaLimit}
                onChange={(quotaLimit) => setValues((current) => ({ ...current, quotaLimit }))}
              />
            </FormField>
            <FormField
              label={t('form.fields.quotaDailyLimit')}
              description={t('form.fields.quotaDailyDesc', { used: initialKey?.quota_daily_used ?? 0 })}
            >
              <QuotaAmountInput
                value={values.quotaDailyLimit}
                onChange={(quotaDailyLimit) => setValues((current) => ({ ...current, quotaDailyLimit }))}
              />
            </FormField>
            <FormField
              label={t('form.fields.quotaWeeklyLimit')}
              description={t('form.fields.quotaWeeklyDesc', { used: initialKey?.quota_weekly_used ?? 0 })}
            >
              <QuotaAmountInput
                value={values.quotaWeeklyLimit}
                onChange={(quotaWeeklyLimit) => setValues((current) => ({ ...current, quotaWeeklyLimit }))}
              />
            </FormField>
            <FormField
              label={t('form.fields.autoReset')}
              description={t('form.fields.autoResetDesc')}
            >
              <Select
                value={values.autoResetOAuthConnectionId || 'none'}
                disabled={oauthConnectionsLoading || parseQuotaLimit(values.quotaLimit) == null}
                onValueChange={(value) => setValues((current) => ({
                  ...current,
                  autoResetOAuthConnectionId: value === 'none' ? '' : value,
                }))}
              >
                <SelectTrigger>
                  <SelectValue placeholder={t('form.fields.autoResetNone')} />
                </SelectTrigger>
                <SelectContent searchable={false}>
                  <SelectItem value="none">{t('form.fields.autoResetNone')}</SelectItem>
                  {autoResetOAuthOptions.map((connection) => (
                    <SelectItem key={connection.id} value={connection.id}>
                      {connection.provider} · {connection.email ?? connection.account_id ?? connection.id.slice(0, 8)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {values.autoResetOAuthConnectionId ? (
                <div className="mt-2 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-3 py-2 text-xs leading-5 text-muted-soft">
                  {t('form.fields.autoResetSummary')}
                </div>
              ) : null}
            </FormField>
          </FormSection>

          <FormSection
            title={(
              <button
                type="button"
                className="flex w-full items-center gap-1.5 text-left text-sm font-semibold text-foreground transition-colors hover:text-foreground"
                onClick={() => setAdvancedExpanded((value) => !value)}
              >
                <ChevronDown className={`h-4 w-4 transition-transform ${advancedExpanded ? 'rotate-0' : '-rotate-90'}`} />
                {t('form.sections.advanced')}
              </button>
            )}
            divided
          >

            {advancedExpanded ? (
              <div className="space-y-5">
                <div className="space-y-3">
                  <div>
                    <span className="block text-sm font-medium">{t('form.advanced.modelMapping')}</span>
                    <p className="text-muted-soft mt-1 text-xs">{t('form.advanced.modelMappingDesc')}</p>
                  </div>
                  <div className="rounded-lg border border-[hsl(var(--glass-border))]">
                    {mappingEntries.length ? (
                      <div className="space-y-3 p-3">
                        {mappingEntries.map((rule, index) => {
                          const identity = mappingPatternIdentity(rule.pattern)
                          const isDuplicate = identity !== '' && duplicateMappingKeys.has(identity)
                          return (
                            <div key={index} className={index < mappingEntries.length - 1 ? 'border-b border-[hsl(var(--glass-divider))] pb-3' : ''}>
                              <div className="flex items-center gap-2">
                                <Input
                                  value={rule.pattern}
                                  placeholder={t('form.advanced.downstreamPlaceholder')}
                                  className={`flex-1 ${isDuplicate ? 'border-red-500 focus-visible:ring-red-500' : ''}`}
                                  onChange={(event) => {
                                    setMappingEntries((prev) => {
                                      const next = [...prev]
                                      next[index] = { ...next[index], pattern: event.target.value }
                                      return next
                                    })
                                  }}
                                />
                                <Select
                                  value={rule.target}
                                  disabled={mappingModelsLoading}
                                  onValueChange={(value) => {
                                    setMappingEntries((prev) => {
                                      const next = [...prev]
                                      next[index] = { ...next[index], target: value }
                                      return next
                                    })
                                  }}
                                >
                                  <SelectTrigger className="flex-1">
                                    {mappingModelsLoading ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                                    <SelectValue
                                      placeholder={t(mappingModelsLoading
                                        ? 'form.advanced.modelMappingLoading'
                                        : 'form.advanced.selectModelPlaceholder')}
                                    />
                                  </SelectTrigger>
                                  <SelectContent>
                                    {mappingModelsLoading ? (
                                      <SelectItem value="__mapping_loading" disabled>{t('form.advanced.modelMappingLoading')}</SelectItem>
                                    ) : mappingModelKeys.length ? (
                                      mappingModelKeys.map((modelKey) => (
                                        <SelectItem key={modelKey} value={modelKey}>{modelKey}</SelectItem>
                                      ))
                                    ) : (
                                      <SelectItem value="__mapping_empty" disabled>{t('form.advanced.modelMappingNoModels')}</SelectItem>
                                    )}
                                  </SelectContent>
                                </Select>
                              </div>
                              <div className="mt-2 flex items-center gap-2">
                                <Tabs
                                  className="flex-1"
                                  value={rule.mode === 'soft' ? 'soft' : 'hard'}
                                  onValueChange={(value) => {
                                    setMappingEntries((prev) => {
                                      const next = [...prev]
                                      next[index] = { ...next[index], mode: value === 'soft' ? 'soft' : 'hard' }
                                      return next
                                    })
                                  }}
                                >
                                  <TabsList className="h-9 w-full">
                                    <TabsTrigger value="hard" className="text-xs">{t('form.advanced.modeHard')}</TabsTrigger>
                                    <TabsTrigger value="soft" className="text-xs">{t('form.advanced.modeSoft')}</TabsTrigger>
                                  </TabsList>
                                </Tabs>
                                <Button
                                  size="icon"
                                  variant="ghost"
                                  className="h-9 w-9 shrink-0 text-muted-soft hover:text-red-400"
                                  onClick={() => {
                                    setMappingEntries((prev) => prev.filter((_, i) => i !== index))
                                  }}
                                >
                                  <Trash2 className="h-4 w-4" />
                                </Button>
                              </div>
                              {isDuplicate ? (
                                <p className="mt-1 pl-1 text-xs text-red-500">{t('form.advanced.duplicateKey')}</p>
                              ) : null}
                            </div>
                          )
                        })}
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setMappingEntries((prev) => [...prev, { pattern: '', target: '', mode: 'hard' }])}
                        >
                          <Plus className="mr-1 h-3.5 w-3.5" />{t('form.advanced.add')}
                        </Button>
                      </div>
                    ) : (
                      <div className="flex h-24 items-center justify-center">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setMappingEntries((prev) => [...prev, { pattern: '', target: '', mode: 'hard' }])}
                        >
                          <Plus className="mr-1 h-3.5 w-3.5" />{t('form.advanced.add')}
                        </Button>
                      </div>
                    )}
                  </div>
                </div>

                <div className="space-y-3 border-t border-[hsl(var(--glass-divider))] pt-5">
                  <Switch
                    checked={values.imageBridgeEnabled}
                    onCheckedChange={(checked) => setValues((current) => ({ ...current, imageBridgeEnabled: checked }))}
                    label={t('form.advanced.imageBridge')}
                    description={t('form.advanced.imageBridgeDesc')}
                  />
                  {values.imageBridgeEnabled ? (
                    <>
                      <FormField
                        label={t('form.advanced.imageBridgeModel')}
                        description={t('form.advanced.imageBridgeModelDesc')}
                        required
                      >
                        <Select
                          value={values.imageBridgeModel}
                          onValueChange={(value) => setValues((current) => ({
                            ...current,
                            imageBridgeModel: value,
                            imageBridgeSiteId: 'auto',
                          }))}
                        >
                          <SelectTrigger>
                            <SelectValue placeholder={t('form.advanced.selectModelPlaceholder')} />
                          </SelectTrigger>
                          <SelectContent>
                            {bridgeSiteModelsLoading ? (
                              <SelectItem value="__loading" disabled>{t('form.advanced.imageBridgeLoading')}</SelectItem>
                            ) : imageBridgeModelKeys.length ? (
                              imageBridgeModelKeys.map((modelKey) => (
                                <SelectItem key={modelKey} value={modelKey}>{modelKey}</SelectItem>
                              ))
                            ) : (
                              <SelectItem value="__none" disabled>{t('form.advanced.imageBridgeNoModels')}</SelectItem>
                            )}
                          </SelectContent>
                        </Select>
                      </FormField>
                      <FormField
                        label={t('form.advanced.imageBridgeSite')}
                        description={t('form.advanced.imageBridgeSiteDesc')}
                      >
                        <Select
                          value={values.imageBridgeSiteId}
                          onValueChange={(value) => setValues((current) => ({ ...current, imageBridgeSiteId: value }))}
                        >
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="auto">{t('form.advanced.imageBridgeSiteAuto')}</SelectItem>
                            {imageBridgeSites.map((site) => (
                              <SelectItem key={site.id} value={site.id}>{site.name}</SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </FormField>
                    </>
                  ) : null}
                </div>

                <div className="space-y-3 border-t border-[hsl(var(--glass-divider))] pt-5">
                  <Switch
                    checked={values.rateLimitEnabled}
                    onCheckedChange={(checked) => setValues((current) => ({ ...current, rateLimitEnabled: checked }))}
                    label={t('form.advanced.rateLimit')}
                    description={t('form.advanced.rateLimitDesc')}
                  />
                  {values.rateLimitEnabled ? (
                    <>
                      <FormField
                        label={t('form.advanced.rpm')}
                        description={t('form.advanced.rpmDesc')}
                      >
                        <Input
                          type="text"
                          inputMode="numeric"
                          value={values.rpmLimit}
                          onChange={(event) => setValues((current) => ({ ...current, rpmLimit: event.target.value }))}
                          placeholder="60"
                        />
                      </FormField>
                      <FormField
                        label={t('form.advanced.tpm')}
                        description={t('form.advanced.tpmDesc')}
                      >
                        <Input
                          type="text"
                          inputMode="numeric"
                          value={values.tpmLimit}
                          onChange={(event) => setValues((current) => ({ ...current, tpmLimit: event.target.value }))}
                          placeholder="100000"
                        />
                      </FormField>
                    </>
                  ) : null}
                </div>
              </div>
            ) : null}
          </FormSection>
        </DrawBody>
        <DrawFooter>
          <Button onClick={handleSubmit} disabled={saveDisabled}>
            {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {t('form.actions.save')}
          </Button>
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            {t('form.actions.cancel')}
          </Button>
        </DrawFooter>
      </DrawContent>
    </Draw>
  )
}

function QuotaAmountInput({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <div className="relative">
      <span className="pointer-events-none absolute left-4 top-1/2 z-10 -translate-y-1/2 text-sm text-muted-soft">$</span>
      <Input
        type="text"
        inputMode="decimal"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="pl-8"
        placeholder="0"
      />
    </div>
  )
}

function parsePositiveIntegerLimit(value: string): number | null | false {
  const trimmed = value.trim()
  if (!trimmed) return null
  const parsed = Number(trimmed)
  if (!Number.isInteger(parsed) || parsed <= 0) return false
  return parsed
}

function parseQuotaLimit(value: string): number | null | false {
  const trimmed = value.trim()
  if (!trimmed) return null
  const parsed = Number(trimmed)
  if (!Number.isFinite(parsed) || parsed < 0) return false
  return parsed === 0 ? null : parsed
}

function FormSection({
  title,
  divided,
  children,
}: {
  title: ReactNode
  divided?: boolean
  children: ReactNode
}) {
  return (
    <section className={`space-y-4 py-5 ${divided ? 'border-t border-[hsl(var(--glass-divider))]' : 'pt-0'}`}>
      {typeof title === 'string' ? <h3 className="text-sm font-semibold text-foreground">{title}</h3> : title}
      <div className="space-y-4">{children}</div>
    </section>
  )
}
