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
