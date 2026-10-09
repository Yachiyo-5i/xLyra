# 模型元数据插件（model_metadata）


上游的模型名五花八门时，用它告诉 xLyra 每个模型的显示名称和能力。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "model_metadata" };
export function describeModels(ctx, input) { ... }
```

一次性函数：不发请求，时限 200 ms。

## 输入

`ctx.siteType`：站点类型。

`input`：

```ts
{ models: ["acme-vision-1", "acme-chat-1"] }
```

`models` 是需要描述的上游模型名，一次最多 500 个。

## 返回

```ts
{
  models: [
    { id: "acme-vision-1", name: "Acme Vision 1", capabilities: { vision: true } }
  ]
}
```

| 字段 | 说明 |
| --- | --- |
| `models` | 必填，最多 500 个 |
| `models[].id` | 必填，**必须是输入里出现过的模型名**，否则报错 |
| `models[].name` | 可选，显示名称 |
| `models[].capabilities` | 可选，对象，最多 64 个键，整体不超过 8 KiB |

不需要为每个输入都返回结果：不认识的模型直接省略，xLyra 会按默认方式处理。

## xLyra 怎么使用它

启用后对所有站点生效。每次同步模型时，xLyra 完成内置的能力补全之后，会对每个模型逐个调用已启用的 `model_metadata` 插件（`input.models` 里只有这一个模型名），并且**只补缺**：

- `capabilities` 里已经有的键，一律不覆盖——上游给的、内置清单里的值都优先；
- 显示名称只在模型当前没有显示名称（或者显示名称就是上游名）时才采用；
- 插件出错只会被忽略，该插件对这个模型不起作用。

因为所有站点都会经过它，请用 `ctx.siteType` 先判断是不是你负责的站点，不是就返回空的 `models`。

## fixtures

```json
{
  "name": "vision model",
  "ctx": { "siteType": "custom" },
  "input": { "models": ["acme-vision-1"] },
  "expect": { "result": { "models": [{ "id": "acme-vision-1", "capabilities": { "vision": true } }] } }
}
```

`input` 必填且按严格规则检查；`expect.result` 只检查你写出的字段，数组按长度和顺序逐项比较。
