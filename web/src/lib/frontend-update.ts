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
