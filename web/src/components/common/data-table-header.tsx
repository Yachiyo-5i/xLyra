import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { useTableColumnSizing } from '@/hooks/use-table-column-sizing'
import { cn } from '@/lib/utils'

export type StickyTableHeader = 'container' | 'page'

export function DataTableHeader({ children, sticky }: { children: ReactNode; sticky?: StickyTableHeader }) {
  return (
    <thead className={cn(
      sticky ? 'sticky z-10 bg-[hsl(var(--surface-base))] shadow-[0_1px_0_hsl(var(--glass-divider))]' : 'bg-[hsl(var(--surface-subtle))]',
      sticky === 'container' && 'top-0',
      sticky === 'page' && '-top-6 lg:-top-8',
    )}>
      {children}
    </thead>
  )
}

export function TableColumnResizeHandle({ sizing, index, label }: {
  sizing: ReturnType<typeof useTableColumnSizing>
  index: number
  label: string
}) {
  const { t } = useTranslation('common')
  const maximum = 100 - sizing.minimumWidths.reduce((total, width, columnIndex) => columnIndex === index ? total : total + width, 0)
  return (
    <span
      role="separator"
      aria-orientation="vertical"
      aria-label={t('table.resizeColumn', { column: label })}
      aria-valuenow={sizing.widths[index]}
      aria-valuemin={sizing.minimumWidths[index]}
      aria-valuemax={maximum}
      tabIndex={0}
      className="absolute -left-2 top-0 z-20 h-full w-4 cursor-col-resize touch-none select-none outline-none after:absolute after:inset-y-2 after:left-1/2 after:w-px after:-translate-x-1/2 after:bg-[hsl(var(--glass-divider))] after:opacity-50 hover:after:bg-primary hover:after:opacity-100 focus-visible:after:bg-primary focus-visible:after:opacity-100"
      style={{ cursor: 'col-resize' }}
      onClick={(event) => event.stopPropagation()}
      onPointerDown={(event) => sizing.onPointerDown(index, event)}
      onPointerMove={sizing.onPointerMove}
      onPointerUp={sizing.onPointerEnd}
      onPointerCancel={sizing.onPointerEnd}
      onKeyDown={(event) => sizing.onKeyDown(index, event)}
    />
  )
}
