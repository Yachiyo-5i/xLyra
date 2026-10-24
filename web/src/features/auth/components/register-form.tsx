import { startTransition, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useForm, useWatch } from 'react-hook-form'
import { useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Check, KeyRound, LockKeyhole, UserRound } from 'lucide-react'
import { z } from 'zod'
import { deleteCurrentAdminSession, registerBootstrapAdmin } from '@/features/auth/api/auth'
import { Button } from '@/components/ui/button'
import { FieldErrors, FieldGroup, FieldRow } from '@/features/auth/components/auth-card'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

type RegisterFormProps = {
  /** 管理员创建成功：页面据此点亮织女星，卡片换成完成状态 */
  onCreated?: () => void
}

type RegisterFormValues = {
  username: string
  password: string
  confirmPassword: string
}

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

/**
 * 注册表单（实例初始化，放在 LyraAuthScene 的卡片里）：账号、密码、确认密码三行分组输入，
 * 下面实时显示密码规则；创建成功后显示完成状态，点「登录」进入登录页。
 */
export function RegisterForm({ onCreated }: RegisterFormProps) {
  const { t } = useTranslation('auth')
  const navigate = useNavigate()
  const location = useLocation()
  const initialize = useAuthStore((state) => state.initialize)
  const clearSession = useAuthStore((state) => state.clearSession)
  const [created, setCreated] = useState(false)

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
  const rules = [
    { label: t('validation.passwordMinLength'), ok: password.length >= 8 },
    { label: t('validation.passwordMustContainLetter'), ok: /[A-Za-z]/.test(password) },
    { label: t('validation.passwordMustContainNumber'), ok: /\d/.test(password) },
  ]
  const sameAsUsername = Boolean(password && username.trim() && password === username.trim())
  const confirmMismatch = Boolean(confirmPassword && password && confirmPassword !== password)

  const goToLogin = () => {
    startTransition(() => {
      navigate(`/login${location.search}`, { replace: true })
    })
  }

  const registerMutation = useMutation({
    mutationFn: registerBootstrapAdmin,
    onSuccess: async () => {
      await deleteCurrentAdminSession().catch(() => undefined)
      clearSession()
      toast.success(t('register.createSuccess'))
      setCreated(true)
      onCreated?.()
    },
    onError: async (error) => {
      if (error instanceof APIError && error.code === 'bootstrap_completed') {
        toast.error(t('register.alreadyInitialized'))
        await initialize(true)
        goToLogin()
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

  if (created) {
    return (
      <div className="space-y-4">
        <p className="text-footnote text-label-3">{t('register.createSuccess')}</p>
        <Button size="lg" className="w-full" onClick={goToLogin}>
          <KeyRound />
          {t('login.loginButton')}
        </Button>
      </div>
    )
  }

  const errors = form.formState.errors

  return (
    <form className="space-y-4" onSubmit={form.handleSubmit(handleSubmit)} noValidate>
      <FieldGroup invalid={Boolean(errors.username)}>
        <FieldRow
          icon={<UserRound />}
          type="text"
          autoComplete="username"
          aria-label={t('register.username')}
          aria-invalid={Boolean(errors.username)}
          placeholder={t('register.usernamePlaceholder')}
          {...form.register('username')}
        />
        <FieldRow
          icon={<LockKeyhole />}
          type="password"
          autoComplete="new-password"
          aria-label={t('register.password')}
          placeholder={t('register.passwordPlaceholder')}
          {...form.register('password')}
        />
        <FieldRow
          icon={<LockKeyhole />}
          type="password"
          autoComplete="new-password"
          aria-label={t('register.confirmPassword')}
          aria-invalid={confirmMismatch}
          placeholder={t('register.confirmPasswordPlaceholder')}
          {...form.register('confirmPassword')}
        />
      </FieldGroup>
      <FieldErrors messages={[errors.username?.message]} />

      {/* 密码规则：满足的一条变绿打勾 */}
      <ul className="space-y-1 px-1" aria-live="polite">
        {rules.map((rule) => (
          <li key={rule.label} className={cn('flex items-center gap-2 text-footnote transition-colors', rule.ok ? 'text-success' : 'text-label-3')}>
            <Check className={cn('size-3.5 shrink-0 transition-opacity', rule.ok ? 'opacity-100' : 'opacity-30')} strokeWidth={2.5} />
            {rule.label}
          </li>
        ))}
        {sameAsUsername ? <li className="pl-5.5 text-footnote text-danger">{t('validation.passwordSameAsUsername')}</li> : null}
        {confirmMismatch ? <li className="pl-5.5 text-footnote text-danger">{t('validation.passwordMismatch')}</li> : null}
      </ul>

      <Button type="submit" size="lg" className="w-full" disabled={!form.formState.isValid} loading={registerMutation.isPending}>
        {registerMutation.isPending ? t('register.creating') : t('register.createButton')}
      </Button>
    </form>
  )
}
