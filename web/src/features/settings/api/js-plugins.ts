import { apiFetch } from '@/lib/http'

export type JSPluginKind =
  | 'quota_probe'
  | 'protocol'
  | 'model_list'
  | 'credential_check'
  | 'site_detect'
  | 'error_classifier'
  | 'model_metadata'
  | 'pricing_parse'
  | string

/** site: bind to sites; endpoint: gets a downstream path; global: applies everywhere. */
export type JSPluginScope = 'site' | 'endpoint' | 'global'

export type JSPluginMetrics24h = {
  calls: number
  errors: number
  error_rate: number
  window_ends?: string
}

export type JSPluginListItem = {
  id: string
  source: string
  enabled_version: string
  version_count: number
  name?: string
  kind?: JSPluginKind
  /** Where the kind takes effect; the server decides, so new kinds need no UI change. */
  scope?: JSPluginScope
  metrics_24h?: JSPluginMetrics24h
}

export type JSPluginTrust = 'trusted' | 'untrusted_signer' | 'unsigned'

export type JSPluginVersion = {
  plugin_id: string
  version: string
  status: string
  package_sha256?: string
  signed?: boolean
  signer?: string
  trust?: JSPluginTrust
  manifest?: Record<string, unknown>
  selftest?: { ok?: boolean; error?: string }
  created_at?: string
}

export type JSPluginTrustedKey = {
  id: string
  name: string
  public_key: string
  fingerprint: string
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
  /** Microseconds; a fixture run is usually well under a millisecond. */
  duration_us?: number
  error?: string
}

export const JS_PLUGIN_MAX_PACKAGE_BYTES = 2 << 20

export const jsPluginQueryKeys = {
  all: ['settings', 'js-plugins'] as const,
  uploaded: () => [...jsPluginQueryKeys.all, 'uploaded'] as const,
  detail: (id: string) => [...jsPluginQueryKeys.all, 'detail', id] as const,
  trustedKeys: () => [...jsPluginQueryKeys.all, 'trusted-keys'] as const,
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

export async function listJSPluginTrustedKeys(signal?: AbortSignal) {
  const result = await apiFetch<{ items: JSPluginTrustedKey[] }>('/api/v1/js-plugin-trusted-keys', { signal })
  return result.items ?? []
}

export async function createJSPluginTrustedKey(body: { name: string; public_key: string }) {
  return apiFetch<JSPluginTrustedKey>('/api/v1/js-plugin-trusted-keys', { method: 'POST', body })
}

export async function deleteJSPluginTrustedKey(id: string) {
  return apiFetch<{ ok: boolean }>(`/api/v1/js-plugin-trusted-keys/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function enableJSPluginVersion(
  pluginId: string,
  version: string,
  options?: { confirm_untrusted?: boolean },
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

export type JSPluginPricingPreview = {
  groups: unknown[] | null
  items: unknown[] | null
}

export async function bindJSPluginSite(
  pluginId: string,
  version: string,
  siteId: string,
  options?: { confirmPricingReviewed?: boolean },
) {
  return apiFetch<{ ok: boolean; kind: string }>(
    `/api/v1/js-plugins/${encodeURIComponent(pluginId)}/versions/${encodeURIComponent(version)}/bind-site`,
    {
      method: 'POST',
      body: { site_id: siteId, confirm_pricing_reviewed: options?.confirmPricingReviewed ?? false },
    },
  )
}

export async function unbindJSPluginSite(pluginId: string, siteId: string, kind: string) {
  return apiFetch<{ ok: boolean }>(`/api/v1/js-plugins/${encodeURIComponent(pluginId)}/unbind-site`, {
    method: 'POST',
    body: { site_id: siteId, kind },
  })
}

export async function previewJSPluginPricing(pluginId: string, siteId: string) {
  return apiFetch<JSPluginPricingPreview>(`/api/v1/js-plugins/${encodeURIComponent(pluginId)}/preview-pricing`, {
    method: 'POST',
    body: { site_id: siteId },
  })
}
