import { useCallback, useEffect, useRef, useState } from 'react'
import { FileJson, LoaderCircle, Upload, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Sheet, SheetBody, SheetContent, SheetFooter, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { TextArea } from '@/components/ui/textarea'
import { useMobileLayout } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'
import type { ImportOAuthAccountsInput } from '@/features/oauth/api/oauth'
import { OAuthProxySelect } from '@/features/oauth/components/oauth-proxy-select'
import type { ImportResult } from '@/features/oauth/lib/types'

type FileItem = {
  file: File
  id: string
}

type ImportMode = 'files' | 'json'

const IMPORT_INPUT_AREA_CLASS = 'h-[156px] min-h-[156px]'

function filesFromClipboard(dataTransfer: DataTransfer) {
  const files = Array.from(dataTransfer.files)
  const itemFiles = Array.from(dataTransfer.items)
    .filter((item) => item.kind === 'file')
    .map((item) => item.getAsFile())
    .filter((file): file is File => Boolean(file))

  if (files.length === 0) return itemFiles
  const seen = new Set(files.map((file) => `${file.name}:${file.size}:${file.lastModified}`))
  return [
    ...files,
    ...itemFiles.filter((file) => !seen.has(`${file.name}:${file.size}:${file.lastModified}`)),
  ]
}

function isEditablePasteTarget(target: EventTarget | null) {
  const element = target as HTMLElement | null
  const tagName = element?.tagName.toLowerCase()
  return tagName === 'input' || tagName === 'textarea' || Boolean(element?.isContentEditable)
}

function importFileKey(file: File) {
  return `${file.name}:${file.size}:${file.lastModified}:${file.type}`
}

export function OAuthImportSheet({
  open,
  pending,
  result,
  onOpenChange,
  onImport,
}: {
  open: boolean
  pending: boolean
  result: ImportResult | null
  onOpenChange: (open: boolean) => void
  onImport: (input: ImportOAuthAccountsInput) => void
}) {
  const { t } = useTranslation('oauth')
  const isMobile = useMobileLayout()
  const [mode, setMode] = useState<ImportMode>('files')
  const [files, setFiles] = useState<FileItem[]>([])
  const [jsonText, setJsonText] = useState('')
  const [jsonError, setJsonError] = useState('')
  const [proxyId, setProxyId] = useState('direct')
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const addFiles = useCallback((incoming: FileList | File[]) => {
    const newFiles: FileItem[] = []
    const incomingSeen = new Set<string>()
    for (const file of Array.from(incoming)) {
      if (file.name.endsWith('.json') || file.type === 'application/json') {
        const key = importFileKey(file)
        if (incomingSeen.has(key)) continue
        incomingSeen.add(key)
        newFiles.push({ file, id: `${file.name}-${file.size}-${file.lastModified}-${Math.random().toString(36).slice(2)}` })
      }
    }
    if (newFiles.length > 0) {
      setFiles((prev) => {
        const existing = new Set(prev.map((item) => importFileKey(item.file)))
        return [
          ...prev,
          ...newFiles.filter((item) => !existing.has(importFileKey(item.file))),
        ]
      })
    }
  }, [])

  const removeFile = useCallback((id: string) => {
    setFiles((prev) => prev.filter((f) => f.id !== id))
  }, [])

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
    if (e.dataTransfer.files.length > 0) {
      addFiles(e.dataTransfer.files)
    }
  }, [addFiles])

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragging(true)
  }, [])

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
  }, [])

  const handleInputChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) {
      addFiles(e.target.files)
      e.target.value = ''
    }
  }, [addFiles])

  const acceptPastedFiles = useCallback((event: ClipboardEvent | React.ClipboardEvent) => {
    if (mode !== 'files' || result || pending) return
    if (isEditablePasteTarget(event.target)) return
    const clipboardData = event.clipboardData
    if (!clipboardData) return
    const pastedFiles = filesFromClipboard(clipboardData)
    if (pastedFiles.length === 0) return
    event.preventDefault()
    addFiles(pastedFiles)
  }, [addFiles, mode, pending, result])

  const handlePasteFiles = useCallback((event: React.ClipboardEvent) => {
    acceptPastedFiles(event)
  }, [acceptPastedFiles])

  useEffect(() => {
    if (!open || mode !== 'files' || result || pending) return
    const listener = (event: ClipboardEvent) => acceptPastedFiles(event)
    document.addEventListener('paste', listener)
    return () => document.removeEventListener('paste', listener)
  }, [acceptPastedFiles, mode, open, pending, result])

  const handleImport = useCallback(() => {
    if (mode === 'files') {
      if (files.length > 0) {
        onImport({ mode: 'files', files: files.map((f) => f.file), proxyId: proxyId === 'direct' ? null : proxyId })
      }
      return
    }

    const trimmed = jsonText.trim()
    if (!trimmed) {
      setJsonError(t('import.jsonRequired'))
      return
    }

    try {
      JSON.parse(trimmed)
    } catch {
      setJsonError(t('import.jsonInvalid'))
      return
    }

    setJsonError('')
    onImport({ mode: 'json', jsonText: trimmed, proxyId: proxyId === 'direct' ? null : proxyId })
  }, [files, jsonText, mode, onImport, proxyId, t])

  const handleClose = useCallback((nextOpen: boolean) => {
    onOpenChange(nextOpen)
    if (!nextOpen) {
      setMode('files')
      setFiles([])
      setJsonText('')
      setJsonError('')
      setProxyId('direct')
    }
  }, [onOpenChange])

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  }

  const bodyClassName = 'flex min-w-0 max-w-full flex-col gap-4 overflow-hidden'
  const body = (
    <>
      {!result ? (
        <div className="flex min-h-0 flex-1 flex-col gap-4">
          <Tabs
            value={mode}
            onValueChange={(value) => {
              setMode(value as ImportMode)
              setJsonError('')
            }}
          >
            <TabsList className="w-full">
              <TabsTrigger value="files">{t('import.mode.files')}</TabsTrigger>
              <TabsTrigger value="json">{t('import.mode.json')}</TabsTrigger>
            </TabsList>
          </Tabs>

          {mode === 'files' ? (
            <div className="flex min-h-0 flex-1 flex-col gap-4">
              <div
                onDrop={handleDrop}
                onDragOver={handleDragOver}
                onDragLeave={handleDragLeave}
                onClick={() => inputRef.current?.click()}
                className={cn(
                  IMPORT_INPUT_AREA_CLASS,
                  'flex cursor-pointer flex-col items-center justify-center gap-2 rounded-inner border-2 border-dashed px-4 py-8 transition-colors',
                  dragging
                    ? 'border-tint bg-tint/5'
                    : 'border-separator bg-elevated-1 hover:border-tint/50 hover:bg-fill-3',
                )}
              >
                <Upload className="h-8 w-8 text-label-2" />
                <div className="text-body font-medium text-label">
                  {t('import.dropzone')}
                </div>
                <div className="text-footnote text-label-2">
                  {t('import.fileHint')}
                </div>
                <input
                  ref={inputRef}
                  type="file"
                  multiple
                  accept=".json,application/json"
                  className="hidden"
                  onChange={handleInputChange}
                />
              </div>

              <OAuthProxySelect
                value={proxyId}
                disabled={pending}
                onChange={setProxyId}
              />

              {files.length > 0 ? (
                <div className="flex min-h-0 flex-1 flex-col gap-2">
                  <div className="text-body font-medium text-label">
                    {t('import.selectedCount', { count: files.length })}
                  </div>
                  <div className="min-h-0 flex-1 space-y-1 overflow-y-auto pr-1">
                    {files.map((item) => (
                      <div
                        key={item.id}
                        className="flex items-center gap-3 rounded-inner border border-separator bg-elevated-1 px-3 py-2"
                      >
                        <FileJson className="h-4 w-4 shrink-0 text-label-2" />
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-body text-label">{item.file.name}</div>
                          <div className="text-footnote text-label-2">{formatSize(item.file.size)}</div>
                        </div>
                        <button
                          type="button"
                          onClick={(e) => { e.stopPropagation(); removeFile(item.id) }}
                          className="shrink-0 rounded p-1 text-label-2 transition-colors hover:bg-fill-3 hover:text-label"
                        >
                          <X className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    ))}
                  </div>
                </div>
              ) : null}
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 flex-col gap-2">
              <TextArea
                value={jsonText}
                onChange={(event) => {
                  setJsonText(event.target.value)
                  if (jsonError) setJsonError('')
                }}
                placeholder={t('import.jsonPlaceholder')}
                className={cn(IMPORT_INPUT_AREA_CLASS, 'resize-none font-mono text-footnote leading-relaxed')}
                spellCheck={false}
              />
              {jsonError ? <div className="text-footnote text-danger">{jsonError}</div> : null}
              <OAuthProxySelect
                value={proxyId}
                disabled={pending}
                onChange={setProxyId}
              />
            </div>
          )}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col gap-4">
          <div className="flex items-center gap-3 rounded-inner border border-separator bg-fill-3 px-4 py-3">
            <div className="text-body">
              {t('import.result.total', { total: result.meta.total })}，
              <span className="font-semibold text-green-500">{t('import.result.succeeded', { count: result.meta.succeeded })}</span>，
              <span className="font-semibold text-danger">{t('import.result.failed', { count: result.meta.failed })}</span>
            </div>
          </div>

          <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
            {result.items.map((item, index) => (
              <div
                key={index}
                className="flex items-start gap-3 rounded-inner border border-separator bg-elevated-1 px-3 py-2"
              >
                <div className="min-w-0 flex-1 space-y-0.5">
                  <div className="flex items-center gap-2">
                    <span className="text-body font-medium text-label">{item.email || '-'}</span>
                    {item.provider && (
                      <Badge variant="neutral" className="text-footnote">{item.provider}</Badge>
                    )}
                    <Badge
                      variant={item.status === 'queued' ? 'info' : 'warning'}
                      className="text-footnote"
                    >
                      {t(`import.result.status.${item.status}`, { defaultValue: t('import.result.status.unknown') })}
                    </Badge>
                  </div>
                  {item.site_name && (
                    <div className="text-footnote text-label-2">{t('import.result.site', { name: item.site_name })}</div>
                  )}
                  {item.error && (
                    <div className="text-footnote text-danger">{item.error}</div>
                  )}
                  {item.warning && (
                    <div className="text-footnote text-warning">{item.warning}</div>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </>
  )

  const importButton = (
    <Button
      onClick={handleImport}
      disabled={(mode === 'files' ? files.length === 0 : jsonText.trim().length === 0) || pending}
    >
      {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
      {mode === 'files' && files.length > 0
        ? t('import.actions.importWithCount', { count: files.length })
        : t('import.actions.import')}
    </Button>
  )

  const cancelButton = (
    <Button
      variant="secondary"
      onClick={() => handleClose(false)}
      disabled={pending}
    >
      {t('import.actions.cancel')}
    </Button>
  )

  const doneButton = (
    <Button onClick={() => handleClose(false)}>
      {t('import.actions.done')}
    </Button>
  )

  const footer = !result ? (
    <>
      {importButton}
      {cancelButton}
    </>
  ) : doneButton

  if (isMobile) {
    return (
      <Sheet open={open} onOpenChange={handleClose}>
        <SheetContent onOpenAutoFocus={(event) => event.preventDefault()}>
          <SheetHeader>
            <SheetTitle>{t('import.title')}</SheetTitle>
          </SheetHeader>
          <SheetBody className={bodyClassName} onPaste={handlePasteFiles}>
            {body}
          </SheetBody>
          <SheetFooter>
            {footer}
          </SheetFooter>
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent size="md" onOpenAutoFocus={(event) => event.preventDefault()}>
        <DialogHeader>
          <DialogTitle>{t('import.title')}</DialogTitle>
        </DialogHeader>
        <DialogBody className={cn(bodyClassName, 'min-h-0 flex-1')} onPaste={handlePasteFiles}>
          {body}
        </DialogBody>
        <DialogFooter cancel={!result ? cancelButton : undefined} confirm={!result ? importButton : doneButton} />
      </DialogContent>
    </Dialog>
  )
}
