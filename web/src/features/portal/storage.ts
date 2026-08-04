const STORAGE_KEY = 'xlyra_portal_key'

export function readStoredPortalKey(): string | null {
  try {
    return window.localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

export function writeStoredPortalKey(key: string) {
  try {
    window.localStorage.setItem(STORAGE_KEY, key)
  } catch {
    // ignore storage failures (private mode, quota)
  }
}

export function clearStoredPortalKey() {
  try {
    window.localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}
