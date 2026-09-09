/**
 * agent 页关键 UI 状态的 localStorage 缓存：
 * - 背景图 URL：settings 接口返回前首帧直接用缓存 URL 起图，消除「等 settings → 才开始下图」的瀑布
 * - 最近会话 id：刷新后直接恢复上次会话，transcript 请求与 sessions/settings 并行发出
 */

const BACKGROUND_IMAGE_KEY = 'xlyra-agent-background-image'
const LAST_SESSION_KEY = 'xlyra-agent-last-session-id'

export function loadCachedBackgroundImage(): string | null {
  try {
    return window.localStorage.getItem(BACKGROUND_IMAGE_KEY)
  } catch {
    return null
  }
}

export function saveCachedBackgroundImage(url: string) {
  try {
    window.localStorage.setItem(BACKGROUND_IMAGE_KEY, url)
  } catch {
    // 隐私模式等场景写入失败可忽略：退化为每次等 settings 接口
  }
}

export function loadLastSessionId(): string | null {
  try {
    return window.localStorage.getItem(LAST_SESSION_KEY)
  } catch {
    return null
  }
}

export function saveLastSessionId(sessionId: string | null) {
  try {
    if (sessionId) {
      window.localStorage.setItem(LAST_SESSION_KEY, sessionId)
    } else {
      window.localStorage.removeItem(LAST_SESSION_KEY)
    }
  } catch {
    // 同上：写入失败仅意味着下次刷新不恢复会话
  }
}
