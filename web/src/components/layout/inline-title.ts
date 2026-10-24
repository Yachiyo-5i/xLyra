import { createContext, useContext } from 'react'

/**
 * 大标题收起：PageHeader 的大标题滚出顶栏区域后，
 * 顶栏中间淡入同名的小号标题。外壳提供上下文，PageHeader 写入，顶栏读取。
 */
export type InlineTitleContextValue = {
  title: string | null
  setTitle: (title: string | null) => void
}

export const InlineTitleContext = createContext<InlineTitleContextValue | null>(null)

/** 顶栏读取：当前应显示的行内标题；没有外壳上下文时为 null */
export function useInlineTitle() {
  return useContext(InlineTitleContext)?.title ?? null
}

/** PageHeader 写入；不在外壳中时为 undefined */
export function useSetInlineTitle() {
  return useContext(InlineTitleContext)?.setTitle
}
