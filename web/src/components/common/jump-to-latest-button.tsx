import { ArrowDown } from 'lucide-react'

type JumpToLatestButtonProps = {
  label: string
  onClick: () => void
}

/** 锁定阅读时悬浮在滚动区底部的「回到最新」入口：点击贴底并恢复自动跟随 */
export function JumpToLatestButton({ label, onClick }: JumpToLatestButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className="absolute bottom-4 left-1/2 z-10 flex h-9 -translate-x-1/2 items-center gap-1.5 rounded-full border border-separator bg-elevated-1 px-3.5 text-footnote font-medium text-label-2 shadow-lg backdrop-blur transition-colors hover:text-label"
    >
      <ArrowDown className="h-3.5 w-3.5" />
      {label}
    </button>
  )
}
