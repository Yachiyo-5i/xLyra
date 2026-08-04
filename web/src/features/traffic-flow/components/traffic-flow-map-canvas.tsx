import { useEffect, useRef, type PointerEvent as ReactPointerEvent } from 'react'
import { siteTypeIconPath } from '@/components/common/brand-utils'
import type { TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import { modelVisual } from '@/features/traffic-flow/lib/model-visual'
import { nodePosition, type FlowNode } from './traffic-flow-map-geometry'

type Point = { x: number; y: number }

type RequestVisual = {
  enteredAt: number
  pulseAt: number | null
  retired: boolean
  retiringAt: number | null
  returning: boolean
}

type HitRoute = {
  points: Point[]
  requestID: string
}

type RequestRoute = {
  flowSpeed: number
  points: Point[]
}

type TrafficFlowMapCanvasProps = {
  ariaLabel: string
  nodes: FlowNode[]
  onRequestRetired: (requestID: string) => void
  onRequestSelect: (requestID: string) => void
  paused: boolean
  pulseKey: number
  requests: TrafficFlowRequest[]
  retiringRequestIDs: Set<string>
}

const entryDuration = 800
const retirementDuration = 2600
const pulseDuration = 900
const baseFlowSpeed = 0.00008
const nodeCountFont = '18px "Alarm Clock", "Alimama FangYuanTi VF", monospace'
const nodeImages = new Map<string, HTMLImageElement>()

export function TrafficFlowMapCanvas(props: TrafficFlowMapCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const propsRef = useRef(props)
  const visualsRef = useRef(new Map<string, RequestVisual>())
  const hitRoutesRef = useRef<HitRoute[]>([])
  const hoveredRequestIDRef = useRef<string | null>(null)
  const motionTimeRef = useRef(0)
  const lastPulseKeyRef = useRef(props.pulseKey)

  useEffect(() => {
    propsRef.current = props
  }, [props])

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    const context = canvas.getContext('2d')
    if (!context) return
    const reducedMotionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
    let reducedMotion = reducedMotionQuery.matches
    let width = 0
    let height = 0
    let gatewayY = 0
    let previousFrame = performance.now()
    let animation = 0

    const resize = () => {
      const bounds = canvas.getBoundingClientRect()
      const ratio = Math.min(window.devicePixelRatio || 1, 2)
      width = bounds.width
      height = bounds.height
      gatewayY = Math.max(0, Math.min(height, window.innerHeight / 2 - bounds.top))
      const pixelWidth = Math.max(1, Math.round(width * ratio))
      const pixelHeight = Math.max(1, Math.round(height * ratio))
      if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
        canvas.width = pixelWidth
        canvas.height = pixelHeight
      }
      context.setTransform(ratio, 0, 0, ratio, 0, 0)
    }

    const render = (now: number) => {
      const current = propsRef.current
      const motionPaused = current.paused || reducedMotion
      if (!motionPaused) motionTimeRef.current += Math.min(50, now - previousFrame)
      previousFrame = now
      context.clearRect(0, 0, width, height)
      drawGrid(context, width, height)
      drawBaseNetwork(context, current.nodes, width, height, gatewayY, motionTimeRef.current, reducedMotion)

      const requestIDs = new Set(current.requests.map((request) => request.request_id))
      for (const requestID of visualsRef.current.keys()) {
        if (!requestIDs.has(requestID)) visualsRef.current.delete(requestID)
      }

      for (const request of current.requests) {
        const returning = request.phase === 'responding' || request.phase === 'completed'
        const existing = visualsRef.current.get(request.request_id)
        if (!existing) {
          visualsRef.current.set(request.request_id, {
            enteredAt: motionTimeRef.current,
            pulseAt: current.pulseKey > 0 ? motionTimeRef.current : null,
            retired: false,
            retiringAt: null,
            returning,
          })
        } else if (existing.returning !== returning && !current.retiringRequestIDs.has(request.request_id)) {
          existing.enteredAt = motionTimeRef.current
          existing.returning = returning
        }
      }

      if (lastPulseKeyRef.current !== current.pulseKey) {
        lastPulseKeyRef.current = current.pulseKey
        visualsRef.current.forEach((visual) => { visual.pulseAt = motionTimeRef.current })
      }

      const hitRoutes: HitRoute[] = []
      for (const request of current.requests) {
        const from = current.nodes.find((node) => node.kind === 'downstream' && node.id === request.api_key_id)
        const gateway = current.nodes.find((node) => node.kind === 'gateway')
        const to = current.nodes.find((node) => node.kind === 'upstream' && node.id === request.upstream_site_id)
        const visualState = visualsRef.current.get(request.request_id)
        if (!from || !gateway || !visualState) continue
        const route = requestRoutePoints(from, to, current.nodes, width, height, gatewayY)
        const points = route.points
        hitRoutes.push({ points, requestID: request.request_id })
        const retiring = current.retiringRequestIDs.has(request.request_id)
        if (retiring && visualState.retiringAt === null) visualState.retiringAt = motionTimeRef.current
        if (!retiring) {
          visualState.retiringAt = null
          visualState.retired = false
        }
        if (retiring && visualState.retiringAt !== null && !visualState.retired && (reducedMotion || motionTimeRef.current - visualState.retiringAt >= retirementDuration)) {
          visualState.retired = true
          current.onRequestRetired(request.request_id)
        }
        drawRequest(context, request, visualState, points, route.flowSpeed, motionTimeRef.current, reducedMotion, hoveredRequestIDRef.current === request.request_id)
      }
      drawNodes(context, current.nodes, current.requests, current.retiringRequestIDs, width, height)
      hitRoutesRef.current = hitRoutes
      animation = window.requestAnimationFrame(render)
    }

    const syncReducedMotion = () => { reducedMotion = reducedMotionQuery.matches }
    const observer = new ResizeObserver(resize)
    observer.observe(canvas)
    reducedMotionQuery.addEventListener('change', syncReducedMotion)
    void document.fonts.load(nodeCountFont).catch(() => { /* 字体已在 CSS 中声明，绘制循环会在加载完成后自动采用 */ })
    resize()
    animation = window.requestAnimationFrame(render)
    return () => {
      observer.disconnect()
      reducedMotionQuery.removeEventListener('change', syncReducedMotion)
      window.cancelAnimationFrame(animation)
    }
  }, [])

  const findRequest = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    const point = { x: event.clientX - bounds.left, y: event.clientY - bounds.top }
    for (let index = hitRoutesRef.current.length - 1; index >= 0; index -= 1) {
      const route = hitRoutesRef.current[index]
      if (distanceToRoute(point, route.points) <= 13) return route.requestID
    }
    return null
  }

  const handlePointerMove = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    const requestID = findRequest(event)
    hoveredRequestIDRef.current = requestID
    event.currentTarget.style.cursor = requestID ? 'pointer' : 'default'
  }

  const handlePointerLeave = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    hoveredRequestIDRef.current = null
    event.currentTarget.style.cursor = 'default'
  }

  const handleClick = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    const requestID = findRequest(event)
    if (requestID) propsRef.current.onRequestSelect(requestID)
  }

  return <canvas ref={canvasRef} className="traffic-flow-map-canvas" role="img" aria-label={props.ariaLabel} onClick={handleClick} onPointerLeave={handlePointerLeave} onPointerMove={handlePointerMove} />
}

function drawGrid(context: CanvasRenderingContext2D, width: number, height: number) {
  context.save()
  context.beginPath()
  context.rect(58, 42, Math.max(0, width - 116), Math.max(0, height - 72))
  context.clip()
  context.lineWidth = 1
  context.strokeStyle = 'rgba(255, 255, 255, 0.026)'
  for (let x = 58; x <= width - 58; x += 44) {
    context.beginPath()
    context.moveTo(x, 42)
    context.lineTo(x, height - 30)
    context.stroke()
  }
  for (let y = 42; y <= height - 30; y += 44) {
    context.beginPath()
    context.moveTo(58, y)
    context.lineTo(width - 58, y)
    context.stroke()
  }
  context.restore()
}

function drawBaseNetwork(context: CanvasRenderingContext2D, nodes: FlowNode[], width: number, height: number, gatewayY: number, motionTime: number, reducedMotion: boolean) {
  const gateway = nodes.find((node) => node.kind === 'gateway')
  if (!gateway) return
  context.save()
  context.lineCap = 'round'
  context.lineWidth = 1
  context.strokeStyle = 'rgba(255, 255, 255, 0.16)'
  context.shadowColor = 'rgba(255, 255, 255, 0.22)'
  context.shadowBlur = 2
  context.setLineDash([0.1, 12])
  const routes: Point[][] = []
  for (const node of nodes) {
    if (node.kind === 'gateway') continue
    const points = baseRoutePoints(node, nodes, width, height, gatewayY)
    routes.push(points)
    tracePoints(context, points, 0, 1)
    context.stroke()
  }
  if (!reducedMotion) {
    context.setLineDash([])
    context.shadowColor = 'rgba(255, 255, 255, 0.32)'
    context.shadowBlur = 3
    routes.forEach((points, routeIndex) => {
      const particleCount = Math.max(1, Math.ceil(routeLength(points) / 12))
      for (let index = 0; index < particleCount; index += 1) {
        const progress = (motionTime * baseFlowSpeed + index / particleCount + routeIndex * 0.17) % 1
        const position = pointAtRoute(points, progress)
        const fadeIn = Math.min(1, progress / 0.08)
        const fadeOut = Math.min(1, (1 - progress) / 0.1)
        context.globalAlpha = Math.min(fadeIn, fadeOut) * 0.58
        context.fillStyle = '#e9fffa'
        context.beginPath()
        context.arc(position.x, position.y, 1.35, 0, Math.PI * 2)
        context.fill()
      }
    })
  }
  context.restore()
}

function drawNodes(context: CanvasRenderingContext2D, nodes: FlowNode[], requests: TrafficFlowRequest[], retiringRequestIDs: Set<string>, width: number, height: number) {
  const nodeWidth = Math.min(268, Math.max(210, window.innerWidth * 0.16))
  const nodeHeight = Math.min(64, Math.max(38, window.innerHeight * 0.05))
  const orbSize = Math.min(42, Math.max(28, window.innerHeight * 0.035))
  const downstreamRequests = new Map<string, TrafficFlowRequest>()
  const upstreamRequests = new Map<string, TrafficFlowRequest>()
  const downstreamCounts = new Map<string, number>()
  const upstreamCounts = new Map<string, number>()
  for (const request of requests) {
    if (!downstreamRequests.has(request.api_key_id)) downstreamRequests.set(request.api_key_id, request)
    if (request.upstream_site_id && !upstreamRequests.has(request.upstream_site_id)) upstreamRequests.set(request.upstream_site_id, request)
    if (retiringRequestIDs.has(request.request_id)) continue
    if (request.phase === 'completed' || request.phase === 'failed' || request.phase === 'cancelled') continue
    downstreamCounts.set(request.api_key_id, (downstreamCounts.get(request.api_key_id) ?? 0) + 1)
    if (request.upstream_site_id) upstreamCounts.set(request.upstream_site_id, (upstreamCounts.get(request.upstream_site_id) ?? 0) + 1)
  }
  for (const node of nodes) {
    if (node.kind === 'gateway') continue
    const request = node.kind === 'downstream' ? downstreamRequests.get(node.id) : upstreamRequests.get(node.id)
    const count = (node.kind === 'downstream' ? downstreamCounts.get(node.id) : upstreamCounts.get(node.id)) ?? 0
    const visual = request ? modelVisual(request.model_provider, request.model_key) : undefined
    const failed = request?.phase === 'failed' || request?.phase === 'cancelled'
    const color = failed ? '#a6a6a6' : visual?.color ?? '#d8d8d8'
    const glow = failed ? 'rgba(166, 166, 166, 0.28)' : visual?.glow ?? 'rgba(216, 216, 216, 0.18)'
    const position = scaledPosition(node, nodes, width, height)
    drawNode(context, node, position, nodeWidth, nodeHeight, orbSize, color, glow, Boolean(request), count)
  }
}

function drawNode(context: CanvasRenderingContext2D, node: FlowNode, position: Point, width: number, height: number, orbSize: number, color: string, glow: string, active: boolean, count: number) {
  const scale = active ? 1.015 : 1
  const nodeWidth = width * scale
  const nodeHeight = height * scale
  const left = position.x - nodeWidth / 2
  const top = position.y - nodeHeight / 2
  const right = position.x + nodeWidth / 2
  const bottom = position.y + nodeHeight / 2
  const upstream = node.kind === 'upstream'

  context.save()
  if (active) {
    context.shadowColor = glow
    context.shadowBlur = 22
  }
  const background = context.createLinearGradient(left, top, right, bottom)
  background.addColorStop(0, 'rgba(23, 23, 23, 0.96)')
  background.addColorStop(1, 'rgba(7, 7, 7, 0.92)')
  context.fillStyle = background
  context.fillRect(left, top, nodeWidth, nodeHeight)
  context.shadowBlur = 0
  context.globalAlpha = active ? 0.78 : 0.31
  context.lineWidth = 1
  context.strokeStyle = active ? color : '#ffffff'
  context.strokeRect(left + 0.5, top + 0.5, nodeWidth - 1, nodeHeight - 1)
  context.globalAlpha = active ? 0.9 : 0.45
  context.beginPath()
  context.moveTo(left, top + 11)
  context.lineTo(left, top)
  context.lineTo(left + 11, top)
  context.moveTo(right - 11, bottom)
  context.lineTo(right, bottom)
  context.lineTo(right, bottom - 11)
  context.stroke()

  const anchorX = upstream ? left - 1 : right + 1
  context.globalAlpha = 1
  context.fillStyle = active ? color : '#0d0d0d'
  context.strokeStyle = active ? color : 'rgba(255, 255, 255, 0.58)'
  context.shadowColor = active ? color : 'transparent'
  context.shadowBlur = active ? 13 : 0
  context.fillRect(anchorX - 4, position.y - 4, 8, 8)
  context.strokeRect(anchorX - 3.5, position.y - 3.5, 7, 7)
  context.shadowBlur = 0

  const innerLeft = left + 15
  const innerRight = right - 15
  const signalX = upstream ? innerRight - 10 : innerLeft
  drawNodeSignal(context, signalX, position.y, color, active)
  const orbX = upstream ? signalX - 12 - orbSize : signalX + 22
  drawNodeOrb(context, node, orbX, position.y - orbSize / 2, orbSize, color, glow, active)

  const liveX = upstream ? innerLeft : innerRight - 5
  let liveReserve = active ? 13 : 0
  if (active) {
    context.fillStyle = color
    context.shadowColor = color
    context.shadowBlur = 11
    context.fillRect(liveX, position.y - 2.5, 5, 5)
    context.shadowBlur = 0
    if (count > 0) {
      const countText = String(Math.min(count, 99)).padStart(2, '0')
      context.font = nodeCountFont
      const countWidth = context.measureText(countText).width
      liveReserve += countWidth + 5
      context.fillStyle = color
      context.shadowColor = color
      context.shadowBlur = 8
      context.textAlign = upstream ? 'left' : 'right'
      context.textBaseline = 'middle'
      context.fillText(countText, upstream ? liveX + 10 : liveX - 5, position.y + 0.5)
      context.shadowBlur = 0
    }
  }

  const textStart = upstream ? innerLeft + liveReserve : orbX + orbSize + 12
  const textEnd = upstream ? orbX - 12 : innerRight - liveReserve
  context.fillStyle = '#f0f0f0'
  context.font = '500 11px "Alimama FangYuanTi VF", "Inter", "SF Pro Display", "Segoe UI", sans-serif'
  context.textAlign = upstream ? 'right' : 'left'
  context.textBaseline = 'middle'
  const textX = upstream ? textEnd : textStart
  context.fillText(fitText(context, node.name, Math.max(0, textEnd - textStart)), textX, position.y)
  context.restore()
}

function drawNodeSignal(context: CanvasRenderingContext2D, x: number, centerY: number, color: string, active: boolean) {
  const heights = [5, 10, 15]
  context.save()
  context.fillStyle = active ? color : 'rgba(255, 255, 255, 0.34)'
  context.shadowColor = active ? color : 'transparent'
  context.shadowBlur = active ? 7 : 0
  heights.forEach((height, index) => context.fillRect(x + index * 4, centerY + 7.5 - height, 2, height))
  context.restore()
}

function drawNodeOrb(context: CanvasRenderingContext2D, node: FlowNode, x: number, y: number, size: number, color: string, glow: string, active: boolean) {
  context.save()
  context.fillStyle = 'rgba(7, 7, 7, 0.96)'
  context.strokeStyle = active ? color : 'rgba(255, 255, 255, 0.36)'
  context.shadowColor = active ? glow : 'transparent'
  context.shadowBlur = active ? 15 : 0
  context.fillRect(x, y, size, size)
  context.strokeRect(x + 0.5, y + 0.5, size - 1, size - 1)
  context.shadowBlur = 0
  const iconPath = node.kind === 'upstream' ? siteTypeIconPath(node.site_type ?? '') : undefined
  const image = iconPath ? loadNodeImage(iconPath) : undefined
  if (image?.complete && image.naturalWidth > 0) {
    context.drawImage(image, x + (size - 22) / 2, y + (size - 22) / 2, 22, 22)
  } else {
    context.fillStyle = '#ededed'
    context.font = '10px "Alimama FangYuanTi VF", "Inter", "SF Pro Display", "Segoe UI", sans-serif'
    context.textAlign = 'center'
    context.textBaseline = 'middle'
    context.fillText(node.name.trim().slice(0, 2).toUpperCase(), x + size / 2, y + size / 2)
  }
  context.restore()
}

function loadNodeImage(path: string) {
  const cached = nodeImages.get(path)
  if (cached) return cached
  const image = new Image()
  image.src = path
  nodeImages.set(path, image)
  return image
}

function fitText(context: CanvasRenderingContext2D, value: string, maxWidth: number) {
  if (context.measureText(value).width <= maxWidth) return value
  let fitted = value
  while (fitted.length > 0 && context.measureText(`${fitted}…`).width > maxWidth) fitted = fitted.slice(0, -1)
  return `${fitted}…`
}

function drawRequest(context: CanvasRenderingContext2D, request: TrafficFlowRequest, state: RequestVisual, points: Point[], flowSpeed: number, motionTime: number, reducedMotion: boolean, hovered: boolean) {
  const failed = request.phase === 'failed' || request.phase === 'cancelled'
  const visual = modelVisual(request.model_provider, request.model_key)
  const color = failed ? '#a6a6a6' : visual.color
  const range = visibleRange(state, motionTime, reducedMotion)
  if (range[1] <= range[0]) return
  context.save()
  context.lineCap = 'round'
  context.lineJoin = 'round'
  context.shadowColor = color
  context.shadowBlur = 8
  context.globalAlpha = 0.28
  context.lineWidth = 16
  context.strokeStyle = color
  context.setLineDash([])
  tracePoints(context, points, range[0], range[1])
  context.stroke()

  context.shadowBlur = 5
  context.globalAlpha = 0.76
  context.lineWidth = hovered ? 6 : 5
  tracePoints(context, points, range[0], range[1])
  context.stroke()

  context.globalAlpha = 1
  context.shadowBlur = 7
  context.lineWidth = 8
  context.setLineDash([0.1, 18])
  context.lineDashOffset = (state.returning ? 1 : -1) * motionTime * flowSpeed
  tracePoints(context, points, range[0], range[1])
  context.stroke()

  context.shadowColor = '#ffffff'
  context.shadowBlur = 5
  context.lineWidth = 2.5
  context.strokeStyle = '#ffffff'
  context.setLineDash([0.1, 10])
  context.lineDashOffset = (state.returning ? 1 : -1) * motionTime * flowSpeed
  tracePoints(context, points, range[0], range[1])
  context.stroke()

  if (!reducedMotion && state.pulseAt !== null) {
    const pulseProgress = Math.min(1, (motionTime - state.pulseAt) / pulseDuration)
    if (pulseProgress < 1) {
      context.globalAlpha = Math.max(0, 0.92 * (1 - pulseProgress))
      context.lineWidth = 10 - pulseProgress * 7
      context.strokeStyle = '#effffb'
      context.shadowColor = color
      context.shadowBlur = 8
      context.setLineDash([])
      tracePoints(context, points, range[0], range[1])
      context.stroke()
    }
  }
  context.restore()
}

function visibleRange(state: RequestVisual, motionTime: number, reducedMotion: boolean): [number, number] {
  if (state.retiringAt !== null) {
    const progress = reducedMotion ? 1 : Math.min(1, (motionTime - state.retiringAt) / retirementDuration)
    return state.returning ? [0, 1 - progress] : [progress, 1]
  }
  const progress = reducedMotion ? 1 : easeOut(Math.min(1, (motionTime - state.enteredAt) / entryDuration))
  return state.returning ? [1 - progress, 1] : [0, progress]
}

function easeOut(value: number) {
  return 1 - Math.pow(1 - value, 3)
}

function baseRoutePoints(node: FlowNode, nodes: FlowNode[], width: number, height: number, gatewayY: number) {
  const from = scaledPosition(node, nodes, width, height)
  const center = { x: width * 0.5, y: gatewayY }
  const startX = pathAnchorX(node.kind as 'downstream' | 'upstream', width)
  if (node.kind === 'downstream') {
    return sampleCubics([[{ x: startX, y: from.y }, { x: width * 0.315, y: from.y }, { x: width * 0.385, y: center.y }, { x: width * 0.418, y: center.y }]])
  }
  return sampleCubics([[{ x: startX, y: from.y }, { x: width * 0.685, y: from.y }, { x: width * 0.615, y: center.y }, { x: width * 0.582, y: center.y }]])
}

function requestRoutePoints(from: FlowNode, to: FlowNode | undefined, nodes: FlowNode[], width: number, height: number, gatewayY: number): RequestRoute {
  const sourceRoute = baseRoutePoints(from, nodes, width, height, gatewayY)
  const destinationRoute = to
    ? baseRoutePoints(to, nodes, width, height, gatewayY).reverse()
    : [{ x: width * 0.5, y: gatewayY }]
  const bridge = sampleLine(sourceRoute[sourceRoute.length - 1], destinationRoute[0], 40)
  const sourceLength = routeLength(sourceRoute)
  const destinationLength = to ? routeLength(destinationRoute) : sourceLength
  return {
    flowSpeed: (sourceLength + destinationLength) * baseFlowSpeed,
    points: [...sourceRoute, ...bridge.slice(1), ...destinationRoute.slice(1)],
  }
}

function scaledPosition(node: FlowNode, nodes: FlowNode[], width: number, height: number) {
  const position = nodePosition(node, nodes)
  return { x: position.x / 100 * width, y: position.y / 100 * height }
}

function pathAnchorX(kind: 'downstream' | 'upstream', width: number) {
  const viewportWidth = window.innerWidth
  const nodeWidth = Math.min(268, Math.max(210, viewportWidth * 0.16))
  return kind === 'downstream' ? width * 0.13 + nodeWidth / 2 + 4 : width * 0.87 - nodeWidth / 2 - 4
}

function sampleCubics(cubics: [Point, Point, Point, Point][]) {
  const points: Point[] = []
  cubics.forEach(([start, controlOne, controlTwo, end], cubicIndex) => {
    for (let index = cubicIndex === 0 ? 0 : 1; index <= 40; index += 1) {
      const time = index / 40
      const inverse = 1 - time
      points.push({
        x: inverse ** 3 * start.x + 3 * inverse ** 2 * time * controlOne.x + 3 * inverse * time ** 2 * controlTwo.x + time ** 3 * end.x,
        y: inverse ** 3 * start.y + 3 * inverse ** 2 * time * controlOne.y + 3 * inverse * time ** 2 * controlTwo.y + time ** 3 * end.y,
      })
    }
  })
  return points
}

function sampleLine(from: Point, to: Point, steps: number) {
  const points: Point[] = []
  for (let index = 0; index <= steps; index += 1) points.push(interpolate(from, to, index / steps))
  return points
}

function tracePoints(context: CanvasRenderingContext2D, points: Point[], start: number, end: number) {
  const lastIndex = points.length - 1
  if (lastIndex < 1 || end <= start) return
  const startPosition = Math.max(0, Math.min(lastIndex, start * lastIndex))
  const endPosition = Math.max(0, Math.min(lastIndex, end * lastIndex))
  const startIndex = Math.floor(startPosition)
  const endIndex = Math.floor(endPosition)
  const startPoint = interpolate(points[startIndex], points[Math.min(lastIndex, startIndex + 1)], startPosition - startIndex)
  const endPoint = interpolate(points[endIndex], points[Math.min(lastIndex, endIndex + 1)], endPosition - endIndex)
  context.beginPath()
  context.moveTo(startPoint.x, startPoint.y)
  for (let index = startIndex + 1; index <= endIndex; index += 1) context.lineTo(points[index].x, points[index].y)
  context.lineTo(endPoint.x, endPoint.y)
}

function pointAtRoute(points: Point[], progress: number) {
  const lastIndex = points.length - 1
  const position = Math.max(0, Math.min(lastIndex, progress * lastIndex))
  const index = Math.floor(position)
  return interpolate(points[index], points[Math.min(lastIndex, index + 1)], position - index)
}

function routeLength(points: Point[]) {
  let length = 0
  for (let index = 1; index < points.length; index += 1) {
    length += Math.hypot(points[index].x - points[index - 1].x, points[index].y - points[index - 1].y)
  }
  return length
}

function interpolate(from: Point, to: Point, progress: number) {
  return { x: from.x + (to.x - from.x) * progress, y: from.y + (to.y - from.y) * progress }
}

function distanceToRoute(point: Point, points: Point[]) {
  let distance = Number.POSITIVE_INFINITY
  for (let index = 1; index < points.length; index += 1) {
    distance = Math.min(distance, distanceToSegment(point, points[index - 1], points[index]))
  }
  return distance
}

function distanceToSegment(point: Point, start: Point, end: Point) {
  const deltaX = end.x - start.x
  const deltaY = end.y - start.y
  const lengthSquared = deltaX * deltaX + deltaY * deltaY
  if (lengthSquared === 0) return Math.hypot(point.x - start.x, point.y - start.y)
  const projection = Math.max(0, Math.min(1, ((point.x - start.x) * deltaX + (point.y - start.y) * deltaY) / lengthSquared))
  return Math.hypot(point.x - (start.x + projection * deltaX), point.y - (start.y + projection * deltaY))
}
