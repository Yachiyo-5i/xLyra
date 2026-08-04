import { useEffect, useMemo, useRef, useState } from 'react'
import { cn } from '@/lib/utils'

export type DivergingBarDatum = {
  label: string
  positive: number
  negative: number
}

type DivergingBarChartProps = {
  data: DivergingBarDatum[]
  positiveLabel?: string
  negativeLabel?: string
  positiveColor?: string
  negativeColor?: string
  barSize?: number
  barCategoryGap?: number | string
  barGap?: number | string
  fillWidth?: boolean
  targetBarWidth?: number
  height?: number | string
  className?: string
  valueFormatter?: (value: number) => string
}

type DivergingBarChartPoint = {
  label: string
  positiveValue: number
  negativeValue: number
}

type ActiveTooltip = {
  x: number
  y: number
  point: DivergingBarChartPoint
}

function defaultValueFormatter(value: number) {
  if (Math.abs(value) >= 1000) return value.toLocaleString()
  if (Number.isInteger(value)) return String(value)
  return value.toFixed(2)
}

export function DivergingBarChart({
  data,
  positiveLabel = 'Positive',
  negativeLabel = 'Negative',
  positiveColor = 'hsl(324 86% 58%)',
  negativeColor = 'hsl(211 96% 52%)',
  barSize = 4,
  barCategoryGap = 0,
  targetBarWidth,
  height = 96,
  className,
  valueFormatter = defaultValueFormatter,
}: DivergingBarChartProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const [containerWidth, setContainerWidth] = useState(0)
  const [activeTooltip, setActiveTooltip] = useState<ActiveTooltip | null>(null)

  useEffect(() => {
    const node = rootRef.current
    if (!node) return

    const updateWidth = () => {
      setContainerWidth(node.clientWidth)
    }
    updateWidth()

    const observer = new ResizeObserver(updateWidth)
    observer.observe(node)

    return () => {
      observer.disconnect()
    }
  }, [])

  const fixedBarWidth = Math.max(1, Math.round(targetBarWidth ?? barSize))
  const barGap = typeof barCategoryGap === 'number' ? Math.max(0, barCategoryGap) : 0
  const barStep = fixedBarWidth + barGap

  const chartData = useMemo<DivergingBarChartPoint[]>(
    () =>
      data.map((item) => ({
        label: item.label,
        positiveValue: Math.max(0, item.positive),
        negativeValue: Math.max(0, item.negative),
      })),
    [data],
  )

  const visibleChartData = useMemo(() => {
    if (containerWidth <= 0) return chartData

    const visibleCount = Math.ceil(containerWidth / barStep) + 1
    return chartData.slice(-visibleCount)
  }, [barStep, chartData, containerWidth])

  const domainMax = useMemo(() => {
    const maxValue = visibleChartData.reduce(
      (max, item) => Math.max(max, item.positiveValue, item.negativeValue),
      0,
    )
    return maxValue > 0 ? maxValue : 1
  }, [visibleChartData])

  const chartWidth = visibleChartData.length * barStep
  const originX = containerWidth - chartWidth
  const baseline = 50
  const verticalPadding = 4
  const halfHeight = baseline - verticalPadding

  const updateTooltip = (event: React.MouseEvent<SVGRectElement>, point: DivergingBarChartPoint) => {
    const rect = rootRef.current?.getBoundingClientRect()
    if (!rect) return

    setActiveTooltip({
      x: event.clientX - rect.left,
      y: event.clientY - rect.top,
      point,
    })
  }

  return (
    <div ref={rootRef} className={cn('relative rounded-lg bg-[hsl(var(--surface-panel))] p-3', className)}>
      <div style={{ height }}>
        <svg
          className="block h-full w-full overflow-hidden"
          role="img"
          viewBox={`0 0 ${Math.max(containerWidth, 1)} 100`}
          preserveAspectRatio="none"
          onMouseLeave={() => setActiveTooltip(null)}
        >
          <line
            x1={0}
            x2={Math.max(containerWidth, 1)}
            y1={baseline}
            y2={baseline}
            stroke="hsl(var(--glass-border-strong))"
            strokeOpacity={0.86}
            vectorEffect="non-scaling-stroke"
          />
          {visibleChartData.map((point, index) => {
            const x = originX + index * barStep
            const positiveHeight = (point.positiveValue / domainMax) * halfHeight
            const negativeHeight = (point.negativeValue / domainMax) * halfHeight

            return (
              <g key={`${point.label}-${index}`}>
                <rect
                  x={x}
                  y={baseline - positiveHeight}
                  width={fixedBarWidth}
                  height={positiveHeight}
                  fill={positiveColor}
                  onMouseMove={(event) => updateTooltip(event, point)}
                />
                <rect
                  x={x}
                  y={baseline}
                  width={fixedBarWidth}
                  height={negativeHeight}
                  fill={negativeColor}
                  onMouseMove={(event) => updateTooltip(event, point)}
                />
              </g>
            )
          })}
        </svg>
      </div>

      {activeTooltip ? (
        <div
          className="pointer-events-none absolute z-10 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel)/0.94)] px-2.5 py-2 text-xs text-foreground shadow-[0_16px_40px_rgb(0_0_0/0.16)] backdrop-blur"
          style={{
            left: Math.min(activeTooltip.x + 12, Math.max(12, containerWidth - 132)),
            top: Math.max(8, activeTooltip.y - 48),
          }}
        >
          <p className="mb-1 text-muted-soft">{activeTooltip.point.label}</p>
          <div className="space-y-0.5">
            <p>{positiveLabel} {valueFormatter(activeTooltip.point.positiveValue)}</p>
            <p>{negativeLabel} {valueFormatter(activeTooltip.point.negativeValue)}</p>
          </div>
        </div>
      ) : null}
    </div>
  )
}
