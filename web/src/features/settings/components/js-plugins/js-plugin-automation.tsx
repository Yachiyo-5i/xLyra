import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { LoaderCircle, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { downstreamAPIKeyQueryKeys, listDownstreamAPIKeys } from '@/features/api-keys/api/api-keys'
import { listOAuthConnections, oauthQueryKeys } from '@/features/oauth/api/oauth'
import {
  createJSPluginAutomation,
  deleteJSPluginAutomation,
  jsPluginAutomationQueryKeys,
  listJSPluginActionLog,
  listJSPluginAutomations,
  updateJSPluginAutomation,
  type JSPluginAutomation,
  type JSPluginAutomationManifest,
  type JSPluginConfigProperty,
} from '@/features/settings/api/js-plugins'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'

type ConfigValue = string | number | boolean

type FormState = { editing: string | null; subjectId: string; targetIds: string[]; config: Record<string, ConfigValue> }

function defaultConfig(manifest: JSPluginAutomationManifest): Record<string, ConfigValue> {
  const out: Record<string, ConfigValue> = {}
  for (const [name, property] of Object.entries(manifest.binding.config?.properties ?? {})) {
    if (property.default !== undefined) out[name] = property.default
  }
  return out
}

function ConfigField({
  name,
  property,
  required,
  value,
  onChange,
}: {
  name: string
  property: JSPluginConfigProperty
  required: boolean
  value: ConfigValue | undefined
  onChange: (value: ConfigValue | undefined) => void
}) {
  const label = property.title || name
  if (property.type === 'boolean') {
    return (
      <Checkbox
        checked={value === true}
        onCheckedChange={(checked) => onChange(checked === true)}
        label={label}
        description={property.description}
      />
    )
  }
  if (property.enum && property.enum.length > 0) {
    return (
      <FormField label={label} description={property.description} required={required}>
        <Select value={value === undefined ? undefined : String(value)} onValueChange={(next) => {
          const match = property.enum?.find((option) => String(option) === next)
          onChange(match)
        }}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent searchable={false}>
            {property.enum.map((option) => (
              <SelectItem key={String(option)} value={String(option)}>
                {String(option)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FormField>
    )
  }
  const numeric = property.type === 'number' || property.type === 'integer'
  return (
    <FormField label={label} description={property.description} required={required}>
      <Input
        type={numeric ? 'number' : 'text'}
        min={property.minimum}
        max={property.maximum}
        value={value === undefined ? '' : String(value)}
        onChange={(event) => {
          const raw = event.target.value
          if (raw === '') return onChange(undefined)
          onChange(numeric ? Number(raw) : raw)
        }}
      />
    </FormField>
  )
}

/** The admin-facing half of an automation plugin: which accounts it watches, which keys it may act on, its parameters, and what it has done. */
export function JSPluginAutomationSection({
  pluginId,
  manifest,
}: {
  pluginId: string
  manifest: JSPluginAutomationManifest
}) {
  const { t, i18n } = useTranslation(['settings', 'common'])
  const queryClient = useQueryClient()
  const [form, setForm] = useState<FormState | null>(null)

  const providers = manifest.binding.subject.providers
  const needsTargets = !!manifest.binding.target?.type
  const finiteOnly = manifest.binding.target?.requires === 'finite_total_quota'

  const automationsQuery = useQuery({
    queryKey: jsPluginAutomationQueryKeys.list(pluginId),
    queryFn: ({ signal }) => listJSPluginAutomations(pluginId, signal),
  })
  const logQuery = useQuery({
    queryKey: jsPluginAutomationQueryKeys.log(pluginId),
    queryFn: ({ signal }) => listJSPluginActionLog(pluginId, signal),
    refetchInterval: 15_000,
  })
  const accountsQuery = useQuery({
    queryKey: oauthQueryKeys.connections(),
    queryFn: listOAuthConnections,
    enabled: form != null,
  })
  const keysQuery = useQuery({
    queryKey: downstreamAPIKeyQueryKeys.list(),
    queryFn: listDownstreamAPIKeys,
    enabled: form != null && needsTargets,
  })

  const accounts = useMemo(
    () =>
      (accountsQuery.data?.items ?? []).filter(
        (item) => !providers?.length || providers.includes(item.provider),
      ),
    [accountsQuery.data, providers],
  )
  const keyOptions = useMemo<MultiSelectOption[]>(
    () =>
      (keysQuery.data?.items ?? []).map((key) => {
        const finite = !key.quota_unlimited && key.quota_limit != null
        return {
          value: key.id,
          label: key.name,
          disabled: finiteOnly && !finite,
          description: finiteOnly && !finite ? t('settings:jsPlugins.automation.noFiniteQuota') : undefined,
        }
      }),
    [keysQuery.data, finiteOnly, t],
  )

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: jsPluginAutomationQueryKeys.list(pluginId) })
    void queryClient.invalidateQueries({ queryKey: jsPluginAutomationQueryKeys.log(pluginId) })
  }
  const errorText = (error: unknown) =>
    error instanceof APIError ? error.message : t('settings:jsPlugins.automation.saveFailed')

  const saveMutation = useMutation({
    mutationFn: async (state: FormState) => {
      const input = { subject_id: state.subjectId, target_ids: state.targetIds, config: state.config }
      if (state.editing) await updateJSPluginAutomation(pluginId, state.editing, input)
      else await createJSPluginAutomation(pluginId, input)
    },
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.automation.saved'))
      setForm(null)
      refresh()
    },
    onError: (error: unknown) => toast.error(errorText(error)),
  })
  const deleteMutation = useMutation({
    mutationFn: (bindingId: string) => deleteJSPluginAutomation(pluginId, bindingId),
    onSuccess: () => {
      toast.success(t('settings:jsPlugins.automation.deleted'))
      refresh()
    },
    onError: (error: unknown) => toast.error(errorText(error)),
  })

  const startAdd = () => setForm({ editing: null, subjectId: '', targetIds: [], config: defaultConfig(manifest) })
  const startEdit = (item: JSPluginAutomation) =>
    setForm({
      editing: item.id,
      subjectId: item.subject_id,
      targetIds: item.targets.map((target) => target.id),
      config: { ...item.config },
    })

  const properties = Object.entries(manifest.binding.config?.properties ?? {})
  const required = new Set(manifest.binding.config?.required ?? [])
  const canSave =
    form != null &&
    form.subjectId !== '' &&
    (!needsTargets || form.targetIds.length > 0) &&
    [...required].every((name) => form.config[name] !== undefined) &&
    !saveMutation.isPending
  const formatTime = (value: string) => new Date(value).toLocaleString(i18n.language)
  const items = automationsQuery.data ?? []

  const statusLabel = (status: string) => t(`settings:jsPlugins.automation.log.status.${status}`, status)
  const actionLabel = (entry: { action: { type?: string; scope?: string; message?: string } }) => {
    const type = entry.action?.type ?? ''
    if (type === 'apikey.reset_usage') {
      return t('settings:jsPlugins.automation.log.reset', { scope: entry.action.scope ?? '' })
    }
    if (type === 'notify') return t('settings:jsPlugins.automation.log.notice', { message: entry.action.message ?? '' })
    return t('settings:jsPlugins.automation.log.handle')
  }

  return (
    <section className="space-y-4 border-t border-[hsl(var(--glass-divider))] pt-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h4 className="text-sm font-semibold text-foreground">{t('settings:jsPlugins.automation.title')}</h4>
          <p className="text-xs text-muted-soft">{t('settings:jsPlugins.automation.hint')}</p>
          {manifest.schedule?.everyMinutes ? (
            <p className="text-xs text-muted-soft">
              {t('settings:jsPlugins.automation.schedule', { minutes: manifest.schedule.everyMinutes })}
            </p>
          ) : null}
        </div>
        {form == null ? (
          <Button type="button" size="sm" variant="outline" className="shrink-0" onClick={startAdd}>
            <Plus className="mr-1.5 h-4 w-4" />
            {t('settings:jsPlugins.automation.add')}
          </Button>
        ) : null}
      </div>

      {automationsQuery.isLoading ? (
        <div className="flex items-center gap-2 py-3 text-sm text-muted-soft">
          <LoaderCircle className="h-4 w-4 animate-spin" />
          {t('settings:jsPlugins.loading')}
        </div>
      ) : items.length === 0 && form == null ? (
        <p className="text-sm text-muted-soft">{t('settings:jsPlugins.automation.empty')}</p>
      ) : (
        <ul className="divide-y divide-[hsl(var(--glass-divider))]">
          {items.map((item) => (
            <li key={item.id} className="flex items-start justify-between gap-3 py-3">
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2 text-sm text-foreground">
                  <span className="truncate font-medium">{item.subject_label || item.subject_id}</span>
                  {item.provider ? <StatusBadge status="idle">{item.provider}</StatusBadge> : null}
                </div>
                {item.targets.length > 0 ? (
                  <p className="break-words text-xs text-muted-soft">
                    {t('settings:jsPlugins.automation.targetsLine', { names: item.targets.map((target) => target.name || target.id).join(', ') })}
                  </p>
                ) : null}
                {Object.keys(item.config).length > 0 ? (
                  <p className="break-words font-mono text-xs text-muted-soft">
                    {Object.entries(item.config).map(([key, value]) => `${key}=${String(value)}`).join('  ')}
                  </p>
                ) : null}
              </div>
              <div className="flex shrink-0 gap-1">
                <Button type="button" size="sm" variant="ghost" aria-label={t('settings:jsPlugins.automation.edit')} onClick={() => startEdit(item)}>
                  <Pencil className="h-4 w-4" />
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  aria-label={t('settings:jsPlugins.automation.delete')}
                  disabled={deleteMutation.isPending}
                  onClick={() => deleteMutation.mutate(item.id)}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {form != null ? (
        <div className="space-y-4 rounded-md border border-[hsl(var(--glass-divider))] p-4">
          <FormField label={t('settings:jsPlugins.automation.account')} required>
            <Select
              value={form.subjectId || undefined}
              disabled={form.editing != null}
              onValueChange={(subjectId) => setForm({ ...form, subjectId })}
            >
              <SelectTrigger>
                <SelectValue placeholder={t('settings:jsPlugins.automation.accountPlaceholder')} />
              </SelectTrigger>
              <SelectContent searchable={false}>
                {accounts.map((account) => (
                  <SelectItem key={account.id} value={account.id}>
                    {account.provider} · {account.email || account.account_id || account.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {!accountsQuery.isLoading && accounts.length === 0 ? (
              <p className="text-xs text-muted-soft">{t('settings:jsPlugins.automation.noAccounts')}</p>
            ) : null}
          </FormField>

          {needsTargets ? (
            <FormField
              label={t('settings:jsPlugins.automation.targets')}
              description={finiteOnly ? t('settings:jsPlugins.automation.targetsFiniteHint') : undefined}
              required
            >
              <MultiSelect
                value={form.targetIds}
                options={keyOptions}
                placeholder={t('settings:jsPlugins.automation.targetsPlaceholder')}
                searchPlaceholder={t('settings:jsPlugins.siteBinding.search')}
                emptyText={t('settings:jsPlugins.automation.noTargets')}
                selectedText={t('settings:jsPlugins.automation.targetsUnit')}
                maxVisibleTags={3}
                onChange={(targetIds) => setForm({ ...form, targetIds })}
              />
            </FormField>
          ) : null}

          {properties.map(([name, property]) => (
            <ConfigField
              key={name}
              name={name}
              property={property}
              required={required.has(name)}
              value={form.config[name]}
              onChange={(value) => {
                const next = { ...form.config }
                if (value === undefined) delete next[name]
                else next[name] = value
                setForm({ ...form, config: next })
              }}
            />
          ))}

          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setForm(null)} disabled={saveMutation.isPending}>
              {t('common:actions.cancel')}
            </Button>
            <Button type="button" size="sm" disabled={!canSave} onClick={() => saveMutation.mutate(form)}>
              {saveMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : t('common:actions.save')}
            </Button>
          </div>
        </div>
      ) : null}

      <div className="space-y-2">
        <h5 className="text-xs font-semibold uppercase tracking-wide text-muted-soft">
          {t('settings:jsPlugins.automation.log.title')}
        </h5>
        {(logQuery.data ?? []).length === 0 ? (
          <p className="text-sm text-muted-soft">{t('settings:jsPlugins.automation.log.empty')}</p>
        ) : (
          <ul className="space-y-1.5">
            {(logQuery.data ?? []).slice(0, 10).map((entry) => (
              <li key={entry.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                <span className="tabular-nums text-muted-soft">{formatTime(entry.created_at)}</span>
                <StatusBadge status={entry.status === 'applied' ? 'success' : entry.status === 'failed' ? 'error' : 'idle'}>
                  {statusLabel(entry.status)}
                </StatusBadge>
                <span className="text-foreground">{actionLabel(entry)}</span>
                {entry.detail ? <span className="break-words text-muted-soft">{entry.detail}</span> : null}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}
