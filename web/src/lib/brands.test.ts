import { describe, expect, it } from 'vitest'
import { inferFallbackBrand } from './brands'
import { resolveThemedIconPath } from '@/components/common/brand-utils'

describe('inferFallbackBrand', () => {
  it.each([
    ['sora-2-official', 'OpenAI'],
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
  ])('recognizes %s as %s', (model, brand) => {
    expect(inferFallbackBrand([model])).toBe(brand)
  })

  it('does not match a family embedded in another word', () => {
    expect(inferFallbackBrand(['mygptproxy'])).toBe('Other Brands')
  })

  it('uses light variants for new monochrome brand icons', () => {
    expect(resolveThemedIconPath('/brand-icons/vidu-dark.png', 'Vidu', 'light')).toBe('/brand-icons/vidu-light.png')
    expect(resolveThemedIconPath('/brand-icons/vidu-dark.png', 'Vidu', 'dark')).toBe('/brand-icons/vidu-dark.png')
    expect(resolveThemedIconPath('/brand-icons/kuaishou.svg', 'Kuaishou', 'light')).toBe('/brand-icons/kuaishou.svg')
    expect(resolveThemedIconPath('/brand-icons/hunyuan-dark.png', 'Hunyuan', 'light')).toBe('/brand-icons/hunyuan-light.png')
  })
})
