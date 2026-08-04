import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Copy, LoaderCircle, RotateCcw, Save } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import {
  fetchPortalSettings,
  updatePortalSettings,
  type PortalDimensions,
  type PortalSettingsConfig,
} from '@/features/settings/api/settings'
import { toast } from '@/lib/toast'

const portalSettingsKey = ['settings', 'portal'] as const

const DEFAULT_PORTAL_SETTINGS: PortalSettingsConfig = {
  enabled: false,
  show_requests: true,
  show_summary: true,
  summary_days: 14,
  request_page_size_max: 50,
  dimensions: {
    site: false,
    model: true,
    tokens: true,
    cost: true,
    latency: true,
    endpoint: true,
    upstream: false,
    error: true,
  },
}

const dimensionKeys: Array<keyof PortalDimensions> = [
  'site',
  'model',
  'tokens',
  'cost',
  'latency',
  'endpoint',
  'upstream',
  'error',
]

export function PortalSettingsPage() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const portalQuery = useQuery({
    queryKey: portalSettingsKey,
    queryFn: fetchPortalSettings,
  })
  const saveMutation = useMutation({
    mutationFn: updatePortalSettings,
    onSuccess: async (config) => {
      await queryClient.invalidateQueries({ queryKey: portalSettingsKey })
      setDraft(config)
      toast.success(t('portal.saved'))
    },
    onError: (error) => {
      toast.error(t('portal.saveFailed'), { description: error.message })
    },
  })
  const [draft, setDraft] = useState<PortalSettingsConfig>(DEFAULT_PORTAL_SETTINGS)

  useEffect(() => {
    if (portalQuery.data) {
      setDraft(portalQuery.data)
    }
  }, [portalQuery.data])

  const summaryDaysError = validateRange(draft.summary_days, 1, 90, t)
  const pageSizeError = validateRange(draft.request_page_size_max, 1, 200, t)
  const portalLink = `${window.location.origin}/portal`

  function patchDraft(patch: Partial<PortalSettingsConfig>) {
    setDraft((current) => ({ ...current, ...patch }))
  }

  function patchDimension(key: keyof PortalDimensions, value: boolean) {
    setDraft((current) => ({ ...current, dimensions: { ...current.dimensions, [key]: value } }))
  }

  function handleSave() {
    if (summaryDaysError || pageSizeError) {
      toast.error(t('portal.validation.invalid'))
      return
    }
    saveMutation.mutate({
      ...draft,
      summary_days: Number(draft.summary_days),
      request_page_size_max: Number(draft.request_page_size_max),
    })
  }

  function handleReset() {
    setDraft(portalQuery.data ?? DEFAULT_PORTAL_SETTINGS)
  }

  if (portalQuery.isLoading) {
    return <p className="text-muted-soft text-sm">{t('portal.loading')}</p>
  }

  return (
    <div className="max-w-4xl space-y-8 lg:h-full lg:min-h-0 lg:overflow-y-auto lg:pr-2">
      <div className="space-y-1.5">
        <h3 className="text-lg font-semibold text-foreground">{t('portal.title')}</h3>
        <p className="text-muted-soft text-sm">{t('portal.description')}</p>
      </div>

      <section className="space-y-4">
        <div className="flex items-center gap-3">
          <h4 className="text-sm font-semibold text-foreground">{t('portal.enabled.title')}</h4>
          <Switch
            checked={draft.enabled}
            onCheckedChange={(enabled) => patchDraft({ enabled })}
            aria-label={t('portal.enabled.title')}
          />
        </div>
        <p className="text-muted-soft text-sm">{t('portal.enabled.desc')}</p>

        {draft.enabled ? (
          <div className="space-y-2 pt-1">
            <p className="text-sm font-medium text-foreground">{t('portal.link.title')}</p>
            <p className="text-muted-soft text-xs">{t('portal.link.desc')}</p>
            <div className="flex items-center gap-2">
              <Input value={portalLink} readOnly spellCheck={false} className="font-mono" onFocus={(event) => event.target.select()} />
              <Button
                type="button"
                variant="outline"
                onClick={() => copyToClipboard(portalLink, t('portal.link.copied'), t('portal.link.copyFailed'))}
              >
                <Copy className="h-4 w-4" />
                {t('portal.link.copy')}
              </Button>
            </div>
          </div>
        ) : null}
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <h4 className="text-sm font-semibold text-foreground">{t('portal.modules.title')}</h4>
        <div className="space-y-3">
          <ToggleRow
            label={t('portal.modules.summary')}
            description={t('portal.modules.summaryDesc')}
            checked={draft.show_summary}
            onChange={(value) => patchDraft({ show_summary: value })}
          />
          <ToggleRow
            label={t('portal.modules.requests')}
            description={t('portal.modules.requestsDesc')}
            checked={draft.show_requests}
            onChange={(value) => patchDraft({ show_requests: value })}
          />
        </div>
        {draft.show_summary || draft.show_requests ? (
          <div className="space-y-4">
            {draft.show_summary ? (
              <FormField
                label={t('portal.modules.summaryDays')}
                description={t('portal.modules.summaryDaysDesc')}
                error={summaryDaysError}
              >
                <Input
                  type="text"
                  inputMode="numeric"
                  value={String(draft.summary_days)}
                  onChange={(event) => patchDraft({ summary_days: toNumber(event.target.value) })}
                />
              </FormField>
            ) : null}
            {draft.show_requests ? (
              <FormField
                label={t('portal.modules.pageSize')}
                description={t('portal.modules.pageSizeDesc')}
                error={pageSizeError}
              >
                <Input
                  type="text"
                  inputMode="numeric"
                  value={String(draft.request_page_size_max)}
                  onChange={(event) => patchDraft({ request_page_size_max: toNumber(event.target.value) })}
                />
              </FormField>
            ) : null}
          </div>
        ) : null}
      </section>

      {draft.show_requests ? (
        <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
          <h4 className="text-sm font-semibold text-foreground">{t('portal.dimensions.title')}</h4>
          <p className="text-muted-soft text-sm">{t('portal.dimensions.desc')}</p>
          <div className="grid gap-3 md:grid-cols-2">
            {dimensionKeys.map((key) => (
              <ToggleRow
                key={key}
                label={t(`portal.dimensions.${key}`)}
                description={t(`portal.dimensions.${key}Desc`)}
                checked={draft.dimensions[key]}
                onChange={(value) => patchDimension(key, value)}
              />
            ))}
          </div>
        </section>
      ) : null}

      <div className="flex items-center gap-3 border-t border-[hsl(var(--divider))] pt-6">
        <Button onClick={handleSave} disabled={saveMutation.isPending}>
          {saveMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {t('common:actions.save', { ns: 'common' })}
        </Button>
        <Button variant="outline" onClick={handleReset} disabled={saveMutation.isPending || portalQuery.isFetching}>
          <RotateCcw className="h-4 w-4" />
          {t('common:actions.reset', { ns: 'common' })}
        </Button>
      </div>
    </div>
  )
}

type ToggleRowProps = {
  label: string
  description: string
  checked: boolean
  onChange: (value: boolean) => void
}

function ToggleRow({ label, description, checked, onChange }: ToggleRowProps) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-md border border-[hsl(var(--divider))] px-4 py-3">
      <div className="min-w-0 space-y-0.5">
        <p className="text-sm font-medium text-foreground">{label}</p>
        <p className="text-muted-soft text-xs">{description}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} aria-label={label} />
    </div>
  )
}

function toNumber(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return 0
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) ? parsed : 0
}

function validateRange(value: number, min: number, max: number, t: (key: string, options?: Record<string, unknown>) => string) {
  if (!Number.isInteger(value) || value < min || value > max) {
    return t('portal.validation.range', { min, max })
  }
  return undefined
}
