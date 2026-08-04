import { startTransition, useEffect, useRef, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { KeyRound, ShieldCheck, UserRound } from 'lucide-react'
import { z } from 'zod'
import { createAdminSession } from '@/features/auth/api/auth'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'
import { resolveAuthRedirectTarget } from '@/lib/navigation'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

const REMEMBER_USERNAME_KEY = 'xlyra.login.username'
const inputIconSlotClassName =
  'pointer-events-none absolute left-5 top-1/2 z-10 grid size-5 -translate-y-1/2 place-items-center text-primary'
const inputIconClassName = 'h-5 w-5'

type LoginFormProps = {
  variant?: 'default' | 'mobile'
}

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

export function LoginForm({ variant = 'default' }: LoginFormProps) {
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

  const isMobile = variant === 'mobile'
  const inputIconSlot = isMobile
    ? 'pointer-events-none absolute left-4 top-1/2 z-10 grid size-5 -translate-y-1/2 place-items-center text-[hsl(var(--primary)/0.88)]'
    : inputIconSlotClassName
  const inputClassName = isMobile
    ? 'h-12 rounded-xl border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-field)/0.72)] pr-4 pl-12 text-base shadow-[0_8px_22px_rgba(0,0,0,0.04)] focus:[box-shadow:0_0_0_3px_hsl(var(--primary)/0.12)]'
    : 'h-[clamp(3rem,3.4vw,3.45rem)] rounded-xl pr-5 pl-14 text-[clamp(0.95rem,1vw,1.05rem)] shadow-[0_10px_24px_rgba(39,45,77,0.06)]'

  return (
    <form
      className={cn(isMobile ? 'space-y-5' : 'space-y-[clamp(1.5rem,2vw,2.25rem)]')}
      onSubmit={form.handleSubmit(handleSubmit)}
    >
      <div className={cn('grid', isMobile ? 'gap-4' : 'gap-[clamp(1rem,1.35vw,1.35rem)]')}>
        <FormField
          label={t('login.username')}
          htmlFor="login-username"
          required
          error={form.formState.errors.username?.message}
        >
          <div className="relative">
            <span className={inputIconSlot}>
              <UserRound className={inputIconClassName} strokeWidth={2.4} />
            </span>
            <Input
              id="login-username"
              className={inputClassName}
              type="text"
              autoComplete="username"
              placeholder={t('login.usernamePlaceholder')}
              {...form.register('username')}
            />
          </div>
        </FormField>

        <FormField
          label={t('login.password')}
          htmlFor="login-password"
          required
          error={form.formState.errors.password?.message}
        >
          <div className="relative">
            <span className={inputIconSlot}>
              <KeyRound className={inputIconClassName} strokeWidth={2.4} />
            </span>
            <Input
              id="login-password"
              className={inputClassName}
              type="password"
              autoComplete="current-password"
              placeholder={t('login.passwordPlaceholder')}
              {...form.register('password')}
            />
          </div>
        </FormField>

        {totpRequired ? (
          <FormField
            label={t('login.totpCode')}
            htmlFor="login-totp"
            required
            error={form.formState.errors.totpCode?.message}
          >
            <div className="relative">
              <span className={inputIconSlot}>
                <ShieldCheck className={inputIconClassName} strokeWidth={2.4} />
              </span>
              <Input
                id="login-totp"
                className={inputClassName}
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder={t('login.totpCodePlaceholder')}
                {...totpField}
                ref={(node) => {
                  totpField.ref(node)
                  totpInputRef.current = node
                }}
              />
            </div>
          </FormField>
        ) : null}

        <label className={cn('text-muted-soft flex w-fit cursor-pointer items-center gap-2 text-sm', isMobile && 'pt-0.5')}>
          <input
            type="checkbox"
            className="h-4 w-4 rounded border-[hsl(var(--glass-border))] accent-[hsl(var(--primary))]"
            checked={rememberUsername}
            onChange={(event) => setRememberUsername(event.target.checked)}
          />
          {t('login.rememberMe')}
        </label>
      </div>

      <Button
        type="submit"
        size="lg"
        className={cn(
          'w-full rounded-xl',
          isMobile
            ? 'h-12 bg-primary text-base text-primary-foreground shadow-[0_18px_34px_hsl(var(--primary)/0.24)] hover:bg-primary hover:brightness-105'
            : 'h-[clamp(3.05rem,3.5vw,3.55rem)] text-[clamp(1rem,1.05vw,1.1rem)] shadow-[0_18px_34px_hsl(var(--primary)/0.24)]',
        )}
        disabled={loginMutation.isPending}
      >
        {loginMutation.isPending ? (
          <>
            {t('login.loggingIn')}
          </>
        ) : (
          t('login.loginButton')
        )}
      </Button>
    </form>
  )
}
