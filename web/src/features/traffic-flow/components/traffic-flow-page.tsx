import { Activity, ArrowLeft, Maximize2, Minimize2, MonitorUp, Pause, Play, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { ProtectedRoute } from '@/components/auth/auth-guard'
import { useMobileDevice, useMobileLayout } from '@/hooks/use-media-query'
import { useTrafficFlowSession } from '@/features/traffic-flow/lib/use-traffic-flow-session'
import { TrafficFlowFeed } from './traffic-flow-feed'
import { TrafficFlowKpis } from './traffic-flow-kpis'
import { TrafficFlowStage } from './traffic-flow-stage'
import './traffic-flow-page.css'

export function TrafficFlowRoute() {
  const isMobileLayout = useMobileLayout()
  const isMobileDevice = useMobileDevice()

  return (
    <ProtectedRoute>
      {isMobileLayout || isMobileDevice ? <TrafficFlowDesktopRequired /> : <TrafficFlowPage />}
    </ProtectedRoute>
  )
}

function TrafficFlowDesktopRequired() {
  const { t } = useTranslation('traffic-flow')
  return (
    <main className="traffic-flow-desktop-required">
      <div className="traffic-flow-desktop-required-grid" aria-hidden="true" />
      <div className="traffic-flow-desktop-required-content">
        <span className="traffic-flow-desktop-required-icon"><MonitorUp aria-hidden="true" /></span>
        <i>{t('desktopRequired.eyebrow')}</i>
        <h1>{t('desktopRequired.title')}</h1>
        <p>{t('desktopRequired.description')}</p>
        <Link to="/dashboard"><ArrowLeft aria-hidden="true" />{t('desktopRequired.back')}</Link>
      </div>
    </main>
  )
}

function TrafficFlowPage() {
  const { t } = useTranslation('traffic-flow')
  const navigate = useNavigate()
  const session = useTrafficFlowSession()

  return (
    <main ref={session.pageRef} className="traffic-flow-page">
      <div className="traffic-flow-background" aria-hidden="true" />
      <div className="traffic-flow-scanline" aria-hidden="true" />

      <header className="traffic-flow-header">
        <button type="button" className="traffic-flow-brand" onClick={() => navigate('/dashboard')}>
          <ArrowLeft className="size-4" />
          <span>xLyra</span>
          <i />
          <strong>{t('header.brand')}</strong>
        </button>
        <div className="traffic-flow-title">
          <Activity className="traffic-flow-title-mark" />
          <div>
            <strong>{t('header.title')}</strong>
            <span>{t('header.subtitle')}</span>
          </div>
        </div>
        <div className="traffic-flow-actions">
          <div className="traffic-flow-status">
            <span className={session.connected ? 'traffic-flow-status-dot is-live' : 'traffic-flow-status-dot'} />
            <span>{session.connected ? t('status.live') : t('status.reconnecting')}</span>
          </div>
          <button type="button" className="traffic-flow-icon-button" onClick={() => session.setPaused((value) => !value)} aria-label={session.paused ? t('actions.resume') : t('actions.pause')}>
            {session.paused ? <Play className="size-4" /> : <Pause className="size-4" />}
          </button>
          <button type="button" className="traffic-flow-icon-button" onClick={() => window.location.reload()} aria-label={t('actions.refresh')}>
            <RotateCcw className="size-4" />
          </button>
          <button
            type="button"
            className="traffic-flow-icon-button"
            onClick={() => void session.togglePageFullscreen()}
            aria-label={session.fullscreen ? t('actions.exitFullscreen') : t('actions.fullscreen')}
          >
            {session.fullscreen ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
          </button>
        </div>
      </header>

      <TrafficFlowKpis
        inFlight={session.activeRequests.length}
        routed={session.routedCount}
        nodes={session.nodeCount}
        tokens={session.displayedTokens}
        rpmLimit={session.rpmLimit}
        t={t}
      />

      <TrafficFlowStage
        topology={session.topology}
        requests={session.visibleRequests}
        retiringRequestIDs={session.retiringRequestIDs}
        selectedNode={session.selectedNode}
        hoveredNode={session.hoveredNode}
        paused={session.paused}
        pulseKey={session.gatewayPulseKey}
        gatewayColor={session.gatewayColor}
        activeCount={session.activeRequests.length}
        downstreamUsage={session.downstreamTokenUsage}
        upstreamUsage={session.upstreamTokenUsage}
        activityBuckets={session.activityBuckets}
        onHoverNode={session.setHoveredNode}
        onSelectNode={session.selectNode}
        onClearSelection={session.clearSelection}
      />

      <TrafficFlowFeed
        requests={session.activeRequests}
        selectedRequest={session.selectedRequest}
        selectedNode={session.selectedNode}
        nodeName={selectedNodeName(session)}
        connected={session.connected}
        onSelectRequest={session.selectRequest}
        t={t}
      />

      <footer className="traffic-flow-footer">
        <div className="traffic-flow-legend">
          <span className="traffic-flow-legend-line" />
          {t('legend.request')}
          <span className="traffic-flow-legend-return" />
          {t('legend.response')}
        </div>
        <time>{session.windowStart.toLocaleTimeString()} — {session.windowEnd.toLocaleTimeString()}</time>
        <span>{t('footer.desktopOnly')}</span>
      </footer>
    </main>
  )
}

function selectedNodeName(session: ReturnType<typeof useTrafficFlowSession>) {
  if (!session.selectedNode || !session.topology) return undefined
  const nodes = session.selectedNode.kind === 'downstream' ? session.topology.downstream : session.topology.upstream
  return nodes.find((item) => item.id === session.selectedNode?.id)?.name
}
