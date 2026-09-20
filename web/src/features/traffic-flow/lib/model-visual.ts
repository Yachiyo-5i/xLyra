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

export function modelVisual(provider: string, modelKey: string): ModelVisual {
  const brand = flowVendorBrand(provider, modelKey)
  const color = flowVendorColor(brand)
  const icon = modelNameIconInfo([provider, modelKey], modelKey)
  return {
    color,
    brand,
    iconPath: icon.iconPath,
    fallback: icon.fallbackText ?? icon.fallback.slice(0, 2).toUpperCase(),
    label: icon.label,
  }
}
