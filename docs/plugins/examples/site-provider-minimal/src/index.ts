import type {
  ErrorClassifyContext,
  ErrorClassifyInput,
  ErrorClassifyResult,
  ModelListDecision,
  PluginMeta,
  ProbeContext,
  ProbeStep,
  RewritePayloadContext,
} from "../types/xlyra";

export const meta: PluginMeta = { apiVersion: 1, id: "example.minimal", kind: "site_provider" };

export function listModels(_ctx: ProbeContext, steps: ProbeStep[]): ModelListDecision {
  if (steps.length === 0) {
    return { request: { method: "GET", path: "/api/models" } };
  }
  const body = steps[0].response.json as { items?: unknown } | null;
  const items = body?.items;
  if (!Array.isArray(items)) {
    return { error: "models response has no items array" };
  }
  const models: { name: string; displayName?: string }[] = [];
  for (const raw of items) {
    if (typeof raw !== "object" || raw == null) {
      return { error: "models response item is not an object" };
    }
    const item = raw as { model_id?: unknown; title?: unknown };
    if (typeof item.model_id !== "string" || item.model_id === "") {
      return { error: "models response item has no model_id" };
    }
    const entry: { name: string; displayName?: string } = { name: item.model_id };
    if (typeof item.title === "string") {
      entry.displayName = item.title;
    }
    models.push(entry);
  }
  return { result: { models } };
}

export function classify(_ctx: ErrorClassifyContext, input: ErrorClassifyInput): ErrorClassifyResult {
  if (input.code === "E429") {
    return { class: "limited", reason: input.message || "E429" };
  }
  return { class: "unknown" };
}

export function rewritePayload(_ctx: RewritePayloadContext, payload: Record<string, unknown>): Record<string, unknown> {
  const next: Record<string, unknown> = { ...payload };
  delete next.user;
  return next;
}
