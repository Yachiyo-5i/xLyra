import { useEffect, useRef } from 'react'
import {
  bandGeometry,
  drawMeteor,
  mulberry32,
  renderMilkyWay,
  renderStarTile,
  spawnMeteor,
  type Meteor,
} from '@/features/auth/components/lyra-sky-layers'
import { cn } from '@/lib/utils'

/**
 * 登录 / 注册页的夜空。由远及近：
 * 远景暗星 → 银河（星点、云气、暗尘带）→ 中景星 → 牛郎星 → 环状星云 M57 → 天琴座 → 流星。
 * 星点不闪烁（闪烁的亮星太抢眼），动感来自整体漂移、视差与偶尔的流星。
 * 各层水平漂移的速度不同，并随鼠标轻微视差移动，形成纵深。
 * 进入时星座连线依次描出；`vegaLit` 为 true 时织女星（Vega，α Lyr）亮起：光晕放大、连线变亮、一圈光环扩散，
 * 银河对岸的牛郎星（Altair）也随之变亮。
 * 只用 Canvas 2D；减弱动态效果时只画静止的一帧（没有漂移与流星）。
 */

/** 天琴座主要恒星：由赤经赤纬换算的平面坐标（x 向右为东侧镜像，y 向下），单位任意 */
const LYRA = {
  vega: { x: 2.22, y: -2.78, r: 2.6 },
  epsilon: { x: 0.74, y: -3.67, r: 1.3 },
  zeta: { x: 0.65, y: -1.6, r: 1.4 },
  delta: { x: -1.3, y: -0.9, r: 1.3 },
  gamma: { x: -2.19, y: 3.32, r: 1.8 },
  beta: { x: -0.42, y: 2.65, r: 1.7 },
} as const

type LyraStar = keyof typeof LYRA

/** 连线顺序：织女星所在的小三角 → 下方的平行四边形 */
const SEGMENTS: Array<[LyraStar, LyraStar]> = [
  ['vega', 'epsilon'],
  ['epsilon', 'zeta'],
  ['zeta', 'vega'],
  ['zeta', 'delta'],
  ['delta', 'gamma'],
  ['gamma', 'beta'],
  ['beta', 'zeta'],
]

const DRAW_START = 600
const SEGMENT_DURATION = 380
const LIT_DURATION = 1400

const easeOut = (value: number) => 1 - (1 - value) ** 3
const clamp01 = (value: number) => Math.min(Math.max(value, 0), 1)

type LyraSkyProps = {
  /** 织女星亮起（注册成功） */
  vegaLit?: boolean
  /** 星座位置：wide 放在右侧（桌面），top 放在上方居中（窄屏）；默认按宽度自动选择 */
  placement?: 'auto' | 'wide' | 'top'
  /** 窄屏时把星座移到这段竖直区域（相对画布的 px）里并缩放到放得下；null 时回到上方默认位置 */
  focus?: { top: number; bottom: number } | null
  /** 是否绘制天琴座星座、牛郎星与 M57；其他页面只想要星空背景时设为 false */
  constellation?: boolean
  className?: string
}

export function LyraSky({ vegaLit = false, placement = 'auto', focus = null, constellation = true, className }: LyraSkyProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const litRef = useRef({ lit: vegaLit, since: vegaLit ? -LIT_DURATION : 0 })
  const startRef = useRef(0)
  const focusRef = useRef(focus)

  useEffect(() => {
    focusRef.current = focus
  }, [focus])

  // 织女星状态变化时记录起始时间，绘制循环据此插值
  useEffect(() => {
    if (litRef.current.lit === vegaLit) return
    litRef.current = { lit: vegaLit, since: performance.now() - startRef.current }
  }, [vegaLit])

  useEffect(() => {
    const canvas = canvasRef.current
    const context = canvas?.getContext('2d')
    if (!canvas || !context) return

    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    let width = 0
    let height = 0
    let dpr = 1
    let farTile: HTMLCanvasElement | null = null
    let midTile: HTMLCanvasElement | null = null
    let milkyWay: HTMLCanvasElement | null = null
    let meteors: Meteor[] = []
    let nextMeteorAt = 2500
    const meteorRandom = mulberry32(7)
    let frame = 0
    // 鼠标视差：目标值与平滑后的当前值（-1…1）
    const pointer = { x: 0, y: 0, tx: 0, ty: 0 }
    startRef.current = performance.now()

    function resize() {
      if (!canvas) return
      const rect = canvas.getBoundingClientRect()
      dpr = Math.min(window.devicePixelRatio || 1, 2)
      width = rect.width
      height = rect.height
      canvas.width = Math.round(width * dpr)
      canvas.height = Math.round(height * dpr)
      const size = { width, height, dpr }
      const wide = placement === 'wide' || (placement === 'auto' && width >= 900)
      farTile = renderStarTile(size, { density: 1500, radius: [0.25, 0.7], alpha: [0.12, 0.42], seed: 11 })
      midTile = renderStarTile(size, { density: 6500, radius: [0.5, 1.15], alpha: [0.3, 0.75], seed: 23 })
      milkyWay = renderMilkyWay(size, bandGeometry(size, wide))
    }

    // 水平漂移的一层：画两份拼成无缝循环
    function drawDrifting(layer: HTMLCanvasElement | null, offsetX: number, offsetY: number, alpha: number) {
      if (!layer || !context) return
      const x = ((offsetX % width) + width) % width
      context.globalAlpha = alpha
      context.drawImage(layer, x, offsetY, width, height)
      context.drawImage(layer, x - width, offsetY, width, height)
    }

    function onPointerMove(event: PointerEvent) {
      if (event.pointerType !== 'mouse' || !width) return
      pointer.tx = (event.clientX / window.innerWidth) * 2 - 1
      pointer.ty = (event.clientY / window.innerHeight) * 2 - 1
    }

    // 星座高约 7.4 个单位（epsilon 到 gamma），竖直中心偏下约 0.18 个单位
    const SPAN = 7.4
    const CENTER_OFFSET = -0.18

    function targetLayout() {
      const wide = placement === 'wide' || (placement === 'auto' && width >= 900)
      if (wide) return { cx: width * 0.74, cy: height * 0.42, unit: Math.min(height * 0.062, width * 0.04) }
      const base = { cx: width * 0.5, cy: height * 0.22, unit: Math.min(height * 0.032, width * 0.07) }
      const region = focusRef.current
      if (!region) return base
      // 空隙太小（很矮的窗口）放不下：星座留在上方并淡出，不压住字标
      if (region.bottom - region.top < 72) return { ...base, cy: Math.max(region.top, 0), unit: base.unit * 0.8, hidden: true }
      // 区域比默认尺寸大时，保持默认大小、放在区域中央
      const unit = Math.min(base.unit, (region.bottom - region.top) / SPAN)
      return { cx: base.cx, cy: (region.top + region.bottom) / 2 - CENTER_OFFSET * unit, unit }
    }

    // 位置平滑地追向目标（约 700ms 走完），表单开合与窗口尺寸变化都不会跳
    let layout: { cx: number; cy: number; unit: number; opacity: number } | null = null
    let lastFrame = 0
    function constellationLayout(now: number) {
      const target = targetLayout()
      const targetOpacity = 'hidden' in target && target.hidden ? 0 : 1
      if (!layout || reduceMotion) {
        layout = { cx: target.cx, cy: target.cy, unit: target.unit, opacity: targetOpacity }
      } else {
        const k = 1 - Math.exp(-Math.max(now - lastFrame, 0) / 190)
        layout = {
          cx: layout.cx + (target.cx - layout.cx) * k,
          cy: layout.cy + (target.cy - layout.cy) * k,
          unit: layout.unit + (target.unit - layout.unit) * k,
          opacity: layout.opacity + (targetOpacity - layout.opacity) * k,
        }
      }
      lastFrame = now
      return layout
    }

    function draw(now: number) {
      if (!context) return
      const t = reduceMotion ? Number.POSITIVE_INFINITY : now - startRef.current
      context.setTransform(dpr, 0, 0, dpr, 0, 0)
      context.clearRect(0, 0, width, height)

      const fieldIn = clamp01(t / 1400)
      const seconds = reduceMotion ? 0 : t / 1000
      pointer.x += (pointer.tx - pointer.x) * 0.04
      pointer.y += (pointer.ty - pointer.y) * 0.04
      const parallax = (depth: number) => ({ x: -pointer.x * depth, y: -pointer.y * depth })

      // 远景暗星 → 银河 → 中景星：越近漂移越快、视差越大
      const far = parallax(4)
      drawDrifting(farTile, seconds * 1.2 + far.x, far.y, fieldIn)
      const band = parallax(6)
      context.globalAlpha = fieldIn
      if (milkyWay) context.drawImage(milkyWay, band.x, band.y, width, height)
      const mid = parallax(10)
      drawDrifting(midTile, seconds * 2.6 + mid.x, mid.y, fieldIn)

      const near = parallax(14)

      // 流星：每 6–12 秒一颗
      if (!reduceMotion) {
        if (t > nextMeteorAt) {
          meteors.push(spawnMeteor({ width, height, dpr }, t, meteorRandom))
          nextMeteorAt = t + 6000 + meteorRandom() * 6000
        }
        context.globalAlpha = 1
        meteors = meteors.filter((meteor) => drawMeteor(context, meteor, t))
      }

      if (!constellation) {
        context.globalAlpha = 1
        if (!reduceMotion) frame = requestAnimationFrame(draw)
        return
      }

      const layout = constellationLayout(now)
      const shift = parallax(8)
      const cx = layout.cx + shift.x
      const cy = layout.cy + shift.y
      const unit = layout.unit
      const point = (name: LyraStar) => ({ x: cx + LYRA[name].x * unit, y: cy + LYRA[name].y * unit })
      const litState = litRef.current
      const litProgress = litState.lit ? easeOut(clamp01((t - litState.since) / LIT_DURATION)) : 0
      const starsIn = clamp01((t - 400) / 600) * layout.opacity

      // 牛郎星（Altair）：在银河对岸，织女星亮起时也跟着变亮
      {
        const wide = placement === 'wide' || (placement === 'auto' && width >= 900)
        const ax = (wide ? width * 0.4 : width * 0.82) + near.x
        const ay = (wide ? height * 0.88 : height * 0.9) + near.y
        const radius = 1.9 + 0.6 * litProgress
        const glow = context.createRadialGradient(ax, ay, 0, ax, ay, radius * (6 + 6 * litProgress))
        glow.addColorStop(0, `rgba(255, 246, 232, ${0.42 + 0.3 * litProgress})`)
        glow.addColorStop(1, 'rgba(255, 246, 232, 0)')
        context.globalAlpha = fieldIn
        context.fillStyle = glow
        context.beginPath()
        context.arc(ax, ay, radius * (6 + 6 * litProgress), 0, Math.PI * 2)
        context.fill()
        context.fillStyle = '#fff8ee'
        context.beginPath()
        context.arc(ax, ay, radius, 0, Math.PI * 2)
        context.fill()
      }

      // 环状星云 M57：位于 β 与 γ 之间，一个蓝心、偏红外缘的小光环
      {
        const b = point('beta')
        const g = point('gamma')
        const mx = (b.x + g.x) / 2
        const my = (b.y + g.y) / 2
        const ring = Math.max(unit * 0.15, 3)
        const nebula = context.createRadialGradient(mx, my, 0, mx, my, ring)
        nebula.addColorStop(0, 'rgba(120, 200, 255, 0.03)')
        nebula.addColorStop(0.55, 'rgba(140, 215, 235, 0.2)')
        nebula.addColorStop(0.78, 'rgba(255, 160, 180, 0.16)')
        nebula.addColorStop(1, 'rgba(255, 160, 180, 0)')
        context.globalAlpha = starsIn
        context.fillStyle = nebula
        context.save()
        context.translate(mx, my)
        context.rotate(-0.5)
        context.scale(1, 0.8)
        context.beginPath()
        context.arc(0, 0, ring, 0, Math.PI * 2)
        context.fill()
        context.restore()
      }

      // 连线：依次描出。模拟真实的光：离星点留出间隙、两端淡出，三层叠加（外层宽而虚、中层柔光、内芯细亮）
      context.lineCap = 'round'
      SEGMENTS.forEach(([from, to], index) => {
        const progress = easeOut(clamp01((t - DRAW_START - index * SEGMENT_DURATION) / SEGMENT_DURATION))
        if (progress <= 0) return
        const a = point(from)
        const b = point(to)
        const length = Math.hypot(b.x - a.x, b.y - a.y)
        if (length < 1) return
        const ux = (b.x - a.x) / length
        const uy = (b.y - a.y) / length
        const gap = Math.min(10, length * 0.18)
        const startX = a.x + ux * gap
        const startY = a.y + uy * gap
        const endLength = gap + (length - gap * 2) * progress
        const endX = a.x + ux * endLength
        const endY = a.y + uy * endLength
        const strength = 1 + 0.9 * litProgress

        // 沿线渐变：两端透明、中段最亮，像光沿着线散开
        const gradient = (alpha: number) => {
          const g = context.createLinearGradient(a.x + ux * gap, a.y + uy * gap, b.x - ux * gap, b.y - uy * gap)
          g.addColorStop(0, 'rgba(200, 215, 255, 0)')
          g.addColorStop(0.18, `rgba(200, 215, 255, ${alpha})`)
          g.addColorStop(0.82, `rgba(200, 215, 255, ${alpha})`)
          g.addColorStop(1, 'rgba(200, 215, 255, 0)')
          return g
        }

        const passes: Array<{ width: number; alpha: number; blur: number }> = [
          { width: 9, alpha: 0.035, blur: 14 },
          { width: 3, alpha: 0.09, blur: 6 },
          { width: 0.9, alpha: 0.42, blur: 2 },
        ]
        for (const pass of passes) {
          context.globalAlpha = 1
          context.lineWidth = pass.width
          context.strokeStyle = gradient(Math.min(pass.alpha * strength, 1) * layout.opacity)
          context.shadowColor = 'rgba(170, 195, 255, 0.6)'
          context.shadowBlur = pass.blur
          context.beginPath()
          context.moveTo(startX, startY)
          context.lineTo(endX, endY)
          context.stroke()
        }
        context.shadowBlur = 0
      })

      // 星座的星：在连线开始前淡入
      for (const name of Object.keys(LYRA) as LyraStar[]) {
        const { x, y } = point(name)
        const isVega = name === 'vega'
        if (isVega) {
          // 织女星光晕：平时很弱，亮起后放大
          const glowRadius = 14 + 70 * litProgress
          const glow = context.createRadialGradient(x, y, 0, x, y, glowRadius)
          glow.addColorStop(0, `rgba(190, 210, 255, ${0.35 + 0.4 * litProgress})`)
          glow.addColorStop(0.35, `rgba(150, 180, 255, ${0.1 + 0.18 * litProgress})`)
          glow.addColorStop(1, 'rgba(150, 180, 255, 0)')
          context.globalAlpha = starsIn
          context.fillStyle = glow
          context.beginPath()
          context.arc(x, y, glowRadius, 0, Math.PI * 2)
          context.fill()

          // 亮起瞬间的一圈光环
          if (litState.lit && !reduceMotion) {
            const ring = clamp01((t - litState.since) / 1200)
            if (ring < 1) {
              context.globalAlpha = (1 - ring) * 0.5
              context.strokeStyle = '#cfe0ff'
              context.lineWidth = 1.2
              context.beginPath()
              context.arc(x, y, 8 + easeOut(ring) * 150, 0, Math.PI * 2)
              context.stroke()
              context.lineWidth = 1
            }
          }
        }
        // 星点：柔光晕 + 亮核，避免实心圆点的「贴纸感」
        const core = LYRA[name].r * (isVega ? 1 + 0.35 * litProgress : 1)
        const halo = context.createRadialGradient(x, y, 0, x, y, core * 4)
        halo.addColorStop(0, 'rgba(225, 233, 255, 0.55)')
        halo.addColorStop(1, 'rgba(225, 233, 255, 0)')
        context.globalAlpha = starsIn
        context.fillStyle = halo
        context.beginPath()
        context.arc(x, y, core * 4, 0, Math.PI * 2)
        context.fill()
        context.fillStyle = isVega ? '#eef3ff' : '#ffffff'
        context.shadowColor = 'rgba(210, 225, 255, 0.9)'
        context.shadowBlur = core * 3
        context.beginPath()
        context.arc(x, y, core, 0, Math.PI * 2)
        context.fill()
        context.shadowBlur = 0
      }
      context.globalAlpha = 1

      if (!reduceMotion) frame = requestAnimationFrame(draw)
    }

    resize()
    const observer = new ResizeObserver(() => {
      resize()
      if (reduceMotion) draw(performance.now())
    })
    observer.observe(canvas)
    const redraw = () => draw(performance.now())
    canvas.addEventListener('lyra-redraw', redraw)
    if (!reduceMotion) window.addEventListener('pointermove', onPointerMove)
    frame = requestAnimationFrame(draw)

    return () => {
      observer.disconnect()
      canvas.removeEventListener('lyra-redraw', redraw)
      window.removeEventListener('pointermove', onPointerMove)
      cancelAnimationFrame(frame)
    }
  }, [placement, constellation])

  // 减弱动态效果时，织女星或星座位置变化后需要补画一帧（循环已停止）
  useEffect(() => {
    if (!window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    canvasRef.current?.dispatchEvent(new Event('lyra-redraw'))
  }, [vegaLit, focus])

  return <canvas ref={canvasRef} aria-hidden="true" className={cn('pointer-events-none absolute inset-0 size-full', className)} />
}
