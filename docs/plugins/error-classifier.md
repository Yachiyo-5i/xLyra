# 错误分类插件（error_classifier）


用于上游的错误格式特殊、xLyra 默认归错类的情形，比如限流被当成了普通错误，或者该冷却的没有冷却。

你只负责**判断这是哪一类失败**。选路、冷却多久、是否重试都仍由 xLyra 决定。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "error_classifier" };
export function classify(ctx, input) { ... }
```

这是**一次性函数**：不发请求，一次调用得出结论。因为在请求路径上，时限只有 **50 ms**。

## 输入

`ctx`：

| 字段 | 说明 |
| --- | --- |
| `siteType` | 站点类型字符串 |

`input` 只包含上游已经返回的错误信息：

| 字段 | 说明 |
| --- | --- |
| `status` | HTTP 状态码 |
| `code` | 可选，上游响应体里的错误码 |
| `type` | 可选，上游响应体里的错误类型 |
| `message` | 可选，错误信息，被截到 1024 字节 |

不会提供完整响应体、响应头或任何请求内容。

## 返回

```ts
{ class: "subscription_limit", reason: "quota used up" }
```

| 字段 | 说明 |
| --- | --- |
| `class` | 必填，只能是下表的值之一 |
| `reason` | 可选，写给日志的简短说明，不超过 200 字节 |

| class | 含义 |
| --- | --- |
| `unknown` | 无法判断，按 xLyra 的默认规则处理 |
| `limited` | 被限流，稍后可重试 |
| `subscription_limit` | 订阅或额度用尽，需要等到重置 |
| `transient` | 临时性故障（上游抖动、5xx） |
| `credential_invalid` | 密钥无效 |

**除 `class` 和 `reason` 以外的任何字段都会被拒绝**，比如试图返回 `cooldownSeconds` 会得到 `js_shape_error`。这是有意的：插件不能决定冷却时长。

## xLyra 怎么使用它

管理员把插件绑定到一个站点后，该站点的上游错误在 xLyra 内置分类之后，再交给插件**校正一次**：

1. xLyra 先用内置规则读出错误码、类型和信息，并给出默认类别；
2. 插件看到这些信息，返回自己的类别；
3. 返回 `unknown`，或插件出错、超时、返回格式不对，**保持默认结果**；
4. 否则采用插件的类别，之后冷却多久、是否切换密钥都由 xLyra 按这个类别决定。

几点说明：

- 类别和后果：`limited` 按限流冷却，`subscription_limit` 冷却到额度重置，`credential_invalid` 冷却该密钥；`transient` 和 `unknown` 不触发冷却，请求按默认方式切换重试。所以把一个被误判成限流的错误改成 `transient`，就是告诉 xLyra 别冷却这把密钥。
- 站点本身有专用的错误处理（如 Codex、Antigravity、OpenCode Go）时，这些专用处理优先，插件不介入。
- 插件连续出错太多会被自动停用，规则见[限制与安全](./limits-and-security.md)。

## fixtures

```json
{
  "name": "quota used up",
  "ctx": { "siteType": "custom" },
  "input": { "status": 429, "code": "insufficient_quota" },
  "expect": { "result": { "class": "subscription_limit" } }
}
```

`input` 必填，并且和钩子返回值一样按严格规则检查（不认识的字段会报错）。`expect.result` 只检查你写出的字段。要验证插件应当报错的情形，写 `expect.error`，填错误信息里应包含的文字。

建议为每一种你关心的错误格式写一个样本，包括"普通 429"和"额度用尽的 429"。
