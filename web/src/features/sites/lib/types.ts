import type { SiteValidationResult } from '@/features/sites/api/sites'

export type ValidationSnapshot = SiteValidationResult & { executedAt: string }
export type SiteUpdatedAtDisplayMode = 'relative' | 'absolute'
