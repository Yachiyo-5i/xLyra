# 最小 site_provider 示例（example.minimal）

演示一个 API Key 站点类型插件：清单声明站点类型和协议，源码实现三个可选钩子。`probe` 和 `check` 没有导出，需要时按文末的片段自己加上。

上游在这个示例里是虚构的 `api.example.com`。它的模型列表不是 OpenAI 的 `data[].id`，错误码用厂商自己的 `E429`，聊天请求不接受 `user` 字段。

## 文件

| 文件 | 作用 |
| --- | --- |
| `manifest.json` | 插件清单。`id` 是 `example.minimal`，站点类型是 `example.minimal/chat` |
| `src/index.ts` | `listModels`、`classify`、`rewritePayload` |
| `fixtures/*.json` | 自检样本，每个文件一个场景，都带 `hook` |
| `tsconfig.json` | 与 `xlyra-plugin init` 生成的相同 |
| `types/xlyra.d.ts` | 类型声明，与脚手架写入的文件相同。只用于 `import type`，不会打进包里 |
| `.gitignore` | 只忽略 `dist/`，与脚手架相同 |

`fixtures/list-models.json` 覆盖分步调用的两步：先请求 `GET /api/models`，再用样本里的上游响应产出模型列表。`classify` 有一正一负。`rewrite-payload.json` 是改写的正例。

## 钩子

`listModels` 把上游的非标准列表转换成标准结果：

```json
{ "items": [{ "model_id": "m1", "title": "Model 1" }] }
```

变成：

```json
{ "models": [{ "name": "m1", "displayName": "Model 1" }] }
```

`classify` 只看宿主传入的 `status`、`code`、`type`、`message`，看不到完整错误体。上游体 `{ "code": "E429", "message": "slow down" }` 在样本里写成同样的 `code` 和 `message`（`status` 按契约必填）。`code` 为 `E429` 时返回 `limited`，其它返回 `unknown`，包括普通的 HTTP 429。

`rewritePayload` 删掉 `user`，不改 `model` 和 `stream`。样本期望里的 `"user": null` 用来拦住“字段还在且不是 null”的情况：自检只比较期望里写出的字段，缺失和 `null` 被视为相同。

## 本地检查

在仓库的 `server` 目录执行。下面的命令不往示例目录写构建产物：

```bash
go run ./cmd/xlyra-plugin test ../docs/plugins/examples/site-provider-minimal
go run ./cmd/xlyra-plugin build --out /tmp/example-minimal ../docs/plugins/examples/site-provider-minimal
go run ./cmd/xlyra-plugin pack --out /tmp/example.minimal-1.0.0.xlp ../docs/plugins/examples/site-provider-minimal
go run ./cmd/xlyra-plugin verify /tmp/example.minimal-1.0.0.xlp
```

已经装好 `xlyra-plugin` 时，把 `go run ./cmd/xlyra-plugin` 换成 `xlyra-plugin` 即可。省略 `--out` 时，`build` 和 `pack` 会把产物写到示例目录的 `dist/`，该目录已被忽略。

## 加上 probe 和 check

这两个钩子是可选的。导出之后，在 `fixtures/` 里各加一个带 `"hook": "probe"` 或 `"hook": "check"` 的分步样本，再跑 `xlyra-plugin test`。

在文件顶部的类型导入里补上 `ProbeDecision` 和 `CredentialCheckDecision`，然后：

```ts
export function probe(_ctx: ProbeContext, steps: ProbeStep[]): ProbeDecision {
  if (steps.length === 0) {
    return { request: { method: "GET", path: "/api/usage" } };
  }
  const body = steps[0].response.json as { total?: number; used?: number } | null;
  const limit = Number(body?.total);
  const used = Number(body?.used);
  return {
    result: {
      kind: "balance",
      entries: [{ label: "balance", unit: "usd", limit, used, remaining: limit - used }],
    },
  };
}

export function check(_ctx: ProbeContext, steps: ProbeStep[]): CredentialCheckDecision {
  if (steps.length === 0) {
    return { request: { method: "GET", path: "/api/me" } };
  }
  const status = steps[0].response.status;
  if (status === 200) return { result: { status: "ok" } };
  if (status === 401 || status === 403) {
    return { result: { status: "invalid", message: `rejected with ${status}` } };
  }
  return { result: { status: "unavailable", message: `site answered ${status}` } };
}
```
