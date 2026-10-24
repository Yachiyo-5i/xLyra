import { useState, type ComponentType } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useNavigate } from 'react-router-dom'
import { ChevronsUpDown, Info, LogOut, Settings, ShieldEllipsis, UserRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useLiquidGlassRef } from '@/components/glass/use-liquid-glass'
import { Button } from '@/components/ui/button'
import { menuButtonClass, menuSeparatorClass, popoverPanelClass } from '@/components/ui/field-styles'
import { Skeleton } from '@/components/ui/skeleton'
import { SystemVersionText } from '@/components/layout/system-version-text'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

type UserAccountMenuProps = {
  mode?: 'sidebar' | 'avatar'
  onNavigate?: () => void
  side?: PopoverPrimitive.PopoverContentProps['side']
  align?: PopoverPrimitive.PopoverContentProps['align']
}

function useCurrentAccount() {
  const user = useAuthStore((state) => state.user)
  const authStatus = useAuthStore((state) => state.status)
  return {
    user,
    isAuthChecking: !user && (authStatus === 'idle' || authStatus === 'checking'),
    displayName: user?.displayName || user?.username || 'Admin',
    username: user?.username || user?.role || 'Admin',
  }
}

/** 账户菜单：桌面侧边栏底部的账户行，或独立的头像按钮，点开为 Liquid Glass 弹出菜单 */
export function UserAccountMenu({
  mode = 'sidebar',
  onNavigate,
  side = mode === 'avatar' ? 'bottom' : 'right',
  align = 'end',
}: UserAccountMenuProps) {
  const { t } = useTranslation('common')
  const { user, isAuthChecking, displayName, username } = useCurrentAccount()
  const [open, setOpen] = useState(false)
  const glassRef = useLiquidGlassRef<HTMLDivElement>('regular')

  if (isAuthChecking) {
    return mode === 'avatar' ? (
      <div className="lg-regular size-10 rounded-full p-1">
        <Skeleton className="size-full rounded-full" />
      </div>
    ) : (
      <div className="flex w-full items-center gap-3 rounded-xl px-3 py-3">
        <Skeleton className="size-10 shrink-0 rounded-lg" />
        <div className="min-w-0 flex-1 space-y-2">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-3 w-16" />
        </div>
      </div>
    )
  }

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        {mode === 'avatar' ? (
          <Button variant="glass" size="icon" className="size-10 overflow-hidden p-0.5" aria-label={t('userMenu.profile')}>
            <UserAvatar avatar={user?.avatar} name={displayName} className="size-full rounded-full border-0 bg-transparent" />
          </Button>
        ) : (
          <button
            type="button"
            className={cn(
              'flex w-full items-center gap-3 rounded-xl px-3 py-3 text-left transition-colors duration-150 hover:bg-fill-4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint/55',
              open && 'bg-fill-3',
            )}
          >
            <UserAvatar avatar={user?.avatar} name={displayName} />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-semibold text-label">{displayName}</p>
              <p className="truncate text-xs text-label-3">{username}</p>
            </div>
            <ChevronsUpDown className="h-4 w-4 shrink-0 text-label-3" />
          </button>
        )}
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          ref={glassRef}
          side={side}
          align={align}
          sideOffset={8}
          className={cn(popoverPanelClass, 'z-[120] w-[248px] origin-(--radix-popover-content-transform-origin)')}
        >
          <AccountMenuContent
            onClose={() => {
              setOpen(false)
              onNavigate?.()
            }}
          />
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}

/**
 * 账户菜单的内容（头像 + 名称、个人信息 / 系统设置 / 审计日志、版本号、退出登录）。
 * 弹出菜单与移动端顶栏的变形菜单共用。onClose 在选择任一项后调用。
 */
export function AccountMenuContent({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation('common')
  const navigate = useNavigate()
  const logout = useAuthStore((state) => state.logout)
  const { user, displayName, username } = useCurrentAccount()
  const [isSigningOut, setIsSigningOut] = useState(false)

  function handleNavigate(to: string) {
    onClose()
    navigate(to)
  }

  async function handleSignOut() {
    try {
      setIsSigningOut(true)
      onClose()
      await logout()
      toast.success(t('userMenu.logoutSuccess'))
      navigate('/login', { replace: true })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('userMenu.logoutFailed'))
    } finally {
      setIsSigningOut(false)
    }
  }

  return (
    <>
      <div className="flex items-center gap-3 px-2.5 py-2">
        <UserAvatar avatar={user?.avatar} name={displayName} />
        <div className="min-w-0">
          <p className="truncate text-headline text-label">{displayName}</p>
          <p className="truncate text-footnote text-label-3">{username}</p>
        </div>
      </div>
      <div className={menuSeparatorClass} />
      <div>
        <UserMenuItem icon={UserRound} label={t('userMenu.profile')} onClick={() => handleNavigate('/settings/global/profile')} />
        <UserMenuItem icon={Settings} label={t('userMenu.systemSettings')} onClick={() => handleNavigate('/settings/global/general')} />
        <UserMenuItem icon={ShieldEllipsis} label={t('userMenu.audit')} onClick={() => handleNavigate('/audit')} />
      </div>
      <div className={menuSeparatorClass} />
      <div className="flex min-h-[var(--control-h-sm)] items-center gap-2.5 px-2.5 text-footnote text-label-3">
        <Info className="size-4" />
        <SystemVersionText />
      </div>
      <div className={menuSeparatorClass} />
      <button type="button" role="menuitem" disabled={isSigningOut} onClick={handleSignOut} className={cn(menuButtonClass, 'text-danger')}>
        <LogOut className="size-4" />
        {t('userMenu.logout')}
      </button>
    </>
  )
}

/** 当前账户的头像（无头像时显示名称前两个字母） */
export function CurrentAccountAvatar({ className }: { className?: string }) {
  const { user, displayName } = useCurrentAccount()
  return <UserAvatar avatar={user?.avatar} name={displayName} className={className} />
}

function UserAvatar({ avatar, name, className }: { avatar?: string; name: string; className?: string }) {
  return (
    <div className={cn('flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-full bg-fill-2 text-xs font-semibold uppercase text-label', className)}>
      {avatar ? <img src={avatar} alt="" className="h-full w-full object-cover" /> : getInitials(name)}
    </div>
  )
}

function UserMenuItem({
  icon: Icon,
  label,
  onClick,
}: {
  icon: ComponentType<{ className?: string }>
  label: string
  onClick: () => void
}) {
  return (
    <button type="button" role="menuitem" onClick={onClick} className={menuButtonClass}>
      <Icon className="size-4 text-label-2" />
      {label}
    </button>
  )
}

function getInitials(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return 'AD'
  return trimmed.slice(0, 2).toUpperCase()
}
