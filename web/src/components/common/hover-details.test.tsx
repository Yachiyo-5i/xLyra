import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { StatusBadge } from '@/components/common/status-badge'
import { HoverDetails } from './hover-details'

const layout = vi.hoisted(() => ({ mobile: false }))

vi.mock('@/hooks/use-media-query', () => ({ useMobileLayout: () => layout.mobile }))
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }))

describe('HoverDetails', () => {
  for (const mobile of [false, true]) {
    it(`preserves the badge and makes it keyboard accessible on ${mobile ? 'mobile' : 'desktop'}`, () => {
      layout.mobile = mobile
      const markup = renderToStaticMarkup(
        <HoverDetails asChild title="刷新失败" content="上游请求超时">
          <StatusBadge status="error" className="shrink-0">错误</StatusBadge>
        </HoverDetails>,
      )
      expect(markup).toContain('role="button"')
      expect(markup).toContain('tabindex="0"')
      expect(markup).toContain('aria-label="刷新失败"')
      expect(markup).toContain('aria-haspopup="dialog"')
      expect(markup).toContain('shrink-0')
      expect(markup).not.toContain('cursor-')
      expect(markup).not.toContain('actions.close')
      expect(markup.match(/<div\b/g)).toHaveLength(1)
    })
  }

  it('keeps disabled content unchanged and noninteractive', () => {
    const child = <StatusBadge status="healthy">正常</StatusBadge>
    expect(renderToStaticMarkup(
      <HoverDetails asChild disabled title="状态" content="无需展开">{child}</HoverDetails>,
    )).toBe(renderToStaticMarkup(child))
  })

  it('preserves a trigger cursor and provides a closed-state description', () => {
    const markup = renderToStaticMarkup(
      <HoverDetails asChild title="额度" accessibleDescription="周额度剩余 70%" content="周额度剩余 70%">
        <span className="cursor-text">70%</span>
      </HoverDetails>,
    )
    expect(markup).toContain('class="cursor-text"')
    expect(markup).toContain('aria-describedby=')
    expect(markup).toContain('role="tooltip"')
    expect(markup).toContain('周额度剩余 70%')
  })
})
