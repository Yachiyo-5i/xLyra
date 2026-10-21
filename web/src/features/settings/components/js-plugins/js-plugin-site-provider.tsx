import { useTranslation } from 'react-i18next'

export type SiteProviderDetail = {
  version: string
  siteType: string
  displayName: string
  hosts: string[]
  grantedHosts: string[]
}

export function JSPluginSiteProviderSection({ items }: { items: SiteProviderDetail[] }) {
  const { t } = useTranslation('settings')
  if (items.length === 0) return null
  return (
    <section className="space-y-4 border-t border-separator pt-4">
      <h4 className="text-headline text-label">{t('jsPlugins.siteProvider.title')}</h4>
      {items.map((item) => (
        <div key={item.version} className="space-y-2">
          {items.length > 1 ? <div className="font-mono text-footnote text-label-2">{item.version}</div> : null}
          <dl className="space-y-2 text-body">
            <div>
              <dt className="text-footnote text-label-2">{t('jsPlugins.siteProvider.siteType')}</dt>
              <dd className="font-mono text-label">{item.siteType || '—'}</dd>
            </div>
            <div>
              <dt className="text-footnote text-label-2">{t('jsPlugins.siteProvider.displayName')}</dt>
              <dd className="text-label">{item.displayName || '—'}</dd>
            </div>
            <div>
              <dt className="text-footnote text-label-2">{t('jsPlugins.siteProvider.hosts')}</dt>
              <dd>
                {item.hosts.length > 0 ? (
                  <ul className="space-y-0.5 font-mono text-label">
                    {item.hosts.map((host) => (
                      <li key={host} className="break-all">{host}</li>
                    ))}
                  </ul>
                ) : (
                  '—'
                )}
              </dd>
            </div>
            {item.grantedHosts.length > 0 ? (
              <div>
                <dt className="text-footnote text-label-2">{t('jsPlugins.siteProvider.grantedHosts')}</dt>
                <dd>
                  <ul className="space-y-0.5 font-mono text-label">
                    {item.grantedHosts.map((permission) => (
                      <li key={permission} className="break-all">{permission}</li>
                    ))}
                  </ul>
                </dd>
              </div>
            ) : null}
          </dl>
        </div>
      ))}
    </section>
  )
}
