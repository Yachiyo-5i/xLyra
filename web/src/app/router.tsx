import { lazy, Suspense, type ComponentType, type ReactNode } from 'react'
import { createBrowserRouter, Navigate } from 'react-router-dom'
import { PublicOnlyRoute } from '@/components/auth/auth-guard'

function lazyNamed<T extends ComponentType>(
  loader: () => Promise<Record<string, T>>,
  exportName: string,
) {
  return lazy(async () => {
    const mod = await loader()
    return { default: mod[exportName] }
  })
}

const APIKeysPage = lazyNamed(() => import('@/routes/api-keys'), 'APIKeysPage')
const AnalyticsPage = lazyNamed(() => import('@/routes/analytics'), 'AnalyticsPage')
const AgentPage = lazyNamed(() => import('@/routes/agent'), 'AgentPage')
const AuditLogsPage = lazyNamed(() => import('@/routes/audit-logs-page'), 'AuditLogsPage')
const BackupSettingsPage = lazyNamed(() => import('@/routes/settings/backup-settings-page'), 'BackupSettingsPage')
const DashboardPage = lazyNamed(() => import('@/routes/dashboard'), 'DashboardPage')
const GeneralSettingsPage = lazyNamed(() => import('@/routes/settings/global/general-settings-page'), 'GeneralSettingsPage')
const AgentSettingsPage = lazyNamed(() => import('@/routes/settings/agent-settings-page'), 'AgentSettingsPage')
const GlobalConfigPage = lazyNamed(() => import('@/routes/settings/global-config-page'), 'GlobalConfigPage')
const LoginPage = lazyNamed(() => import('@/routes/login'), 'LoginPage')
const ModelsPage = lazyNamed(() => import('@/routes/models'), 'ModelsPage')
const ModelsPriceSettingsPage = lazyNamed(() => import('@/routes/settings/models-price-settings-page'), 'ModelsPriceSettingsPage')
const SystemProxySettingsPage = lazyNamed(() => import('@/routes/settings/global/system-proxy-settings-page'), 'SystemProxySettingsPage')
const NotFoundPage = lazyNamed(() => import('@/routes/not-found'), 'NotFoundPage')
const OAuthPage = lazyNamed(() => import('@/routes/oauth'), 'OAuthPage')
const PlaygroundPage = lazyNamed(() => import('@/routes/playground'), 'PlaygroundPage')
const PortalPage = lazyNamed(() => import('@/routes/portal'), 'PortalPage')
const PortalSettingsPage = lazyNamed(() => import('@/routes/settings/global/portal-settings-page'), 'PortalSettingsPage')
const ProfileSettingsPage = lazyNamed(() => import('@/routes/settings/global/profile-settings-page'), 'ProfileSettingsPage')
const ProtectedLayout = lazyNamed(() => import('@/app/protected-layout'), 'ProtectedLayout')
const RateLimitSettingsPage = lazyNamed(() => import('@/routes/settings/global/rate-limit-settings-page'), 'RateLimitSettingsPage')
const RegisterPage = lazyNamed(() => import('@/routes/register'), 'RegisterPage')
const RequestsPage = lazyNamed(() => import('@/routes/requests'), 'RequestsPage')
const RoutesPage = lazyNamed(() => import('@/routes/routing'), 'RoutesPage')
const SiteGroupsSettingsPage = lazyNamed(() => import('@/routes/settings/global/site-groups-settings-page'), 'SiteGroupsSettingsPage')
const SitesPage = lazyNamed(() => import('@/routes/sites'), 'SitesPage')
const TrafficFlowPage = lazyNamed(() => import('@/routes/traffic-flow'), 'TrafficFlowPage')

function lazyElement(element: ReactNode) {
  return (
    <Suspense
      fallback={
        <div className="flex h-full min-h-[280px] items-center justify-center text-sm text-muted-soft">
          Loading...
        </div>
      }
    >
      {element}
    </Suspense>
  )
}

export const appRouter = createBrowserRouter([
  {
    path: '/',
    element: lazyElement(<ProtectedLayout />),
    children: [
      { index: true, element: <Navigate to="/dashboard" replace /> },
      { path: 'playground', element: lazyElement(<PlaygroundPage />) },
      { path: 'agent', element: lazyElement(<AgentPage />) },
      { path: 'dashboard', element: lazyElement(<DashboardPage />) },
      { path: 'analytics', element: lazyElement(<AnalyticsPage />) },
      { path: 'sites', element: lazyElement(<SitesPage />) },
      { path: 'models', element: lazyElement(<ModelsPage />) },
      { path: 'oauth', element: lazyElement(<OAuthPage />) },
      { path: 'api-keys', element: lazyElement(<APIKeysPage />) },
      { path: 'routes', element: lazyElement(<RoutesPage />) },
      { path: 'requests', element: lazyElement(<RequestsPage />) },
      { path: 'audit', element: lazyElement(<AuditLogsPage />) },
      {
        path: 'settings',
        children: [
          { index: true, element: <Navigate to="/settings/global" replace /> },
          {
            path: 'global',
            element: lazyElement(<GlobalConfigPage />),
            children: [
              { index: true, element: <Navigate to="/settings/global/general" replace /> },
              { path: 'profile', element: lazyElement(<ProfileSettingsPage />) },
              { path: 'general', element: lazyElement(<GeneralSettingsPage />) },
              { path: 'system-proxy', element: lazyElement(<SystemProxySettingsPage />) },
              { path: 'rate-limit', element: lazyElement(<RateLimitSettingsPage />) },
              { path: 'site-groups', element: lazyElement(<SiteGroupsSettingsPage />) },
              { path: 'portal', element: lazyElement(<PortalSettingsPage />) },
            ],
          },
          { path: 'agent', element: lazyElement(<AgentSettingsPage />) },
          { path: 'models-price', element: lazyElement(<ModelsPriceSettingsPage />) },
          { path: 'backup', element: lazyElement(<BackupSettingsPage />) },
        ],
      },
      { path: '*', element: lazyElement(<NotFoundPage />) },
    ],
  },
  {
    path: '/traffic-flow',
    element: lazyElement(<TrafficFlowPage />),
  },
  {
    path: '/login',
    element: (
      <PublicOnlyRoute mode="login">
        {lazyElement(<LoginPage />)}
      </PublicOnlyRoute>
    ),
  },
  {
    path: '/register',
    element: (
      <PublicOnlyRoute mode="register">
        {lazyElement(<RegisterPage />)}
      </PublicOnlyRoute>
    ),
  },
  {
    path: '/portal',
    element: lazyElement(<PortalPage />),
  },
])
