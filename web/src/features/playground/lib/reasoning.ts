import type { GatewayModel, ReasoningEffort } from '@/features/playground/lib/types'

const BASE_REASONING_EFFORTS: ReasoningEffort[] = ['light', 'medium', 'high', 'xhigh']
type ReasoningModel = Pick<GatewayModel, 'id' | 'mappedModel'>

export function supportsExtendedReasoning(model: ReasoningModel | null | undefined): boolean {
  const normalized = (model?.mappedModel || model?.id || '').trim().toLowerCase()
  return normalized === 'gpt-5.6' || normalized.startsWith('gpt-5.6-')
}

export function reasoningEffortsForModel(model: ReasoningModel | null | undefined): ReasoningEffort[] {
  return supportsExtendedReasoning(model)
    ? [...BASE_REASONING_EFFORTS, 'max', 'ultra']
    : [...BASE_REASONING_EFFORTS]
}

export function normalizeReasoningEffort(model: ReasoningModel | null | undefined, effort: ReasoningEffort): ReasoningEffort {
  return (effort === 'max' || effort === 'ultra') && !supportsExtendedReasoning(model) ? 'xhigh' : effort
}
