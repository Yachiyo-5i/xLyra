import type { GatewayModel, ReasoningEffort } from '@/features/playground/lib/types'

const BASE_REASONING_EFFORTS: ReasoningEffort[] = ['low', 'medium', 'high', 'xhigh']
type ReasoningModel = Pick<GatewayModel, 'id' | 'mappedModel'>

function normalizedReasoningModel(model: ReasoningModel | null | undefined): string {
  return (model?.mappedModel || model?.id || '').trim().toLowerCase()
}

export function supportsMaxReasoning(model: ReasoningModel | null | undefined): boolean {
  const normalized = normalizedReasoningModel(model)
  return normalized === 'gpt-5.6' || normalized.startsWith('gpt-5.6-')
}

export function supportsUltraReasoning(model: ReasoningModel | null | undefined): boolean {
  const normalized = normalizedReasoningModel(model)
  return normalized === 'gpt-5.6-sol' || normalized === 'gpt-5.6-terra'
}

export function reasoningEffortsForModel(model: ReasoningModel | null | undefined): ReasoningEffort[] {
  const efforts = [...BASE_REASONING_EFFORTS]
  if (supportsMaxReasoning(model)) efforts.push('max')
  if (supportsUltraReasoning(model)) efforts.push('ultra')
  return efforts
}

export function normalizeReasoningEffort(model: ReasoningModel | null | undefined, effort: ReasoningEffort): ReasoningEffort {
  if (effort === 'ultra' && !supportsUltraReasoning(model)) {
    return supportsMaxReasoning(model) ? 'max' : 'xhigh'
  }
  return effort === 'max' && !supportsMaxReasoning(model) ? 'xhigh' : effort
}
