import { memo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'

type MarkdownMessageProps = {
  content: string
  className?: string
}

function MarkdownMessageImpl({ content, className }: MarkdownMessageProps) {
  return (
    <div className={cn('playground-markdown text-sm leading-6 text-foreground', className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
    </div>
  )
}

export const MarkdownMessage = memo(MarkdownMessageImpl)
