import { Activity, ArrowLeft, Maximize2, Minimize2, MonitorUp, Pause, Play, RotateCcw } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { ProtectedRoute } from '@/components/auth/auth-guard'
import { LyraSky } from '@/features/auth/components/lyra-sky'
import { Button } from '@/components/ui/button'
import { StatusBadge } from '@/components/common/status-badge'
import { useMobileDevice, useMobileLayout } from '@/hooks/use-media-query'
import { useTrafficFlowSession } from '@/features/traffic-flow/lib/use-traffic-flow-session'
import { TrafficFlowFeed } from './traffic-flow-feed'
import { TrafficFlowKpis } from './traffic-flow-kpis'
import { TrafficFlowStage } from './traffic-flow-stage'
import { TrafficFlowTokensDialog } from './traffic-flow-tokens-dialog'
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
        <MonitorUp className="traffic-flow-desktop-required-icon" aria-hidden="true" />
        <i>{t('desktopRequired.eyebrow')}</i>
        <h1>{t('desktopRequired.title')}</h1>
        <p>{t('desktopRequired.description')}</p>
        <Button asChild variant="secondary" className="mt-6">
          <Link to="/dashboard"><ArrowLeft aria-hidden="true" />{t('desktopRequired.back')}</Link>
        </Button>
      </div>
    </main>
  )
}

function TrafficFlowPage() {
  const { t } = useTranslation('traffic-flow')
  const navigate = useNavigate()
  const session = useTrafficFlowSession()
  const [tokensOpen, setTokensOpen] = useState(false)
  const [pageEl, setPageEl] = useState<HTMLElement | null>(null)
  const kpisRef = useRef<HTMLElement>(null)
  const tokensButtonRef = useRef<HTMLButtonElement>(null)

  return (
    <main ref={setPageEl} className={tokensOpen ? 'traffic-flow-page is-tokens-open' : 'traffic-flow-page'}>
      <LyraSky constellation={false} placement="wide" className="traffic-flow-starfield" />

      <header className="traffic-flow-header traffic-flow-surface">
        <Button type="button" variant="ghost" size="sm" className="traffic-flow-brand" onClick={() => navigate('/dashboard')}>
          <ArrowLeft className="size-4" />
          <span>xLyra</span>
        </Button>
        <div className="traffic-flow-title">
          <Activity className="traffic-flow-title-mark" />
          <strong>{t('header.title')}</strong>
        </div>
        <div className="traffic-flow-actions">
          <StatusBadge status={session.connected ? 'healthy' : 'warning'} className="traffic-flow-status">
            {session.connected ? t('status.live') : t('status.reconnecting')}
          </StatusBadge>
          <Button type="button" variant="ghost" size="icon-sm" onClick={() => session.setPaused((value) => !value)} aria-label={session.paused ? t('actions.resume') : t('actions.pause')}>
            {session.paused ? <Play className="size-4" /> : <Pause className="size-4" />}
          </Button>
          <Button type="button" variant="ghost" size="icon-sm" onClick={() => window.location.reload()} aria-label={t('actions.refresh')}>
            <RotateCcw className="size-4" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={() => void session.togglePageFullscreen(pageEl)}
            aria-label={session.fullscreen ? t('actions.exitFullscreen') : t('actions.fullscreen')}
          >
            {session.fullscreen ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
          </Button>
        </div>
      </header>

      <TrafficFlowKpis
        inFlight={session.activeRequests.length}
        routed={session.routedCount}
        nodes={session.nodeCount}
        tokens={session.displayedTokens}
        rpmLimit={session.rpmLimit}
        tokensOpen={tokensOpen}
        kpisRef={kpisRef}
        tokensButtonRef={tokensButtonRef}
        t={t}
        onTokensClick={() => setTokensOpen((open) => !open)}
      />

      <TrafficFlowStage
        topology={session.topology}
        requests={session.visibleRequests}
        retiringRequestIDs={session.retiringRequestIDs}
        selectedRequest={session.selectedRequest}
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
        onRequestDrained={session.finalizeRetirement}
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

      <TrafficFlowTokensDialog
        open={tokensOpen}
        cells={Object.values(session.usageCells)}
        selectedNode={session.selectedNode}
        container={pageEl}
        kpisRef={kpisRef}
        tokensButtonRef={tokensButtonRef}
        t={t}
        onOpenChange={setTokensOpen}
        highlightNode={session.highlightNode}
      />
    </main>
  )
}

function selectedNodeName(session: ReturnType<typeof useTrafficFlowSession>) {
  if (!session.selectedNode || !session.topology) return undefined
  const nodes = session.selectedNode.kind === 'downstream' ? session.topology.downstream : session.topology.upstream
  return nodes.find((item) => item.id === session.selectedNode?.id)?.name
}
