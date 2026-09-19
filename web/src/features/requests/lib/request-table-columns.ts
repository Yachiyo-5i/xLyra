export type RequestTableColumnWidths = readonly number[]

export const REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS: RequestTableColumnWidths = [3, 11, 18, 11, 7, 15, 10, 8, 7, 10]
export const REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS: RequestTableColumnWidths = [3, 7, 10, 7, 6, 0, 8, 6, 6, 7]
export const REQUEST_TABLE_COLUMN_SIZING = {
  storageKey: 'xlyra:requests:table-column-widths:v1',
  defaultWidths: REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS,
  minimumWidths: REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS,
}
