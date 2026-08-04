import { createContext, useContext } from 'react'
import type {
  ResolvedThemeMode,
  ThemeAccent,
  ThemeBase,
  ThemeMode,
  ThemeSettings,
} from '@/lib/theme'

export type ThemeContextValue = ThemeSettings & {
  resolvedMode: ResolvedThemeMode
  setMode: (mode: ThemeMode) => void
  setBase: (base: ThemeBase) => void
  setAccent: (accent: ThemeAccent) => void
  setTheme: (theme: Partial<ThemeSettings>) => void
}

export const ThemeContext = createContext<ThemeContextValue | null>(null)

export function useTheme() {
  const context = useContext(ThemeContext)

  if (!context) {
    throw new Error('useTheme must be used inside ThemeProvider')
  }

  return context
}

export function useResolvedTheme() {
  const { resolvedMode } = useTheme()
  return resolvedMode
}
