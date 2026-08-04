import { useEffect, useMemo, useState } from 'react'
import cronstrue from 'cronstrue/i18n'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle, RotateCcw, Save } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { TextArea } from '@/components/ui/textarea'
import {
  fetchGeneralSettings,
  updateGeneralSettings,
  type GeneralSettingsConfig,
} from '@/features/settings/api/settings'
import { toast } from '@/lib/toast'

const generalSettingsKey = ['settings', 'general'] as const

type GeneralSettingsDraft = {
  siteRefreshCron: string
  newAPICheckinCron: string
  whitelistEnabled: boolean
  whitelistText: string
  logLevel: string
  cleanupEnabled: boolean
  retentionDays: string
  requestDetailCleanupEnabled: boolean
  requestDetailRetentionDays: string
  sessionLifetimeHours: string
}

const DEFAULT_GENERAL_SETTINGS: GeneralSettingsDraft = {
  siteRefreshCron: '0 */15 * * *',
  newAPICheckinCron: '0 9 * * *',
  whitelistEnabled: false,
  whitelistText: '',
  logLevel: 'info',
  cleanupEnabled: true,
  retentionDays: '30',
  requestDetailCleanupEnabled: true,
  requestDetailRetentionDays: '90',
  sessionLifetimeHours: '24',
}

const logLevels = ['debug', 'info', 'warn', 'error'] as const

export function GeneralSettingsPage() {
  const { t, i18n } = useTranslation('settings')
  const queryClient = useQueryClient()
  const generalQuery = useQuery({
    queryKey: generalSettingsKey,
    queryFn: fetchGeneralSettings,
  })
  const saveMutation = useMutation({
    mutationFn: updateGeneralSettings,
    onSuccess: async (config) => {
      await queryClient.invalidateQueries({ queryKey: generalSettingsKey })
      setDraft(generalSettingsToDraft(config))
      toast.success(t('general.saved'))
    },
    onError: (error) => {
      toast.error(t('general.saveFailed'), { description: error.message })
    },
  })
  const [draft, setDraft] = useState<GeneralSettingsDraft>(DEFAULT_GENERAL_SETTINGS)

  useEffect(() => {
    if (generalQuery.data) {
      setDraft(generalSettingsToDraft(generalQuery.data))
    }
  }, [generalQuery.data])

  const cronLocale = i18n.language?.startsWith('zh') ? 'zh_CN' : i18n.language?.startsWith('jp') || i18n.language?.startsWith('ja') ? 'ja' : 'en'
  const siteRefreshCron = useMemo(() => describeCron(draft.siteRefreshCron, cronLocale, t), [draft.siteRefreshCron, cronLocale, t])
  const newAPICheckinCron = useMemo(() => describeCron(draft.newAPICheckinCron, cronLocale, t), [draft.newAPICheckinCron, cronLocale, t])
  const whitelistSummary = useMemo(() => summarizeWhitelist(draft.whitelistText, t), [draft.whitelistText, t])
  const retentionError = draft.cleanupEnabled ? validateRetentionDays(draft.retentionDays, t) : undefined
  const requestDetailRetentionError = draft.requestDetailCleanupEnabled ? validateRetentionDays(draft.requestDetailRetentionDays, t) : undefined
  const sessionLifetimeError = validateSessionLifetimeHours(draft.sessionLifetimeHours, t)

  function patchDraft(patch: Partial<GeneralSettingsDraft>) {
    setDraft((current) => ({ ...current, ...patch }))
  }

  function handleSave() {
    if (!siteRefreshCron.valid) {
      toast.error(t('general.validation.siteRefreshCronInvalid'), { description: siteRefreshCron.text })
      return
    }
    if (!newAPICheckinCron.valid) {
      toast.error(t('general.validation.newAPICheckinCronInvalid'), { description: newAPICheckinCron.text })
      return
    }
    if (whitelistSummary.invalid.length) {
      toast.error(t('general.validation.whitelistIPInvalid'), { description: whitelistSummary.invalid.join('、') })
      return
    }
    if (retentionError) {
      toast.error(t('general.validation.retentionConfigInvalid'), { description: retentionError })
      return
    }
    if (requestDetailRetentionError) {
      toast.error(t('general.validation.requestDetailRetentionConfigInvalid'), { description: requestDetailRetentionError })
      return
    }
    if (sessionLifetimeError) {
      toast.error(t('general.validation.sessionLifetimeInvalid'), { description: sessionLifetimeError })
      return
    }
    saveMutation.mutate(draftToGeneralSettings(draft))
  }

  function handleReset() {
    setDraft(generalQuery.data ? generalSettingsToDraft(generalQuery.data) : DEFAULT_GENERAL_SETTINGS)
  }

  if (generalQuery.isLoading) {
    return <p className="text-muted-soft text-sm">{t('general.loading')}</p>
  }

  return (
    <div className="max-w-4xl space-y-8 lg:h-full lg:min-h-0 lg:overflow-y-auto lg:pr-2">
      <div className="space-y-1.5">
        <h3 className="text-lg font-semibold text-foreground">{t('general.title')}</h3>
        <p className="text-muted-soft text-sm">{t('general.description')}</p>
      </div>

      <section className="space-y-4">
        <h4 className="text-sm font-semibold text-foreground">{t('general.security.title')}</h4>

        <div className="grid gap-4 md:grid-cols-2">
          <FormField
            label={t('general.security.sessionLifetime')}
            description={t('general.security.sessionLifetimeDesc')}
            error={sessionLifetimeError}
          >
            <Input
              type="text"
              inputMode="numeric"
              value={draft.sessionLifetimeHours}
              onChange={(event) => patchDraft({ sessionLifetimeHours: event.target.value })}
            />
          </FormField>
        </div>
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <div className="flex items-center gap-2">
          <h4 className="text-sm font-semibold text-foreground">{t('general.tasks.title')}</h4>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <FormField
            label={t('general.tasks.siteRefreshCron')}
            description={siteRefreshCron.valid ? siteRefreshCron.text : undefined}
            error={siteRefreshCron.valid ? undefined : siteRefreshCron.text}
          >
            <Input
              value={draft.siteRefreshCron}
              onChange={(event) => patchDraft({ siteRefreshCron: event.target.value })}
              spellCheck={false}
              className="font-mono"
            />
          </FormField>

          <FormField
            label={t('general.tasks.newAPICheckinCron')}
            description={newAPICheckinCron.valid ? newAPICheckinCron.text : undefined}
            error={newAPICheckinCron.valid ? undefined : newAPICheckinCron.text}
          >
            <Input
              value={draft.newAPICheckinCron}
              onChange={(event) => patchDraft({ newAPICheckinCron: event.target.value })}
              spellCheck={false}
              className="font-mono"
            />
          </FormField>
        </div>
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <div className="flex items-center gap-3">
          <h4 className="text-sm font-semibold text-foreground">{t('general.whitelist.title')}</h4>
          <Switch
            checked={draft.whitelistEnabled}
            onCheckedChange={(enabled) => patchDraft({ whitelistEnabled: enabled })}
            aria-label={t('general.whitelist.title')}
          />
        </div>

        <FormField
          label={t('general.whitelist.ipAddress')}
          description={whitelistSummary.text}
          error={whitelistSummary.invalid.length ? `${t('general.whitelist.invalidAddress')}：${whitelistSummary.invalid.join('、')}` : undefined}
        >
          <TextArea
            value={draft.whitelistText}
            onChange={(event) => patchDraft({ whitelistText: event.target.value })}
            disabled={!draft.whitelistEnabled}
            spellCheck={false}
            className="min-h-[132px] font-mono"
          />
        </FormField>
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <h4 className="text-sm font-semibold text-foreground">{t('general.log.title')}</h4>

        <FormField label={t('general.log.level')}>
          <Select value={draft.logLevel} onValueChange={(logLevel) => patchDraft({ logLevel })}>
            <SelectTrigger className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent searchable={false}>
              {logLevels.map((level) => (
                <SelectItem key={level} value={level}>
                  {level.toUpperCase()}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FormField>

        <div className="grid gap-4 md:grid-cols-2">
          <FormField label={t('general.log.autoCleanup')}>
            <div className="flex h-11 items-center gap-3">
              <Switch
                checked={draft.cleanupEnabled}
                onCheckedChange={(cleanupEnabled) => patchDraft({ cleanupEnabled })}
                aria-label={t('general.log.autoCleanup')}
              />
            </div>
          </FormField>

          {draft.cleanupEnabled ? (
            <FormField
              label={t('general.log.retentionDays')}
              error={retentionError}
            >
              <Input
                type="text"
                inputMode="numeric"
                value={draft.retentionDays}
                onChange={(event) => patchDraft({ retentionDays: event.target.value })}
              />
            </FormField>
          ) : null}
        </div>
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <h4 className="text-sm font-semibold text-foreground">{t('general.data.title')}</h4>

        <div className="grid gap-4 md:grid-cols-2">
          <FormField label={t('general.data.requestDetailAutoCleanup')}>
            <div className="flex h-11 items-center gap-3">
              <Switch
                checked={draft.requestDetailCleanupEnabled}
                onCheckedChange={(requestDetailCleanupEnabled) => patchDraft({ requestDetailCleanupEnabled })}
                aria-label={t('general.data.requestDetailAutoCleanup')}
              />
            </div>
          </FormField>

          {draft.requestDetailCleanupEnabled ? (
            <FormField
              label={t('general.data.requestDetailRetentionDays')}
              description={t('general.data.requestDetailRetentionDesc')}
              error={requestDetailRetentionError}
            >
              <Input
                type="text"
                inputMode="numeric"
                value={draft.requestDetailRetentionDays}
                onChange={(event) => patchDraft({ requestDetailRetentionDays: event.target.value })}
              />
            </FormField>
          ) : null}
        </div>
      </section>

      <div className="flex items-center gap-3 border-t border-[hsl(var(--divider))] pt-6">
        <Button onClick={handleSave} disabled={saveMutation.isPending}>
          {saveMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {t('common:actions.save', { ns: 'common' })}
        </Button>
        <Button variant="outline" onClick={handleReset} disabled={saveMutation.isPending || generalQuery.isFetching}>
          <RotateCcw className="h-4 w-4" />
          {t('common:actions.reset', { ns: 'common' })}
        </Button>
      </div>
    </div>
  )
}

function generalSettingsToDraft(config: GeneralSettingsConfig): GeneralSettingsDraft {
  return {
    siteRefreshCron: config.tasks.site_refresh_cron,
    newAPICheckinCron: config.tasks.newapi_checkin_cron,
    whitelistEnabled: config.ip_whitelist.enabled,
    whitelistText: config.ip_whitelist.entries.join('\n'),
    logLevel: config.log.level,
    cleanupEnabled: config.log.cleanup_enabled,
    retentionDays: String(config.log.retention_days),
    requestDetailCleanupEnabled: config.data?.request_detail_cleanup_enabled ?? true,
    requestDetailRetentionDays: String(config.data?.request_detail_retention_days ?? 90),
    sessionLifetimeHours: String(config.security?.session_lifetime_hours ?? 24),
  }
}

function draftToGeneralSettings(draft: GeneralSettingsDraft): GeneralSettingsConfig {
  return {
    tasks: {
      site_refresh_cron: draft.siteRefreshCron.trim(),
      newapi_checkin_cron: draft.newAPICheckinCron.trim(),
    },
    ip_whitelist: {
      enabled: draft.whitelistEnabled,
      entries: draft.whitelistText.split(/\r?\n/).map((line) => line.trim()).filter(Boolean),
    },
    log: {
      level: draft.logLevel,
      cleanup_enabled: draft.cleanupEnabled,
      retention_days: Number(draft.retentionDays.trim()),
    },
    data: {
      request_detail_cleanup_enabled: draft.requestDetailCleanupEnabled,
      request_detail_retention_days: Number(draft.requestDetailRetentionDays.trim()),
    },
    security: {
      session_lifetime_hours: Number(draft.sessionLifetimeHours.trim()),
    },
  }
}

type CronDescription = {
  valid: boolean
  text: string
}

function describeCron(value: string, locale: string, t: (key: string, options?: Record<string, unknown>) => string): CronDescription {
  const normalized = value.trim().replace(/\s+/g, ' ')
  if (!normalized) {
    return { valid: false, text: t('general.validation.cronRequired') }
  }

  const cronLocale = locale === 'zh_CN' || locale === 'ja' ? locale : 'en'

  try {
    return {
      valid: true,
      text: cronstrue.toString(normalized, {
        locale: cronLocale,
        use24HourTimeFormat: true,
        throwExceptionOnParseError: true,
      }),
    }
  } catch (error) {
    return {
      valid: false,
      text: error instanceof Error ? t('general.validation.cronInvalidWithMessage', { message: error.message }) : t('general.validation.cronInvalid'),
    }
  }
}

function summarizeWhitelist(value: string, t: (key: string, options?: Record<string, unknown>) => string) {
  const lines = value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
  const invalid = lines.filter((line) => !isIPAddressOrCIDR(line))

  if (!lines.length) {
    return { text: t('general.whitelist.ipAddressDesc'), invalid }
  }

  return {
    text: t('general.whitelist.summary', { total: lines.length, valid: lines.length - invalid.length }),
    invalid,
  }
}

function isIPAddressOrCIDR(value: string) {
  const [address, prefix] = value.split('/')
  if (!isIPv4(address) && !isIPv6(address)) return false
  if (prefix === undefined) return true
  if (!/^\d+$/.test(prefix)) return false
  const parsedPrefix = Number(prefix)
  return isIPv4(address)
    ? parsedPrefix >= 0 && parsedPrefix <= 32
    : parsedPrefix >= 0 && parsedPrefix <= 128
}

function isIPv4(value: string) {
  const parts = value.split('.')
  if (parts.length !== 4) return false
  return parts.every((part) => {
    if (!/^\d+$/.test(part)) return false
    const parsed = Number(part)
    return parsed >= 0 && parsed <= 255
  })
}

function isIPv6(value: string) {
  return value.includes(':') && /^[0-9a-fA-F:]+$/.test(value)
}

function validateRetentionDays(value: string, t: (key: string) => string) {
  const trimmed = value.trim()
  if (!trimmed) return t('general.validation.retentionDaysRequired')
  const parsed = Number(trimmed)
  if (!Number.isInteger(parsed) || parsed <= 0) return t('general.validation.retentionDaysPositive')
  return undefined
}

function validateSessionLifetimeHours(value: string, t: (key: string) => string) {
  const trimmed = value.trim()
  if (!trimmed) return t('general.validation.sessionLifetimeRequired')
  const parsed = Number(trimmed)
  if (!Number.isInteger(parsed)) return t('general.validation.sessionLifetimeInteger')
  if (parsed < 0 || parsed > 720) return t('general.validation.sessionLifetimeRange')
  return undefined
}
