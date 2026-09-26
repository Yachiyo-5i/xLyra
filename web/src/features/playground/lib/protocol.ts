import type { ChatProtocol, GatewayModel } from '@/features/playground/lib/types'

const GEMINI_SITE_TYPES = new Set(['antigravity', 'google_gemini'])

export function autoProtocol(model: GatewayModel | undefined): ChatProtocol {
  const types = model?.endpointTypes ?? []
  const siteTypes = model?.siteTypes ?? []
  const hasGeminiEndpoint = types.includes('google-gemini')
  const hasGeminiSite = siteTypes.some((siteType) => GEMINI_SITE_TYPES.has(siteType))
  if (hasGeminiEndpoint && hasGeminiSite) return 'gemini'
  if (hasGeminiEndpoint && !types.includes('openai-response') && !types.some((type) => type.startsWith('anthropic')) && !types.includes('openai')) {
    return 'gemini'
  }
  if (types.includes('openai-response')) return 'responses'
  if (types.some((type) => type.startsWith('anthropic'))) return 'messages'
  return 'chat'
}
