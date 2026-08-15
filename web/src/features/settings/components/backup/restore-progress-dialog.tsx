import { useEffect, useRef, useState } from 'react'
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
  importBackupSSE,
  restoreAutomaticBackupFileSSE,
  type BackupImportSummary,
} from '@/features/settings/api/settings'

type StepId = 'download' | 'decrypt' | 'parse' | 'import'
type StepState = 'pending' | 'in_progress' | 'complete' | 'error'
type Outcome = 'running' | 'success' | 'error'

export type RestoreTask =
  | { type: 'automatic'; key: string; filename: string }
  | { type: 'manual'; file: File; passphrase: string }

const STEPS: { id: StepId; tKey: string }[] = [
  { id: 'download', tKey: 'backup.automatic.restoreProgress.stepDownload' },
  { id: 'decrypt', tKey: 'backup.automatic.restoreProgress.stepDecrypt' },
  { id: 'parse', tKey: 'backup.automatic.restoreProgress.stepParse' },
  { id: 'import', tKey: 'backup.automatic.restoreProgress.stepImport' },
]

type RestoreProgressDialogProps = {
  task: RestoreTask | null
  onClose: () => void
}

type RestoreProgressRunProps = {
  task: RestoreTask
  onClose: () => void
  onRetry: () => void
}

function initialSteps(task: RestoreTask): Record<StepId, StepState> {
  return {
    download: task.type === 'manual' ? 'complete' : 'pending',
    decrypt: 'pending',
    parse: 'pending',
    import: 'pending',
  }
}

export function RestoreProgressDialog({ task, onClose }: RestoreProgressDialogProps) {
  const [attempt, setAttempt] = useState(0)
  if (!task) return null

  const taskKey = task.type === 'automatic'
    ? task.key
    : `${task.file.name}:${task.file.size}:${task.file.lastModified}`
  return (
    <RestoreProgressRun
      key={`${task.type}:${taskKey}:${attempt}`}
      task={task}
      onClose={onClose}
      onRetry={() => setAttempt((value) => value + 1)}
    />
  )
}

function RestoreProgressRun({ task, onClose, onRetry }: RestoreProgressRunProps) {
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

  useEffect(() => {
    const currentTask = task
    const abort = new AbortController()
    let activeStep: StepId = currentTask.type === 'automatic' ? 'download' : 'decrypt'

    async function run() {
      try {
        const stream = currentTask.type === 'automatic'
          ? restoreAutomaticBackupFileSSE(currentTask.key, abort.signal)
          : importBackupSSE({ file: currentTask.file, passphrase: currentTask.passphrase }, abort.signal)

        for await (const event of stream) {
          if (event.step === 'complete') {
            setSummary(event.summary ?? null)
            setRows(event.summary?.rows ?? event.rows ?? 0)
            setSteps((current) => ({ ...current, import: 'complete' }))
            setOutcome('success')
            return
          }

          activeStep = event.step
          setSteps((current) => ({ ...current, [event.step]: event.status }))
          if (event.step === 'download') {
            setDownloadBytes(event.bytes ?? 0)
            setDownloadTotal(event.total_bytes ?? 0)
          }
          if (event.step === 'import') {
            setRows(event.rows ?? 0)
            if (event.total_rows !== undefined) setTotalRows(event.total_rows)
            setCurrentTable(event.table ?? '')
          }
          if (event.status === 'error') {
            setErrorMessage(event.message ?? '')
            setOutcome('error')
            return
          }
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

  const visibleSteps = task.type === 'manual' ? STEPS.filter((step) => step.id !== 'download') : STEPS
  const displayedRows = useAnimatedInteger(rows)
  const filename = task.type === 'automatic' ? task.filename : task.file.name
  const title = outcome === 'success'
    ? t('backup.automatic.restoreProgress.completeTitle')
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
        </DialogBody>

        <DialogFooter className={outcome === 'running' ? 'justify-start' : undefined}>
          {outcome === 'running' ? (
            <p role="status" className="text-xs text-muted-soft">
              {t('backup.automatic.restoreProgress.keepOpen')}
            </p>
          ) : outcome === 'error' ? (
            <>
              <Button variant="outline" onClick={onClose}>{t('backup.automatic.restoreProgress.close')}</Button>
              <Button onClick={onRetry}>
                <RotateCcw aria-hidden className="h-4 w-4" />
                {t('backup.automatic.restoreProgress.retry')}
              </Button>
            </>
          ) : (
            <>
              <Button variant="outline" onClick={onClose}>{t('backup.automatic.restoreProgress.close')}</Button>
              <Button onClick={() => window.location.reload()}>{t('backup.automatic.restoreProgress.reload')}</Button>
            </>
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
  return <Circle aria-hidden className="h-5 w-5 text-muted-soft" />
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
