import {
  useEffect,
  useMemo,
  useState,
  type PropsWithChildren,
} from 'react'
import { ThemeContext, type ThemeContextValue } from '@/hooks/theme-context'
import {
  THEME_STORAGE_KEY,
  parseStoredTheme,
  resolveThemeMode,
  serializeTheme,
  type ResolvedThemeMode,
  type ThemeSettings,
} from '@/lib/theme'

function getSystemPrefersDark() {
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

function updateThemeMeta(resolvedMode: ResolvedThemeMode) {
  const themeColor = resolvedMode === 'dark' ? '#0f141f' : '#f4f7fb'
  let meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')

  if (!meta) {
    meta = document.createElement('meta')
    meta.name = 'theme-color'
    document.head.appendChild(meta)
  }

  meta.content = themeColor
}

export function ThemeProvider({ children }: PropsWithChildren) {
  const [theme, setThemeState] = useState<ThemeSettings>(() =>
    parseStoredTheme(window.localStorage.getItem(THEME_STORAGE_KEY)),
  )
  const [systemPrefersDark, setSystemPrefersDark] = useState(() => getSystemPrefersDark())

  useEffect(() => {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const listener = (event: MediaQueryListEvent) => setSystemPrefersDark(event.matches)

    mediaQuery.addEventListener('change', listener)

    return () => mediaQuery.removeEventListener('change', listener)
  }, [])

  const resolvedMode = useMemo(
    () => resolveThemeMode(theme.mode, systemPrefersDark),
    [systemPrefersDark, theme.mode],
  )

  useEffect(() => {
    const root = document.documentElement

    root.classList.remove('dark', 'light')
    root.classList.add(resolvedMode)
    root.dataset.base = theme.base
    root.dataset.accent = theme.accent
    root.style.colorScheme = resolvedMode

    updateThemeMeta(resolvedMode)
    window.localStorage.setItem(THEME_STORAGE_KEY, serializeTheme(theme))
  }, [resolvedMode, theme])

  const value = useMemo<ThemeContextValue>(
    () => ({
      ...theme,
      resolvedMode,
      setMode: (mode) => setThemeState((current) => ({ ...current, mode })),
      setBase: (base) => setThemeState((current) => ({ ...current, base })),
      setAccent: (accent) => setThemeState((current) => ({ ...current, accent })),
      setTheme: (nextTheme) => setThemeState((current) => ({ ...current, ...nextTheme })),
    }),
    [resolvedMode, theme],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}
