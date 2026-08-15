type ProviderId =
  | 'openai'
  | 'google'
  | 'deepseek'
  | 'xai'
  | 'moonshotai-cn'
  | 'anthropic'
  | 'qwen'
  | 'zhipuai'
  | 'minimax'
  | 'nvidia'
  | 'xiaomi'
  | 'kimi_code'
  | 'bytedance'
  | 'vidu'
  | 'stepfun'
  | 'alibaba'
  | 'kuaishou'
  | 'baai'
  | 'flux'
  | 'hunyuan'
  | 'sensenova'

export type ProviderCatalogEntry = {
  id: ProviderId
  name: string
  iconPath: string
}

export const providerCatalog: ProviderCatalogEntry[] = [
  { id: 'openai', name: 'OpenAI', iconPath: '/brand-icons/openai.png' },
  { id: 'anthropic', name: 'Anthropic', iconPath: '/brand-icons/anthropic.png' },
  { id: 'google', name: 'Google', iconPath: '/brand-icons/google.png' },
  { id: 'xiaomi', name: 'Xiaomi MiMo', iconPath: '/brand-icons/xiaomi.png' },
  { id: 'moonshotai-cn', name: 'Moonshot', iconPath: '/brand-icons/moonshot.png' },
  { id: 'minimax', name: 'MiniMax', iconPath: '/brand-icons/minimax.png' },
  { id: 'deepseek', name: 'DeepSeek', iconPath: '/brand-icons/deepseek.png' },
  { id: 'qwen', name: 'Qwen', iconPath: '/brand-icons/qwen.png' },
  { id: 'zhipuai', name: 'ZhipuAI', iconPath: '/brand-icons/zhipu.png' },
  { id: 'xai', name: 'xAI', iconPath: '/brand-icons/xai.png' },
  { id: 'nvidia', name: 'NVIDIA', iconPath: '/brand-icons/nvidia.png' },
  { id: 'kimi_code', name: 'Kimi Code', iconPath: '/brand-icons/moonshot.png' },
  { id: 'bytedance', name: 'ByteDance', iconPath: '/brand-icons/bytedance-dark.png' },
  { id: 'vidu', name: 'Vidu', iconPath: '/brand-icons/vidu-dark.png' },
  { id: 'stepfun', name: 'StepFun', iconPath: '/brand-icons/stepfun-dark.png' },
  { id: 'alibaba', name: 'Alibaba', iconPath: '/brand-icons/alibaba-dark.png' },
  { id: 'kuaishou', name: 'Kuaishou', iconPath: '/brand-icons/kuaishou.svg' },
  { id: 'baai', name: 'BAAI', iconPath: '/brand-icons/baai-dark.png' },
  { id: 'flux', name: 'FLUX', iconPath: '/brand-icons/flux-dark.png' },
  { id: 'hunyuan', name: 'Hunyuan', iconPath: '/brand-icons/hunyuan-dark.png' },
  { id: 'sensenova', name: 'SenseNova', iconPath: '/brand-icons/sensenova-dark.png' },
]

const providerCatalogMap = new Map(providerCatalog.map((p) => [p.id, p]))

export const BRAND_ORDER = providerCatalog.map((p) => p.name)

export const OTHER_BRAND_KEY = 'other'
export const OTHER_BRAND_LABEL = 'Other Brands'

export function getProviderCatalogEntry(id: string): ProviderCatalogEntry | undefined {
  return providerCatalogMap.get(id as ProviderId)
}

export function brandGroupKey(brand: string): string {
  return BRAND_ORDER.includes(brand) ? brand : OTHER_BRAND_KEY
}

export function brandOrderIndex(brand: string): number {
  const key = brandGroupKey(brand)
  if (key === OTHER_BRAND_KEY) return BRAND_ORDER.length
  return BRAND_ORDER.indexOf(key)
}

export function inferFallbackBrand(candidates: string[]): string {
  if (hasBrandFamily(candidates, ['openai', 'gpt', 'o1', 'o3', 'o4', 'dall', 'sora', 'codex'])) return 'OpenAI'
  if (hasBrandFamily(candidates, ['gemini', 'gemma', 'google', 'deep-research', 'lyria', 'nano-banana', 'veo'])) return 'Google'
  if (hasBrandFamily(candidates, ['claude', 'anthropic'])) return 'Anthropic'
  if (hasBrandFamily(candidates, ['grok', 'xai'])) return 'xAI'
  if (hasBrandFamily(candidates, ['deepseek'])) return 'DeepSeek'
  if (hasBrandFamily(candidates, ['qwen', 'qwen2', 'qwen3'])) return 'Qwen'
  if (hasBrandFamily(candidates, ['glm', 'zhipu', 'bigmodel', 'z.ai'])) return 'ZhipuAI'
  if (hasBrandFamily(candidates, ['kimi', 'moonshot', 'moonshotai', 'k3'])) return 'Moonshot'
  if (hasBrandFamily(candidates, ['minimax'])) return 'MiniMax'
  if (hasBrandFamily(candidates, ['mimo', 'xiaomi'])) return 'Xiaomi MiMo'
  if (hasBrandFamily(candidates, ['nemotron', 'nvidia'])) return 'NVIDIA'
  if (hasBrandFamily(candidates, ['bytedance', 'doubao', 'seedance', 'seedream'])) return 'ByteDance'
  if (hasBrandFamily(candidates, ['vidu', 'viduq'])) return 'Vidu'
  if (hasBrandFamily(candidates, ['step', 'stepaudio'])) return 'StepFun'
  if (hasBrandFamily(candidates, ['funaudiollm', 'tongyi', 'wan', 'happyhorse'])) return 'Alibaba'
  if (hasBrandFamily(candidates, ['kling', 'kwai', 'kolors'])) return 'Kuaishou'
  if (hasBrandFamily(candidates, ['bge'])) return 'BAAI'
  if (hasBrandFamily(candidates, ['flux'])) return 'FLUX'
  if (hasBrandFamily(candidates, ['hy', 'hunyuan'])) return 'Hunyuan'
  if (hasBrandFamily(candidates, ['sensenova'])) return 'SenseNova'
  if (hasBrandFamily(candidates, ['llama', 'meta'])) return 'Meta'
  if (hasBrandFamily(candidates, ['mistral'])) return 'Mistral'

  return OTHER_BRAND_LABEL
}

function hasBrandFamily(candidates: string[], families: string[]): boolean {
  return candidates.some((candidate) => families.some((family) => containsBrandFamily(candidate, family)))
}

function containsBrandFamily(value: string, family: string): boolean {
  const text = value.trim().toLowerCase().replace(/[_/]+/g, '-')
  const target = family.trim().toLowerCase().replace(/[_/]+/g, '-')
  if (!text || !target) return false

  let start = 0
  while (start < text.length) {
    const index = text.indexOf(target, start)
    if (index < 0) return false

    const before = text[index - 1]
    const after = text[index + target.length]
    const beforeBoundary = !before || /[-_./\s]/.test(before)
    const afterBoundary = !after || /[-_./\s]/.test(after) || /\d/.test(after)
    if (beforeBoundary && afterBoundary) return true

    start = index + target.length
  }

  return false
}
