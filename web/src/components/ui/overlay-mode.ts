import { createContext, useContext } from 'react'

/**
 * 覆盖层当前的呈现形态：
 * - center：居中弹窗（桌面的 Dialog、移动端保持居中的确认弹窗）
 * - side：右侧面板（桌面的 Sheet）
 * - draw：底部抽屉（移动端的 Dialog 配置弹窗 / Sheet 面板都会变成 Draw）
 *
 * Dialog / Sheet 的 Content 把形态放进上下文，它们的 Header / Footer 据此切换排法。
 */
export type OverlayMode = 'center' | 'side' | 'draw'

export const OverlayModeContext = createContext<OverlayMode>('center')

export function useOverlayMode() {
  return useContext(OverlayModeContext)
}
