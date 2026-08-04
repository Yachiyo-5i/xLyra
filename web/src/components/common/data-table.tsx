import { Fragment, type ReactNode } from 'react'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
  type RowData,
} from '@tanstack/react-table'
import { cn } from '@/lib/utils'

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    className?: string
    headerClassName?: string
    cellClassName?: string
    align?: 'left' | 'center' | 'right'
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
}: DataTableProps<TData, TValue>) {
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId,
  })

  const rows = table.getRowModel().rows
  const isEmpty = rows.length === 0

  if (isEmpty && hideHeaderWhenEmpty && !renderBodyAppend) {
    return (
      <div className={cn('overflow-hidden rounded-lg', className)}>
        {emptyState ?? (
          <div className="px-4 py-10 text-center text-sm text-[hsl(var(--text-muted-soft))]">
            No results.
          </div>
        )}
      </div>
    )
  }

  return (
    <div className={cn('overflow-hidden rounded-lg', className)}>
      <div className="overflow-x-auto">
        <table className="w-full min-w-full table-fixed border-collapse text-left">
          <thead className="bg-[hsl(var(--surface-subtle))]">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id} className="text-faint text-xs uppercase tracking-[0.16em]">
                {headerGroup.headers.map((header) => {
                  const meta = header.column.columnDef.meta

                  return (
                    <th
                      key={header.id}
                      className={cn(
                        'whitespace-nowrap px-4 py-3 font-medium',
                        getAlignmentClass(meta?.align),
                        meta?.className,
                        meta?.headerClassName,
                      )}
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(header.column.columnDef.header, header.getContext())}
                    </th>
                  )
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {rows.length ? (
              rows.map((row, index) => (
                <Fragment key={row.id}>
                  {renderRowBefore?.(row.original, index, columns.length)}
                  <tr
                    className={cn(
                      'border-t border-[hsl(var(--glass-divider))] transition-colors [&>td]:transition-colors',
                      selectedRowId === row.id
                        ? '[&>td]:bg-[hsl(var(--surface-subtle))] [&>td:first-child]:rounded-l-md [&>td:last-child]:rounded-r-md'
                        : 'hover:[&>td]:bg-[hsl(var(--surface-subtle))] hover:[&>td:first-child]:rounded-l-md hover:[&>td:last-child]:rounded-r-md',
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
                            'px-4 py-4 align-middle',
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
              <tr className="border-t border-[hsl(var(--glass-divider))]">
                <td
                  colSpan={columns.length}
                  className="px-4 py-10 text-center text-sm text-[hsl(var(--text-muted-soft))]"
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
