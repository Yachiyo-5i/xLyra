import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  bindJSPluginSite,
  bindJSPluginSiteQuotaProbe,
  previewJSPluginPricing,
  unbindJSPluginSite,
} from '@/features/settings/api/js-plugins'
import { sitesQueryKeys, type Site } from '@/features/sites/api/sites'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'

const QUOTA_PROBE = 'quota_probe'
const PRICING_PARSE = 'pricing_parse'
const QUOTA_PLUGIN_PREFIX = 'plugin:'

/** The plugin id bound to a site for this kind, or the built-in probe type for quota_probe. */
function boundValue(site: Site, kind: string): string {
  const config = site.gateway_config
  if (kind === QUOTA_PROBE) return config?.quota_probe ?? ''
  return config?.plugins?.[kind] ?? ''
}

function boundPluginId(site: Site, kind: string): string {
  const value = boundValue(site, kind)
  if (kind !== QUOTA_PROBE) return value
  return value.startsWith(QUOTA_PLUGIN_PREFIX) ? value.slice(QUOTA_PLUGIN_PREFIX.length) : ''
}

type Props = {
  pluginId: string
  version: string
  kind: string
  sites: Site[]
  loading: boolean
}

// Binding is optional: an enabled plugin is loaded either way, and these kinds
// only take effect on the sites ticked here. A site can be bound to one plugin
// per kind, so ticking a site that already has another plugin replaces it.
export function JSPluginSiteBinding({ pluginId, version, kind, sites, loading }: Props) {
  const { t } = useTranslation(['settings'])
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<string[] | null>(null)
  const [previews, setPreviews] = useState<Record<string, { items: number; groups: number }>>({})
  const [previewing, setPreviewing] = useState<string | null>(null)
  const [reviewed, setReviewed] = useState(false)

  const bound = sites.filter((site) => boundPluginId(site, kind) === pluginId).map((site) => site.id)
  const selected = new Set(draft ?? bound)
  const toBind = [...selected].filter((id) => !bound.includes(id))
  const toUnbind = bound.filter((id) => !selected.has(id))
  const changed = toBind.length > 0 || toUnbind.length > 0
  const pricing = kind === PRICING_PARSE
  const pricingReady = !pricing || toBind.length === 0 || (reviewed && toBind.every((id) => previews[id]))
  const siteName = (id: string) => sites.find((site) => site.id === id)?.name ?? id

  const toggle = (id: string, checked: boolean) => {
    const next = new Set(selected)
    if (checked) next.add(id)
    else next.delete(id)
    setDraft([...next])
    if (!checked) setReviewed(false)
  }

  const apply = useMutation({
    mutationFn: async () => {
      const failures: string[] = []
      const run = async (id: string, action: () => Promise<unknown>) => {
        try {
          await action()
        } catch (error) {
          failures.push(`${siteName(id)}: ${error instanceof APIError ? error.message : String(error)}`)
        }
      }
      for (const id of toUnbind) await run(id, () => unbindJSPluginSite(pluginId, id, kind))
      for (const id of toBind) {
        await run(id, () =>
          kind === QUOTA_PROBE
            ? bindJSPluginSiteQuotaProbe(pluginId, version, id)
            : bindJSPluginSite(pluginId, version, id, { confirmPricingReviewed: reviewed }),
        )
      }
      if (failures.length > 0) throw new Error(failures.join('\n'))
    },
    onSuccess: () => toast.success(t('settings:jsPlugins.siteBinding.applied')),
    onError: (error: unknown) => toast.error(error instanceof Error ? error.message : t('settings:jsPlugins.siteBinding.failed')),
    onSettled: () => {
      setDraft(null)
      setPreviews({})
      setReviewed(false)
      void queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all })
    },
  })

  const preview = async (id: string) => {
    setPreviewing(id)
    try {
      const result = await previewJSPluginPricing(pluginId, id)
      setPreviews((current) => ({ ...current, [id]: { items: result.items?.length ?? 0, groups: result.groups?.length ?? 0 } }))
    } catch (error) {
      toast.error(error instanceof APIError ? error.message : t('settings:jsPlugins.previewPricingFailed'))
    } finally {
      setPreviewing(null)
    }
  }

  const isQuota = kind === QUOTA_PROBE
  const title = isQuota ? t('settings:jsPlugins.bindQuotaTitle') : t('settings:jsPlugins.bindSiteTitle')
  const hint = isQuota ? t('settings:jsPlugins.bindQuotaHint') : t(`settings:jsPlugins.bindSiteHint.${kind}`)

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
      ) : sites.length === 0 ? (
        <p className="text-sm text-muted-soft">{t('settings:jsPlugins.siteBinding.noSites')}</p>
      ) : (
        <ul className="max-h-60 divide-y divide-[hsl(var(--glass-divider))] overflow-y-auto">
          {sites.map((site) => {
            const other = boundPluginId(site, kind) !== pluginId ? boundValue(site, kind) : ''
            const newlyTicked = selected.has(site.id) && !bound.includes(site.id)
            const summary = previews[site.id]
            return (
              <li key={site.id} className="py-2">
                <div className="flex items-center justify-between gap-3">
                  <Checkbox
                    checked={selected.has(site.id)}
                    onCheckedChange={(checked) => toggle(site.id, checked)}
                    label={site.name}
                    description={other ? `${site.slug} · ${t('settings:jsPlugins.siteBinding.other', { id: other })}` : site.slug}
                  />
                  {pricing && newlyTicked ? (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="shrink-0"
                      disabled={previewing !== null || apply.isPending}
                      onClick={() => void preview(site.id)}
                    >
                      {previewing === site.id ? <LoaderCircle className="h-4 w-4 animate-spin" /> : t('settings:jsPlugins.previewPricing')}
                    </Button>
                  ) : null}
                </div>
                {pricing && newlyTicked && summary ? (
                  <p className="mt-1 pl-7 text-xs text-muted-soft">{t('settings:jsPlugins.pricingPreviewSummary', summary)}</p>
                ) : null}
              </li>
            )
          })}
        </ul>
      )}

      {pricing && toBind.length > 0 ? (
        <div className="border-t border-[hsl(var(--glass-divider))] pt-3">
          <Checkbox
            checked={reviewed}
            onCheckedChange={setReviewed}
            disabled={!toBind.every((id) => previews[id])}
            label={t('settings:jsPlugins.pricingReviewConfirm')}
          />
        </div>
      ) : null}

      <div className="flex items-center justify-between gap-3 border-t border-[hsl(var(--glass-divider))] pt-3">
        <span className="text-xs text-muted-soft">{t('settings:jsPlugins.siteBinding.selected', { count: selected.size })}</span>
        <Button type="button" size="sm" disabled={!changed || !pricingReady || apply.isPending} onClick={() => apply.mutate()}>
          {apply.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : t('settings:jsPlugins.siteBinding.apply')}
        </Button>
      </div>
    </section>
  )
}
