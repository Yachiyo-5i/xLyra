import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createTrafficFlowStream,
  getTrafficFlowTopology,
  type TrafficFlowEvent,
  type TrafficFlowRequest,
  type TrafficFlowSnapshot,
  type TrafficFlowUsageTotal,
} from '@/features/traffic-flow/api/traffic-flow'
import { bumpActivityBucket, emptyActivityBuckets, syncActivityBuckets, type ActivityBucket } from '@/features/traffic-flow/lib/activity-buckets'
import { modelVisual } from '@/features/traffic-flow/lib/model-visual'
import { fetchRateLimitSettings } from '@/features/settings/api/settings'
import { sameNode, type TrafficFlowNodeRef } from '@/features/traffic-flow/lib/wing-layout'

const flowDrainFallbackDuration = 5200

export function useTrafficFlowSession() {
  const queryClient = useQueryClient()
  const [requests, setRequests] = useState<Record<string, TrafficFlowRequest>>({})
  const [retiringRequestIDs, setRetiringRequestIDs] = useState<Set<string>>(() => new Set())
  const [selectedRequestID, setSelectedRequestID] = useState<string | null>(null)
  const [selectedNode, setSelectedNode] = useState<TrafficFlowNodeRef | null>(null)
  const [hoveredNode, setHoveredNode] = useState<TrafficFlowNodeRef | null>(null)
  const [activityBuckets, setActivityBuckets] = useState<ActivityBucket[]>(() => emptyActivityBuckets())
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
  const pageRef = useRef<HTMLElement>(null)
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
  const requestsRef = useRef(requests)
  requestsRef.current = requests
  const topologyQuery = useQuery({ queryKey: ['traffic-flow', 'topology'], queryFn: getTrafficFlowTopology, refetchOnWindowFocus: 'always', staleTime: 30_000, refetchInterval: 60_000 })
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
    if (previousTotal === null || total <= previousTotal) return
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
    setSelectedRequestID((current) => current === requestID ? null : current)
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
        if (isNewRequest && !pausedRef.current) setActivityBuckets((current) => bumpActivityBucket(current))
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
  const activeColors = useMemo(() => {
    const seen = new Set<string>()
    const colors: string[] = []
    for (const request of activeRequests) {
      const color = modelVisual(request.model_provider, request.model_key).color
      if (!seen.has(color)) {
        seen.add(color)
        colors.push(color)
      }
    }
    return colors
  }, [activeRequests])

  useEffect(() => {
    if (activeColors.length <= 1) return
    const timer = window.setInterval(() => setColorCycleIndex((index) => index + 1), 1600)
    return () => window.clearInterval(timer)
  }, [activeColors.length])

  useEffect(() => {
    const timer = window.setInterval(() => setActivityBuckets((current) => syncActivityBuckets(current)), 15_000)
    return () => window.clearInterval(timer)
  }, [])

  const clearSelection = useCallback(() => {
    setSelectedRequestID(null)
    setSelectedNode(null)
  }, [])

  const selectNode = useCallback((node: TrafficFlowNodeRef | null) => {
    if (!node) {
      clearSelection()
      return
    }
    setSelectedNode((current) => {
      if (sameNode(current, node)) {
        setSelectedRequestID(null)
        return null
      }
      setSelectedRequestID(null)
      return node
    })
  }, [clearSelection])

  const selectRequest = useCallback((requestID: string | null) => {
    if (!requestID) {
      setSelectedRequestID(null)
      return
    }
    const request = requestsRef.current[requestID]
    setSelectedRequestID(requestID)
    if (!request) return
    setSelectedNode(request.upstream_site_id
      ? { kind: 'upstream', id: request.upstream_site_id }
      : { kind: 'downstream', id: request.api_key_id })
  }, [])

  const nodeCount = (topology?.downstream.length ?? 0) + (topology?.gateway ? 1 : 0) + (topology?.upstream.length ?? 0)
  const rateLimit = rateLimitQuery.data?.rate_limit
  const rpmLimit = rateLimit?.status === 'enabled' && rateLimit.rpm_limit != null ? rateLimit.rpm_limit : null

  return {
    topology,
    requests,
    visibleRequests,
    activeRequests,
    retiringRequestIDs,
    selectedRequestID,
    selectedRequest: selectedRequestID ? requests[selectedRequestID] : undefined,
    selectedNode,
    hoveredNode,
    setHoveredNode,
    selectNode,
    selectRequest,
    clearSelection,
    activityBuckets,
    connected,
    paused,
    setPaused,
    fullscreen,
    gatewayPulseKey,
    gatewayColor: activeColors.length === 0 ? '#c8c8c8' : activeColors[colorCycleIndex % activeColors.length],
    displayedTokens,
    downstreamTokenUsage,
    upstreamTokenUsage,
    windowStart,
    windowEnd,
    routedCount: activeRequests.filter((request) => request.upstream_site_id).length,
    nodeCount,
    rpmLimit,
    finalizeRetirement,
    pageRef,
    togglePageFullscreen: () => toggleFullscreen(pageRef.current, setFullscreen),
  }
}

export async function toggleFullscreen(element: HTMLElement | null, setFullscreen: (value: boolean) => void) {
  if (!element) return
  if (document.fullscreenElement) {
    await document.exitFullscreen()
    setFullscreen(false)
    return
  }
  await element.requestFullscreen()
  setFullscreen(true)
}
