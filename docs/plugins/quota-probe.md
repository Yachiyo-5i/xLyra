# 额度探测插件（quota_probe）

额度探测插件告诉 xLyra：去站点的哪个接口查余额，以及怎么把返回内容整理成统一的结构。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "quota_probe" };
export function probe(ctx, steps) { ... }
```

`meta` 的 `id`、`kind`、`apiVersion` 必须和 `manifest.json` 一致，否则加载失败。

## 调用方式

xLyra 反复调用 `probe(ctx, steps)`，每次只能返回下面三者之一，返回多个或一个都没有会报 `js_shape_error`：

| 返回 | 含义 |
| --- | --- |
| `{ request }` | 请求 xLyra 发一个 HTTP 请求，响应会出现在下一次调用的 `steps` 里 |
| `{ result }` | 完成，返回额度结果 |
| `{ error: "message" }` | 失败，`message` 必须是非空字符串 |

最多调用 6 次，也就是最多发 5 个 HTTP 请求，超过则判失败。

### ctx

| 字段 | 说明 |
| --- | --- |
| `siteType` | 站点类型字符串 |
| `baseURL` | 已规整后的站点地址，不含尾部 `/`。规整方式由清单的 `quotaProbe.baseURLMode` 决定 |
| `credentialType` | 凭据类型，如 `api_key` |
| `now` | 当前时间，毫秒时间戳 |

`ctx` 里没有任何密钥。

### steps

`steps` 是已完成的请求列表，每项是 `{ request, response }`：

| response 字段 | 说明 |
| --- | --- |
| `status` | HTTP 状态码。网络层失败（连不上、超时）时为 `0`，同时 `error` 里有原因 |
| `headers` | 响应头（已去掉 `Set-Cookie`），同名多值用 `, ` 连接 |
| `body` | 原始文本 |
| `json` | `body` 解析后的值，解析失败为 `null` |
| `error` | 网络层失败时的原因，否则为空 |

第一次调用时 `steps` 是空数组。**非 2xx 状态不会自动变成错误**，由你决定怎么处理，这样才能做"这个接口 404 就换另一个接口"的回退。

## request

| 字段 | 说明 |
| --- | --- |
| `method` | `GET`、`POST`、`PUT`、`PATCH`、`DELETE`，默认 `GET` |
| `path` | 必填，必须以单个 `/` 开头，拼在站点地址后面，规则见[限制](./limits-and-security.md) |
| `headers` | 可选，字符串到字符串。`Authorization`、`Cookie` 等会被丢弃，xLyra 会自己写入鉴权头 |
| `query` | 可选，字符串到字符串。`path` 里也可以带 `?a=b` |
| `body` | 可选，对象、数组或字符串。GET/HEAD 请求不发送。对象和数组按 JSON 发送，默认加 `Content-Type: application/json` |

出现不认识的字段会报 `js_shape_error`。

## result

| 字段 | 说明 |
| --- | --- |
| `kind` | 必填，非空字符串，如 `balance`、`token_plan` |
| `plan` | 可选，套餐名 |
| `expiresAt` | 可选，到期时间字符串 |
| `isAvailable` | 可选，布尔值 |
| `entries` | 必填，数组。**为空会被当成失败** |

### entries 的每一项

| 字段 | 说明 |
| --- | --- |
| `label` | 必填，非空字符串，如 `balance`、`five_hour`、`weekly` |
| `unit` | 可选，如 `usd`、`percent` |
| `remaining`、`limit`、`used` | 可选，数字 |
| `unlimited` | 可选，布尔值 |
| `resetAt` | 可选，重置时间字符串 |
| `cashBalance`、`voucherBalance`、`grantedBalance`、`toppedUpBalance` | 可选，数字，余额拆分用 |

数字字段必须是数字类型，不能是数字字符串；上游返回字符串数字时，请自己用 `Number()` 转换并检查 `Number.isFinite`。

## 多步回退的写法

```ts
// done() 是你自己写的辅助函数：把上游 JSON 整理成 result 或 error
export function probe(_ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/usage/token" } };
  if (steps.length === 1) {
    if (steps[0].response.status === 200) return done(steps[0].response.json);
    // 第一个接口不行，换备用接口
    return { request: { method: "GET", path: "/v1/dashboard/billing/subscription" } };
  }
  return done(steps[1].response.json);
}
```

用 `steps.length` 判断走到了第几步，用 `steps[i].request.path` 区分是哪一步的响应。

## 谁来调用它

- 管理员把这个插件绑定到某个站点后，该站点的"刷新额度"就会走它。
- 站点编辑页里"额度探测"的下拉框**不会**出现第三方插件，绑定由管理员通过后台完成，见[发布与安装](./publishing.md)。
- 第三方探测插件**不能触发冷却**，冷却只由 xLyra 内置探测触发。
