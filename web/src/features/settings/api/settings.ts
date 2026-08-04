import { apiFetch, apiURL, type DownloadTicket } from '@/lib/http'

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

export async function importBackup(input: { file: File; passphrase: string }) {
  const body = new FormData()
  body.set('file', input.file)
  body.set('passphrase', input.passphrase)
  return apiFetch<{ backup: BackupImportSummary }>('/api/v1/settings/backup/import', {
    method: 'POST',
    body,
  })
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

export async function restoreAutomaticBackupFile(key: string) {
  return apiFetch<{ backup: BackupImportSummary }>('/api/v1/settings/backup/automatic/files/restore', {
    method: 'POST',
    body: { key },
  })
}

export async function deleteAutomaticBackupFile(key: string) {
  return apiFetch<void>('/api/v1/settings/backup/automatic/files', {
    method: 'DELETE',
    body: { key },
  })
}
