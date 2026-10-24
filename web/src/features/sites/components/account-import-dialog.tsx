import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { JsonFileDropArea, JsonFileList } from '@/components/common/json-file-drop'
import { useJsonFiles } from '@/components/common/use-json-files'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { useNestedDialogClassNames } from '@/features/sites/components/use-nested-dialog'
import { accountUIText, fillAccountText } from '@/features/sites/components/account-provider-login'
import { importAccounts, type AccountImportResponse, type AccountUIText } from '@/features/sites/api/sites'
import { toast } from '@/lib/toast'

/**
 * Imports accounts from JSON files for an account-pool site. Copy falls back to
 * `accounts.importDialog`, and a manifest `ui.texts` entry wins when it is set.
 * Whether the dialog is offered comes from the site type's accounts capability.
 */
export function AccountImportDialog({
  open,
  siteId,
  regionLabel,
  onOpenChange,
  onImported,
  texts,
  language,
}: {
  open: boolean
  siteId: string
  regionLabel: (regionId: string) => string
  onOpenChange: (open: boolean) => void
  onImported: () => void | Promise<void>
  texts?: Record<string, AccountUIText> | null
  language: string
}) {
  const { t } = useTranslation('sites')
  const text = (suffix: string, textKey: string, vars?: Record<string, string | number>) => {
    const custom = accountUIText(texts, textKey, language)
    if (custom) return fillAccountText(custom, vars)
    return vars ? t(`accounts.importDialog.${suffix}`, vars) : t(`accounts.importDialog.${suffix}`)
  }
  const nestedZ = useNestedDialogClassNames()
  const { files, addFiles, removeFile, clearFiles } = useJsonFiles()
  const [result, setResult] = useState<AccountImportResponse | null>(null)

  const importMutation = useMutation({
    mutationFn: async () => {
      const documents = await Promise.all(files.map(async (item) => ({ name: item.file.name, content: await item.file.text() })))
      return importAccounts(siteId, documents)
    },
    onSuccess: async (response) => {
      setResult(response)
      await onImported()
      if (response.meta.failed > 0) {
        toast.warning(text('toastDone', 'import_toast_done'), {
          description: text('summary', 'import_summary', response.meta),
        })
      } else {
        toast.success(text('toastDone', 'import_toast_done'))
      }
    },
    onError: (error) => toast.error(text('toastFailed', 'import_toast_failed'), { description: error.message }),
  })

  function handleOpenChange(next: boolean) {
    if (!next && importMutation.isPending) return
    onOpenChange(next)
    if (!next) {
      clearFiles()
      setResult(null)
      importMutation.reset()
    }
  }

  const pending = importMutation.isPending

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        presentation="center"
        size="md"
        overlayClassName={nestedZ.overlayClassName}
        className={nestedZ.className}
        onOpenAutoFocus={(event) => event.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>{text('title', 'import_title')}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex min-w-0 max-w-full flex-col gap-4 overflow-hidden">
          {result ? (
            <div className="flex min-h-0 flex-1 flex-col gap-4">
              <div className="rounded-inner border border-separator bg-fill-3 px-4 py-3 text-body">
                {text('summary', 'import_summary', result.meta)}
              </div>
              <div className="min-h-0 flex-1 space-y-2 overflow-y-auto pr-1">
                {result.items.map((item, index) => (
                  <div key={`${item.source}-${index}`} className="space-y-0.5 rounded-inner border border-separator bg-elevated-1 px-3 py-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-body font-medium text-label">{item.label || text('unnamed', 'import_unnamed')}</span>
                      {item.region ? <Badge variant="neutral" className="text-footnote">{regionLabel(item.region)}</Badge> : null}
                      <Badge variant={item.status === 'imported' ? 'success' : 'warning'} className="text-footnote">
                        {text(item.status === 'imported' ? 'imported' : 'failed', item.status === 'imported' ? 'import_imported' : 'import_failed')}
                      </Badge>
                    </div>
                    <div className="truncate text-footnote text-label-2">{item.source}</div>
                    {item.error ? <div className="text-footnote text-danger">{item.error}</div> : null}
                    {item.warning ? <div className="text-footnote text-warning">{item.warning}</div> : null}
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <div className="flex min-h-0 flex-1 flex-col gap-4">
              <JsonFileDropArea
                onFiles={addFiles}
                active={open}
                disabled={pending}
                title={text('dropzone', 'import_dropzone')}
                hint={text('hint', 'import_hint')}
              />
              <JsonFileList
                files={files}
                countLabel={text('selectedCount', 'import_selected_count', { count: files.length })}
                onRemove={removeFile}
              />
            </div>
          )}
        </DialogBody>
        <DialogFooter
          cancel={result ? undefined : (
            <Button variant="secondary" disabled={pending} onClick={() => handleOpenChange(false)}>
              {text('cancel', 'cancel')}
            </Button>
          )}
          confirm={result ? (
            <Button onClick={() => handleOpenChange(false)}>{text('done', 'import_done')}</Button>
          ) : (
            <Button disabled={files.length === 0 || pending} onClick={() => importMutation.mutate()}>
              {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {files.length > 0
                ? text('importWithCount', 'import_with_count', { count: files.length })
                : text('import', 'import_submit')}
            </Button>
          )}
        />
      </DialogContent>
    </Dialog>
  )
}
