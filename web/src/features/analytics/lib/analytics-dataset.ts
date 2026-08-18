import type {
  AnalyticsBreakdownItem,
  AnalyticsDataset,
  AnalyticsMatrixCell,
  AnalyticsSeries,
  AnalyticsSeriesPoint,
  AnalyticsUsage,
  AnalyticsUsageFact,
} from '@/features/analytics/api/analytics'
import type { AnalyticsFiltersState } from '@/features/analytics/components/analytics-filter-bar'
import type { AnalyticsTrendDimension } from '@/features/analytics/lib/analytics-utils'

type Metrics = {
  requests: number
  successCount: number
  failureCount: number
  promptTokens: number
  completionTokens: number
  cachedTokens: number
  totalTokens: number
  cost: number
  latencyCount: number
  latencyTotalMS: number
  latencyMaxMS: number
  upstreamLatencyCount: number
  upstreamLatencyTotalMS: number
}

type DimensionRef = {
  key: string
  id?: string | null
  label: string
}

type Aggregate = {
  ref: DimensionRef
  metrics: Metrics
}

function emptyMetrics(): Metrics {
  return {
    requests: 0,
    successCount: 0,
    failureCount: 0,
    promptTokens: 0,
    completionTokens: 0,
    cachedTokens: 0,
    totalTokens: 0,
    cost: 0,
    latencyCount: 0,
    latencyTotalMS: 0,
    latencyMaxMS: 0,
    upstreamLatencyCount: 0,
    upstreamLatencyTotalMS: 0,
  }
}

function addFact(metrics: Metrics, fact: AnalyticsUsageFact) {
  metrics.requests += fact.requests
  metrics.successCount += fact.success_count
  metrics.failureCount += fact.failure_count
  metrics.promptTokens += fact.prompt_tokens
  metrics.completionTokens += fact.completion_tokens
  metrics.cachedTokens += fact.cached_tokens
  metrics.totalTokens += fact.total_tokens
  metrics.cost += fact.cost
  metrics.latencyCount += fact.latency_count
  metrics.latencyTotalMS += fact.latency_total_ms
  metrics.latencyMaxMS = Math.max(metrics.latencyMaxMS, fact.latency_max_ms)
  metrics.upstreamLatencyCount += fact.upstream_latency_count
  metrics.upstreamLatencyTotalMS += fact.upstream_latency_total_ms
}

function addMetrics(target: Metrics, source: Metrics) {
  target.requests += source.requests
  target.successCount += source.successCount
  target.failureCount += source.failureCount
  target.promptTokens += source.promptTokens
  target.completionTokens += source.completionTokens
  target.cachedTokens += source.cachedTokens
  target.totalTokens += source.totalTokens
  target.cost += source.cost
  target.latencyCount += source.latencyCount
  target.latencyTotalMS += source.latencyTotalMS
  target.latencyMaxMS = Math.max(target.latencyMaxMS, source.latencyMaxMS)
  target.upstreamLatencyCount += source.upstreamLatencyCount
  target.upstreamLatencyTotalMS += source.upstreamLatencyTotalMS
}

function aggregateFacts(facts: AnalyticsUsageFact[]) {
  const metrics = emptyMetrics()
  for (const fact of facts) addFact(metrics, fact)
  return metrics
}

function successRate(metrics: Metrics) {
  return metrics.requests > 0 ? metrics.successCount / metrics.requests : null
}

function cacheHitRate(metrics: Metrics) {
  return metrics.promptTokens > 0 ? metrics.cachedTokens / metrics.promptTokens : null
}

function averageLatency(metrics: Metrics) {
  return metrics.latencyCount > 0 ? metrics.latencyTotalMS / metrics.latencyCount : 0
}

function averageUpstreamLatency(metrics: Metrics) {
  return metrics.upstreamLatencyCount > 0
    ? metrics.upstreamLatencyTotalMS / metrics.upstreamLatencyCount
    : 0
}

function matchesDimensions(fact: AnalyticsUsageFact, filters: AnalyticsFiltersState) {
  if (filters.siteIds.length > 0 && (!fact.site_id || !filters.siteIds.includes(fact.site_id))) return false
  if (filters.modelKeys.length > 0 && !filters.modelKeys.includes(fact.model_key)) return false
  if (filters.apiKeyIds.length > 0 && (!fact.api_key_id || !filters.apiKeyIds.includes(fact.api_key_id))) return false
  return true
}

function costByCurrency(facts: AnalyticsUsageFact[]) {
  const result: Record<string, number> = {}
  for (const fact of facts) result[fact.currency] = (result[fact.currency] ?? 0) + fact.cost
  return result
}

function displayCurrency(requested: string, costs: Record<string, number>) {
  const currencies = Object.keys(costs).sort((a, b) => a.localeCompare(b))
  if (requested && currencies.includes(requested)) return requested
  return currencies.reduce((selected, currency) => {
    if (!selected || costs[currency] > costs[selected]) return currency
    return selected
  }, '') || 'USD'
}

function dimensionRef(fact: AnalyticsUsageFact, dimension: 'site' | 'model' | 'api_key'): DimensionRef {
  if (dimension === 'site') return { key: fact.site_key, id: fact.site_id, label: fact.site_label }
  if (dimension === 'api_key') return { key: fact.api_key_key, id: fact.api_key_id, label: fact.api_key_label }
  return { key: fact.model_key, id: fact.model_id, label: fact.model_label }
}

function orderedAggregates(aggregates: Map<string, Aggregate>) {
  return [...aggregates.values()].sort((a, b) => (
    b.metrics.cost - a.metrics.cost
    || b.metrics.requests - a.metrics.requests
    || a.ref.label.localeCompare(b.ref.label)
  ))
}

function buildBreakdown(facts: AnalyticsUsageFact[], dimension: 'site' | 'model' | 'api_key') {
  const aggregates = new Map<string, Aggregate>()
  for (const fact of facts) {
    const ref = dimensionRef(fact, dimension)
    let aggregate = aggregates.get(ref.key)
    if (!aggregate) {
      aggregate = { ref, metrics: emptyMetrics() }
      aggregates.set(ref.key, aggregate)
    }
    addFact(aggregate.metrics, fact)
  }
  return orderedAggregates(aggregates).slice(0, 20).map(({ ref, metrics }): AnalyticsBreakdownItem => ({
    key: ref.key,
    id: ref.id,
    label: ref.label,
    requests: metrics.requests,
    success_count: metrics.successCount,
    failure_count: metrics.failureCount,
    success_rate: successRate(metrics),
    prompt_tokens: metrics.promptTokens,
    completion_tokens: metrics.completionTokens,
    cached_tokens: metrics.cachedTokens,
    total_tokens: metrics.totalTokens,
    cost: metrics.cost,
    avg_latency_ms: averageLatency(metrics),
    max_latency_ms: metrics.latencyMaxMS,
  }))
}

function topKeys(facts: AnalyticsUsageFact[], dimension: 'site' | 'model') {
  const costs = new Map<string, { cost: number; label: string }>()
  for (const fact of facts) {
    const ref = dimensionRef(fact, dimension)
    const current = costs.get(ref.key)
    costs.set(ref.key, { cost: (current?.cost ?? 0) + fact.cost, label: current?.label ?? ref.label })
  }
  return new Set([...costs.entries()]
    .sort((a, b) => b[1].cost - a[1].cost || a[1].label.localeCompare(b[1].label))
    .slice(0, 10)
    .map(([key]) => key))
}

function buildMatrix(facts: AnalyticsUsageFact[]) {
  const sites = topKeys(facts, 'site')
  const models = topKeys(facts, 'model')
  const cells = new Map<string, AnalyticsMatrixCell>()
  for (const fact of facts) {
    if (!sites.has(fact.site_key) || !models.has(fact.model_key)) continue
    const key = `${fact.site_key}\u0000${fact.model_key}`
    let cell = cells.get(key)
    if (!cell) {
      cell = {
        site: dimensionRef(fact, 'site'),
        model: dimensionRef(fact, 'model'),
        requests: 0,
        total_tokens: 0,
        cost: 0,
      }
      cells.set(key, cell)
    }
    cell.requests += fact.requests
    cell.total_tokens += fact.total_tokens
    cell.cost += fact.cost
  }
  return [...cells.values()].sort((a, b) => (
    b.cost - a.cost
    || b.requests - a.requests
    || a.site.label.localeCompare(b.site.label)
    || a.model.label.localeCompare(b.model.label)
  ))
}

function seriesPoint(date: string, metrics: Metrics): AnalyticsSeriesPoint {
  return {
    date,
    requests: metrics.requests,
    success_count: metrics.successCount,
    failure_count: metrics.failureCount,
    prompt_tokens: metrics.promptTokens,
    completion_tokens: metrics.completionTokens,
    cached_tokens: metrics.cachedTokens,
    total_tokens: metrics.totalTokens,
    cost: metrics.cost,
    avg_latency_ms: averageLatency(metrics),
    max_latency_ms: metrics.latencyMaxMS,
  }
}

function buildSeries(facts: AnalyticsUsageFact[], dimension: AnalyticsTrendDimension) {
  const members = new Map<string, Aggregate & { points: Map<string, Metrics> }>()
  for (const fact of facts) {
    const ref = dimensionRef(fact, dimension)
    let member = members.get(ref.key)
    if (!member) {
      member = { ref, metrics: emptyMetrics(), points: new Map() }
      members.set(ref.key, member)
    }
    addFact(member.metrics, fact)
    let point = member.points.get(fact.date)
    if (!point) {
      point = emptyMetrics()
      member.points.set(fact.date, point)
    }
    addFact(point, fact)
  }
  const ordered = [...members.values()].sort((a, b) => (
    b.metrics.cost - a.metrics.cost
    || b.metrics.requests - a.metrics.requests
    || a.ref.label.localeCompare(b.ref.label)
  ))
  const series: AnalyticsSeries[] = ordered.slice(0, 9).map((member) => ({
    key: member.ref.key,
    id: member.ref.id,
    label: member.ref.label,
    points: [...member.points.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, metrics]) => seriesPoint(date, metrics)),
  }))
  if (ordered.length > 9) {
    const points = new Map<string, Metrics>()
    for (const member of ordered.slice(9)) {
      for (const [date, metrics] of member.points) {
        let point = points.get(date)
        if (!point) {
          point = emptyMetrics()
          points.set(date, point)
        }
        addMetrics(point, metrics)
      }
    }
    series.push({
      key: 'other',
      label: '其他',
      points: [...points.entries()]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([date, metrics]) => seriesPoint(date, metrics)),
    })
  }
  return series
}

export function buildAnalyticsUsage(
  dataset: AnalyticsDataset,
  filters: AnalyticsFiltersState,
  trendDimension: AnalyticsTrendDimension,
): AnalyticsUsage {
  const currentByDimension = dataset.current.filter((fact) => matchesDimensions(fact, filters))
  const previousByDimension = dataset.previous.filter((fact) => matchesDimensions(fact, filters))
  const costs = costByCurrency(currentByDimension)
  const currencies = Object.keys(costs).sort((a, b) => a.localeCompare(b))
  const currency = displayCurrency(filters.currency, costs)
  const current = currentByDimension.filter((fact) => fact.currency === currency)
  const previous = previousByDimension.filter((fact) => fact.currency === currency)
  const currentMetrics = aggregateFacts(current)
  const previousMetrics = aggregateFacts(previous)

  return {
    meta: {
      from: dataset.meta.from,
      to: dataset.meta.to,
      days: dataset.meta.days,
      timezone: dataset.meta.timezone,
      generated_at: dataset.meta.generated_at,
      group_by: trendDimension,
      currency,
      available_currencies: currencies,
      data_from: dataset.meta.data_from,
      granularity: dataset.meta.granularity,
      filters: {
        site_ids: filters.siteIds,
        model_keys: filters.modelKeys,
        api_key_ids: filters.apiKeyIds,
        success: true,
      },
    },
    totals: {
      requests: currentMetrics.requests,
      success_count: currentMetrics.successCount,
      failure_count: currentMetrics.failureCount,
      success_rate: successRate(currentMetrics),
      prompt_tokens: currentMetrics.promptTokens,
      completion_tokens: currentMetrics.completionTokens,
      cached_tokens: currentMetrics.cachedTokens,
      total_tokens: currentMetrics.totalTokens,
      cache_hit_rate: cacheHitRate(currentMetrics),
      cost: currentMetrics.cost,
      cost_by_currency: costs,
      avg_latency_ms: averageLatency(currentMetrics),
      max_latency_ms: currentMetrics.latencyMaxMS,
      avg_upstream_latency_ms: averageUpstreamLatency(currentMetrics),
      previous_period: {
        from: dataset.meta.previous_from,
        to: dataset.meta.previous_to,
        requests: previousMetrics.requests,
        success_rate: successRate(previousMetrics),
        total_tokens: previousMetrics.totalTokens,
        cost: previousMetrics.cost,
        avg_latency_ms: averageLatency(previousMetrics),
      },
    },
    breakdowns: {
      site: buildBreakdown(current, 'site'),
      model: buildBreakdown(current, 'model'),
      api_key: buildBreakdown(current, 'api_key'),
      matrix: buildMatrix(current),
    },
    series: buildSeries(current, trendDimension),
    api_key_contributions: [],
  }
}

export function filterAnalyticsContributionPoints(
  points: AnalyticsUsage['api_key_contributions'],
  apiKeyIds: string[],
) {
  if (apiKeyIds.length === 0) return points
  const selected = new Set(apiKeyIds)
  return points.filter((point) => selected.has(point.api_key_id))
}

export function analyticsModelKeys(
  facts: AnalyticsUsageFact[],
  filters?: Pick<AnalyticsFiltersState, 'siteIds' | 'apiKeyIds' | 'currency'>,
) {
  const keys = new Set<string>()
  for (const fact of facts) {
    if (filters?.siteIds.length && (!fact.site_id || !filters.siteIds.includes(fact.site_id))) continue
    if (filters?.apiKeyIds.length && (!fact.api_key_id || !filters.apiKeyIds.includes(fact.api_key_id))) continue
    if (filters?.currency && fact.currency !== filters.currency) continue
    if (fact.model_key && fact.model_key !== 'other' && fact.model_key !== 'unknown') {
      keys.add(fact.model_key)
    }
  }
  return [...keys].sort((a, b) => a.localeCompare(b))
}
