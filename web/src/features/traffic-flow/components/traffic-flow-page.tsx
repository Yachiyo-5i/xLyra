import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, ArrowLeft, CircleHelp, Database, Maximize2, Minimize2, MonitorUp, Network, Pause, Play, Radio, RotateCcw, ShieldCheck, Undo2, Waypoints, X, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { ProtectedRoute } from '@/components/auth/auth-guard'
import { useMobileDevice, useMobileLayout } from '@/hooks/use-media-query'
import {
  createTrafficFlowStream,
  getTrafficFlowTopology,
  type TrafficFlowEvent,
  type TrafficFlowRequest,
  type TrafficFlowSnapshot,
  type TrafficFlowTopology,
  type TrafficFlowUsageTotal,
} from '@/features/traffic-flow/api/traffic-flow'
import { modelVisual } from '@/features/traffic-flow/lib/model-visual'
import { fetchRateLimitSettings } from '@/features/settings/api/settings'
import { GatewayCore } from './gateway-core/GatewayCore'
import { TrafficFlowMapCanvas } from './traffic-flow-map-canvas'
import type { FlowNode } from './traffic-flow-map-geometry'
import './traffic-flow-page.css'

const flowDrainFallbackDuration = 5200

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
  return <main className="traffic-flow-desktop-required">
    <div className="traffic-flow-desktop-required-grid" aria-hidden="true" />
    <div className="traffic-flow-desktop-required-content">
      <span className="traffic-flow-desktop-required-icon"><MonitorUp aria-hidden="true" /></span>
      <i>{t('desktopRequired.eyebrow')}</i>
      <h1>{t('desktopRequired.title')}</h1>
      <p>{t('desktopRequired.description')}</p>
      <Link to="/dashboard"><ArrowLeft aria-hidden="true" />{t('desktopRequired.back')}</Link>
    </div>
  </main>
}

function TrafficFlowPage() {
  const { t } = useTranslation('traffic-flow')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [requests, setRequests] = useState<Record<string, TrafficFlowRequest>>({})
  const [retiringRequestIDs, setRetiringRequestIDs] = useState<Set<string>>(() => new Set())
  const [selectedRequestID, setSelectedRequestID] = useState<string | null>(null)
  const [connected, setConnected] = useState(false)
  const [paused, setPaused] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [gatewayPulseKey, setGatewayPulseKey] = useState(0)
  const [colorCycleIndex, setColorCycleIndex] = useState(0)
  const [tokenTarget, setTokenTarget] = useState(0)
  const [displayedTokens, setDisplayedTokens] = useState(0)
  const [downstreamTokenUsage, setDownstreamTokenUsage] = useState<Record<string, TrafficFlowUsageTotal>>({})
  const [upstreamTokenUsage, setUpstreamTokenUsage] = useState<Record<string, TrafficFlowUsageTotal>>({})
  const [windowStart] = useState(() => new Date())
  const [windowEnd, setWindowEnd] = useState(() => new Date())
  const pageRef = useRef<HTMLDivElement>(null)
  const retirementTimersRef = useRef<Map<string, number>>(new Map())
  const knownRequestIDsRef = useRef<Set<string>>(new Set())
  const knownNodeIDsRef = useRef<Set<string> | null>(null)
  const refetchedNodeIDsRef = useRef<Set<string>>(new Set())
  const lastSequenceRef = useRef(0)
  const topologyRefetchAtRef = useRef(0)
  const pausedRef = useRef(false)
  const lastGatewayPulseAtRef = useRef(Number.NEGATIVE_INFINITY)
  const lastServerTokenTotalRef = useRef<number | null>(null)
  const lastDownstreamUsageRef = useRef<Record<string, number>>({})
  const lastUpstreamUsageRef = useRef<Record<string, number>>({})
  const usageBaselineReadyRef = useRef({ downstream: false, upstream: false })
  const tokenDisplayRef = useRef(0)
  const topologyQuery = useQuery({ queryKey: ['traffic-flow', 'topology'], queryFn: getTrafficFlowTopology, staleTime: 30_000, refetchInterval: 60_000 })
  const rateLimitQuery = useQuery({ queryKey: ['settings', 'rate-limits'], queryFn: fetchRateLimitSettings, staleTime: 60_000 })
  const topology = topologyQuery.data ?? null

  useEffect(() => {
    pausedRef.current = paused
  }, [paused])

  useEffect(() => {
    if (!topology) return
    const ids = new Set<string>()
    topology.downstream.forEach((node) => ids.add(`downstream:${node.id}`))
    topology.upstream.forEach((node) => ids.add(`upstream:${node.id}`))
    knownNodeIDsRef.current = ids
    refetchedNodeIDsRef.current.forEach((key) => {
      if (ids.has(key)) refetchedNodeIDsRef.current.delete(key)
    })
  }, [topology])

  const applyServerTokenTotal = useCallback((serverTotal: number | undefined) => {
    if (typeof serverTotal !== 'number' || !Number.isFinite(serverTotal)) return
    const total = Math.max(0, Math.floor(serverTotal))
    const previousTotal = lastServerTokenTotalRef.current
    lastServerTokenTotalRef.current = total
    if (previousTotal === null || total <= previousTotal) {
      return
    }
    setTokenTarget((current) => current + total - previousTotal)
  }, [])

  const applyUsageTotals = useCallback((usage: TrafficFlowUsageTotal[] | undefined, kind: 'downstream' | 'upstream', initialize = false) => {
    if (!usage) return
    const previousTotals = kind === 'downstream' ? lastDownstreamUsageRef.current : lastUpstreamUsageRef.current
    const setUsage = kind === 'downstream' ? setDownstreamTokenUsage : setUpstreamTokenUsage
    const establishBaseline = initialize && !usageBaselineReadyRef.current[kind]
    const increments: Array<{ id: string; name: string; delta: number }> = []
    for (const item of usage) {
      if (!item.id || !Number.isFinite(item.total_tokens)) continue
      const total = Math.max(0, Math.floor(item.total_tokens))
      const previous = previousTotals[item.id]
      previousTotals[item.id] = total
      if (establishBaseline || (previous !== undefined && total <= previous)) continue
      const delta = previous === undefined ? total : total - previous
      if (delta <= 0) continue
      increments.push({ id: item.id, name: item.name, delta })
    }
    if (initialize) usageBaselineReadyRef.current[kind] = true
    if (increments.length === 0) return
    setUsage((current) => {
      const next = { ...current }
      for (const { id, name, delta } of increments) {
        const existing = next[id]
        next[id] = { id, name: name || existing?.name || id, total_tokens: (existing?.total_tokens ?? 0) + delta }
      }
      return next
    })
  }, [])

  const applyUsageEvent = useCallback((usage: TrafficFlowUsageTotal | undefined, tokens: number | undefined, kind: 'downstream' | 'upstream') => {
    if (!usage?.id || !Number.isFinite(usage.total_tokens)) return
    const previousTotals = kind === 'downstream' ? lastDownstreamUsageRef.current : lastUpstreamUsageRef.current
    const setUsage = kind === 'downstream' ? setDownstreamTokenUsage : setUpstreamTokenUsage
    const total = Math.max(0, Math.floor(usage.total_tokens))
    const previous = previousTotals[usage.id]
    previousTotals[usage.id] = total
    if (previous !== undefined && total <= previous) return
    const eventTokens = typeof tokens === 'number' && Number.isFinite(tokens) ? Math.max(0, Math.floor(tokens)) : total
    const delta = previous === undefined ? eventTokens : total - previous
    if (delta <= 0) return
    setUsage((current) => {
      const existing = current[usage.id]
      return {
        ...current,
        [usage.id]: {
          id: usage.id,
          name: usage.name || existing?.name || usage.id,
          total_tokens: (existing?.total_tokens ?? 0) + delta,
        },
      }
    })
  }, [])

  const finalizeRetirement = useCallback((requestID: string) => {
    const timer = retirementTimersRef.current.get(requestID)
    if (timer) window.clearTimeout(timer)
    retirementTimersRef.current.delete(requestID)
    setRequests((current) => {
      if (!current[requestID]) return current
      const next = { ...current }
      delete next[requestID]
      return next
    })
    setRetiringRequestIDs((current) => {
      if (!current.has(requestID)) return current
      const next = new Set(current)
      next.delete(requestID)
      return next
    })
  }, [])

  const scheduleRetirementFallback = useCallback((requestID: string) => {
    const schedule = () => {
      const existing = retirementTimersRef.current.get(requestID)
      if (existing !== undefined) window.clearTimeout(existing)
      const timer = window.setTimeout(() => {
        if (!retirementTimersRef.current.has(requestID)) return
        if (pausedRef.current) {
          schedule()
          return
        }
        finalizeRetirement(requestID)
      }, flowDrainFallbackDuration)
      retirementTimersRef.current.set(requestID, timer)
    }
    schedule()
  }, [finalizeRetirement])

  useEffect(() => {
    const syncFullscreen = () => setFullscreen(Boolean(document.fullscreenElement))
    document.addEventListener('fullscreenchange', syncFullscreen)
    return () => document.removeEventListener('fullscreenchange', syncFullscreen)
  }, [])

  useEffect(() => {
    if (tokenTarget === tokenDisplayRef.current) return
    const start = tokenDisplayRef.current
    const startedAt = performance.now()
    let animation = 0
    const render = (time: number) => {
      const progress = Math.min(1, (time - startedAt) / 620)
      const eased = 1 - (1 - progress) ** 3
      const value = Math.round(start + (tokenTarget - start) * eased)
      tokenDisplayRef.current = value
      setDisplayedTokens(value)
      if (progress < 1) animation = window.requestAnimationFrame(render)
    }
    animation = window.requestAnimationFrame(render)
    return () => window.cancelAnimationFrame(animation)
  }, [tokenTarget])

  useEffect(() => {
    const stream = createTrafficFlowStream()
    const isStaleEvent = (sequence: number | undefined) => {
      if (typeof sequence !== 'number' || !Number.isFinite(sequence)) return false
      if (sequence <= lastSequenceRef.current) return true
      lastSequenceRef.current = sequence
      return false
    }
    const requestTopologyRefetch = () => {
      const now = Date.now()
      if (now - topologyRefetchAtRef.current < 5000) return false
      topologyRefetchAtRef.current = now
      void queryClient.invalidateQueries({ queryKey: ['traffic-flow', 'topology'] })
      return true
    }
    const parseSnapshot = (event: MessageEvent<string>) => {
      try {
        const snapshot = JSON.parse(event.data) as TrafficFlowSnapshot
        if (typeof snapshot.sequence === 'number' && Number.isFinite(snapshot.sequence)) lastSequenceRef.current = snapshot.sequence
        setWindowEnd(new Date())
        applyServerTokenTotal(snapshot.total_tokens)
        applyUsageTotals(snapshot.downstream_usage, 'downstream', true)
        applyUsageTotals(snapshot.upstream_usage, 'upstream', true)
        const items = snapshot.requests ?? []
        const snapshotIDs = new Set(items.map((item) => item.request_id))
        const startRetiring: string[] = []
        for (const requestID of knownRequestIDsRef.current) {
          if (!snapshotIDs.has(requestID) && !retirementTimersRef.current.has(requestID)) startRetiring.push(requestID)
        }
        for (const requestID of snapshotIDs) {
          const timer = retirementTimersRef.current.get(requestID)
          if (timer !== undefined) {
            window.clearTimeout(timer)
            retirementTimersRef.current.delete(requestID)
          }
        }
        knownRequestIDsRef.current = snapshotIDs
        startRetiring.forEach(scheduleRetirementFallback)
        setRetiringRequestIDs((current) => {
          const next = new Set(current)
          for (const requestID of snapshotIDs) next.delete(requestID)
          for (const requestID of startRetiring) next.add(requestID)
          return next
        })
        const retainedIDs = new Set(retirementTimersRef.current.keys())
        setRequests((current) => {
          const next: Record<string, TrafficFlowRequest> = {}
          for (const [requestID, request] of Object.entries(current)) {
            if (retainedIDs.has(requestID)) next[requestID] = request
          }
          for (const item of items) next[item.request_id] = item
          return next
        })
      } catch { /* 忽略无法解析的载荷，连接状态由 onopen/onerror 驱动 */ }
    }
    const parseUpsert = (event: MessageEvent<string>) => {
      try {
        const payload = JSON.parse(event.data) as TrafficFlowEvent
        if (isStaleEvent(payload.sequence)) return
        if (!payload.request) return
        setWindowEnd(new Date())
        const knownNodes = knownNodeIDsRef.current
        if (knownNodes) {
          const unknownKeys: string[] = []
          const downstreamKey = `downstream:${payload.request.api_key_id}`
          if (!knownNodes.has(downstreamKey) && !refetchedNodeIDsRef.current.has(downstreamKey)) unknownKeys.push(downstreamKey)
          if (payload.request.upstream_site_id) {
            const upstreamKey = `upstream:${payload.request.upstream_site_id}`
            if (!knownNodes.has(upstreamKey) && !refetchedNodeIDsRef.current.has(upstreamKey)) unknownKeys.push(upstreamKey)
          }
          if (unknownKeys.length > 0 && requestTopologyRefetch()) {
            unknownKeys.forEach((key) => refetchedNodeIDsRef.current.add(key))
          }
        }
        const isNewRequest = !knownRequestIDsRef.current.has(payload.request.request_id)
        knownRequestIDsRef.current.add(payload.request.request_id)
        const now = performance.now()
        if (isNewRequest && now - lastGatewayPulseAtRef.current >= 300) {
          lastGatewayPulseAtRef.current = now
          setGatewayPulseKey((current) => current + 1)
        }
        const timer = retirementTimersRef.current.get(payload.request.request_id)
        if (timer !== undefined) window.clearTimeout(timer)
        retirementTimersRef.current.delete(payload.request.request_id)
        setRetiringRequestIDs((current) => {
          if (!current.has(payload.request!.request_id)) return current
          const next = new Set(current)
          next.delete(payload.request!.request_id)
          return next
        })
        setRequests((current) => ({ ...current, [payload.request!.request_id]: payload.request! }))
      } catch { /* 忽略无法解析的载荷 */ }
    }
    const parseRemove = (event: MessageEvent<string>) => {
      try {
        const payload = JSON.parse(event.data) as TrafficFlowEvent
        if (isStaleEvent(payload.sequence)) return
        if (!payload.request_id) return
        setWindowEnd(new Date())
        const requestID = payload.request_id
        knownRequestIDsRef.current.delete(requestID)
        setRetiringRequestIDs((current) => new Set(current).add(requestID))
        scheduleRetirementFallback(requestID)
      } catch { /* 忽略无法解析的载荷 */ }
    }
    const parseUsage = (event: MessageEvent<string>) => {
      try {
        const payload = JSON.parse(event.data) as TrafficFlowEvent
        if (isStaleEvent(payload.sequence)) return
        setWindowEnd(new Date())
        applyServerTokenTotal(payload.total_tokens)
        applyUsageEvent(payload.downstream_usage, payload.tokens, 'downstream')
        applyUsageEvent(payload.upstream_usage, payload.tokens, 'upstream')
      } catch { /* 忽略无法解析的载荷 */ }
    }
    const closeStream = () => {
      stream.close()
      retirementTimersRef.current.forEach((timer) => window.clearTimeout(timer))
      retirementTimersRef.current.clear()
    }
    stream.onopen = () => setConnected(true)
    stream.onerror = () => setConnected(false)
    stream.addEventListener('snapshot', parseSnapshot)
    stream.addEventListener('upsert', parseUpsert)
    stream.addEventListener('remove', parseRemove)
    stream.addEventListener('usage', parseUsage)
    return closeStream
  }, [applyServerTokenTotal, applyUsageEvent, applyUsageTotals, queryClient, scheduleRetirementFallback])

  const visibleRequests = useMemo(() => Object.values(requests), [requests])
  const activeRequests = useMemo(() => visibleRequests.filter((request) => !retiringRequestIDs.has(request.request_id)), [retiringRequestIDs, visibleRequests])
  const nodes = useMemo(() => buildNodes(topology), [topology])
  const activeColors = useMemo(() => {
    const seen = new Set<string>()
    const colors: string[] = []
    for (const request of activeRequests) {
      const color = modelVisual(request.model_provider, request.model_key).color
      if (!seen.has(color)) { seen.add(color); colors.push(color) }
    }
    return colors
  }, [activeRequests])

  useEffect(() => {
    if (activeColors.length <= 1) return
    const timer = window.setInterval(() => setColorCycleIndex((i) => i + 1), 1600)
    return () => window.clearInterval(timer)
  }, [activeColors.length])

  const gatewayColor = activeColors.length === 0 ? '#8a9490' : activeColors[colorCycleIndex % activeColors.length]
  const selectedRequest = selectedRequestID ? requests[selectedRequestID] : undefined
  const routedCount = activeRequests.filter((request) => request.upstream_site_id).length
  const respondingCount = activeRequests.filter((request) => request.phase === 'responding' || request.phase === 'completed').length
  const rateLimit = rateLimitQuery.data?.rate_limit
  const rpmLimit = rateLimit?.status === 'enabled' && rateLimit.rpm_limit != null ? rateLimit.rpm_limit : null

  return (
    <main ref={pageRef} className="traffic-flow-page">
      <div className="traffic-flow-background" aria-hidden="true" />
      <div className="traffic-flow-scanline" aria-hidden="true" />

      <header className="traffic-flow-header">
        <button type="button" className="traffic-flow-brand" onClick={() => navigate('/dashboard')}>
          <ArrowLeft className="size-4" />
          <span>xLyra</span>
          <i />
          <strong>ROUTE COMMAND</strong>
        </button>
        <div className="traffic-flow-title">
          <Activity className="traffic-flow-title-mark" />
          <div><strong>{t('header.title')}</strong><span>{t('header.subtitle')}</span></div>
        </div>
        <div className="traffic-flow-actions">
          <div className="traffic-flow-status">
            <span className={connected ? 'traffic-flow-status-dot is-live' : 'traffic-flow-status-dot'} />
            <span>{connected ? t('status.live') : t('status.reconnecting')}</span>
          </div>
          <button type="button" className="traffic-flow-icon-button" onClick={() => setPaused((value) => !value)} aria-label={paused ? t('actions.resume') : t('actions.pause')}>
            {paused ? <Play className="size-4" /> : <Pause className="size-4" />}
          </button>
          <button type="button" className="traffic-flow-icon-button" onClick={() => window.location.reload()} aria-label={t('actions.refresh')}><RotateCcw className="size-4" /></button>
          <button type="button" className="traffic-flow-icon-button" onClick={() => toggleFullscreen(pageRef.current, setFullscreen)} aria-label={fullscreen ? t('actions.exitFullscreen') : t('actions.fullscreen')}>
            {fullscreen ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
          </button>
        </div>
      </header>

      <section className="traffic-flow-metrics" aria-label={t('metrics.label')}>
        <Metric icon={Activity} label={t('metrics.inFlight')} value={activeRequests.length.toString().padStart(2, '0')} limit={rpmLimit === null ? undefined : rpmLimit.toLocaleString()} accent="#f2f2f2" />
        <Metric icon={Waypoints} label={t('metrics.routed')} value={routedCount.toString().padStart(2, '0')} accent="#c8c8c8" />
        <Metric icon={Undo2} label={t('metrics.returning')} value={respondingCount.toString().padStart(2, '0')} accent="#969696" />
        <Metric icon={Network} label={t('metrics.nodes')} value={nodes.length.toString().padStart(2, '0')} accent="#dcdcdc" />
        <TokenMetric label={t('metrics.tokens')} value={formatCompactTokens(displayedTokens)} downstream={downstreamTokenUsage} upstream={upstreamTokenUsage} windowStart={windowStart} windowEnd={windowEnd} t={t} />
      </section>

      <section className="traffic-flow-map" aria-label={t('map.label')}>
        <div className="traffic-flow-map-corner traffic-flow-map-corner-tl" />
        <div className="traffic-flow-map-corner traffic-flow-map-corner-tr" />
        <div className="traffic-flow-map-corner traffic-flow-map-corner-bl" />
        <div className="traffic-flow-map-corner traffic-flow-map-corner-br" />
        <div className="traffic-flow-sector traffic-flow-sector-center"><span>02</span>{t('lanes.gateway')}</div>
        <div className="traffic-flow-lane-panel traffic-flow-lane-panel-downstream" aria-hidden="true"><div className="traffic-flow-lane-panel-heading"><i /><div><strong>{t('lanes.downstream')}</strong><span>{t('lanes.downstreamCaption')}</span></div></div></div>
        <div className="traffic-flow-lane-panel traffic-flow-lane-panel-upstream" aria-hidden="true"><div className="traffic-flow-lane-panel-heading"><i /><div><strong>{t('lanes.upstream')}</strong><span>{t('lanes.upstreamCaption')}</span></div></div></div>
        <TrafficFlowMapCanvas nodes={nodes} requests={visibleRequests} retiringRequestIDs={retiringRequestIDs} paused={paused} pulseKey={gatewayPulseKey} ariaLabel={t('map.label')} onRequestRetired={finalizeRetirement} onRequestSelect={setSelectedRequestID} />
        <GatewayCore active={activeRequests.length > 0} load={Math.min(1, activeRequests.length / 8)} pulseKey={gatewayPulseKey} paused={paused} color={gatewayColor} />
        {!topology ? <div className="traffic-flow-empty"><Radio className="size-5" />{t('status.loading')}</div> : null}
        {topology && activeRequests.length === 0 ? <div className="traffic-flow-idle"><CircleHelp className="size-4" />{t('status.waiting')}</div> : null}
      </section>

      <section className={`traffic-flow-activity ${selectedRequest ? 'is-detail' : ''}`} aria-label={t('activity.label')}>
        {selectedRequest ? <FlowDetailDock request={selectedRequest} onClose={() => setSelectedRequestID(null)} t={t} /> : <>
          <div className="traffic-flow-activity-heading"><Activity className="size-4" /><span>{t('activity.label')}</span><i /></div>
          <div className="traffic-flow-activity-list">
            {activeRequests.slice(0, 5).map((request) => <FlowActivity key={request.request_id} request={request} onSelect={() => setSelectedRequestID(request.request_id)} t={t} />)}
            {activeRequests.length === 0 ? <span className="traffic-flow-activity-empty">{t('activity.empty')}</span> : null}
          </div>
          <div className="traffic-flow-activity-state"><ShieldCheck className="size-4" /><span>NODE SYNCED</span></div>
        </>}
      </section>

      <footer className="traffic-flow-footer">
        <div className="traffic-flow-legend"><span className="traffic-flow-legend-line" />{t('legend.request')}<span className="traffic-flow-legend-return" />{t('legend.response')}</div>
        <span>{t('footer.desktopOnly')}</span>
      </footer>
    </main>
  )
}

function Metric({ icon: Icon, label, value, limit, accent, hasDetails = false, onPointerEnter, onPointerMove, onPointerLeave }: { icon: LucideIcon; label: string; value: string; limit?: string; accent: string; hasDetails?: boolean; onPointerEnter?: (event: ReactPointerEvent<HTMLDivElement>) => void; onPointerMove?: (event: ReactPointerEvent<HTMLDivElement>) => void; onPointerLeave?: () => void }) {
  return <div className={`traffic-flow-metric${hasDetails ? ' has-details' : ''}`} style={{ '--metric-accent': accent } as CSSProperties} onPointerEnter={onPointerEnter} onPointerMove={onPointerMove} onPointerLeave={onPointerLeave}><span>{label}</span><strong>{value}{limit ? <small> / {limit}</small> : null}</strong><Icon className="traffic-flow-metric-icon" aria-hidden="true" /><i /></div>
}

function TokenMetric({ label, value, downstream, upstream, windowStart, windowEnd, t }: { label: string; value: string; downstream: Record<string, TrafficFlowUsageTotal>; upstream: Record<string, TrafficFlowUsageTotal>; windowStart: Date; windowEnd: Date; t: (key: string) => string }) {
  const [position, setPosition] = useState<{ x: number; y: number } | null>(null)
  const trackPointer = (event: ReactPointerEvent<HTMLDivElement>) => setPosition({ x: event.clientX, y: event.clientY })
  return <>
    <Metric icon={Database} label={label} value={value} accent="#b7d8c5" hasDetails onPointerEnter={trackPointer} onPointerMove={trackPointer} onPointerLeave={() => setPosition(null)} />
    {position ? <TokenUsageTooltip position={position} value={value} downstream={downstream} upstream={upstream} windowStart={windowStart} windowEnd={windowEnd} t={t} /> : null}
  </>
}

function TokenUsageTooltip({ position, value, downstream, upstream, windowStart, windowEnd, t }: { position: { x: number; y: number }; value: string; downstream: Record<string, TrafficFlowUsageTotal>; upstream: Record<string, TrafficFlowUsageTotal>; windowStart: Date; windowEnd: Date; t: (key: string) => string }) {
  const downstreamItems = sortUsageTotals(downstream)
  const upstreamItems = sortUsageTotals(upstream)
  const tooltipWidth = 460
  const tooltipHeight = Math.min(310, window.innerHeight - 24)
  const preferredLeft = position.x + 18 + tooltipWidth > window.innerWidth - 12 ? position.x - tooltipWidth - 18 : position.x + 18
  const preferredTop = position.y + 18 + tooltipHeight > window.innerHeight - 12 ? position.y - tooltipHeight - 18 : position.y + 18
  const left = Math.max(12, Math.min(preferredLeft, window.innerWidth - tooltipWidth - 12))
  const top = Math.max(12, Math.min(preferredTop, window.innerHeight - tooltipHeight - 12))
  const crossDay = windowStart.toDateString() !== windowEnd.toDateString()
  return <aside className="traffic-flow-token-tooltip" style={{ left, top }} role="tooltip">
    <div className="traffic-flow-token-tooltip-heading"><Database aria-hidden="true" /><span>{t('tokenDetails.title')}</span><time>{formatWindowTime(windowStart, crossDay)} — {formatWindowTime(windowEnd, crossDay)}</time><strong>{value}</strong></div>
    <div className="traffic-flow-token-tooltip-grid">
      <TokenUsageSection title={t('tokenDetails.downstream')} items={downstreamItems} empty={t('tokenDetails.empty')} />
      <TokenUsageSection title={t('tokenDetails.upstream')} items={upstreamItems} empty={t('tokenDetails.empty')} />
    </div>
  </aside>
}

function TokenUsageSection({ title, items, empty }: { title: string; items: TrafficFlowUsageTotal[]; empty: string }) {
  const total = items.reduce((sum, item) => sum + item.total_tokens, 0)
  return <section className="traffic-flow-token-usage-section"><header><span>{title}</span><strong>{formatCompactTokens(total)}</strong></header><div>{items.length > 0 ? items.map((item) => <p key={item.id} style={{ '--usage-share': `${total > 0 ? Math.max(2, item.total_tokens / total * 100) : 0}%` } as CSSProperties}><span title={item.name}>{item.name}</span><strong>{item.total_tokens.toLocaleString()}</strong></p>) : <small>{empty}</small>}</div></section>
}

function sortUsageTotals(usage: Record<string, TrafficFlowUsageTotal>) {
  return Object.values(usage).sort((left, right) => right.total_tokens - left.total_tokens || left.name.localeCompare(right.name))
}

function formatCompactTokens(value: number) {
  if (value < 1000) return value.toString()
  const units = [{ threshold: 1_000_000_000, suffix: 'B' }, { threshold: 1_000_000, suffix: 'M' }, { threshold: 1000, suffix: 'K' }]
  const unit = units.find((candidate) => value >= candidate.threshold) ?? units[units.length - 1]
  const scaled = value / unit.threshold
  const decimals = scaled >= 100 ? 0 : scaled >= 10 ? 1 : 2
  return `${Number(scaled.toFixed(decimals))}${unit.suffix}`
}

function formatWindowTime(value: Date, withDate: boolean) {
  const pad = (unit: number) => String(unit).padStart(2, '0')
  const time = `${pad(value.getHours())}:${pad(value.getMinutes())}:${pad(value.getSeconds())}`
  return withDate ? `${pad(value.getMonth() + 1)}-${pad(value.getDate())} ${time}` : time
}

function buildNodes(topology: TrafficFlowTopology | null): FlowNode[] {
  const result: FlowNode[] = []
  const seen = new Set<string>()
  const add = (node: FlowNode) => {
    const key = `${node.kind}:${node.id}`
    if (!seen.has(key)) { seen.add(key); result.push(node) }
  }
  topology?.downstream.forEach((node) => add({ ...node, kind: 'downstream' }))
  if (topology?.gateway) add({ ...topology.gateway, kind: 'gateway' })
  topology?.upstream.forEach((node) => add({ ...node, kind: 'upstream' }))
  return result
}

function FlowActivity({ request, onSelect, t }: { request: TrafficFlowRequest; onSelect: () => void; t: (key: string) => string }) {
  const visual = modelVisual(request.model_provider, request.model_key)
  return <button type="button" className="traffic-flow-activity-item" onClick={onSelect} style={{ '--model-color': visual.color } as CSSProperties}>
    <span className="traffic-flow-activity-model">{visual.iconPath ? <img src={visual.iconPath} alt="" /> : visual.fallback}</span>
    <span className="traffic-flow-activity-copy"><strong>{request.model_key || t('inspector.unknownModel')}</strong><i>{request.api_key_name} <b>→</b> {request.upstream_site_name || t('inspector.waiting')}</i></span>
    <span className="traffic-flow-activity-phase">{t(`phase.${request.phase}`)}</span>
  </button>
}

function FlowDetailDock({ request, onClose, t }: { request: TrafficFlowRequest; onClose: () => void; t: (key: string) => string }) {
  const visual = modelVisual(request.model_provider, request.model_key)
  return <div className="traffic-flow-detail-dock" style={{ '--model-color': visual.color } as CSSProperties}>
    <div className="traffic-flow-detail-heading"><Activity className="size-4" /><span>{t('inspector.request')}</span><i /></div>
    <div className="traffic-flow-detail-endpoint"><span>{t('inspector.downstream')}</span><strong>{request.api_key_name}</strong></div>
    <div className="traffic-flow-detail-model"><span className="traffic-flow-detail-model-icon">{visual.iconPath ? <img src={visual.iconPath} alt="" /> : visual.fallback}</span><div><span>{t('inspector.request')}</span><strong>{request.model_key || t('inspector.unknownModel')}</strong></div></div>
    <div className="traffic-flow-detail-endpoint"><span>{t('inspector.upstream')}</span><strong>{request.upstream_site_name || t('inspector.waiting')}</strong></div>
    <div className="traffic-flow-detail-phase"><span>{t('inspector.phase')}</span><strong>{t(`phase.${request.phase}`)}</strong><i>{t('inspector.attempt')} {request.attempt || 1}</i></div>
    <button type="button" className="traffic-flow-detail-close" onClick={onClose} aria-label={t('actions.close')}><X className="size-4" /></button>
  </div>
}

async function toggleFullscreen(element: HTMLElement | null, setFullscreen: (value: boolean) => void) {
  if (!element) return
  if (document.fullscreenElement) { await document.exitFullscreen(); setFullscreen(false); return }
  await element.requestFullscreen()
  setFullscreen(true)
}
