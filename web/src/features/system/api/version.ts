import { apiFetch } from '@/lib/http'

export type SystemVersion = {
  version: string
  commit?: string
  buildTime?: string
}

type SystemVersionResponse = {
  version: string
  commit?: string
  build_time?: string
}

export function fetchSystemVersion() {
  return apiFetch<SystemVersionResponse>('/api/v1/system/version', { auth: 'none' }).then((response) => ({
    version: response.version || 'dev',
    commit: response.commit,
    buildTime: response.build_time,
  }) satisfies SystemVersion)
}
