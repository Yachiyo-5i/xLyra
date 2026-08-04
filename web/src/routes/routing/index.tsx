import { useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { RoutesWorkspace } from '@/features/routes/components/routes-workspace'

export function RoutesPage() {
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    if (!location.search) return
    navigate({ pathname: location.pathname, hash: location.hash }, { replace: true })
  }, [location.hash, location.pathname, location.search, navigate])

  return <RoutesWorkspace initialSearch={location.search} />
}
