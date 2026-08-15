import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'

const namespaces = [
  'common',
  'auth',
  'settings',
  'audit',
  'models',
  'requests',
  'routes',
  'api-keys',
  'dashboard',
  'analytics',
  'oauth',
  'sites',
  'components',
  'portal',
  'playground',
  'traffic-flow',
] as const

type Locale = 'zh' | 'en' | 'jp'
type Namespace = (typeof namespaces)[number]
type ResourceModule = { default: Record<string, unknown> }

const resourceLoaders: Record<Locale, Record<Namespace, () => Promise<ResourceModule>>> = {
  zh: {
    common: () => import('./zh/common.json'),
    auth: () => import('./zh/auth.json'),
    settings: () => import('./zh/settings.json'),
    audit: () => import('./zh/audit.json'),
    models: () => import('./zh/models.json'),
    requests: () => import('./zh/requests.json'),
    routes: () => import('./zh/routes.json'),
    'api-keys': () => import('./zh/api-keys.json'),
    dashboard: () => import('./zh/dashboard.json'),
    analytics: () => import('./zh/analytics.json'),
    oauth: () => import('./zh/oauth.json'),
    sites: () => import('./zh/sites.json'),
    components: () => import('./zh/components.json'),
    portal: () => import('./zh/portal.json'),
    playground: () => import('./zh/playground.json'),
    'traffic-flow': () => import('./zh/traffic-flow.json'),
  },
  en: {
    common: () => import('./en/common.json'),
    auth: () => import('./en/auth.json'),
    settings: () => import('./en/settings.json'),
    audit: () => import('./en/audit.json'),
    models: () => import('./en/models.json'),
    requests: () => import('./en/requests.json'),
    routes: () => import('./en/routes.json'),
    'api-keys': () => import('./en/api-keys.json'),
    dashboard: () => import('./en/dashboard.json'),
    analytics: () => import('./en/analytics.json'),
    oauth: () => import('./en/oauth.json'),
    sites: () => import('./en/sites.json'),
    components: () => import('./en/components.json'),
    portal: () => import('./en/portal.json'),
    playground: () => import('./en/playground.json'),
    'traffic-flow': () => import('./en/traffic-flow.json'),
  },
  jp: {
    common: () => import('./jp/common.json'),
    auth: () => import('./jp/auth.json'),
    settings: () => import('./jp/settings.json'),
    audit: () => import('./jp/audit.json'),
    models: () => import('./jp/models.json'),
    requests: () => import('./jp/requests.json'),
    routes: () => import('./jp/routes.json'),
    'api-keys': () => import('./jp/api-keys.json'),
    dashboard: () => import('./jp/dashboard.json'),
    analytics: () => import('./jp/analytics.json'),
    oauth: () => import('./jp/oauth.json'),
    sites: () => import('./jp/sites.json'),
    components: () => import('./jp/components.json'),
    portal: () => import('./jp/portal.json'),
    playground: () => import('./jp/playground.json'),
    'traffic-flow': () => import('./jp/traffic-flow.json'),
  },
}

const dynamicResourcesBackend = {
  type: 'backend' as const,
  read(language: string, namespace: string, callback: (error: Error | null, resources?: Record<string, unknown>) => void) {
    const locale = normalizeLocale(language)
    const loader = resourceLoaders[locale]?.[namespace as Namespace]
    if (!loader) {
      callback(new Error(`Missing i18n namespace: ${locale}/${namespace}`))
      return
    }

    loader()
      .then((module) => callback(null, module.default))
      .catch((error: unknown) => callback(error instanceof Error ? error : new Error(String(error))))
  },
}

function normalizeLocale(language: string): Locale {
  const normalized = language.toLowerCase()
  if (normalized.startsWith('en')) return 'en'
  if (normalized.startsWith('ja') || normalized.startsWith('jp')) return 'jp'
  return 'zh'
}

export const i18nReady = i18n
  .use(dynamicResourcesBackend)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    ns: [namespaces[0]],
    defaultNS: 'common',
    fallbackLng: {
      ja: ['jp'],
      default: ['zh'],
    },
    supportedLngs: ['zh', 'en', 'jp', 'ja'],
    interpolation: {
      escapeValue: false,
    },
    detection: {
      order: ['localStorage'],
      lookupLocalStorage: 'xlyra-i18n-lang',
      caches: ['localStorage'],
    },
    react: {
      useSuspense: true,
    },
  })

export default i18n
