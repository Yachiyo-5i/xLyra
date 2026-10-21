# API Key 站点类型（site_provider）

`site_provider` 用来声明一种新的 **API Key 站点类型**。管理员启用之后，创建站点时可以选它，用 API Key 接上游：列模型、校验密钥、健康检查、经网关转发。

它只支持 API Key。不支持账号池，也不支持 OAuth。协议只有清单里声明的那一种，不能再挂替代协议。

可选钩子用来处理这个类型和内置站点不一样的地方：额度查询、模型列表、密钥校验、错误分类、请求正文改写。五个都可以不导出。没导出的那一项走通用适配器或 xLyra 的默认行为。

仓库里的最小示例是 [`examples/site-provider-minimal/`](./examples/site-provider-minimal/)。下文的清单和钩子都引用这个目录里的文件，不另写一套。

## 和其他 kind 的区别

| | `site_provider` | 其他 kind |
| --- | --- | --- |
| 作用 | 新增一种站点类型 | 挂到已有站点或全局上 |
| 一个包 | 只声明一个站点类型 | 一个包一种 kind，不声明站点类型 |
| 生效 | 启用后按站点类型自动生效，不能逐站点绑定 | 多数要管理员绑定到站点 |
| 凭据 | 站点 API Key，由 xLyra 放进请求头 | 取决于 kind |

站点类型 ID 是 `<插件 id>/<名字>`，例如示例的 `example.minimal/chat`。斜线左侧必须等于清单的 `id`。一个包不能再写 `quotaProbe`、`site`、`protocol`、`automation`。

已有站点类型的模型列表、密钥校验或错误分类，用 [`model_list`](./model-list.md)、[`credential_check`](./credential-check.md)、[`error_classifier`](./error-classifier.md)。不要为了这三件事去声明一种新的站点类型。

## 开发流程

```bash
xlyra-plugin init --kind site_provider --id example.minimal my-site
```

`init` 的目录必须不存在或为空。`--kind` 不写时是 `quota_probe`，可以是 `xlyra-plugin kinds` 列出的任意一种。`--id` 不写时由目录名推导，不合规会提示改用 `--id`。`--name` 是清单里的展示名，不写则用 id。

然后改 `manifest.json` 和 `src/index.ts`，按需要补 `fixtures/*.json`。示例包已经带好可直接对照的文件：

| 文件 | 作用 |
| --- | --- |
| [`manifest.json`](./examples/site-provider-minimal/manifest.json) | 站点类型、主机、协议 |
| [`src/index.ts`](./examples/site-provider-minimal/src/index.ts) | `listModels`、`classify`、`rewritePayload` |
| [`fixtures/`](./examples/site-provider-minimal/fixtures/) | 四个带 `hook` 的样本 |
| [`README.md`](./examples/site-provider-minimal/README.md) | 示例说明，以及 `probe` / `check` 的可选写法 |

```bash
xlyra-plugin test my-site
xlyra-plugin build my-site
xlyra-plugin keygen --out my-key.pem          # 只需第一次
xlyra-plugin pack --sign my-key.pem my-site   # dist/<id>-<version>.xlp
xlyra-plugin verify dist/example.minimal-1.0.0.xlp
```

`build` 的 `--out` 默认是工程下的 `dist`。`pack` 的 `--out` 不写时是 `dist/<id>-<version>.xlp`。`test` 可以加 `--watch`。参数详见[命令行工具](./cli.md)。

`site_provider` 必须由受信密钥签名，未签名的包不能启用。把 `.xlp` 和公钥交给管理员：先导入公钥，再上传，启用时在请求里带上 `confirm_hosts`，内容与清单 `hosts` 相同（顺序无所谓）。启用成功后，创建页会出现这个站点类型。

对真实站点试分步钩子：

```bash
xlyra-plugin run --hook listModels --key-env RELAY_KEY --base-url https://api.example.com my-site
```

`--hook` 只能是 `probe`、`listModels`、`check`。`classify` 和 `rewritePayload` 不走 `run`，用 `test`。本地 `run` 只按站点地址做同源限制，不套用管理员确认的那份主机白名单。

## 清单 siteProvider

字段规则见[清单的 siteProvider 部分](./manifest.md#siteprovider-部分)。示例包的完整清单：

```json
{
  "apiVersion": 1,
  "hostApi": 1,
  "id": "example.minimal",
  "kind": "site_provider",
  "name": "最小站点示例",
  "description": "最小的 site_provider 示例：非标准模型列表、厂商错误码、删除上游不支持的字段",
  "version": "1.0.0",
  "license": "MIT",
  "siteProvider": {
    "siteType": "example.minimal/chat",
    "display": {
      "name": { "zh": "示例站点", "en": "Example Site" },
      "subtitle": { "zh": "API Key 站点", "en": "API key site" }
    },
    "hosts": ["api.example.com"],
    "credential": { "model": "api_key" },
    "protocol": {
      "kind": "openai_chat",
      "path": "/v1/chat/completions",
      "officialBaseURL": "https://api.example.com",
      "defaultEndpointTypes": ["openai"],
      "requestParams": { "unsupported": ["logit_bias"] }
    },
    "models": { "path": "/api/models", "auth": "bearer" }
  }
}
```

### 站点类型 ID

`siteType` 必须匹配 `^[a-z0-9][a-z0-9.-]{1,62}[a-z0-9]/[a-z][a-z0-9_]{0,31}$`。左侧 3–64 位，规则与插件 `id` 相同；右侧以小写字母开头，后面最多 31 位小写字母、数字或 `_`。左侧必须等于本包 `id`。不能占用内置站点类型的名字（内置类型的 ID 不含 `/`）。

### hosts

1–16 个小写 DNS 主机名，不能重复。不能带协议、端口、路径或用户信息，不能是 IP，至少含一个 `.`，每一段不能空、不能超过 63 字节，也不能以 `-` 开头或结尾。整个主机名不超过 253 字节。

`protocol.officialBaseURL` 必须是 `https`，要有主机，不能有用户信息、查询或片段。主机名忽略大小写、去掉端口后，必须出现在 `hosts` 里。

### 协议与端点类型

`protocol.kind` 只能是 `openai_chat`、`openai_responses`、`anthropic_messages`、`google_gemini`。网关按模型的端点类型和下游路径选适配器，不单独看 `protocol.kind`。两者一致时，`protocol.path` 才会写进这次协议的上游路径：`openai_chat`、`openai_responses`、`anthropic_messages` 把它拼在站点 `base_url` 后面，`base_url` 为空才用 `officialBaseURL`。`google_gemini` 适配器不读 `protocol.path`，地址是站点 `base_url` 加上 `/v1beta/models/<模型>:generateContent`（流式是 `:streamGenerateContent?alt=sse`）。

`defaultEndpointTypes` 至少一项，每项只能是 `openai`、`openai-response`、`anthropic-messages`、`google-gemini`。校验只看取值，不会按 `protocol.kind` 推断。这些值交给通用适配器：它拉到的每个模型会带上同样的 `supported_endpoint_types`，网关用这份列表选适配器。导出了 `listModels` 时，模型能力以钩子返回的 `capabilities` 为准，不会自动套上这份列表；钩子没写 `supported_endpoint_types` 时，网关可能落到 OpenAI chat 适配器。

`models.path` 不写等于 `/v1/models`，写了就要符合 `protocol.path` 的路径规则。`models.auth` 不写或 `bearer` 表示 `Authorization: Bearer <key>`；`header:<Name>` 表示把密钥放进该头，不加 `Bearer`。没有 `listModels` / `check` 时，通用适配器用这个路径做列模型、验 Key 和健康检查。

### requestParams

对象里只能有这些键，多出来的键会让规格校验失败，插件在接入时被跳过，站点类型不会出现：

| 键 | 作用 |
| --- | --- |
| `unsupported` | 从上游正文里删掉这些键 |
| `allowed_values` | 某个键的值不在允许列表里时，删掉这个键 |
| `defaults` | 正文里没有这个键时补上 |
| `fixed` | 正文里已经有这个键时，改成指定值 |
| `forced` | 无论原来有没有，都写成指定值 |
| `validation` | 在 `rewritePayload` 之后检查类型和最小/最大值。不通过则本次请求 400，错误类型 `request_param_validation_failed` |
| `supported` | 只被合并进规格，不用来过滤或删除参数 |

`unsupported`、`allowed_values`、`defaults`、`fixed`、`forced` 由 `applyResolvedRequestPolicy` 改正文。`openai_chat`、`openai_responses`、`anthropic_messages` 在组装上游正文时会调用它，发生在 `rewritePayload` 之前。`google_gemini` 的正文组装没有调用它，这五项不会改到那份正文上。`validation` 在改写之后对四种协议都会检查。`supported` 不参与这些处理。

替代协议、按模型覆盖、reasoning 都不在这张白名单里，写了会被拒绝。

## 启用与信任

启用 `site_provider` 有三条，缺一不可：

1. 包必须由受信公钥签名。未签名，或签名者不在受信列表里，返回 `js_plugin_untrusted_not_allowed`（HTTP 403）。`confirm_untrusted` 不能绕过。
2. 每次启用都要确认主机列表，包括升级后主机没变的那一次。请求体带 `confirm_hosts`，作为集合必须和清单 `hosts` 相同。没带、多了或少了，返回 `js_plugin_hosts_required`（HTTP 400），`error.hosts` 是清单里的完整列表。确认结果记成 `host:<主机名>`。
3. 已有站点使用旧的站点类型 ID 时，新版本不能改这个 ID。比较时忽略大小写。否则 `js_plugin_site_type_locked`（HTTP 409），响应带 `from`、`to`、`count`。

这种包不能绑定到单个站点。停用不受上面第 3 条限制。

## 钩子

五个都是可选导出。没导出就当没有这个钩子。`meta.id`、`meta.kind`、`meta.apiVersion` 必须和清单一致。

时限和大小与同类钩子相同：

| 钩子 | 每次调用 | 说明 |
| --- | --- | --- |
| `probe`、`listModels`、`check` | 200 ms | 最多调用 6 次（最多 5 个 HTTP 请求）。单个请求体 64 KiB，交给插件的响应体 1 MiB，单次 HTTP 20 秒 |
| `classify`、`rewritePayload` | 50 ms | 在请求路径上。等待运行时的上限是 250 ms，钩子本身仍是 50 ms |
| 所有钩子 | 内存 16 MiB，返回值 1 MiB | 日志每次调用最多 20 条，每条 1024 字节 |

分步钩子的 `ctx` 是 `siteType`、`baseURL`、`credentialType`、`now`（毫秒）。没有密钥。`site_provider` 没有 `baseURLMode`，地址按 `as_is` 整理（去掉尾部 `/`）；没传地址时用 `officialBaseURL`。

`listModels`、`check`、`classify` 的优先级：手工绑定的同类插件 > 站点类型自带钩子 > 通用适配器或默认分类。`probe` 不走这套绑定。站点上已经保存、并且能识别的 `quota_probe` 配置优先；没有配置，或者值是 `none`，又导出了 `probe` 时，自动用站点类型自己的钩子。

`listModels`、`check`、`probe` 失败（抛错、超时、返回 `{ error }`、主机不允许）时，这一次操作失败，不会悄悄改走通用适配器。

### listModels

拉模型列表时调用。先看站点有没有绑定 `model_list`，没有再看本钩子，再没有就用通用适配器的 `GET models.path`，解析 OpenAI 形的 `data[].id`。

返回 `{ request }`、`{ result }` 或 `{ error }` 之一。`result` 的形状与[模型列表插件](./model-list.md)相同：`models` 最多 2000 项，每项 `name` 必填。

示例包 [`src/index.ts`](./examples/site-provider-minimal/src/index.ts) 把 `{ "items": [{ "model_id", "title" }] }` 收成标准列表：

```ts
export function listModels(_ctx: ProbeContext, steps: ProbeStep[]): ModelListDecision {
  if (steps.length === 0) {
    return { request: { method: "GET", path: "/api/models" } };
  }
  const body = steps[0].response.json as { items?: unknown } | null;
  const items = body?.items;
  if (!Array.isArray(items)) {
    return { error: "models response has no items array" };
  }
  const models: { name: string; displayName?: string }[] = [];
  for (const raw of items) {
    if (typeof raw !== "object" || raw == null) {
      return { error: "models response item is not an object" };
    }
    const item = raw as { model_id?: unknown; title?: unknown };
    if (typeof item.model_id !== "string" || item.model_id === "") {
      return { error: "models response item has no model_id" };
    }
    const entry: { name: string; displayName?: string } = { name: item.model_id };
    if (typeof item.title === "string") {
      entry.displayName = item.title;
    }
    models.push(entry);
  }
  return { result: { models } };
}
```

样本是 [`fixtures/list-models.json`](./examples/site-provider-minimal/fixtures/list-models.json)。

### check

验 Key 和健康检查共用这个钩子。先看站点有没有绑定 `credential_check`，没有再看本钩子，再没有就用通用适配器去请求模型列表（2xx 当成功）。

`result.status` 只能是 `ok`、`invalid`、`unavailable`，含义与[密钥校验插件](./credential-check.md)相同。`invalid` 会被当成密钥无效；`unavailable` 会被当成暂时失败，不会当成坏密钥。健康检查把 `ok` 当成通过，把 `invalid` 记为校验失败，把 `unavailable` 记为上游不可达。钩子还没给出结果、这次调用的错误本身被分类成限流时，健康检查仍算通过。

示例包没有导出 `check`。要加的话，用示例 README 里的这一段，并在 `fixtures/` 放 `"hook": "check"` 的样本：

```ts
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

### probe

额度刷新时调用。站点没有配置探测类型（创建页对这种站点写入的 `none` 会当成没配置）且本钩子已导出时，自动用它。`result` 与[额度探测插件](./quota-probe.md)相同。

失败只让这一次刷新失败，不会改去探测内置站点的额度接口。

示例包没有导出 `probe`。示例 README 里的写法是：

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
```

### classify

上游错误被默认分类器读完之后调用。先看站点有没有绑定 `error_classifier`，没有再看本钩子。内置站点类型（ID 不含 `/`）不会走站点类型钩子。

`ctx` 只有 `siteType`。`input` 只有这四项，没有响应头、密钥和原始正文：

| 字段 | 说明 |
| --- | --- |
| `status` | HTTP 状态码 |
| `code` | 可选，默认分类器从正文读出的错误码 |
| `type` | 可选，错误类型 |
| `message` | 可选，错误信息，截到 1024 字节，不把字符截断 |

`class` 只能是 `limited`、`subscription_limit`、`transient`、`credential_invalid`、`unknown`。`reason` 最长 200 字节。返回 `unknown`、不认识的 class、超时或抛错，都保持原来的分类。钩子不决定冷却多久、是否重试。

这次调用收集到的日志不会写入服务日志。

示例包只把 `code === "E429"` 收成 `limited`，其它（包括普通的 HTTP 429）返回 `unknown`：

```ts
export function classify(_ctx: ErrorClassifyContext, input: ErrorClassifyInput): ErrorClassifyResult {
  if (input.code === "E429") {
    return { class: "limited", reason: input.message || "E429" };
  }
  return { class: "unknown" };
}
```

样本是 [`fixtures/classify-limited.json`](./examples/site-provider-minimal/fixtures/classify-limited.json) 和 [`fixtures/classify-unknown.json`](./examples/site-provider-minimal/fixtures/classify-unknown.json)。

### rewritePayload

网关把上游正文组装好之后、计费、参数校验、流式判断之前调用，也在把账号信息写进正文之前。流式请求同样会走。只改请求正文，不改响应。

`ctx` 为：

| 字段 | 说明 |
| --- | --- |
| `siteType` | 站点类型 |
| `model` | 正文里的 `model` 字符串；没有则用上游模型名 |
| `stream` | 正文里的 `stream` 是布尔值时用它，否则是 `false` |
| `protocol` | 这次上游正文的规范协议名。四种清单协议对应 `openai_chat`、`openai_responses`、`anthropic_messages`、`google_gemini` |

返回值必须是对象。**不得新增、删除或修改 `model` 和 `stream` 这两个键**：原来没有的不能补上，原来有的必须原样保留（`ctx.stream` 为 `false` 也不代表可以写上 `"stream": false`）。`google_gemini` 在把其它协议转换成 Gemini 正文时会删掉 `model`，这时不能再把 `model` 写回去。违反、抛错、超时、形状不对，或正文 JSON 超过 1 MiB（1048576 字节），本次请求失败，HTTP 502，错误类型 `plugin_rewrite_failed`，上游请求不会发出，也不会换下一把密钥重试。

没导出这个钩子则正文原样继续。

示例包删掉 `user`，留下 `model` 和 `stream`：

```ts
export function rewritePayload(_ctx: RewritePayloadContext, payload: Record<string, unknown>): Record<string, unknown> {
  const next: Record<string, unknown> = { ...payload };
  delete next.user;
  return next;
}
```

样本是 [`fixtures/rewrite-payload.json`](./examples/site-provider-minimal/fixtures/rewrite-payload.json)。期望里的 `"user": null` 用来表示这个键应当不存在：自检只比较期望里写出的字段，缺失和 `null` 视为相同。

这个钩子能看到用户消息正文。日志会被原样写入服务日志，见下文。

## 出站限制

`hosts` 按主机名精确匹配：去掉端口、忽略大小写，必须和列表中的某一项完全相同。不做子域匹配，没有通配符。

- 创建和更新站点时，`base_url` 的主机必须在列表里，否则拒绝保存。`base_url` 本身还要是带主机的 `http` 或 `https`。
- 通用适配器在列模型、验 Key、健康检查之前检查 `base_url` 的主机，不在列表里就不发请求。它的客户端拒绝离开列表的重定向：最多 5 次，不能从 `https` 降到 `http`，目标协议必须是 `https` 或与第一次请求相同。
- 网关在发出上游请求之前，用最终 URL 再查一次主机。站点模型测试走同一条限制。不通过则不发请求，HTTP 502，错误类型 `upstream_host_not_allowed`，不换密钥重试。上游客户端的重定向规则与通用适配器相同。
- `probe`、`listModels`、`check` 发出的请求要同时满足两件事：与站点 `base_url` 同源（协议、主机、端口相同；重定向也是，最多 10 次），以及解析后的 URL 通过上面的主机名检查。路径规则与[资源上限](./limits-and-security.md)里的插件 URL 相同，越界是 `js_url_rejected`。

站点类型已停用或不在启用快照里时，没有白名单。类型名含 `/` 的出站会被拒绝。内置站点类型不查这张表。

钩子的重定向检查是同源，不会在每一跳上再调用一次主机白名单。同源已经把主机和端口钉在站点 `base_url` 上，而发请求前的 URL 已经查过白名单。

## 生命周期

停用后，这种站点的数据还在，但 `SiteTypeAvailable` 为假：不参与路由，也不出现在下游模型目录。站点列表的 `provider_status` 为 `unavailable`，界面显示「提供方插件未启用」。启用中的插件站点类型是 `enabled`。内置站点类型没有这个字段。

仍有站点使用该插件任一版本声明过的站点类型时，不能卸载整个插件，也不能删除它的最后一个版本，错误是 `js_plugin_in_use`（HTTP 409），响应带 `site_type`、`count`。先删掉这些站点。停用不受这个限制。删除的不是最后一个版本时，不做这项检查。

多实例部署下，启用、停用和删除大约每 5 秒同步一次，不用重启。

熔断对 `site_provider` 只累计调用和失败，不会停用版本。一个坏钩子不会被自动摘掉，需要管理员手动停用。其它 kind 的自动停用规则见[限制与安全](./limits-and-security.md#自动保护熔断)。

## 安全注意

插件拿不到密钥，也不能自己发请求。分步钩子只描述 `request`，鉴权头由 xLyra 加上。`rewritePayload` 的 `ctx` 里没有密钥、请求头或账号字段；密钥是在正文改写之后才放进请求头的。

宿主写出插件日志时不脱敏。`probe`、`listModels`、`check`、`rewritePayload` 的日志会进服务日志。`classify` 这次调用收集到的日志会被丢掉。`rewritePayload` 能看到用户消息正文，请不要把用户消息打进日志。

请只启用可信来源的插件。未受信的 `site_provider` 不能靠二次确认启用。

## 已知限制

- 插件图标：`display.icon` 会校验（包内相对路径、`.png` 或 `.webp`、文件头符合格式、不超过 64 KiB），但接入站点类型时图标地址固定为空，后端不提供这张图片。创建页先查内置图标映射，插件类型没有映射，于是用显示名的首字符做占位。
- `display.brandColor` 只校验 `#RRGGBB`，不会送到创建页。
- 创建页的额度探测下拉只对 `openai` 和 `anthropic` 出现，倍率只对 `openai`、`anthropic`、`google_gemini` 出现。插件站点类型的倍率能力固定为关。创建时写入的额度探测值是 `none`，服务端把它当成没配置；导出了 `probe` 就会自动探测。
- 显示名和副标题按 `zh`、然后 `en`、再按语言标签字母序取第一个非空值。写成单个字符串时，用的就是这句。
- 替代协议不开放。协议规格里不能写 `alternate_protocols`。
- `rewritePayload` 的输入或返回值，JSON 超过 1 MiB 会失败。大图 base64 很容易超过。构造超大返回值时，也可能先碰到 50 ms 时限，错误是 `js_timeout` 而不是体积错误。

## 排错

启用、卸载和网关相关的错误码见[错误码](./errors.md)。和本类型直接相关的是：

| 错误码 | HTTP | 含义 |
| --- | --- | --- |
| `js_plugin_untrusted_not_allowed` | 403 | 未由受信密钥签名。`confirm_untrusted` 无效 |
| `js_plugin_hosts_required` | 400 | 没确认主机，或 `confirm_hosts` 与清单不一致。`error.hosts` 为完整列表 |
| `js_plugin_site_type_locked` | 409 | 有站点仍用旧的站点类型 ID。响应带 `from`、`to`、`count` |
| `js_plugin_in_use` | 409 | 仍有站点时卸载插件或删除最后一个版本。响应带 `site_type`、`count` |
| `plugin_rewrite_failed` | 502 | `rewritePayload` 失败，请求未发出 |
| `upstream_host_not_allowed` | 502 | 上游主机不在确认过的列表里，请求未发出 |
| `request_param_validation_failed` | 400 | `requestParams.validation` 没通过。这是改写成功之后的参数校验 |

钩子运行失败仍使用 `js_timeout`、`js_shape_error`、`js_exception`、`js_url_rejected`、`js_plugin_response_too_large` 等类型，格式见[错误码](./errors.md)。
