export const RESPONSE_TIMER_REVEAL_MS = 6_000
export const RESPONSE_TIMER_TICK_MS = 100

export function responseTimestamp(): number {
  return performance.now()
}

export function formatResponseDuration(durationMs: number): string {
  return `${(Math.max(0, durationMs) / 1_000).toFixed(1)}s`
}
