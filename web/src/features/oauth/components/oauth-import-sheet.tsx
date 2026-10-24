import { useCallback, useState } from 'react'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Sheet, SheetBody, SheetContent, SheetFooter, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { TextArea } from '@/components/ui/textarea'
import { useMobileLayout } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'
import { JSON_DROP_AREA_CLASS, JsonFileDropArea, JsonFileList } from '@/components/common/json-file-drop'
import { useJsonFiles } from '@/components/common/use-json-files'
import type { ImportOAuthAccountsInput } from '@/features/oauth/api/oauth'
import { OAuthProxySelect } from '@/features/oauth/components/oauth-proxy-select'
import type { ImportResult } from '@/features/oauth/lib/types'

type ImportMode = 'files' | 'json'

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
  const { files, addFiles, removeFile, clearFiles } = useJsonFiles()
  const [jsonText, setJsonText] = useState('')
  const [jsonError, setJsonError] = useState('')
  const [proxyId, setProxyId] = useState('direct')

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
      clearFiles()
      setJsonText('')
      setJsonError('')
      setProxyId('direct')
    }
  }, [clearFiles, onOpenChange])

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
              <JsonFileDropArea
                onFiles={addFiles}
                active={open && !result}
                disabled={pending}
                title={t('import.dropzone')}
                hint={t('import.fileHint')}
              />

              <OAuthProxySelect
                value={proxyId}
                disabled={pending}
                onChange={setProxyId}
              />

              <JsonFileList
                files={files}
                countLabel={t('import.selectedCount', { count: files.length })}
                onRemove={removeFile}
              />
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
                className={cn(JSON_DROP_AREA_CLASS, 'resize-none font-mono text-footnote leading-relaxed')}
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
          <SheetBody className={bodyClassName}>
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
        <DialogBody className={cn(bodyClassName, 'min-h-0 flex-1')}>
          {body}
        </DialogBody>
        <DialogFooter cancel={!result ? cancelButton : undefined} confirm={!result ? importButton : doneButton} />
      </DialogContent>
    </Dialog>
  )
}
