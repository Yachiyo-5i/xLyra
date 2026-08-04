import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle, RotateCcw, Save } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/lib/toast'
import {
  fetchRateLimitSettings,
  updateRateLimitSettings,
  type RateLimitConfig,
} from '@/features/settings/api/settings'

const rateLimitSettingsKey = ['settings', 'rate-limits'] as const

export function RateLimitSettingsPage() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const rateLimitsQuery = useQuery({
    queryKey: rateLimitSettingsKey,
    queryFn: fetchRateLimitSettings,
  })
  const saveMutation = useMutation({
    mutationFn: (config: RateLimitConfig) => updateRateLimitSettings({ rate_limit: config }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: rateLimitSettingsKey })
      toast.success(t('rateLimit.saved'))
    },
    onError: (error) => {
      toast.error(t('rateLimit.saveFailed'), { description: error.message })
    },
  })

  if (rateLimitsQuery.isLoading) {
    return <p className="text-muted-soft text-sm">{t('rateLimit.loading')}</p>
  }

  const config = rateLimitsQuery.data?.rate_limit ?? {
    status: 'disabled',
    rpm_limit: null,
    tpm_limit: null,
  }

  return (
    <RateLimitSettingsForm
      key={`${config.status}:${config.rpm_limit ?? ''}:${config.tpm_limit ?? ''}`}
      config={config}
      isSaving={saveMutation.isPending}
      isFetching={rateLimitsQuery.isFetching}
      onSave={(nextConfig) => saveMutation.mutate(nextConfig)}
    />
  )
}

type RateLimitSettingsFormProps = {
  config: RateLimitConfig
  isSaving: boolean
  isFetching: boolean
  onSave: (config: RateLimitConfig) => void
}

function RateLimitSettingsForm({
  config,
  isSaving,
  isFetching,
  onSave,
}: RateLimitSettingsFormProps) {
  const { t } = useTranslation('settings')
  const [enabled, setEnabled] = useState(config.status === 'enabled')
  const [rpmLimit, setRPMLimit] = useState(config.rpm_limit == null ? '' : String(config.rpm_limit))
  const [tpmLimit, setTPMLimit] = useState(config.tpm_limit == null ? '' : String(config.tpm_limit))

  function handleSave() {
    const parsedRPM = parsePositiveIntegerLimit(rpmLimit)
    const parsedTPM = parsePositiveIntegerLimit(tpmLimit)
    if (enabled && !parsedRPM && !parsedTPM) {
      toast.error(t('rateLimit.validation.atLeastOneRequired'))
      return
    }
    if (parsedRPM === false) {
      toast.error(t('rateLimit.validation.rpmPositiveInteger'))
      return
    }
    if (parsedTPM === false) {
      toast.error(t('rateLimit.validation.tpmPositiveInteger'))
      return
    }

    onSave({
      status: enabled ? 'enabled' : 'disabled',
      rpm_limit: parsedRPM || null,
      tpm_limit: parsedTPM || null,
    })
  }

  function handleReset() {
    setEnabled(config.status === 'enabled')
    setRPMLimit(config.rpm_limit == null ? '' : String(config.rpm_limit))
    setTPMLimit(config.tpm_limit == null ? '' : String(config.tpm_limit))
  }

  return (
    <div className="max-w-3xl space-y-6">
      <div className="space-y-1.5">
        <h3 className="text-lg font-semibold text-foreground">{t('rateLimit.title')}</h3>
        <p className="text-muted-soft text-sm">{t('rateLimit.description')}</p>
      </div>

      <div className="pt-2">
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-1">
            <p className="text-sm font-medium text-foreground">{t('rateLimit.rateLimit')}</p>
            <p className="text-muted-soft text-xs leading-5">{t('rateLimit.rateLimitDesc')}</p>
          </div>
          <Switch checked={enabled} onCheckedChange={setEnabled} aria-label={t('rateLimit.rateLimit')} />
        </div>
      </div>

      {enabled ? (
        <div className="grid w-full gap-4 md:grid-cols-2">
          <FormField
            label={t('rateLimit.rpmLimit')}
            description={t('rateLimit.rpmLimitDesc')}
          >
            <Input
              type="text"
              inputMode="numeric"
              value={rpmLimit}
              onChange={(event) => setRPMLimit(event.target.value)}
              placeholder="300"
            />
          </FormField>

          <FormField
            label={t('rateLimit.tpmLimit')}
            description={t('rateLimit.tpmLimitDesc')}
          >
            <Input
              type="text"
              inputMode="numeric"
              value={tpmLimit}
              onChange={(event) => setTPMLimit(event.target.value)}
              placeholder="200000"
            />
          </FormField>
        </div>
      ) : null}

      <div className="flex items-center gap-3">
        <Button onClick={handleSave} disabled={isSaving}>
          {isSaving ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {t('common:actions.save', { ns: 'common' })}
        </Button>
        <Button variant="outline" onClick={handleReset} disabled={isSaving || isFetching}>
          <RotateCcw className="h-4 w-4" />
          {t('common:actions.reset', { ns: 'common' })}
        </Button>
      </div>
    </div>
  )
}

function parsePositiveIntegerLimit(value: string): number | null | false {
  const trimmed = value.trim()
  if (!trimmed) return null
  const parsed = Number(trimmed)
  if (!Number.isInteger(parsed) || parsed <= 0) return false
  return parsed
}
