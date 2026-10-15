export const meta = { apiVersion: 1, id: "xlyra.quota.newapi", kind: "quota_probe" };

function utcDate(now, dayOffset) {
  const date = new Date(now + dayOffset * 86400000);
  const month = String(date.getUTCMonth() + 1).padStart(2, "0");
  const day = String(date.getUTCDate()).padStart(2, "0");
  return date.getUTCFullYear() + "-" + month + "-" + day;
}

function parseToken(resp) {
  const failure = failed(resp);
  if (failure) return null;
  const data = obj(resp.json && resp.json.data);
  if (!data) return { error: "token usage endpoint did not return data" };
  const entry = { label: "balance", unit: "usd" };
  if (data.unlimited_quota === true) {
    entry.unlimited = true;
    const used = num(data.total_used);
    if (used != null) entry.used = used / 500000;
    return { result: { kind: "unlimited", entries: [entry] } };
  }
  const remaining = num(data.total_available);
  const limit = num(data.total_granted);
  const used = num(data.total_used);
  if (remaining != null) entry.remaining = remaining / 500000;
  if (limit != null) entry.limit = limit / 500000;
  if (used != null) entry.used = used / 500000;
  if (entry.remaining == null && entry.limit == null) return { error: "token usage endpoint did not return usable quota fields" };
  return { result: { kind: "balance", entries: [entry] } };
}

function subscriptionPayload(steps) {
  for (let i = steps.length - 1; i >= 0; i--) {
    const path = steps[i].request.path;
    if (path !== "/v1/dashboard/billing/subscription" && path !== "/dashboard/billing/subscription") continue;
    const response = steps[i].response;
    if (response.status >= 200 && response.status < 300 && response.json) return response.json;
  }
  return null;
}

export function probe(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/usage/token" } };
  const last = steps[steps.length - 1];
  const path = last.request.path;
  if (path === "/api/usage/token") {
    const parsed = parseToken(last.response);
    if (parsed && parsed.result) return parsed;
    return { request: { method: "GET", path: "/v1/dashboard/billing/subscription" } };
  }
  if (path === "/v1/dashboard/billing/subscription" || path === "/dashboard/billing/subscription") {
    if ((last.response.status === 404 || last.response.status === 405) && path === "/v1/dashboard/billing/subscription") {
      return { request: { method: "GET", path: "/dashboard/billing/subscription" } };
    }
    const failure = failed(last.response);
    if (failure) return { error: failure };
    const limit = num(last.response.json.hard_limit_usd);
    if (limit != null && limit >= 100000000) {
      return { result: { kind: "unlimited", entries: [{ label: "balance", unit: "usd", unlimited: true }] } };
    }
    return {
      request: {
        method: "GET",
        path: "/v1/dashboard/billing/usage",
        query: { start_date: utcDate(ctx.now, -99), end_date: utcDate(ctx.now, 1) },
      },
    };
  }
  if (path === "/v1/dashboard/billing/usage" || path === "/dashboard/billing/usage") {
    if ((last.response.status === 404 || last.response.status === 405) && path === "/v1/dashboard/billing/usage") {
      return { request: { method: "GET", path: "/dashboard/billing/usage", query: last.request.query || {} } };
    }
    const failure = failed(last.response);
    if (failure) return { error: failure };
    const subscription = subscriptionPayload(steps) || {};
    const limit = num(subscription.hard_limit_usd);
    const entry = { label: "balance", unit: "usd" };
    if (limit != null) entry.limit = limit;
    const total = num(last.response.json.total_usage);
    if (total != null) {
      entry.used = total / 100;
      if (limit != null) entry.remaining = limit - entry.used;
    }
    if (entry.limit == null && entry.used == null) return { error: "billing endpoints did not return usable quota fields" };
    return { result: { kind: "balance", entries: [entry] } };
  }
  return { error: "unexpected probe step" };
}
