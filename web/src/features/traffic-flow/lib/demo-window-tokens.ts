import type { TrafficFlowTopology, TrafficFlowUsageCell, TrafficFlowUsageTotal } from '@/features/traffic-flow/api/traffic-flow'
import { usageCellKey } from '@/features/traffic-flow/lib/token-usage-filter'

type DemoRecipe = {
  key: number
  site: number
  model: string
  provider: string
  input: number
  output: number
  cached: number
}

const recipes: DemoRecipe[] = [
  { key: 0, site: 0, model: 'gpt-5.4', provider: 'openai', input: 82_400, output: 4_180, cached: 38_400 },
  { key: 0, site: 1, model: 'claude-sonnet-4.6', provider: 'anthropic', input: 21_600, output: 8_640, cached: 4_200 },
  { key: 1, site: 0, model: 'gpt-5.4', provider: 'openai', input: 12_500, output: 1_820, cached: 2_100 },
  { key: 1, site: 2, model: 'gemini-2.5-pro', provider: 'google', input: 6_400, output: 2_240, cached: 800 },
  { key: 2, site: 3, model: 'MiniMax-M2.5', provider: 'minimax', input: 9_800, output: 3_120, cached: 0 },
  { key: 3, site: 4, model: 'deepseek-chat', provider: 'deepseek', input: 15_200, output: 5_460, cached: 1_900 },
  { key: 0, site: 5, model: 'grok-4', provider: 'xai', input: 4_300, output: 1_260, cached: 0 },
  { key: 4, site: 0, model: 'qwen3-max', provider: 'qwen', input: 7_600, output: 2_180, cached: 1_100 },
]

export type DemoWindowTokens = {
  total: number
  cells: Record<string, TrafficFlowUsageCell>
  downstream: Record<string, TrafficFlowUsageTotal>
  upstream: Record<string, TrafficFlowUsageTotal>
}

export function seedDemoWindowTokens(topology: TrafficFlowTopology): DemoWindowTokens | null {
  const keys = topology.downstream
  const sites = topology.upstream
  if (keys.length === 0 || sites.length === 0) return null

  const cells: Record<string, TrafficFlowUsageCell> = {}
  const downstream: Record<string, TrafficFlowUsageTotal> = {}
  const upstream: Record<string, TrafficFlowUsageTotal> = {}
  let total = 0

  for (const recipe of recipes) {
    const key = keys[recipe.key % keys.length]
    const site = sites[recipe.site % sites.length]
    const totalTokens = recipe.input + recipe.output
    const cell: TrafficFlowUsageCell = {
      api_key_id: key.id,
      api_key_name: key.name,
      site_id: site.id,
      site_name: site.name,
      model_key: recipe.model,
      model_provider: recipe.provider,
      total_tokens: totalTokens,
      input_tokens: recipe.input,
      output_tokens: recipe.output,
      cached_tokens: recipe.cached,
    }
    const id = usageCellKey(cell)
    const existing = cells[id]
    if (existing) {
      existing.total_tokens += cell.total_tokens
      existing.input_tokens = (existing.input_tokens ?? 0) + (cell.input_tokens ?? 0)
      existing.output_tokens = (existing.output_tokens ?? 0) + (cell.output_tokens ?? 0)
      existing.cached_tokens = (existing.cached_tokens ?? 0) + (cell.cached_tokens ?? 0)
    } else {
      cells[id] = cell
    }
    total += totalTokens
    addTotal(downstream, key.id, key.name, totalTokens)
    addTotal(upstream, site.id, site.name, totalTokens)
  }

  return { total, cells, downstream, upstream }
}

function addTotal(target: Record<string, TrafficFlowUsageTotal>, id: string, name: string, tokens: number) {
  const existing = target[id]
  target[id] = { id, name, total_tokens: (existing?.total_tokens ?? 0) + tokens }
}
