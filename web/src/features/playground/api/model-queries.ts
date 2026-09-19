import { queryOptions, type QueryClient } from '@tanstack/react-query'
import { listPlaygroundModels } from '@/features/playground/api/playground'

export const playgroundModelQueryKey = (apiKeyId: string | null) => ['playground', 'models', apiKeyId] as const

export function playgroundModelQueryOptions(apiKeyId: string | null) {
  return queryOptions({
    queryKey: playgroundModelQueryKey(apiKeyId),
    queryFn: ({ signal }) => listPlaygroundModels(apiKeyId as string, signal),
    enabled: Boolean(apiKeyId),
    staleTime: 5 * 60 * 1000,
    refetchOnWindowFocus: 'always',
    refetchOnMount: 'always',
    retry: false,
  })
}

export async function invalidatePlaygroundModels(queryClient: QueryClient, apiKeyId: string) {
  const filter = { queryKey: playgroundModelQueryKey(apiKeyId), exact: true }
  await queryClient.cancelQueries(filter)
  await queryClient.invalidateQueries(filter)
}
