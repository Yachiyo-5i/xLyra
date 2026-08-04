import { useTranslation } from 'react-i18next'
import { Copy } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { Sheet, SheetBody, SheetContent, SheetFooter, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { QRCode } from '@/components/common/qr-code'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'

export type TOTPSetupState = {
  secret: string
  otpauthURL: string
}

type ProfileTOTPSetupSheetProps = {
  open: boolean
  setup: TOTPSetupState | null
  code: string
  setupPending: boolean
  enablePending: boolean
  onCodeChange: (code: string) => void
  onOpenChange: (open: boolean) => void
  onSubmit: () => void
}

export function ProfileTOTPSetupSheet({
  open,
  setup,
  code,
  setupPending,
  enablePending,
  onCodeChange,
  onOpenChange,
  onSubmit,
}: ProfileTOTPSetupSheetProps) {
  const { t } = useTranslation('settings')

  const steps = [
    t('profile.totp.setupSheet.step1'),
    t('profile.totp.setupSheet.step2'),
    t('profile.totp.setupSheet.step3'),
  ]

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>{t('profile.totp.setupSheet.title')}</SheetTitle>
        </SheetHeader>
        <SheetBody className="space-y-5">
          <div className="grid grid-cols-3 gap-2 text-xs">
            {steps.map((item, index) => (
              <div
                key={item}
                className="rounded-lg bg-[hsl(var(--surface-subtle))] px-3 py-2 text-muted-soft"
              >
                <span className="mr-1 font-semibold text-foreground">{index + 1}</span>
                {item}
              </div>
            ))}
          </div>

          {setupPending ? (
            <div className="space-y-4">
              <div className="mx-auto h-[216px] w-[216px] animate-pulse rounded-lg bg-[hsl(var(--surface-subtle))]" />
              <div className="h-10 animate-pulse rounded-md bg-[hsl(var(--surface-subtle))]" />
              <div className="h-10 animate-pulse rounded-md bg-[hsl(var(--surface-subtle))]" />
            </div>
          ) : setup ? (
            <div className="space-y-5">
              <div className="flex justify-center">
                <QRCode value={setup.otpauthURL} size={192} />
              </div>

              <FormField label={t('profile.totp.setupSheet.backupKey')}>
                <div className="flex gap-2">
                  <Input readOnly className="font-mono" value={setup.secret} />
                  <Button variant="secondary" onClick={() => copyText(setup.secret, t('profile.totp.setupSheet.copied'))}>
                    <Copy className="h-4 w-4" />
                    {t('profile.totp.setupSheet.copy')}
                  </Button>
                </div>
              </FormField>

              <FormField label={t('profile.sessions.status')}>
                <Input
                  value={code}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  placeholder={t('profile.totp.setupSheet.codePlaceholder')}
                  onChange={(event) => onCodeChange(event.target.value)}
                />
              </FormField>
            </div>
          ) : (
            <div className="rounded-lg bg-[hsl(var(--surface-subtle))] px-4 py-3 text-sm text-muted-soft">
              {t('profile.totp.setupSheet.noQRCode')}
            </div>
          )}
        </SheetBody>
        <SheetFooter>
          <div className="flex justify-start gap-2">
            <Button disabled={!setup || enablePending || !code.trim()} onClick={onSubmit}>
              {t('profile.totp.setupSheet.finish')}
            </Button>
            <Button variant="secondary" onClick={() => onOpenChange(false)}>
              {t('profile.totp.setupSheet.cancel')}
            </Button>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

async function copyText(value: string, message: string) {
  await copyToClipboard(value, message)
}
