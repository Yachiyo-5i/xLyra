import { startTransition } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useForm, useWatch } from 'react-hook-form'
import { useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { KeyRound, UserRound } from 'lucide-react'
import { z } from 'zod'
import { deleteCurrentAdminSession, registerBootstrapAdmin } from '@/features/auth/api/auth'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Progress, type ProgressVariant } from '@/components/ui/progress'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

type RegisterFormProps = {
  variant?: 'default' | 'mobile'
}

type RegisterFormValues = {
  username: string
  password: string
  confirmPassword: string
}

const inputIconSlotClassName =
  'pointer-events-none absolute left-5 top-1/2 z-10 grid size-5 -translate-y-1/2 place-items-center text-primary'
const inputIconClassName = 'h-5 w-5'

function useRegisterSchema() {
  const { t } = useTranslation('auth')
  return z
    .object({
      username: z.string().trim().min(1, t('validation.usernameRequired')),
      password: z
        .string()
        .min(8, t('validation.passwordMinLength'))
        .regex(/[A-Za-z]/, t('validation.passwordMustContainLetter'))
        .regex(/\d/, t('validation.passwordMustContainNumber')),
      confirmPassword: z.string().min(8, t('validation.confirmPasswordRequired')),
    })
    .refine((values) => values.password !== values.username, {
      message: t('validation.passwordSameAsUsername'),
      path: ['password'],
    })
    .refine((values) => values.password === values.confirmPassword, {
      message: t('validation.passwordMismatch'),
      path: ['confirmPassword'],
    })
}

export function RegisterForm({ variant = 'default' }: RegisterFormProps) {
  const { t } = useTranslation('auth')
  const navigate = useNavigate()
  const location = useLocation()
  const initialize = useAuthStore((state) => state.initialize)
  const clearSession = useAuthStore((state) => state.clearSession)

  const registerSchema = useRegisterSchema()

  const form = useForm<RegisterFormValues>({
    resolver: zodResolver(registerSchema),
    mode: 'onChange',
    defaultValues: {
      username: '',
      password: '',
      confirmPassword: '',
    },
  })
  const [username, password, confirmPassword] = useWatch({
    control: form.control,
    name: ['username', 'password', 'confirmPassword'],
  })
  const passwordStrength = getPasswordStrength(password, username, t)
  const confirmPasswordMismatch = Boolean(confirmPassword && password && confirmPassword !== password)
  const isMobile = variant === 'mobile'
  const inputIconSlot = isMobile
    ? 'pointer-events-none absolute left-4 top-1/2 z-10 grid size-5 -translate-y-1/2 place-items-center text-[hsl(var(--primary)/0.88)]'
    : inputIconSlotClassName
  const inputClassName = isMobile
    ? 'h-12 rounded-xl border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-field)/0.72)] pr-4 pl-12 text-base shadow-[0_8px_22px_rgba(0,0,0,0.04)] focus:[box-shadow:0_0_0_3px_hsl(var(--primary)/0.12)]'
    : 'h-[clamp(3rem,3.4vw,3.45rem)] rounded-xl pr-5 pl-14 text-[clamp(0.95rem,1vw,1.05rem)] shadow-[0_10px_24px_rgba(39,45,77,0.06)]'

  const registerMutation = useMutation({
    mutationFn: registerBootstrapAdmin,
    onSuccess: async () => {
      await deleteCurrentAdminSession().catch(() => undefined)
      clearSession()
      toast.success(t('register.createSuccess'))

      startTransition(() => {
        navigate(`/login${location.search}`, { replace: true })
      })
    },
    onError: async (error) => {
      if (error instanceof APIError && error.code === 'bootstrap_completed') {
        toast.error(t('register.alreadyInitialized'))
        await initialize(true)

        startTransition(() => {
          navigate(`/login${location.search}`, { replace: true })
        })

        return
      }

      toast.error(error instanceof Error ? error.message : t('register.createFailed'))
    },
  })

  async function handleSubmit(values: RegisterFormValues) {
    await registerMutation.mutateAsync({
      username: values.username.trim(),
      password: values.password,
    })
  }

  return (
    <form
      className={cn(isMobile ? 'space-y-5' : 'space-y-[clamp(1.5rem,2vw,2.25rem)]')}
      onSubmit={form.handleSubmit(handleSubmit)}
    >
      <div className={cn('grid', isMobile ? 'gap-4' : 'gap-[clamp(1rem,1.35vw,1.35rem)]')}>
        <FormField
          label={t('register.username')}
          htmlFor="register-username"
          required
          error={form.formState.errors.username?.message}
        >
          <div className="relative">
            <span className={inputIconSlot}>
              <UserRound className={inputIconClassName} strokeWidth={2.4} />
            </span>
            <Input
              id="register-username"
              className={inputClassName}
              type="text"
              autoComplete="username"
              placeholder={t('register.usernamePlaceholder')}
              {...form.register('username')}
            />
          </div>
        </FormField>

        <FormField
          label={t('register.password')}
          htmlFor="register-password"
          required
          error={form.formState.errors.password?.message}
        >
          <div className="relative">
            <span className={inputIconSlot}>
              <KeyRound className={inputIconClassName} strokeWidth={2.4} />
            </span>
            <Input
              id="register-password"
              className={inputClassName}
              type="password"
              autoComplete="new-password"
              placeholder={t('register.passwordPlaceholder')}
              {...form.register('password')}
            />
          </div>
          {password ? <PasswordStrengthBar strength={passwordStrength} /> : null}
        </FormField>

        <FormField
          label={t('register.confirmPassword')}
          htmlFor="register-confirm-password"
          required
          error={confirmPasswordMismatch ? t('validation.passwordMismatch') : form.formState.errors.confirmPassword?.message}
        >
          <div className="relative">
            <span className={inputIconSlot}>
              <KeyRound className={inputIconClassName} strokeWidth={2.4} />
            </span>
            <Input
              id="register-confirm-password"
              className={inputClassName}
              type="password"
              autoComplete="new-password"
              placeholder={t('register.confirmPasswordPlaceholder')}
              {...form.register('confirmPassword')}
            />
          </div>
        </FormField>
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
        disabled={registerMutation.isPending}
      >
        {registerMutation.isPending ? (
          <>
            {t('register.creating')}
          </>
        ) : (
          <>
            {t('register.createButton')}
          </>
        )}
      </Button>
    </form>
  )
}

type PasswordStrength = {
  label: string
  value: number
  variant: ProgressVariant
}

type TFunction = (key: string) => string

function getPasswordStrength(password: string, username: string, t: TFunction): PasswordStrength {
  if (!password) {
    return { label: t('passwordStrength.empty'), value: 0, variant: 'muted' }
  }

  if (username.trim() && password === username.trim()) {
    return { label: t('passwordStrength.unusable'), value: 20, variant: 'danger' }
  }

  const checks = [
    password.length >= 8,
    /[A-Za-z]/.test(password),
    /\d/.test(password),
    /[^A-Za-z0-9]/.test(password) || (/[a-z]/.test(password) && /[A-Z]/.test(password)),
  ]
  const score = checks.filter(Boolean).length

  if (score <= 2) {
    return { label: t('passwordStrength.weak'), value: score * 25, variant: 'danger' }
  }

  if (score === 3) {
    return { label: t('passwordStrength.medium'), value: 75, variant: 'warning' }
  }

  return { label: t('passwordStrength.strong'), value: 100, variant: 'success' }
}

function PasswordStrengthBar({ strength }: { strength: PasswordStrength }) {
  const { t } = useTranslation('auth')

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-soft">{t('passwordStrength.label')}</span>
        <span className="font-medium text-foreground">{strength.label}</span>
      </div>
      <Progress value={strength.value} variant={strength.variant} className="h-1.5" />
    </div>
  )
}
