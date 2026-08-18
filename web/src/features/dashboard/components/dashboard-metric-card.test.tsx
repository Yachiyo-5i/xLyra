import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { DashboardMetricCard } from './dashboard-metric-card'

describe('DashboardMetricCard', () => {
  it('stacks metrics in narrow cards and restores columns when the card is wide enough', () => {
    const markup = renderToStaticMarkup(
      <DashboardMetricCard
        title="请求与 Tokens"
        primaryLabel="今日请求 / 总请求"
        primaryValue="192 / 90.8K"
        secondaryLabel="今日 Tokens / 总 Tokens"
        secondaryValue="21.7M / 2.4B"
      />,
    )

    expect(markup).toContain('@container/metric')
    expect(markup).toContain('grid-cols-1')
    expect(markup).toContain('@[21rem]/metric:grid-cols-')
    expect(markup).toContain('whitespace-nowrap')
    expect(markup).toContain('192 / 90.8K')
    expect(markup).toContain('21.7M / 2.4B')
  })
})
