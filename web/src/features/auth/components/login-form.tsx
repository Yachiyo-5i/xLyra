import { startTransition, useEffect, useRef, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { LockKeyhole, ShieldCheck, UserRound } from 'lucide-react'
import { z } from 'zod'
import { createAdminSession } from '@/features/auth/api/auth'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { FieldErrors, FieldGroup, FieldRow } from '@/features/auth/components/auth-card'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'
import { resolveAuthRedirectTarget } from '@/lib/navigation'
import { useAuthStore } from '@/stores/auth-store'

const REMEMBER_USERNAME_KEY = 'xlyra.login.username'
type LoginFormValues = {
  username: string
  password: string
  totpCode?: string
}

function useLoginSchema() {
  const { t } = useTranslation('auth')
  return z.object({
    username: z.string().trim().min(1, t('validation.usernameRequired')),
    password: z.string().min(8, t('validation.passwordMinLength')),
    totpCode: z.string().trim().optional(),
  })
}

/** 登录表单（放在 LyraAuthScene 的卡片里）：分组输入 + 记住账号 + 主按钮；需要时多出一行两步验证码 */
export function LoginForm() {
  const { t } = useTranslation('auth')
  const navigate = useNavigate()
  const location = useLocation()
  const setSession = useAuthStore((state) => state.setSession)
  const redirectTarget = resolveAuthRedirectTarget(location.search)
  const [totpRequired, setTotpRequired] = useState(false)
  const rememberedUsername = window.localStorage.getItem(REMEMBER_USERNAME_KEY) ?? ''
  const [rememberUsername, setRememberUsername] = useState(Boolean(rememberedUsername))
  const totpInputRef = useRef<HTMLInputElement | null>(null)

  const loginSchema = useLoginSchema()

  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: {
      username: rememberedUsername,
      password: '',
      totpCode: '',
    },
  })
  const totpField = form.register('totpCode')

  useEffect(() => {
    if (totpRequired) {
      requestAnimationFrame(() => totpInputRef.current?.focus())
    }
  }, [totpRequired])

  const loginMutation = useMutation({
    mutationFn: createAdminSession,
    onSuccess: (session) => {
      const username = form.getValues('username').trim()
      if (rememberUsername && username) {
        window.localStorage.setItem(REMEMBER_USERNAME_KEY, username)
      } else {
        window.localStorage.removeItem(REMEMBER_USERNAME_KEY)
      }

      setSession(session)
      toast.success(t('login.loginSuccess'))

      startTransition(() => {
        navigate(redirectTarget, { replace: true })
      })
    },
    onError: (error) => {
      if (error instanceof APIError && error.code === 'totp_required') {
        setTotpRequired(true)
        toast.error(t('login.totpRequired'))
        return
      }
      if (error instanceof APIError && error.code === 'invalid_totp') {
        setTotpRequired(true)
        toast.error(t('login.totpInvalid'))
        return
      }
      setTotpRequired(false)
      toast.error(error instanceof Error ? error.message : t('login.loginFailed'))
    },
  })

  async function handleSubmit(values: LoginFormValues) {
    await loginMutation.mutateAsync({
      username: values.username.trim(),
      password: values.password,
      totpCode: values.totpCode?.trim() || undefined,
    })
  }

  const errors = form.formState.errors

  return (
    <form className="space-y-4" onSubmit={form.handleSubmit(handleSubmit)} noValidate>
      <FieldGroup invalid={Boolean(errors.username || errors.password || errors.totpCode)}>
        <FieldRow
          icon={<UserRound />}
          type="text"
          autoComplete="username"
          aria-label={t('login.username')}
          aria-invalid={Boolean(errors.username)}
          placeholder={t('login.usernamePlaceholder')}
          {...form.register('username')}
        />
        <FieldRow
          icon={<LockKeyhole />}
          type="password"
          autoComplete="current-password"
          aria-label={t('login.password')}
          aria-invalid={Boolean(errors.password)}
          placeholder={t('login.passwordPlaceholder')}
          {...form.register('password')}
        />
        {totpRequired ? (
          <FieldRow
            icon={<ShieldCheck />}
            type="text"
            inputMode="numeric"
            autoComplete="one-time-code"
            aria-label={t('login.totpCode')}
            aria-invalid={Boolean(errors.totpCode)}
            placeholder={t('login.totpCodePlaceholder')}
            className="font-mono tracking-[0.3em]"
            {...totpField}
            ref={(node) => {
              totpField.ref(node)
              totpInputRef.current = node
            }}
          />
        ) : null}
      </FieldGroup>
      <FieldErrors messages={[errors.username?.message, errors.password?.message, errors.totpCode?.message]} />

      <Checkbox label={t('login.rememberMe')} checked={rememberUsername} onCheckedChange={setRememberUsername} />

      <Button type="submit" size="lg" className="w-full" loading={loginMutation.isPending}>
        {loginMutation.isPending ? t('login.loggingIn') : t('login.loginButton')}
      </Button>
    </form>
  )
}
