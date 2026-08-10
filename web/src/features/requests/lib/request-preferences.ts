import {
  defaultRequestTableColumnWidths,
  isRequestTableColumnWidths,
  type RequestTableColumnWidths,
} from '@/features/requests/lib/request-table-columns'

const REQUESTS_AUTO_REFRESH_STORAGE_KEY = 'xlyra:requests:auto-refresh'
const REQUESTS_TABLE_COLUMN_WIDTHS_STORAGE_KEY = 'xlyra:requests:table-column-widths:v1'

export function readRequestsAutoRefreshPreference(): boolean {
  try {
    return typeof window !== 'undefined' && window.localStorage.getItem(REQUESTS_AUTO_REFRESH_STORAGE_KEY) === 'true'
  } catch {
    return false
  }
}

export function writeRequestsAutoRefreshPreference(enabled: boolean): void {
  try {
    window.localStorage.setItem(REQUESTS_AUTO_REFRESH_STORAGE_KEY, enabled ? 'true' : 'false')
  } catch {
    // Storage can be disabled or unavailable; the in-memory preference remains authoritative.
  }
}

export function readRequestsTableColumnWidthsPreference(): RequestTableColumnWidths {
  try {
    if (typeof window === 'undefined') return defaultRequestTableColumnWidths()
    const value = window.localStorage.getItem(REQUESTS_TABLE_COLUMN_WIDTHS_STORAGE_KEY)
    if (!value) return defaultRequestTableColumnWidths()

    const parsed: unknown = JSON.parse(value)
    return isRequestTableColumnWidths(parsed) ? [...parsed] : defaultRequestTableColumnWidths()
  } catch {
    return defaultRequestTableColumnWidths()
  }
}

export function writeRequestsTableColumnWidthsPreference(widths: RequestTableColumnWidths): boolean {
  if (!isRequestTableColumnWidths(widths)) return false

  try {
    if (typeof window === 'undefined') return false
    window.localStorage.setItem(REQUESTS_TABLE_COLUMN_WIDTHS_STORAGE_KEY, JSON.stringify(widths))
    return true
  } catch {
    return false
  }
}

export { REQUESTS_AUTO_REFRESH_STORAGE_KEY, REQUESTS_TABLE_COLUMN_WIDTHS_STORAGE_KEY }
