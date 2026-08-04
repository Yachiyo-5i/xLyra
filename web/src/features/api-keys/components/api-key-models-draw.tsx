import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Search } from 'lucide-react'
import { BrandMark } from '@/components/common/brand-mark'
import { buildModelGlyph, siteTypeIconPath } from '@/components/common/brand-utils'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { Badge } from '@/components/ui/badge'
import { Draw, DrawBody, DrawContent, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { enabledSiteModels, formatSiteTypeLabel } from '@/features/api-keys/lib/api-key-utils'
import type { CanonicalModelItem } from '@/features/sites/api/sites'
import { getProviderCatalogEntry } from '@/lib/brands'

export function APIKeyModelsDraw({
  apiKey,
  canonicalModels,
  onOpenChange,
}: {
  apiKey: DownstreamAPIKey | null
  canonicalModels: CanonicalModelItem[]
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation(['api-keys', 'components'])
  const [search, setSearch] = useState('')
  const rows = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    const siteModels = enabledSiteModels(apiKey)
    const canonicalByKey = new Map(canonicalModels.map((model) => [model.model_key, model]))
    const source = apiKey?.model_policy === 'allow_list'
      ? siteModels.map((model) => {
          const canonical = canonicalByKey.get(model.canonical_model_key ?? '')
          const provider = canonical?.provider ?? ''
          const providerEntry = provider ? getProviderCatalogEntry(provider) : undefined
          const siteTypeLabel = model.site_type ? formatSiteTypeLabel(model.site_type) : ''
          const iconLabel = providerEntry?.name || siteTypeLabel || provider || model.site_name || '-'
          return {
            id: model.site_model_id ?? model.id ?? `${model.site_id}:${model.upstream_model_name}`,
            modelKey: model.canonical_model_key || model.model_key || model.upstream_model_name || '-',
            displayName: model.display_name || model.upstream_model_name || model.canonical_model_key || '-',
            provider: providerEntry?.name || siteTypeLabel || provider || '-',
            siteName: model.site_name,
            upstreamName: model.upstream_model_name,
            badgeLabel: model.site_name || '-',
            badgeToneKey: model.site_id || model.site_name || '-',
            iconPath: providerEntry?.iconPath ?? (model.site_type ? siteTypeIconPath(model.site_type) : undefined),
            iconLabel,
          }
        })
      : canonicalModels.map((model) => {
          const providerEntry = getProviderCatalogEntry(model.provider)
          const providerLabel = providerEntry?.name || model.provider || '-'
          return {
            id: model.id,
            modelKey: model.model_key,
            displayName: model.display_name,
            provider: providerLabel,
            siteName: '',
            upstreamName: '',
            badgeLabel: providerLabel,
            badgeToneKey: providerLabel,
            iconPath: providerEntry?.iconPath,
            iconLabel: providerLabel,
          }
        })

    const badgeToneByKey = new Map<string, string>()
    for (const model of source) {
      if (!badgeToneByKey.has(model.badgeToneKey)) {
        badgeToneByKey.set(model.badgeToneKey, SITE_BADGE_TONE_CLASS_NAMES[badgeToneByKey.size % SITE_BADGE_TONE_CLASS_NAMES.length])
      }
    }

    const filteredSource = keyword
      ? source.filter((model) => (
          `${model.modelKey} ${model.displayName} ${model.provider} ${model.siteName} ${model.upstreamName}`.toLowerCase().includes(keyword)
        ))
      : source

    return filteredSource.map((model) => ({
      ...model,
      badgeToneClassName: badgeToneByKey.get(model.badgeToneKey) ?? SITE_BADGE_TONE_CLASS_NAMES[0],
    }))
  }, [apiKey, canonicalModels, search])

  return (
    <Draw open={Boolean(apiKey)} onOpenChange={onOpenChange}>
      <DrawContent
        side="right"
        size="wide"
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        <DrawHeader>
          <DrawTitle>{t('table.headers.models')}</DrawTitle>
        </DrawHeader>
        <DrawBody className="space-y-4">
          <div className="relative">
            <Search className="text-foreground/40 pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('modelsDraw.search')}
              className="pl-9"
            />
          </div>
          <div className="overflow-y-auto rounded-lg border border-[hsl(var(--glass-border))]">
            {rows.length ? (
              rows.map((model) => (
                <div
                  key={model.id}
                  className="grid grid-cols-[minmax(0,1fr)_minmax(96px,160px)] gap-4 border-t border-[hsl(var(--glass-divider))] px-4 py-3 first:border-t-0"
                >
                  <div className="flex min-w-0 items-center gap-3">
                    <BrandMark
                      iconPath={model.iconPath}
                      label={model.iconLabel}
                      fallback={model.provider || model.modelKey}
                      fallbackText={buildModelGlyph(model.provider || model.modelKey)}
                      size="sm"
                    />
                    <button
                      type="button"
                      className="min-w-0 max-w-full truncate bg-transparent p-0 text-left text-sm font-medium text-foreground"
                      title={`${model.modelKey}${model.upstreamName ? ` / ${model.upstreamName}` : ''}`}
                      onClick={() => copyToClipboard(model.modelKey, t('components:modelsDraw.copied'), t('components:modelsDraw.copyFailed'))}
                    >
                      {model.modelKey}
                    </button>
                  </div>
                  <div className="flex min-w-0 items-center justify-end">
                    <Badge variant="neutral" className={`max-w-full truncate ${model.badgeToneClassName}`} title={model.badgeLabel || '-'}>
                      {model.badgeLabel || '-'}
                    </Badge>
                  </div>
                </div>
              ))
            ) : (
              <div className="text-muted-soft py-10 text-center text-sm">{t('modelsDraw.noModels')}</div>
            )}
          </div>
        </DrawBody>
      </DrawContent>
    </Draw>
  )
}

const SITE_BADGE_TONE_CLASS_NAMES = [
  'border-sky-600/20 bg-sky-500/10 text-sky-800 [html.dark_&]:text-sky-300',
  'border-emerald-600/20 bg-emerald-500/10 text-emerald-800 [html.dark_&]:text-emerald-300',
  'border-amber-600/20 bg-amber-500/10 text-amber-900 [html.dark_&]:text-amber-300',
  'border-violet-600/20 bg-violet-500/10 text-violet-800 [html.dark_&]:text-violet-300',
  'border-rose-600/20 bg-rose-500/10 text-rose-800 [html.dark_&]:text-rose-300',
  'border-cyan-600/20 bg-cyan-500/10 text-cyan-800 [html.dark_&]:text-cyan-300',
  'border-lime-600/20 bg-lime-500/10 text-lime-900 [html.dark_&]:text-lime-300',
  'border-orange-600/20 bg-orange-500/10 text-orange-800 [html.dark_&]:text-orange-300',
  'border-fuchsia-600/20 bg-fuchsia-500/10 text-fuchsia-800 [html.dark_&]:text-fuchsia-300',
  'border-teal-600/20 bg-teal-500/10 text-teal-800 [html.dark_&]:text-teal-300',
  'border-blue-600/20 bg-blue-500/10 text-blue-800 [html.dark_&]:text-blue-300',
  'border-pink-600/20 bg-pink-500/10 text-pink-800 [html.dark_&]:text-pink-300',
]
