import type { SiteAPIKeyModel } from '@/features/sites/api/sites'

type TFunction = (key: string, options?: Record<string, unknown>) => string

export function formatEndpointTypeLabel(value: string): string {
  switch (value) {
    case 'openai':
      return 'completions'
    case 'openai-response':
      return 'responses'
    case 'openai-image':
      return 'images'
    case 'openai-embedding':
      return 'embeddings'
    case 'openai-audio-speech':
      return 'audio/speech'
    case 'openai-response-compact':
      return 'responses-compact'
    case 'anthropic':
    case 'anthropic-messages':
      return 'messages'
    case 'google-gemini':
      return 'gemini'
    default:
      return value
  }
}

export function upstreamEndpointTypes(model: SiteAPIKeyModel, fallback: string[] = []): string[] {
  const normalize = (values: string[]) => [...new Set(values.map(normalizeEndpointType).filter(Boolean))]
  if (Array.isArray(model.effective_endpoint_types)) return normalize(model.effective_endpoint_types)
  if (model.endpoint_override?.mode === 'disabled') return []
  const declared = model.supported_endpoint_types ?? fallback
  const selected = model.endpoint_override?.mode === 'allowlist'
    ? model.endpoint_override.endpoint_types ?? []
    : declared
  const fallbackTypes = normalize(fallback)
  return normalize(selected).filter((value) => {
    if (fallbackTypes.length === 0 || fallbackTypes.includes(value)) return true
    const family = endpointTypeFamily(value)
    return family !== '' && fallbackTypes.some((item) => endpointTypeFamily(item) === family)
  })
}

function endpointTypeFamily(value: string): string {
  switch (normalizeEndpointType(value)) {
    case 'openai':
    case 'openai-response':
    case 'anthropic-messages':
    case 'google-gemini':
      return 'text'
    case 'openai-image':
      return 'image'
    case 'openai-embedding':
      return 'embedding'
    case 'openai-audio-speech':
      return 'audio-speech'
    default:
      return ''
  }
}

function normalizeEndpointType(value: string): string {
  switch (value.trim().toLowerCase()) {
    case 'chat':
    case 'completions':
    case 'openai-chat':
    case 'openai-completions':
      return 'openai'
    case 'responses':
    case 'openai-responses':
      return 'openai-response'
    case 'messages':
    case 'anthropic-message':
      return 'anthropic-messages'
    case 'gemini':
      return 'google-gemini'
    default:
      return value.trim().toLowerCase()
  }
}

export function formatMatchSource(source: string, t?: TFunction): string {
  if (source === 'manual') return t ? t('matchSource.manual') : '手动'
  if (source === 'auto') return t ? t('matchSource.auto') : '自动'
  if (source === 'unmatched') return t ? t('matchSource.unmatched') : '未命中'
  return source || '-'
}

export function isNewAPISite(siteType: string): boolean {
  return siteType === 'newapi'
}
