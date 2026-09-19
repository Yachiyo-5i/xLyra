export type TableColumnSizingOptions = {
  storageKey: string
  defaultWidths: readonly number[]
  minimumWidths: readonly number[]
}

const TABLE_TOTAL_WIDTH_UNITS = 10_000
const TABLE_WIDTH_PRECISION = 100

export function defaultTableColumnWidths(weights: readonly number[]): number[] {
  return distributeUnits(TABLE_TOTAL_WIDTH_UNITS, [...weights]).map((units) => units / TABLE_WIDTH_PRECISION)
}

export function readTableColumnWidths(options: TableColumnSizingOptions): readonly number[] {
  try {
    if (typeof window !== 'undefined') {
      const value = window.localStorage.getItem(options.storageKey)
      const parsed: unknown = value ? JSON.parse(value) : null
      if (isTableColumnWidths(parsed, options.minimumWidths)) return [...parsed]
    }
  } catch {
    return [...options.defaultWidths]
  }
  return [...options.defaultWidths]
}

export function writeTableColumnWidths(options: TableColumnSizingOptions, widths: readonly number[]): boolean {
  if (!isTableColumnWidths(widths, options.minimumWidths)) return false
  try {
    if (typeof window === 'undefined') return false
    window.localStorage.setItem(options.storageKey, JSON.stringify(widths))
    return true
  } catch {
    return false
  }
}

export function isTableColumnWidths(value: unknown, minimumWidths: readonly number[]): value is readonly number[] {
  if (!Array.isArray(value) || value.length !== minimumWidths.length) return false

  let totalUnits = 0
  for (let index = 0; index < value.length; index += 1) {
    const width = value[index]
    const widthUnits = toWidthUnits(width)
    if (widthUnits == null || widthUnits < minimumWidths[index] * TABLE_WIDTH_PRECISION) {
      return false
    }
    totalUnits += widthUnits
  }

  return totalUnits === TABLE_TOTAL_WIDTH_UNITS
}

export function resizeTableColumnBoundary(
  widths: readonly number[],
  boundaryIndex: number,
  deltaPercent: number,
  options: TableColumnSizingOptions,
): number[] {
  const { defaultWidths, minimumWidths } = options
  if (!isTableColumnWidths(widths, minimumWidths) || !Number.isFinite(deltaPercent)) {
    return [...defaultWidths]
  }
  if (boundaryIndex < 0 || boundaryIndex >= widths.length - 1) {
    return [...widths]
  }

  const deltaUnits = Math.round(deltaPercent * TABLE_WIDTH_PRECISION)
  if (deltaUnits === 0) return [...widths]

  const widthUnits = widths.map((width) => toWidthUnits(width) ?? 0)
  const targetMinimum = minimumWidths[boundaryIndex] * TABLE_WIDTH_PRECISION
  const otherIndexes = widthUnits.map((_, index) => index).filter((index) => index !== boundaryIndex)
  const otherMinimum = otherIndexes.reduce(
    (total, index) => total + minimumWidths[index] * TABLE_WIDTH_PRECISION,
    0,
  )
  const nextTarget = clamp(
    widthUnits[boundaryIndex] + deltaUnits,
    targetMinimum,
    TABLE_TOTAL_WIDTH_UNITS - otherMinimum,
  )
  const targetDelta = nextTarget - widthUnits[boundaryIndex]
  if (targetDelta === 0) return [...widths]

  const otherWeights = otherIndexes.map((index) => targetDelta > 0
    ? widthUnits[index] - minimumWidths[index] * TABLE_WIDTH_PRECISION
    : widthUnits[index],
  )
  const linkedChanges = distributeUnits(Math.abs(targetDelta), otherWeights)

  widthUnits[boundaryIndex] = nextTarget
  otherIndexes.forEach((index, otherIndex) => {
    widthUnits[index] += targetDelta > 0 ? -linkedChanges[otherIndex] : linkedChanges[otherIndex]
  })
  return widthUnits.map((units) => units / TABLE_WIDTH_PRECISION)
}

function distributeUnits(total: number, weights: number[]) {
  const weightTotal = weights.reduce((sum, weight) => sum + weight, 0)
  if (weightTotal === 0) return weights.map(() => 0)

  const allocations = weights.map((weight) => Math.floor((total * weight) / weightTotal))
  let remaining = total - allocations.reduce((sum, allocation) => sum + allocation, 0)
  const fractions = weights
    .map((weight, index) => ({ index, remainder: (total * weight) % weightTotal }))
    .sort((left, right) => right.remainder - left.remainder || left.index - right.index)

  for (const { index } of fractions) {
    if (remaining === 0) break
    allocations[index] += 1
    remaining -= 1
  }
  return allocations
}

function toWidthUnits(value: unknown): number | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null
  const scaled = value * TABLE_WIDTH_PRECISION
  const units = Math.round(scaled)
  // Decimal percentages from JSON are subject to binary floating-point noise
  // (for example, 0.07 * 100). Accept only values that round to 0.01%.
  if (Math.abs(scaled - units) > Number.EPSILON * Math.max(1, Math.abs(scaled)) * 4) {
    return null
  }
  return Number.isSafeInteger(units) ? units : null
}

function clamp(value: number, minimum: number, maximum: number) {
  return Math.min(Math.max(value, minimum), maximum)
}
