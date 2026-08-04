import { useEffect, useMemo, useState } from 'react'
import type { ComponentType, ReactNode } from 'react'
import { Activity, Cpu, Database, HardDrive, Network } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { DivergingBarChart, type DivergingBarDatum } from '@/components/common/diverging-bar-chart'
import { Card } from '@/components/ui/card'
import { Progress, type ProgressThreshold } from '@/components/ui/progress'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { createDashboardResourceStream, type DashboardSystemResourcePoint } from '@/features/dashboard/api/dashboard'
import { cn } from '@/lib/utils'
import {
  dashboardChartColors,
  dashboardTooltipItemStyle,
  dashboardTooltipLabelStyle,
  dashboardTooltipStyle,
} from './chart-style'

type ResourceTab = 'overview' | 'cpu' | 'memory' | 'disk' | 'network'

type SystemResourcePanelProps = {
  className?: string
}

const maxResourceSamples = 480
const usageThresholds: ProgressThreshold[] = [
  { max: 60, variant: 'success' },
  { max: 80, variant: 'warning' },
  { max: 100, variant: 'danger' },
]

export function SystemResourcePanel({ className }: SystemResourcePanelProps) {
  const { t } = useTranslation('dashboard')
  const [activeTab, setActiveTab] = useState<ResourceTab>('overview')
  const [samples, setSamples] = useState<DashboardSystemResourcePoint[]>([])
  const [streamState, setStreamState] = useState<'connecting' | 'connected' | 'error'>('connecting')

  const resourceTabs: Array<{ label: string; value: ResourceTab; icon: ComponentType<{ className?: string }> }> = [
    { label: t('resources.tabs.overview'), value: 'overview', icon: Activity },
    { label: 'CPU', value: 'cpu', icon: Cpu },
    { label: t('resources.tabs.memory'), value: 'memory', icon: Database },
    { label: t('resources.tabs.disk'), value: 'disk', icon: HardDrive },
    { label: t('resources.tabs.network'), value: 'network', icon: Network },
  ]

  const active = resourceTabs.find((item) => item.value === activeTab) ?? resourceTabs[0]
  const latest = samples.at(-1)

  useEffect(() => {
    const stream = createDashboardResourceStream()

    stream.onopen = () => {
      setStreamState('connected')
    }
    stream.onerror = () => {
      setStreamState('error')
    }
    stream.addEventListener('resource', (event) => {
      try {
        const point = JSON.parse(event.data) as DashboardSystemResourcePoint
        setStreamState('connected')
        setSamples((current) => [...current, point].slice(-maxResourceSamples))
      } catch {
        setStreamState('error')
      }
    })

    return () => {
      stream.close()
    }
  }, [])

  const resourceMeta = useMemo(() => buildResourceMeta(activeTab, latest, t), [activeTab, latest, t])

  return (
    <Card className={cn('flex min-h-0 flex-col rounded-lg p-5', className)}>
      <div className="mb-4 shrink-0 space-y-1">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
          <h3 className="text-base font-semibold text-foreground">{t('resources.title')}</h3>
          <div className="scrollbar-hidden min-w-0 max-w-full overflow-x-auto">
            <Tabs variant="slash" value={activeTab} onValueChange={(value) => setActiveTab(value as ResourceTab)}>
              <TabsList>
                {resourceTabs.map((item) => (
                  <TabsTrigger key={item.value} value={item.value} className="text-sm">
                    {item.label}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          </div>
        </div>
        <p className="text-sm text-muted-soft">{formatStreamState(streamState, latest, t)}</p>
      </div>

      <div className="scrollbar-hidden min-h-0 flex-1 overflow-auto">
        {samples.length ? (
          <div className="flex h-full min-h-0 flex-col gap-3">
            {activeTab === 'overview' ? (
              <ResourceOverview latest={latest} t={t} />
            ) : activeTab === 'disk' ? (
              <DiskResourceView latest={latest} samples={samples} t={t} />
            ) : (
              <>
                <div className="grid shrink-0 grid-cols-[minmax(0,0.85fr)_minmax(0,0.85fr)_minmax(0,1.35fr)] gap-3">
                  {resourceMeta.metrics.map((metric) => (
                    <div key={metric.label} className="min-w-0">
                      <p className="text-xs text-muted-soft">{metric.label}</p>
                      <p className="mt-1 truncate text-sm font-semibold tabular-nums text-foreground" title={metric.value}>
                        {metric.value}
                      </p>
                    </div>
                  ))}
                </div>
                {activeTab === 'network' ? (
                  <ResourceDivergingChart
                    data={buildDivergingResourceData(samples, 'network')}
                    positiveLabel={t('resources.metrics.download')}
                    negativeLabel={t('resources.metrics.upload')}
                  />
                ) : (
                  <ResourceChart tab={activeTab} data={samples} t={t} />
                )}
              </>
            )}
          </div>
        ) : (
          <div className="flex h-full min-h-0 items-center justify-center rounded-lg bg-[hsl(var(--surface-subtle)/0.42)]">
            <div className="space-y-1 text-center">
              <p className="text-sm font-medium text-foreground">{t('resources.status.connecting', { label: active.label })}</p>
              <p className="text-sm text-muted-soft">{t('resources.status.waiting')}</p>
            </div>
          </div>
        )}
      </div>
    </Card>
  )
}

function ResourceOverview({ latest, t }: { latest?: DashboardSystemResourcePoint; t: (key: string) => string }) {
  if (!latest) return null

  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <ResourceOverviewItem
        icon={Cpu}
        label="CPU"
        value={`${formatFixed(latest.cpu.usage_percent)}%`}
        description={`Load ${formatFixed(latest.cpu.load1)} · ${latest.cpu.cores} ${t('resources.metrics.cores')}`}
        caption={`5m ${formatFixed(latest.cpu.load5)} · 15m ${formatFixed(latest.cpu.load15)}`}
        progress={latest.cpu.usage_percent}
      />
      <ResourceOverviewItem
        icon={Database}
        label={t('resources.tabs.memory')}
        value={`${formatFixed(latest.memory.usage_percent)}%`}
        description={`${formatBytes(latest.memory.used_bytes)} / ${formatBytes(latest.memory.total_bytes)}`}
        caption={`${t('resources.metrics.available')} ${formatBytes(latest.memory.available_bytes)}`}
        progress={latest.memory.usage_percent}
      />
      <ResourceOverviewItem
        icon={HardDrive}
        label={t('resources.tabs.disk')}
        value={`${formatFixed(latest.disk.usage_percent)}%`}
        description={`${t('resources.metrics.read')} ${formatBytesPerSecond(latest.disk.read_bytes_per_sec)} · ${t('resources.metrics.write')} ${formatBytesPerSecond(latest.disk.write_bytes_per_sec)}`}
        caption={`${t('resources.metrics.used')} ${formatBytes(latest.disk.used_bytes)} / ${formatBytes(latest.disk.total_bytes)}`}
        progress={latest.disk.usage_percent}
      />
      <ResourceOverviewItem
        icon={Network}
        label={t('resources.tabs.network')}
        value={formatBytesPerSecond(latest.network.rx_bytes_per_sec + latest.network.tx_bytes_per_sec)}
        description={`${t('resources.metrics.download')} ${formatBytesPerSecond(latest.network.rx_bytes_per_sec)} · ${t('resources.metrics.upload')} ${formatBytesPerSecond(latest.network.tx_bytes_per_sec)}`}
        caption={`${t('resources.metrics.total')} ${formatBytes(latest.network.rx_bytes_total)} / ${formatBytes(latest.network.tx_bytes_total)}`}
      >
        <NetworkThroughputBar rx={latest.network.rx_bytes_per_sec} tx={latest.network.tx_bytes_per_sec} />
      </ResourceOverviewItem>
    </div>
  )
}

function ResourceOverviewItem({
  icon: Icon,
  label,
  value,
  description,
  caption,
  progress,
  children,
}: {
  icon: ComponentType<{ className?: string }>
  label: string
  value: string
  description: string
  caption: string
  progress?: number
  children?: ReactNode
}) {
  return (
    <div className="flex flex-col rounded-lg bg-[hsl(var(--surface-subtle)/0.42)] p-3">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <Icon className="h-4 w-4 shrink-0 text-muted-soft" />
          <span className="text-sm font-medium text-foreground">{label}</span>
        </div>
        <span className="text-sm font-semibold tabular-nums text-foreground">{value}</span>
      </div>
      <div className="mt-2 min-w-0 space-y-2">
        <p className="truncate text-xs text-muted-soft" title={description}>{description}</p>
        {typeof progress === 'number' ? (
          <Progress value={progress} thresholds={usageThresholds} className="h-1.5" />
        ) : (
          children
        )}
        <p className="truncate text-xs text-muted-soft" title={caption}>{caption}</p>
      </div>
    </div>
  )
}

function NetworkThroughputBar({ rx, tx }: { rx: number; tx: number }) {
  const total = Math.max(0, rx) + Math.max(0, tx)
  const rxPercent = total > 0 ? (Math.max(0, rx) / total) * 100 : 50

  return (
    <div className="flex h-1.5 overflow-hidden rounded-full bg-[hsl(var(--glass-border))]">
      <div className="bg-[hsl(190_88%_42%)]" style={{ width: `${rxPercent}%` }} />
      <div className="flex-1 bg-[hsl(154_70%_38%)]" />
    </div>
  )
}

function DiskResourceView({
  latest,
  samples,
  t,
}: {
  latest?: DashboardSystemResourcePoint
  samples: DashboardSystemResourcePoint[]
  t: (key: string) => string
}) {
  if (!latest) return null

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div className="grid shrink-0 gap-3 sm:grid-cols-[minmax(0,1.2fr)_minmax(0,0.8fr)]">
        <div className="min-w-0 rounded-lg bg-[hsl(var(--surface-subtle)/0.42)] p-3">
          <div className="mb-2 flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="text-sm font-medium text-foreground">{t('resources.metrics.capacity')}</p>
              <p className="truncate text-xs text-muted-soft" title={latest.disk.path}>{t('resources.metrics.mountPoint')} {latest.disk.path}</p>
            </div>
            <p className="text-sm font-semibold tabular-nums text-foreground">{formatFixed(latest.disk.usage_percent)}%</p>
          </div>
          <Progress value={latest.disk.usage_percent} thresholds={usageThresholds} className="h-1.5" />
          <div className="mt-2 grid grid-cols-3 gap-3 text-xs">
            <DiskCapacityMetric label={t('resources.metrics.used')} value={formatBytes(latest.disk.used_bytes)} />
            <DiskCapacityMetric label={t('resources.metrics.available')} value={formatBytes(latest.disk.free_bytes)} />
            <DiskCapacityMetric label={t('resources.metrics.total')} value={formatBytes(latest.disk.total_bytes)} />
          </div>
        </div>
        <div className="min-w-0 rounded-lg bg-[hsl(var(--surface-subtle)/0.42)] p-3">
          <p className="text-sm font-medium text-foreground">{t('resources.metrics.readWrite')}</p>
          <div className="mt-2 grid grid-cols-2 gap-3">
            <DiskIOMetric label={t('resources.metrics.read')} value={formatBytesPerSecond(latest.disk.read_bytes_per_sec)} />
            <DiskIOMetric label={t('resources.metrics.write')} value={formatBytesPerSecond(latest.disk.write_bytes_per_sec)} />
          </div>
          <p
            className="mt-2 truncate text-xs text-muted-soft"
            title={`${t('resources.metrics.read')} ${formatBytes(latest.disk.read_bytes_total)} / ${t('resources.metrics.write')} ${formatBytes(latest.disk.write_bytes_total)}`}
          >
            {(t as (key: string, options?: Record<string, unknown>) => string)('resources.description.totalReadWrite', { read: formatBytes(latest.disk.read_bytes_total), write: formatBytes(latest.disk.write_bytes_total) })}
          </p>
        </div>
      </div>
      <ResourceDivergingChart
        data={buildDivergingResourceData(samples, 'disk')}
        positiveLabel={t('resources.metrics.read')}
        negativeLabel={t('resources.metrics.write')}
        className="min-h-[82px]"
      />
    </div>
  )
}

function DiskCapacityMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <p className="text-muted-soft">{label}</p>
      <p className="mt-0.5 truncate font-medium tabular-nums text-foreground" title={value}>{value}</p>
    </div>
  )
}

function DiskIOMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <p className="text-xs text-muted-soft">{label}</p>
      <p className="mt-1 truncate text-sm font-semibold tabular-nums text-foreground" title={value}>{value}</p>
    </div>
  )
}

function ResourceChart({ tab, data, t }: { tab: Exclude<ResourceTab, 'overview' | 'disk' | 'network'>; data: DashboardSystemResourcePoint[]; t: (key: string) => string }) {
  return (
    <div className="min-h-0 flex-1">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="hsl(var(--glass-border))" vertical={false} />
          <XAxis
            dataKey="time"
            minTickGap={28}
            tickLine={false}
            axisLine={false}
            tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 11 }}
          />
          <YAxis
            domain={[0, 100]}
            tickFormatter={formatPercentTick}
            tickLine={false}
            axisLine={false}
            width={48}
            tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 11 }}
          />
          <Tooltip
            contentStyle={dashboardTooltipStyle}
            itemStyle={dashboardTooltipItemStyle}
            labelStyle={dashboardTooltipLabelStyle}
            formatter={(value, name) => [
              `${formatFixed(Number(value))}%`,
              name,
            ]}
          />
          <Line type="monotone" dataKey={resourceDataKey(tab)} name={tab === 'memory' ? t('resources.tabs.memory') : 'CPU'} stroke={dashboardChartColors[0]} strokeWidth={2} dot={false} activeDot={{ r: 4 }} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}

function ResourceDivergingChart({
  data,
  positiveLabel,
  negativeLabel,
  className,
}: {
  data: DivergingBarDatum[]
  positiveLabel: string
  negativeLabel: string
  className?: string
}) {
  return (
    <div className={cn('min-h-[96px] flex-1', className)}>
      <DivergingBarChart
        data={data}
        positiveLabel={positiveLabel}
        negativeLabel={negativeLabel}
        positiveColor="hsl(324 86% 58%)"
        negativeColor="hsl(211 96% 52%)"
        valueFormatter={formatBytesPerSecond}
        height="100%"
        barCategoryGap={0}
        barGap={0}
        fillWidth
        targetBarWidth={2}
        className="h-full bg-transparent p-0"
      />
    </div>
  )
}

function buildDivergingResourceData(
  samples: DashboardSystemResourcePoint[],
  type: 'disk' | 'network',
): DivergingBarDatum[] {
  return samples.map((sample) => {
    if (type === 'disk') {
      return {
        label: sample.time,
        positive: sample.disk.read_bytes_per_sec,
        negative: sample.disk.write_bytes_per_sec,
      }
    }

    return {
      label: sample.time,
      positive: sample.network.rx_bytes_per_sec,
      negative: sample.network.tx_bytes_per_sec,
    }
  })
}

function buildResourceMeta(tab: ResourceTab, latest: DashboardSystemResourcePoint | undefined, t: (key: string) => string) {
  if (!latest) {
    return {
      metrics: [
        { label: t('resources.metrics.current'), value: '-' },
        { label: t('resources.metrics.peak'), value: '-' },
        { label: t('resources.metrics.updatedAt'), value: '-' },
      ],
    }
  }

  if (tab === 'cpu') {
    return {
      metrics: [
        { label: t('resources.metrics.usage'), value: `${formatFixed(latest.cpu.usage_percent)}%` },
        { label: t('resources.metrics.cores'), value: `${latest.cpu.cores}` },
        { label: 'Load', value: `${formatFixed(latest.cpu.load1)} / ${formatFixed(latest.cpu.load5)} / ${formatFixed(latest.cpu.load15)}` },
      ],
    }
  }

  if (tab === 'memory') {
    return {
      metrics: [
        { label: t('resources.metrics.usage'), value: `${formatFixed(latest.memory.usage_percent)}%` },
        { label: t('resources.metrics.used'), value: formatBytes(latest.memory.used_bytes) },
        { label: t('resources.metrics.available'), value: formatBytes(latest.memory.available_bytes) },
      ],
    }
  }

  if (tab === 'disk') {
    return {
      metrics: [
        { label: t('resources.metrics.usage'), value: `${formatFixed(latest.disk.usage_percent)}%` },
        { label: t('resources.metrics.used'), value: formatBytes(latest.disk.used_bytes) },
        { label: t('resources.metrics.available'), value: formatBytes(latest.disk.free_bytes) },
      ],
    }
  }

  return {
    metrics: [
      { label: t('resources.metrics.download'), value: formatBytesPerSecond(latest.network.rx_bytes_per_sec) },
      { label: t('resources.metrics.upload'), value: formatBytesPerSecond(latest.network.tx_bytes_per_sec) },
      { label: t('resources.metrics.total'), value: `${formatBytes(latest.network.rx_bytes_total)} / ${formatBytes(latest.network.tx_bytes_total)}` },
    ],
  }
}

function resourceDataKey(tab: Exclude<ResourceTab, 'overview' | 'disk' | 'network'>) {
  if (tab === 'memory') return 'memory_usage_percent'
  return 'cpu_usage_percent'
}

function formatStreamState(state: 'connecting' | 'connected' | 'error', latest: DashboardSystemResourcePoint | undefined, t: (key: string) => string) {
  if (latest) return `${t('resources.realtime')} · ${latest.time}`
  if (state === 'error') return t('resources.waitingReconnect')
  return t('resources.connecting')
}

function formatPercentTick(value: number) {
  return `${Math.round(value)}%`
}

function formatBytesPerSecond(value: number) {
  return `${formatBytes(value)}/s`
}

function formatBytes(value: number) {
  if (!Number.isFinite(value)) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let next = Math.max(0, value)
  let index = 0
  while (next >= 1024 && index < units.length - 1) {
    next /= 1024
    index += 1
  }
  const precision = next >= 100 || index === 0 ? 0 : next >= 10 ? 1 : 2
  return `${next.toFixed(precision)} ${units[index]}`
}

function formatFixed(value: number) {
  if (!Number.isFinite(value)) return '-'
  if (Math.abs(value) >= 100) return value.toFixed(0)
  if (Math.abs(value) >= 10) return value.toFixed(1)
  return value.toFixed(2)
}
