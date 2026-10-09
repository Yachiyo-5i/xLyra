# 价格解析插件（pricing_parse）


## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "pricing_parse" };
export function parsePricing(ctx, payload) { ... }
```

一次性函数：不发请求，时限 200 ms。

## 清单

价格表从哪里取，由清单声明，必须写：

```json
"site": { "baseURLMode": "as_is", "pricingPath": "/api/pricing" }
```

`pricingPath` 必须是以 `/` 开头的站点路径（规则和请求 `path` 一样，不能离开站点）。

## xLyra 怎么使用它

插件**不发请求**，价格表由 xLyra 取：

1. 管理员启用插件后，在管理后台选择一个站点，点"预览价格"。xLyra 用该站点的 API Key 请求 `pricingPath`，把 JSON 交给插件，把解析结果**只展示、不保存**；
2. 管理员核对无误，勾选确认，再"绑定"。**没有确认就不能绑定**；
3. 绑定后，该站点每次**刷新模型**时都会重新取价格表、解析并保存（沿用 xLyra 原有的价格保存方式）；
4. 没有绑定，或插件被停用，该站点的价格保持原样。

目前只对使用 API Key 的站点生效（走"刷新模型"的站点）；通过账户令牌同步的站点暂不支持。

## 输入

`ctx.siteType`：站点类型。`payload`：站点公布的价格数据，**原样的 JSON**，格式由站点决定，所以你需要自己处理字段缺失和类型问题。

## 返回

```ts
{
  groups: [{ name: "default", ratio: 1 }],
  items: [{ model: "acme-chat-1", group: "default", modelRatio: 2.5, completionRatio: 4 }]
}
```

### groups（可选，最多 200 个）

| 字段 | 说明 |
| --- | --- |
| `name` | 必填 |
| `displayName` | 可选 |
| `ratio` | 可选，数字 |
| `auto` | 可选，布尔值 |

### items（必填，最多 5000 个）

| 字段 | 说明 |
| --- | --- |
| `model` | 必填，模型名 |
| `displayName`、`group`、`billingType`、`currency` | 可选，字符串 |
| `groupRatio`、`modelRatio`、`completionRatio`、`cacheRatio`、`createCacheRatio`、`createCache1hRatio`、`imageRatio`、`audioRatio`、`audioCompletionRatio` | 可选，数字（倍率） |
| `modelPrice`、`inputValue`、`outputValue`、`perRequestValue` | 可选，数字（价格） |

站点没有公布的字段直接省略，不要用 `0` 代替"没有"。数字必须是数字类型，字符串形式的数字会被拒绝，请用 `Number()` 转换并检查 `Number.isFinite()`。

## fixtures

```json
{
  "name": "two models",
  "ctx": { "siteType": "custom" },
  "input": { "groups": { "default": 1 }, "models": [{ "name": "acme-chat-1", "ratio": 2.5 }] },
  "expect": { "result": { "items": [{ "model": "acme-chat-1", "modelRatio": 2.5 }] } }
}
```

这里的 `input` 是站点价格数据本身，没有字段限制。把线上真实的价格数据脱敏后做成样本最可靠。
