import { useCallback, useMemo, useRef, useState } from 'react'
import type { DragEvent, ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import cronstrue from 'cronstrue/i18n'
import {
  AlertTriangle,
  CalendarClock,
  CheckCircle2,
  Cloud,
  DatabaseBackup,
  Download,
  FileArchive,
  LoaderCircle,
  Play,
  RefreshCw,
  Save,
  Trash2,
  Upload,
  X,
} from 'lucide-react'
import { PageHeader } from '@/components/common/page-header'
import { StatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  backupDownloadURL,
  deleteAutomaticBackupFile,
  exportBackup,
  fetchAutomaticBackupConfig,
  importBackup,
  listAutomaticBackupFiles,
  restoreAutomaticBackupFile,
  runAutomaticBackup,
  testAutomaticBackupConfig,
  updateAutomaticBackupConfig,
  type AutomaticBackupConfig,
  type AutomaticBackupConfigInput,
  type AutomaticBackupFile,
  type BackupImportSummary,
} from '@/features/settings/api/settings'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'

const automaticBackupConfigKey = ['settings', 'backup', 'automatic'] as const
const automaticBackupFilesKey = ['settings', 'backup', 'automatic', 'files'] as const
const automaticBackupFilePollMs = 3000

type AutomaticBackupDraft = {
  enabled: boolean
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  secretKey: string
  cron: string
  retentionCount: string
  backupPassphrase: string
  forcePathStyle: boolean
  useSSL: boolean
  skipTLSVerify: boolean
}

const DEFAULT_AUTOMATIC_BACKUP_DRAFT: AutomaticBackupDraft = {
  enabled: false,
  endpoint: '',
  region: '',
  bucket: '',
  prefix: 'xlyra/',
  accessKey: '',
  secretKey: '',
  cron: '0 3 * * *',
  retentionCount: '7',
  backupPassphrase: '',
  forcePathStyle: true,
  useSSL: true,
  skipTLSVerify: false,
}

export function BackupSettingsPage() {
  const { t, i18n } = useTranslation('settings')
  const queryClient = useQueryClient()
  const [exportOpen, setExportOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [exportPassphrase, setExportPassphrase] = useState('')
  const [exportConfirm, setExportConfirm] = useState('')
  const [importPassphrase, setImportPassphrase] = useState('')
  const [importFile, setImportFile] = useState<File | null>(null)
  const [dragging, setDragging] = useState(false)
  const [automaticDraftOverride, setAutomaticDraftOverride] = useState<AutomaticBackupDraft | null>(null)
  const [restoreTarget, setRestoreTarget] = useState<AutomaticBackupFile | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AutomaticBackupFile | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  const automaticQuery = useQuery({
    queryKey: automaticBackupConfigKey,
    queryFn: fetchAutomaticBackupConfig,
  })
  const cronLocale = i18n.language?.startsWith('zh') ? 'zh_CN' : i18n.language?.startsWith('jp') || i18n.language?.startsWith('ja') ? 'ja' : 'en'
  const automaticConfig = automaticQuery.data?.automatic_backup
  const savedAutomaticDraft = automaticConfig ? automaticConfigToDraft(automaticConfig) : DEFAULT_AUTOMATIC_BACKUP_DRAFT
  const automaticDraft = automaticDraftOverride ?? savedAutomaticDraft
  const automaticCron = useMemo(() => describeCron(automaticDraft.cron, cronLocale, t), [automaticDraft.cron, cronLocale, t])
  const retentionError = validatePositiveInteger(automaticDraft.retentionCount, t, 365)
  const storageDirty = Boolean(automaticDraftOverride && !sameAutomaticStorage(automaticDraft, savedAutomaticDraft))
  const savedStorageComplete = Boolean(
    automaticConfig?.endpoint.trim()
    && automaticConfig.bucket.trim()
    && automaticConfig.access_key.trim()
    && automaticConfig.has_secret_key,
  )
  const savedAutomaticBackupReady = Boolean(savedStorageComplete && automaticConfig?.has_backup_passphrase)
  const canTestStorage = savedStorageComplete && !storageDirty
  const filesQuery = useQuery({
    queryKey: automaticBackupFilesKey,
    queryFn: listAutomaticBackupFiles,
    enabled: savedAutomaticBackupReady,
    refetchInterval: (query) => query.state.data?.files.some((file) => file.status === 'running') ? automaticBackupFilePollMs : false,
  })
  const automaticFiles = filesQuery.data?.files ?? []

  const exportMutation = useMutation({
    mutationFn: exportBackup,
    onSuccess: (ticket) => {
      const link = document.createElement('a')
      link.href = backupDownloadURL(ticket)
      link.download = ticket.filename || 'xlyra-backup.zip.xlyra'
      document.body.append(link)
      link.click()
      link.remove()
      toast.success(t('backup.export.success'))
      closeExportDialog()
    },
    onError: (error) => {
      toast.error(t('backup.export.failed'), { description: error.message })
    },
  })

  const importMutation = useMutation({
    mutationFn: importBackup,
    onSuccess: ({ backup }) => {
      toast.success(t('backup.import.success'), { description: importSummaryText(backup, t) })
      closeImportDialog()
    },
    onError: (error) => {
      toast.error(t('backup.import.failed'), { description: error.message })
    },
  })

  const saveStorageMutation = useMutation({
    mutationFn: updateAutomaticBackupConfig,
    onSuccess: async ({ automatic_backup }) => {
      queryClient.setQueryData(automaticBackupConfigKey, { automatic_backup })
      await queryClient.invalidateQueries({ queryKey: automaticBackupConfigKey })
      const saved = automaticConfigToDraft(automatic_backup)
      setAutomaticDraftOverride((current) => reconcileAutomaticDraft(current, saved, 'storage'))
      toast.success(t('backup.automatic.saveSuccess'))
    },
    onError: (error) => {
      toast.error(t('backup.automatic.saveFailed'), { description: error.message })
    },
  })

  const saveScheduleMutation = useMutation({
    mutationFn: updateAutomaticBackupConfig,
    onSuccess: async ({ automatic_backup }) => {
      queryClient.setQueryData(automaticBackupConfigKey, { automatic_backup })
      await queryClient.invalidateQueries({ queryKey: automaticBackupConfigKey })
      const saved = automaticConfigToDraft(automatic_backup)
      setAutomaticDraftOverride((current) => reconcileAutomaticDraft(current, saved, 'schedule'))
      toast.success(t('backup.automatic.saveSuccess'))
    },
    onError: (error) => {
      toast.error(t('backup.automatic.saveFailed'), { description: error.message })
    },
  })

  const testAutomaticMutation = useMutation({
    mutationFn: testAutomaticBackupConfig,
    onSuccess: ({ test }) => {
      if (test.ok) {
        toast.success(t('backup.automatic.testSuccess'), { description: test.message })
      } else {
        toast.error(t('backup.automatic.testFailed'), { description: test.message })
      }
    },
    onError: (error) => {
      toast.error(t('backup.automatic.testFailed'), { description: error.message })
    },
  })

  const runAutomaticMutation = useMutation({
    mutationFn: runAutomaticBackup,
    onSuccess: async ({ backup }) => {
      await queryClient.invalidateQueries({ queryKey: automaticBackupFilesKey })
      toast.success(t('backup.automatic.runSuccess'), { description: backup.filename })
    },
    onError: (error) => {
      toast.error(t('backup.automatic.runFailed'), { description: error.message })
    },
  })

  const restoreAutomaticMutation = useMutation({
    mutationFn: restoreAutomaticBackupFile,
    onSuccess: async ({ backup }) => {
      setRestoreTarget(null)
      await queryClient.invalidateQueries({ queryKey: automaticBackupConfigKey })
      toast.success(t('backup.import.success'), { description: importSummaryText(backup, t) })
    },
    onError: (error) => {
      toast.error(t('backup.automatic.restoreFailed'), { description: error.message })
    },
  })

  const deleteAutomaticMutation = useMutation({
    mutationFn: deleteAutomaticBackupFile,
    onSuccess: async () => {
      setDeleteTarget(null)
      await queryClient.invalidateQueries({ queryKey: automaticBackupFilesKey })
      toast.success(t('backup.automatic.deleteSuccess'))
    },
    onError: (error) => {
      toast.error(t('backup.automatic.deleteFailed'), { description: error.message })
    },
  })

  function closeExportDialog() {
    setExportOpen(false)
    setExportPassphrase('')
    setExportConfirm('')
  }

  function closeImportDialog() {
    setImportOpen(false)
    setImportFile(null)
    setImportPassphrase('')
    setDragging(false)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  function handleExport() {
    if (!exportPassphrase.trim()) {
      toast.error(t('backup.validation.passphraseRequired'))
      return
    }
    if (exportPassphrase !== exportConfirm) {
      toast.error(t('backup.validation.passphraseMismatch'))
      return
    }
    exportMutation.mutate(exportPassphrase)
  }

  function handleImport() {
    if (!importFile) {
      toast.error(t('backup.validation.fileRequired'))
      return
    }
    if (!isBackupFile(importFile)) {
      toast.error(t('backup.validation.fileInvalid'))
      return
    }
    if (!importPassphrase.trim()) {
      toast.error(t('backup.validation.passphraseRequired'))
      return
    }
    importMutation.mutate({ file: importFile, passphrase: importPassphrase })
  }

  function patchAutomaticDraft(patch: Partial<AutomaticBackupDraft>) {
    setAutomaticDraftOverride((current) => ({ ...(current ?? automaticDraft), ...patch }))
  }

  function handleSaveAutomaticStorage() {
    const missing = validateAutomaticStorageRequiredFields(automaticDraft, automaticConfig, t)
    if (missing.length) {
      toast.error(t('backup.automatic.validation.required'), { description: missing.join('、') })
      return
    }
    saveStorageMutation.mutate(automaticStorageDraftToInput(automaticDraft, savedAutomaticDraft))
  }

  function handleSaveAutomaticSchedule() {
    const missing = automaticDraft.enabled ? validateAutomaticScheduleRequiredFields(automaticDraft, automaticConfig, t) : []
    if (missing.length) {
      toast.error(t('backup.automatic.validation.required'), { description: missing.join('、') })
      return
    }
    if (automaticDraft.enabled && !savedStorageComplete) {
      toast.error(t('backup.automatic.validation.required'), { description: t('backup.automatic.storage.title') })
      return
    }
    if (!automaticCron.valid) {
      toast.error(t('backup.automatic.validation.cronInvalid'), { description: automaticCron.text })
      return
    }
    if (retentionError) {
      toast.error(t('backup.automatic.validation.retentionInvalid'), { description: retentionError })
      return
    }
    saveScheduleMutation.mutate(automaticScheduleDraftToInput(automaticDraft, savedAutomaticDraft))
  }

  const selectImportFile = useCallback((file: File | null) => {
    if (!file) return
    if (!isBackupFile(file)) {
      toast.error(t('backup.validation.fileInvalid'))
      return
    }
    setImportFile(file)
  }, [t])

  const handleDrop = useCallback((event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragging(false)
    selectImportFile(event.dataTransfer.files.item(0))
  }, [selectImportFile])

  return (
    <div className="space-y-7">
      <PageHeader
        eyebrow={t('backup.eyebrow')}
        title={t('backup.title')}
        description={t('backup.description')}
      />

      <div className="max-w-3xl">
        <Tabs defaultValue="manual" variant="segmented" className="space-y-4 pt-4">
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-3">
            <div className="min-w-0 shrink-0">
              <TabsList className="w-fit min-w-64 max-w-full">
                <TabsTrigger value="manual" className="px-4">{t('backup.tabs.manual')}</TabsTrigger>
                <TabsTrigger value="automatic" className="px-4">{t('backup.tabs.automatic')}</TabsTrigger>
              </TabsList>
            </div>
          </div>

          <TabsContent value="manual" className="space-y-8">
            <section className="space-y-4">
              <SectionTitle icon={<Download className="h-4 w-4" />} title={t('backup.export.title')} />
              <Notice items={[
                t('backup.export.noticeSensitive'),
                t('backup.export.noticePassphrase'),
                t('backup.export.noticeUsersExcluded'),
              ]} />
              <Button onClick={() => setExportOpen(true)} disabled={exportMutation.isPending}>
                <FileArchive className="h-4 w-4" />
                {t('backup.export.action')}
              </Button>
            </section>

            <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-7">
              <SectionTitle icon={<Upload className="h-4 w-4" />} title={t('backup.import.title')} />
              <Notice tone="danger" items={[
                t('backup.import.noticeOverwrite'),
                t('backup.import.noticePreserved'),
                t('backup.import.noticeRestart'),
              ]} />
              <Button variant="destructive" onClick={() => setImportOpen(true)} disabled={importMutation.isPending}>
                <DatabaseBackup className="h-4 w-4" />
                {t('backup.import.action')}
              </Button>
            </section>
          </TabsContent>

          <TabsContent value="automatic" className="space-y-8">
            {automaticQuery.isLoading ? (
              <p className="text-sm text-muted-soft">{t('backup.automatic.loading')}</p>
            ) : (
              <>
                <section className="space-y-4">
                  <SectionTitle icon={<Cloud className="h-4 w-4" />} title={t('backup.automatic.storage.title')} />
                  <div className="grid gap-4 md:grid-cols-2">
                    <FormField label={t('backup.automatic.storage.endpoint')} required>
                      <Input
                        value={automaticDraft.endpoint}
                        onChange={(event) => patchAutomaticDraft({ endpoint: event.target.value })}
                        placeholder="https://s3.example.com"
                      />
                    </FormField>
                    <FormField label={t('backup.automatic.storage.region')}>
                      <Input
                        value={automaticDraft.region}
                        onChange={(event) => patchAutomaticDraft({ region: event.target.value })}
                        placeholder="us-east-1"
                      />
                    </FormField>
                    <FormField label={t('backup.automatic.storage.bucket')} required>
                      <Input
                        value={automaticDraft.bucket}
                        onChange={(event) => patchAutomaticDraft({ bucket: event.target.value })}
                      />
                    </FormField>
                    <FormField label={t('backup.automatic.storage.prefix')}>
                      <Input
                        value={automaticDraft.prefix}
                        onChange={(event) => patchAutomaticDraft({ prefix: event.target.value })}
                      />
                    </FormField>
                    <FormField label={t('backup.automatic.storage.accessKey')} required>
                      <Input
                        value={automaticDraft.accessKey}
                        onChange={(event) => patchAutomaticDraft({ accessKey: event.target.value })}
                        spellCheck={false}
                      />
                    </FormField>
                    <FormField
                      label={t('backup.automatic.storage.secretKey')}
                      description={automaticConfig?.has_secret_key ? t('backup.automatic.storage.secretSaved', { value: automaticConfig.secret_key_masked }) : undefined}
                      required={!automaticConfig?.has_secret_key}
                    >
                      <Input
                        type="password"
                        autoComplete="new-password"
                        value={automaticDraft.secretKey}
                        onChange={(event) => patchAutomaticDraft({ secretKey: event.target.value })}
                      />
                    </FormField>
                  </div>
                  <div className="grid gap-4 md:grid-cols-3">
                    <ToggleField
                      label={t('backup.automatic.storage.forcePathStyle')}
                      checked={automaticDraft.forcePathStyle}
                      onCheckedChange={(forcePathStyle) => patchAutomaticDraft({ forcePathStyle })}
                    />
                    <ToggleField
                      label={t('backup.automatic.storage.useSSL')}
                      checked={automaticDraft.useSSL}
                      onCheckedChange={(useSSL) => patchAutomaticDraft({ useSSL })}
                    />
                    <ToggleField
                      label={t('backup.automatic.storage.skipTLSVerify')}
                      checked={automaticDraft.skipTLSVerify}
                      onCheckedChange={(skipTLSVerify) => patchAutomaticDraft({ skipTLSVerify })}
                    />
                  </div>
                  <div className="flex flex-wrap items-center gap-3">
                    <Button onClick={handleSaveAutomaticStorage} disabled={saveStorageMutation.isPending}>
                      {saveStorageMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                      {t('common:actions.save', { ns: 'common' })}
                    </Button>
                    <Button variant="outline" onClick={() => testAutomaticMutation.mutate()} disabled={!canTestStorage || testAutomaticMutation.isPending}>
                      {testAutomaticMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
                      {t('backup.automatic.testAction')}
                    </Button>
                  </div>
                </section>

                <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-7">
                  <SectionTitle icon={<CalendarClock className="h-4 w-4" />} title={t('backup.automatic.schedule.title')} />
                  <div className="grid gap-4 md:grid-cols-2">
                    <ToggleField
                      label={t('backup.automatic.schedule.enabled')}
                      checked={automaticDraft.enabled}
                      onCheckedChange={(enabled) => patchAutomaticDraft({ enabled })}
                    />
                    <FormField label={t('backup.automatic.schedule.cron')} description={automaticCron.valid ? automaticCron.text : undefined} error={automaticCron.valid ? undefined : automaticCron.text} required>
                      <Input
                        value={automaticDraft.cron}
                        onChange={(event) => patchAutomaticDraft({ cron: event.target.value })}
                        spellCheck={false}
                        className="font-mono"
                      />
                    </FormField>
                    <FormField label={t('backup.automatic.schedule.retention')} error={retentionError}>
                      <Input
                        type="text"
                        inputMode="numeric"
                        value={automaticDraft.retentionCount}
                        onChange={(event) => patchAutomaticDraft({ retentionCount: event.target.value })}
                      />
                    </FormField>
                    <FormField
                      label={t('backup.automatic.schedule.passphrase')}
                      description={automaticConfig?.has_backup_passphrase ? t('backup.automatic.schedule.passphraseSaved', { value: automaticConfig.backup_passphrase_masked }) : t('backup.automatic.schedule.passphraseDesc')}
                      required={!automaticConfig?.has_backup_passphrase}
                    >
                      <Input
                        type="password"
                        autoComplete="new-password"
                        value={automaticDraft.backupPassphrase}
                        onChange={(event) => patchAutomaticDraft({ backupPassphrase: event.target.value })}
                      />
                    </FormField>
                  </div>
                  <Notice items={[
                    t('backup.automatic.noticeNoSecrets'),
                    t('backup.automatic.noticeImportKeepsCurrent'),
                  ]} />
                  <div className="flex flex-wrap items-center gap-3">
                    <Button onClick={handleSaveAutomaticSchedule} disabled={saveScheduleMutation.isPending}>
                      {saveScheduleMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                      {t('common:actions.save', { ns: 'common' })}
                    </Button>
                    <Button variant="outline" onClick={() => runAutomaticMutation.mutate()} disabled={!savedAutomaticBackupReady || runAutomaticMutation.isPending}>
                      {runAutomaticMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Play className="h-4 w-4" />}
                      {t('backup.automatic.runAction')}
                    </Button>
                  </div>
                </section>

                <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-7">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <SectionTitle icon={<FileArchive className="h-4 w-4" />} title={t('backup.automatic.files.title')} />
                    <Button variant="outline" size="sm" onClick={() => filesQuery.refetch()} disabled={!savedAutomaticBackupReady || filesQuery.isFetching}>
                      {filesQuery.isFetching ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                      {t('backup.automatic.files.refresh')}
                    </Button>
                  </div>
                  {!savedAutomaticBackupReady ? (
                    <div className="rounded-md border border-[hsl(var(--divider))] bg-[hsl(var(--surface-subtle))] px-4 py-6 text-sm text-muted-soft">
                      {t('backup.automatic.files.notReady')}
                    </div>
                  ) : automaticFiles.length === 0 ? (
                    <div className="rounded-md border border-[hsl(var(--divider))] bg-[hsl(var(--surface-subtle))] px-4 py-6 text-sm text-muted-soft">
                      {filesQuery.isFetching ? t('backup.automatic.files.loading') : t('backup.automatic.files.empty')}
                    </div>
                  ) : (
                    <div className="overflow-hidden rounded-md border border-[hsl(var(--divider))]">
                      <div className="divide-y divide-[hsl(var(--divider))]">
                        {automaticFiles.map((file) => (
                          <div key={file.key} className="grid gap-3 px-4 py-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
                            <div className="min-w-0">
                              <div className="flex min-w-0 flex-wrap items-center gap-2">
                                <div className="min-w-0 truncate text-sm font-medium text-foreground">{file.filename}</div>
                                <AutomaticBackupFileStatus file={file} />
                              </div>
                              <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-soft">
                                {file.status === 'running' ? null : <span>{formatFileSize(file.size)}</span>}
                                <span>{formatDateTime(file.last_modified)}</span>
                                <span className="max-w-full truncate font-mono">{file.key}</span>
                                {file.error ? <span className="text-[hsl(var(--destructive))]">{file.error}</span> : null}
                              </div>
                            </div>
                            <div className="flex items-center gap-2">
                              <Button variant="outline" size="sm" onClick={() => setRestoreTarget(file)} disabled={file.status === 'running' || file.status === 'failed'}>
                                <DatabaseBackup className="h-4 w-4" />
                                {t('backup.automatic.files.restore')}
                              </Button>
                              <Button variant="destructive" size="sm" onClick={() => setDeleteTarget(file)} disabled={file.status === 'running'}>
                                <Trash2 className="h-4 w-4" />
                                {t('backup.automatic.files.delete')}
                              </Button>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </section>
              </>
            )}
          </TabsContent>
        </Tabs>
      </div>

      <Dialog open={exportOpen} onOpenChange={(open) => (open ? setExportOpen(true) : closeExportDialog())}>
        <DialogContent className="w-[min(92vw,560px)]">
          <DialogHeader>
            <DialogTitle>{t('backup.export.dialogTitle')}</DialogTitle>
            <DialogDescription>{t('backup.export.dialogDescription')}</DialogDescription>
          </DialogHeader>
          <DialogBody className="space-y-4">
            <FormField label={t('backup.passphrase')} description={t('backup.passphraseDesc')} required>
              <Input
                type="password"
                autoComplete="new-password"
                value={exportPassphrase}
                onChange={(event) => setExportPassphrase(event.target.value)}
              />
            </FormField>
            <FormField label={t('backup.confirmPassphrase')} required>
              <Input
                type="password"
                autoComplete="new-password"
                value={exportConfirm}
                onChange={(event) => setExportConfirm(event.target.value)}
              />
            </FormField>
          </DialogBody>
          <DialogFooter>
            <Button variant="outline" onClick={closeExportDialog} disabled={exportMutation.isPending}>
              {t('common:actions.cancel', { ns: 'common' })}
            </Button>
            <Button onClick={handleExport} disabled={exportMutation.isPending}>
              {exportMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <FileArchive className="h-4 w-4" />}
              {t('backup.export.confirmAction')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={importOpen} onOpenChange={(open) => (open ? setImportOpen(true) : closeImportDialog())}>
        <DialogContent className="w-[min(92vw,620px)]">
          <DialogHeader>
            <DialogTitle>{t('backup.import.dialogTitle')}</DialogTitle>
            <DialogDescription>{t('backup.import.confirmDescription')}</DialogDescription>
          </DialogHeader>
          <DialogBody className="space-y-4">
            <div
              onDrop={handleDrop}
              onDragOver={(event) => {
                event.preventDefault()
                setDragging(true)
              }}
              onDragLeave={(event) => {
                event.preventDefault()
                setDragging(false)
              }}
              onClick={() => fileInputRef.current?.click()}
              className={cn(
                'flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed px-4 py-8 text-center transition-colors',
                dragging
                  ? 'border-[hsl(var(--accent))] bg-[hsl(var(--accent))]/5'
                  : 'border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] hover:border-[hsl(var(--accent))]/50 hover:bg-[hsl(var(--surface-subtle))]',
              )}
            >
              <Upload className="h-8 w-8 text-muted-soft" />
              <div className="text-sm font-medium text-foreground">{t('backup.import.dropzone')}</div>
              <div className="text-xs text-muted-soft">{t('backup.import.fileHint')}</div>
              <input
                ref={fileInputRef}
                type="file"
                accept=".xlyra,application/vnd.xlyra.backup"
                className="hidden"
                onChange={(event) => {
                  selectImportFile(event.target.files?.item(0) ?? null)
                  event.target.value = ''
                }}
              />
            </div>

            {importFile ? (
              <div className="flex items-center gap-3 rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] px-3 py-2">
                <FileArchive className="h-4 w-4 shrink-0 text-muted-soft" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm text-foreground">{importFile.name}</div>
                  <div className="text-xs text-muted-soft">{formatFileSize(importFile.size)}</div>
                </div>
                <button
                  type="button"
                  onClick={() => setImportFile(null)}
                  className="shrink-0 rounded p-1 text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
                  aria-label={t('backup.import.removeFile')}
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            ) : null}

            <FormField label={t('backup.passphrase')} description={t('backup.import.passphraseDesc')} required>
              <Input
                type="password"
                autoComplete="current-password"
                value={importPassphrase}
                onChange={(event) => setImportPassphrase(event.target.value)}
              />
            </FormField>
          </DialogBody>
          <DialogFooter>
            <Button variant="outline" onClick={closeImportDialog} disabled={importMutation.isPending}>
              {t('common:actions.cancel', { ns: 'common' })}
            </Button>
            <Button variant="destructive" onClick={handleImport} disabled={importMutation.isPending}>
              {importMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <DatabaseBackup className="h-4 w-4" />}
              {t('backup.import.confirmAction')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={Boolean(restoreTarget)}
        title={t('backup.automatic.restoreConfirmTitle')}
        description={restoreTarget ? t('backup.automatic.restoreConfirmDescription', { filename: restoreTarget.filename }) : ''}
        confirmLabel={t('backup.automatic.restoreConfirmAction')}
        pending={restoreAutomaticMutation.isPending}
        destructive
        onCancel={() => setRestoreTarget(null)}
        onConfirm={() => {
          if (restoreTarget) restoreAutomaticMutation.mutate(restoreTarget.key)
        }}
      />

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        title={t('backup.automatic.deleteConfirmTitle')}
        description={deleteTarget ? t('backup.automatic.deleteConfirmDescription', { filename: deleteTarget.filename }) : ''}
        confirmLabel={t('backup.automatic.deleteConfirmAction')}
        pending={deleteAutomaticMutation.isPending}
        destructive
        onCancel={() => setDeleteTarget(null)}
        onConfirm={() => {
          if (deleteTarget) deleteAutomaticMutation.mutate(deleteTarget.key)
        }}
      />
    </div>
  )
}

function SectionTitle({ icon, title }: { icon: ReactNode; title: string }) {
  return (
    <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
      {icon}
      <span>{title}</span>
    </div>
  )
}

function Notice({ items, tone = 'default' }: { items: string[]; tone?: 'default' | 'danger' }) {
  return (
    <div className="rounded-md border border-[hsl(var(--divider))] bg-[hsl(var(--surface-subtle))] px-4 py-3">
      <div className="flex gap-3">
        <AlertTriangle className={tone === 'danger' ? 'mt-0.5 h-4 w-4 shrink-0 text-red-500' : 'mt-0.5 h-4 w-4 shrink-0 text-primary'} />
        <ul className="space-y-1 text-xs leading-5 text-muted-soft">
          {items.map((item) => <li key={item}>{item}</li>)}
        </ul>
      </div>
    </div>
  )
}

function AutomaticBackupFileStatus({ file }: { file: AutomaticBackupFile }) {
  const { t } = useTranslation('settings')
  if (file.status === 'running') {
    return <StatusBadge status="syncing" className="px-2 py-0.5 text-[11px]">{t('backup.automatic.files.running')}</StatusBadge>
  }
  if (file.status === 'failed') {
    return <StatusBadge status="error" className="px-2 py-0.5 text-[11px]">{t('backup.automatic.files.failed')}</StatusBadge>
  }
  return null
}

function ToggleField({ label, checked, onCheckedChange }: { label: string; checked: boolean; onCheckedChange: (checked: boolean) => void }) {
  return (
    <FormField label={label}>
      <div className="flex h-11 items-center gap-3">
        <Switch checked={checked} onCheckedChange={onCheckedChange} aria-label={label} />
      </div>
    </FormField>
  )
}

function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  pending,
  destructive,
  onCancel,
  onConfirm,
}: {
  open: boolean
  title: string
  description: string
  confirmLabel: string
  pending: boolean
  destructive?: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  const { t } = useTranslation('settings')
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onCancel() }}>
      <DialogContent className="w-[min(92vw,520px)]">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel} disabled={pending}>
            {t('common:actions.cancel', { ns: 'common' })}
          </Button>
          <Button variant={destructive ? 'destructive' : 'default'} onClick={onConfirm} disabled={pending}>
            {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function importSummaryText(summary: BackupImportSummary, t: (key: string, options?: Record<string, unknown>) => string) {
  return t('backup.import.summary', {
    tables: summary.tables,
    rows: summary.rows,
    configKeys: summary.config_keys,
  })
}

function isBackupFile(file: File) {
  return file.name.endsWith('.xlyra') || file.type === 'application/vnd.xlyra.backup'
}

function formatFileSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function automaticConfigToDraft(config: AutomaticBackupConfig): AutomaticBackupDraft {
  return {
    enabled: config.enabled,
    endpoint: config.endpoint,
    region: config.region,
    bucket: config.bucket,
    prefix: config.prefix,
    accessKey: config.access_key,
    secretKey: '',
    cron: config.cron,
    retentionCount: String(config.retention_count),
    backupPassphrase: '',
    forcePathStyle: config.force_path_style,
    useSSL: config.use_ssl,
    skipTLSVerify: config.skip_tls_verify,
  }
}

function automaticStorageDraftToInput(draft: AutomaticBackupDraft, saved: AutomaticBackupDraft): AutomaticBackupConfigInput {
  return {
    enabled: saved.enabled,
    cron: saved.cron.trim(),
    retention_count: Number(saved.retentionCount.trim()),
    endpoint: draft.endpoint.trim(),
    region: draft.region.trim(),
    bucket: draft.bucket.trim(),
    prefix: draft.prefix.trim(),
    access_key: draft.accessKey.trim(),
    secret_key: draft.secretKey.trim(),
    backup_passphrase: '',
    force_path_style: draft.forcePathStyle,
    use_ssl: draft.useSSL,
    skip_tls_verify: draft.skipTLSVerify,
  }
}

function automaticScheduleDraftToInput(draft: AutomaticBackupDraft, saved: AutomaticBackupDraft): AutomaticBackupConfigInput {
  return {
    enabled: draft.enabled,
    cron: draft.cron.trim(),
    retention_count: Number(draft.retentionCount.trim()),
    endpoint: saved.endpoint.trim(),
    region: saved.region.trim(),
    bucket: saved.bucket.trim(),
    prefix: saved.prefix.trim(),
    access_key: saved.accessKey.trim(),
    secret_key: '',
    backup_passphrase: draft.backupPassphrase.trim(),
    force_path_style: saved.forcePathStyle,
    use_ssl: saved.useSSL,
    skip_tls_verify: saved.skipTLSVerify,
  }
}

function validateAutomaticStorageRequiredFields(draft: AutomaticBackupDraft, config: AutomaticBackupConfig | undefined, t: (key: string) => string) {
  const missing: string[] = []
  if (!draft.endpoint.trim()) missing.push(t('backup.automatic.storage.endpoint'))
  if (!draft.bucket.trim()) missing.push(t('backup.automatic.storage.bucket'))
  if (!draft.accessKey.trim()) missing.push(t('backup.automatic.storage.accessKey'))
  if (!draft.secretKey.trim() && !config?.has_secret_key) missing.push(t('backup.automatic.storage.secretKey'))
  return missing
}

function validateAutomaticScheduleRequiredFields(draft: AutomaticBackupDraft, config: AutomaticBackupConfig | undefined, t: (key: string) => string) {
  const missing: string[] = []
  if (!draft.backupPassphrase.trim() && !config?.has_backup_passphrase) missing.push(t('backup.automatic.schedule.passphrase'))
  return missing
}

function sameAutomaticStorage(left: AutomaticBackupDraft, right: AutomaticBackupDraft) {
  return (
    left.endpoint === right.endpoint
    && left.region === right.region
    && left.bucket === right.bucket
    && left.prefix === right.prefix
    && left.accessKey === right.accessKey
    && left.secretKey === right.secretKey
    && left.forcePathStyle === right.forcePathStyle
    && left.useSSL === right.useSSL
    && left.skipTLSVerify === right.skipTLSVerify
  )
}

function sameAutomaticSchedule(left: AutomaticBackupDraft, right: AutomaticBackupDraft) {
  return (
    left.enabled === right.enabled
    && left.cron === right.cron
    && left.retentionCount === right.retentionCount
    && left.backupPassphrase === right.backupPassphrase
  )
}

function reconcileAutomaticDraft(current: AutomaticBackupDraft | null, saved: AutomaticBackupDraft, savedPart: 'storage' | 'schedule') {
  if (!current) return null
  const next = { ...current }
  if (savedPart === 'storage') {
    next.endpoint = saved.endpoint
    next.region = saved.region
    next.bucket = saved.bucket
    next.prefix = saved.prefix
    next.accessKey = saved.accessKey
    next.secretKey = ''
    next.forcePathStyle = saved.forcePathStyle
    next.useSSL = saved.useSSL
    next.skipTLSVerify = saved.skipTLSVerify
  } else {
    next.enabled = saved.enabled
    next.cron = saved.cron
    next.retentionCount = saved.retentionCount
    next.backupPassphrase = ''
  }
  return sameAutomaticStorage(next, saved) && sameAutomaticSchedule(next, saved) ? null : next
}

function describeCron(value: string, locale: string, t: (key: string, options?: Record<string, unknown>) => string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return { valid: false, text: t('backup.automatic.validation.cronRequired') }
  }
  try {
    return { valid: true, text: cronstrue.toString(trimmed, { locale }) }
  } catch (error) {
    return { valid: false, text: t('backup.automatic.validation.cronInvalidWithMessage', { message: error instanceof Error ? error.message : String(error) }) }
  }
}

function validatePositiveInteger(value: string, t: (key: string, options?: Record<string, unknown>) => string, max?: number) {
  const trimmed = value.trim()
  if (!trimmed) return t('backup.automatic.validation.retentionRequired')
  if (!/^\d+$/.test(trimmed)) return t('backup.automatic.validation.retentionPositive')
  const parsed = Number(trimmed)
  if (!Number.isSafeInteger(parsed) || parsed <= 0) return t('backup.automatic.validation.retentionPositive')
  if (max && parsed > max) return t('backup.automatic.validation.retentionMax', { max })
  return undefined
}

function formatDateTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
