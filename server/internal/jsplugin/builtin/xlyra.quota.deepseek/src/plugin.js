export const meta = { apiVersion: 1, id: "xlyra.quota.deepseek", kind: "quota_probe" };

export function probe(_ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/user/balance" } };
  const resp = steps[0].response;
  const failure = failed(resp);
  if (failure) return { error: failure };
  const payload = resp.json;
  const entries = [];
  for (const raw of arr(payload.balance_infos)) {
    const item = obj(raw);
    if (!item) continue;
    const remaining = num(item.total_balance);
    if (remaining == null) continue;
    const entry = { label: "balance", unit: unit(item.currency, "cny"), remaining };
    const granted = num(item.granted_balance);
    const topped = num(item.topped_up_balance);
    if (granted != null) entry.grantedBalance = granted;
    if (topped != null) entry.toppedUpBalance = topped;
    entries.push(entry);
  }
  if (!entries.length) return { error: "balance endpoint did not contain balance data" };
  const result = { kind: "balance", entries };
  if (typeof payload.is_available === "boolean") result.isAvailable = payload.is_available;
  return { result };
}
