# 站点识别插件（site_detect）


让 xLyra 能根据一个地址判断"这是不是某类站点"。**这个类型不带任何凭据**，只能访问站点公开的接口。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "site_detect" };
export function detect(ctx, steps) { ... }
```

## ctx

只有两个字段：

| 字段 | 说明 |
| --- | --- |
| `baseURL` | 要识别的站点地址 |
| `now` | 当前时间，毫秒时间戳 |

没有 `siteType`（正是要识别的东西），也没有 `credentialType`。

## 调用方式

分步方式与[额度探测](./quota-probe.md)相同，最多 6 次，每次 200 ms。区别是：**发出的请求不带 `Authorization` 头**。

## 清单

```json
"site": { "baseURLMode": "as_is" }
```

## result

```ts
{ matched: true, siteType: "newapi", confidence: 0.95, features: { version: "1.2.3" } }
```

| 字段 | 说明 |
| --- | --- |
| `matched` | 必填，布尔值 |
| `siteType` | 识别成功时必填：这个站点属于 xLyra 的哪一种站点类型，如 `newapi`。**必须是 xLyra 已经支持的类型**，插件不能创造新类型，否则这个结果会被忽略 |
| `confidence` | 可选，0 到 1 之间的数字，超出范围会被拒绝 |
| `features` | 可选，对象，最多 32 个键，整体不超过 8 KiB，用来带出版本等信息 |

没认出来也要返回 `{ matched: false }`，不要返回 `error`。`error` 只用于插件自己出了问题的情形。

## xLyra 怎么使用它

启用后，对所有站点生效：管理员在新建站点时点"识别站点类型"，xLyra 会同时运行内置的识别和所有已启用的 `site_detect` 插件，取**置信度最高**的结果；插件只有在置信度严格高于内置结果时才会胜出。没写 `confidence` 时按 0.5 计算。

- 返回的 `siteType` 不是 xLyra 支持的类型，或插件出错，这个结果会被忽略，不影响其他识别；
- 识别用的请求不带密钥，只访问你填写的这个站点地址。

## 写法建议

- 优先使用站点公开的状态接口，如 `/api/status`。
- 用多个特征交叉判断，并据此给出 `confidence`，不要只凭一个字段断定。
- 先发一个请求再根据结果决定要不要发第二个，最多 5 个请求。
