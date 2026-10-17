import { useEffect, useRef, type MutableRefObject } from 'react'
import { mulberry32 } from '@/features/auth/components/lyra-sky-layers'
import type { TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import { createFrameGovernor, type QualityTier } from '@/features/traffic-flow/lib/frame-governor'
import { linkControls, pointOnCubic } from '@/features/traffic-flow/lib/flow-packets'
import type { FlowPulseView } from '@/features/traffic-flow/lib/use-flow-motion'
import { nodeKey } from '@/features/traffic-flow/lib/wing-layout'

/**
 * 星际航道层：所有节点到中枢的航道、星尘、请求彗星和失败火花都画在这一块 Canvas 里。
 *
 * 性能上的取舍（低配设备也要顺）：
 * - 航道本身只在布局或状态变化时画进一张离屏画布，之后每帧只 drawImage 一次；
 * - 每帧动态的只有星尘（几十个小点）和彗星（每颗最多十来段线）；
 * - 没有请求时降到 30 帧，减少动态效果时只在变化时重画；
 * - 画质档位按真实帧间隔自动下调（见 frame-governor）：中档星尘减半、彗星尾巴变短，低档只留彗星；
 * - 动画数据（飞行位置）通过 ref 传入，不经过 React 渲染。
 */

export type LaneState = 'idle' | 'active' | 'hot' | 'dim'

export type LaneView = {
  key: string
  kind: 'downstream' | 'upstream'
  id: string
  x: number
  y: number
  state: LaneState
  /** 该航道当前请求的主色（十六进制）；没有请求时用冷白 */
  color: string
}

type FlowCanvasProps = {
  lanes: LaneView[]
  pulsesRef: MutableRefObject<FlowPulseView[]>
  selectedRequestId?: string
  requests: TrafficFlowRequest[]
  paused: boolean
  reducedMotion: boolean
}

type Rgb = [number, number, number]

/** 航道两端的不透明度（节点端、中段），到中枢端一律渐隐到 0 */
const LANE_ALPHA: Record<LaneState, [number, number]> = {
  idle: [0.34, 0.12],
  active: [0.85, 0.42],
  hot: [1, 0.7],
  dim: [0.1, 0.04],
}
const LANE_WIDTH: Record<LaneState, number> = { idle: 1.1, active: 1.6, hot: 2, dim: 1 }
const DUST_ALPHA: Record<LaneState, number> = { idle: 0.45, active: 1, hot: 1, dim: 0.1 }
const COOL_WHITE: Rgb = [231, 235, 243]
const SPARK_RED: Rgb = [255, 69, 58]

const DUST_PER_LANE: Record<QualityTier, number> = { 0: 0, 1: 2, 2: 5 }
const TAIL_SEGMENTS: Record<QualityTier, number> = { 0: 3, 1: 6, 2: 10 }
const IDLE_FRAME_MS = 33

type Controls = ReturnType<typeof linkControls>
type Dust = { phase: number; speed: number; size: number }
type Spark = { x: number; y: number; vx: number; vy: number; born: number }
type LaneCache = { lane: LaneView; controls: Controls; dust: Dust[]; rgb: Rgb }

function parseHex(value: string): Rgb {
  const hex = value.replace('#', '')
  const full = hex.length === 3 ? hex.split('').map((char) => char + char).join('') : hex
  const parsed = Number.parseInt(full, 16)
  if (Number.isNaN(parsed)) return COOL_WHITE
  return [(parsed >> 16) & 255, (parsed >> 8) & 255, parsed & 255]
}

function hashString(value: string) {
  let hash = 2166136261
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

const rgba = ([r, g, b]: Rgb, alpha: number) => `rgba(${r}, ${g}, ${b}, ${alpha.toFixed(3)})`

export function FlowCanvas({ lanes, pulsesRef, selectedRequestId, requests, paused, reducedMotion }: FlowCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const lanesRef = useRef(lanes)
  const selectedRef = useRef(selectedRequestId)
  const pausedRef = useRef(paused)
  const reducedRef = useRef(reducedMotion)
  const cacheRef = useRef(new Map<string, LaneCache>())
  const sparksRef = useRef<Spark[]>([])
  const seenFailedRef = useRef<Set<string> | null>(null)
  const sizeRef = useRef({ width: 0, height: 0 })
  const dirtyRef = useRef(true)

  useEffect(() => {
    lanesRef.current = lanes
    dirtyRef.current = true
  }, [lanes])

  useEffect(() => {
    selectedRef.current = selectedRequestId
  }, [selectedRequestId])

  useEffect(() => {
    pausedRef.current = paused
    reducedRef.current = reducedMotion
    dirtyRef.current = true
  }, [paused, reducedMotion])

  // 失败的请求在航道中段碎成几点红色火花（第一次拿到的快照里已有的失败不算）
  useEffect(() => {
    const seen = seenFailedRef.current
    const failed = requests.filter((request) => request.phase === 'failed')
    if (!seen) {
      seenFailedRef.current = new Set(failed.map((request) => request.request_id))
      return
    }
    for (const request of failed) {
      if (seen.has(request.request_id)) continue
      seen.add(request.request_id)
      const key = request.upstream_site_id ? nodeKey('upstream', request.upstream_site_id) : nodeKey('downstream', request.api_key_id)
      const entry = cacheRef.current.get(key)
      if (!entry || reducedRef.current) continue
      const { p0, p1, p2, p3 } = entry.controls
      const origin = pointOnCubic(p0, p1, p2, p3, 0.55)
      const now = performance.now()
      for (let index = 0; index < 7; index += 1) {
        const angle = (index / 7) * Math.PI * 2 + Math.random() * 0.8
        const speed = 18 + Math.random() * 34
        sparksRef.current.push({ x: origin.x, y: origin.y, vx: Math.cos(angle) * speed, vy: Math.sin(angle) * speed, born: now })
      }
    }
  }, [requests])

  useEffect(() => {
    const canvas = canvasRef.current
    const context = canvas?.getContext('2d')
    if (!canvas || !context) return

    const governor = createFrameGovernor()
    const layer = document.createElement('canvas')
    const layerContext = layer.getContext('2d')!
    let dpr = 1
    let frameId = 0
    let last = performance.now()
    let lastDraw = 0
    let time = 0
    let builtFor: LaneView[] | null = null

    const resize = () => {
      const width = canvas.clientWidth
      const height = canvas.clientHeight
      sizeRef.current = { width, height }
      dpr = Math.min(window.devicePixelRatio || 1, governor.tier >= 2 ? 1.5 : 1)
      for (const element of [canvas, layer]) {
        element.width = Math.max(1, Math.round(width * dpr))
        element.height = Math.max(1, Math.round(height * dpr))
      }
      builtFor = null
      dirtyRef.current = true
    }

    // 航道的几何与静态外观：只在布局 / 状态变化时重建
    const rebuild = () => {
      const { width, height } = sizeRef.current
      builtFor = lanesRef.current
      const cache = new Map<string, LaneCache>()
      layerContext.setTransform(dpr, 0, 0, dpr, 0, 0)
      layerContext.clearRect(0, 0, width, height)
      layerContext.lineCap = 'round'
      for (const lane of lanesRef.current) {
        const controls = linkControls(lane, { width, height })
        const rgb = lane.state === 'idle' ? COOL_WHITE : parseHex(lane.color)
        const random = mulberry32(hashString(lane.key))
        const dust: Dust[] = Array.from({ length: DUST_PER_LANE[2] }, () => ({
          phase: random(),
          speed: 0.035 + random() * 0.05,
          size: 0.8 + random() * 0.9,
        }))
        cache.set(lane.key, { lane, controls, dust, rgb })

        const { p0, p1, p2, p3 } = controls
        const [head, middle] = LANE_ALPHA[lane.state]
        const gradient = layerContext.createLinearGradient(p0.x, p0.y, p3.x, p3.y)
        gradient.addColorStop(0, rgba(rgb, head))
        gradient.addColorStop(0.55, rgba(rgb, middle))
        gradient.addColorStop(1, rgba(rgb, 0))
        layerContext.strokeStyle = gradient
        layerContext.beginPath()
        layerContext.moveTo(p0.x, p0.y)
        layerContext.bezierCurveTo(p1.x, p1.y, p2.x, p2.y, p3.x, p3.y)
        if (lane.state === 'hot') {
          layerContext.globalAlpha = 0.2
          layerContext.lineWidth = 6
          layerContext.stroke()
          layerContext.globalAlpha = 1
        }
        layerContext.lineWidth = LANE_WIDTH[lane.state]
        layerContext.stroke()
      }
      cacheRef.current = cache
    }

    const drawDust = (tier: QualityTier) => {
      const count = DUST_PER_LANE[tier]
      if (count === 0) return
      for (const entry of cacheRef.current.values()) {
        const { controls, dust, rgb, lane } = entry
        const strength = DUST_ALPHA[lane.state]
        context.fillStyle = rgba(rgb, 1)
        for (let index = 0; index < count; index += 1) {
          const particle = dust[index]
          const progress = (particle.phase + time * particle.speed) % 1
          // 下游航道的星尘飘向中枢，上游航道的从中枢飘出
          const t = lane.kind === 'downstream' ? progress : 1 - progress
          const point = pointOnCubic(controls.p0, controls.p1, controls.p2, controls.p3, t)
          context.globalAlpha = Math.sin(progress * Math.PI) * 0.7 * strength
          context.fillRect(point.x - particle.size / 2, point.y - particle.size / 2, particle.size, particle.size)
        }
      }
      context.globalAlpha = 1
    }

    const drawComets = (tier: QualityTier) => {
      const selected = selectedRef.current
      const segments = TAIL_SEGMENTS[tier]
      context.lineCap = 'round'
      for (const pulse of pulsesRef.current) {
        const entry = cacheRef.current.get(pulse.seatKey)
        if (!entry) continue
        const { p0, p1, p2, p3 } = entry.controls
        const rgb = parseHex(pulse.color)
        const response = pulse.role === 'response'
        const dim = selected && pulse.requestId !== selected ? 0.16 : 1
        const tail = response ? 0.12 : 0.2
        const forward = pulse.toward ? -1 : 1
        let previous = pointOnCubic(p0, p1, p2, p3, pulse.t)
        // 从头部往尾部一段一段画：越靠尾越细、越透明
        for (let index = 1; index <= segments; index += 1) {
          const fraction = index / segments
          const t = Math.min(1, Math.max(0, pulse.t + forward * tail * fraction))
          const point = pointOnCubic(p0, p1, p2, p3, t)
          context.strokeStyle = rgba(rgb, (1 - fraction) ** 1.4 * (response ? 0.55 : 0.85) * dim)
          context.lineWidth = (response ? 1.2 : 1.8) * (1 - fraction * 0.75)
          context.beginPath()
          context.moveTo(previous.x, previous.y)
          context.lineTo(point.x, point.y)
          context.stroke()
          previous = point
        }
        const head = pointOnCubic(p0, p1, p2, p3, pulse.t)
        context.fillStyle = rgba(rgb, 0.22 * dim)
        context.beginPath()
        context.arc(head.x, head.y, response ? 5 : 7, 0, Math.PI * 2)
        context.fill()
        context.fillStyle = rgba(rgb, (response ? 0.8 : 1) * dim)
        context.beginPath()
        context.arc(head.x, head.y, response ? 1.7 : 2.3, 0, Math.PI * 2)
        context.fill()
      }
    }

    const drawSparks = (now: number) => {
      const sparks = sparksRef.current
      if (sparks.length === 0) return
      sparksRef.current = sparks.filter((spark) => now - spark.born < 700)
      for (const spark of sparksRef.current) {
        const age = (now - spark.born) / 700
        const seconds = (now - spark.born) / 1000
        context.fillStyle = rgba(SPARK_RED, (1 - age) * 0.9)
        context.beginPath()
        context.arc(spark.x + spark.vx * seconds, spark.y + spark.vy * seconds, 1.6 * (1 - age * 0.5), 0, Math.PI * 2)
        context.fill()
      }
    }

    const frame = (now: number) => {
      frameId = requestAnimationFrame(frame)
      const delta = now - last
      last = now
      const tier = governor.sample(delta)

      const reduced = reducedRef.current
      const busy = pulsesRef.current.length > 0 || sparksRef.current.length > 0
      if (reduced && !dirtyRef.current) return
      // 空闲时只剩缓慢漂移的星尘，30 帧足够
      if (!busy && !reduced && now - lastDraw < IDLE_FRAME_MS) return
      lastDraw = now

      if (builtFor !== lanesRef.current) rebuild()
      dirtyRef.current = false
      if (!pausedRef.current && !reduced) time += Math.min(delta, 100) / 1000

      const { width, height } = sizeRef.current
      context.setTransform(dpr, 0, 0, dpr, 0, 0)
      context.clearRect(0, 0, width, height)
      context.drawImage(layer, 0, 0, width, height)
      if (reduced) return
      context.globalCompositeOperation = 'lighter'
      drawDust(tier)
      drawComets(tier)
      drawSparks(performance.now())
      context.globalCompositeOperation = 'source-over'
    }

    resize()
    const observer = new ResizeObserver(resize)
    observer.observe(canvas)
    frameId = requestAnimationFrame(frame)
    return () => {
      cancelAnimationFrame(frameId)
      observer.disconnect()
    }
  }, [pulsesRef])

  return <canvas ref={canvasRef} className="traffic-flow-links" aria-hidden="true" />
}
