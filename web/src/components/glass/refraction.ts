/**
 * Liquid Glass 折射。
 *
 * 用 SVG `feDisplacementMap` 作为 `backdrop-filter: url(#id)`，让玻璃边缘像凸透镜一样
 * 弯折其下方的真实页面内容，并对 R / G / B 三个通道使用略有差异的位移量产生轻微色散。
 *
 * 只在 Chromium 内核中启用：Safari / Firefox 能解析 `backdrop-filter: url()` 却渲染为空，
 * `@supports` 会误判。其他浏览器保留 `.lg-*` 类的纯 CSS 毛玻璃效果。
 */

export type RefractionProfile = {
  /** 边缘折射带宽度（px）：只有这一圈弯折背景，中间区域保持平整 */
  bezel: number
  /** 最大位移量（px），越大折射越明显 */
  scale: number
  /** 色散：G、B 通道相对 R 通道的位移衰减比例 */
  dispersion: number
  /** 折射前先做的模糊（px） */
  blur: number
  saturate: number
  brightness: number
}

export const refractionProfiles = {
  regular: { bezel: 26, scale: 64, dispersion: 0.06, blur: 10, saturate: 170, brightness: 1.04 },
  clear: { bezel: 22, scale: 72, dispersion: 0.08, blur: 0.5, saturate: 190, brightness: 1.06 },
} satisfies Record<string, RefractionProfile>

export type RefractionVariant = keyof typeof refractionProfiles

export type EngineSignals = {
  userAgent: string
  /** `window.chrome` 是否存在：Chromium 内核（含 Electron）独有 */
  hasChromeObject: boolean
  /** `navigator.userAgentData` 是否存在：只有 Chromium 实现（仅安全上下文中可用） */
  hasUserAgentData: boolean
}

/**
 * 判断是否为 Chromium 内核。优先看内核独有的对象，而不是 UA 字符串：
 * Chrome 开发者工具切到 iPhone 等移动端视图时，UA 会被改成 iOS Safari 的字符串，
 * 只看 UA 会把真实的 Chromium 误判为 WebKit。
 */
export function isChromiumEngine({ userAgent, hasChromeObject, hasUserAgentData }: EngineSignals) {
  // iOS 上的 Chrome / Edge / Firefox 只是 WebKit 外壳
  if (/CriOS|FxiOS|EdgiOS/.test(userAgent)) return false
  if (hasChromeObject || hasUserAgentData) return true
  return /Chrome\/|Chromium\//.test(userAgent) && !/Firefox\//.test(userAgent)
}

let cachedSupport: boolean | null = null

export function supportsGlassRefraction() {
  if (cachedSupport != null) return cachedSupport
  if (typeof navigator === 'undefined' || typeof window === 'undefined') return false
  cachedSupport = isChromiumEngine({
    userAgent: navigator.userAgent,
    hasChromeObject: typeof (window as { chrome?: unknown }).chrome === 'object',
    hasUserAgentData: 'userAgentData' in navigator,
  })
  return cachedSupport
}

/** 应用启动时调用：在 <html> 上标记是否启用折射，供 CSS 调整玻璃底色 */
export function installGlassRefraction() {
  if (supportsGlassRefraction()) document.documentElement.dataset.glassRefraction = 'true'
}

const SVG_NS = 'http://www.w3.org/2000/svg'
let filterHost: SVGSVGElement | null = null

function getFilterHost() {
  if (filterHost?.isConnected) return filterHost
  filterHost = document.createElementNS(SVG_NS, 'svg')
  filterHost.setAttribute('aria-hidden', 'true')
  filterHost.setAttribute('width', '0')
  filterHost.setAttribute('height', '0')
  filterHost.style.position = 'absolute'
  filterHost.style.pointerEvents = 'none'
  document.body.appendChild(filterHost)
  return filterHost
}

/**
 * 生成圆角矩形的位移贴图：R、G 通道编码「指向内部的法线 × 折射强度」，128 为不位移。
 * 折射强度随离边缘的距离按凸透镜曲面衰减，只作用于 bezel 宽度内。
 */
export function createDisplacementMap(width: number, height: number, radius: number, bezel: number) {
  const canvas = document.createElement('canvas')
  canvas.width = width
  canvas.height = height
  const context = canvas.getContext('2d')
  if (!context) return null

  const image = context.createImageData(width, height)
  const data = image.data
  const halfW = width / 2
  const halfH = height / 2
  const r = Math.min(radius, halfW, halfH)
  const innerW = halfW - r
  const innerH = halfH - r

  for (let y = 0; y < height; y += 1) {
    const py = y + 0.5 - halfH
    const ay = Math.abs(py)
    const qy = ay - innerH
    for (let x = 0; x < width; x += 1) {
      const px = x + 0.5 - halfW
      const ax = Math.abs(px)
      const qx = ax - innerW

      // 圆角矩形有符号距离场：distance < 0 表示在内部
      let distance: number
      let nx: number
      let ny: number
      if (qx > 0 && qy > 0) {
        const length = Math.hypot(qx, qy)
        distance = length - r
        nx = qx / length
        ny = qy / length
      } else if (qx > qy) {
        distance = qx - r
        nx = 1
        ny = 0
      } else {
        distance = qy - r
        nx = 0
        ny = 1
      }

      const inset = -distance
      const index = (y * width + x) * 4
      let dx = 0
      let dy = 0
      if (inset >= 0 && inset < bezel) {
        // 凸透镜曲面：越靠近边缘斜率越大，弯折越强
        const t = inset / bezel
        const strength = (1 - t) ** 2.4
        // 采样点向内偏移（内法线 = -外法线），四个象限按坐标符号还原方向
        dx = -nx * Math.sign(px || 1) * strength
        dy = -ny * Math.sign(py || 1) * strength
      }
      data[index] = Math.round(128 + dx * 127)
      data[index + 1] = Math.round(128 + dy * 127)
      data[index + 2] = 128
      data[index + 3] = 255
    }
  }

  context.putImageData(image, 0, 0)
  return canvas.toDataURL()
}

function setAttributes(element: Element, attributes: Record<string, string | number>) {
  for (const [key, value] of Object.entries(attributes)) element.setAttribute(key, String(value))
}

/** 创建或更新一个折射滤镜，返回可放进 backdrop-filter 的完整取值 */
export function upsertRefractionFilter(
  id: string,
  size: { width: number; height: number; radius: number },
  profile: RefractionProfile,
) {
  const width = Math.max(1, Math.round(size.width))
  const height = Math.max(1, Math.round(size.height))
  const bezel = Math.min(profile.bezel, width / 2, height / 2)
  const map = createDisplacementMap(width, height, size.radius, bezel)
  if (!map) return null

  const host = getFilterHost()
  let filter = host.querySelector<SVGFilterElement>(`#${CSS.escape(id)}`)
  if (!filter) {
    filter = document.createElementNS(SVG_NS, 'filter')
    filter.id = id
    host.appendChild(filter)
  }
  setAttributes(filter, {
    x: 0,
    y: 0,
    width,
    height,
    filterUnits: 'userSpaceOnUse',
    'color-interpolation-filters': 'sRGB',
  })

  const channel = (name: 'r' | 'g' | 'b', scale: number, matrix: string) =>
    `<feDisplacementMap in="SourceGraphic" in2="map" scale="${scale.toFixed(2)}" xChannelSelector="R" yChannelSelector="G" result="d${name}"/>` +
    `<feColorMatrix in="d${name}" type="matrix" values="${matrix}" result="${name}"/>`

  const { scale, dispersion } = profile
  filter.innerHTML =
    `<feImage href="${map}" x="0" y="0" width="${width}" height="${height}" preserveAspectRatio="none" result="map"/>` +
    channel('r', scale, '1 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 0 1 0') +
    channel('g', scale * (1 - dispersion), '0 0 0 0 0  0 1 0 0 0  0 0 0 0 0  0 0 0 1 0') +
    channel('b', scale * (1 - dispersion * 2), '0 0 0 0 0  0 0 0 0 0  0 0 1 0 0  0 0 0 1 0') +
    '<feBlend in="r" in2="g" mode="screen" result="rg"/>' +
    '<feBlend in="rg" in2="b" mode="screen"/>'

  return `blur(${profile.blur}px) url(#${id}) saturate(${profile.saturate}%) brightness(${profile.brightness})`
}

export function removeRefractionFilter(id: string) {
  filterHost?.querySelector(`#${CSS.escape(id)}`)?.remove()
}
