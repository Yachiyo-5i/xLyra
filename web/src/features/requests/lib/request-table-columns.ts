export type RequestTableColumnWidths = readonly number[]

const REQUEST_TABLE_TOTAL_WIDTH_UNITS = 10_000
const REQUEST_TABLE_WIDTH_PRECISION = 100

export const REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS: RequestTableColumnWidths = [3, 11, 16, 11, 7, 15, 12, 8, 7, 10]
export const REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS: RequestTableColumnWidths = [3, 7, 10, 7, 6, 0, 8, 6, 6, 7]

export function defaultRequestTableColumnWidths(): number[] {
  return [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
}

export function isRequestTableColumnWidths(value: unknown): value is RequestTableColumnWidths {
  if (!Array.isArray(value) || value.length !== REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS.length) return false

  let totalUnits = 0
  for (let index = 0; index < value.length; index += 1) {
    const width = value[index]
    const widthUnits = toWidthUnits(width)
    if (widthUnits == null || widthUnits < REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS[index] * REQUEST_TABLE_WIDTH_PRECISION) {
      return false
    }
    totalUnits += widthUnits
  }

  return totalUnits === REQUEST_TABLE_TOTAL_WIDTH_UNITS
}

export function resizeRequestTableColumnBoundary(
  widths: RequestTableColumnWidths,
  boundaryIndex: number,
  deltaPercent: number,
): number[] {
  if (!isRequestTableColumnWidths(widths) || !Number.isFinite(deltaPercent)) {
    return defaultRequestTableColumnWidths()
  }
  if (boundaryIndex < 0 || boundaryIndex >= widths.length - 1) {
    return [...widths]
  }

  const deltaUnits = Math.round(deltaPercent * REQUEST_TABLE_WIDTH_PRECISION)
  if (deltaUnits === 0) return [...widths]

  const widthUnits = widths.map((width) => toWidthUnits(width) ?? 0)
  const targetMinimum = REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS[boundaryIndex] * REQUEST_TABLE_WIDTH_PRECISION
  const otherIndexes = widthUnits.map((_, index) => index).filter((index) => index !== boundaryIndex)
  const otherMinimum = otherIndexes.reduce(
    (total, index) => total + REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS[index] * REQUEST_TABLE_WIDTH_PRECISION,
    0,
  )
  const nextTarget = clamp(
    widthUnits[boundaryIndex] + deltaUnits,
    targetMinimum,
    REQUEST_TABLE_TOTAL_WIDTH_UNITS - otherMinimum,
  )
  const targetDelta = nextTarget - widthUnits[boundaryIndex]
  if (targetDelta === 0) return [...widths]

  const otherWeights = otherIndexes.map((index) => targetDelta > 0
    ? widthUnits[index] - REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS[index] * REQUEST_TABLE_WIDTH_PRECISION
    : widthUnits[index],
  )
  const linkedChanges = distributeUnits(Math.abs(targetDelta), otherWeights)

  widthUnits[boundaryIndex] = nextTarget
  otherIndexes.forEach((index, otherIndex) => {
    widthUnits[index] += targetDelta > 0 ? -linkedChanges[otherIndex] : linkedChanges[otherIndex]
  })
  return widthUnits.map((units) => units / REQUEST_TABLE_WIDTH_PRECISION)
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
  const scaled = value * REQUEST_TABLE_WIDTH_PRECISION
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
