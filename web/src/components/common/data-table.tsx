import { Fragment, useRef, type ReactNode } from 'react'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type RowData,
} from '@tanstack/react-table'
import { cn } from '@/lib/utils'
import { DataTableHeader, TableColumnResizeHandle, type StickyTableHeader } from '@/components/common/data-table-header'
import { useTableColumnSizing } from '@/hooks/use-table-column-sizing'
import type { TableColumnSizingOptions } from '@/lib/table-column-widths'

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    className?: string
    headerClassName?: string
    cellClassName?: string
    align?: 'left' | 'center' | 'right'
    resizeLabel?: string
  }
}

type DataTableProps<TData, TValue> = {
  columns: ColumnDef<TData, TValue>[]
  data: TData[]
  getRowId?: (originalRow: TData, index: number, parent?: unknown) => string
  selectedRowId?: string | null
  onRowClick?: (row: TData) => void
  emptyState?: ReactNode
  className?: string
  hideHeaderWhenEmpty?: boolean
  columnSizing?: TableColumnSizingOptions
  stickyHeader?: StickyTableHeader
  renderRowBefore?: (row: TData, index: number, colSpan: number) => ReactNode
  renderBodyAppend?: (colSpan: number) => ReactNode
}

function getAlignmentClass(align?: 'left' | 'center' | 'right') {
  if (align === 'center') return 'text-center'
  if (align === 'right') return 'text-right'
  return 'text-left'
}

export function DataTable<TData, TValue>({
  columns,
  data,
  getRowId,
  selectedRowId,
  onRowClick,
  emptyState,
  className,
  hideHeaderWhenEmpty,
  renderRowBefore,
  renderBodyAppend,
  columnSizing,
  stickyHeader,
}: DataTableProps<TData, TValue>) {
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId,
  })

  const tableRef = useRef<HTMLTableElement>(null)
  const sizing = useTableColumnSizing(tableRef, columnSizing)
  const leafColumns = table.getVisibleLeafColumns()
  const rows = table.getRowModel().rows
  const isEmpty = rows.length === 0

  if (isEmpty && hideHeaderWhenEmpty && !renderBodyAppend) {
    return (
      <div className={cn('overflow-hidden', className)}>
        {emptyState ?? (
          <div className="px-4 py-10 text-center text-body text-label-3">
            No results.
          </div>
        )}
      </div>
    )
  }

  return (
    // 容器外观（Card material="thick"）由调用方提供；表格本身只负责行列
    <div className={cn(stickyHeader !== 'page' && 'overflow-hidden', className)}>
      <div className={stickyHeader === 'page' ? undefined : 'overflow-x-auto'}>
        <table ref={tableRef} className="w-full min-w-full table-fixed border-collapse text-left">
          {columnSizing ? (
            <colgroup>
              {leafColumns.map((column, index) => <col key={column.id} style={{ width: `${sizing.widths[index]}%` }} />)}
            </colgroup>
          ) : null}
          <DataTableHeader sticky={stickyHeader}>
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id} className="text-footnote font-semibold text-label-2">
                {headerGroup.headers.map((header, index) => {
                  const meta = header.column.columnDef.meta
                  const previousColumn = leafColumns[index - 1]
                  const resizeLabel = previousColumn?.columnDef.meta?.resizeLabel
                    ?? (typeof previousColumn?.columnDef.header === 'string' ? previousColumn.columnDef.header : String(index))

                  return (
                    <th
                      key={header.id}
                      className={cn(
                        'relative h-9 whitespace-nowrap px-4 py-2',
                        getAlignmentClass(meta?.align),
                        meta?.className,
                        meta?.headerClassName,
                      )}
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(header.column.columnDef.header, header.getContext())}
                      {columnSizing && index > 0 && headerGroup.depth === table.getHeaderGroups().length - 1 ? (
                        <TableColumnResizeHandle sizing={sizing} index={index - 1} label={resizeLabel} />
                      ) : null}
                    </th>
                  )
                })}
              </tr>
            ))}
          </DataTableHeader>
          <tbody>
            {rows.length ? (
              rows.map((row, index) => (
                <Fragment key={row.id}>
                  {renderRowBefore?.(row.original, index, columns.length)}
                  <tr
                    className={cn(
                      'border-t border-separator text-body text-label transition-colors duration-150',
                      selectedRowId === row.id ? 'bg-tint/10' : 'hover:bg-fill-4',
                      onRowClick && 'cursor-pointer',
                    )}
                    onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                  >
                    {row.getVisibleCells().map((cell) => {
                      const meta = cell.column.columnDef.meta

                      return (
                        <td
                          key={cell.id}
                          className={cn(
                            'h-11 px-4 py-3 align-middle',
                            getAlignmentClass(meta?.align),
                            meta?.className,
                            meta?.cellClassName,
                          )}
                        >
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </td>
                      )
                    })}
                  </tr>
                </Fragment>
              ))
            ) : renderBodyAppend ? null : (
              <tr className="border-t border-separator">
                <td
                  colSpan={columns.length}
                  className="px-4 py-10 text-center text-body text-label-3"
                >
                  {emptyState ?? 'No results.'}
                </td>
              </tr>
            )}
            {renderBodyAppend?.(columns.length)}
          </tbody>
        </table>
      </div>
    </div>
  )
}
