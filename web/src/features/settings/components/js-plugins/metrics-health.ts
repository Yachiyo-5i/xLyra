export type MetricsHealthLevel = 'none' | 'ok' | 'warn' | 'bad'

// none: no calls. ok: no failures. warn: failures, but successes are at least as many.
// bad: more failures than successes.
export function metricsHealthLevel(calls: number, errors: number): MetricsHealthLevel {
  if (calls <= 0) return 'none'
  if (errors <= 0) return 'ok'
  return calls - errors >= errors ? 'warn' : 'bad'
}
