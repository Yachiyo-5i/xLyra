import type { ReactNode } from 'react'
import { Badge } from '@/components/ui/badge'
import { QuotaEntries } from '@/features/sites/components/quota-entries'
import { accountQuotaEntries, legacyGrokQuotaEntries } from '@/features/sites/lib/site-utils'
import type { SiteAccount } from '@/features/sites/api/sites'

export type AccountRowExtras = {
  pills?: ReactNode
  detail?: ReactNode
}

export function grokRowView(account: SiteAccount): AccountRowExtras {
  const tier = 'tier' in account ? account.tier : undefined
  const quotaEntries = accountQuotaEntries(account.entries, legacyGrokQuotaEntries('quota_known' in account ? account : {}))
  return {
    pills: tier ? <Badge variant="neutral">{tier}</Badge> : undefined,
    detail: quotaEntries.length ? <QuotaEntries entries={quotaEntries} /> : undefined,
  }
}
