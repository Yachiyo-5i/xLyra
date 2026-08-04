import { useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { SitesWorkspace } from '@/features/sites/components/sites-workspace'

export function SitesPage() {
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    if (!location.search) return
    navigate({ pathname: location.pathname, hash: location.hash }, { replace: true })
  }, [location.hash, location.pathname, location.search, navigate])

  return <SitesWorkspace initialSearch={location.search} />
}
