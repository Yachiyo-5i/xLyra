import { create } from 'zustand'
import i18n from '@/locales/i18n'
import {
  deleteCurrentAdminSession,
  fetchAuthState,
  type AdminAuthSession,
  type AdminRecord,
  type CurrentAdminSession,
} from '@/features/auth/api/auth'
import { APIError, setCSRFToken as setHTTPCSRFToken } from '@/lib/http'

type AuthUser = {
  id: string
  username: string
  nickname: string
  avatar: string
  displayName: string
  role: string
  status: string
  totpEnabled: boolean
}

type AuthState = {
  status: 'idle' | 'checking' | 'registration-required' | 'unauthenticated' | 'authenticated' | 'error'
  initialized: boolean | null
  user: AuthUser | null
  expiresAt: string | null
  csrfToken: string | null
  errorMessage: string | null
  errorStatus: number | null
  initialize: (force?: boolean) => Promise<void>
  setSession: (session: AdminAuthSession) => void
  setAdmin: (admin: AdminRecord) => void
  setCSRFToken: (csrfToken: string | null) => void
  clearSession: () => void
  logout: () => Promise<void>
}

let initializePromise: Promise<void> | null = null

function formatRole(role?: string) {
  if (!role) {
    return 'Admin'
  }

  return role
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(' ')
}

function buildDisplayName(admin: Pick<AdminRecord, 'id' | 'username' | 'nickname'>) {
  return admin.nickname?.trim() || admin.username?.trim() || `Admin ${admin.id.slice(0, 8)}`
}

function buildUserFromAdmin(admin: AdminRecord): AuthUser {
  return {
    id: admin.id,
    username: admin.username,
    nickname: admin.nickname,
    avatar: admin.avatar,
    displayName: buildDisplayName(admin),
    role: formatRole(admin.role),
    status: admin.status,
    totpEnabled: admin.totpEnabled,
  }
}

function buildUserFromCurrentSession(session: CurrentAdminSession): AuthUser {
  return buildUserFromAdmin(session.admin)
}

function syncCSRFToken(csrfToken: string | null) {
  setHTTPCSRFToken(csrfToken)
  return csrfToken
}

export const useAuthStore = create<AuthState>((set, get) => ({
  status: 'idle',
  initialized: null,
  user: null,
  expiresAt: null,
  csrfToken: null,
  errorMessage: null,
  errorStatus: null,
  initialize: async (force = false) => {
    if (initializePromise && !force) {
      return initializePromise
    }

    initializePromise = (async () => {
      set((state) => ({
        ...state,
        status: 'checking',
        errorMessage: null,
        errorStatus: null,
      }))

      try {
        const authState = await fetchAuthState()

        if (!authState.initialized) {
          set({
            status: 'registration-required',
            initialized: false,
            user: null,
            expiresAt: null,
            csrfToken: syncCSRFToken(null),
            errorMessage: null,
            errorStatus: null,
          })
          return
        }

        if (authState.authenticated && authState.session) {
          set({
            status: 'authenticated',
            initialized: true,
            user: buildUserFromCurrentSession(authState.session),
            expiresAt: authState.session.expiresAt,
            csrfToken: syncCSRFToken(authState.session.csrfToken),
            errorMessage: null,
            errorStatus: null,
          })
          return
        }

        set({
          status: 'unauthenticated',
          initialized: true,
          user: null,
          expiresAt: null,
          csrfToken: syncCSRFToken(null),
          errorMessage: null,
          errorStatus: null,
        })
      } catch (error) {
        set({
          status: 'error',
          initialized: get().initialized,
          errorMessage: error instanceof Error ? error.message : i18n.t('auth.serviceUnavailable', { ns: 'common' }),
          errorStatus: error instanceof APIError ? error.status : null,
        })
      }
    })().finally(() => {
      initializePromise = null
    })

    return initializePromise
  },
  setSession: (session) => {
    set({
      status: 'authenticated',
      initialized: true,
      user: buildUserFromAdmin(session.admin),
      expiresAt: session.expiresAt,
      csrfToken: syncCSRFToken(session.csrfToken),
      errorMessage: null,
      errorStatus: null,
    })
  },
  setAdmin: (admin) => {
    set((state) => ({
      user: buildUserFromAdmin(admin),
      status: state.status === 'authenticated' ? state.status : 'authenticated',
      initialized: true,
      errorMessage: null,
      errorStatus: null,
    }))
  },
  setCSRFToken: (csrfToken) => {
    set({ csrfToken: syncCSRFToken(csrfToken) })
  },
  clearSession: () => {
    set({
      status: 'unauthenticated',
      initialized: true,
      user: null,
      expiresAt: null,
      csrfToken: syncCSRFToken(null),
      errorMessage: null,
      errorStatus: null,
    })
  },
  logout: async () => {
    await deleteCurrentAdminSession()
    get().clearSession()
  },
}))
