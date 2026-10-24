export type RequestTableColumnWidths = readonly number[]

export const REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS: RequestTableColumnWidths = [3, 10, 20, 10, 7, 13, 10, 7, 6, 14]
export const REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS: RequestTableColumnWidths = [3, 7, 10, 7, 6, 0, 8, 6, 6, 7]
export const REQUEST_TABLE_COLUMN_SIZING = {
  storageKey: 'xlyra:requests:table-column-widths:v5',
  defaultWidths: REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS,
  minimumWidths: REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS,
}
