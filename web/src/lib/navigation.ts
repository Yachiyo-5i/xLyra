import {
  Activity,
  BadgeDollarSign,
  Cable,
  ChartNoAxesCombined,
  DatabaseBackup,
  DatabaseZap,
  KeyRound,
  MessagesSquare,
  Route,
  Settings,
  ShieldCheck,
  ShieldEllipsis,
  Waypoints,
  type LucideIcon,
} from 'lucide-react'

export type AppNavItem = {
  to: string
  label: string
  icon: LucideIcon
  description: string
  desktopOnly?: boolean
  children?: AppNavItem[]
}

export type AppNavSection = {
  key: string
  label: string
  items: AppNavItem[]
}

type TFunction = (key: string) => string

export function getAppNavSections(t: TFunction): AppNavSection[] {
  return [
    {
      key: 'workspace',
      label: t('nav.workspace'),
      items: [
        {
          to: '/playground',
          label: t('nav.playground'),
          icon: MessagesSquare,
          description: 'Test chat and image models end-to-end through the gateway.',
        },
      ],
    },
    {
      key: 'overview',
      label: t('nav.overview'),
      items: [
        {
          to: '/dashboard',
          label: t('nav.dashboard'),
          icon: ChartNoAxesCombined,
          description: 'Global health, alerts, throughput and spend at a glance.',
        },
        {
          to: '/traffic-flow',
          label: t('nav.trafficFlow'),
          icon: Waypoints,
          description: 'Monitor live request movement across gateway routes.',
          desktopOnly: true,
        },
      ],
    },
    {
      key: 'resources',
      label: t('nav.resources'),
      items: [
        {
          to: '/sites',
          label: t('nav.sites'),
          icon: Cable,
          description: 'Manage upstream stations, credentials and probe results.',
        },
        {
          to: '/oauth',
          label: t('nav.oauth'),
          icon: ShieldCheck,
          description: 'Connect official Codex accounts and inspect synced identity data.',
        },
        {
          to: '/models',
          label: t('nav.models'),
          icon: DatabaseZap,
          description: 'Normalize aliases and inspect canonical model coverage.',
        },
        {
          to: '/api-keys',
          label: t('nav.apiKeys'),
          icon: KeyRound,
          description: 'Manage downstream gateway keys and model access.',
        }
      ],
    },
    {
      key: 'operations',
      label: t('nav.operations'),
      items: [
        {
          to: '/routes',
          label: t('nav.routes'),
          icon: Route,
          description: 'Define scoring, failover and routing policy behavior.',
        },
        {
          to: '/requests',
          label: t('nav.requests'),
          icon: Activity,
          description: 'Trace request execution, retries and route explanations.',
        },
        {
          to: '/audit',
          label: t('nav.audit'),
          icon: ShieldEllipsis,
          description: 'Admin operation audit logs.',
        },
        {
          to: '/settings',
          label: t('nav.settings'),
          icon: Settings,
          description: 'Runtime defaults, environment details and admin controls.',
          children: [
            {
              to: '/settings/global',
              label: t('nav.globalSettings'),
              icon: Settings,
              description: 'Global gateway configuration and defaults.',
            },
            {
              to: '/settings/models-price',
              label: t('nav.modelsPrice'),
              icon: BadgeDollarSign,
              description: 'Review and maintain upstream model prices.',
            },
            {
              to: '/settings/backup',
              label: t('nav.backup'),
              icon: DatabaseBackup,
              description: 'Export and restore encrypted system backups.',
            },
          ],
        },
      ],
    },
  ]
}

type RedirectTarget = {
  pathname: string
  search?: string
  hash?: string
}

export function buildRedirectSearch(target: RedirectTarget) {
  const redirect = `${target.pathname}${target.search ?? ''}${target.hash ?? ''}`
  return new URLSearchParams({ redirect }).toString()
}

export function resolveAuthRedirectTarget(search: string, fallback = '/dashboard') {
  const redirect = new URLSearchParams(search).get('redirect')

  if (!redirect || !redirect.startsWith('/') || redirect.startsWith('//')) {
    return fallback
  }

  if (redirect === '/login' || redirect === '/register') {
    return fallback
  }

  return redirect
}
