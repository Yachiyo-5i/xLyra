export const ACTIVITY_BUCKET_MS = 60_000
export const ACTIVITY_BUCKET_COUNT = 20

export type ActivityBucket = {
  at: number
  count: number
}

export function emptyActivityBuckets(now = Date.now()): ActivityBucket[] {
  const current = Math.floor(now / ACTIVITY_BUCKET_MS) * ACTIVITY_BUCKET_MS
  return Array.from({ length: ACTIVITY_BUCKET_COUNT }, (_, index) => ({
    at: current - (ACTIVITY_BUCKET_COUNT - 1 - index) * ACTIVITY_BUCKET_MS,
    count: 0,
  }))
}

export function syncActivityBuckets(buckets: ActivityBucket[], now = Date.now()): ActivityBucket[] {
  return emptyActivityBuckets(now).map((bucket) => buckets.find((item) => item.at === bucket.at) ?? bucket)
}

export function bumpActivityBucket(buckets: ActivityBucket[], now = Date.now()): ActivityBucket[] {
  const next = syncActivityBuckets(buckets, now)
  const last = next[next.length - 1]
  next[next.length - 1] = { ...last, count: last.count + 1 }
  return next
}
