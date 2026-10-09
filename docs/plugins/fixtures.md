# fixtures 测试样本

fixtures 是放在 `fixtures/*.json` 里的录制样本：给定上游的响应，插件应该产出什么。它们会进入 `.xlp`，**管理员上传时服务端会再跑一遍**，所以本地 `xlyra-plugin test` 通过，上传时也会通过。

## 额度探测样本

```json
{
  "name": "newapi billing fallback",
  "ctx": { "siteType": "newapi", "baseURL": "https://relay.example", "credentialType": "api_key", "now": 1791331200000 },
  "responses": [
    { "match": { "method": "GET", "path": "/api/usage/token" }, "status": 404, "body": "not found" },
    { "match": { "path": "/v1/dashboard/billing/subscription" }, "status": 200, "json": { "hard_limit_usd": 20 } },
    { "match": { "pathPrefix": "/v1/dashboard/billing/usage" }, "status": 200, "json": { "total_usage": 750 } }
  ],
  "expect": {
    "requests": [ { "path": "/api/usage/token" }, { "path": "/v1/dashboard/billing/subscription" }, { "pathPrefix": "/v1/dashboard/billing/usage" } ],
    "result": { "kind": "balance", "entries": [ { "label": "balance", "unit": "usd", "limit": 20, "used": 7.5, "remaining": 12.5 } ] }
  }
}
```

### 字段

| 字段 | 说明 |
| --- | --- |
| `name` | 样本名，出现在报错里 |
| `ctx` | 传给 `probe` 的 `ctx`，`now` 固定后样本结果才稳定 |
| `responses` | 模拟的上游响应列表 |
| `expect` | 期望 |

### responses 的匹配

插件每发出一个请求，就从 `responses` 里按顺序找**第一个还没被用过**的、`match` 符合的项，每项只能用一次。

- `match.method`：可选，不区分大小写。
- `match.path`：完全相等。
- `match.pathPrefix`：前缀匹配。
- `path` 和 `pathPrefix` **至少要写一个**，否则永远匹配不上。
- 找不到匹配的响应，样本失败，报 `unexpected request`。
- 响应内容：`status`，以及 `json`（对象）或 `body`（文本）之一；只给 `json` 时 `body` 自动生成，只给 `body` 且内容是 JSON 时 `json` 自动解析。`error` 可用来模拟网络层失败。样本里的响应没有响应头。

### expect

| 字段 | 说明 |
| --- | --- |
| `requests` | 期望插件**依次**发出的请求，用 `method` / `path` / `pathPrefix` 描述，**数量必须相等** |
| `result` | 期望的最终结果 |
| `error` | 期望插件返回这个 `error` 字符串（与 `result` 二选一） |

`result` 的比较规则：

- **只检查你写出来的字段**，没写的字段不管。所以想验证某字段"不存在"是做不到的。
- 数组要求长度一致、逐项比较。
- 数字允许 `1e-9` 的误差。
- `entries` 里没有值的字段不会出现在实际结果里，所以不要在期望里写 `null`。

## 其他分步类型的样本

`model_list`、`credential_check`、`site_detect` 的样本格式与额度探测完全相同（`ctx`、`responses`、`expect.requests`、`expect.result`），区别只在 `expect.result` 写的是各自的结果结构。`site_detect` 的 `ctx` 只需要 `baseURL` 和 `now`，`expect.result` 里要写 `siteType`。

## 一次性函数类型的样本

`error_classifier`、`model_metadata`、`pricing_parse` 不发请求，样本里没有 `responses`：

```json
{
  "name": "plain rate limit",
  "ctx": { "siteType": "custom" },
  "input": { "status": 429, "code": "rate_limit_exceeded" },
  "expect": { "result": { "class": "limited" } }
}
```

| 字段 | 说明 |
| --- | --- |
| `ctx.siteType` | 传给函数的站点类型 |
| `input` | 必填。`error_classifier` 和 `model_metadata` 的 `input` 与 xLyra 实际传入的结构一致，**按严格规则检查**，不认识的字段会报错；`pricing_parse` 的 `input` 是站点价格数据，不限制结构 |
| `expect.result` | 期望的结果，只检查你写出的字段 |
| `expect.error` | 期望调用失败，值是错误信息里应包含的文字（与 `result` 二选一） |

## 协议插件样本

```json
{
  "name": "passthrough",
  "payload": { "model": "my-model", "prompt": "hi" },
  "candidate": { "siteType": "custom", "baseURL": "https://relay.example", "upstreamName": "upstream-model", "upstreamModel": "upstream-model" },
  "response": { "status": 200, "json": { "usage": { "input_tokens": 10, "output_tokens": 5 } } },
  "expect": {
    "decode": { "model": "my-model" },
    "request": { "path": "/v1/generate", "payload": { "model": "upstream-model" } },
    "parse": { "passthrough": true, "usage": { "prompt_tokens": 10, "completion_tokens": 5 } }
  }
}
```

流程：先用 `payload` 调 `decodeRequest`，再用 `candidate` 调 `buildRequest`，如果有 `response.status`（非 0）再调 `parseResponse`（非 2xx 时改调 `parseError`）。如果插件导出了 `signRequest`，在 `buildRequest` 之后会调用它。

| 字段 | 说明 |
| --- | --- |
| `payload` | 必填，客户端请求体 |
| `candidate` | 选中的上游信息 |
| `response` | 模拟的上游响应；省略则不测 `parseResponse` |
| `expect.error` | 期望 `decodeRequest` 返回的错误 `message` |
| `expect.decode.model` | 期望解析出的模型名 |
| `expect.request` | 期望的上游请求 |
| `expect.parse` | 期望的解析结果 |
| `expect.sign` | 插件导出了 `signRequest` 时，期望的签名结果 |
| `expect.parseError` | `response.status` 为非 2xx 且插件导出了 `parseError` 时，期望的错误改写结果 |

`expect.sign` 可以写 `stringToSign`、`algorithm`、`encoding`、`header`、`prefix`、`headers` 中的任意几项，写了就逐项精确比对，写了不认识的字段会报错。样本里请求体按键名排序的 JSON 给出，和 xLyra 实际发送的一致；请求头是 `buildRequest` 返回的头再加 `Content-Type: application/json`。样本里没写 `expect.sign` 时，`signRequest` 仍会被调用，只检查返回格式是否合法。

`expect.parseError` 可以写 `body`、`contentType`。插件没有导出对应钩子却写了 `expect.sign` 或 `expect.parseError`，样本会失败，避免期望被悄悄忽略。

**目前协议样本的比对范围很窄**，只检查这几项，其余字段写了也不会被验证：

- `expect.request.path`
- `expect.request.payload.model`
- `expect.parse.passthrough`
- `expect.parse.usage.prompt_tokens` 和 `completion_tokens`

所以请自己在样本里多写几组输入，覆盖边界情况。

## 提示

- 每个样本文件一个场景，文件名要能说明场景，如 `weekly-only.json`、`error-code.json`。
- 把线上真实的响应脱敏后录成样本，是最可靠的回归手段。
- 至少要有一个样本，否则不能打包。

## 自动化样本

`automation` 的样本没有 `responses`：`ctx` 提供 `now`、`config`（绑定参数）和 `state`（上次保存的状态），`input` 是事件，`expect.result` 是期望的 `{ actions, state }`，只比对写出的字段。详见[自动化插件](./automation.md#写-fixtures)。
