import { isValidElement, memo, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Copy } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { cn } from '@/lib/utils'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'

type MarkdownMessageProps = {
  content: string
  className?: string
}

function reactNodeText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(reactNodeText).join('')
  if (isValidElement<{ children?: ReactNode }>(node)) return reactNodeText(node.props.children)
  return ''
}

function MarkdownCodeBlock({ children }: ComponentPropsWithoutRef<'pre'>) {
  const { t } = useTranslation('playground')
  const code = reactNodeText(children)

  return (
    <div className="playground-code-block">
      <button
        type="button"
        onClick={() => void copyToClipboard(code, t('actions.copied'), t('actions.copyFailed'))}
        className="absolute right-2 top-2 z-10 flex h-7 w-7 items-center justify-center rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-base))]/90 text-muted-soft shadow-sm backdrop-blur-sm transition-colors hover:text-foreground"
        aria-label={t('actions.copy')}
        title={t('actions.copy')}
      >
        <Copy className="h-3.5 w-3.5" />
      </button>
      <pre>{children}</pre>
    </div>
  )
}

function MarkdownMessageImpl({ content, className }: MarkdownMessageProps) {
  return (
    <div className={cn('playground-markdown text-sm leading-6 text-foreground', className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ pre: MarkdownCodeBlock }}>
        {content}
      </ReactMarkdown>
    </div>
  )
}

export const MarkdownMessage = memo(MarkdownMessageImpl)
