import type { ClassNames } from 'react-day-picker'

/**
 * 日期 / 日期时间选择器共用的日历样式。
 * 日期格为圆形；今天用细环标出；选中为强调色实底 + 深色字。
 */
export const calendarClassNames: Partial<ClassNames> = {
  root: 'relative w-full',
  months: 'flex flex-col gap-4',
  month: 'space-y-2',
  month_caption: 'relative flex h-10 items-center justify-center px-10',
  dropdowns: 'flex items-center justify-center gap-1.5',
  caption_label: 'text-headline text-label',
  nav: 'pointer-events-none absolute inset-x-0 top-0 flex h-10 items-center justify-between',
  button_previous:
    'pointer-events-auto flex size-8 items-center justify-center rounded-full text-label-2 transition-colors hover:bg-fill-3 hover:text-label disabled:pointer-events-none disabled:opacity-30',
  button_next:
    'pointer-events-auto flex size-8 items-center justify-center rounded-full text-label-2 transition-colors hover:bg-fill-3 hover:text-label disabled:pointer-events-none disabled:opacity-30',
  chevron: 'text-current',
  month_grid: 'w-full border-collapse',
  weekdays: 'grid grid-cols-7',
  weekday: 'flex h-8 items-center justify-center text-caption-1 text-label-3',
  weeks: 'grid gap-0.5',
  week: 'grid grid-cols-7 gap-0.5',
  day: 'relative flex items-center justify-center p-0 text-center',
  day_button:
    'flex size-9 items-center justify-center rounded-full text-body text-label tabular-nums transition-colors duration-100 hover:bg-fill-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint/55',
  selected: '[&>button]:bg-tint [&>button]:font-semibold [&>button]:text-tint-foreground [&>button]:hover:bg-tint',
  today: '[&>button]:font-semibold [&>button]:ring-1 [&>button]:ring-inset [&>button]:ring-white/35',
  outside: '[&>button]:text-label-3',
  disabled: '[&>button]:pointer-events-none [&>button]:opacity-30',
  hidden: 'invisible',
}

/** 日历顶部的年 / 月下拉触发器 */
export const calendarDropdownTriggerClass =
  'h-[var(--control-h-sm)] min-w-[96px] rounded-full bg-fill-3 px-3 text-callout font-medium [&>span]:overflow-visible [&>span]:whitespace-nowrap [&>span]:text-clip'

/** 时间列表中的一项（小时 / 分钟） */
export function timeOptionClass(selected: boolean) {
  return selected
    ? 'flex h-8 w-full items-center justify-center rounded-full bg-tint text-callout font-semibold tabular-nums text-tint-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint/55'
    : 'flex h-8 w-full items-center justify-center rounded-full text-callout tabular-nums text-label-2 transition-colors hover:bg-fill-3 hover:text-label focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint/55'
}
