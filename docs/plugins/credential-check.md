# 密钥校验插件（credential_check）


用于默认的密钥校验或健康检查不适用于某个站点的情形：你决定请求哪个接口、怎么判断密钥是否有效。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "credential_check" };
export function check(ctx, steps) { ... }
```

## 调用方式

与[额度探测](./quota-probe.md)相同的分步方式：`ctx` 没有密钥，返回 `{ request }`、`{ result }`、`{ error }` 之一，最多 6 次，每次 200 ms。

## 清单

```json
"site": { "baseURLMode": "as_is" }
```

## result

```ts
{ status: "invalid", message: "rejected with 401" }
```

| 字段 | 说明 |
| --- | --- |
| `status` | 必填，只能是下面三个值之一 |
| `message` | 可选，给管理员看的说明 |

| status | 含义 |
| --- | --- |
| `ok` | 密钥可用 |
| `invalid` | 站点明确拒绝了这把密钥（如 401、403） |
| `unavailable` | 站点没能给出答案（超时、5xx 等），**无法判断**密钥是否有效 |

请把"站点出问题"和"密钥有问题"分开：站点暂时不可用时返回 `unavailable`，不要返回 `invalid`，否则可能让一把好密钥被误判为失效。

## 与 `{ error }` 的区别

`{ error: "..." }` 表示**插件自己无法完成判断**（比如响应格式完全意料之外）。已经得出判断时，用 `result.status`。

## xLyra 怎么使用它

管理员把插件绑定到一个站点后，下面两处改用它判断密钥：

- 添加或校验密钥时的"验证";
- 站点的**健康检查**（使用该站点第一把可用的密钥）。

三种结果在 xLyra 里的含义：

| status | 校验 | 健康检查 |
| --- | --- | --- |
| `ok` | 通过 | 健康 |
| `invalid` | 失败，并按"密钥无效"处理 | 失败（`validation_failed`） |
| `unavailable` | 失败，但按"临时故障"处理，**不会**把密钥当作无效 | 失败（`upstream_unreachable`） |

## fixtures

`expect.result` 写 `{ "status": "ok" }` 这样的期望。建议至少写三个样本：`ok`、`invalid`、`unavailable`。
