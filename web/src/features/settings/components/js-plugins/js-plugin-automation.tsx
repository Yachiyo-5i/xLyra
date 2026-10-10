import { useState } from 'react'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { LoaderCircle, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { FormField } from '@/components/ui/form-field'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  createJSPluginAutomation,
  deleteJSPluginAutomation,
  jsPluginAutomationQueryKeys,
  listJSPluginActionLog,
  listJSPluginAutomationOptions,
  listJSPluginAutomations,
  updateJSPluginAutomation,
  type JSPluginAutomation,
  type JSPluginAutomationInput,
  type JSPluginAutomationManifest,
  type JSPluginAutomationOption,
  type JSPluginAutomationValue,
} from '@/features/settings/api/js-plugins'
import { resolveText } from '@/features/settings/lib/localized-text'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'

type FormState = { editing: string | null; values: Record<string, JSPluginAutomationValue | undefined> }

const isObjectInput = (input: JSPluginAutomationInput) => input.type === 'oauth_connection' || input.type === 'api_key'
const isRequired = (input: JSPluginAutomationInput) => input.required ?? isObjectInput(input)

function initialValues(manifest: JSPluginAutomationManifest, item?: JSPluginAutomation): FormState['values'] {
  const values: FormState['values'] = {}
  for (const input of manifest.inputs) {
    if (item) {
      const saved = item.inputs.find((candidate) => candidate.name === input.name)
      if (isObjectInput(input)) {
        const ids = (saved?.entities ?? []).map((entity) => entity.id)
        values[input.name] = input.multiple ? ids : ids[0]
      } else {
        values[input.name] = saved?.value ?? input.default
      }
    } else if (isObjectInput(input)) {
      values[input.name] = input.multiple ? [] : undefined
    } else {
      values[input.name] = input.default
    }
  }
  return values
}

function filled(value: JSPluginAutomationValue | undefined) {
  if (value === undefined || value === '') return false
  return Array.isArray(value) ? value.length > 0 : true
}

/** The admin-facing half of an automation plugin: a form drawn from the inputs the plugin declares, the bindings made with it, and what it has done. */
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
  const text = (value: Parameters<typeof resolveText>[0]) => resolveText(value, i18n.language)

  const objectInputs = manifest.inputs.filter(isObjectInput)
  const subjectInput = manifest.inputs.find((input) => input.eventSubject) ?? objectInputs[0]

  const automationsQuery = useQuery({
    queryKey: jsPluginAutomationQueryKeys.list(pluginId),
    queryFn: ({ signal }) => listJSPluginAutomations(pluginId, signal),
  })
  const logQuery = useQuery({
    queryKey: jsPluginAutomationQueryKeys.log(pluginId),
    queryFn: ({ signal }) => listJSPluginActionLog(pluginId, signal),
    refetchInterval: 15_000,
  })
  // Every object input is offered the objects xLyra lists for it; the server
  // rules out the ones the plugin's conditions exclude and says why.
  const optionQueries = useQueries({
    queries: objectInputs.map((input) => ({
      queryKey: jsPluginAutomationQueryKeys.options(pluginId, input.name),
      queryFn: ({ signal }: { signal: AbortSignal }) => listJSPluginAutomationOptions(pluginId, input.name, signal),
      enabled: form != null,
    })),
  })
  const optionsByInput: Record<string, { options: JSPluginAutomationOption[]; loading: boolean }> = {}
  objectInputs.forEach((input, index) => {
    optionsByInput[input.name] = { options: optionQueries[index]?.data ?? [], loading: optionQueries[index]?.isLoading ?? false }
  })

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: jsPluginAutomationQueryKeys.list(pluginId) })
    void queryClient.invalidateQueries({ queryKey: jsPluginAutomationQueryKeys.log(pluginId) })
  }
  const errorText = (error: unknown) =>
    error instanceof APIError ? error.message : t('settings:jsPlugins.automation.saveFailed')

  const saveMutation = useMutation({
    mutationFn: async (state: FormState) => {
      const inputs: Record<string, JSPluginAutomationValue> = {}
      for (const [name, value] of Object.entries(state.values)) {
        if (value !== undefined && value !== '') inputs[name] = value
      }
      if (state.editing) await updateJSPluginAutomation(pluginId, state.editing, inputs)
      else await createJSPluginAutomation(pluginId, inputs)
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

  const canSave =
    form != null && manifest.inputs.every((input) => !isRequired(input) || filled(form.values[input.name])) && !saveMutation.isPending
  const formatTime = (value: string) => new Date(value).toLocaleString(i18n.language)
  const items = automationsQuery.data ?? []

  const title = text(manifest.form?.title) ?? t('settings:jsPlugins.automation.title')
  const description = text(manifest.form?.description) ?? t('settings:jsPlugins.automation.hint')
  const addLabel = text(manifest.form?.addLabel) ?? t('settings:jsPlugins.automation.add')
  const emptyText = text(manifest.form?.emptyText) ?? t('settings:jsPlugins.automation.empty')
  const inputTitle = (input: JSPluginAutomationInput) => text(input.title) ?? input.name
  const reasonText = (option: JSPluginAutomationOption) =>
    option.reason_code
      ? t(`settings:jsPlugins.automation.reason.${option.reason_code}`, option.reason ?? option.reason_code)
      : option.reason

  const setValue = (name: string, value: JSPluginAutomationValue | undefined) =>
    setForm((current) => (current ? { ...current, values: { ...current.values, [name]: value } } : current))

  const renderInput = (input: JSPluginAutomationInput, state: FormState) => {
    const label = inputTitle(input)
    const hint = text(input.description)
    const value = state.values[input.name]

    if (isObjectInput(input)) {
      const { options, loading } = optionsByInput[input.name] ?? { options: [], loading: false }
      const emptyOptions = text(input.emptyText) ?? t('settings:jsPlugins.automation.noOptions')
      const placeholder = text(input.placeholder) ?? t('settings:jsPlugins.automation.selectPlaceholder')
      return (
        <FormField key={input.name} label={label} description={hint} required={isRequired(input)}>
          {input.multiple ? (
            <MultiSelect
              value={Array.isArray(value) ? value : []}
              options={options.map<MultiSelectOption>((option) => ({
                value: option.id,
                label: option.name,
                disabled: option.disabled,
                description: option.disabled ? reasonText(option) : option.description,
              }))}
              placeholder={placeholder}
              searchPlaceholder={t('settings:jsPlugins.siteBinding.search')}
              emptyText={emptyOptions}
              selectedText={t('settings:jsPlugins.automation.selectedUnit')}
              maxVisibleTags={3}
              onChange={(ids) => setValue(input.name, ids)}
            />
          ) : (
            <Select value={typeof value === 'string' ? value : undefined} onValueChange={(id) => setValue(input.name, id)}>
              <SelectTrigger>
                <SelectValue placeholder={placeholder} />
              </SelectTrigger>
              <SelectContent searchable={false}>
                {options.map((option) => (
                  <SelectItem key={option.id} value={option.id} disabled={option.disabled}>
                    {option.name}
                    {option.disabled ? ` — ${reasonText(option)}` : option.description ? ` · ${option.description}` : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          {!loading && options.length === 0 ? <p className="text-xs text-muted-soft">{emptyOptions}</p> : null}
        </FormField>
      )
    }

    if (input.type === 'boolean') {
      return (
        <Checkbox
          key={input.name}
          checked={value === true}
          onCheckedChange={(checked) => setValue(input.name, checked === true)}
          label={label}
          description={hint}
        />
      )
    }
    if (input.enum && input.enum.length > 0) {
      return (
        <FormField key={input.name} label={label} description={hint} required={isRequired(input)}>
          <Select
            value={value === undefined ? undefined : String(value)}
            onValueChange={(next) => setValue(input.name, input.enum?.find((option) => String(option) === next) as JSPluginAutomationValue)}
          >
            <SelectTrigger>
              <SelectValue placeholder={text(input.placeholder)} />
            </SelectTrigger>
            <SelectContent searchable={false}>
              {input.enum.map((option) => (
                <SelectItem key={String(option)} value={String(option)}>
                  {String(option)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FormField>
      )
    }
    const numeric = input.type === 'number' || input.type === 'integer'
    return (
      <FormField key={input.name} label={label} description={hint} required={isRequired(input)}>
        <Input
          type={numeric ? 'number' : 'text'}
          min={input.minimum}
          max={input.maximum}
          placeholder={text(input.placeholder)}
          value={value === undefined || Array.isArray(value) ? '' : String(value)}
          onChange={(event) => {
            const raw = event.target.value
            if (raw === '') return setValue(input.name, undefined)
            setValue(input.name, numeric ? Number(raw) : raw)
          }}
        />
      </FormField>
    )
  }

  // A binding is headed by the object its events are about; the other inputs follow.
  const describe = (item: JSPluginAutomation) => {
    const named = (input: JSPluginAutomationInput) => {
      const saved = item.inputs.find((candidate) => candidate.name === input.name)
      const value = isObjectInput(input)
        ? (saved?.entities ?? []).map((entity) => entity.name || entity.id).join(', ')
        : saved?.value === undefined
          ? ''
          : String(saved.value)
      return value ? `${inputTitle(input)}: ${value}` : ''
    }
    const heading = subjectInput
      ? (item.inputs.find((candidate) => candidate.name === subjectInput.name)?.entities ?? []).map((entity) => entity.name || entity.id).join(', ')
      : ''
    const rest = manifest.inputs.filter((input) => input !== subjectInput).map(named).filter(Boolean)
    return { heading: heading || item.id, rest }
  }

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
          <h4 className="text-sm font-semibold text-foreground">{title}</h4>
          <p className="text-xs text-muted-soft">{description}</p>
          {manifest.schedule?.everyMinutes ? (
            <p className="text-xs text-muted-soft">
              {t('settings:jsPlugins.automation.schedule', { minutes: manifest.schedule.everyMinutes })}
            </p>
          ) : null}
        </div>
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="shrink-0"
          onClick={() => setForm({ editing: null, values: initialValues(manifest) })}
        >
          <Plus className="mr-1.5 h-4 w-4" />
          {addLabel}
        </Button>
      </div>

      {automationsQuery.isLoading ? (
        <div className="flex items-center gap-2 py-3 text-sm text-muted-soft">
          <LoaderCircle className="h-4 w-4 animate-spin" />
          {t('settings:jsPlugins.loading')}
        </div>
      ) : items.length === 0 ? (
        <p className="text-sm text-muted-soft">{emptyText}</p>
      ) : (
        <ul className="divide-y divide-[hsl(var(--glass-divider))]">
          {items.map((item) => {
            const { heading, rest } = describe(item)
            return (
              <li key={item.id} className="flex items-start justify-between gap-3 py-3">
                <div className="min-w-0 space-y-1">
                  <div className="truncate text-sm font-medium text-foreground">{heading}</div>
                  {rest.map((line) => (
                    <p key={line} className="break-words text-xs text-muted-soft">
                      {line}
                    </p>
                  ))}
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    aria-label={t('settings:jsPlugins.automation.edit')}
                    onClick={() => setForm({ editing: item.id, values: initialValues(manifest, item) })}
                  >
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
            )
          })}
        </ul>
      )}

      <Dialog open={form != null} onOpenChange={(open) => (open ? undefined : setForm(null))}>
        <DialogContent size="md">
          <DialogHeader>
            <DialogTitle>{form?.editing ? t('settings:jsPlugins.automation.editTitle') : addLabel}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          <DialogBody className="space-y-4">{form ? manifest.inputs.map((input) => renderInput(input, form)) : null}</DialogBody>
          <DialogFooter
            cancel={
              <Button type="button" variant="outline" onClick={() => setForm(null)} disabled={saveMutation.isPending}>
                {t('common:actions.cancel')}
              </Button>
            }
            confirm={
              <Button type="button" disabled={!canSave} onClick={() => form && saveMutation.mutate(form)}>
                {saveMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : t('common:actions.save')}
              </Button>
            }
          />
        </DialogContent>
      </Dialog>

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
