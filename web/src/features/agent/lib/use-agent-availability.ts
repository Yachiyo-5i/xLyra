import { useQuery } from '@tanstack/react-query'
import { fetchAgentRuntimeSettings } from '@/features/agent/api/agent'

// Shares the query cache with the agent settings page, so nav visibility follows saves/refreshes there.
export const agentSettingsKey = ['settings', 'agent'] as const

/**
 * Whether the agent workspace is available (runner configured).
 * Defaults to available while loading or on query failure to avoid nav flicker;
 * the entry only hides once "not configured" is confirmed.
 */
export function useAgentAvailability(): boolean {
  const query = useQuery({
    queryKey: [...agentSettingsKey, 'runtime'],
    queryFn: fetchAgentRuntimeSettings,
    retry: false,
    staleTime: 30_000,
  })
  return query.data ? Boolean(query.data.runner_base_url) : true
}
