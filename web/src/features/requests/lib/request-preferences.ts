const REQUESTS_AUTO_REFRESH_STORAGE_KEY = 'xlyra:requests:auto-refresh'

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

export { REQUESTS_AUTO_REFRESH_STORAGE_KEY }
