import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { grantedHostPermissions, siteProviderOf } from '@/features/settings/api/js-plugins'
import { pickCurrentVersionRow, type CurrentVersionRow } from '@/features/settings/components/js-plugins/site-provider-version'
import { resolveText } from '@/features/settings/lib/localized-text'

type SiteProviderVersionSource = CurrentVersionRow & {
  manifest?: Record<string, unknown>
  granted_permissions?: string[]
}

export function JSPluginSiteProviderSection({ versions }: { versions: readonly SiteProviderVersionSource[] }) {
  const { t, i18n } = useTranslation('settings')
  const row = pickCurrentVersionRow(versions)
  const info = row ? siteProviderOf(row.manifest) : null
  if (!row || !info) return null
  const grantedHosts = grantedHostPermissions(row.granted_permissions)
  const versionTag = row.status === 'enabled' ? 'versionEnabled' : 'versionLatest'
  return (
    <section className="space-y-4 border-t border-separator pt-4">
      <h4 className="text-headline text-label">{t('jsPlugins.siteProvider.title')}</h4>
      <div className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-footnote text-label-2">{row.version}</span>
          <Badge variant={versionTag === 'versionEnabled' ? 'success' : 'neutral'}>
            {t(`jsPlugins.siteProvider.${versionTag}`)}
          </Badge>
        </div>
        <dl className="space-y-2 text-body">
          <div>
            <dt className="text-footnote text-label-2">{t('jsPlugins.siteProvider.siteType')}</dt>
            <dd className="font-mono text-label">{info.siteType || '—'}</dd>
          </div>
          <div>
            <dt className="text-footnote text-label-2">{t('jsPlugins.siteProvider.displayName')}</dt>
            <dd className="text-label">{resolveText(info.displayName, i18n.language) || '—'}</dd>
          </div>
          <div>
            <dt className="text-footnote text-label-2">{t('jsPlugins.siteProvider.hosts')}</dt>
            <dd>
              {info.hosts.length > 0 ? (
                <ul className="space-y-0.5 font-mono text-label">
                  {info.hosts.map((host) => (
                    <li key={host} className="break-all">{host}</li>
                  ))}
                </ul>
              ) : (
                '—'
              )}
            </dd>
          </div>
          {grantedHosts.length > 0 ? (
            <div>
              <dt className="text-footnote text-label-2">{t('jsPlugins.siteProvider.grantedHosts')}</dt>
              <dd>
                <ul className="space-y-0.5 font-mono text-label">
                  {grantedHosts.map((permission) => (
                    <li key={permission} className="break-all">{permission}</li>
                  ))}
                </ul>
              </dd>
            </div>
          ) : null}
        </dl>
      </div>
    </section>
  )
}
