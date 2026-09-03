import { useCallback, useEffect, useMemo, useRef } from 'react'
import { AgentLiquidGlassRenderer } from './agent-liquid-renderer'
import { resolveLiquidGlassSettings, type LiquidGlassSettings, type LiquidGlassVariant } from './core/types'

export type AgentLiquidGlassSettings = Partial<LiquidGlassSettings>

type AgentLiquidGlassPanelProps = {
  backgroundImage: string
  className?: string
  contentClassName?: string
  settings?: AgentLiquidGlassSettings
  variant?: LiquidGlassVariant
  sampleBackground?: boolean | number
  flat?: boolean
  children: React.ReactNode
}

export function AgentLiquidGlassPanel({ backgroundImage, className = '', contentClassName = '', settings, variant = 'frosted', sampleBackground = true, flat = false, children }: AgentLiquidGlassPanelProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const rendererRef = useRef<AgentLiquidGlassRenderer | null>(null)
  const mergedSettings = useMemo(() => resolveLiquidGlassSettings(variant, settings), [settings, variant])
  const settingsRef = useRef(mergedSettings)
  useEffect(() => { settingsRef.current = mergedSettings }, [mergedSettings])

  const apply = useCallback(() => {
    const root = rootRef.current
    const renderer = rendererRef.current
    if (!root || !renderer) return
    const width = root.clientWidth
    const height = root.clientHeight
    if (!width || !height) return
    const current = settingsRef.current
    renderer.setBackgroundSampling(sampleBackground)
    renderer.resize(width, height)
    renderer.setSettings({ ...current, radius: Math.min(current.radius, height / 2), lensWidth: width, lensHeight: height })
    renderer.setGeometry(width / 2, height / 2, 0, false, 1, 1, 0)
  }, [sampleBackground])

  useEffect(() => {
    if (flat) return
    const root = rootRef.current
    const canvas = canvasRef.current
    if (!root || !canvas) return
    let renderer: AgentLiquidGlassRenderer
    try { renderer = new AgentLiquidGlassRenderer(canvas, backgroundImage, settingsRef.current) } catch { return }
    rendererRef.current = renderer
    const observer = new ResizeObserver(apply)
    observer.observe(root)
    apply()
    return () => { observer.disconnect(); renderer.dispose(); rendererRef.current = null }
  }, [apply, backgroundImage, flat])

  useEffect(() => { if (!flat) apply() }, [apply, mergedSettings, flat])

  const profile = backgroundImage.includes('plain') ? 'dark' : 'bright'

  return (
    <div ref={rootRef} className={`agent-liquid-surface agent-liquid-surface--${profile} ${flat ? 'agent-liquid-surface--flat' : ''} ${className}`} style={{ borderRadius: mergedSettings.radius }}>
      <canvas ref={canvasRef} className="agent-liquid-surface__canvas" aria-hidden="true" />
      <div className={`agent-liquid-surface__content ${contentClassName}`}>{children}</div>
    </div>
  )
}
