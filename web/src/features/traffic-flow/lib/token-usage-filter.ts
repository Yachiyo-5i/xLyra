import type { TrafficFlowUsageCell } from '@/features/traffic-flow/api/traffic-flow'
import { flowVendorBrand, flowVendorColor } from '@/features/traffic-flow/lib/model-visual'

export type TokenUsageFilter = {
  keyIds: string[]
  siteIds: string[]
  vendors: string[]
  modelKeys: string[]
}

export type TokenUsageParts = {
  total_tokens: number
  input_tokens: number
  output_tokens: number
  cached_tokens: number
}

export type TokenUsageRank = TokenUsageParts & {
  id: string
  name: string
  share: number
  color?: string
  vendor?: string
  keyCount: number
  siteCount: number
  modelCount: number
}

export type TokenUsageBreakdown = TokenUsageParts & {
  keys: TokenUsageRank[]
  sites: TokenUsageRank[]
  models: TokenUsageRank[]
  vendors: TokenUsageRank[]
}

export function emptyTokenUsageFilter(): TokenUsageFilter {
  return { keyIds: [], siteIds: [], vendors: [], modelKeys: [] }
}

export function emptyTokenUsageParts(): TokenUsageParts {
  return { total_tokens: 0, input_tokens: 0, output_tokens: 0, cached_tokens: 0 }
}

export function usageCellKey(cell: Pick<TrafficFlowUsageCell, 'api_key_id' | 'site_id' | 'model_key'>) {
  return `${cell.api_key_id}\u001f${cell.site_id}\u001f${cell.model_key}`
}

export function isTokenUsageFilterActive(filter: TokenUsageFilter) {
  return filter.keyIds.length > 0 || filter.siteIds.length > 0 || filter.vendors.length > 0 || filter.modelKeys.length > 0
}

export function toggleTokenUsageFilter(filter: TokenUsageFilter, field: keyof TokenUsageFilter, value: string): TokenUsageFilter {
  const current = filter[field]
  const next = current.includes(value) ? current.filter((item) => item !== value) : [...current, value]
  return { ...filter, [field]: next }
}

export function cellVendor(cell: Pick<TrafficFlowUsageCell, 'model_provider' | 'model_key'>) {
  return flowVendorBrand(cell.model_provider, cell.model_key)
}

export function cellParts(cell: Pick<TrafficFlowUsageCell, 'total_tokens' | 'input_tokens' | 'output_tokens' | 'cached_tokens'>): TokenUsageParts {
  return {
    total_tokens: nonNegative(cell.total_tokens),
    input_tokens: nonNegative(cell.input_tokens),
    output_tokens: nonNegative(cell.output_tokens),
    cached_tokens: nonNegative(cell.cached_tokens),
  }
}

export function addTokenParts(left: TokenUsageParts, right: TokenUsageParts): TokenUsageParts {
  return {
    total_tokens: left.total_tokens + right.total_tokens,
    input_tokens: left.input_tokens + right.input_tokens,
    output_tokens: left.output_tokens + right.output_tokens,
    cached_tokens: left.cached_tokens + right.cached_tokens,
  }
}

export function deriveTokenUsage(cells: Iterable<TrafficFlowUsageCell>, filter: TokenUsageFilter = emptyTokenUsageFilter()): TokenUsageBreakdown {
  const list = Array.from(cells)
  const filtered = list.filter((cell) => matchesTokenUsageFilter(cell, filter))
  return {
    ...sumParts(filtered),
    keys: rankTokens(list.filter((cell) => matchesTokenUsageFilter(cell, { ...filter, keyIds: [] })), (cell) => cell.api_key_id, (cell) => cell.api_key_name || cell.api_key_id),
    sites: rankTokens(list.filter((cell) => matchesTokenUsageFilter(cell, { ...filter, siteIds: [] })), (cell) => cell.site_id, (cell) => cell.site_name || cell.site_id),
    models: rankTokens(list.filter((cell) => matchesTokenUsageFilter(cell, { ...filter, modelKeys: [] })), (cell) => cell.model_key, (cell) => cell.model_key, (cell) => flowVendorColor(cellVendor(cell))),
    vendors: rankTokens(list.filter((cell) => matchesTokenUsageFilter(cell, { ...filter, vendors: [] })), cellVendor, cellVendor, (cell) => flowVendorColor(cellVendor(cell))),
  }
}

export function matchesTokenUsageFilter(cell: TrafficFlowUsageCell, filter: TokenUsageFilter) {
  if (filter.keyIds.length > 0 && !filter.keyIds.includes(cell.api_key_id)) return false
  if (filter.siteIds.length > 0 && !filter.siteIds.includes(cell.site_id)) return false
  if (filter.modelKeys.length > 0 && !filter.modelKeys.includes(cell.model_key)) return false
  if (filter.vendors.length > 0 && !filter.vendors.includes(cellVendor(cell))) return false
  return true
}

function rankTokens(
  cells: TrafficFlowUsageCell[],
  idOf: (cell: TrafficFlowUsageCell) => string,
  nameOf: (cell: TrafficFlowUsageCell) => string,
  colorOf?: (cell: TrafficFlowUsageCell) => string,
) {
  const grouped = new Map<string, { rank: TokenUsageRank; keys: Set<string>; sites: Set<string>; models: Set<string> }>()
  for (const cell of cells) {
    const id = idOf(cell)
    const existing = grouped.get(id)
    if (existing) {
      existing.rank = { ...existing.rank, ...addTokenParts(existing.rank, cellParts(cell)) }
      if (cell.api_key_id) existing.keys.add(cell.api_key_id)
      if (cell.site_id) existing.sites.add(cell.site_id)
      if (cell.model_key) existing.models.add(cell.model_key)
      continue
    }
    grouped.set(id, {
      rank: {
        id,
        name: nameOf(cell),
        ...cellParts(cell),
        share: 0,
        color: colorOf?.(cell),
        vendor: cellVendor(cell),
        keyCount: 0,
        siteCount: 0,
        modelCount: 0,
      },
      keys: new Set(cell.api_key_id ? [cell.api_key_id] : []),
      sites: new Set(cell.site_id ? [cell.site_id] : []),
      models: new Set(cell.model_key ? [cell.model_key] : []),
    })
  }
  const columnTotal = sumParts(cells).total_tokens
  return Array.from(grouped.values())
    .map(({ rank, keys, sites, models }) => ({
      ...rank,
      share: columnTotal > 0 ? rank.total_tokens / columnTotal : 0,
      keyCount: keys.size,
      siteCount: sites.size,
      modelCount: models.size,
    }))
    .sort((left, right) => right.total_tokens - left.total_tokens || left.name.localeCompare(right.name) || left.id.localeCompare(right.id))
}

function sumParts(cells: TrafficFlowUsageCell[]) {
  return cells.reduce<TokenUsageParts>((sum, cell) => addTokenParts(sum, cellParts(cell)), emptyTokenUsageParts())
}

function nonNegative(value: number | undefined) {
  return typeof value === 'number' && Number.isFinite(value) ? Math.max(0, Math.floor(value)) : 0
}
