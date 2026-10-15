export const meta = { apiVersion: 1, id: "xlyra.quota.xlyra", kind: "quota_probe" };

export function probe(_ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/user/balance" } };
  const resp = steps[0].response;
  const failure = failed(resp);
  if (failure) return { error: failure };
  const payload = resp.json;
  const unlimited = payload.quota_unlimited === true;
  const entry = { label: "balance", unit: unit(payload.unit, "usd") };
  const primary = unlimited ? num(payload.quota_used) : num(payload.quota_total_used);
  const fallback = unlimited ? num(payload.quota_total_used) : num(payload.quota_used);
  const used = primary != null ? primary : fallback;
  if (used != null) entry.used = used;
  if (unlimited) {
    entry.unlimited = true;
    return { result: { kind: "unlimited", entries: [entry] } };
  }
  const limit = num(payload.quota_limit);
  const remaining = num(payload.balance);
  if (limit != null) entry.limit = limit;
  if (remaining != null) entry.remaining = remaining;
  if (entry.remaining == null && entry.limit == null) return { error: "balance endpoint did not return usable quota fields" };
  return { result: { kind: "balance", entries: [entry] } };
}
