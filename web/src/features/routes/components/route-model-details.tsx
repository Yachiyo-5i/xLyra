import type { ReactNode } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle, XCircle } from 'lucide-react'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import {
  listRouteCandidates,
  listRouteTraces,
  selectRoute,
  type RouteCandidateItem,
  type RouteCooldown,
  type RouteOverviewItem,
  type RouteTrace,
} from '@/features/routes/api/routes'
import { getCanonicalModelMatrix, sitesQueryKeys, type Site, type SiteAPIKey, type SiteModel } from '@/features/sites/api/sites'
import { findCurrentRouteChannel, routeChannelRowsFromMatrix, routeChannelStatus, routeChannelSwitchDisabledReason } from '@/features/routes/lib/route-channels'
import {
  formatCooldownScope,
  formatDateTime,
  formatDuration,
  formatLatency,
  formatModelHealth,
  formatRoutePricing,
  routeCooldownReasonLabel,
  routeCooldownSiteName,
  routeCooldownSourceLabel,
  routeCooldownTargetLabel,
} from '@/features/routes/lib/route-format'
import type { PendingChannelState, RouteChannelRow } from '@/features/routes/lib/types'

const EMPTY_TRACES: RouteTrace[] = []

type SelectionQuery = UseQueryResult<Awaited<ReturnType<typeof selectRoute>>>
type CandidatesQuery = UseQueryResult<Awaited<ReturnType<typeof listRouteCandidates>>>
type TracesQuery = UseQueryResult<Awaited<ReturnType<typeof listRouteTraces>>>

export function RouteModelDetails({
  item,
  selectionQuery,
  candidatesQuery,
  tracesQuery,
  cooldowns,
  sites,
  modelsMap,
  apiKeysMap,
  cooldownsLoading,
  clearingId,
  pendingChannelState,
  onToggleChannel,
  onClearCooldown,
  compact = false,
}: {
  item?: RouteOverviewItem
  selectionQuery: SelectionQuery
  candidatesQuery: CandidatesQuery
  tracesQuery: TracesQuery
  cooldowns: RouteCooldown[]
  sites: Site[]
  modelsMap: Record<string, SiteModel[]>
  apiKeysMap: Record<string, SiteAPIKey[]>
  cooldownsLoading: boolean
  clearingId?: string
  pendingChannelState?: PendingChannelState
  onToggleChannel: (row: RouteChannelRow, enabled: boolean) => void
  onClearCooldown: (item: RouteCooldown) => void
  compact?: boolean
}) {
  const { t } = useTranslation('routes')
  const modelId = item?.canonical_model.id || selectionQuery.data?.canonical_model.id || ''
  const matrixQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'routes-matrix', modelId],
    queryFn: () => getCanonicalModelMatrix(modelId),
    enabled: Boolean(modelId),
  })
  const channelRows = routeChannelRowsFromMatrix(
    matrixQuery.data?.items ?? [],
    sites,
    apiKeysMap,
    selectionQuery.data?.selected,
    candidatesQuery.data?.items ?? [],
    cooldowns,
  )
  const currentChannel = findCurrentRouteChannel(channelRows, selectionQuery.data?.selected)

  return (
    <div className={cn('border-t border-[hsl(var(--glass-divider))] pb-4 pt-3', compact ? 'px-3' : 'px-4')}>
      <Tabs defaultValue="routing" variant="segmented">
        <TabsList className={compact ? 'h-10' : undefined}>
          <TabsTrigger value="routing" className={compact ? 'px-2 text-xs' : undefined}>
            {t('details.tabs.routing')}
          </TabsTrigger>
          <TabsTrigger value="traces" className={compact ? 'px-2 text-xs' : undefined}>
            {t('details.tabs.traces')}
          </TabsTrigger>
          <TabsTrigger value="cooldowns" className={cn('gap-1.5', compact && 'px-2 text-xs')}>
            {t('details.tabs.cooldowns')}
            {cooldowns.length > 0 ? (
              <span className="inline-flex min-w-5 items-center justify-center rounded-full bg-[hsl(var(--warning)/0.15)] px-1.5 py-0.5 text-[11px] font-medium text-[hsl(var(--warning))]">
                {cooldowns.length}
              </span>
            ) : null}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="routing">
          <div className="space-y-5">
            <CurrentRouteSection query={selectionQuery} currentChannel={currentChannel} t={t} />
            <UpstreamCoverageSection
              rows={channelRows}
              loading={matrixQuery.isLoading || candidatesQuery.isLoading}
              error={matrixQuery.isError ? matrixQuery.error.message : undefined}
              pendingChannelState={pendingChannelState}
              onToggleChannel={onToggleChannel}
              t={t}
            />
          </div>
        </TabsContent>
        <TabsContent value="traces">
          <RouteTracesSection query={tracesQuery} t={t} />
        </TabsContent>
        <TabsContent value="cooldowns">
          <CooldownsSection
            items={cooldowns}
            sites={sites}
            modelsMap={modelsMap}
            loading={cooldownsLoading}
            clearingId={clearingId}
            onClear={onClearCooldown}
            t={t}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}

type TFunction = (key: string, options?: Record<string, unknown>) => string

function CurrentRouteSection({ query, currentChannel, t }: { query: SelectionQuery; currentChannel?: RouteChannelRow; t: TFunction }) {
  if (query.isLoading) {
    return (
      <div className="space-y-3">
        <SectionTitle>{t('details.currentRoute.title')}</SectionTitle>
        <Skeleton className="h-16 w-full" />
      </div>
    )
  }

  if (query.isError) {
    return (
      <div className="space-y-3">
        <SectionTitle>{t('details.currentRoute.title')}</SectionTitle>
        <RouteInfoLine
          badges={<Badge variant="warning">{t('details.currentRoute.noAvailableRoute')}</Badge>}
          primary={t('details.currentRoute.fallbackHint')}
        />
      </div>
    )
  }

  if (!query.data) {
    return null
  }

  return (
    <div className="space-y-3">
      <SectionTitle>{t('details.currentRoute.title')}</SectionTitle>
      {currentChannel ? <RouteChannelLine row={currentChannel} current t={t} /> : <CandidateRouteLine candidate={query.data.selected} current t={t} />}
    </div>
  )
}

function UpstreamCoverageSection({
  rows,
  loading,
  error,
  pendingChannelState,
  onToggleChannel,
  t,
}: {
  rows: RouteChannelRow[]
  loading: boolean
  error?: string
  pendingChannelState?: PendingChannelState
  onToggleChannel: (row: RouteChannelRow, enabled: boolean) => void
  t: TFunction
}) {
  return (
    <div className="space-y-3">
      <SectionTitle>{t('details.coverage.title')}</SectionTitle>
      {loading ? (
        <div className="space-y-3">
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
        </div>
      ) : error ? (
        <SoftEmpty text={t('details.coverage.loadFailed')} />
      ) : rows.length ? (
        <div className="divide-y divide-[hsl(var(--glass-divider))]">
          {rows.map((row) => {
            return (
              <RouteChannelLine
                key={row.id}
                row={row}
                pending={pendingChannelState?.id === row.id ? pendingChannelState : undefined}
                onToggle={(enabled) => onToggleChannel(row, enabled)}
                t={t}
              />
            )
          })}
        </div>
      ) : (
        <SoftEmpty text={t('details.coverage.noConfig')} />
      )}
    </div>
  )
}

function RouteChannelLine({
  row,
  pending,
  current,
  onToggle,
  t,
}: {
  row: RouteChannelRow
  pending?: PendingChannelState
  current?: boolean
  onToggle?: (enabled: boolean) => void
  t?: TFunction
}) {
  const status = routeChannelStatus(row, t)
  const checked = pending ? pending.enabled : row.enabled
  const disabledReason = routeChannelSwitchDisabledReason(row, pending, t)

  return (
    <RouteInfoLine
      badges={
        <>
          <Badge variant={current ? 'accent' : status.badgeVariant}>{current ? (t ? t('details.currentRoute.current') : '当前使用') : status.label}</Badge>
          {row.apiKeyName ? (
            <Badge variant="neutral" title={row.groupName ? `${row.apiKeyName} · ${row.groupName}` : row.apiKeyName}>
              {row.apiKeyName}
            </Badge>
          ) : null}
          {row.apiKeyRoutingPriority != null ? <Badge variant="neutral">P{row.apiKeyRoutingPriority}</Badge> : null}
          {row.apiKeyUpstreamCostMultiplier != null ? <Badge variant="neutral">{row.apiKeyUpstreamCostMultiplier}x</Badge> : null}
        </>
      }
      primary={`${row.siteName} → ${row.upstreamName}`}
      meta={[
        row.groupName ? (t ? t('details.coverage.group', { name: row.groupName }) : `分组 ${row.groupName}`) : '',
        formatModelHealth(row.candidate, t),
        formatRoutePricing(row.pricing, t),
      ].filter(Boolean)}
      action={
        onToggle ? (
          <span className={cn('inline-flex', disabledReason && 'cursor-not-allowed')} title={disabledReason}>
            <Switch
              checked={checked}
              disabled={Boolean(disabledReason)}
              className={cn(disabledReason && 'cursor-not-allowed opacity-50')}
              aria-label={
                t
                  ? t('details.coverage.switchLabel', {
                      site: row.siteName,
                      apiKey: row.apiKeyName ?? '',
                      model: row.upstreamName,
                    })
                  : `切换 ${row.siteName} ${row.apiKeyName ?? ''} ${row.upstreamName}`
              }
              onCheckedChange={onToggle}
            />
          </span>
        ) : undefined
      }
    />
  )
}

function CandidateRouteLine({ candidate, current, t }: { candidate: RouteCandidateItem; current?: boolean; t?: TFunction }) {
  return (
    <RouteInfoLine
      badges={
        <>
          <Badge variant={current ? 'accent' : 'neutral'}>
            {current
              ? t
                ? t('details.currentRoute.current')
                : '当前使用'
              : t
                ? t('details.currentRoute.routable', { rank: candidate.rank })
                : `可路由 #${candidate.rank}`}
          </Badge>
          {candidate.pricing.group_name ? <Badge variant="neutral">{candidate.pricing.group_name}</Badge> : null}
          {candidate.credential.name ? <Badge variant="neutral">{candidate.credential.name}</Badge> : null}
        </>
      }
      primary={`${candidate.site.name} → ${candidate.model.upstream_model_name}`}
      meta={[
        `Key ${candidate.availability.available_api_key_count}/${candidate.availability.total_api_key_count}`,
        candidate.site.supports_api_key_cost_multiplier
          ? `P${candidate.credential.routing_priority} · ${candidate.credential.upstream_cost_multiplier}x`
          : `P${candidate.credential.routing_priority}`,
        formatModelHealth(candidate, t),
        formatRoutePricing(candidate.pricing, t),
      ]}
    />
  )
}

function RouteInfoLine({ badges, primary, meta, action }: { badges?: ReactNode; primary: string; meta?: string[]; action?: ReactNode }) {
  return (
    <div className="py-2.5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            {badges}
            <span className="min-w-0 break-all text-sm font-medium text-foreground">{primary}</span>
          </div>
          {meta?.length ? (
            <div className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-sm text-muted-soft">
              {meta.map((value) => (
                <span key={value}>{value}</span>
              ))}
            </div>
          ) : null}
        </div>
        {action ? <div className="shrink-0 pt-0.5">{action}</div> : null}
      </div>
    </div>
  )
}

function RouteTracesSection({ query, t }: { query: TracesQuery; t: TFunction }) {
  const items = (query.data?.items ?? EMPTY_TRACES).slice(0, 5)

  return (
    <div className="space-y-3">
      <SectionTitle>{t('details.traces.title')}</SectionTitle>
      {query.isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-12 w-full" />
          <Skeleton className="h-12 w-full" />
        </div>
      ) : query.isError ? (
        <SoftEmpty text={t('details.traces.loadFailed')} />
      ) : items.length ? (
        <div className="divide-y divide-[hsl(var(--glass-divider))]">
          {items.map((trace) => {
            const attempt = trace.attempts.find((item) => item.success) ?? trace.attempts.at(-1)
            return (
              <RouteInfoLine
                key={trace.parent_request_id}
                badges={
                  <StatusBadge status={trace.success ? 'healthy' : 'error'}>
                    {trace.success ? t('details.traces.success') : t('details.traces.failure')}
                  </StatusBadge>
                }
                primary={attempt?.site.name ?? '-'}
                meta={[
                  formatDateTime(trace.started_at),
                  formatLatency(trace.total_latency_ms),
                  attempt?.credential.name ? `${t('format.upstreamCredential')} ${attempt.credential.name}` : '',
                  trace.failover_count > 0
                    ? t('details.traces.failover', {
                        count: trace.failover_count,
                      })
                    : '',
                ].filter(Boolean)}
              />
            )
          })}
        </div>
      ) : (
        <SoftEmpty text={t('details.traces.noTraces')} />
      )}
    </div>
  )
}

function CooldownsSection({
  items,
  sites,
  modelsMap,
  loading,
  clearingId,
  onClear,
  t,
}: {
  items: RouteCooldown[]
  sites: Site[]
  modelsMap: Record<string, SiteModel[]>
  loading: boolean
  clearingId?: string
  onClear: (item: RouteCooldown) => void
  t: TFunction
}) {
  return (
    <div className="space-y-3">
      <SectionTitle>{t('details.cooldowns.title')}</SectionTitle>
      {loading ? (
        <Skeleton className="h-20 w-full" />
      ) : items.length ? (
        <div className="max-h-[300px] divide-y divide-[hsl(var(--glass-divider))] overflow-y-auto pr-1">
          {items.map((item) => {
            const clearing = clearingId === item.site_id
            const siteName = routeCooldownSiteName(item, sites, t)
            const target = routeCooldownTargetLabel(item, modelsMap, t)
            const source = routeCooldownSourceLabel(item, t)
            const reason = routeCooldownReasonLabel(item, t)

            return (
              <div key={item.id} className="flex items-start justify-between gap-3 py-2.5">
                <div className="min-w-0 flex-1">
                  <RouteInfoLine
                    badges={
                      <>
                        <Badge variant="warning">{formatCooldownScope(item.scope, t)}</Badge>
                        <Badge variant="neutral">{source}</Badge>
                      </>
                    }
                    primary={`${siteName} · ${target}`}
                    meta={[
                      reason,
                      t('details.cooldowns.remaining', {
                        duration: formatDuration(item.remaining_seconds),
                      }),
                    ].filter(Boolean)}
                  />
                </div>
                <Button size="sm" variant="outline" onClick={() => onClear(item)} disabled={clearing}>
                  {clearing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <XCircle className="h-4 w-4" />}
                  {t('details.cooldowns.clear')}
                </Button>
              </div>
            )
          })}
        </div>
      ) : (
        <SoftEmpty text={t('details.cooldowns.noCooldowns')} />
      )}
    </div>
  )
}

function SectionTitle({ children }: { children: string }) {
  return <div className="text-xs font-medium tracking-[0.14em] text-[hsl(var(--text-muted-soft))]">{children}</div>
}

function SoftEmpty({ text }: { text: string }) {
  return <div className="text-muted-soft rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] px-3 py-3 text-sm">{text}</div>
}
