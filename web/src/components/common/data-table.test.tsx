import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import type { ColumnDef } from '@tanstack/react-table'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { DataTable } from '@/components/common/data-table'
import translations from '@/locales/zh/common.json'

const i18n = createInstance()
const columns: ColumnDef<{ name: string }>[] = [
  { accessorKey: 'name', header: '名称', cell: ({ row }) => row.original.name },
  { id: 'updated', header: () => <button type="button">更新时间</button>, meta: { resizeLabel: '更新时间' } },
  { id: 'actions', header: '操作' },
]
const sizing = { storageKey: 'table-test', defaultWidths: [40, 35, 25], minimumWidths: [20, 15, 10] }

beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: { zh: { common: translations } }, interpolation: { escapeValue: false } })
})

afterEach(() => vi.unstubAllGlobals())

function render(props: Partial<React.ComponentProps<typeof DataTable<{ name: string }, unknown>>>) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <DataTable columns={columns} data={[{ name: '示例' }]} {...props} />
    </I18nextProvider>,
  )
}

describe('DataTable shared column sizing and header', () => {
  it('renders persisted widths and accessible resize handles on each boundary', () => {
    vi.stubGlobal('window', { localStorage: { getItem: () => '[50,30,20]' } })
    const html = render({ columnSizing: sizing, stickyHeader: 'container' })
    expect(html).toContain('<col style="width:50%"')
    expect(html).toContain('<col style="width:30%"')
    expect(html.match(/role="separator"/g)).toHaveLength(2)
    expect(html).toContain('aria-label="调整名称列宽"')
    expect(html).toContain('aria-label="调整更新时间列宽"')
    expect(html).toContain('aria-valuenow="50"')
    expect(html).toContain('aria-valuemax="75"')
    expect(html).toContain('top-0')
    expect(html).toContain('overflow-x-auto')
  })

  it('allows page scrolling to reach the sticky header without a nested scroll container', () => {
    const html = render({ columnSizing: sizing, stickyHeader: 'page' })
    expect(html).toContain('-top-6 lg:-top-8')
    expect(html).not.toContain('overflow-x-auto')
    expect(html).not.toContain('overflow-hidden')
  })

  it('keeps the default table without resizing or sticky header', () => {
    const html = render({})
    expect(html).not.toContain('role="separator"')
    expect(html).not.toContain('<colgroup>')
    expect(html).not.toContain('sticky')
    expect(html).toContain('overflow-x-auto')
    expect(html).toContain('surface-subtle')
  })

  it('preserves custom group rows and empty states with sizing enabled', () => {
    const html = render({
      columnSizing: sizing,
      renderRowBefore: (_row, _index, colSpan) => <tr><td colSpan={colSpan}>分组</td></tr>,
      renderBodyAppend: (colSpan) => <tr><td colSpan={colSpan}>折叠组</td></tr>,
    })
    expect(html).toContain('<td colSpan="3">分组</td>')
    expect(html).toContain('<td colSpan="3">折叠组</td>')
    const empty = render({ columnSizing: sizing, data: [], hideHeaderWhenEmpty: true, emptyState: '没有记录' })
    expect(empty).toContain('没有记录')
    expect(empty).not.toContain('<table')
  })
})
