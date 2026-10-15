export const meta = { apiVersion: 1, id: "xlyra.protocol.typesafe-systemone", kind: "protocol" };

export function decodeRequest(_ctx, payload) {
  const model = String(payload.model ?? "").trim();
  if (!model) return { error: { status: 400, code: "invalid_model", message: "model is required" } };
  return { model };
}

export function buildRequest(ctx, payload) {
  const upstream = ctx.candidate.upstreamName;
  return {
    path: "/v1/systemone",
    headers: {},
    payload: { ...payload, model: upstream },
  };
}

export function parseResponse(_ctx, resp) {
  const u = obj(resp.json)?.usage ?? {};
  return {
    passthrough: true,
    usage: {
      prompt_tokens: num(u.input_tokens) ?? 0,
      completion_tokens: num(u.output_tokens) ?? 0,
    },
  };
}
