import { apiFetch } from '@/lib/http'

export type JSPluginKind = 'quota_probe' | 'protocol' | string

export type JSPluginMetrics24h = {
  calls: number
  errors: number
  error_rate: number
  window_ends?: string
}

export type JSPluginBuiltin = {
  id: string
  name: string
  description?: string
  version: string
  kind: JSPluginKind
  source: 'builtin'
  replaces?: string
  protocol?: string
}

export type JSPluginListItem = {
  id: string
  source: string
  enabled_version: string
  version_count: number
  name?: string
  kind?: JSPluginKind
  metrics_24h?: JSPluginMetrics24h
}

export type JSPluginVersion = {
  plugin_id: string
  version: string
  status: string
  package_sha256?: string
  signed?: boolean
  signer?: string
  manifest?: Record<string, unknown>
  selftest?: { ok?: boolean; error?: string }
  created_at?: string
}

export type JSPluginDetail = {
  id: string
  versions: JSPluginVersion[]
}

export type JSPluginTryResult = {
  plugin_id: string
  version: string
  ok: boolean
  duration_ms?: number
  error?: string
}

export const jsPluginQueryKeys = {
  all: ['settings', 'js-plugins'] as const,
  builtins: () => [...jsPluginQueryKeys.all, 'builtins'] as const,
  uploaded: () => [...jsPluginQueryKeys.all, 'uploaded'] as const,
  detail: (id: string) => [...jsPluginQueryKeys.all, 'detail', id] as const,
}

export async function listBuiltinJSPlugins(signal?: AbortSignal) {
  const result = await apiFetch<{ items: JSPluginBuiltin[] }>('/api/v1/js-plugins/builtins', { signal })
  return result.items ?? []
}

export async function listUploadedJSPlugins(signal?: AbortSignal) {
  const result = await apiFetch<{ items: JSPluginListItem[] }>('/api/v1/js-plugins', { signal })
  return result.items ?? []
}

export async function getJSPlugin(id: string, signal?: AbortSignal) {
  return apiFetch<JSPluginDetail>(`/api/v1/js-plugins/${encodeURIComponent(id)}`, { signal })
}

export async function uploadJSPlugin(file: File) {
  return apiFetch<JSPluginVersion>('/api/v1/js-plugins', {
    method: 'POST',
    body: file,
    headers: {
      'Content-Type': 'application/octet-stream',
    },
  })
}

export async function enableJSPluginVersion(
  pluginId: string,
  version: string,
  options?: { confirm_unsigned?: boolean },
) {
  return apiFetch<{ ok: boolean; generation?: number }>(
    `/api/v1/js-plugins/${encodeURIComponent(pluginId)}/versions/${encodeURIComponent(version)}/enable`,
    { method: 'POST', body: options ?? {} },
  )
}

export async function disableJSPlugin(pluginId: string) {
  return apiFetch<{ ok: boolean; generation?: number }>(
    `/api/v1/js-plugins/${encodeURIComponent(pluginId)}/disable`,
    { method: 'POST' },
  )
}

export async function deleteJSPluginVersion(pluginId: string, version: string) {
  return apiFetch<{ ok: boolean }>(
    `/api/v1/js-plugins/${encodeURIComponent(pluginId)}/versions/${encodeURIComponent(version)}`,
    { method: 'DELETE' },
  )
}

export async function tryJSPlugin(pluginId: string, version?: string) {
  return apiFetch<JSPluginTryResult>(`/api/v1/js-plugins/${encodeURIComponent(pluginId)}/try`, {
    method: 'POST',
    body: version ? { version } : {},
  })
}

export async function bindJSPluginProtocolSlug(pluginId: string, version: string, slug: string) {
  return apiFetch<{ ok: boolean }>(
    `/api/v1/js-plugins/${encodeURIComponent(pluginId)}/versions/${encodeURIComponent(version)}/bind-protocol`,
    { method: 'POST', body: { slug } },
  )
}

export async function bindJSPluginSiteQuotaProbe(pluginId: string, version: string, siteId: string) {
  return apiFetch<{ ok: boolean; quota_probe?: string }>(
    `/api/v1/js-plugins/${encodeURIComponent(pluginId)}/versions/${encodeURIComponent(version)}/bind-quota-probe`,
    { method: 'POST', body: { site_id: siteId } },
  )
}
