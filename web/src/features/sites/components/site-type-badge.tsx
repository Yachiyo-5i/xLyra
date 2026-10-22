import type { CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import type { SiteTypeInfo } from '@/features/sites/api/sites'
import { formatSiteTypeLabel, siteTypeColor } from '@/features/sites/lib/site-utils'
import { cn } from '@/lib/utils'

type SiteTypeBadgeProps = {
  siteType: string
  siteTypes?: SiteTypeInfo[]
  className?: string
}

/**
 * 站点类型徽标：颜色取自该类型的 logo 主色（见 siteTypeColor）。
 * 底是品牌色 16%，文字是品牌色向白色提亮一些，保证深色背景上的对比；
 * logo 没有颜色的类型退回中性徽标。
 */
export function SiteTypeBadge({ siteType, siteTypes, className }: SiteTypeBadgeProps) {
  const { i18n } = useTranslation()
  const label = formatSiteTypeLabel(siteType, siteTypes, i18n.language)
  const color = siteTypeColor(siteType)
  if (!color) {
    return <Badge variant="neutral" className={className}>{label}</Badge>
  }
  return (
    <Badge
      className={cn(
        'bg-[color-mix(in_srgb,var(--site-type-color)_16%,transparent)] text-[color-mix(in_srgb,var(--site-type-color)_72%,white)]',
        className,
      )}
      style={{ '--site-type-color': color } as CSSProperties}
    >
      {label}
    </Badge>
  )
}
