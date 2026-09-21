import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import type { RequestLogItem } from '@/features/requests/api/requests'
import translations from '@/locales/zh/requests.json'
import { RequestModelMapping } from './request-model-mapping'

const i18n = createInstance()
beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: { zh: { requests: translations } }, interpolation: { escapeValue: false } })
})

function renderModel(overrides: Partial<RequestLogItem>, inline = false) {
  const item: RequestLogItem = {
    id: 'log-1', request_id: 'req-1', success: true, created_at: '',
    api_key: {}, site: {}, usage: {}, model: { canonical_model: 'requested-model', upstream_model: 'requested-model' },
    requested_model: 'requested-model', ...overrides,
  }
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <RequestModelMapping item={item} inline={inline} />
    </I18nextProvider>,
  )
}

describe('response model mismatch badge', () => {
  it.each([false, true])('shows the returned model in desktop/mobile mode (inline=%s)', (inline) => {
    const markup = renderModel({ upstream_response_model: 'returned-model' }, inline)
    expect(markup).toContain('>requested-model</span>')
    expect(markup).toContain('>returned-model</span>')
    expect(markup).toContain('title="上游响应模型与请求及路由模型均不一致，可能已换模型：returned-model"')
    expect(markup).toContain('var(--destructive)')
  })

  it.each([undefined, null, '', '  ', 'requested-model', ' requested-model '])('hides absent or matching response model %s', (model) => {
    expect(renderModel({ upstream_response_model: model })).not.toContain('aria-label="上游响应模型')
  })

  it('does not infer a missing historical request model from route configuration', () => {
    expect(renderModel({ requested_model: null, upstream_response_model: 'returned-model' })).not.toContain('aria-label="上游响应模型')
  })

  it('keeps the soft mapping display and compares against the original request', () => {
    const markup = renderModel({
      original_model: 'original-model', requested_model: 'mapped-model', mapped_model: 'mapped-model',
      mapping_mode: 'soft', model: { upstream_model: 'mapped-model' }, upstream_response_model: 'mapped-model',
    })
    expect(markup).toContain('>original-model</span>')
    expect(markup).toContain(translations.modelMapping.softFallback)
    expect(markup).not.toContain('aria-label="上游响应模型')
  })

  it('does not flag a response that matches the routed upstream model', () => {
    const markup = renderModel({
      original_model: 'deepseek-v4-flash', requested_model: 'deepseek-v4-flash',
      model: { upstream_model: 'deepseek-v4-flash-0731' },
      upstream_response_model: 'deepseek-v4-flash-0731',
    })
    expect(markup).toContain('>deepseek-v4-flash</span>')
    expect(markup).toContain('>deepseek-v4-flash-0731</span>')
    expect(markup).not.toContain('aria-label="上游响应模型')
  })

  it('uses the canonical model key for version aliases', () => {
    const markup = renderModel({
      requested_model: 'deepseek-v4-flash',
      model: { canonical_model: 'deepseek-v4-flash', upstream_model: 'deepseek-v4-flash-0731' },
      upstream_response_model: 'z-ai/deepseek-v4-flash',
    })
    expect(markup).toContain('badge-warning-bg')
    expect(markup).not.toContain('var(--destructive)')
  })

  it.each(['requested-model-20260921', 'Requested-Model'])('compares full names without alias or case normalization: %s', (model) => {
    expect(renderModel({ model: { canonical_model: null, upstream_model: null }, upstream_response_model: model })).toContain('var(--destructive)')
  })

  it.each([
    ['glm-5.3-flash', 'z-ai/glm-5.3-flash'],
    ['z-ai/glm-5.3-flash', 'glm-5.3-flash'],
    ['provider-a/glm-5.3-flash', 'provider-b/glm-5.3-flash'],
  ])('uses a gold badge for namespace differences: %s to %s', (requested, response) => {
    const markup = renderModel({ requested_model: requested, upstream_response_model: response })
    expect(markup).toContain('badge-warning-bg')
    expect(markup).not.toContain('var(--destructive)')
    expect(markup).toContain('上游响应模型仅厂商前缀不同')
  })

  it.each(['z-ai/glm-5.3', 'z-ai/glm-5.3-pro', 'z-ai/glm-5.2-flash', 'z-ai/'])('keeps differing variants and versions red: %s', (response) => {
    expect(renderModel({ requested_model: 'glm-5.3-flash', upstream_response_model: response })).toContain('var(--destructive)')
  })

  it('treats a dated model suffix as the same catalog model', () => {
    const markup = renderModel({
      requested_model: 'glm-5.3-flash',
      model: { canonical_model: 'glm-5.3-flash', upstream_model: 'glm-5.3-flash-20260921' },
      upstream_response_model: 'z-ai/glm-5.3-flash-20260921',
    })
    expect(markup).toContain('badge-warning-bg')
    expect(markup).not.toContain('var(--destructive)')
  })

  it.each([false, true])('places muted reasoning beside the requested model (inline=%s)', (inline) => {
    const markup = renderModel({ reasoning_effort: 'high' }, inline)
    expect(markup).toMatch(/items-baseline[^>]*><span[^>]*>requested-model<\/span><span[^>]*text-\[10px\][^>]*font-normal[^>]*text-muted-soft[^>]*>high<\/span>/)
    expect(markup).toContain('title="推理强度: high"')
  })
})
