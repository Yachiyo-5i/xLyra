import { renderToStaticMarkup } from 'react-dom/server'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AccountHeaderActions } from '@/features/sites/components/account-dialogs'

const layout = vi.hoisted(() => ({ mobile: false }))

vi.mock('@/hooks/use-media-query', () => ({
  useMobileLayout: () => layout.mobile,
}))

function render() {
  return renderToStaticMarkup(
    <AccountHeaderActions
      addLabel="添加账号"
      onAdd={() => {}}
      importLabel="导入账号"
      onImport={() => {}}
      refreshAllLabel="全部刷新"
      showRefreshAll={false}
      onRefreshAll={() => {}}
    />,
  )
}

function buttons(markup: string) {
  return [...markup.matchAll(/<button\b[^>]*>[\s\S]*?<\/button>/g)].map((match) => match[0])
}

describe('AccountHeaderActions layout', () => {
  beforeEach(() => {
    layout.mobile = false
  })

  it('shows text only on desktop: no icon in the import or add button', () => {
    const found = buttons(render())
    expect(found).toHaveLength(2)
    for (const button of found) expect(button).not.toContain('<svg')
    expect(found[0]).toContain('导入账号')
    expect(found[1]).toContain('添加账号')
  })

  it('shows icons only on mobile: an icon with an accessible name and no visible text', () => {
    layout.mobile = true
    const found = buttons(render())
    expect(found).toHaveLength(2)
    for (const button of found) {
      expect(button).toContain('<svg')
      expect(button.replace(/<svg[\s\S]*?<\/svg>/g, '').replace(/<[^>]*>/g, '').trim()).toBe('')
    }
    expect(found[0]).toContain('aria-label="导入账号"')
    expect(found[1]).toContain('aria-label="添加账号"')
  })
})
