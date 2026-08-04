import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, Gauge, LoaderCircle, PencilLine, Plus, Search, Trash2, XCircle } from 'lucide-react'
import { ErrorState } from '@/components/common/error-state'
import { toSlug } from '@/lib/slug'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import { toast } from '@/lib/toast'
import {
  fetchSystemProxyConfig,
  testSystemProxy,
  updateSystemProxyConfig,
  type ProxyConfig,
  type SystemProxyTestResult,
} from '@/features/settings/api/settings'

const systemProxySettingsKey = ['settings', 'system-proxy'] as const
const EMPTY_PROXIES: ProxyConfig[] = []
const proxyTypes = ['http', 'https', 'socks5'] as const

type ProxyFormValues = {
  id: string
  name: string
  type: string
  url: string
}

type ProxyTestRecord =
  | { status: 'success'; result: SystemProxyTestResult }
  | { status: 'error'; result: SystemProxyTestResult }

const emptyForm: ProxyFormValues = {
  id: '',
  name: '',
  type: 'http',
  url: '',
}

export function SystemProxySettingsPage() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [editingProxy, setEditingProxy] = useState<ProxyConfig | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ProxyConfig | null>(null)
  const [form, setForm] = useState<ProxyFormValues>(emptyForm)
  const [testResults, setTestResults] = useState<Record<string, ProxyTestRecord>>({})

  const systemProxyQuery = useQuery({
    queryKey: systemProxySettingsKey,
    queryFn: fetchSystemProxyConfig,
  })
  const proxies = systemProxyQuery.data?.proxies ?? EMPTY_PROXIES

  const filteredProxies = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    if (!keyword) return proxies
    return proxies.filter((proxy) => (
      `${proxy.name} ${proxy.type} ${proxy.url}`.toLowerCase().includes(keyword)
    ))
  }, [proxies, search])

  const saveMutation = useMutation({
    mutationFn: async () => {
      const next = proxyFromForm(form, editingProxy)
      validateProxy(next, proxies, editingProxy?.id, t)
      const nextProxies = editingProxy
        ? proxies.map((proxy) => (proxy.id === editingProxy.id ? next : proxy))
        : [...proxies, next]
      return updateSystemProxyConfig({ proxies: nextProxies })
    },
    onSuccess: async () => {
      closeForm()
      await queryClient.invalidateQueries({ queryKey: systemProxySettingsKey })
      toast.success(t('systemProxy.toast.saved'))
    },
    onError: (error) => {
      toast.error(t('systemProxy.toast.saveFailed'), { description: error.message })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (proxyID: string) => updateSystemProxyConfig({ proxies: proxies.filter((proxy) => proxy.id !== proxyID) }),
    onSuccess: async () => {
      setDeleteTarget(null)
      await queryClient.invalidateQueries({ queryKey: systemProxySettingsKey })
      toast.success(t('systemProxy.toast.deleted'))
    },
    onError: (error) => {
      toast.error(t('systemProxy.toast.deleteFailed'), { description: error.message })
    },
  })

  const testMutation = useMutation({
    mutationFn: async (proxy: ProxyConfig) => {
      const result = await testSystemProxy(proxy)
      return { proxy, result }
    },
    onSuccess: ({ proxy, result }) => {
      setTestResults((current) => ({
        ...current,
        [proxy.id]: { status: result.ok ? 'success' : 'error', result },
      }))
    },
    onError: (error, proxy) => {
      const message = error instanceof Error ? error.message : String(error)
      setTestResults((current) => ({
        ...current,
        [proxy.id]: {
          status: 'error',
          result: {
            ok: false,
            stage: 'request',
            latency_ms: 0,
            message,
          },
        },
      }))
      toast.error(t('systemProxy.toast.testFailed'), { description: message })
    },
  })

  function startCreate() {
    setEditingProxy(null)
    setForm({ ...emptyForm, id: newProxyID(t('systemProxy.newProxy')) })
    setFormOpen(true)
  }

  function startEdit(proxy: ProxyConfig) {
    setEditingProxy(proxy)
    setForm({
      id: proxy.id,
      name: proxy.name,
      type: proxy.type || 'http',
      url: proxy.url,
    })
    setFormOpen(true)
  }

  function closeForm() {
    setFormOpen(false)
    setEditingProxy(null)
    setForm(emptyForm)
  }

  if (systemProxyQuery.isLoading) {
    return <p className="text-sm text-muted-soft">{t('systemProxy.loading')}</p>
  }

  if (systemProxyQuery.isError) {
    return <ErrorState title={t('systemProxy.loadFailed')} description={systemProxyQuery.error.message} />
  }

  return (
    <div className="max-w-3xl space-y-6">
      <div className="space-y-1.5">
        <h3 className="text-lg font-semibold text-foreground">{t('systemProxy.title')}</h3>
        <p className="text-sm text-muted-soft">{t('systemProxy.description')}</p>
      </div>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative min-w-0 sm:max-w-sm sm:flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-muted-soft" />
          <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder={t('systemProxy.search')} className="pl-9" />
        </div>
        <Button onClick={startCreate}>
          <Plus className="h-4 w-4" />
          {t('systemProxy.actions.create')}
        </Button>
      </div>

      {filteredProxies.length ? (
        <div className="divide-y divide-[hsl(var(--divider))] border-y border-[hsl(var(--divider))]">
          {filteredProxies.map((proxy) => (
            <div key={proxy.id} className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-2 py-4">
              <div className="min-w-0 flex flex-wrap items-center gap-2 pr-1">
                <span className="min-w-0 truncate font-medium text-foreground">{proxy.name}</span>
                <Badge variant="secondary" className="shrink-0">{proxy.type.toUpperCase()}</Badge>
              </div>
              <ProxyActionButtons
                proxy={proxy}
                testing={testMutation.isPending && testMutation.variables?.id === proxy.id}
                onEdit={startEdit}
                onDelete={setDeleteTarget}
                onTest={(item) => testMutation.mutate(item)}
              />
              <p className="min-w-0 truncate text-sm text-muted-soft">{proxy.url}</p>
              <ProxyTestStatus
                testRecord={testResults[proxy.id]}
                testing={testMutation.isPending && testMutation.variables?.id === proxy.id}
              />
            </div>
          ))}
        </div>
      ) : null}

      <Draw open={formOpen} onOpenChange={(open) => { if (!open && !saveMutation.isPending) closeForm() }}>
        <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
          <DrawHeader>
            <DrawTitle>{editingProxy ? t('systemProxy.form.editTitle') : t('systemProxy.form.createTitle')}</DrawTitle>
          </DrawHeader>
          <DrawBody className="space-y-4">
            <FormField label={t('systemProxy.form.name')} required>
              <Input value={form.name} onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))} placeholder={t('systemProxy.form.namePlaceholder')} />
            </FormField>

            <FormField label={t('systemProxy.form.type')} required>
              <Select value={form.type || 'http'} onValueChange={(type) => setForm((current) => ({ ...current, type }))}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent searchable={false}>
                  {proxyTypes.map((type) => (
                    <SelectItem key={type} value={type}>
                      {type.toUpperCase()}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </FormField>

            <FormField label={t('systemProxy.form.url')} required description={t('systemProxy.form.urlHint')}>
              <Input
                value={form.url}
                onChange={(event) => setForm((current) => ({ ...current, url: event.target.value }))}
                placeholder={form.type === 'socks5' ? 'socks5://127.0.0.1:7890' : 'http://127.0.0.1:7890'}
              />
            </FormField>
          </DrawBody>
          <DrawFooter>
            <Button onClick={() => saveMutation.mutate()} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {editingProxy ? t('systemProxy.actions.save') : t('systemProxy.actions.create')}
            </Button>
            <Button variant="ghost" onClick={closeForm} disabled={saveMutation.isPending}>{t('systemProxy.actions.cancel')}</Button>
          </DrawFooter>
        </DrawContent>
      </Draw>

      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open && !deleteMutation.isPending) setDeleteTarget(null) }}>
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('systemProxy.deleteDialog.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pt-0">
            <DialogDescription className="mt-0">
              {deleteTarget ? t('systemProxy.deleteDialog.confirm', { name: deleteTarget.name }) : t('systemProxy.deleteDialog.confirmDefault')}
            </DialogDescription>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleteMutation.isPending}>{t('systemProxy.actions.cancel')}</Button>
            <Button variant="destructive" onClick={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget.id) }} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {t('systemProxy.actions.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function ProxyActionButtons({
  proxy,
  testing,
  className,
  onEdit,
  onDelete,
  onTest,
}: {
  proxy: ProxyConfig
  testing: boolean
  className?: string
  onEdit: (proxy: ProxyConfig) => void
  onDelete: (proxy: ProxyConfig) => void
  onTest: (proxy: ProxyConfig) => void
}) {
  const { t } = useTranslation('settings')
  return (
    <div className={`flex shrink-0 items-center justify-start gap-1 sm:gap-1.5 ${className ?? ''}`}>
      <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => onTest(proxy)} disabled={testing} title={t('systemProxy.actions.test')}>
        {testing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Gauge className="h-4 w-4" />}
      </Button>
      <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => onEdit(proxy)} title={t('systemProxy.actions.edit')}>
        <PencilLine className="h-4 w-4" />
      </Button>
      <Button size="icon" variant="ghost" className="h-8 w-8 text-red-500 hover:bg-red-500/10 hover:text-red-400" onClick={() => onDelete(proxy)} title={t('systemProxy.actions.delete')}>
        <Trash2 className="h-4 w-4" />
      </Button>
    </div>
  )
}

function ProxyTestStatus({
  testRecord,
  testing,
  className,
}: {
  testRecord?: ProxyTestRecord
  testing: boolean
  className?: string
}) {
  const { t } = useTranslation('settings')
  const statusLabel = proxyTestStatusLabel(testRecord, t)
  const statusTitle = testRecord?.result.message || statusLabel
  return (
    <div className={`flex h-8 min-w-[96px] shrink-0 items-center justify-center gap-1.5 rounded-md border px-2 text-xs sm:min-w-[112px] ${proxyTestStatusClass(testRecord)} ${className ?? ''}`} title={statusTitle}>
      {testing ? (
        <>
          <LoaderCircle className="h-3.5 w-3.5 animate-spin text-muted-soft" />
          <span className="text-muted-soft">{t('systemProxy.test.testing')}</span>
        </>
      ) : testRecord?.status === 'success' ? (
        <>
          <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
          <span className="text-emerald-600 dark:text-emerald-400">{statusLabel}</span>
        </>
      ) : testRecord?.status === 'error' ? (
        <>
          <XCircle className="h-3.5 w-3.5 text-red-500" />
          <span className="text-red-500">{statusLabel}</span>
        </>
      ) : (
        <span className="text-muted-soft">{t('systemProxy.test.untested')}</span>
      )}
    </div>
  )
}

function proxyTestStatusLabel(record: ProxyTestRecord | undefined, t: (key: string, options?: Record<string, unknown>) => string) {
  if (!record) return t('systemProxy.test.untested')
  if (record.status === 'success') {
    return t('systemProxy.test.success', { latency: record.result.latency_ms })
  }
  return t('systemProxy.test.failed')
}

function proxyTestStatusClass(record: ProxyTestRecord | undefined) {
  if (record?.status === 'success') {
    return 'border-emerald-500/25 bg-emerald-500/10'
  }
  if (record?.status === 'error') {
    return 'border-red-500/25 bg-red-500/10'
  }
  return 'border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] text-muted-soft'
}

function proxyFromForm(form: ProxyFormValues, editingProxy: ProxyConfig | null): ProxyConfig {
  const name = form.name.trim()
  return {
    id: editingProxy?.id || form.id || newProxyID(name),
    name,
    type: form.type.trim().toLowerCase(),
    url: form.url.trim(),
  }
}

function validateProxy(proxy: ProxyConfig, existing: ProxyConfig[], editingID: string | undefined, t: (key: string) => string) {
  if (!proxy.name) throw new Error(t('systemProxy.validation.nameRequired'))
  if (!proxy.url) throw new Error(t('systemProxy.validation.urlRequired'))
  if (!proxyTypes.includes(proxy.type as typeof proxyTypes[number])) throw new Error(t('systemProxy.validation.typeInvalid'))
  try {
    const parsed = new URL(proxy.url)
    if (!parsed.protocol || !parsed.host) throw new Error()
    if (proxy.type === 'socks5' && parsed.protocol !== 'socks5:') throw new Error(t('systemProxy.validation.socksScheme'))
    if (proxy.type !== 'socks5' && parsed.protocol !== 'http:' && parsed.protocol !== 'https:') throw new Error(t('systemProxy.validation.httpScheme'))
  } catch (error) {
    if (error instanceof Error && error.message) throw error
    throw new Error(t('systemProxy.validation.urlInvalid'))
  }
  const duplicated = existing.some((item) => item.id !== editingID && item.name.trim().toLowerCase() === proxy.name.trim().toLowerCase())
  if (duplicated) throw new Error(t('systemProxy.validation.nameDuplicate'))
}

function newProxyID(name: string) {
  const suffix = typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID().slice(0, 8)
    : Date.now().toString(36)
  return `${toSlug(name) || 'proxy'}-${suffix}`
}
