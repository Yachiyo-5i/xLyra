export const meta = { apiVersion: 1, id: "xlyra.quota.sub2api", kind: "quota_probe" };

const windows = [
  ["daily", "daily_usage_usd", "daily_limit_usd"],
  ["weekly", "weekly_usage_usd", "weekly_limit_usd"],
  ["monthly", "monthly_usage_usd", "monthly_limit_usd"],
];

export function probe(_ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/usage" } };
  const resp = steps[0].response;
  const failure = failed(resp);
  if (failure) return { error: failure };
  const payload = resp.json;
  if (payload.isValid === false) return { error: "usage endpoint reported invalid API key" };
  const entries = [];
  const plan = str(payload.planName);
  let expiresAt = "";
  let planWindows = false;
  const quota = obj(payload.quota);
  if (quota) {
    const entry = { label: "balance", unit: unit(quota.unit, "usd") };
    const limit = num(quota.limit);
    const used = num(quota.used);
    const remaining = num(quota.remaining);
    if (limit != null) entry.limit = limit;
    if (used != null) entry.used = used;
    if (remaining != null) entry.remaining = remaining;
    if (entry.remaining != null || entry.limit != null) entries.push(entry);
  } else {
    const remaining = num(payload.remaining);
    if (remaining != null) entries.push({ label: "balance", unit: "usd", remaining });
  }
  const subscription = obj(payload.subscription);
  if (subscription) {
    expiresAt = str(subscription.expires_at);
    for (const [label, usedKey, limitKey] of windows) {
      const window = obj(subscription[label]);
      const percentage = window ? num(window.percentage) : null;
      if (percentage != null) {
        const entry = { label, unit: "percent", limit: 100, used: percentage, remaining: Math.max(0, 100 - percentage) };
        const reset = str(window.resets_at);
        if (reset) entry.resetAt = reset;
        entries.push(entry);
        planWindows = true;
        continue;
      }
      const limit = num(subscription[limitKey]);
      const used = num(subscription[usedKey]);
      if (limit == null || limit <= 0 || used == null) continue;
      entries.push({ label, unit: "usd", limit, used, remaining: limit - used });
    }
  }
  for (const raw of arr(payload.rate_limits)) {
    const window = obj(raw);
    if (!window) continue;
    const label = str(window.window);
    if (!label) continue;
    const limit = num(window.limit);
    if (limit == null) continue;
    const entry = { label, unit: "requests", limit };
    const used = num(window.used);
    const remaining = num(window.remaining);
    if (used != null) entry.used = used;
    if (remaining != null) entry.remaining = remaining;
    entries.push(entry);
  }
  let kind = "balance";
  if (planWindows || plan) kind = "subscription_plan";
  else if (entries.length > 1) kind = "mixed";
  const result = { kind, plan, entries };
  if (expiresAt) result.expiresAt = expiresAt;
  return { result };
}
