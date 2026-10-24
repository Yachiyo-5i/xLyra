import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { installAppViewportSize } from '@/lib/app-viewport'
import { installGlassRefraction } from '@/components/glass/refraction'
import { useAuthStore } from '@/stores/auth-store'
import { i18nReady } from './locales/i18n'
import './index.css'
import App from './App'

installAppViewportSize()
installGlassRefraction()
// 主题已固定为深色，清理旧版主题切换留下的本地存储
try {
  window.localStorage.removeItem('xlyra-theme')
} catch {
  // 存储不可用时无需处理
}
void useAuthStore.getState().initialize()

i18nReady.then(() => {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
})
