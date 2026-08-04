export const DEFAULT_API_KEY_FORM_DRAFT = {
  name: '',
  apiKey: '',
  routingPriority: '1.0',
  upstreamCostMultiplier: '1.0',
}

export type APIKeyFormDraft = typeof DEFAULT_API_KEY_FORM_DRAFT

export type ParsedAPIKeyForm = {
  name: string
  apiKey?: string
  routingPriority: number
  upstreamCostMultiplier?: number
}

export function parseSiteAPIKeyForm(
  draft: APIKeyFormDraft,
  options: { includeCostMultiplier: boolean; requireAPIKey: boolean },
): ParsedAPIKeyForm | null {
  const routingPriority = Number(draft.routingPriority)
  if (
    !Number.isFinite(routingPriority) ||
    routingPriority < 1 ||
    routingPriority > 5
  )
    return null
  if (Math.abs(routingPriority * 10 - Math.round(routingPriority * 10)) > 1e-9)
    return null

  const apiKey = draft.apiKey.trim()
  if (options.requireAPIKey && !apiKey) return null

  const result: ParsedAPIKeyForm = {
    name: draft.name.trim(),
    routingPriority,
  }
  if (apiKey) result.apiKey = apiKey
  if (!options.includeCostMultiplier) return result

  const upstreamCostMultiplier = Number(draft.upstreamCostMultiplier)
  if (
    !Number.isFinite(upstreamCostMultiplier) ||
    upstreamCostMultiplier < 0.01 ||
    upstreamCostMultiplier > 100
  )
    return null
  if (
    Math.abs(
      upstreamCostMultiplier * 10000 -
        Math.round(upstreamCostMultiplier * 10000),
    ) > 1e-9
  )
    return null
  result.upstreamCostMultiplier = upstreamCostMultiplier
  return result
}
