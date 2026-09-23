import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { installAppViewportSize } from '@/lib/app-viewport'
import { useAuthStore } from '@/stores/auth-store'
import { i18nReady } from './locales/i18n'
import './index.css'
import App from './App'

installAppViewportSize()
void useAuthStore.getState().initialize()

i18nReady.then(() => {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
})
