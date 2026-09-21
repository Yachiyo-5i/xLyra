import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import { requestQueryKeys, type RequestLogItem } from '@/features/requests/api/requests'
import translations from '@/locales/zh/requests.json'
import { RequestDetailContent } from './request-detail-row'

const i18n = createInstance()
beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: { zh: { requests: translations } }, interpolation: { escapeValue: false } })
})

describe('request detail response model', () => {
  it.each([
    ['glm-5.3-flash', 'badge-neutral-bg'],
    ['z-ai/glm-5.3-flash', 'badge-warning-bg'],
    ['different-model', 'var(--destructive)'],
  ])('shows the observed response model with the correct color: %s', (responseModel, color) => {
    const item: RequestLogItem = {
      id: 'log-1', request_id: 'req-1', success: true, created_at: '',
      api_key: {}, site: {}, usage: {}, model: {}, requested_model: 'glm-5.3-flash',
      upstream_response_model: responseModel,
    }
    const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } })
    client.setQueryData(requestQueryKeys.detail(item.id), { request: item })
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <RequestDetailContent item={item} />
        </I18nextProvider>
      </QueryClientProvider>,
    )
    expect(markup).toContain('>响应模型</span>')
    expect(markup).toContain(`>${responseModel}</span>`)
    const responseBadge = markup.match(/<span class="([^"]*)" title="[^"]*" aria-label="[^"]*"><span class="truncate">[^<]*<\/span><\/span>/)?.[1]
    expect(responseBadge).toContain(color)
    client.clear()
  })
})
