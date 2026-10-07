export const meta = { apiVersion: 1, id: "xlyra.quota.moonshot", kind: "quota_probe" };

export function probe(_ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/users/me/balance" } };
  const resp = steps[0].response;
  const failure = failed(resp);
  if (failure) return { error: failure };
  const payload = resp.json;
  const code = num(payload.code);
  if (code != null && code !== 0) return { error: "balance endpoint returned code " + code };
  const data = obj(payload.data) || {};
  const remaining = num(data.available_balance);
  if (remaining == null) return { error: "balance endpoint did not contain balance data" };
  const entry = { label: "balance", unit: "cny", remaining };
  const cash = num(data.cash_balance);
  const voucher = num(data.voucher_balance);
  if (cash != null) entry.cashBalance = cash;
  if (voucher != null) entry.voucherBalance = voucher;
  return { result: { kind: "balance", isAvailable: remaining > 0, entries: [entry] } };
}
