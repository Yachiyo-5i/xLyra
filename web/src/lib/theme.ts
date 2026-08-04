export type ThemeMode = 'light' | 'dark' | 'system'
export type ThemeBase = 'neutral' | 'stone' | 'zinc' | 'gray'
export type ThemeAccent =
  | 'default'
  | 'amber'
  | 'blue'
  | 'cyan'
  | 'fuchsia'
  | 'green'
  | 'indigo'
  | 'lime'
  | 'orange'
  | 'pink'
export type ResolvedThemeMode = 'light' | 'dark'

export type ThemeSettings = {
  mode: ThemeMode
  base: ThemeBase
  accent: ThemeAccent
}

export const THEME_STORAGE_KEY = 'xlyra-theme'

const DEFAULT_THEME_SETTINGS: ThemeSettings = {
  mode: 'light',
  base: 'zinc',
  accent: 'amber',
}

export type TFunction = (key: string) => string

export function getThemeModeOptions(t?: TFunction): Array<{
  value: ThemeMode
  label: string
  shortLabel: string
}> {
  return [
    { value: 'light', label: t ? t('theme.modeLight') : '浅色', shortLabel: t ? t('theme.modeLight') : '浅色' },
    { value: 'dark', label: t ? t('theme.modeDark') : '深色', shortLabel: t ? t('theme.modeDark') : '深色' },
    { value: 'system', label: t ? t('theme.modeSystem') : '系统', shortLabel: t ? t('theme.modeSystem') : '系统' },
  ]
}

const themeModeOptions = getThemeModeOptions()

export const themeBaseOptions: Array<{
  value: ThemeBase
  label: string
  swatch: string
}> = [
  { value: 'neutral', label: 'Neutral', swatch: '#7c7c7f' },
  { value: 'stone', label: 'Stone', swatch: '#8d857b' },
  { value: 'zinc', label: 'Zinc', swatch: '#71717a' },
  { value: 'gray', label: 'Gray', swatch: '#7a8598' },
]

export const themeAccentOptions: Array<{
  value: ThemeAccent
  label: string
  swatch: string
}> = [
  { value: 'default', label: 'Default', swatch: '#7c7c7f' },
  { value: 'amber', label: 'Amber', swatch: '#f59e0b' },
  { value: 'blue', label: 'Blue', swatch: '#2563eb' },
  { value: 'cyan', label: 'Cyan', swatch: '#0891b2' },
  { value: 'fuchsia', label: 'Fuchsia', swatch: '#c026d3' },
  { value: 'green', label: 'Green', swatch: '#22c55e' },
  { value: 'indigo', label: 'Indigo', swatch: '#6366f1' },
  { value: 'lime', label: 'Lime', swatch: '#84cc16' },
  { value: 'orange', label: 'Orange', swatch: '#f97316' },
  { value: 'pink', label: 'Pink', swatch: '#ec4899' },
]

export function resolveThemeMode(mode: ThemeMode, prefersDark: boolean): ResolvedThemeMode {
  if (mode === 'system') return prefersDark ? 'dark' : 'light'
  return mode
}

export function parseStoredTheme(rawValue: string | null): ThemeSettings {
  if (!rawValue) return DEFAULT_THEME_SETTINGS

  if (rawValue === 'light' || rawValue === 'dark') {
    return { ...DEFAULT_THEME_SETTINGS, mode: rawValue }
  }

  try {
    const parsed = JSON.parse(rawValue) as Partial<ThemeSettings>

    return {
      mode: parsed.mode && themeModeOptions.some((option) => option.value === parsed.mode)
        ? parsed.mode
        : DEFAULT_THEME_SETTINGS.mode,
      base: parsed.base && themeBaseOptions.some((option) => option.value === parsed.base)
        ? parsed.base
        : DEFAULT_THEME_SETTINGS.base,
      accent: parsed.accent && themeAccentOptions.some((option) => option.value === parsed.accent)
        ? parsed.accent
        : DEFAULT_THEME_SETTINGS.accent,
    }
  } catch {
    return DEFAULT_THEME_SETTINGS
  }
}

export function serializeTheme(settings: ThemeSettings) {
  return JSON.stringify(settings)
}

export function getResolvedThemeLabel(mode: ResolvedThemeMode, t?: TFunction) {
  if (t) return mode === 'dark' ? t('theme.displayDark') : t('theme.displayLight')
  return mode === 'dark' ? '深色' : '浅色'
}
