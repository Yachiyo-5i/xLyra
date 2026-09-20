import { useEffect, useRef, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { registerSW } from 'virtual:pwa-register'
import { Button } from '@/components/ui/button'
import {
  UPDATE_CHECK_INTERVAL_MS,
  fetchRemoteBuild,
  hasRemoteFrontendUpdate,
  readClientBuildId,
} from '@/lib/frontend-update'

const CLIENT_BUILD_ID = readClientBuildId()

export function PwaUpdatePrompt() {
  const { t } = useTranslation('common')
  const [needsRefresh, setNeedsRefresh] = useState(false)
  const [isReloading, setIsReloading] = useState(false)
  const registrationRef = useRef<ServiceWorkerRegistration>(undefined)
  const updateAvailableRef = useRef(false)
  const updateCheckRef = useRef<Promise<void> | null>(null)

  useEffect(() => {
    const markUpdateAvailable = (registration?: ServiceWorkerRegistration) => {
      if (updateAvailableRef.current) {
        return
      }
      if (registration) {
        registrationRef.current = registration
      }
      updateAvailableRef.current = true
      setNeedsRefresh(true)
    }

    const checkForUpdate = () => {
      if (
        updateAvailableRef.current ||
        updateCheckRef.current ||
        document.visibilityState !== 'visible' ||
        !navigator.onLine
      ) {
        return
      }

      updateCheckRef.current = Promise.all([
        fetchRemoteBuild(),
        registrationRef.current?.update().catch(() => undefined) ?? Promise.resolve(),
      ])
        .then(([remoteBuild]) => {
          if (updateAvailableRef.current) {
            return
          }

          const registration = registrationRef.current
          if (registration?.waiting || registration?.installing || hasRemoteFrontendUpdate(remoteBuild, CLIENT_BUILD_ID)) {
            markUpdateAvailable(registration)
          }
        })
        .catch(() => undefined)
        .finally(() => {
          updateCheckRef.current = null
        })
    }

    registerSW({
      immediate: true,
      onNeedRefresh: () => {
        void navigator.serviceWorker.getRegistration().then((currentRegistration) => {
          markUpdateAvailable(registrationRef.current ?? currentRegistration)
        })
      },
      onRegisteredSW: (_swUrl, currentRegistration) => {
        registrationRef.current = currentRegistration
        checkForUpdate()
      },
    })

    const intervalId = window.setInterval(checkForUpdate, UPDATE_CHECK_INTERVAL_MS)

    window.addEventListener('focus', checkForUpdate)
    window.addEventListener('online', checkForUpdate)
    window.addEventListener('pageshow', checkForUpdate)
    document.addEventListener('visibilitychange', checkForUpdate)

    return () => {
      window.clearInterval(intervalId)
      window.removeEventListener('focus', checkForUpdate)
      window.removeEventListener('online', checkForUpdate)
      window.removeEventListener('pageshow', checkForUpdate)
      document.removeEventListener('visibilitychange', checkForUpdate)
    }
  }, [])

  const handleReload = () => {
    setIsReloading(true)

    const reloadPage = (() => {
      let reloadStarted = false
      return () => {
        if (reloadStarted) {
          return
        }
        reloadStarted = true
        window.location.reload()
      }
    })()

    const activateWaitingWorker = (worker: ServiceWorker) => {
      navigator.serviceWorker.addEventListener('controllerchange', reloadPage, { once: true })
      window.setTimeout(reloadPage, 8000)
      try {
        worker.postMessage({ type: 'SKIP_WAITING' })
      } catch {
        reloadPage()
      }
    }

    void (async () => {
      const registration = registrationRef.current ?? (await navigator.serviceWorker.getRegistration())
      const waitingWorker = registration?.waiting ?? registration?.installing
      if (waitingWorker) {
        activateWaitingWorker(waitingWorker)
        return
      }

      try {
        await registration?.unregister()
      } catch {
        // Old SW would keep serving the precached shell; ignore unregister failures and reload anyway.
      }
      reloadPage()
    })()
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
