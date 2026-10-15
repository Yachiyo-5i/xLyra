import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { QUOTA_PROBE, type SiteBinding } from '@/features/settings/components/js-plugins/use-site-binding'
import type { Site } from '@/features/sites/api/sites'
import { siteTypeIcon } from '@/features/sites/lib/site-utils'
import { useResolvedTheme } from '@/hooks/theme-context'

// The site type's brand icon in the variant that suits the current theme; some icons are
// light-on-dark only, so the plain icon_url from the API can vanish on a light background.
function themedSiteIcon(site: Site, mode: 'light' | 'dark') {
  return siteTypeIcon(site.site_type, undefined, mode) ?? site.icon_url
}

function SiteName({ site, mode }: { site: Site; mode: 'light' | 'dark' }) {
  const icon = themedSiteIcon(site, mode)
  return (
    <span className="flex min-w-0 items-center gap-2">
      {icon ? <img src={icon} alt="" className="h-4 w-4 shrink-0 rounded-sm object-contain" /> : null}
      <span className="truncate text-sm text-foreground">{site.name}</span>
    </span>
  )
}

export function JSPluginSiteBindingSection({
  binding,
  sites,
  loading,
}: {
  binding: SiteBinding
  sites: Site[]
  loading: boolean
}) {
  const { t } = useTranslation(['settings'])
  const resolvedMode = useResolvedTheme()
  const isQuota = binding.kind === QUOTA_PROBE
  const title = isQuota ? t('settings:jsPlugins.bindQuotaTitle') : t('settings:jsPlugins.bindSiteTitle')
  const hint = isQuota ? t('settings:jsPlugins.bindQuotaHint') : t(`settings:jsPlugins.bindSiteHint.${binding.kind}`)

  // Only the name and the site type's icon identify a site here, plus whether it is enabled.
  const options: MultiSelectOption[] = sites.map((site) => ({
    value: site.id,
    label: site.name,
    icon: themedSiteIcon(site, resolvedMode),
    description: binding.otherBinding[site.id]
      ? t('settings:jsPlugins.siteBinding.other', { id: binding.otherBinding[site.id] })
      : undefined,
    badge: (
      <StatusBadge status={site.enabled ? 'success' : 'disabled'}>
        {site.enabled ? t('settings:jsPlugins.siteBinding.enabled') : t('settings:jsPlugins.siteBinding.disabled')}
      </StatusBadge>
    ),
  }))
  const newSites = sites.filter((site) => binding.toBind.includes(site.id))

  return (
    <section className="space-y-3 border-t border-[hsl(var(--glass-divider))] pt-4">
      <div className="space-y-1">
        <h4 className="text-sm font-semibold text-foreground">{title}</h4>
        <p className="text-xs text-muted-soft">{hint}</p>
        <p className="text-xs text-muted-soft">{t('settings:jsPlugins.siteBinding.optional')}</p>
      </div>

      {loading ? (
        <div className="flex items-center gap-2 py-3 text-sm text-muted-soft">
          <LoaderCircle className="h-4 w-4 animate-spin" />
          {t('settings:jsPlugins.loading')}
        </div>
      ) : (
        <MultiSelect
          value={binding.selected}
          options={options}
          placeholder={t('settings:jsPlugins.siteBinding.placeholder')}
          searchPlaceholder={t('settings:jsPlugins.siteBinding.search')}
          emptyText={t('settings:jsPlugins.siteBinding.noSites')}
          selectedText={t('settings:jsPlugins.siteBinding.selectedUnit')}
          maxVisibleTags={3}
          disabled={binding.applying}
          onChange={binding.setSelected}
        />
      )}

      {binding.pricing && newSites.length > 0 ? (
        <div className="divide-y divide-[hsl(var(--glass-divider))] border-t border-[hsl(var(--glass-divider))]">
          {newSites.map((site) => {
            const summary = binding.previews[site.id]
            return (
              <div key={site.id} className="py-2">
                <div className="flex items-center justify-between gap-3">
                  <SiteName site={site} mode={resolvedMode} />
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    className="shrink-0"
                    disabled={binding.previewing !== null || binding.applying}
                    onClick={() => void binding.preview(site.id)}
                  >
                    {binding.previewing === site.id ? (
                      <LoaderCircle className="h-4 w-4 animate-spin" />
                    ) : (
                      t('settings:jsPlugins.previewPricing')
                    )}
                  </Button>
                </div>
                {summary ? (
                  <p className="mt-1 text-xs text-muted-soft">{t('settings:jsPlugins.pricingPreviewSummary', summary)}</p>
                ) : null}
              </div>
            )
          })}
          <div className="pt-3">
            <Checkbox
              checked={binding.reviewed}
              onCheckedChange={binding.setReviewed}
              disabled={!binding.toBind.every((id) => binding.previews[id])}
              label={t('settings:jsPlugins.pricingReviewConfirm')}
            />
          </div>
        </div>
      ) : null}
    </section>
  )
}
