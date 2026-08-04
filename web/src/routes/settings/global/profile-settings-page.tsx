import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { KeyRound, PauseCircle, PlayCircle, RefreshCw, ShieldCheck, Trash2 } from 'lucide-react'
import { localeFromLanguage } from '@/lib/locale'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { AvatarUpload } from '@/components/common/avatar-upload'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { toast } from '@/lib/toast'
import { useAuthStore } from '@/stores/auth-store'
import {
  createProfileAccessToken,
  deleteOtherProfileSessions,
  deleteProfileAccessToken,
  deleteProfileSession,
  disableProfileTOTP,
  enableProfileTOTP,
  getProfile,
  getProfileAccessToken,
  listProfileSessions,
  profileQueryKeys,
  setProfileAccessTokenEnabled,
  setupProfileTOTP,
  updateProfileAccount,
  updateProfilePassword,
} from '@/features/auth/api/profile'
import { CreatedAccessTokenDialog, DeleteAccessTokenDialog } from '@/routes/settings/global/components/profile-access-token-dialogs'
import { ProfileTOTPSetupSheet, type TOTPSetupState } from '@/routes/settings/global/components/profile-totp-setup-sheet'

export function ProfileSettingsPage() {
  const { t, i18n } = useTranslation('settings')
  const queryClient = useQueryClient()
  const setAdmin = useAuthStore((state) => state.setAdmin)
  const profileQuery = useQuery({ queryKey: profileQueryKeys.current(), queryFn: getProfile })
  const sessionsQuery = useQuery({ queryKey: profileQueryKeys.sessions(), queryFn: listProfileSessions })
  const accessTokenQuery = useQuery({ queryKey: profileQueryKeys.accessToken(), queryFn: getProfileAccessToken })
  const [passwordForm, setPasswordForm] = useState({ currentPassword: '', newPassword: '', confirmPassword: '' })
  const [totpSetup, setTotpSetup] = useState<TOTPSetupState | null>(null)
  const [totpCode, setTotpCode] = useState('')
  const [totpSheetOpen, setTotpSheetOpen] = useState(false)
  const [disableTotpForm, setDisableTotpForm] = useState({ currentPassword: '', code: '' })
  const [createdToken, setCreatedToken] = useState<string | null>(null)
  const [createdTokenDialogOpen, setCreatedTokenDialogOpen] = useState(false)
  const [createdTokenCopied, setCreatedTokenCopied] = useState(false)
  const [deleteTokenDialogOpen, setDeleteTokenDialogOpen] = useState(false)
  const [avatarDraft, setAvatarDraft] = useState({ value: '', touched: false })
  const copiedTimerRef = useRef<number | null>(null)

  const accountMutation = useMutation({
    mutationFn: updateProfileAccount,
    onSuccess: async (result) => {
      setAdmin(result.admin)
      setAvatarDraft({ value: '', touched: false })
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.current() })
      toast.success(t('profile.adminInfo.saved'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.adminInfo.saveFailed')),
  })

  const passwordMutation = useMutation({
    mutationFn: updateProfilePassword,
    onSuccess: async (result) => {
      setAdmin(result.admin)
      setPasswordForm({ currentPassword: '', newPassword: '', confirmPassword: '' })
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.sessions() })
      toast.success(t('profile.password.updated'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.password.updateFailed')),
  })

  const accessTokenCreateMutation = useMutation({
    mutationFn: createProfileAccessToken,
    onSuccess: async (result) => {
      const token = result.access_token.token ?? null
      setCreatedToken(token)
      setCreatedTokenDialogOpen(Boolean(token))
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.accessToken() })
      if (token) {
        const copied = await copyToClipboard(token, t('profile.accessToken.createdAndCopied'), null)
        if (copied) {
          showCreatedTokenCopied()
        } else {
          toast.success(t('profile.accessToken.created'))
        }
        return
      }
      toast.success(t('profile.accessToken.created'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.accessToken.createFailed')),
  })

  const accessTokenEnabledMutation = useMutation({
    mutationFn: setProfileAccessTokenEnabled,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.accessToken() })
      toast.success(t('profile.accessToken.statusUpdated'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.accessToken.statusUpdateFailed')),
  })

  const accessTokenDeleteMutation = useMutation({
    mutationFn: deleteProfileAccessToken,
    onSuccess: async () => {
      setCreatedToken(null)
      setCreatedTokenDialogOpen(false)
      setCreatedTokenCopied(false)
      setDeleteTokenDialogOpen(false)
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.accessToken() })
      toast.success(t('profile.accessToken.deleted'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.accessToken.deleteFailed')),
  })

  const setupTotpMutation = useMutation({
    mutationFn: setupProfileTOTP,
    onSuccess: (result) => {
      setTotpSetup({ secret: result.secret, otpauthURL: result.otpauth_url })
      setAdmin(result.admin)
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.totp.setupFailed')),
  })

  const enableTotpMutation = useMutation({
    mutationFn: enableProfileTOTP,
    onSuccess: async (result) => {
      setAdmin(result.admin)
      setTotpSetup(null)
      setTotpCode('')
      setTotpSheetOpen(false)
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.current() })
      toast.success(t('profile.totp.enabledSuccess'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.totp.enableFailed')),
  })

  const disableTotpMutation = useMutation({
    mutationFn: disableProfileTOTP,
    onSuccess: async (result) => {
      setAdmin(result.admin)
      setDisableTotpForm({ currentPassword: '', code: '' })
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.current() })
      toast.success(t('profile.totp.disabledSuccess'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.totp.disableFailed')),
  })

  const deleteSessionMutation = useMutation({
    mutationFn: deleteProfileSession,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.sessions() })
      toast.success(t('profile.sessions.deleted'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.sessions.deleteFailed')),
  })

  const deleteOtherSessionsMutation = useMutation({
    mutationFn: deleteOtherProfileSessions,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: profileQueryKeys.sessions() })
      toast.success(t('profile.sessions.otherDeleted'))
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t('profile.sessions.deleteFailed')),
  })

  const admin = profileQuery.data?.admin
  const accessToken = accessTokenQuery.data?.access_token
  const avatarValue = avatarDraft.touched ? avatarDraft.value : (admin?.avatar ?? '')

  function submitPassword() {
    if (passwordForm.newPassword !== passwordForm.confirmPassword) {
      toast.error(t('profile.password.mismatch'))
      return
    }
    passwordMutation.mutate({
      currentPassword: passwordForm.currentPassword,
      newPassword: passwordForm.newPassword,
    })
  }

  function openTotpSetup() {
    setTotpCode('')
    setTotpSetup(null)
    setTotpSheetOpen(true)
    setupTotpMutation.mutate()
  }

  function handleTotpSheetOpenChange(open: boolean) {
    setTotpSheetOpen(open)
    if (!open) {
      setTotpSetup(null)
      setTotpCode('')
    }
  }

  function handleCreatedTokenDialogOpenChange(open: boolean) {
    setCreatedTokenDialogOpen(open)
    if (!open) {
      if (copiedTimerRef.current) {
        window.clearTimeout(copiedTimerRef.current)
        copiedTimerRef.current = null
      }
      setCreatedToken(null)
      setCreatedTokenCopied(false)
    }
  }

  function showCreatedTokenCopied() {
    if (copiedTimerRef.current) {
      window.clearTimeout(copiedTimerRef.current)
    }

    setCreatedTokenCopied(true)
    copiedTimerRef.current = window.setTimeout(() => {
      setCreatedTokenCopied(false)
      copiedTimerRef.current = null
    }, 1600)
  }

  async function copyCreatedToken() {
    if (!createdToken) return
    const copied = await copyToClipboard(createdToken, t('profile.accessToken.createdDialog.copied'), null)
    if (copied) showCreatedTokenCopied()
  }

  return (
    <div className="scrollbar-hidden h-full min-h-0 max-w-5xl space-y-8 overflow-y-auto pr-2">
      <div className="space-y-1.5">
        <h3 className="text-lg font-semibold text-foreground">{t('profile.title')}</h3>
        <p className="text-muted-soft text-sm">{t('profile.description')}</p>
      </div>

      <section className="space-y-4">
        <SectionTitle title={t('profile.adminInfo.title')} />
        <form
          key={admin?.updatedAt ?? admin?.id ?? 'profile-account'}
          className="grid max-w-2xl gap-4"
          onSubmit={(event) => {
            event.preventDefault()
            if (!admin?.username) return
            const formData = new FormData(event.currentTarget)
            accountMutation.mutate({
              username: admin.username,
              nickname: String(formData.get('nickname') ?? '').trim(),
              avatar: avatarValue.trim(),
            })
          }}
        >
          <FormField label={t('profile.adminInfo.username')}>
            <Input value={admin?.username ?? ''} readOnly disabled />
          </FormField>
          <FormField label={t('profile.adminInfo.nickname')}>
            <Input name="nickname" defaultValue={admin?.nickname ?? ''} />
          </FormField>
          <FormField label={t('profile.adminInfo.avatar')}>
            <AvatarUpload
              value={avatarValue}
              fallback={admin?.nickname || admin?.username || ''}
              onChange={(value) => setAvatarDraft({ value, touched: true })}
            />
          </FormField>
          <div>
            <Button
              type="submit"
              disabled={accountMutation.isPending || profileQuery.isLoading}
            >
              {t('profile.adminInfo.saveButton')}
            </Button>
          </div>
        </form>
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <SectionTitle title={t('profile.password.title')} />
        <div className="grid max-w-2xl gap-4">
          <FormField label={t('profile.password.currentPassword')}>
            <Input type="password" value={passwordForm.currentPassword} placeholder={t('profile.password.currentPasswordPlaceholder')} onChange={(event) => setPasswordForm((prev) => ({ ...prev, currentPassword: event.target.value }))} />
          </FormField>
          <FormField label={t('profile.password.newPassword')}>
            <Input type="password" value={passwordForm.newPassword} placeholder={t('profile.password.newPasswordPlaceholder')} onChange={(event) => setPasswordForm((prev) => ({ ...prev, newPassword: event.target.value }))} />
          </FormField>
          <FormField label={t('profile.password.confirmPassword')}>
            <Input type="password" value={passwordForm.confirmPassword} placeholder={t('profile.password.confirmPasswordPlaceholder')} onChange={(event) => setPasswordForm((prev) => ({ ...prev, confirmPassword: event.target.value }))} />
          </FormField>
          <div>
            <Button disabled={passwordMutation.isPending || !passwordForm.currentPassword || !passwordForm.newPassword} onClick={submitPassword}>
              {t('profile.password.updateButton')}
            </Button>
          </div>
        </div>
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <div className="flex items-center gap-2">
          <SectionTitle title={t('profile.totp.title')} />
          <Badge variant={admin?.totpEnabled ? 'success' : 'neutral'}>{admin?.totpEnabled ? t('profile.totp.enabled') : t('profile.totp.disabled')}</Badge>
        </div>
        <div className="max-w-2xl space-y-4">
          <div className="flex items-center gap-3">
            {!admin?.totpEnabled ? (
              <Button variant="secondary" disabled={setupTotpMutation.isPending} onClick={openTotpSetup}>
                <ShieldCheck className="h-4 w-4" />
                {t('profile.totp.enableButton')}
              </Button>
            ) : null}
          </div>

          {admin?.totpEnabled ? (
            <div className="grid gap-3">
              <FormField label={t('profile.password.currentPassword')}>
                <Input type="password" value={disableTotpForm.currentPassword} placeholder={t('profile.password.currentPasswordPlaceholder')} onChange={(event) => setDisableTotpForm((prev) => ({ ...prev, currentPassword: event.target.value }))} />
              </FormField>
              <FormField label={t('profile.sessions.status')}>
                <Input value={disableTotpForm.code} placeholder={t('profile.totp.setupSheet.codePlaceholder')} onChange={(event) => setDisableTotpForm((prev) => ({ ...prev, code: event.target.value }))} />
              </FormField>
              <div>
                <Button
                  variant="secondary"
                  disabled={disableTotpMutation.isPending || !disableTotpForm.currentPassword || !disableTotpForm.code}
                  onClick={() => disableTotpMutation.mutate(disableTotpForm)}
                >
                  {t('profile.totp.disableButton')}
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <div className="flex items-center gap-2">
          <SectionTitle title={t('profile.accessToken.title')} />
          <Badge variant={accessToken ? (accessToken.enabled ? 'success' : 'neutral') : 'neutral'}>
            {accessToken ? (accessToken.enabled ? t('profile.accessToken.enabled') : t('profile.accessToken.disabled')) : t('profile.accessToken.notCreated')}
          </Badge>
        </div>
        <div className="max-w-3xl space-y-4">
          <div className="flex flex-wrap gap-3">
            <Button variant="secondary" disabled={accessTokenCreateMutation.isPending} onClick={() => accessTokenCreateMutation.mutate()}>
              {accessToken ? <RefreshCw className="h-4 w-4" /> : <KeyRound className="h-4 w-4" />}
              {accessToken ? t('profile.accessToken.updateButton') : t('profile.accessToken.createButton')}
            </Button>
            {accessToken ? (
              <Button
                variant="secondary"
                disabled={accessTokenEnabledMutation.isPending}
                onClick={() => accessTokenEnabledMutation.mutate(!accessToken.enabled)}
              >
                {accessToken.enabled ? <PauseCircle className="h-4 w-4" /> : <PlayCircle className="h-4 w-4" />}
                {accessToken.enabled ? t('profile.accessToken.disableButton') : t('profile.accessToken.enableButton')}
              </Button>
            ) : null}
            {accessToken ? (
              <Button variant="destructive" disabled={accessTokenDeleteMutation.isPending} onClick={() => setDeleteTokenDialogOpen(true)}>
                <Trash2 className="h-4 w-4" />
                {t('profile.accessToken.deleteButton')}
              </Button>
            ) : null}
          </div>
        </div>
      </section>

      <section className="space-y-4 border-t border-[hsl(var(--divider))] pt-6">
        <div className="flex items-center justify-between gap-3">
          <SectionTitle title={t('profile.sessions.title')} />
          <Button variant="secondary" disabled={deleteOtherSessionsMutation.isPending} onClick={() => deleteOtherSessionsMutation.mutate()}>
            {t('profile.sessions.deleteOtherSessions')}
          </Button>
        </div>
        <div className="overflow-hidden rounded-lg border border-[hsl(var(--glass-border))]">
          <table className="w-full table-fixed text-left text-sm">
            <thead className="bg-[hsl(var(--surface-subtle))] text-muted-soft">
              <tr>
                <th className="px-3 py-3 font-medium">{t('profile.sessions.status')}</th>
                <th className="px-3 py-3 font-medium">{t('profile.sessions.ip')}</th>
                <th className="px-3 py-3 font-medium">{t('profile.sessions.lastActive')}</th>
                <th className="px-3 py-3 font-medium">{t('profile.sessions.expires')}</th>
                <th className="px-3 py-3 text-right font-medium">{t('profile.sessions.actions')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-[hsl(var(--divider))]">
              {(sessionsQuery.data?.items ?? []).map((item) => (
                <tr key={item.id}>
                  <td className="px-3 py-3">{item.current ? <Badge variant="success">{t('profile.sessions.current')}</Badge> : <Badge variant="neutral">{t('profile.sessions.other')}</Badge>}</td>
                  <td className="px-3 py-3">{item.ip_address ?? '-'}</td>
                  <td className="px-3 py-3">{formatDateTime(item.last_seen_at, i18n.language)}</td>
                  <td className="px-3 py-3">{formatDateTime(item.expires_at, i18n.language)}</td>
                  <td className="px-3 py-3 text-right">
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={item.current || deleteSessionMutation.isPending}
                      onClick={() => deleteSessionMutation.mutate(item.id)}
                    >
                      {t('profile.sessions.delete')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <ProfileTOTPSetupSheet
        open={totpSheetOpen}
        setup={totpSetup}
        code={totpCode}
        setupPending={setupTotpMutation.isPending}
        enablePending={enableTotpMutation.isPending}
        onCodeChange={setTotpCode}
        onOpenChange={handleTotpSheetOpenChange}
        onSubmit={() => enableTotpMutation.mutate(totpCode.trim())}
      />

      <CreatedAccessTokenDialog
        open={createdTokenDialogOpen}
        token={createdToken}
        copied={createdTokenCopied}
        onOpenChange={handleCreatedTokenDialogOpenChange}
        onCopy={copyCreatedToken}
      />

      <DeleteAccessTokenDialog
        open={deleteTokenDialogOpen}
        pending={accessTokenDeleteMutation.isPending}
        onOpenChange={setDeleteTokenDialogOpen}
        onConfirm={() => accessTokenDeleteMutation.mutate()}
      />
    </div>
  )
}

function SectionTitle({ title, description }: { title: string; description?: string }) {
  return (
    <div className="space-y-1">
      <h4 className="font-semibold text-foreground">{title}</h4>
      {description ? <p className="text-muted-soft text-sm">{description}</p> : null}
    </div>
  )
}

function formatDateTime(value?: string | null, language?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(localeFromLanguage(language), {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

