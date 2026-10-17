import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

type ConfirmDialogProps = {
  open: boolean
  title: string
  description: string
  confirmLabel: string
  pending?: boolean
  destructive?: boolean
  onCancel: () => void
  onConfirm: () => void
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel,
  pending = false,
  destructive,
  onCancel,
  onConfirm,
}: ConfirmDialogProps) {
  const { t } = useTranslation('common')
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onCancel() }}>
      {/* 提醒类弹窗（仿 iOS Alert）：始终居中，文字居中，两个按钮等宽并排 */}
      <DialogContent className="w-[min(92vw,380px)]">
        <DialogHeader className="border-0 px-6 pb-1 pt-6 text-center">
          <DialogTitle className="text-title-3">{title}</DialogTitle>
          <DialogDescription className="mt-2">{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter
          className="grid grid-cols-2 border-0 px-6 pb-6 pt-5"
          cancel={<Button variant="secondary" onClick={onCancel} disabled={pending}>{t('actions.cancel')}</Button>}
          confirm={(
            <Button variant={destructive ? 'destructive' : 'default'} onClick={onConfirm} loading={pending}>
              {confirmLabel}
            </Button>
          )}
        />
      </DialogContent>
    </Dialog>
  )
}
