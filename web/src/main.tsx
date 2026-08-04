import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { installAppViewportSize } from '@/lib/app-viewport'
import { i18nReady } from './locales/i18n'
import './index.css'
import App from './App'

installAppViewportSize()

i18nReady.then(() => {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
})
