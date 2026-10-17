import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { mulberry32 } from '@/features/auth/components/lyra-sky-layers'
import { createFrameGovernor, type QualityTier } from '@/features/traffic-flow/lib/frame-governor'
import { useReducedMotion } from '@/features/traffic-flow/lib/use-reduced-motion'

/**
 * 中枢：天琴座星系。
 * 一颗织女星式的蓝白主星居中，两条对数螺旋臂缓慢旋转（内圈快、外圈慢），两圈轨道上各有一颗数据卫星在绕行。
 * 有请求在途时旋转加快、核心变亮；每次有新请求进入网关，核心向外荡开一圈光环。
 * 只用 Canvas 2D；减少动态效果或暂停时只画静止的一帧。
 */

type HubProps = {
  active: boolean
  /** 0…1，在途请求越多越亮 */
  load: number
  /** 当前活动请求的主色，用来给核心光晕和光环着色 */
  color: string
  /** 值变化时荡开一圈光环 */
  pulseKey: number
  paused: boolean
}

type Particle = {
  order: number
  radius: number
  angle: number
  size: number
  alpha: number
  warm: number
}

const PARTICLES = 1500
const ARMS = 2
const SPIRAL_TURNS = 2.7
const TILT = 0.52
const ROTATION = -0.42
const PULSE_MS = 1300
const FRAME_MS = 33
const ALPHA_BUCKETS = 5
const WARM_BUCKETS = 3
/** 各画质档位绘制的星点比例 */
const TIER_FRACTION: Record<QualityTier, number> = { 0: 0.3, 1: 0.6, 2: 1 }

type ParticleGroup = { style: string; items: Particle[] }

function groupParticles(particles: Particle[]): ParticleGroup[] {
  const groups: ParticleGroup[] = []
  for (let warm = 0; warm < WARM_BUCKETS; warm += 1) {
    const mix = warm / (WARM_BUCKETS - 1)
    const red = Math.round(205 + 50 * mix)
    const green = Math.round(220 + 16 * mix)
    const blue = Math.round(255 - 49 * mix)
    for (let level = 0; level < ALPHA_BUCKETS; level += 1) {
      const alpha = 0.22 + (level / (ALPHA_BUCKETS - 1)) * 0.7
      groups.push({ style: `rgba(${red}, ${green}, ${blue}, ${alpha.toFixed(2)})`, items: [] })
    }
  }
  for (const particle of particles) {
    const warm = Math.min(WARM_BUCKETS - 1, Math.round(particle.warm * (WARM_BUCKETS - 1)))
    const depth = 0.7 + 0.3 * (1 - particle.radius)
    const level = Math.min(ALPHA_BUCKETS - 1, Math.round(((particle.alpha * depth - 0.22) / 0.7) * (ALPHA_BUCKETS - 1)))
    groups[warm * ALPHA_BUCKETS + Math.max(0, level)].items.push(particle)
  }
  return groups
}

function createParticles(): Particle[] {
  const random = mulberry32(1977)
  const gaussian = () => {
    const u = Math.max(random(), 1e-6)
    return Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * random())
  }
  const particles = Array.from({ length: PARTICLES }, (_, index) => {
    // 每 4 颗里有 1 颗落在中心的核球里，让中心更密、更亮
    if (index % 4 === 0) {
      return {
        order: 0,
        radius: 0.03 + Math.abs(gaussian()) * 0.1,
        angle: random() * Math.PI * 2,
        size: 0.5 + random() ** 2 * 1.4,
        alpha: 0.5 + random() * 0.5,
        warm: 1,
      }
    }
    const arm = index % ARMS
    const t = Math.pow(random(), 0.8)
    // 臂的宽度随半径略增；约 1/6 的星点撒在臂间，让旋臂有明暗对比，而不是一整圈
    const scatter = index % 6 === 1 ? (random() - 0.5) * Math.PI : gaussian() * (0.035 + t * 0.07)
    return {
      order: 0,
      radius: 0.05 + t * 0.95,
      angle: (arm / ARMS) * Math.PI * 2 + t * SPIRAL_TURNS + scatter,
      size: 0.5 + random() ** 2.2 * 1.7,
      alpha: 0.4 + random() * 0.6,
      warm: Math.max(0, 1 - t * 2.4),
    }
  })
  // 随机的绘制顺序：低画质档只画前面一部分，剩下的仍均匀分布
  const order = Array.from({ length: particles.length }, (_, index) => index)
  for (let index = order.length - 1; index > 0; index -= 1) {
    const swap = Math.floor(random() * (index + 1))
    ;[order[index], order[swap]] = [order[swap], order[index]]
  }
  particles.forEach((particle, index) => {
    particle.order = order[index]
  })
  return particles
}

function parseColor(value: string): [number, number, number] {
  const hex = value.replace('#', '')
  const full = hex.length === 3 ? hex.split('').map((char) => char + char).join('') : hex
  const parsed = Number.parseInt(full, 16)
  if (Number.isNaN(parsed)) return [200, 215, 255]
  return [(parsed >> 16) & 255, (parsed >> 8) & 255, parsed & 255]
}

export function LyraHub({ active, load, color, pulseKey, paused }: HubProps) {
  const { t } = useTranslation('traffic-flow')
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const reducedMotion = useReducedMotion()
  const stateRef = useRef({ active, load, color, paused: paused || reducedMotion })
  const pulseRef = useRef({ key: pulseKey, at: -Infinity })

  useEffect(() => {
    stateRef.current = { active, load, color, paused: paused || reducedMotion }
  }, [active, load, color, paused, reducedMotion])

  useEffect(() => {
    if (pulseKey !== pulseRef.current.key) pulseRef.current = { key: pulseKey, at: performance.now() }
  }, [pulseKey])

  useEffect(() => {
    const canvas = canvasRef.current
    const context = canvas?.getContext('2d')
    if (!canvas || !context) return

    const particles = createParticles()
    const groups = groupParticles(particles)
    const governor = createFrameGovernor()
    let lastDraw = 0
    let size = 0
    let dpr = 1
    let frame = 0
    let last = performance.now()
    let spin = 0
    let time = 0
    let intensity = 0.55
    let tint = parseColor(stateRef.current.color)

    const resize = () => {
      dpr = Math.min(window.devicePixelRatio || 1, 1.5)
      size = canvas.clientWidth
      canvas.width = Math.round(size * dpr)
      canvas.height = Math.round(size * dpr)
      draw(performance.now())
    }

    const project = (radius: number, angle: number, scale: number) => {
      const x = Math.cos(angle) * radius * scale
      const y = Math.sin(angle) * radius * scale * TILT
      const cos = Math.cos(ROTATION)
      const sin = Math.sin(ROTATION)
      return { x: x * cos - y * sin, y: x * sin + y * cos }
    }

    const ellipse = (cx: number, cy: number, radius: number) => {
      context.save()
      context.translate(cx, cy)
      context.rotate(ROTATION)
      context.scale(1, TILT)
      context.beginPath()
      context.arc(0, 0, radius, 0, Math.PI * 2)
      context.restore()
    }

    function draw(now: number) {
      if (!context) return
      const tier = governor.tier
      const state = stateRef.current
      const target = parseColor(state.color)
      tint = tint.map((value, index) => value + (target[index] - value) * 0.06) as [number, number, number]
      const goal = state.active ? 0.8 + state.load * 0.2 : 0.55
      intensity += (goal - intensity) * 0.05

      context.setTransform(dpr, 0, 0, dpr, 0, 0)
      context.clearRect(0, 0, size, size)
      const cx = size / 2
      const cy = size / 2
      const scale = size * 0.5
      const [tr, tg, tb] = tint

      context.globalCompositeOperation = 'lighter'

      // 外层淡淡的星云晕
      const haze = context.createRadialGradient(cx, cy, 0, cx, cy, scale * 1.05)
      haze.addColorStop(0, `rgba(${tr}, ${tg}, ${tb}, ${0.1 * intensity})`)
      haze.addColorStop(0.5, `rgba(120, 140, 220, ${0.045 * intensity})`)
      haze.addColorStop(1, 'rgba(120, 140, 220, 0)')
      context.fillStyle = haze
      context.fillRect(0, 0, size, size)

      // 两圈轨道
      context.lineWidth = 1
      for (const [radius, alpha] of [[0.52, 0.13], [0.86, 0.08]] as const) {
        context.strokeStyle = `rgba(214, 224, 255, ${alpha})`
        ellipse(cx, cy, radius * scale)
        context.stroke()
      }

      // 螺旋臂：内圈转得快，外圈转得慢。
      // 星点按（颜色 × 亮度）预先分好组，每组只 fill 一次，而不是每颗星各 fill 一次。
      const cos = Math.cos(ROTATION)
      const sin = Math.sin(ROTATION)
      const limit = Math.round(PARTICLES * TIER_FRACTION[tier])
      const gain = Math.min(1.4, 0.85 + intensity * 0.6)
      for (const group of groups) {
        context.fillStyle = group.style
        context.globalAlpha = Math.min(1, gain)
        context.beginPath()
        for (const particle of group.items) {
          if (particle.order >= limit) continue
          const angle = particle.angle + spin / (0.55 + particle.radius * 2.2)
          const ex = Math.cos(angle) * particle.radius * scale
          const ey = Math.sin(angle) * particle.radius * scale * TILT
          const x = cx + ex * cos - ey * sin
          const y = cy + ex * sin + ey * cos
          context.moveTo(x + particle.size, y)
          context.arc(x, y, particle.size, 0, Math.PI * 2)
        }
        context.fill()
      }
      context.globalAlpha = 1

      // 轨道上的数据卫星
      const satellites = [
        { radius: 0.52, speed: 0.55, phase: 0.8, size: 2.2 },
        { radius: 0.86, speed: -0.32, phase: 3.6, size: 1.8 },
      ]
      for (const satellite of satellites) {
        const point = project(satellite.radius, satellite.phase + time * satellite.speed * (state.active ? 1.8 : 1), scale)
        const glow = context.createRadialGradient(cx + point.x, cy + point.y, 0, cx + point.x, cy + point.y, satellite.size * 5)
        glow.addColorStop(0, `rgba(${tr}, ${tg}, ${tb}, ${0.55 * intensity})`)
        glow.addColorStop(1, `rgba(${tr}, ${tg}, ${tb}, 0)`)
        context.fillStyle = glow
        context.beginPath()
        context.arc(cx + point.x, cy + point.y, satellite.size * 5, 0, Math.PI * 2)
        context.fill()
        context.fillStyle = 'rgba(240, 245, 255, 0.95)'
        context.beginPath()
        context.arc(cx + point.x, cy + point.y, satellite.size, 0, Math.PI * 2)
        context.fill()
      }

      // 新请求进入：一圈光环从核心荡开
      const pulse = Math.min(1, (now - pulseRef.current.at) / PULSE_MS)
      if (pulse < 1) {
        const eased = 1 - (1 - pulse) ** 3
        context.strokeStyle = `rgba(${tr}, ${tg}, ${tb}, ${(1 - pulse) * 0.55})`
        context.lineWidth = 1.4
        ellipse(cx, cy, scale * (0.08 + eased * 1.0))
        context.stroke()
      }

      // 核心：暖白的恒星 + 蓝色光晕 + 柔和的十字星芒
      const coreRadius = scale * (0.3 + 0.08 * intensity)
      const core = context.createRadialGradient(cx, cy, 0, cx, cy, coreRadius)
      core.addColorStop(0, `rgba(255, 250, 240, ${0.95 * Math.min(1, intensity + 0.2)})`)
      core.addColorStop(0.18, `rgba(235, 240, 255, ${0.6 * intensity})`)
      core.addColorStop(0.5, `rgba(${tr}, ${tg}, ${tb}, ${0.2 * intensity})`)
      core.addColorStop(1, `rgba(${tr}, ${tg}, ${tb}, 0)`)
      context.fillStyle = core
      context.beginPath()
      context.arc(cx, cy, coreRadius, 0, Math.PI * 2)
      context.fill()

      const rayLength = scale * (0.42 + 0.2 * intensity)
      for (const [dx, dy, factor] of [[1, 0, 1], [-1, 0, 1], [0, 1, 0.7], [0, -1, 0.7]] as const) {
        const ray = context.createLinearGradient(cx, cy, cx + dx * rayLength, cy + dy * rayLength)
        ray.addColorStop(0, `rgba(235, 242, 255, ${0.5 * intensity * factor})`)
        ray.addColorStop(1, 'rgba(235, 242, 255, 0)')
        context.strokeStyle = ray
        context.lineWidth = 1
        context.beginPath()
        context.moveTo(cx, cy)
        context.lineTo(cx + dx * rayLength, cy + dy * rayLength)
        context.stroke()
      }

      context.fillStyle = 'rgba(255, 252, 245, 0.98)'
      context.beginPath()
      context.arc(cx, cy, 2.6 + intensity * 1.2, 0, Math.PI * 2)
      context.fill()
      context.globalCompositeOperation = 'source-over'
    }

    const tick = (now: number) => {
      const elapsed = now - last
      const delta = Math.min(elapsed / 1000, 0.1)
      last = now
      governor.sample(elapsed)
      const state = stateRef.current
      if (!state.paused) {
        time += delta
        spin += delta * (state.active ? 0.2 : 0.07)
      }
      // 旋转很慢，30 帧足够；光环荡开时才全速画
      if (now - lastDraw >= FRAME_MS || now - pulseRef.current.at < PULSE_MS) {
        lastDraw = now
        draw(now)
      }
      frame = requestAnimationFrame(tick)
    }

    resize()
    const observer = new ResizeObserver(resize)
    observer.observe(canvas)
    frame = requestAnimationFrame(tick)
    return () => {
      cancelAnimationFrame(frame)
      observer.disconnect()
    }
  }, [])

  return (
    <div className="traffic-flow-hub" aria-hidden="true">
      <canvas ref={canvasRef} className="traffic-flow-hub-canvas" />
      <div className="traffic-flow-hub-copy">
        <strong>{t('core.title')}</strong>
        <span>{t('core.subtitle')}</span>
        <em>{active ? t('core.active') : t('core.ready')}</em>
      </div>
    </div>
  )
}
