import { useMemo, useState, type ReactNode } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { FlaskConical, LoaderCircle, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { BrandMark } from '@/components/common/brand-mark'
import { EmptyState } from '@/components/common/empty-state'
import { StatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { Draw, DrawBody, DrawContent, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { toast } from '@/lib/toast'
import {
  listCanonicalModels,
  listSiteAPIKeys,
  listSiteModels,
  sitesQueryKeys,
  testSiteModel,
  type CanonicalModelItem,
  type Site,
  type SiteAPIKey,
  type SiteModel,
  type SiteModelTestProtocol,
  type SiteModelTestResult,
} from '@/features/sites/api/sites'
import { siteModelIconInfo } from '@/features/sites/lib/model-icon'

const EMPTY_MODELS: SiteModel[] = []
const EMPTY_API_KEYS: SiteAPIKey[] = []
const AUTO_CREDENTIAL_ID = 'auto'
type SiteModelTestStreamMode = 'stream' | 'non_stream'

type TestRecord =
  | { status: 'success'; result: SiteModelTestResult }
  | { status: 'error'; message: string }

export function SiteModelTestSheet({
  site,
  models,
  onOpenChange,
}: {
  site: Site | null
  models: SiteModel[]
  onOpenChange: (open: boolean) => void
}) {
  const open = Boolean(site)
  const { t } = useTranslation(['sites', 'components'])
  const [results, setResults] = useState<Record<string, TestRecord>>({})
  const [pendingModelIds, setPendingModelIds] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const [protocol, setProtocol] = useState<SiteModelTestProtocol>('auto')
  const [streamMode, setStreamMode] = useState<SiteModelTestStreamMode>('stream')
  const [credentialId, setCredentialId] = useState(AUTO_CREDENTIAL_ID)
  const modelsQuery = useQuery({
    queryKey: site ? sitesQueryKeys.models(site.id) : [...sitesQueryKeys.all, 'models', 'test-none'],
    queryFn: async () => {
      if (!site) return { items: EMPTY_MODELS }
      return listSiteModels(site.id)
    },
    enabled: open,
    initialData: models.length ? { items: models } : undefined,
  })
  const canonicalModelsQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'canonical-models'],
    queryFn: listCanonicalModels,
    enabled: open,
  })
  const apiKeysQuery = useQuery({
    queryKey: site ? [...sitesQueryKeys.detail(site.id), 'api-keys'] : [...sitesQueryKeys.all, 'api-keys', 'test-none'],
    queryFn: async () => {
      if (!site) return { items: EMPTY_API_KEYS }
      return listSiteAPIKeys(site.id)
    },
    enabled: open && site?.supports_multiple_api_keys === true,
  })
  const availableAPIKeys = useMemo(
    () => [...(apiKeysQuery.data?.items ?? EMPTY_API_KEYS)]
      .filter((apiKey) => apiKey.enabled && !apiKey.secret_missing)
      .sort((left, right) => {
        const priority = (right.routing_priority ?? 1) - (left.routing_priority ?? 1)
        if (priority !== 0) return priority
        return left.id.localeCompare(right.id)
      }),
    [apiKeysQuery.data?.items],
  )
  const selectedCredentialId = availableAPIKeys.some((apiKey) => apiKey.id === credentialId)
    ? credentialId
    : AUTO_CREDENTIAL_ID
  const testMutation = useMutation({
    mutationFn: async (model: SiteModel) => {
      if (!site) throw new Error('site required')
      const result = await testSiteModel(site.id, model.id, {
        protocol,
        stream: streamMode === 'stream',
        siteCredentialId: selectedCredentialId === AUTO_CREDENTIAL_ID ? undefined : selectedCredentialId,
      })
      return { modelId: model.id, result }
    },
    onMutate: (model) => {
      setPendingModelIds((current) => (
        current.includes(model.id) ? current : [...current, model.id]
      ))
    },
    onSuccess: ({ modelId, result }) => {
      setResults((current) => ({ ...current, [modelId]: { status: 'success', result } }))
    },
    onError: (error, model) => {
      const message = error instanceof Error ? error.message : String(error)
      setResults((current) => ({ ...current, [model.id]: { status: 'error', message } }))
      toast.error(t('test.toast.testFailed'), { description: message })
    },
    onSettled: (_data, _error, model) => {
      if (!model) return
      setPendingModelIds((current) => current.filter((id) => id !== model.id))
    },
  })

  const availableModels = useMemo(
    () => [...(modelsQuery.data?.items ?? EMPTY_MODELS)]
      .sort((a, b) => a.upstream_model_name.localeCompare(b.upstream_model_name)),
    [modelsQuery.data?.items],
  )
  const keyword = search.trim().toLowerCase()
  const filteredModels = useMemo(
    () => {
      if (!keyword) return availableModels
      return availableModels.filter((model) => [
        model.upstream_model_name,
        model.display_name ?? '',
        model.id,
      ].some((value) => value.toLowerCase().includes(keyword)))
    },
    [availableModels, keyword],
  )
  const canonicalMap = useMemo<ReadonlyMap<string, CanonicalModelItem>>(
    () => new Map((canonicalModelsQuery.data?.items ?? []).map((model) => [model.id, model])),
    [canonicalModelsQuery.data?.items],
  )

  function handleOpenChange(nextOpen: boolean) {
    onOpenChange(nextOpen)
    if (!nextOpen) {
      setSearch('')
      setCredentialId(AUTO_CREDENTIAL_ID)
    }
  }

  return (
    <Draw open={open} onOpenChange={handleOpenChange}>
      <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
        <DrawHeader>
          <DrawTitle>{site ? t('test.title', { name: site.name }) : t('test.title', { name: '' })}</DrawTitle>
        </DrawHeader>
        <DrawBody className="space-y-3">
          {modelsQuery.isLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 5 }).map((_, index) => (
                <div key={index} className="h-14 rounded-lg bg-[hsl(var(--surface-subtle))]" />
              ))}
            </div>
          ) : availableModels.length ? (
            <>
              <div className="relative min-w-0">
                <Search className="text-foreground/40 pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
                <Input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={t('components:modelsDraw.searchPlaceholder')}
                  className="pl-10"
                />
              </div>
              <div className={site?.supports_multiple_api_keys ? 'grid grid-cols-1 gap-2 sm:grid-cols-3 sm:gap-3' : 'grid grid-cols-2 gap-2 sm:gap-3'}>
                <Select value={protocol} onValueChange={(value) => setProtocol(value as SiteModelTestProtocol)}>
                  <SelectTrigger className="h-10 min-w-0 px-3 sm:px-4">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent searchable={false}>
                    <SelectItem value="auto">{t('test.protocol.auto')}</SelectItem>
                    <SelectItem value="chat_completions">{t('test.protocol.chatCompletions')}</SelectItem>
                    <SelectItem value="responses">{t('test.protocol.responses')}</SelectItem>
                    <SelectItem value="messages">{t('test.protocol.messages')}</SelectItem>
                  </SelectContent>
                </Select>
                <Select value={streamMode} onValueChange={(value) => setStreamMode(value as SiteModelTestStreamMode)}>
                  <SelectTrigger className="h-10 min-w-0 px-3 sm:px-4">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent searchable={false}>
                    <SelectItem value="stream">{t('test.stream.stream')}</SelectItem>
                    <SelectItem value="non_stream">{t('test.stream.nonStream')}</SelectItem>
                  </SelectContent>
                </Select>
                {site?.supports_multiple_api_keys ? (
                  <Select
                    value={selectedCredentialId}
                    onValueChange={(value) => {
                      setCredentialId(value)
                      setResults({})
                    }}
                  >
                    <SelectTrigger className="h-10 min-w-0 px-3 sm:px-4">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent searchable={false}>
                      <SelectItem value={AUTO_CREDENTIAL_ID}>{t('test.credential.auto')}</SelectItem>
                      {availableAPIKeys.map((apiKey) => (
                        <SelectItem key={apiKey.id} value={apiKey.id}>
                          {apiKey.name || apiKey.key || apiKey.id} · P{apiKey.routing_priority ?? 1}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : null}
              </div>
              <div className="overflow-hidden rounded-lg border border-[hsl(var(--glass-border))]">
                <table className="w-full table-fixed border-collapse text-left">
                  <thead className="bg-[hsl(var(--surface-subtle))]">
                    <tr className="text-faint text-xs uppercase tracking-[0.16em]">
                      <th className="w-[42%] px-3 py-2.5 font-medium md:px-4 md:py-3">{t('test.headers.model')}</th>
                      <th className="w-[26%] px-2 py-2.5 font-medium md:px-4 md:py-3">{t('test.headers.status')}</th>
                      <th className="w-[20%] px-2 py-2.5 font-medium md:px-4 md:py-3">{t('test.headers.latency')}</th>
                      <th className="w-[12%] px-2 py-2.5 text-right font-medium md:px-4 md:py-3">{t('test.headers.action')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredModels.length ? filteredModels.map((model) => {
                      const record = results[model.id]
                      const result = record?.status === 'success' ? record.result : null
                      const pending = pendingModelIds.includes(model.id)
                      const failureResponse = result && !result.ok
                        ? formatTestFailureResponse(result.result.failure_response, result.result.error_message)
                        : ''
                      const icon = siteModelIconInfo(model, canonicalMap, site)
                      return (
                        <tr key={model.id} className="border-t border-[hsl(var(--glass-divider))]">
                          <td className="px-3 py-2.5 align-middle md:px-4 md:py-3">
                            <div className="flex min-w-0 items-center gap-2">
                              <BrandMark
                                iconPath={icon.iconPath}
                                label={icon.label}
                                fallback={icon.fallback}
                                fallbackText={icon.fallbackText}
                                size="sm"
                              />
                              <div className="truncate text-sm font-medium text-foreground" title={model.upstream_model_name}>
                                {model.upstream_model_name}
                              </div>
                            </div>
                          </td>
                          <td className="px-2 py-2.5 align-middle md:px-4 md:py-3">
                            <div className="flex min-w-0 items-center gap-1.5">
                              {record ? (
                                record.status === 'success' ? (
                                  failureResponse ? (
                                    <FailureResponsePopover value={failureResponse}>
                                      <StatusBadge status={record.result.ok ? 'healthy' : 'error'} className="whitespace-nowrap px-2 py-0.5 text-xs">
                                        {record.result.ok ? t('test.status.success') : t('test.status.failed')}
                                      </StatusBadge>
                                    </FailureResponsePopover>
                                  ) : (
                                    <StatusBadge status={record.result.ok ? 'healthy' : 'error'} className="whitespace-nowrap px-2 py-0.5 text-xs">
                                      {record.result.ok ? t('test.status.success') : t('test.status.failed')}
                                    </StatusBadge>
                                  )
                                ) : (
                                  <FailureResponsePopover value={record.message}>
                                    <StatusBadge status="error" className="whitespace-nowrap px-2 py-0.5 text-xs">{t('test.status.failed')}</StatusBadge>
                                  </FailureResponsePopover>
                                )
                              ) : (
                                <StatusBadge status="idle" className="whitespace-nowrap px-2 py-0.5 text-xs">{t('test.status.untested')}</StatusBadge>
                              )}
                              {result?.result.status_code ? (
                                <span className="shrink-0 text-xs text-muted-soft">{result.result.status_code}</span>
                              ) : null}
                              {result?.request.credential_name ? (
                                <span className="min-w-0 truncate text-xs text-muted-soft" title={result.request.credential_name}>
                                  {result.request.credential_name}
                                </span>
                              ) : null}
                            </div>
                          </td>
                          <td className="px-2 py-2.5 align-middle text-sm tabular-nums text-foreground md:px-4 md:py-3">
                            {result?.result.latency_ms != null ? `${result.result.latency_ms} ms` : '-'}
                          </td>
                          <td className="px-2 py-2.5 align-middle text-right md:px-4 md:py-3">
                            <Button
                              size="icon"
                              variant="ghost"
                              className="h-8 w-8 border-0 bg-transparent p-0 shadow-none hover:bg-transparent hover:text-primary hover:shadow-none"
                              onClick={() => testMutation.mutate(model)}
                              disabled={pending}
                              aria-label={t('test.test')}
                              title={t('test.test')}
                            >
                              {pending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <FlaskConical className="h-3.5 w-3.5" />}
                            </Button>
                          </td>
                        </tr>
                      )
                    }) : (
                      <tr>
                        <td colSpan={4} className="px-4 py-10 text-center text-sm text-muted-soft">{t('components:modelsDraw.noModels')}</td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </>
          ) : (
            <EmptyState title={t('test.empty')} description={t('test.empty')} />
          )}
        </DrawBody>
      </DrawContent>
    </Draw>
  )
}

function FailureResponsePopover({ value, children }: { value: string; children: ReactNode }) {
  const [open, setOpen] = useState(false)

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen} modal={false}>
      <PopoverPrimitive.Trigger asChild>
        <span
          className="inline-flex"
          onMouseEnter={() => setOpen(true)}
          onMouseLeave={() => setOpen(false)}
          onFocus={() => setOpen(true)}
          onBlur={() => setOpen(false)}
        >
          {children}
        </span>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="start"
          sideOffset={8}
          className="glass-panel-strong z-[140] w-[420px] max-w-[calc(100vw-32px)] rounded-lg p-3"
          onMouseEnter={() => setOpen(true)}
          onMouseLeave={() => setOpen(false)}
          onClick={(event) => event.stopPropagation()}
        >
          <code className="block max-h-56 overflow-auto whitespace-pre-wrap break-all rounded-md bg-[hsl(var(--surface-subtle))] px-3 py-2 text-xs leading-relaxed text-muted-foreground">
            {value}
          </code>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}

function formatTestFailureResponse(value: unknown, fallback?: string | null) {
  const message = extractFailureMessage(value)
  if (message) return message
  if (fallback?.trim()) return fallback.trim()
  if (value == null) return ''
  if (typeof value === 'string') return value.trim()
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function extractFailureMessage(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (!trimmed) return ''
    try {
      return extractFailureMessage(JSON.parse(trimmed)) || trimmed
    } catch {
      return trimmed
    }
  }
  if (Array.isArray(value)) {
    return value.map(extractFailureMessage).find(Boolean) ?? ''
  }
  if (typeof value !== 'object') return String(value)
  const record = value as Record<string, unknown>
  for (const key of ['message', 'detail', 'error_message']) {
    const message = stringValue(record[key])
    if (message) return message
  }
  const nested = extractFailureMessage(record.error)
    || extractFailureMessage(record.response)
    || extractFailureMessage(record.failure)
  if (nested) return nested
  return ''
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}
