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

三个钩子都必须导出。

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

协议插件的 fixtures 格式见 [fixtures](./fixtures.md)。注意：目前协议样本只比对**请求路径、`payload.model`、`passthrough` 和用量的 prompt/completion**，其余字段不会被检查。
