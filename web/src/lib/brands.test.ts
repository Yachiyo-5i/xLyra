import { describe, expect, it } from 'vitest'
import { getProviderCatalogEntry, getProviderCatalogEntryByName, inferFallbackBrand } from './brands'
import { siteTypeIconPath } from '@/components/common/brand-utils'

describe('inferFallbackBrand', () => {
  it.each([
    ['sora-2-official', 'OpenAI'],
    ['codex-auto-review', 'OpenAI'],
    ['veo3.1-fast', 'Google'],
    ['nano_banana_2', 'Google'],
    ['qwen3.7-max', 'Qwen'],
    ['grok', 'xAI'],
    ['moonshotai-kimi-k2.6', 'Moonshot'],
    ['bytedance-seed-seed-oss-36b-instruct', 'ByteDance'],
    ['viduq3-fast', 'Vidu'],
    ['stepaudio-2.5-asr', 'StepFun'],
    ['wan2.6-flash', 'Alibaba'],
    ['tongyi-mai-z-image', 'Alibaba'],
    ['kling-v3', 'Kuaishou'],
    ['bge-m3', 'BAAI'],
    ['flux-kontext-pro', 'FLUX'],
    ['happyhorse-1.1', 'Alibaba'],
    ['k3-256k', 'Moonshot'],
    ['hy3-preview', 'Hunyuan'],
    ['tencent-hunyuan-mt-7b', 'Hunyuan'],
    ['sensenova-u1-fast', 'SenseNova'],
    ['jev-latest', 'TypeSafe'],
  ])('recognizes %s as %s', (model, brand) => {
    expect(inferFallbackBrand([model])).toBe(brand)
  })

  it('does not match a family embedded in another word', () => {
    expect(inferFallbackBrand(['mygptproxy'])).toBe('Other Brands')
  })

  it('includes TypeSafe in the provider catalog', () => {
    const entry = getProviderCatalogEntry('typesafe')
    expect(entry).toMatchObject({
      id: 'typesafe',
      name: 'TypeSafe',
      iconPath: '/brand-icons/typesafe-dark.png',
    })
    expect(getProviderCatalogEntryByName('TypeSafe')).toBe(entry)
    expect(getProviderCatalogEntry(' TYPESAFE ')).toBe(entry)
  })

  it('maps site types to bundled brand icons', () => {
    expect(siteTypeIconPath('typesafe')).toBe('/brand-icons/typesafe-dark.png')
    expect(siteTypeIconPath('openai')).toBe('/brand-icons/openai.png')
  })
})
