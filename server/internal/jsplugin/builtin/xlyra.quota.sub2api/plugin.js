function num(v) {
  if (typeof v === "number") return Number.isFinite(v) ? v : null;
  if (typeof v === "string") {
    const s = v.trim();
    if (!s) return null;
    const m = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?/.exec(s);
    if (!m) return null;
    const n = Number(m[0]);
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

function str(v) {
  return typeof v === "string" ? v.trim() : "";
}

function unit(v, fallback) {
  const s = str(v).toLowerCase();
  return s || fallback;
}

function obj(v) {
  return v && typeof v === "object" && !Array.isArray(v) ? v : null;
}

function arr(v) {
  return Array.isArray(v) ? v : [];
}

function clamp(v) {
  return Math.min(100, Math.max(0, v));
}

function clipUTF8(message, limit) {
  const bytes = new TextEncoder().encode(message);
  const n = Math.min(bytes.length, limit);
  let out = "";
  let i = 0;
  while (i < n) {
    const b = bytes[i];
    let need = 0;
    if (b < 0x80) need = 1;
    else if (b >= 0xc2 && b <= 0xdf) need = 2;
    else if (b >= 0xe0 && b <= 0xef) need = 3;
    else if (b >= 0xf0 && b <= 0xf4) need = 4;
    if (need === 0 || i + need > n) {
      out += "\uFFFD";
      i++;
      continue;
    }
    let ok = true;
    for (let j = 1; j < need; j++) {
      if ((bytes[i + j] & 0xc0) !== 0x80) ok = false;
    }
    if (!ok) {
      out += "\uFFFD";
      i++;
      continue;
    }
    out += new TextDecoder().decode(bytes.slice(i, i + need));
    i += need;
  }
  return out;
}

function httpError(resp) {
  let message = typeof resp.body === "string" ? resp.body.trim() : "";
  message = clipUTF8(message, 256);
  return "HTTP " + resp.status + ": " + message;
}

function failed(resp) {
  if (!resp || resp.status === 0) return resp && resp.error ? resp.error : "request failed";
  if (resp.status < 200 || resp.status >= 300) return httpError(resp);
  if (resp.json == null || typeof resp.json !== "object" || Array.isArray(resp.json)) return "invalid JSON response";
  return "";
}
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
