import { useEffect, useRef } from 'react'
import { mulberry32 } from '@/features/auth/components/lyra-sky-layers'

/**
 * 全局背景（L0）：一层很空的星空。
 *
 * 设计取舍：星点不抢戏。
 * - 全是 1 个物理像素大小的细点，亮度很低且偏暗的占绝大多数；没有光晕、没有“亮星”，
 *   背景被提亮靠的是这些细点的整体密度，而不是某几颗星的光；
 * - 用抖动网格撒点，避免随机撒点常见的“一簇一簇”和空洞，看起来是均匀的细碎星尘；
 * - 密度由一张很缓的低频场调制，并且靠近窗口边缘更密、中央更疏：内容大多压在中央，文字附近尽量干净；
 * - 只在挂载和窗口尺寸变化时画一次，之后没有任何逐帧开销。
 */

const TINTS = ['226,232,255', '226,232,255', '236,240,255', '208,222,255', '255,244,228']
const CELL = 30

const smooth = (value: number) => value * value * (3 - 2 * value)

export function AmbientBackground() {
  const canvasRef = useRef<HTMLCanvasElement>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    const context = canvas?.getContext('2d')
    if (!canvas || !context) return

    const draw = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      const width = canvas.clientWidth
      const height = canvas.clientHeight
      canvas.width = Math.max(1, Math.round(width * dpr))
      canvas.height = Math.max(1, Math.round(height * dpr))
      context.setTransform(dpr, 0, 0, dpr, 0, 0)
      context.clearRect(0, 0, width, height)

      const random = mulberry32(20261018)
      // 低频密度场：三组缓慢的正弦叠加
      const waves = Array.from({ length: 3 }, () => ({
        fx: (0.6 + random() * 1.4) / Math.max(width, 1) * Math.PI * 2,
        fy: (0.6 + random() * 1.4) / Math.max(height, 1) * Math.PI * 2,
        px: random() * Math.PI * 2,
        py: random() * Math.PI * 2,
      }))
      const onePixel = 1 / dpr
      for (let cy = 0; cy < height; cy += CELL) {
        for (let cx = 0; cx < width; cx += CELL) {
          const x = cx + random() * CELL
          const y = cy + random() * CELL
          const roll = random()
          const brightness = random()
          const tint = TINTS[Math.floor(random() * TINTS.length)]
          const size = random()

          let field = 0
          for (const wave of waves) field += 0.5 + 0.5 * Math.sin(x * wave.fx + wave.px) * Math.sin(y * wave.fy + wave.py)
          field /= waves.length
          // 离中心越远越密（0 在中心，1 在边角）
          const dx = (x / width - 0.5) * 2
          const dy = (y / height - 0.5) * 2
          const edge = smooth(Math.min(1, Math.hypot(dx, dy) / 1.25))
          const probability = 0.34 * (0.35 + 0.9 * field) * (0.4 + 0.9 * edge)
          if (roll > probability) continue

          // 亮度偏暗：立方分布，绝大多数很淡，极少数稍亮，仍然很克制
          const alpha = 0.07 + brightness ** 2.4 * 0.36
          const radius = size > 0.96 ? onePixel * 1.7 : Math.max(onePixel, 0.55)
          context.fillStyle = `rgba(${tint}, ${alpha.toFixed(3)})`
          context.beginPath()
          context.arc(x, y, radius, 0, Math.PI * 2)
          context.fill()
        }
      }
    }

    draw()
    let timer = 0
    const onResize = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(draw, 120)
    }
    window.addEventListener('resize', onResize)
    return () => {
      window.clearTimeout(timer)
      window.removeEventListener('resize', onResize)
    }
  }, [])

  return (
    <div className="ambient-background" aria-hidden="true">
      <canvas ref={canvasRef} className="size-full" />
    </div>
  )
}
