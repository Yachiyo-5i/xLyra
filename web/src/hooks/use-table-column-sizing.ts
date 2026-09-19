import { type KeyboardEvent, type RefObject, type PointerEvent as ReactPointerEvent, useRef, useState } from 'react'
import { readTableColumnWidths, resizeTableColumnBoundary, writeTableColumnWidths, type TableColumnSizingOptions } from '@/lib/table-column-widths'

type ColumnResizeState = {
  boundaryIndex: number
  pointerID: number
  startX: number
  tableWidth: number
  widths: readonly number[]
}

export function useTableColumnSizing(tableRef: RefObject<HTMLTableElement | null>, options?: TableColumnSizingOptions) {
  const [widths, setWidths] = useState(() => options ? readTableColumnWidths(options) : [])
  const widthsRef = useRef(widths)
  const resizeStateRef = useRef<ColumnResizeState | null>(null)

  function updateWidths(next: readonly number[]) {
    widthsRef.current = next
    setWidths(next)
  }

  function onPointerDown(boundaryIndex: number, event: ReactPointerEvent<HTMLSpanElement>) {
    if (!options || event.button !== 0) return
    const tableWidth = tableRef.current?.getBoundingClientRect().width ?? 0
    if (tableWidth <= 0) return
    event.preventDefault()
    event.stopPropagation()
    event.currentTarget.setPointerCapture(event.pointerId)
    resizeStateRef.current = {
      boundaryIndex,
      pointerID: event.pointerId,
      startX: event.clientX,
      tableWidth,
      widths: widthsRef.current,
    }
  }

  function onPointerMove(event: ReactPointerEvent<HTMLSpanElement>) {
    const state = resizeStateRef.current
    if (!options || !state || state.pointerID !== event.pointerId) return
    event.preventDefault()
    event.stopPropagation()
    const delta = ((event.clientX - state.startX) / state.tableWidth) * 100
    updateWidths(resizeTableColumnBoundary(state.widths, state.boundaryIndex, delta, options))
  }

  function onPointerEnd(event: ReactPointerEvent<HTMLSpanElement>) {
    const state = resizeStateRef.current
    if (!options || !state || state.pointerID !== event.pointerId) return
    event.preventDefault()
    event.stopPropagation()
    resizeStateRef.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    writeTableColumnWidths(options, widthsRef.current)
  }

  function onKeyDown(boundaryIndex: number, event: KeyboardEvent<HTMLSpanElement>) {
    if (!options || (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight')) return
    event.preventDefault()
    event.stopPropagation()
    const direction = event.key === 'ArrowLeft' ? -1 : 1
    const next = resizeTableColumnBoundary(widthsRef.current, boundaryIndex, direction * (event.shiftKey ? 5 : 1), options)
    updateWidths(next)
    writeTableColumnWidths(options, next)
  }

  return { widths, minimumWidths: options?.minimumWidths ?? [], onPointerDown, onPointerMove, onPointerEnd, onKeyDown }
}
