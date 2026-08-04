import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ListChecks, Search } from 'lucide-react'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { BrandMark } from '@/components/common/brand-mark'
import { resolveThemedIconPath } from '@/components/common/brand-utils'
import { useResolvedTheme } from '@/hooks/theme-context'
import { useMobileLayout } from '@/hooks/use-media-query'
import {
  buildBrandItems,
  buildCodexAccountStatus,
  buildMarketplaceModels,
  buildSiteItems,
  listCanonicalModels,
  listOAuthConnections,
  listAllSitePricings,
  listSiteAPIKeys,
  listSiteModels,
  listSitesWithOAuth,
  modelSearchText,
  oauthQueryKeys,
  sitesQueryKeys,
  type CanonicalModelItem,
  type FilterItem,
  type OAuthConnectionListItem,
  type Site,
  type SiteAPIKey,
  type SiteModel,
} from '../api/models'
import { ModelCard } from './model-card'
import { ModelCatalogSurface } from './model-catalog-surface'
import { MobileModelsList } from './mobile-models-list'

const EMPTY_SITES: Site[] = []
const EMPTY_API_KEYS_MAP: Record<string, SiteAPIKey[]> = {}
const EMPTY_OAUTH_CONNECTIONS: OAuthConnectionListItem[] = []
const EMPTY_CANONICAL_MODELS: CanonicalModelItem[] = []

export function ModelsWorkspace() {
  const { t } = useTranslation('models')
  const isMobile = useMobileLayout()
  const [selectedBrand, setSelectedBrand] = useState<string>('all')
  const [selectedSite, setSelectedSite] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [catalogOpen, setCatalogOpen] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const sitesQuery = useQuery({
    queryKey: sitesQueryKeys.listWithOAuth(),
    queryFn: listSitesWithOAuth,
  })

  const sites = sitesQuery.data?.items ?? EMPTY_SITES

  const pricingRowsQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'all-pricings'],
    queryFn: listAllSitePricings,
  })
  const modelsMapQuery = useQuery({
    queryKey: [
      ...sitesQueryKeys.all,
      'models-marketplace',
      sites.map((site) => site.id).join(','),
    ],
    queryFn: async () => {
      const entries = await Promise.all(
        sites.map(async (site) => {
          const result = await listSiteModels(site.id)
          return [site.id, result.items] as const
        }),
      )
      return Object.fromEntries(entries) as Record<string, SiteModel[]>
    },
    enabled: sites.length > 0,
  })
  const apiKeysMapQuery = useQuery({
    queryKey: [
      ...sitesQueryKeys.all,
      'models-marketplace-api-keys',
      sites.map((site) => site.id).join(','),
    ],
    queryFn: async () => {
      const targetSites = sites.filter(
        (site) =>
          site.site_type === 'newapi' ||
          site.supports_multiple_api_keys === true,
      )
      const entries = await Promise.all(
        targetSites.map(async (site) => {
          const result = await listSiteAPIKeys(site.id)
          return [site.id, result.items] as const
        }),
      )
      return Object.fromEntries(entries) as Record<string, SiteAPIKey[]>
    },
    enabled: sites.length > 0,
  })
  const oauthConnectionsQuery = useQuery({
    queryKey: oauthQueryKeys.connections(),
    queryFn: listOAuthConnections,
  })
  const canonicalModelsQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'canonical-models'],
    queryFn: listCanonicalModels,
  })

  const marketplaceModels = useMemo(
    () =>
      buildMarketplaceModels(
        sites,
        modelsMapQuery.data ?? {},
        pricingRowsQuery.data?.items ?? [],
        canonicalModelsQuery.data?.items ?? EMPTY_CANONICAL_MODELS,
        apiKeysMapQuery.data ?? EMPTY_API_KEYS_MAP,
        buildCodexAccountStatus(
          oauthConnectionsQuery.data?.items ?? EMPTY_OAUTH_CONNECTIONS,
        ),
      ),
    [
      apiKeysMapQuery.data,
      canonicalModelsQuery.data?.items,
      oauthConnectionsQuery.data?.items,
      pricingRowsQuery.data?.items,
      sites,
      modelsMapQuery.data,
    ],
  )

  const brandItems = useMemo(
    () => buildBrandItems(marketplaceModels, t),
    [marketplaceModels, t],
  )
  const siteItems = useMemo(
    () => buildSiteItems(sites, modelsMapQuery.data ?? {}),
    [sites, modelsMapQuery.data],
  )

  const filteredModels = useMemo(() => {
    const keyword = search.trim().toLowerCase()

    return marketplaceModels.filter((model) => {
      const matchesBrand =
        selectedBrand === 'all' ||
        model.brand === selectedBrand ||
        (selectedBrand === 'other' &&
          !brandItems.some(
            (b) =>
              b.key === model.brand && b.key !== 'all' && b.key !== 'other',
          ))
      const matchesSite =
        !selectedSite ||
        model.supportedSites.some((site) => site.siteId === selectedSite)
      const matchesSearch = !keyword || modelSearchText(model).includes(keyword)

      return matchesBrand && matchesSite && matchesSearch
    })
  }, [marketplaceModels, search, selectedBrand, selectedSite, brandItems])

  const isLoading =
    sitesQuery.isLoading ||
    modelsMapQuery.isLoading ||
    pricingRowsQuery.isLoading ||
    apiKeysMapQuery.isLoading ||
    oauthConnectionsQuery.isLoading ||
    canonicalModelsQuery.isLoading

  const isError =
    sitesQuery.isError ||
    modelsMapQuery.isError ||
    pricingRowsQuery.isError ||
    apiKeysMapQuery.isError ||
    oauthConnectionsQuery.isError ||
    canonicalModelsQuery.isError

  const errorMessage =
    sitesQuery.error?.message ??
    modelsMapQuery.error?.message ??
    pricingRowsQuery.error?.message ??
    apiKeysMapQuery.error?.message ??
    oauthConnectionsQuery.error?.message ??
    canonicalModelsQuery.error?.message ??
    t('page.loadFailed')

  if (isLoading) {
    return <ModelsWorkspaceSkeleton isMobile={isMobile} />
  }

  if (isError) {
    return (
      <ErrorState title={t('page.loadFailed')} description={errorMessage} />
    )
  }

  if (isMobile) {
    return (
      <div className="relative space-y-4 pb-2">
        <PageHeader
          eyebrow={t('page.eyebrow')}
          title={t('page.title')}
          description={t('page.description')}
        />

        <div className="sticky top-0 z-20 -mx-1">
          <div className="rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-2 shadow-[0_18px_38px_rgba(0,0,0,0.10)] backdrop-blur-[32px] backdrop-saturate-150">
            <label className="relative block min-w-0">
              <Search className="text-muted-soft pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('filters.search')}
                className="h-10 pl-10"
              />
            </label>

            <div className="mt-2 flex min-w-0 items-center gap-2 overflow-x-auto pb-0.5">
              <Select value={selectedBrand} onValueChange={setSelectedBrand}>
                <SelectTrigger
                  variant="filter"
                  filterLabel={t('filters.brand')}
                  active={selectedBrand !== 'all'}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent searchable={false} widthMode="content">
                  {brandItems.map((item) => {
                    const label =
                      item.key === 'all' ? t('filters.allBrands') : item.label

                    return (
                      <SelectItem
                        key={item.key}
                        value={item.key}
                        icon={
                          <MobileFilterSelectIcon item={item} label={label} />
                        }
                      >
                        {label}
                      </SelectItem>
                    )
                  })}
                </SelectContent>
              </Select>

              <Select
                value={selectedSite ?? 'all'}
                onValueChange={(value) =>
                  setSelectedSite(value === 'all' ? null : value)
                }
              >
                <SelectTrigger
                  variant="filter"
                  filterLabel={t('filters.site')}
                  active={Boolean(selectedSite)}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent searchable={false} widthMode="content">
                  <SelectItem
                    value="all"
                    icon={
                      <MobileFilterSelectIcon
                        label={t('filters.allSites')}
                        fallbackText="站"
                      />
                    }
                  >
                    {t('filters.allSites')}
                  </SelectItem>
                  {siteItems.map((item) => (
                    <SelectItem
                      key={item.key}
                      value={item.key}
                      icon={<MobileFilterSelectIcon item={item} />}
                    >
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        </div>

        {filteredModels.length ? (
          <MobileModelsList
            items={filteredModels}
            expandedId={expandedId}
            onExpandedIdChange={setExpandedId}
          />
        ) : (
          <EmptyState
            title={t('empty.noModels')}
            description={t('empty.noModelsDesc')}
          />
        )}

        <Button
          className="fixed bottom-[calc(env(safe-area-inset-bottom,0px)+5.5rem)] right-5 z-20 h-12 w-12 rounded-full p-0 shadow-[0_18px_34px_rgba(15,23,42,0.22)]"
          onClick={() => setCatalogOpen(true)}
          aria-label={t('filters.catalog')}
        >
          <ListChecks className="h-5 w-5" />
        </Button>

        <ModelCatalogSurface
          open={catalogOpen}
          onOpenChange={setCatalogOpen}
          sites={sites}
          modelsMap={modelsMapQuery.data ?? {}}
        />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col gap-6">
      <PageHeader
        eyebrow={t('page.eyebrow')}
        title={t('page.title')}
        description={t('page.description')}
      />

      {/* Search + Action bar */}
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="relative w-72 shrink-0">
          <Search className="text-foreground/40 pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 z-10" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('filters.search')}
            className="pl-10"
          />
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Button variant="secondary" onClick={() => setCatalogOpen(true)}>
            <ListChecks className="h-4 w-4" />
            {t('filters.catalog')}
          </Button>
        </div>
      </div>

      {/* Main content: filter sidebar + model cards */}
      <div className="grid min-h-0 flex-1 gap-6 xl:grid-cols-[248px_minmax(0,1fr)]">
        <aside className="min-h-0 space-y-4 overflow-y-auto pr-1">
          <FilterColumn
            title={t('filters.brand')}
            items={brandItems}
            selectedKey={selectedBrand}
            onSelect={setSelectedBrand}
          />
          <FilterColumn
            title={t('filters.site')}
            items={siteItems}
            selectedKey={selectedSite}
            onSelect={(key) =>
              setSelectedSite((current) => (current === key ? null : key))
            }
          />
        </aside>

        <section className="min-h-0 overflow-y-auto pr-1">
          <div className="space-y-4">
            {filteredModels.length ? (
              filteredModels.map((model) => {
                const expanded = expandedId === model.id

                return (
                  <ModelCard
                    key={model.id}
                    model={model}
                    expanded={expanded}
                    onToggle={() =>
                      setExpandedId((current) =>
                        current === model.id ? null : model.id,
                      )
                    }
                  />
                )
              })
            ) : (
              <EmptyState
                title={t('empty.noModels')}
                description={t('empty.noModelsDesc')}
              />
            )}
          </div>
        </section>
      </div>

      <ModelCatalogSurface
        open={catalogOpen}
        onOpenChange={setCatalogOpen}
        sites={sites}
        modelsMap={modelsMapQuery.data ?? {}}
      />
    </div>
  )
}

function MobileFilterSelectIcon({
  item,
  label: labelOverride,
  fallbackText,
}: {
  item?: Pick<FilterItem, 'label' | 'iconPath' | 'fallbackText'>
  label?: string
  fallbackText?: string
}) {
  const resolvedMode = useResolvedTheme()
  const label = labelOverride ?? item?.label ?? ''
  const fallback = Array.from(
    fallbackText ?? item?.fallbackText ?? label.trim() ?? '?',
  )
    .slice(0, 2)
    .join('')

  if (item?.iconPath) {
    return (
      <img
        src={resolveThemedIconPath(item.iconPath, label, resolvedMode)}
        alt=""
        className="h-4 w-4 object-contain"
      />
    )
  }

  return (
    <span className="flex h-4 w-4 items-center justify-center rounded-[4px] bg-[hsl(var(--surface-subtle))] text-[9px] font-semibold leading-none text-muted-soft">
      {fallback || '?'}
    </span>
  )
}

function FilterColumn({
  title,
  items,
  selectedKey,
  onSelect,
}: {
  title: string
  items: {
    key: string
    label: string
    count: number
    iconPath?: string
    fallbackText?: string
  }[]
  selectedKey: string | null
  onSelect: (key: string) => void
}) {
  return (
    <div className="space-y-2">
      <h2 className="px-1 text-sm font-medium text-foreground">{title}</h2>
      <div className="space-y-1">
        {items.map((item) => {
          const active = selectedKey === item.key

          return (
            <button
              key={item.key}
              type="button"
              onClick={() => onSelect(item.key)}
              className={`flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm transition-colors ${
                active
                  ? 'bg-[hsl(var(--surface-subtle))] text-foreground'
                  : 'bg-transparent text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground'
              }`}
            >
              <BrandMark
                iconPath={item.iconPath}
                label={item.label}
                fallback={item.label}
                fallbackText={item.fallbackText}
                size="sm"
              />
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              <span
                className={`inline-flex min-w-5 items-center justify-center rounded-full px-1.5 py-0.5 text-[11px] font-medium ${
                  active
                    ? 'bg-[hsl(var(--surface-subtle))] text-foreground'
                    : 'bg-[hsl(var(--surface-subtle))] text-muted-soft'
                }`}
              >
                {item.count}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function ModelsWorkspaceSkeleton({ isMobile = false }: { isMobile?: boolean }) {
  if (isMobile) {
    return (
      <div className="relative space-y-4 pb-2">
        <div className="space-y-2">
          <Skeleton className="h-4 w-16" />
          <Skeleton className="h-8 w-32" />
          <Skeleton className="h-4 w-64 max-w-full" />
        </div>
        <div className="rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-2">
          <Skeleton className="h-10 w-full" />
          <div className="mt-2 grid grid-cols-2 gap-2">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </div>
        </div>
        <div className="space-y-3">
          <Skeleton className="h-32 w-full" />
          <Skeleton className="h-32 w-full" />
          <Skeleton className="h-32 w-full" />
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col gap-6">
      <div className="space-y-2">
        <Skeleton className="h-4 w-16" />
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-4 w-64" />
      </div>
      <div className="flex gap-3">
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-10 w-28" />
      </div>
      <div className="grid min-h-0 flex-1 gap-6 xl:grid-cols-[248px_minmax(0,1fr)]">
        <div className="min-h-0 space-y-4 overflow-y-auto pr-1">
          <Skeleton className="h-8 w-24" />
          <Skeleton className="h-11 w-full" />
          <Skeleton className="h-11 w-full" />
          <Skeleton className="h-11 w-full" />
          <Skeleton className="mt-6 h-8 w-24" />
          <Skeleton className="h-11 w-full" />
          <Skeleton className="h-11 w-full" />
        </div>
        <div className="min-h-0 overflow-y-auto pr-1">
          <div className="space-y-4">
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-28 w-full" />
            <Skeleton className="h-28 w-full" />
          </div>
        </div>
      </div>
    </div>
  )
}
