import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

/**
 * Risk notice shown before any OAuth account is connected. The user has to tick
 * the acknowledgement before the confirm button works; the tick lives inside the
 * dialog content, so every opening starts unchecked.
 */
export function OAuthDisclaimerDialog({
  open,
  onOpenChange,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent presentation="center" className="w-[min(92vw,520px)] overflow-hidden">
        <DisclaimerContent onCancel={() => onOpenChange(false)} onConfirm={onConfirm} />
      </DialogContent>
    </Dialog>
  )
}

function DisclaimerContent({ onCancel, onConfirm }: { onCancel: () => void; onConfirm: () => void }) {
  const { t } = useTranslation('oauth')
  const [acknowledged, setAcknowledged] = useState(false)

  return (
    <>
      <DialogHeader className="border-b-0 pb-2">
        <DialogTitle>{t('workspace.disclaimer.title')}</DialogTitle>
      </DialogHeader>
      <DialogBody className="space-y-3 pt-0 text-body text-label-2">
        <p>{t('workspace.disclaimer.terms')}</p>
        <p>{t('workspace.disclaimer.ban')}</p>
        <p>{t('workspace.disclaimer.liability')}</p>
        <div className="border-t border-separator pt-3">
          <Checkbox
            checked={acknowledged}
            onCheckedChange={setAcknowledged}
            label={t('workspace.disclaimer.acknowledge')}
          />
        </div>
      </DialogBody>
      <DialogFooter
        className="border-t-0 pt-2"
        cancel={<Button variant="outline" onClick={onCancel}>{t('workspace.disclaimer.cancel')}</Button>}
        confirm={<Button disabled={!acknowledged} onClick={onConfirm}>{t('workspace.disclaimer.confirm')}</Button>}
      />
    </>
  )
}
