/**
 * 输入类控件共用的样式。
 * Input / Textarea / Select / MultiSelect / 日期时间选择器共享同一套外观，避免各写一份。
 */

/** 输入框外观：填充色底、无描边、R-control 圆角；校验失败时显示红色外环 */
export const fieldSurfaceClass =
  'rounded-control bg-fill-3 text-label outline-none transition-[background-color,box-shadow,color,opacity] duration-150 disabled:cursor-not-allowed disabled:opacity-40 aria-[invalid=true]:ring-2 aria-[invalid=true]:ring-danger/60'

/** 文本输入获得焦点（含鼠标点击）时显示强调色外环 */
export const fieldFocusClass = 'focus:ring-2 focus:ring-tint/50'

/** 按钮型触发器只在键盘聚焦或展开时显示外环 */
export const fieldTriggerClass = `${fieldSurfaceClass} flex h-[var(--control-h)] w-full items-center justify-between gap-3 px-3.5 text-left text-body focus-visible:ring-2 focus-visible:ring-tint/50 data-[state=open]:ring-2 data-[state=open]:ring-tint/50`

/** 触发器里的前后图标 */
export const fieldIconClass = 'size-4 shrink-0 text-label-3'

/** 触发器右侧的清除按钮（绝对定位在箭头左边） */
export const fieldClearButtonClass =
  'absolute right-9 top-1/2 flex size-6 -translate-y-1/2 items-center justify-center rounded-full text-label-3 transition-colors hover:bg-fill-2 hover:text-label'

/** 筛选器触发器：虚线胶囊，选中后变为实线并加浅强调底 */
export const filterTriggerClass =
  'inline-flex h-[var(--control-h-sm)] w-fit shrink-0 items-center justify-center gap-1.5 rounded-full border border-dashed border-white/22 bg-transparent px-3 text-left text-callout font-medium text-label-2 outline-none transition-colors duration-150 hover:bg-fill-4 hover:text-label focus-visible:ring-2 focus-visible:ring-tint/50'
export const filterTriggerActiveClass = 'border-solid border-tint/45 bg-tint/10 text-label'

/** 弹出面板（菜单、下拉、日期面板）：功能层 Liquid Glass，从触发点放大展开 */
export const popoverPanelClass =
  'lg-regular rounded-card p-1.5 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95'

/** 面板里的搜索框 */
export const panelSearchClass = 'flex h-[var(--control-h-sm)] items-center gap-2 rounded-inner bg-fill-3 px-2.5'

/** 菜单项：圆角与面板同心（20 − 6 = 14） */
export const menuItemClass =
  'relative flex min-h-[var(--control-h-sm)] w-full cursor-default select-none items-center gap-2 rounded-[14px] px-2.5 py-1.5 text-body text-label outline-none transition-colors duration-100 data-[highlighted]:bg-fill-3 data-[disabled]:pointer-events-none data-[disabled]:opacity-40'

/** 面板内分组标题 */
export const menuGroupLabelClass = 'px-2.5 pb-1 pt-2.5 text-caption-1 text-label-3 first:pt-1'

/** 非 Radix 菜单（Popover 里手写的按钮列表）中的一行：账户菜单、语言菜单等 */
export const menuButtonClass =
  'flex min-h-[var(--control-h-sm)] w-full items-center gap-2.5 rounded-[14px] px-2.5 py-1.5 text-left text-body text-label outline-none transition-colors duration-100 hover:bg-fill-3 focus-visible:bg-fill-3 disabled:pointer-events-none disabled:opacity-40'

/** 面板内的分隔线：左右与菜单项文字对齐 */
export const menuSeparatorClass = 'mx-2.5 my-1 h-px bg-separator'
