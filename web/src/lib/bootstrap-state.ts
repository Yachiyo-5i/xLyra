export const bootstrapInitializedCookie = 'xlyra_admin_initialized'

declare global {
  interface Window {
    __XLYRA_BOOTSTRAP__?: {
      initialized?: boolean
    }
  }
}

export function bootstrapInitializedFromCookie(cookie: string): boolean | null {
  for (const part of cookie.split(';')) {
    const item = part.trim()
    if (item === `${bootstrapInitializedCookie}=1`) return true
    if (item === `${bootstrapInitializedCookie}=0`) return false
  }
  return null
}

export function readBootstrapInitialized(): boolean | null {
  if (typeof document !== 'undefined') {
    const fromCookie = bootstrapInitializedFromCookie(document.cookie)
    if (fromCookie !== null) return fromCookie
  }
  if (typeof window === 'undefined') return null
  const injected = window.__XLYRA_BOOTSTRAP__?.initialized
  if (typeof injected === 'boolean') return injected
  return null
}

export function rememberBootstrapInitialized(initialized: boolean) {
  if (typeof document === 'undefined') return
  const secure = typeof location !== 'undefined' && location.protocol === 'https:' ? '; Secure' : ''
  document.cookie = `${bootstrapInitializedCookie}=${initialized ? '1' : '0'}; Path=/; Max-Age=31536000; SameSite=Lax${secure}`
  if (typeof window !== 'undefined') {
    window.__XLYRA_BOOTSTRAP__ = { initialized }
  }
}
