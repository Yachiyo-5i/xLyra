import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { CompactRequestsTokensCard } from './compact-requests-tokens-card'

describe('CompactRequestsTokensCard', () => {
  it('keeps request and token values in a readable today/total matrix', () => {
    const markup = renderToStaticMarkup(
      <CompactRequestsTokensCard
        title="请求与 Tokens"
        todayLabel="今日"
        totalLabel="累计"
        requestsLabel="请求"
        tokensLabel="Tokens"
        requestsToday="192"
        requestsTotal="90.8K"
        tokensToday="21.7M"
        tokensTotal="2.4B"
        successRateLabel="成功率"
        successRate="97.5%"
        yesterdayRequestsLabel="昨日请求"
        requestsYesterday="194"
        yesterdayTokensLabel="昨日 Tokens"
        tokensYesterday="20M"
        tokenUsage={[
          { label: '今日', usage: { total: 21_700_000, input: 18_000_000, output: 3_700_000, cached: 4_000_000 } },
          { label: '累计', usage: { total: 2_400_000_000, input: 2_000_000_000, output: 400_000_000, cached: 500_000_000 } },
        ]}
        tokenUsageLabels={{ total: '总计', input: '输入', output: '输出', cached: '缓存', hitRate: '命中率' }}
      />,
    )

    const values = [...markup.matchAll(/<strong[^>]*>([^<]+)<\/strong>/g)].map((match) => match[1])

    expect(markup).toContain('role="group"')
    expect(markup).toContain('aria-label="请求与 Tokens"')
    expect(markup).toContain('tabindex="0"')
    expect(markup).toContain('whitespace-nowrap')
    expect(markup).toContain('tabular-nums')
    expect(values.slice(0, 4)).toEqual(['192', '90.8K', '21.7M', '2.4B'])
  })
})
