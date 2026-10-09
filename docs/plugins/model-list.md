# 模型列表插件（model_list）


用于站点的模型列表接口格式特殊的情形：你告诉 xLyra 去请求哪个接口，并把返回内容整理成统一的模型列表。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "model_list" };
export function listModels(ctx, steps) { ... }
```

## 调用方式

与[额度探测](./quota-probe.md)完全相同的分步方式：

- `ctx`：`siteType`、`baseURL`、`credentialType`、`now`，没有任何密钥；
- `steps`：已完成的请求与响应；
- 返回 `{ request }`、`{ result }`、`{ error }` 之一，最多调用 6 次，每次 200 ms；
- 请求由 xLyra 发送并加上鉴权头。

## 清单

```json
"site": { "baseURLMode": "trim_v1", "defaultBaseURL": "https://api.example.com" }
```

`baseURLMode` 取值与额度探测相同（`as_is`、`trim_v1`、`trim_v1_fold`、`origin`），不写等于 `as_is`。

## result

```ts
{ models: [{ name: "gpt-x", displayName: "GPT X", capabilities: { vision: true } }] }
```

| 字段 | 说明 |
| --- | --- |
| `models` | 必填，最多 2000 个。可以为空数组，表示站点确实没有模型 |
| `models[].name` | 必填，非空字符串，客户端调用时要使用的上游模型名 |
| `models[].displayName` | 可选 |
| `models[].capabilities` | 可选，对象，最多 64 个键，整体不超过 8 KiB |

出现不认识的字段会报 `js_shape_error`。

## xLyra 怎么使用它

管理员把插件绑定到一个站点后，该站点**同步模型**时（包括刷新模型、刷新单个密钥）改用这个插件拉取列表，取代内置的拉取方式。

- 每把密钥各调用一次，请求由 xLyra 发送并加上这把密钥；
- 插件失败（返回 `error`、格式不对、超时）时，按"这把密钥同步失败"处理，和内置方式失败一样；
- 没有绑定，或绑定的插件已被停用，该站点回到内置方式。

## fixtures

与额度探测相同，`expect.result` 写期望的 `{ models: [...] }`，`expect.requests` 写期望依次发出的请求。例子见 `xlyra-plugin init --kind model_list`。
