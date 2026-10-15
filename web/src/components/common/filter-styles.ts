import { cn } from '@/lib/utils'

/** 筛选栏选项：行高 36、圆角 10，选中 fill-3 底 */
export function filterOptionClass(active: boolean) {
  return cn(
    'flex w-full items-center gap-2.5 rounded-control px-3 py-2 text-left text-body transition-colors duration-150',
    active ? 'bg-fill-3 text-label' : 'text-label-2 hover:bg-fill-4 hover:text-label',
  )
}
