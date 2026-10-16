import { HoverDetails } from '@/components/common/hover-details'

/**
 * 异常账号卡片里的错误信息：标题 + 一行红字摘要，悬停（移动端点击）展开完整内容。
 * 与「额度」标题同级，不再套红色底框。
 */
export function OAuthErrorPreview({
  label,
  message,
}: {
  label: string
  message: string
}) {
  return (
    <div className="min-w-0">
      <div className="mb-1.5 text-footnote font-medium text-label-2">{label}</div>
      <HoverDetails
        asChild
        title={label}
        titleClassName="text-danger"
        contentClassName="w-[min(560px,calc(100vw-2rem))]"
        content={<div className="whitespace-pre-wrap break-words text-label-2">{message}</div>}
      >
        <p className="line-clamp-1 break-all text-footnote text-danger">{message}</p>
      </HoverDetails>
    </div>
  )
}
