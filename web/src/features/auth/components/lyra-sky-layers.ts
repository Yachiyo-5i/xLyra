/**
 * 登录页夜空的背景图层（lyra-sky.tsx 使用）。大量静态的星点在窗口尺寸变化时预先画进离屏画布，
 * 每帧只做平移与少量动态元素（亮星闪烁、流星），保证低端设备也流畅。
 */

export function mulberry32(seed: number) {
  let value = seed
  return () => {
    value |= 0
    value = (value + 0x6d2b79f5) | 0
    let t = Math.imul(value ^ (value >>> 15), 1 | value)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** 近似正态分布（Box–Muller） */
function gaussian(random: () => number) {
  const u = Math.max(random(), 1e-6)
  return Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * random())
}

/** 恒星的颜色：大多是蓝白，少量偏暖（K / M 型星） */
const STAR_TINTS = ['220, 230, 255', '220, 230, 255', '235, 240, 255', '255, 244, 228', '255, 214, 180', '200, 215, 255']

export type Size = { width: number; height: number; dpr: number }

function createLayerCanvas({ width, height, dpr }: Size) {
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(width * dpr))
  canvas.height = Math.max(1, Math.round(height * dpr))
  const context = canvas.getContext('2d')!
  context.scale(dpr, dpr)
  return { canvas, context }
}

/** 一层均匀分布的静态星点（远景 / 中景），左右可无缝拼接以便水平漂移 */
export function renderStarTile(size: Size, options: { density: number; radius: [number, number]; alpha: [number, number]; seed: number }) {
  const { canvas, context } = createLayerCanvas(size)
  const random = mulberry32(options.seed)
  const count = Math.round((size.width * size.height) / options.density)
  for (let index = 0; index < count; index += 1) {
    const x = random() * size.width
    const y = random() * size.height
    const r = options.radius[0] + random() ** 2.2 * (options.radius[1] - options.radius[0])
    const alpha = options.alpha[0] + random() * (options.alpha[1] - options.alpha[0])
    const tint = STAR_TINTS[Math.floor(random() * STAR_TINTS.length)]
    context.fillStyle = `rgba(${tint}, ${alpha})`
    // 靠近左右边缘的星在另一侧再画一份，拼接处不断开
    const copies = [x]
    if (x < r * 2) copies.push(x + size.width)
    if (x > size.width - r * 2) copies.push(x - size.width)
    for (const copyX of copies) {
      context.beginPath()
      context.arc(copyX, y, r, 0, Math.PI * 2)
      context.fill()
    }
  }
  return canvas
}

export type BandGeometry = { x0: number; y0: number; x1: number; y1: number; width: number }

/** 银河的走向：宽屏从左下斜穿画面中部（星座左侧），窄屏从左下到右上斜穿 */
export function bandGeometry({ width, height }: Size, wide: boolean): BandGeometry {
  return wide
    ? { x0: width * 0.3, y0: height * 1.15, x1: width * 0.66, y1: -height * 0.15, width: Math.max(width * 0.085, 70) }
    : { x0: -width * 0.1, y0: height * 0.78, x1: width * 1.1, y1: height * 0.28, width: Math.max(width * 0.2, 60) }
}

/**
 * 银河：沿一条斜线密集分布的暗淡星点 + 几团冷色与暖色的云气 + 一条偏离中线的暗尘带。
 * 云气很淡（单团 2%–4%），叠出来的是一条若有若无的光带，而不是明显的渐变光斑。
 */
export function renderMilkyWay(size: Size, band: BandGeometry) {
  const { canvas, context } = createLayerCanvas(size)
  const random = mulberry32(1977)
  const dx = band.x1 - band.x0
  const dy = band.y1 - band.y0
  const length = Math.hypot(dx, dy)
  const ux = dx / length
  const uy = dy / length
  const nx = -uy
  const ny = ux
  const at = (t: number, offset: number) => ({ x: band.x0 + dx * t + nx * offset, y: band.y0 + dy * t + ny * offset })
  // 沿银河方向的疏密起伏：几组正弦叠加，形成一团一团的星云感
  const clump = (t: number) => 0.55 + 0.25 * Math.sin(t * 17 + 1.3) + 0.2 * Math.sin(t * 41 + 0.4)

  context.globalCompositeOperation = 'lighter'
  const clouds: Array<[string, number]> = [
    ['150, 170, 255', 0.04],
    ['175, 150, 255', 0.025],
    ['255, 205, 170', 0.02],
  ]
  for (let index = 0; index < 46; index += 1) {
    const t = random()
    const [color, alpha] = clouds[index % clouds.length]
    const { x, y } = at(t, gaussian(random) * band.width * 0.35)
    const radius = band.width * (0.6 + random() * 1.1)
    const gradient = context.createRadialGradient(x, y, 0, x, y, radius)
    gradient.addColorStop(0, `rgba(${color}, ${alpha * clump(t)})`)
    gradient.addColorStop(1, `rgba(${color}, 0)`)
    context.fillStyle = gradient
    context.fillRect(x - radius, y - radius, radius * 2, radius * 2)
  }

  // 密集的暗星
  const starCount = Math.round(length * band.width * 0.06)
  for (let index = 0; index < starCount; index += 1) {
    const t = random()
    if (random() > clump(t)) continue
    const { x, y } = at(t, gaussian(random) * band.width * 0.5)
    const r = 0.25 + random() ** 3 * 0.75
    const tint = STAR_TINTS[Math.floor(random() * STAR_TINTS.length)]
    context.fillStyle = `rgba(${tint}, ${0.1 + random() * 0.4})`
    context.beginPath()
    context.arc(x, y, r, 0, Math.PI * 2)
    context.fill()
  }

  // 暗尘带：沿中线一侧挖掉一部分亮度，银河就有了层次
  context.globalCompositeOperation = 'destination-out'
  for (let index = 0; index < 28; index += 1) {
    const t = random()
    const { x, y } = at(t, band.width * (0.12 + gaussian(random) * 0.08))
    const radius = band.width * (0.18 + random() * 0.3)
    const gradient = context.createRadialGradient(x, y, 0, x, y, radius)
    gradient.addColorStop(0, `rgba(0, 0, 0, ${0.35 + random() * 0.25})`)
    gradient.addColorStop(1, 'rgba(0, 0, 0, 0)')
    context.fillStyle = gradient
    context.fillRect(x - radius, y - radius, radius * 2, radius * 2)
  }
  context.globalCompositeOperation = 'source-over'
  return canvas
}

export type Meteor = { x: number; y: number; vx: number; vy: number; length: number; born: number; life: number }

/** 流星：从画面上部斜向左下划过，约 0.9 秒 */
export function spawnMeteor({ width, height }: Size, now: number, random: () => number): Meteor {
  const angle = (200 + random() * 25) * (Math.PI / 180)
  const speed = 0.9 + random() * 0.6
  return {
    x: width * (0.25 + random() * 0.75),
    y: height * random() * 0.35,
    vx: Math.cos(angle) * speed,
    vy: -Math.sin(angle) * speed,
    length: 110 + random() * 120,
    born: now,
    life: 800 + random() * 400,
  }
}

export function drawMeteor(context: CanvasRenderingContext2D, meteor: Meteor, now: number) {
  const age = (now - meteor.born) / meteor.life
  if (age >= 1) return false
  const elapsed = now - meteor.born
  const headX = meteor.x + meteor.vx * elapsed
  const headY = meteor.y + meteor.vy * elapsed
  const speed = Math.hypot(meteor.vx, meteor.vy)
  const tail = meteor.length * Math.min(1, age * 3)
  const tailX = headX - (meteor.vx / speed) * tail
  const tailY = headY - (meteor.vy / speed) * tail
  // 先亮后暗：前 20% 渐亮，之后衰减
  const alpha = age < 0.2 ? age / 0.2 : 1 - (age - 0.2) / 0.8
  const gradient = context.createLinearGradient(headX, headY, tailX, tailY)
  gradient.addColorStop(0, `rgba(235, 240, 255, ${0.85 * alpha})`)
  gradient.addColorStop(1, 'rgba(235, 240, 255, 0)')
  context.strokeStyle = gradient
  context.lineWidth = 1.2
  context.lineCap = 'round'
  context.beginPath()
  context.moveTo(headX, headY)
  context.lineTo(tailX, tailY)
  context.stroke()
  return true
}
