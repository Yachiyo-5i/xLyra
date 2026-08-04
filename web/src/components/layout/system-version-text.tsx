import { useQuery } from '@tanstack/react-query'
import { fetchSystemVersion } from '@/features/system/api/version'

type SystemVersionTextProps = {
  prefix?: string
}

export function SystemVersionText({ prefix = 'Version' }: SystemVersionTextProps) {
  const versionQuery = useQuery({
    queryKey: ['system', 'version'],
    queryFn: fetchSystemVersion,
    staleTime: Infinity,
    retry: 1,
  })
  const version = versionQuery.data?.version ?? 'dev'

  return <span>{prefix} {version}</span>
}
