import { useState, type ComponentType } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useNavigate } from 'react-router-dom'
import { ChevronsUpDown, Info, LogOut, Settings, ShieldEllipsis, UserRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
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

export function UserAccountMenu({
  mode = 'sidebar',
  onNavigate,
  side = mode === 'avatar' ? 'bottom' : 'right',
  align = 'end',
}: UserAccountMenuProps) {
  const { t } = useTranslation('common')
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.user)
  const authStatus = useAuthStore((state) => state.status)
  const logout = useAuthStore((state) => state.logout)
  const [open, setOpen] = useState(false)
  const [isSigningOut, setIsSigningOut] = useState(false)
  const isAuthChecking = !user && (authStatus === 'idle' || authStatus === 'checking')
  const displayName = user?.displayName || user?.username || 'Admin'
  const username = user?.username || user?.role || 'Admin'

  function handleUserMenuNavigate(to: string) {
    setOpen(false)
    onNavigate?.()
    navigate(to)
  }

  async function handleSignOut() {
    try {
      setIsSigningOut(true)
      setOpen(false)
      await logout()
      toast.success(t('userMenu.logoutSuccess'))
      onNavigate?.()
      navigate('/login', { replace: true })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('userMenu.logoutFailed'))
    } finally {
      setIsSigningOut(false)
    }
  }

  if (isAuthChecking) {
    return mode === 'avatar' ? (
      <div className="size-10 rounded-full border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-1">
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
          <Button
            variant="ghost"
            size="icon"
            className="size-10 overflow-hidden rounded-full border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-0 text-foreground shadow-[0_14px_34px_rgba(0,0,0,0.1)] backdrop-blur-[32px] backdrop-saturate-150 hover:bg-[hsl(var(--card)/0.34)]"
            aria-label={t('userMenu.profile')}
          >
            <UserAvatar avatar={user?.avatar} name={displayName} className="size-full rounded-full border-0 bg-transparent" />
          </Button>
        ) : (
          <button
            type="button"
            className={cn(
              'flex w-full items-center gap-3 rounded-xl px-3 py-3 text-left transition-colors duration-150 hover:bg-[hsl(var(--surface-soft-hover))] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))]',
              open && 'bg-[hsl(var(--surface-soft-hover))]',
            )}
          >
            <UserAvatar avatar={user?.avatar} name={displayName} />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-semibold text-foreground">{displayName}</p>
              <p className="text-muted-soft truncate text-xs">{username}</p>
            </div>
            <ChevronsUpDown className="text-muted-soft h-4 w-4 shrink-0" />
          </button>
        )}
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          side={side}
          align={align}
          sideOffset={8}
          className="z-[120] w-[240px] overflow-hidden rounded-xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--dialog-surface))] shadow-[var(--shadow-dialog)] backdrop-blur-xl"
        >
          <div className="flex items-center gap-3 px-3 py-3">
            <UserAvatar avatar={user?.avatar} name={displayName} />
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold text-foreground">{displayName}</p>
              <p className="text-muted-soft truncate text-xs">{username}</p>
            </div>
          </div>
          <div className="border-t border-[hsl(var(--divider))] py-1">
            <UserMenuItem icon={UserRound} label={t('userMenu.profile')} onClick={() => handleUserMenuNavigate('/settings/global/profile')} />
            <UserMenuItem icon={Settings} label={t('userMenu.systemSettings')} onClick={() => handleUserMenuNavigate('/settings/global/general')} />
            <UserMenuItem icon={ShieldEllipsis} label={t('userMenu.audit')} onClick={() => handleUserMenuNavigate('/audit')} />
          </div>
          <div className="border-t border-[hsl(var(--divider))] py-1">
            <div className="flex w-full items-center gap-3 px-3 py-2.5 text-sm font-medium text-foreground">
              <Info className="text-muted-soft h-4 w-4" />
              <SystemVersionText />
            </div>
          </div>
          <div className="border-t border-[hsl(var(--divider))] py-1">
            <button
              type="button"
              disabled={isSigningOut}
              onClick={handleSignOut}
              className="flex w-full items-center gap-3 px-3 py-2.5 text-sm font-medium text-[hsl(var(--destructive))] transition-colors hover:bg-[hsl(var(--surface-subtle))] disabled:cursor-not-allowed disabled:opacity-60"
            >
              <LogOut className="h-4 w-4" />
              {t('userMenu.logout')}
            </button>
          </div>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}

function UserAvatar({ avatar, name, className }: { avatar?: string; name: string; className?: string }) {
  return (
    <div className={cn('flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-field))] text-xs font-semibold uppercase text-foreground', className)}>
      {avatar ? (
        <img src={avatar} alt="" className="h-full w-full object-cover" />
      ) : (
        getInitials(name)
      )}
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
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-3 px-3 py-2.5 text-sm font-medium text-foreground transition-colors hover:bg-[hsl(var(--surface-subtle))]"
    >
      <Icon className="text-muted-soft h-4 w-4" />
      {label}
    </button>
  )
}

function getInitials(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return 'AD'
  return trimmed.slice(0, 2).toUpperCase()
}
