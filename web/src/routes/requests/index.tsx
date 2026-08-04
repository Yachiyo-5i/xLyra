import { useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { RequestsWorkspace } from '@/features/requests/components/requests-workspace'

export function RequestsPage() {
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    if (!location.search) return
    navigate({ pathname: location.pathname, hash: location.hash }, { replace: true })
  }, [location.hash, location.pathname, location.search, navigate])

  return <RequestsWorkspace initialSearch={location.search} />
}
