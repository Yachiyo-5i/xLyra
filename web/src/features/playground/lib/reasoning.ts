import type { ReasoningEffort } from '@/features/playground/lib/types'

const BASE_REASONING_EFFORTS: ReasoningEffort[] = ['low', 'medium', 'high', 'xhigh']
const MAX_REASONING_MODELS = new Set([
  'gpt-5.6-sol',
  'gpt-5.6-terra',
  'gpt-5.6-luna',
])

export function supportsMaxReasoning(model: string | null): boolean {
  return model !== null && MAX_REASONING_MODELS.has(model)
}

export function reasoningEffortsForModel(model: string | null): ReasoningEffort[] {
  return supportsMaxReasoning(model) ? [...BASE_REASONING_EFFORTS, 'max'] : [...BASE_REASONING_EFFORTS]
}

export function normalizeReasoningEffort(model: string | null, effort: ReasoningEffort): ReasoningEffort {
  return effort === 'max' && !supportsMaxReasoning(model) ? 'xhigh' : effort
}
