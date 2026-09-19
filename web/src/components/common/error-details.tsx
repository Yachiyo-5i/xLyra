import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { HoverDetails } from '@/components/common/hover-details'

export function ErrorDetails({ children, message, description, asChild = false }: {
  children: ReactNode
  message?: string
  description?: string
  asChild?: boolean
}) {
  const { t } = useTranslation('common')
  return (
    <HoverDetails
      asChild={asChild}
      disabled={!message}
      title={t('status.error')}
      description={description}
      titleClassName="text-red-400"
      content={<div className="whitespace-pre-wrap break-words text-muted-foreground">{message}</div>}
    >
      {children}
    </HoverDetails>
  )
}
