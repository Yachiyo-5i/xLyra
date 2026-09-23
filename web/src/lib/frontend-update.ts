export const UPDATE_CHECK_INTERVAL_MS = 30_000
export const VERSION_URL = '/version.json'

export function readClientBuildId(envBuildId?: string): string {
  const value = envBuildId ?? import.meta.env.VITE_BUILD_ID
  return value && value.length > 0 ? value : 'development'
}

export function parseRemoteBuild(data: unknown): string | undefined {
  if (typeof data !== 'object' || data === null) {
    return undefined
  }

  const build = 'build' in data ? data.build : undefined
  return typeof build === 'string' && build.length > 0 ? build : undefined
}

export function hasRemoteFrontendUpdate(remoteBuild: string | undefined, clientBuild: string): boolean {
  if (!remoteBuild || clientBuild === 'development') {
    return false
  }

  return remoteBuild !== clientBuild
}

export async function fetchRemoteBuild(fetcher: typeof fetch = fetch): Promise<string | undefined> {
  try {
    const response = await fetcher(VERSION_URL, { cache: 'no-store' })
    if (!response.ok) {
      return undefined
    }

    return parseRemoteBuild(await response.json())
  } catch {
    return undefined
  }
}

export const FRONTEND_RELOAD_PARAM = '__xlyra_reload'

export type FrontendUpdateWorker = {
  state: string
  postMessage: (message: { type: 'SKIP_WAITING' }) => void
  addEventListener: (type: 'statechange', listener: () => void) => void
  removeEventListener: (type: 'statechange', listener: () => void) => void
}

export type FrontendUpdateRegistration = {
  waiting: FrontendUpdateWorker | null
  installing: FrontendUpdateWorker | null
  update: () => Promise<unknown>
  addEventListener: (type: 'updatefound', listener: () => void) => void
  removeEventListener: (type: 'updatefound', listener: () => void) => void
  unregister?: () => Promise<boolean>
}

type CacheBucket = {
  keys: () => Promise<string[]>
  delete: (name: string) => Promise<boolean>
}

export function cacheBustedHref(href: string, now: number): string {
  const url = new URL(href)
  url.searchParams.set(FRONTEND_RELOAD_PARAM, String(now))
  return url.toString()
}

export function hrefWithoutReloadParam(href: string): string | null {
  const url = new URL(href)
  if (!url.searchParams.has(FRONTEND_RELOAD_PARAM)) {
    return null
  }
  url.searchParams.delete(FRONTEND_RELOAD_PARAM)
  return url.toString()
}

export async function resolveUpdatedWorker(registration: FrontendUpdateRegistration): Promise<FrontendUpdateWorker | null> {
  const current = registration.waiting ?? registration.installing
  if (current) {
    return current
  }

  let discovered: FrontendUpdateWorker | null = null
  const onFound = () => {
    discovered = registration.installing ?? registration.waiting
  }
  registration.addEventListener('updatefound', onFound)
  try {
    await registration.update()
  } catch {
    return discovered ?? registration.waiting ?? registration.installing
  } finally {
    registration.removeEventListener('updatefound', onFound)
  }
  return discovered ?? registration.waiting ?? registration.installing
}

export function waitForInstalledWorker(worker: FrontendUpdateWorker, timeoutMs: number): Promise<void> {
  if (worker.state === 'installed' || worker.state === 'activated') {
    return Promise.resolve()
  }

  return new Promise((resolve) => {
    const finish = () => {
      window.clearTimeout(timer)
      worker.removeEventListener('statechange', onChange)
      resolve()
    }
    const onChange = () => {
      if (worker.state === 'installed' || worker.state === 'activated' || worker.state === 'redundant') {
        finish()
      }
    }
    const timer = window.setTimeout(finish, timeoutMs)
    worker.addEventListener('statechange', onChange)
  })
}

export async function clearFrontendCaches(cacheStorage: CacheBucket): Promise<void> {
  const names = await cacheStorage.keys()
  await Promise.all(names.map((name) => cacheStorage.delete(name)))
}

export async function applyFrontendUpdate(input: {
  registration: FrontendUpdateRegistration | null
  href: string
  now: number
  reload: (href: string) => void
  waitForControllerChange: (timeoutMs: number) => Promise<void>
  clearCaches: () => Promise<void>
  unregister: (registration: FrontendUpdateRegistration) => Promise<void>
}): Promise<void> {
  const reloadOnce = (() => {
    let started = false
    return () => {
      if (started) {
        return
      }
      started = true
      input.reload(cacheBustedHref(input.href, input.now))
    }
  })()

  const worker = input.registration ? await resolveUpdatedWorker(input.registration) : null
  if (worker && input.registration) {
    await waitForInstalledWorker(worker, 4000)
    const controllerChange = input.waitForControllerChange(4000)
    try {
      worker.postMessage({ type: 'SKIP_WAITING' })
    } catch {
      await input.clearCaches()
      await input.unregister(input.registration)
      reloadOnce()
      return
    }
    await controllerChange
    reloadOnce()
    return
  }

  await input.clearCaches()
  if (input.registration) {
    await input.unregister(input.registration)
  }
  reloadOnce()
}
