import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import type { SiteUptimeItem, UptimeStatus } from './dashboard-types'

type SiteUptimeStripProps = {
  sites: SiteUptimeItem[]
  className?: string
}

const uptimeStatusClasses: Record<UptimeStatus, string> = {
  healthy: 'bg-success',
  degraded: 'bg-warning',
  down: 'bg-danger',
  idle: 'bg-label-4',
}

export function SiteUptimeStrip({ sites, className }: SiteUptimeStripProps) {
  const { t } = useTranslation('dashboard')
  const groupedSites = groupUptimeSites(sites)
  const [activeGroup, setActiveGroup] = useState<UptimeSiteGroup>('sites')
  const activeSites = groupedSites[activeGroup]

  const legendItems: Array<{ label: string; status: UptimeStatus }> = [
    { label: t('uptime.legend.healthy'), status: 'healthy' },
    { label: t('uptime.legend.degraded'), status: 'degraded' },
    { label: t('uptime.legend.down'), status: 'down' },
    { label: t('uptime.legend.idle'), status: 'idle' },
  ]

  return (
    <Card className={cn('flex min-h-0 flex-col p-5', className)}>
      <div className="mb-4 shrink-0 space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          <h3 className="text-headline text-label">{t('uptime.title')}</h3>
          <Tabs variant="slash" value={activeGroup} onValueChange={(value) => setActiveGroup(value as UptimeSiteGroup)}>
            <TabsList>
              <TabsTrigger value="sites" className="text-body">
                Sites
              </TabsTrigger>
              <TabsTrigger value="oauth" className="text-body">
                OAuth
              </TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          <p className="text-body text-label-2">{t('uptime.recent24h')}</p>
          <div className="flex flex-wrap gap-2">
            {legendItems.map((item) => (
              <Badge key={item.status} variant="neutral" className="gap-2 whitespace-nowrap">
                <span className={cn('size-2 rounded-sm', uptimeStatusClasses[item.status])} />
                {item.label}
              </Badge>
            ))}
          </div>
        </div>
      </div>
      <div className="scrollbar-hidden min-h-0 flex-1 space-y-3 overflow-auto pr-1">
        {activeSites.length ? (
          activeSites.map((site) => (
            <div
              key={site.id}
              className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-2 sm:grid-cols-[150px_minmax(0,1fr)_56px] sm:gap-3"
            >
              <div className="min-w-0 sm:col-start-1 sm:row-start-1">
                <p className="break-words text-body font-medium text-label" title={site.name}>{site.name}</p>
              </div>
              <div
                className="col-span-2 row-start-2 grid gap-0.5 sm:col-span-1 sm:col-start-2 sm:row-start-1"
                style={{ gridTemplateColumns: `repeat(${site.buckets.length}, minmax(0, 1fr))` }}
              >
                {site.buckets.map((bucket, index) => (
                  <span
                    key={`${site.id}-${bucket.time}-${index}`}
                    title={`${bucket.time} ${bucket.status}`}
                    className={cn('h-4 rounded-[4px]', uptimeStatusClasses[bucket.status])}
                  />
                ))}
              </div>
              <p className="col-start-2 row-start-1 text-right text-body font-medium tabular-nums text-label sm:col-start-3">
                {site.uptime}
              </p>
            </div>
          ))
        ) : (
          <p className="rounded-lg bg-fill-4 px-4 py-5 text-center text-body text-label-2">
            {t('uptime.empty', { type: activeGroup === 'oauth' ? 'OAuth' : 'Sites' })}
          </p>
        )}
      </div>
    </Card>
  )
}

type UptimeSiteGroup = 'sites' | 'oauth'

function groupUptimeSites(sites: SiteUptimeItem[]): Record<UptimeSiteGroup, SiteUptimeItem[]> {
  const groups: Record<UptimeSiteGroup, SiteUptimeItem[]> = {
    sites: [],
    oauth: [],
  }

  for (const site of sites) {
    if (isOAuthSite(site.siteType)) {
      groups.oauth.push(site)
    } else {
      groups.sites.push(site)
    }
  }

  return groups
}

function isOAuthSite(siteType: string) {
  return siteType === 'codex' || siteType === 'antigravity'
}
