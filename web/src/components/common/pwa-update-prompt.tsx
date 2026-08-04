import { useEffect, useRef, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { registerSW } from 'virtual:pwa-register'
import { Button } from '@/components/ui/button'

const UPDATE_CHECK_INTERVAL_MS = 15 * 60 * 1000

export function PwaUpdatePrompt() {
  const { t } = useTranslation('common')
  const [needsRefresh, setNeedsRefresh] = useState(false)
  const [isReloading, setIsReloading] = useState(false)
  const registrationRef = useRef<ServiceWorkerRegistration>(undefined)
  const updateAvailableRef = useRef(false)
  const updateCheckRef = useRef<Promise<void> | null>(null)

  useEffect(() => {
    registerSW({
      immediate: true,
      onNeedRefresh: () => {
        void navigator.serviceWorker.getRegistration().then((currentRegistration) => {
          const registration = registrationRef.current ?? currentRegistration
          if (!registration?.waiting || updateAvailableRef.current) {
            return
          }

          registrationRef.current = registration
          updateAvailableRef.current = true
          setNeedsRefresh(true)
        })
      },
      onRegisteredSW: (_swUrl, currentRegistration) => {
        registrationRef.current = currentRegistration
      },
    })

    const checkForUpdate = () => {
      const registration = registrationRef.current
      if (
        !registration ||
        updateAvailableRef.current ||
        updateCheckRef.current ||
        document.visibilityState !== 'visible' ||
        !navigator.onLine
      ) {
        return
      }

      updateCheckRef.current = registration
        .update()
        .then(() => undefined)
        .catch(() => undefined)
        .finally(() => {
          updateCheckRef.current = null
        })
    }
    const intervalId = window.setInterval(checkForUpdate, UPDATE_CHECK_INTERVAL_MS)

    window.addEventListener('focus', checkForUpdate)
    document.addEventListener('visibilitychange', checkForUpdate)

    return () => {
      window.clearInterval(intervalId)
      window.removeEventListener('focus', checkForUpdate)
      document.removeEventListener('visibilitychange', checkForUpdate)
    }
  }, [])

  const handleReload = () => {
    setIsReloading(true)

    const waitingWorker = registrationRef.current?.waiting
    if (!waitingWorker) {
      window.location.reload()
      return
    }

    let reloadStarted = false
    const reloadPage = () => {
      if (reloadStarted) {
        return
      }
      reloadStarted = true
      window.location.reload()
    }

    navigator.serviceWorker.addEventListener('controllerchange', reloadPage, { once: true })
    window.setTimeout(reloadPage, 5000)

    try {
      waitingWorker.postMessage({ type: 'SKIP_WAITING' })
    } catch {
      reloadPage()
    }
  }

  if (!needsRefresh) {
    return null
  }

  return (
    <div
      aria-labelledby="pwa-update-title"
      aria-live="assertive"
      className="fixed left-1/2 top-[calc(env(safe-area-inset-top,0px)+0.75rem)] z-[100] flex w-[calc(100%-1.5rem)] max-w-md -translate-x-1/2 items-center gap-3 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] p-3 text-foreground shadow-[0_18px_48px_rgba(4,8,18,0.28)] backdrop-blur-xl sm:p-4"
      role="alertdialog"
    >
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <RefreshCw className="mt-0.5 size-5 shrink-0 text-primary" />
        <div className="min-w-0">
          <p id="pwa-update-title" className="text-sm font-semibold">
            {t('pwaUpdate.title')}
          </p>
          <p className="mt-0.5 text-xs leading-5 text-[hsl(var(--text-muted-soft))] sm:text-sm">
            {t('pwaUpdate.description')}
          </p>
        </div>
      </div>
      <Button size="sm" disabled={isReloading} onClick={handleReload}>
        {isReloading ? t('pwaUpdate.reloading') : t('pwaUpdate.reload')}
      </Button>
    </div>
  )
}
