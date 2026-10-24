import { useMemo, useState, type PropsWithChildren } from 'react'
import { InlineTitleContext } from '@/components/layout/inline-title'

export function InlineTitleProvider({ children }: PropsWithChildren) {
  const [title, setTitle] = useState<string | null>(null)
  const value = useMemo(() => ({ title, setTitle }), [title])
  return <InlineTitleContext.Provider value={value}>{children}</InlineTitleContext.Provider>
}
