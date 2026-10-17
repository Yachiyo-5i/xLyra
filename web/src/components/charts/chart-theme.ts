import type { CSSProperties } from 'react'

/**
 * 图表主题。recharts 把颜色写进 SVG 属性，
 * 这里给出与 `--chart-1…8` 相同的十六进制值，方便在 JS 里取用、拼透明度。
 * 顺序即系列顺序：蓝、青、绿、橙、紫、粉、黄、靛。
 */
export const chartColors = ['#0a84ff', '#40c8e0', '#30d158', '#ff9f0a', '#bf5af2', '#ff375f', '#ffd60a', '#5e5ce6']

/** 网格线：只画水平线，用分隔线颜色 */
export const chartGridStroke = 'var(--separator)'

/** 坐标轴文字：Caption 2（11）、label-3，数字等宽 */
export const chartTick = { fill: 'var(--label-tertiary)', fontSize: 11, fontFamily: 'var(--font-mono)' }

/** 悬停时的柱状背景条 */
export const chartCursorFill = 'var(--fill-quaternary)'

/** 环形图扇区之间的分隔描边：与卡片底色一致，看起来像留了缝 */
export const chartSliceStroke = 'var(--bg-elevated-1)'

/** Tooltip：功能层 Liquid Glass regular（与 .lg-regular 一致），圆角 12 */
export const chartTooltipStyle: CSSProperties = {
  border: '1px solid transparent',
  borderRadius: 12,
  background:
    'var(--top-light) padding-box, linear-gradient(rgb(24 26 33 / 0.58), rgb(24 26 33 / 0.58)) padding-box, linear-gradient(160deg, rgb(255 255 255 / 0.16), rgb(255 255 255 / 0.045) 36%, rgb(255 255 255 / 0.025) 64%, rgb(255 255 255 / 0.07)) border-box',
  boxShadow: 'inset 0 1px 0 rgb(255 255 255 / 0.06), 0 8px 26px rgb(0 0 0 / 0.3)',
  WebkitBackdropFilter: 'blur(24px) saturate(150%)',
  backdropFilter: 'blur(24px) saturate(150%)',
  color: 'var(--label-primary)',
  padding: '8px 12px',
  fontSize: 12,
  lineHeight: 1.35,
}

export const chartTooltipLabelStyle: CSSProperties = {
  color: 'var(--label-secondary)',
  fontSize: 12,
  marginBottom: 4,
}

export const chartTooltipItemStyle: CSSProperties = {
  color: 'var(--label-primary)',
  fontSize: 12,
  padding: '1px 0',
}
