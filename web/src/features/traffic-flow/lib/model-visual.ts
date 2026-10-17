import { modelNameIconInfo } from '@/features/sites/lib/model-icon'
import { OTHER_BRAND_LABEL, inferFallbackBrand } from '@/lib/brands'

export type ModelVisual = {
  color: string
  brand: string
  iconPath?: string
  fallback: string
  label: string
}

/** Lifted cockpit hues. Identity is vendor, not site or API key. */
export const VENDOR_FLOW_COLORS: Record<string, string> = {
  OpenAI: '#62e7bd',
  Anthropic: '#f4b86a',
  Google: '#7bb7ff',
  DeepSeek: '#5de0df',
  xAI: '#f08aa5',
  Qwen: '#c9a7ff',
  Moonshot: '#f38bca',
  MiniMax: '#ff9d4a',
}

export const FALLBACK_FLOW_COLOR = '#a5b8c8'

export function flowVendorBrand(provider: string, modelKey: string): string {
  const fromModel = inferFallbackBrand([modelKey])
  if (fromModel !== OTHER_BRAND_LABEL) return fromModel
  return inferFallbackBrand([provider])
}

export function flowVendorColor(brand: string): string {
  return VENDOR_FLOW_COLORS[brand] ?? FALLBACK_FLOW_COLOR
}

const visualCache = new Map<string, ModelVisual>()
const VISUAL_CACHE_LIMIT = 256

/** 品牌推断要做一堆字符串匹配，而拓扑页每次渲染每个节点都会问一遍，所以按（厂商, 模型）缓存 */
export function modelVisual(provider: string, modelKey: string): ModelVisual {
  const cacheKey = `${provider}\u0000${modelKey}`
  const cached = visualCache.get(cacheKey)
  if (cached) return cached
  const brand = flowVendorBrand(provider, modelKey)
  const color = flowVendorColor(brand)
  const icon = modelNameIconInfo([provider, modelKey], modelKey)
  const visual: ModelVisual = {
    color,
    brand,
    iconPath: icon.iconPath,
    fallback: icon.fallbackText ?? icon.fallback.slice(0, 2).toUpperCase(),
    label: icon.label,
  }
  if (visualCache.size >= VISUAL_CACHE_LIMIT) visualCache.clear()
  visualCache.set(cacheKey, visual)
  return visual
}
