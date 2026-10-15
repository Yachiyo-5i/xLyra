# 协议插件（protocol）

协议插件让 xLyra 能对接一种非标准的上游：把下游请求翻译成上游请求，再从上游响应里取出用量。**仅支持非流式请求。**

下游路径固定为 `/v1/plugins/<slug>`，`slug` 由管理员在启用时分配，插件不能自选路径、也不能映射成 `/v1/chat/completions` 之类的标准路径。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "protocol" };
export function decodeRequest(ctx, payload) { ... }
export function buildRequest(ctx, payload) { ... }
export function parseResponse(ctx, resp) { ... }
```

前三个钩子必须导出。另有两个可选钩子 `signRequest`、`parseError`，不导出就按默认行为处理，见后文。

## decodeRequest(ctx, payload)

读出客户端想用的模型名。

- `ctx.downstreamPath`：下游路径。
- `payload`：客户端发来的 JSON 对象。

返回之一：

| 返回 | 含义 |
| --- | --- |
| `{ model: "name" }` | 必须是非空字符串 |
| `{ error: { status, code, message } }` | 拒绝请求。三个字段都必填，`status` 是数字，`code` 和 `message` 是非空字符串 |

返回其他字段会报 `js_shape_error`。

## buildRequest(ctx, payload)

描述发给上游的请求。

- `ctx.candidate`：`siteType`、`baseURL`、`upstreamName`、`upstreamModel`。选中的上游站点用 `upstreamName` 作为上游模型名。
- `payload`：同上。

返回：

| 字段 | 说明 |
| --- | --- |
| `path` | 必填，非空字符串，规则和额度探测的 `path` 一样，会拼到站点地址后面且不能离开该站点 |
| `headers` | 可选，字符串到字符串。会过滤掉 `Authorization`、`Cookie` 等。**与清单 `protocol.auth` 指定的鉴权头同名的头会被忽略**，鉴权头由 xLyra 写入 |
| `payload` | 可选，对象，作为上游请求体，省略时为空对象 |

也可以返回 `{ error: "message" }` 表示无法构造请求。

HTTP 方法不由插件决定，取自清单的 `protocol.method`。

## parseResponse(ctx, resp)

从上游响应里整理出给客户端的结果和用量。

- `ctx` 与 `buildRequest` 相同。
- `resp`：`status`、`headers`、`body`（文本）、`json`（解析失败为 `null`）。响应体最大 4 MiB。

返回对象，字段如下，至少要有 `passthrough`、`body`、`status`、`usage` 之一：

| 字段 | 说明 |
| --- | --- |
| `passthrough` | `true` 表示把上游响应原样转给客户端 |
| `status` | 可选，改写返回给客户端的状态码 |
| `contentType` | 可选，改写 `Content-Type` |
| `body` | 可选，字符串，改写响应体 |
| `usage` | 可选，`{ prompt_tokens, completion_tokens, total_tokens }`，均为可选整数。不给 `total_tokens` 时，用前两者之和 |
| `error` | 非空字符串表示解析失败，请求会按错误处理 |

用量由 xLyra 记录。插件**不能决定价格或计费**，只负责如实报告上游给出的 token 数。

## signRequest(ctx, req)（可选）

上游要求对请求做签名时使用。**插件只决定"签什么"，签名由 xLyra 用站点密钥计算**，密钥不会进入 JS。

- `ctx` 与 `buildRequest` 相同。
- `req`：即将发出的请求，`method`、`url`（含查询串）、`headers`（不含鉴权和 Cookie）、`body`（实际发出的请求体文本，最大 1 MiB）。
- 调用发生在所有请求头都就位之后，所以签名覆盖的就是真正发出的内容。

返回：

| 字段 | 说明 |
| --- | --- |
| `stringToSign` | 必填，要签名的文本，最大 64 KiB |
| `algorithm` | 必填，`hmac-sha256`、`hmac-sha1`、`hmac-sha512` 之一 |
| `encoding` | 可选，`hex`（默认）或 `base64` |
| `header` | 必填，写入签名的请求头，例如 `Authorization`、`X-Signature`。不能是 `Host`、`Cookie`、`Content-Length` 等 |
| `prefix` | 可选，写在签名前面的文字，例如 `HMAC-SHA256 ` |
| `headers` | 可选，要一起带上的非机密头，例如时间戳、随机数。不能与 `header` 同名 |

也可以返回 `{ error: "message" }`。

规则：

- 导出 `signRequest` 的插件，清单里必须写 `"auth": "none"`。这样站点密钥只会用于签名，不会同时以明文放进请求头。
- 签名失败（钩子报错、返回格式不对、请求体超过 1 MiB）时，这次上游调用以 `upstream_sign_failed` 失败，不会不带签名就发出去。
- 管理员配置的自定义请求头先写入，签名头最后写入，所以签名头不会被覆盖。
- 密钥整个作为 HMAC 的密钥使用。需要把密钥拆成多段（如 AK/SK）的场景目前不支持。

```ts
export function signRequest(ctx, req) {
  const stamp = String(Math.floor(Date.now() / 1000));
  return {
    stringToSign: [req.method, req.url, stamp, req.body].join("\n"),
    algorithm: "hmac-sha256",
    header: "X-Signature",
    headers: { "X-Timestamp": stamp },
  };
}
```

## parseError(ctx, resp)（可选）

上游返回非 2xx 时，`parseResponse` 不会被调用，默认原样把错误转给客户端。导出 `parseError` 后，可以改写错误响应体，让客户端看到统一的错误格式。

- `resp` 与 `parseResponse` 的相同：`status`、`headers`、`body`、`json`。
- 返回 `{ body, contentType? }`。`body` 必填，是字符串。

规则：

- **状态码保持上游给的值**，不能通过这个钩子改。重试、冷却、熔断等判断仍按真实状态码走。要影响冷却分类，请用 [`error_classifier`](./error-classifier.md)。
- 钩子报错或返回格式不对时，客户端仍看到上游原始响应，不会因此多出一个错误。
- 超过 4 MiB 的错误响应不会调用该钩子。

## 清单里的协议部分

```json
"protocol": {
  "name": "acme_proto",
  "downstreamPath": "/v1/plugins/acme_proto",
  "endpointType": "acme_proto",
  "method": "POST",
  "auth": "bearer"
}
```

`auth` 取值：`bearer`（`Authorization: Bearer <密钥>`）、`none`、或 `header:<Header-Name>`（密钥写入指定请求头）。详见[清单](./manifest.md)。

## 单次调用的时限

协议钩子在请求路径上，超时只有 **50 ms**，比探测插件的 200 ms 更短。不要在这里做重计算。

## 样本

协议插件的 fixtures 格式见 [fixtures](./fixtures.md)。注意：`expect.request`、`expect.parse` 的比对范围很窄，`signRequest` 和 `parseError` 则按字段逐项比对。
