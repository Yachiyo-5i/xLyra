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
export const meta = { apiVersion: 1, id: "xlyra.quota.glm", kind: "quota_probe" };

const unitMinutes = { 1: 1440, 3: 60, 5: 1, 6: 10080 };

function windowLabel(item) {
  const unit = num(item.unit);
  const number = num(item.number);
  if (unit == null || number == null || number <= 0) return "";
  const minutes = unitMinutes[Math.trunc(unit)];
  if (minutes == null) return "";
  switch (minutes * Math.trunc(number)) {
    case 300:
      return "five_hour";
    case 10080:
      return "weekly";
    default:
      return "";
  }
}

function resetAt(value) {
  const ms = num(value);
  if (ms == null) return "";
  const date = new Date(Math.trunc(ms));
  if (Number.isNaN(date.getTime())) return "";
  return date.toISOString().replace(/\.\d{3}Z$/, "Z");
}

function percentEntry(label, used, item) {
  const clamped = clamp(used);
  const entry = { label, unit: "percent", limit: 100, used: clamped, remaining: 100 - clamped };
  const reset = resetAt(item.nextResetTime);
  if (reset) entry.resetAt = reset;
  return entry;
}

function limitEntry(label, item) {
  const usage = num(item.usage);
  const current = num(item.currentValue);
  if (usage != null && usage > 0 && current != null) return percentEntry(label, (current / usage) * 100, item);
  const used = num(item.percentage);
  if (used == null) return null;
  return percentEntry(label, used, item);
}

function planName(level) {
  const text = str(level);
  if (!text) return "";
  return text.charAt(0).toUpperCase() + text.slice(1).toLowerCase();
}

export function probe(_ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/monitor/usage/quota/limit" } };
  const resp = steps[0].response;
  const failure = failed(resp);
  if (failure) return { error: failure };
  const payload = resp.json;
  const code = num(payload.code);
  if (code == null || code !== 200) return { error: "quota limit endpoint returned error: " + str(payload.msg) };
  const data = obj(payload.data);
  if (!data) return { error: "quota limit endpoint did not return data" };
  const entries = [];
  const index = {};
  for (const raw of arr(data.limits)) {
    const item = obj(raw);
    if (!item) continue;
    const type = str(item.type).toUpperCase();
    let entry = null;
    if (type === "TOKENS_LIMIT" || type === "CREDIT_LIMIT") {
      const label = windowLabel(item);
      if (!label) continue;
      entry = limitEntry(label, item);
    } else if (type === "TIME_LIMIT") {
      entry = limitEntry("monthly", item);
    }
    if (!entry) continue;
    const at = index[entry.label];
    if (at != null) {
      if (entry.remaining != null && entry.remaining < entries[at].remaining) entries[at] = entry;
      continue;
    }
    index[entry.label] = entries.length;
    entries.push(entry);
  }
  if (index.five_hour == null && index.weekly == null) {
    return { error: "quota limit endpoint did not contain token quota data" };
  }
  return { result: { kind: "token_plan", plan: planName(data.level), entries } };
}
