import { forwardRef, type InputHTMLAttributes, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

/** 登录 / 注册 / 状态页的表单卡片：Liquid Glass thick、圆角 24 */
export function AuthCard({
  title,
  description,
  children,
  className,
}: {
  title: string
  description?: string
  children: ReactNode
  className?: string
}) {
  return (
    <div className={cn('lg-thick rounded-shell p-6', className)}>
      {/* 右侧留出关闭按钮的位置 */}
      <h2 className="pr-10 text-title-2 text-label">{title}</h2>
      {description ? <p className="mt-1 text-subhead text-label-2">{description}</p> : null}
      <div className="mt-5">{children}</div>
    </div>
  )
}

/** 分组输入：一块 fill-3 底的圆角容器，行与行之间用分隔线隔开（仿 iOS 表单分组） */
export function FieldGroup({ children, invalid = false }: { children: ReactNode; invalid?: boolean }) {
  return (
    <div
      className={cn(
        'overflow-hidden rounded-inner bg-fill-3 transition-shadow [&>*+*]:border-t [&>*+*]:border-separator',
        invalid && 'ring-2 ring-danger/50',
      )}
    >
      {children}
    </div>
  )
}

type FieldRowProps = InputHTMLAttributes<HTMLInputElement> & {
  icon: ReactNode
}

/** 分组里的一行输入：左侧 label-3 图标，行高 48；聚焦时整行微微提亮 */
export const FieldRow = forwardRef<HTMLInputElement, FieldRowProps>(({ icon, className, ...props }, ref) => (
  <label className="flex h-12 items-center gap-3 px-4 transition-colors focus-within:bg-fill-4">
    <span className="flex size-4 shrink-0 items-center justify-center text-label-3 [&_svg]:size-4">{icon}</span>
    <input
      ref={ref}
      {...props}
      className={cn('h-full min-w-0 flex-1 bg-transparent text-body text-label outline-none placeholder:text-label-3', className)}
    />
  </label>
))
FieldRow.displayName = 'FieldRow'

/** 分组下方的错误提示（只显示第一条，避免一堆红字） */
export function FieldErrors({ messages }: { messages: Array<string | undefined> }) {
  const message = messages.find(Boolean)
  if (!message) return null
  return (
    <p role="alert" className="px-1 text-footnote text-danger">
      {message}
    </p>
  )
}
