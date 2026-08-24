import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, Circle, DatabaseBackup, LoaderCircle, RotateCcw, XCircle } from 'lucide-react'

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
import { Progress } from '@/components/ui/progress'
import {
  cancelAutomaticBackupRestoreTask,
  fetchAutomaticBackupRestoreTask,
  importBackupSSE,
  startAutomaticBackupRestore,
  type BackupImportSummary,
  type AutomaticRestoreTask,
  type RestoreProgressEvent,
} from '@/features/settings/api/settings'

type StepId = 'download' | 'decrypt' | 'parse' | 'import' | 'files'
type StepState = 'pending' | 'in_progress' | 'complete' | 'error' | 'canceled'
type Outcome = 'running' | 'success' | 'error' | 'canceled'

export type RestoreTask =
  | { type: 'automatic'; key: string; filename: string; taskID?: string }
  | { type: 'manual'; file: File; passphrase: string }

export type BackgroundRestoreTask = Extract<RestoreTask, { type: 'automatic' }> & { taskID: string }

const STEPS: { id: StepId; tKey: string }[] = [
  { id: 'download', tKey: 'backup.automatic.restoreProgress.stepDownload' },
  { id: 'decrypt', tKey: 'backup.automatic.restoreProgress.stepDecrypt' },
  { id: 'parse', tKey: 'backup.automatic.restoreProgress.stepParse' },
  { id: 'import', tKey: 'backup.automatic.restoreProgress.stepImport' },
  { id: 'files', tKey: 'backup.automatic.restoreProgress.stepFiles' },
]

type RestoreProgressDialogProps = {
  task: RestoreTask | null
  onClose: () => void
  onBackground: (task: BackgroundRestoreTask) => void
}

type RestoreProgressRunProps = {
  task: RestoreTask
  onClose: () => void
  onRetry: () => void
  onBackground: (task: BackgroundRestoreTask) => void
}

function initialSteps(task: RestoreTask): Record<StepId, StepState> {
  return {
    download: task.type === 'manual' ? 'complete' : 'pending',
    decrypt: 'pending',
    parse: 'pending',
    import: 'pending',
    files: 'pending',
  }
}

export function RestoreProgressDialog({ task, onClose, onBackground }: RestoreProgressDialogProps) {
  const [attempt, setAttempt] = useState(0)
  const [retryFresh, setRetryFresh] = useState(false)
  const runTask = useMemo(() => {
    if (!task) return null
    return retryFresh && task.type === 'automatic' ? { ...task, taskID: undefined } : task
  }, [retryFresh, task])
  if (!runTask) return null
  const taskKey = runTask.type === 'automatic'
    ? runTask.key
    : `${runTask.file.name}:${runTask.file.size}:${runTask.file.lastModified}`
  return (
    <RestoreProgressRun
      key={`${runTask.type}:${taskKey}:${attempt}`}
      task={runTask}
      onClose={onClose}
      onBackground={onBackground}
      onRetry={() => {
        setRetryFresh(true)
        setAttempt((value) => value + 1)
      }}
    />
  )
}

function RestoreProgressRun({ task, onClose, onRetry, onBackground }: RestoreProgressRunProps) {
  const { t, i18n } = useTranslation('settings')
  const [steps, setSteps] = useState<Record<StepId, StepState>>(() => initialSteps(task))
  const [rows, setRows] = useState(0)
  const [totalRows, setTotalRows] = useState(0)
  const [currentTable, setCurrentTable] = useState('')
  const [downloadBytes, setDownloadBytes] = useState(0)
  const [downloadTotal, setDownloadTotal] = useState(0)
  const [summary, setSummary] = useState<BackupImportSummary | null>(null)
  const [errorMessage, setErrorMessage] = useState('')
  const [outcome, setOutcome] = useState<Outcome>('running')
  const [automaticRestore, setAutomaticRestore] = useState<AutomaticRestoreTask | null>(null)
  const [cancelPending, setCancelPending] = useState(false)
  const [cancelError, setCancelError] = useState('')
  const automaticStartRef = useRef<ReturnType<typeof startAutomaticBackupRestore> | null>(null)

  useEffect(() => {
    const currentTask = task
    const abort = new AbortController()
    let activeStep: StepId = currentTask.type === 'automatic' ? 'download' : 'decrypt'
    let activeStepIndex = currentTask.type === 'automatic' ? 0 : 1

    async function run() {
      try {
        const applyEvent = (event: RestoreProgressEvent) => {
          if (event.step === 'complete') {
            setSummary(event.summary ?? null)
            setRows(event.summary?.rows ?? event.rows ?? 0)
            setSteps({ download: 'complete', decrypt: 'complete', parse: 'complete', import: 'complete', files: 'complete' })
            setOutcome('success')
            return true
          }

          const activeEventStep = event.step
          const stepIndex = STEPS.findIndex((step) => step.id === activeEventStep)
          if (stepIndex < activeStepIndex) return false
          activeStep = activeEventStep
          activeStepIndex = stepIndex
          setSteps((current) => advanceRestoreSteps(current, activeEventStep, event.status))
          if (event.step === 'download') {
            if (event.bytes !== undefined) setDownloadBytes(event.bytes)
            if (event.total_bytes !== undefined) setDownloadTotal(event.total_bytes)
          }
          if (event.step === 'import') {
            setRows(event.rows ?? 0)
            if (event.total_rows !== undefined) setTotalRows(event.total_rows)
            setCurrentTable(event.table ?? '')
          }
          if (event.status === 'error') {
            setErrorMessage(event.message ?? '')
            setOutcome('error')
            return true
          }
          return false
        }

        const applyTerminalStep = (event: RestoreProgressEvent, state: Extract<StepState, 'error' | 'canceled'>) => {
          const terminalStep = event.step === 'complete' ? activeStep : event.step
          activeStep = terminalStep
          const stepIndex = STEPS.findIndex((step) => step.id === terminalStep)
          setSteps((current) => {
            const next = { ...current }
            for (let index = 0; index < stepIndex; index += 1) next[STEPS[index].id] = 'complete'
            next[terminalStep] = state
            return next
          })
        }

        if (currentTask.type === 'automatic') {
          if (!currentTask.taskID && !automaticStartRef.current) {
            automaticStartRef.current = startAutomaticBackupRestore(currentTask.key)
          }
          let { restore } = currentTask.taskID
            ? await fetchAutomaticBackupRestoreTask(currentTask.taskID, abort.signal)
            : await automaticStartRef.current!
          if (abort.signal.aborted) return
          setAutomaticRestore(restore)
          while (!abort.signal.aborted) {
            if (restore.status === 'completed') {
              setSummary(restore.summary ?? null)
              setRows(restore.summary?.rows ?? 0)
              setSteps({ download: 'complete', decrypt: 'complete', parse: 'complete', import: 'complete', files: 'complete' })
              setOutcome('success')
              return
            }
            if (restore.status === 'canceled') {
              applyTerminalStep(restore.progress, 'canceled')
              setErrorMessage(restore.error ?? restore.progress.message ?? '')
              setOutcome('canceled')
              return
            }
            if (restore.status === 'failed') {
              applyTerminalStep(restore.progress, 'error')
              setErrorMessage(restore.error ?? restore.progress.message ?? '')
              setOutcome('error')
              return
            }
            if (applyEvent(restore.progress)) return
            await new Promise((resolve) => window.setTimeout(resolve, 1000))
            if (abort.signal.aborted) return
            const result = await fetchAutomaticBackupRestoreTask(restore.id, abort.signal)
            restore = result.restore
            setAutomaticRestore(restore)
          }
          return
        }

        const stream = importBackupSSE({ file: currentTask.file, passphrase: currentTask.passphrase }, abort.signal)
        for await (const event of stream) {
          if (applyEvent(event)) return
        }
      } catch (error) {
        if (abort.signal.aborted) return
        setSteps((current) => ({ ...current, [activeStep]: 'error' }))
        setErrorMessage(error instanceof Error ? error.message : '')
        setOutcome('error')
      }
    }

    void run()
    return () => abort.abort()
  }, [task])

  useEffect(() => {
    if (outcome === 'success') window.location.reload()
  }, [outcome])

  async function handleCancel() {
    if (!automaticRestore || !automaticRestore.cancellable || cancelPending) return
    setCancelPending(true)
    setCancelError('')
    try {
      const result = await cancelAutomaticBackupRestoreTask(automaticRestore.id)
      setAutomaticRestore(result.restore)
    } catch {
      setCancelError(t('backup.automatic.restoreProgress.cancelUnavailable'))
    } finally {
      setCancelPending(false)
    }
  }

  function handleBackground() {
    if (task.type !== 'automatic' || !automaticRestore) return
    onBackground({ ...task, taskID: automaticRestore.id })
  }

  const visibleSteps = task.type === 'manual' ? STEPS.filter((step) => step.id !== 'download') : STEPS
  const displayedRows = useAnimatedInteger(rows)
  const filename = task.type === 'automatic' ? task.filename : task.file.name
  const title = outcome === 'success'
    ? t('backup.automatic.restoreProgress.completeTitle')
    : outcome === 'canceled'
      ? t('backup.automatic.restoreProgress.canceledTitle')
    : outcome === 'error'
      ? t('backup.automatic.restoreProgress.errorTitle')
      : t('backup.automatic.restoreProgress.title')
  const description = outcome === 'success' && summary
    ? t('backup.automatic.restoreProgress.completeDescription', {
        tables: summary.tables.toLocaleString(i18n.language),
        rows: summary.rows.toLocaleString(i18n.language),
      })
    : outcome === 'error'
      ? t('backup.automatic.restoreProgress.errorDescription')
      : outcome === 'canceled'
        ? t('backup.automatic.restoreProgress.canceledDescription')
      : t('backup.automatic.restoreProgress.description', { filename })

  return (
    <Dialog open onOpenChange={(open) => { if (!open && outcome !== 'running') onClose() }}>
      <DialogContent className="w-[min(92vw,560px)]">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        <DialogBody className="space-y-4">
          <div className="truncate rounded-xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-3 py-2 font-mono text-xs text-muted-soft">
            {filename}
          </div>

          <div className="overflow-hidden rounded-2xl border border-[hsl(var(--glass-border))]" aria-live="polite">
            {visibleSteps.map((step, index) => {
              const state = steps[step.id]
              return (
                <div
                  key={step.id}
                  className={`flex items-center gap-3 px-4 py-3 ${index ? 'border-t border-[hsl(var(--glass-divider))]' : ''}`}
                >
                  <StepIcon state={state} />
                  <span className={`min-w-0 flex-1 text-sm ${state === 'in_progress' ? 'font-medium text-foreground' : 'text-muted-soft'}`}>
                    {t(step.tKey)}
                  </span>
                  <span className={`text-xs ${state === 'error' ? 'text-[hsl(var(--destructive))]' : state === 'complete' ? 'text-[hsl(var(--badge-success-text))]' : 'text-muted-soft'}`}>
                    {t(`backup.automatic.restoreProgress.status.${state}`)}
                  </span>
                </div>
              )
            })}
          </div>

          {steps.download === 'in_progress' && downloadTotal > 0 ? (
            <div role="status" className="space-y-2 rounded-xl bg-[hsl(var(--surface-subtle))] px-4 py-3">
              <Progress value={downloadBytes} max={downloadTotal} variant="accent" showValue />
              <p className="text-xs text-muted-soft">
                {t('backup.automatic.restoreProgress.downloaded', {
                  downloaded: formatBytes(downloadBytes, i18n.language),
                  total: formatBytes(downloadTotal, i18n.language),
                })}
              </p>
            </div>
          ) : null}

          {steps.import === 'in_progress' ? (
            <div role="status" className="space-y-3 rounded-xl bg-[hsl(var(--surface-subtle))] px-4 py-3">
              <div className="flex items-center gap-3">
                <DatabaseBackup aria-hidden className="h-4 w-4 text-primary" />
                <p className="min-w-0 text-sm text-foreground">{currentTableLabel(currentTable, t)}</p>
              </div>
              {totalRows > 0 ? (
                <Progress value={displayedRows} max={totalRows} variant="accent" showValue />
              ) : null}
              <div className="pl-7">
                <p className="mt-0.5 text-xs text-muted-soft">
                  {totalRows > 0
                    ? t('backup.automatic.restoreProgress.rowsImportedWithTotal', {
                        count: displayedRows.toLocaleString(i18n.language),
                        total: totalRows.toLocaleString(i18n.language),
                      })
                    : t('backup.automatic.restoreProgress.rowsImported', { count: displayedRows.toLocaleString(i18n.language) })}
                </p>
              </div>
            </div>
          ) : null}

          {outcome === 'error' ? (
            <div role="alert" className="rounded-xl border border-[hsl(var(--destructive)/0.35)] bg-[hsl(var(--destructive)/0.08)] px-4 py-3 text-sm leading-6 text-[hsl(var(--destructive))]">
              {errorMessage || t('backup.automatic.restoreProgress.unknownError')}
            </div>
          ) : null}
          {cancelError ? (
            <div role="alert" className="rounded-xl border border-[hsl(var(--destructive)/0.35)] bg-[hsl(var(--destructive)/0.08)] px-4 py-3 text-sm leading-6 text-[hsl(var(--destructive))]">
              {cancelError}
            </div>
          ) : null}
        </DialogBody>

        <DialogFooter>
          {outcome === 'running' && task.type === 'automatic' ? (
            <>
              <Button variant="outline" onClick={handleBackground} disabled={!automaticRestore}>
                {t('backup.automatic.restoreProgress.background')}
              </Button>
              <Button
                variant="destructive"
                onClick={handleCancel}
                disabled={!automaticRestore?.cancellable || cancelPending || automaticRestore.status === 'canceling'}
              >
                {cancelPending || automaticRestore?.status === 'canceling' ? <LoaderCircle aria-hidden className="h-4 w-4 animate-spin" /> : <XCircle aria-hidden className="h-4 w-4" />}
                {cancelPending || automaticRestore?.status === 'canceling'
                  ? t('backup.automatic.restoreProgress.canceling')
                  : t('backup.automatic.restoreProgress.cancel')}
              </Button>
            </>
          ) : outcome === 'running' ? (
            <p role="status" className="text-xs text-muted-soft">
              {t('backup.automatic.restoreProgress.keepOpen')}
            </p>
          ) : outcome === 'error' || outcome === 'canceled' ? (
            <>
              <Button variant="outline" onClick={onClose}>{t('backup.automatic.restoreProgress.close')}</Button>
              <Button onClick={onRetry}>
                <RotateCcw aria-hidden className="h-4 w-4" />
                {t('backup.automatic.restoreProgress.retry')}
              </Button>
            </>
          ) : (
            <p role="status" className="flex items-center gap-2 text-xs text-muted-soft">
              <LoaderCircle aria-hidden className="h-4 w-4 animate-spin" />
              {t('backup.automatic.restoreProgress.reload')}
            </p>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function StepIcon({ state }: { state: StepState }) {
  if (state === 'complete') {
    return <CheckCircle2 aria-hidden className="h-5 w-5 text-[hsl(var(--badge-success-text))]" />
  }
  if (state === 'in_progress') {
    return <LoaderCircle aria-hidden className="h-5 w-5 animate-spin text-primary" />
  }
  if (state === 'error') {
    return <XCircle aria-hidden className="h-5 w-5 text-[hsl(var(--destructive))]" />
  }
  if (state === 'canceled') {
    return <XCircle aria-hidden className="h-5 w-5 text-muted-soft" />
  }
  return <Circle aria-hidden className="h-5 w-5 text-muted-soft" />
}

function advanceRestoreSteps(current: Record<StepId, StepState>, step: StepId, status: RestoreProgressEvent['status']) {
  const stepIndex = STEPS.findIndex((item) => item.id === step)
  const laterStepStarted = STEPS.slice(stepIndex + 1).some((item) => current[item.id] !== 'pending')
  if (laterStepStarted || (current[step] === 'complete' && status === 'in_progress')) return current
  const next = { ...current }
  for (let index = 0; index < stepIndex; index += 1) next[STEPS[index].id] = 'complete'
  next[step] = status
  return next
}

function currentTableLabel(table: string, t: (key: string) => string) {
  if (table === 'request_logs') return t('backup.automatic.restoreProgress.currentRequestLogs')
  if (table === 'usage_records') return t('backup.automatic.restoreProgress.currentUsageRecords')
  return t('backup.automatic.restoreProgress.currentBaseData')
}

function formatBytes(value: number, locale: string) {
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let size = value / 1024
  let unit = units[0]
  for (let index = 1; index < units.length && size >= 1024; index += 1) {
    size /= 1024
    unit = units[index]
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(size)} ${unit}`
}

function useAnimatedInteger(target: number) {
  const [value, setValue] = useState(target)
  const valueRef = useRef(target)

  useEffect(() => {
    if (target === valueRef.current) return
    const start = valueRef.current
    const startedAt = performance.now()
    let frame = 0
    const update = (now: number) => {
      const elapsed = Math.min((now - startedAt) / 600, 1)
      const eased = 1 - ((1 - elapsed) ** 3)
      const next = Math.round(start + ((target - start) * eased))
      valueRef.current = next
      setValue(next)
      if (elapsed < 1) frame = requestAnimationFrame(update)
    }
    frame = requestAnimationFrame(update)
    return () => cancelAnimationFrame(frame)
  }, [target])

  return value
}
