export const meta = { apiVersion: 1, id: "xlyra.quota.kimi", kind: "quota_probe" };

function planName(level, region) {
  level = str(level).toUpperCase();
  if (!level) return "";
  region = str(region).toUpperCase();
  switch (level) {
    case "LEVEL_BASIC":
      return region === "REGION_CN" || region === "CN" || region === "CHINA" ? "Andante" : "Moderato";
    case "LEVEL_STANDARD":
      return "Moderato";
    case "LEVEL_INTERMEDIATE":
      return "Allegretto";
    case "LEVEL_ADVANCED":
      return "Allegro";
    case "LEVEL_PREMIUM":
      return "Vivace";
    default: {
      let name = level.replace(/^LEVEL_/, "").split("_").join(" ");
      if (!name) return "";
      return name.charAt(0).toUpperCase() + name.slice(1).toLowerCase();
    }
  }
}

function fiveHour(window) {
  const duration = num(window && window.duration);
  if (duration == null) return false;
  const timeUnit = str(window.timeUnit).toUpperCase();
  if (timeUnit.includes("MINUTE")) return duration === 300;
  if (timeUnit.includes("HOUR")) return duration === 5;
  if (timeUnit.includes("SECOND")) return duration === 18000;
  return false;
}

function detailEntry(label, raw) {
  const detail = obj(raw);
  if (!detail) return null;
  const entry = { label, unit: "percent" };
  const limit = num(detail.limit);
  const used = num(detail.used);
  let remaining = num(detail.remaining);
  if (remaining == null && limit != null && used != null) remaining = Math.max(0, limit - used);
  if (limit != null) entry.limit = limit;
  if (used != null) entry.used = used;
  if (remaining != null) entry.remaining = remaining;
  const reset = str(detail.resetTime);
  if (reset) entry.resetAt = reset;
  if (entry.remaining == null && entry.limit == null) return null;
  return entry;
}

export function probe(_ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/usages" } };
  const resp = steps[0].response;
  const failure = failed(resp);
  if (failure) return { error: failure };
  const payload = resp.json;
  const user = obj(payload.user) || {};
  const membership = obj(user.membership) || {};
  const plan = planName(membership.level, user.region);
  const entries = [];
  for (const item of arr(payload.limits)) {
    const row = obj(item);
    if (!row || !fiveHour(obj(row.window))) continue;
    const entry = detailEntry("five_hour", row.detail);
    if (entry) entries.push(entry);
  }
  const weekly = detailEntry("weekly", payload.usage);
  if (weekly) entries.push(weekly);
  if (!entries.length) return { error: "usages endpoint did not contain quota data" };
  return { result: { kind: "token_plan", plan, entries } };
}
