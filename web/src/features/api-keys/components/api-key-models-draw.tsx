import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Search } from 'lucide-react'
import { BrandMark } from '@/components/common/brand-mark'
import { buildModelGlyph, siteTypeIconPath } from '@/components/common/brand-utils'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { Badge } from '@/components/ui/badge'
import { Draw, DrawBody, DrawContent, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { enabledSiteModels } from '@/features/api-keys/lib/api-key-utils'
import type { CanonicalModelItem, Site } from '@/features/sites/api/sites'
import { getProviderCatalogEntry } from '@/lib/brands'

type ModelListRow = {
  modelKey: string
  displayName: string
  provider: string
  modelIconPath?: string
  siteId: string
  siteName: string
  siteType: string
  sitePriority: number
  siteGlobalEnabled: boolean
  siteKeyEnabled: boolean
  siteIconPath?: string
}

export function APIKeyModelsDraw({
  apiKey,
  canonicalModels,
  sites,
  onOpenChange,
}: {
  apiKey: DownstreamAPIKey | null
  canonicalModels: CanonicalModelItem[]
  sites: Site[]
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation('api-keys')
  const [search, setSearch] = useState('')
  const [filterModel, setFilterModel] = useState('')

  const isAllowListModel = apiKey?.model_policy === 'allow_list'

  const allRows = useMemo<ModelListRow[]>(() => {
    if (!apiKey) return []

    const canonicalByKey = new Map(canonicalModels.map((m) => [m.model_key, m]))
    const siteById = new Map(sites.map((s) => [s.id, s]))
    const keySiteById = new Map((apiKey.sites ?? []).map((s) => [s.site_id, s]))

    let result: ModelListRow[]

    if (isAllowListModel) {
      result = enabledSiteModels(apiKey).map((sm) => {
        const canonicalKey = sm.canonical_model_key || sm.model_key || ''
        const canonical = canonicalByKey.get(canonicalKey)
        const provider = canonical?.provider ?? sm.site_type ?? ''
        const modelEntry = getProviderCatalogEntry(provider)
        const siteId = sm.site_id ?? ''
        const site = siteById.get(siteId)
        const keySite = keySiteById.get(siteId)
        return {
          modelKey: canonicalKey,
          displayName: sm.display_name || canonical?.display_name || canonicalKey,
          provider,
          modelIconPath: modelEntry?.iconPath,
          siteId,
          siteName: sm.site_name || site?.name || siteId,
          siteType: sm.site_type || site?.site_type || '',
          sitePriority: site?.routing_priority ?? 0,
          siteGlobalEnabled: site?.enabled ?? true,
          siteKeyEnabled: keySite?.enabled !== false,
          siteIconPath: (sm.site_type || site?.site_type)
            ? siteTypeIconPath(sm.site_type || site?.site_type || '')
            : undefined,
        }
      })
    } else {
      result = canonicalModels.map((m) => {
        const entry = getProviderCatalogEntry(m.provider)
        return {
          modelKey: m.model_key,
          displayName: m.display_name,
          provider: m.provider,
          modelIconPath: entry?.iconPath,
          siteId: '',
          siteName: '',
          siteType: '',
          sitePriority: 0,
          siteGlobalEnabled: true,
          siteKeyEnabled: true,
          siteIconPath: undefined,
        }
      })
    }

    result.sort((a, b) => {
      if (b.sitePriority !== a.sitePriority) return b.sitePriority - a.sitePriority
      if (a.siteId !== b.siteId) return a.siteName.localeCompare(b.siteName)
      return a.modelKey.localeCompare(b.modelKey)
    })

    return result
  }, [apiKey, canonicalModels, sites, isAllowListModel])

  const uniqueModelKeys = useMemo<string[]>(() => {
    const seen = new Set<string>()
    for (const row of allRows) {
      if (row.modelKey) seen.add(row.modelKey)
    }
    return Array.from(seen).sort()
  }, [allRows])

  const rows = useMemo<ModelListRow[]>(() => {
    const keyword = search.trim().toLowerCase()
    return allRows.filter((r) => {
      if (filterModel && r.modelKey !== filterModel) return false
      if (!keyword) return true
      return `${r.modelKey} ${r.displayName} ${r.siteName}`.toLowerCase().includes(keyword)
    })
  }, [allRows, search, filterModel])

  return (
    <Draw open={Boolean(apiKey)} onOpenChange={onOpenChange}>
      <DrawContent
        side="right"
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        <DrawHeader>
          <DrawTitle>{t('modelsDraw.title')}</DrawTitle>
        </DrawHeader>
        <DrawBody className="flex flex-col gap-3 overflow-hidden">
          <div className="flex shrink-0 gap-2">
            <div className="relative min-w-0 flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-foreground/40" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={t('modelsDraw.search')}
                className="pl-9"
              />
            </div>
            <Select value={filterModel || '__all__'} onValueChange={(v) => setFilterModel(v === '__all__' ? '' : v)}>
              <SelectTrigger variant="filter" filterLabel={t('modelsDraw.filterAll')} active={Boolean(filterModel)}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent widthMode="content">
                <SelectItem value="__all__">{t('modelsDraw.filterAll')}</SelectItem>
                {uniqueModelKeys.map((key) => (
                  <SelectItem key={key} value={key}>{key}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {rows.length === 0 ? (
              <div className="py-10 text-center text-muted-soft">
                {t('modelsDraw.noModels')}
              </div>
            ) : (
              rows.map((row, i) => {
                const isSiteDisabled = isAllowListModel && (!row.siteGlobalEnabled || !row.siteKeyEnabled)
                return (
                  <button
                    key={`${row.siteId}:${row.modelKey}:${i}`}
                    type="button"
                    className="flex w-full items-center gap-2.5 border-t border-[hsl(var(--glass-divider))] px-1 py-2 text-left first:border-t-0 hover:bg-[hsl(var(--surface-raised))]"
                    onClick={() => copyToClipboard(row.modelKey, t('table.copySuccess'), t('table.copyFailed'))}
                  >
                    <BrandMark
                      iconPath={row.modelIconPath}
                      label={row.provider}
                      fallback={row.provider}
                      fallbackText={buildModelGlyph(row.provider)}
                      size="xs"
                    />
                    <span className="min-w-0 flex-1 truncate text-foreground">
                      {row.modelKey}
                    </span>
                    {isAllowListModel && row.siteName && (
                      <SiteBadge
                        siteName={row.siteName}
                        siteType={row.siteType}
                        siteIconPath={row.siteIconPath}
                        priority={row.sitePriority}
                        disabled={isSiteDisabled}
                        t={t}
                      />
                    )}
                  </button>
                )
              })
            )}
          </div>
        </DrawBody>
      </DrawContent>
    </Draw>
  )
}

function SiteBadge({
  siteName,
  siteType,
  siteIconPath,
  priority,
  disabled,
  t,
}: {
  siteName: string
  siteType: string
  siteIconPath?: string
  priority: number
  disabled: boolean
  t: (key: string) => string
}) {
  return (
    <span className="flex shrink-0 items-center gap-1">
      {disabled && (
        <Badge variant="warning">{t('modelsDraw.siteDisabled')}</Badge>
      )}
      <span className="inline-flex items-center gap-1 rounded bg-[hsl(var(--surface-sunken))] px-1.5 py-0.5">
        <BrandMark
          iconPath={siteIconPath}
          label={siteName}
          fallback={siteType || siteName}
          fallbackText={buildModelGlyph(siteType || siteName)}
          transparent
          size="xs"
        />
        <span className="leading-tight text-muted-soft">
          {siteName}
        </span>
        <span className="leading-tight tabular-nums text-muted-soft opacity-60">
          P{priority}
        </span>
      </span>
    </span>
  )
}
