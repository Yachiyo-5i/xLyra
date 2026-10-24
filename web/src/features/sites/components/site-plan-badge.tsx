import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { Site } from '@/features/sites/api/sites'

/**
 * Coding Plan 订阅档位 badge，适用于 Kimi（Andante/…/Vivace）和 GLM（Pro/Max 等）。
 * 档位名由后端 quota probe 从各平台额度接口解析（Kimi 取 membership.level，
 * GLM 取 data.level），通过 site.quota_probe.plan 透传；探测未成功时（plan 为空）不渲染。
 */
export function SitePlanBadge({ site, className }: { site: Site; className?: string }) {
  const plan = site.quota_probe?.plan
  if (!plan) return null
  return (
    // 计费 / 档位信息只是补充说明：中性标签，不用强调色抢过站点名
    <Badge variant="neutral" className={cn('shrink-0 select-none', className)}>
      {plan}
    </Badge>
  )
}
