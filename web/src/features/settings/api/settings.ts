import { apiFetch, apiFetchResponse, apiURL, type DownloadTicket } from '@/lib/http'

export type ProxyConfig = {
  id: string
  name: string
  url: string
  type: string
}

export type SystemProxyConfig = {
  proxies: ProxyConfig[]
}

export type SystemProxyTestResult = {
  ok: boolean
  stage: string
  latency_ms: number
  message: string
}

export type GeneralSettingsConfig = {
  tasks: {
    site_refresh_cron: string
    newapi_checkin_cron: string
  }
  ip_whitelist: {
    enabled: boolean
    entries: string[]
  }
  log: {
    level: string
    cleanup_enabled: boolean
    retention_days: number
  }
  data: {
    request_detail_cleanup_enabled: boolean
    request_detail_retention_days: number
  }
  security: {
    session_lifetime_hours: number
  }
}

type RateLimitStatus = 'enabled' | 'disabled'

export type RateLimitConfig = {
  status: RateLimitStatus
  rpm_limit?: number | null
  tpm_limit?: number | null
}

export type RateLimitSettings = {
  rate_limit: RateLimitConfig
}

export async function fetchSystemProxyConfig() {
  return apiFetch<SystemProxyConfig>('/api/v1/settings/system-proxy')
}

export async function updateSystemProxyConfig(config: SystemProxyConfig) {
  return apiFetch<SystemProxyConfig>('/api/v1/settings/system-proxy', {
    method: 'PUT',
    body: config,
  })
}

export async function testSystemProxy(proxy: ProxyConfig) {
  return apiFetch<SystemProxyTestResult>('/api/v1/settings/system-proxy/test', {
    method: 'POST',
    body: { proxy },
  })
}


export async function fetchGeneralSettings() {
  return apiFetch<GeneralSettingsConfig>('/api/v1/settings/general')
}

export async function updateGeneralSettings(config: GeneralSettingsConfig) {
  return apiFetch<GeneralSettingsConfig>('/api/v1/settings/general', {
    method: 'PUT',
    body: config,
  })
}

export type PortalDimensions = {
  site: boolean
  model: boolean
  tokens: boolean
  cost: boolean
  latency: boolean
  endpoint: boolean
  upstream: boolean
  error: boolean
}

export type PortalSettingsConfig = {
  enabled: boolean
  show_requests: boolean
  show_summary: boolean
  summary_days: number
  request_page_size_max: number
  dimensions: PortalDimensions
}

export async function fetchPortalSettings() {
  return apiFetch<PortalSettingsConfig>('/api/v1/settings/portal')
}

export async function updatePortalSettings(config: PortalSettingsConfig) {
  return apiFetch<PortalSettingsConfig>('/api/v1/settings/portal', {
    method: 'PUT',
    body: config,
  })
}

export async function fetchRateLimitSettings() {
  return apiFetch<RateLimitSettings>('/api/v1/settings/rate-limits')
}

export async function updateRateLimitSettings(settings: RateLimitSettings) {
  return apiFetch<RateLimitSettings>('/api/v1/settings/rate-limits', {
    method: 'PUT',
    body: settings,
  })
}

export type BackupImportSummary = {
  tables: number
  rows: number
  config_keys: number
  format_version: number
}

export type AutomaticBackupConfig = {
  enabled: boolean
  cron: string
  retention_count: number
  endpoint: string
  region: string
  bucket: string
  prefix: string
  access_key: string
  secret_key_masked?: string
  backup_passphrase_masked?: string
  has_secret_key: boolean
  has_backup_passphrase: boolean
  force_path_style: boolean
  use_ssl: boolean
  skip_tls_verify: boolean
  ready: boolean
}

export type AutomaticBackupConfigInput = {
  enabled: boolean
  cron: string
  retention_count: number
  endpoint: string
  region: string
  bucket: string
  prefix: string
  access_key: string
  secret_key: string
  backup_passphrase: string
  force_path_style: boolean
  use_ssl: boolean
  skip_tls_verify: boolean
}

export type AutomaticBackupTestResult = {
  ok: boolean
  stage: string
  latency_ms: number
  message: string
}

export type AutomaticBackupFile = {
  key: string
  filename: string
  size: number
  last_modified: string
  status?: 'running' | 'failed'
  error?: string
}

export type AutomaticBackupRunTask = {
  key: string
  filename: string
  status: 'running'
  started_at: string
}

export async function exportBackup(passphrase: string) {
  const result = await apiFetch<{ download: DownloadTicket }>('/api/v1/settings/backup/export', {
    method: 'POST',
    body: { passphrase },
  })
  return result.download
}

export function backupDownloadURL(ticket: DownloadTicket) {
  return apiURL(ticket.url)
}

export async function fetchAutomaticBackupConfig() {
  return apiFetch<{ automatic_backup: AutomaticBackupConfig }>('/api/v1/settings/backup/automatic')
}

export async function updateAutomaticBackupConfig(input: AutomaticBackupConfigInput) {
  return apiFetch<{ automatic_backup: AutomaticBackupConfig }>('/api/v1/settings/backup/automatic', {
    method: 'PUT',
    body: input,
  })
}

export async function testAutomaticBackupConfig() {
  return apiFetch<{ test: AutomaticBackupTestResult }>('/api/v1/settings/backup/automatic/test', {
    method: 'POST',
  })
}

export async function listAutomaticBackupFiles() {
  return apiFetch<{ files: AutomaticBackupFile[] }>('/api/v1/settings/backup/automatic/files')
}

export async function runAutomaticBackup() {
  return apiFetch<{ backup: AutomaticBackupRunTask }>('/api/v1/settings/backup/automatic/run', {
    method: 'POST',
  })
}

export type RestoreProgressEvent = {
  step: 'download' | 'decrypt' | 'parse' | 'import' | 'complete'
  status: 'in_progress' | 'complete' | 'error'
  rows?: number
  total_rows?: number
  table?: string
  bytes?: number
  total_bytes?: number
  summary?: BackupImportSummary
  message?: string
}

export async function* restoreAutomaticBackupFileSSE(key: string, signal?: AbortSignal): AsyncGenerator<RestoreProgressEvent> {
  const response = await apiFetchResponse('/api/v1/settings/backup/automatic/files/restore', {
    method: 'POST',
    headers: { Accept: 'text/event-stream' },
    body: { key },
    signal,
  })
  for await (const event of readRestoreProgress(response)) {
    yield event
  }
}

export async function* importBackupSSE(input: { file: File; passphrase: string }, signal?: AbortSignal): AsyncGenerator<RestoreProgressEvent> {
  const body = new FormData()
  body.set('file', input.file)
  body.set('passphrase', input.passphrase)

  const response = await apiFetchResponse('/api/v1/settings/backup/import', {
    method: 'POST',
    headers: { Accept: 'text/event-stream' },
    body,
    signal,
  })
  for await (const event of readRestoreProgress(response)) {
    yield event
  }
}

async function* readRestoreProgress(response: Response): AsyncGenerator<RestoreProgressEvent> {
  if (!response.body) {
    throw new Error('restore response stream is unavailable')
  }
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let terminal = false

  try {
    while (true) {
      const { done, value } = await reader.read()
      buffer += decoder.decode(value, { stream: !done }).replaceAll('\r\n', '\n')
      const parts = buffer.split('\n\n')
      buffer = parts.pop() ?? ''
      if (done && buffer.trim()) {
        parts.push(buffer)
        buffer = ''
      }
      for (const part of parts) {
        const data = part
          .split('\n')
          .filter((line) => line.startsWith('data:'))
          .map((line) => line.slice(5).trimStart())
          .join('\n')
        if (!data) continue
        const event = JSON.parse(data) as RestoreProgressEvent
        if (event.step === 'complete' || event.status === 'error') {
          terminal = true
        }
        yield event
      }
      if (done) break
    }
  } finally {
    reader.releaseLock()
  }
  if (!terminal) {
    throw new Error('restore response stream ended unexpectedly')
  }
}

export async function deleteAutomaticBackupFile(key: string) {
  return apiFetch<void>('/api/v1/settings/backup/automatic/files', {
    method: 'DELETE',
    body: { key },
  })
}
