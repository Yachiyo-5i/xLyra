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
