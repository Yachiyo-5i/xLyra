import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { Site } from '@/features/sites/api/sites'

/**
 * Kimi Coding Plan 的订阅档位 badge（Andante/Moderato/Allegretto/Allegro/Vivace）。
 * 档位名由后端 quota probe 从 /coding/v1/usages 的 membership.level 解析，
 * 通过 site.quota_probe.plan 透传；探测未成功时（plan 为空）不渲染。
 */
export function SitePlanBadge({ site, className }: { site: Site; className?: string }) {
  const plan = site.quota_probe?.plan
  if (!plan) return null
  return (
    <Badge
      variant="accent"
      className={cn('shrink-0 px-1.5 py-0 text-[11px] select-none', className)}
    >
      {plan}
    </Badge>
  )
}
