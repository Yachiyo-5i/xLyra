import { useTranslation } from 'react-i18next'
import { Check, Copy } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'

type CreatedAccessTokenDialogProps = {
  open: boolean
  token: string | null
  copied: boolean
  onOpenChange: (open: boolean) => void
  onCopy: () => void
}

type DeleteAccessTokenDialogProps = {
  open: boolean
  pending: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}

export function CreatedAccessTokenDialog({
  open,
  token,
  copied,
  onOpenChange,
  onCopy,
}: CreatedAccessTokenDialogProps) {
  const { t } = useTranslation('settings')

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(92vw,560px)] overflow-hidden">
        <DialogHeader>
          <div className="space-y-1.5">
            <DialogTitle>{t('profile.accessToken.createdDialog.title')}</DialogTitle>
            <p className="text-muted-soft text-sm">{t('profile.accessToken.createdDialog.description')}</p>
          </div>
        </DialogHeader>
        <DialogBody className="space-y-4">
          <FormField>
            <div className="flex gap-2">
              <Input readOnly className="font-mono text-xs" value={token ?? ''} />
              <Button
                variant="secondary"
                disabled={!token}
                className={
                  copied
                    ? 'border-[hsl(var(--badge-success-border))] bg-[hsl(var(--badge-success-bg))] text-[hsl(var(--badge-success-text))] hover:bg-[hsl(var(--badge-success-bg))]'
                    : undefined
                }
                onClick={onCopy}
              >
                {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                {copied ? t('profile.accessToken.createdDialog.copied') : t('profile.accessToken.createdDialog.copy')}
              </Button>
            </div>
          </FormField>
        </DialogBody>
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>{t('profile.accessToken.createdDialog.done')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function DeleteAccessTokenDialog({
  open,
  pending,
  onOpenChange,
  onConfirm,
}: DeleteAccessTokenDialogProps) {
  const { t } = useTranslation('settings')

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && pending) return
        onOpenChange(nextOpen)
      }}
    >
      <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
        <DialogHeader>
          <DialogTitle>{t('profile.accessToken.deleteDialog.title')}</DialogTitle>
        </DialogHeader>
        <DialogBody className="text-sm text-muted-soft">
          {t('profile.accessToken.deleteDialog.description')}
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" disabled={pending} onClick={() => onOpenChange(false)}>
            {t('profile.accessToken.deleteDialog.cancel')}
          </Button>
          <Button variant="destructive" disabled={pending} onClick={onConfirm}>
            {t('profile.accessToken.deleteDialog.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
