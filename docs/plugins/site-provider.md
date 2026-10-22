# 站点类型（site_provider）

`site_provider` 用来声明一种新的站点类型。管理员启用之后，创建站点时可以选它，经网关转发。凭据模型是 `credential.model`：

| `credential.model` | 站点怎么接上游 |
| --- | --- |
| `api_key` | 一把站点 API Key。列模型、校验密钥、健康检查、转发都用这把 Key |
| `account_pool` | 多账号。登录、刷新、导入、按账号区域选地址、按账号额度选路，由 xLyra 执行。插件只描述流程 |

两种模型的协议都只有清单里声明的那一种，不能再挂替代协议。没有独立的 OAuth 连接类型（没有 `connection`，也没有 `oauth2_pkce`）。账号池里的设备码登录是 `login.kind = oauth2_device_code`，由宿主的 OAuth 引擎执行，不经过 JS。

`api_key` 的可选钩子用来处理这个类型和内置站点不一样的地方：额度查询、模型列表、密钥校验、错误分类、请求正文改写。五个都可以不导出。没导出的那一项走通用适配器或 xLyra 的默认行为。`account_pool` 在这五个之外还有登录、刷新、摘要钩子，哪些必填见[账号池站点](#账号池站点credentialmodelaccount_pool)。

仓库里的最小 API Key 示例是 [`examples/site-provider-minimal/`](./examples/site-provider-minimal/)。下文 API Key 的清单和钩子引用 minimal 目录里的文件。账号池没有入库示例，用 `xlyra-plugin init` 生成模板。

## 和其他 kind 的区别

| | `site_provider` | 其他 kind |
| --- | --- | --- |
| 作用 | 新增一种站点类型 | 挂到已有站点或全局上 |
| 一个包 | 只声明一个站点类型 | 一个包一种 kind，不声明站点类型 |
| 生效 | 启用后按站点类型自动生效，不能逐站点绑定 | 多数要管理员绑定到站点 |
| 凭据 | `api_key`：站点 API Key，由 xLyra 放进请求头。`account_pool`：`account:<siteType>:<uuid>`，密钥留在宿主 | 取决于 kind |

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

### display

`display.name` 必填，`display.subtitle` 可选。两者都是一个字符串，或按语言分的对象（最多 8 种语言，每种不超过 400 字节），键保持清单里的写法，例如 `zh`、`en`、`jp`。

管理端按当前界面语言展示这两段文案。先精确匹配语言码，再匹配语言前缀（`zh-CN` 用 `zh`）。界面语言是日文时，`ja` 和 `jp` 都能对上。没有当前语言时用 `en`，再没有用 `zh`，还没有就用其余非空文本里语言标签字母序的第一个。只含空白的值会丢掉，不参与选择。建议至少写 `zh` 和 `en`。

站点类型接口仍返回 `display_name` 和 `subtitle` 字符串，取值顺序是 `zh`、然后 `en`、再按语言标签字母序的第一个非空值，给不读多语言字段的客户端做回退。多语言正文在 `display_name_i18n` 和 `subtitle_i18n`，没有可用文本时省略。内置站点类型没有这两个字段。

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

## 请求头模板

`siteProvider.request` 可以不写。不写时，网关转发的请求头与改这项功能之前相同：内置站点类型不读这份配置，没有 `request` 的插件站点也不会多头或少头。

```json
"request": {
  "headers": {
    "Authorization": "Bearer {{cred.api_key}}",
    "X-Client": "xlyra"
  },
  "stripDownstreamHeaders": true
}
```

### 语法

模板里只有三种占位符，花括号内不能有空格，不能嵌套，`{{` 必须用 `}}` 关上。没有通用表达式。

| 占位符 | 值 |
| --- | --- |
| `{{cred.<field>}}` | 凭据字段。`<field>` 必须属于当前 `credential.model` 允许的集合。`api_key` 只有 `api_key`，值是整串站点 Key。`account_pool` 的集合是 `credential.fields` 里声明的每一个字段名 |
| `{{region.<key>}}` | 这次账号解析出的区域的 `attrs.<key>`。`<key>` 只能是 `origin` 或 `referer`，并且每一个区域都声明了这个键。`api_key` 没有区域，不能使用 |
| `{{site.baseURL}}` | 这次实际使用的上游基址，去掉尾部 `/` |

`{{ cred.api_key }}`、`{{cred.api_key`、`{{secret.x}}`、`{{site.baseurl}}` 都会在校验清单时拒绝。`{{secret.<field>}}` 不是头模板占位符，只用在账号池钩子描述的请求里，见[密钥不进 JS](#密钥不进-js)。`api_key` 写 `{{cred.domain}}` 会拒绝，因为 `domain` 不在允许的字段里。校验按调用方传入的字段集合判断，不把字段名写死。同一个模板可以多次使用同一个占位符。占位符以外的字面量不能含 CR、LF 或其它控制字符。

头的值可以是字符串，也可以是对象。对象只有 `value`、`when`、`unless` 三个键。`when` 和 `unless` 互斥，各自最多出现一次，值只能是 `cred.<已声明字段>`。字段值为空，或这次变量表里没有这个字段，都算空。`when` 在字段非空时发送；`unless` 在字段为空时发送。条件不成立时删掉这个头，不写成空字符串，也不渲染 `value`（所以里面的占位符缺失不会因此失败）。两个条件都不写时，对象和字符串一样，总是发送。

```json
"X-Domain": { "value": "{{cred.domain}}", "when": "cred.domain" },
"X-No-Department-Info": { "value": "1", "unless": "cred.domain" }
```

`headers` 最多 16 项。头名必须是合法的 HTTP token。忽略大小写后相同的头名算重复。下面这些不能由模板写：`Host`、`Content-Length`、`Transfer-Encoding`、`Connection`、`Upgrade`、`TE`、`Trailer`、`Proxy-Authorization`、`Proxy-Connection`、`Cookie`、`Set-Cookie`，以及任何 `X-Forwarded-*`。`Content-Type` 和 `Accept` 由协议适配器决定，模板也不能写。`Authorization` 可以写，而且只有模板里声明了，才会按模板的值发送。

### 何时写入

网关只在一个位置应用模板：协议适配器写完头、下游头按现有规则透传、内置站点自己的头都处理完之后，给上游请求签名之前。站点模型测试走同一处函数。

`stripDownstreamHeaders` 不写等于 `false`。

- `false`：已经写上的头都保留，包括这条路径会透传的下游头。然后写入模板，同名头以模板为准。
- `true`：先按白名单删头，只留模板里声明的头名，再加上 `Content-Type` 和 `Accept`，然后写入模板。下游带来的 `X-Stainless-*`、`Anthropic-Beta` 不会留下。模板没声明的头也会被删掉，包括协议适配器刚写上的。Messages 适配器会删掉 `Authorization` 并写上 `x-api-key`；若模板没有声明 `x-api-key`，剥离之后它不在请求里。模板声明了 `Authorization` 时，按模板的值写回去。

下游透传只发生在 `/v1/messages` 和 `/v1/responses`，并且本来就不转发 `Cookie`、`Host`、`Authorization` 等。剥离是在透传之后再收窄一次。

渲染失败时，这次请求 HTTP 502，错误类型 `plugin_header_template_failed`。失败原因是变量不在这次的变量表里，或渲染结果含 CR、LF、其它控制字符。错误信息里有头名和原因，不含密钥的值。请求不会发往上游，也不会换下一把密钥。

### 和 models.auth 的分工

`models.auth` 只决定通用适配器在列模型、验 Key、健康检查时把密钥放进哪个头。网关转发不看它。转发头在没有 `request.headers` 时仍由协议适配器决定；写了 `request.headers` 之后，由模板在适配器之后覆盖。模板里声明 `Authorization` 不会和 `models.auth` 冲突。

### 变量从哪来

`api_key` 插件站点没有单独的凭据解析器。网关把解密后的站点 Key 放进 `cred.api_key`，把这次使用的上游基址放进 `site.baseURL`。

`account_pool` 的变量表放三类值：非密钥字段，access token 字段（默认 `access_token`，清单用 `accessTokenField` 改名时用那个名字），以及解析出的区域的 `region.<key>`。`refresh_token` 和其它 `secret: true` 的字段不会放进变量表。模板里写了 `{{cred.refresh_token}}` 能通过清单校验（字段已声明），转发时变量不存在，这次请求失败，错误类型 `plugin_header_template_failed`，不换下一把凭据。条件头在字段缺失时按空处理，不走这个失败。`site.baseURL` 在 `request.baseURLFrom` 为 `region` 时是该账号解析出的区域基址。

### 安全

模板在主机检查之前渲染。渲染失败，或主机不在启用时确认的列表里，请求都不会发出。密钥只会随实际发出的请求到达已确认的主机。变量值里的 CR、LF 或其它控制字符会使这次请求失败，避免把换行写进头里。插件代码看不到这些值。

## 账号池站点（credential.model=account_pool）

`account_pool` 描述一种多账号站点。登录、刷新、导入、选区域、选账号、把密钥放进上游请求，都由 xLyra 的 Go 代码执行。插件导出的函数只描述下一步请求和结果形状。密钥留在宿主的秘密袋里，不进 JS。

这种清单可以不写 `models`。不写表示不按账号拉模型列表。要拉，只写 `models.perAccount: true`，并且必须导出 `listModels`。账号池导出了 `listModels` 但没有 `models.perAccount: true` 时，清单加载直接拒绝。空对象 `{}`、`models.path`、`models.auth` 都会被拒绝。`models.path` / `models.auth` 仍只属于 `api_key`；`api_key` 写 `perAccount` 也会被拒绝。五个可选钩子的调用方式见 [钩子](#钩子)。`perAccount` 的 `listModels` 见下面的 [按账号列模型](#按账号列模型)。

`api_key` 清单不能写下面这些账号池字段：`credential.fields`、`accessTokenField`、`refreshTokenField`、`expiresAtField`、`expiresFrom`、`identity`、`display`、`regions`、`defaultRegion`、`login`、`refresh`、`import`、`selection`、`ui`。`quota` 和 `request` 两种模型都可以写。

字段以本节为准。

### 凭据字段

`credential.fields` 是账号凭据包里的字段，1–16 项，不能重复。

| 字段 | 说明 |
| --- | --- |
| `name` | `^[a-z][a-z0-9_]{0,31}$` |
| `secret` | 可选。为真时，值是密钥：不进 JS，不进管理端明文，不进头模板变量（access token 字段除外） |
| `type` | 可选，`string`（默认）或 `unix`。`unix` 在钩子回写时必须是非负十进制整数字符串 |

至少要有一个 `secret: true` 的字段。

| 名字 | 默认 | 约束 |
| --- | --- | --- |
| `accessTokenField` | `access_token` | 必须是已声明的 secret 字段。网关把它的值当作上游密钥 |
| `refreshTokenField` | 不设 | 设了就必须是已声明的 secret 字段。`refresh.kind` 为 `oauth2_refresh` 时必填 |
| `expiresAtField` | 不设 | 设了就必须是已声明字段，且 `type` 为 `unix`。网关用它和 `leadSeconds` 决定要不要刷新。不设则网关不按到期时间自动刷新 |
| `expiresFrom` | 不设 | 只允许 `claims.exp`，并且必须同时写了 `expiresAtField`。到期字段已经有值时不覆盖。令牌响应、刷新合并、导入都是这样：字段空着才从 access token 的 JWT `exp` 取 Unix 秒 |
| `identity` | 必填 | 见下 |
| `display` | 不设 | 见下。行上显示的名字。不写时仍用 identity |

### identity 与 display

`identity` 和 `display` 都是有序来源。JSON 可以是一个字符串，或一个字符串数组。一个字符串在写回时仍是字符串。数组最多 8 项，不能重复，不能有空项。取第一个非空值。单条来源全部失败时，`field.*` 报这个字段为空，其它情况报 identity claim 为空。数组全部失败也报 identity claim 为空。

| 写法 | 含义 |
| --- | --- |
| `claims.<路径>` | 从 access token 的 JWT payload 读字符串。`claims.sub` 读 payload 的 `sub`。路径可以再点一层，例如 `claims.profile.email`。签名不校验。`oauth2_device_code` 在 access token 读不到时再试 id token。claim 缺失或不是非空字符串就试下一条 |
| `field.<name>` | 读凭据包里这个非密钥字段。字段必须已声明，且不能是 secret |
| `summary.label` | 只允许出现在 `display`。用 `summarizeAccount` 返回的 `label`。宿主把它存进 meta 的 `display`。超过 256 字节、含控制字符、或擦掉密钥后文本变了，就丢掉，不当显示名 |

`identity` 不能写 `summary.label`。选中的值如果包含秘密袋里的密钥，这次登录失败。

同一站点上，身份相同的再次登录沿用原来的 `credential_type`，不另建一把凭据。显式重新授权（带目标凭据 id）更新那一把。

管理端行上的名字按这个顺序：`display` 解析出的值，然后 `identity`，然后入库时的掩码。`display` 把 `summary.label` 放在前面时，摘要返回的昵称会盖过 JWT 里的 email。

### regions 与 defaultRegion

`regions` 必填，1–8 项。`defaultRegion` 必填，必须是其中某个 `id`。因此合法的账号池站点，创建页的 `supports_regions` 为真。

| 字段 | 说明 |
| --- | --- |
| `id` | `^[a-z][a-z0-9_-]{0,15}$`，不能重复 |
| `label` | 可选。字符串，或按语言分的对象（最多 8 种语言，每种不超过 400 字节） |
| `baseURL` | `https` URL，主机必须在 `hosts` 里。站点类型的官方基址取 `defaultRegion` 这一项，去掉尾部 `/` |
| `realms` | 可选，最多 8 项。每一项是小写的 `[a-z0-9_.-]`，不能空 |
| `domains` | 可选，最多 8 项。每一项是和 `hosts` 同样规则的小写主机名 |
| `attrs` | 可选，最多 2 项。键只能是 `origin` 或 `referer`。值 1–256 字节，不能含控制字符。头模板 `{{region.<key>}}` 只能使用每个区域都声明了的键 |

账号用哪一个区域，按这个顺序，命中即停：

1. 凭据字段 `region` 非空：原样当作区域 id，即使这份清单里没有这个 id。
2. 凭据字段 `realm` 与某个区域的 `id` 相同，或出现在该区域的 `realms` 里（忽略大小写）。
3. 凭据字段 `domain` 出现在某个区域的 `domains` 里（忽略大小写）。
4. 都没有：用 `defaultRegion`。

`request.baseURLFrom` 为 `region` 时，网关用上面选出的区域的 `baseURL`。选中的 id 在清单里不存在，这次凭据失败。

### login

`login` 必填。`kind` 只能是：

| `kind` | 谁执行 | 必须导出 |
| --- | --- | --- |
| `oauth2_device_code` | 宿主的设备码引擎。JS 不参与 start / poll | 不导出 `startLogin` / `pollLogin` |
| `custom_poll` | 钩子 `startLogin`、`pollLogin` | 这两个都要导出 |

`params` 可选。每一项的 `name` 必填，`type` 只能是 `select`，`source` 只能是 `regions`（因此必须有 `regions`）。管理端把选中的区域 id 放进 `params.region` 传给开始登录。钩子看到的 `ctx.params` 就是这些值，不是凭据。

`oauth2_device_code` 还要：

| 字段 | 说明 |
| --- | --- |
| `deviceCodeURL` | 必填。`https`，主机在 `hosts` 里 |
| `tokenURL` | 必填。同上 |
| `clientID` | 必填。不能含控制字符 |
| `scope` | 可选。不能含控制字符 |
| `grantType` | 可选。设备码轮询用的 `grant_type`。空着等于 `urn:ietf:params:oauth:grant-type:device_code`。刷新固定用 `refresh_token`，不受这项影响 |
| `extraHeaders` | 可选。静态头，不能含 `{{`。头名是 HTTP token，忽略大小写后不能重复。不能是 `Authorization`、`Cookie`、`Host`、`Content-Length`、`Transfer-Encoding`、`Connection`、`Proxy-Authorization`，也不能是 `X-Forwarded-*`。值不能空，不能含控制字符。引擎会改写 `Content-Type` 和 `Accept` |

`custom_poll` 不能写 `deviceCodeURL`、`tokenURL`、`clientID`、`scope`、`grantType`、`extraHeaders`。

`secretPaths` 见[密钥不进 JS](#密钥不进-js)。`custom_poll` 的 `secretPaths` 必须有一条路径映射到 access token 字段。`oauth2_device_code` 的令牌由引擎从令牌响应读取，不靠 `secretPaths`。

设备码引擎：`interval` 缺省或小于 1 时按 5 秒，大于 60 秒时按 60 秒；`slow_down` 在当前间隔上加 5 秒，不超过 60 秒。`expires_in` 缺省按 600 秒，大于 1800 秒时按 1800 秒。`verification_uri` 的主机不在 `hosts` 里，这次 start 失败，不下发地址。

### refresh

`refresh` 必填。`kind` 只能是：

| `kind` | 行为 | 必须导出 |
| --- | --- | --- |
| `oauth2_refresh` | 宿主用 refresh token 调令牌端点。响应里没有新的 refresh token 时，保留原来的 | 不导出 `refreshTokens`。`refreshTokenField` 必填 |
| `custom_steps` | 钩子 `refreshTokens` | 要导出 `refreshTokens` |
| `none` | 不刷新。调用刷新接口会失败 | 不导出 `refreshTokens` |

`leadSeconds` 可选，30–3600。不写时，`oauth2_refresh` 和 `custom_steps` 用 300。`none` 不能写 `leadSeconds`、`secretPaths`、`tokenURL`。

网关在选路时看到期时间：`expiresAtField` 缺了、空了或解析不了，就刷新；到期时刻早于「现在 + leadSeconds」就刷新；刚好等于这个提前量则不刷新。`Refresh` 钩子本身不看 `leadSeconds`。管理端的「刷新账号」、登录完成后的那一次刷新、导入成功后的那一次刷新，都会立刻换令牌，不等到提前量。

`oauth2_refresh` 的 `tokenURL` 可选。不写就用 `login.tokenURL`；两边都空则校验失败。写了就必须是 `hosts` 里的 `https`。

自定义刷新的基址：先按[区域顺序](#regions-与-defaultregion)选区域，否则用默认区域。

### import

`import` 可选。不写等于不能导入。`enabled: true` 时创建页 `supports_import` 为真，并且 `ui.operations` 才可以包含 `import`。

导入不进 JS。宿主按清单把 JSON 文件映射成凭据字段。

| 字段 | 说明 |
| --- | --- |
| `containers` | 可选，最多 8 项。每一项匹配 `^[A-Za-z0-9_.-]+$`，可以是点分路径（`data.accounts`）。用来从包装对象里取出账号数组或单个账号对象 |
| `fieldMap` | 键必须是已声明的字段名。每个键 1–8 个别名，别名同样匹配上面的正则，不能重复。别名是**每个账号对象的顶层键**，按数组顺序用第一个非空的字符串或数字。其它 JSON 类型跳过。nickname 收成 email 就写成 `"email": ["nickname", "nick_name", "name"]`，并声明非密钥字段 `email` |
| `valueMap` | 可选。键必须是已声明字段，不能是到期字段。最多 8 个字段，每个 1–8 条。来源和目标都是 1–64 字节，匹配 `^[A-Za-z0-9_.-]+$`，不能含控制字符。别名取到值并去掉首尾空白之后做精确匹配，命中就改成目标值。不匹配的值原样留下 |
| `msThreshold` | 可选。不写等于 `1000000000000`（1e12）。写出时必须是 1 到 `1000000000000000`（1e15）的整数。大于等于这个数的到期数字按 Unix 毫秒，更小的正数按 Unix 秒 |

`enabled` 为真时，`fieldMap` 必须覆盖每一个 secret 字段。`identity` 和 `display` 里每一条 `field.<name>` 也必须在 `fieldMap` 里。

文件形状：一个 JSON 值。UTF-8 BOM 会去掉。可以是对象数组、单个对象，或对象里第一条能落到对象 / 数组上的 `containers` 路径。路径缺失或落到标量上就试下一条；都没有则把根对象本身当作一个账号。空数组、不是对象也不是数组、超过上限，整份文件拒绝。

到期时间（`expiresAtField`）：大于等于 `msThreshold`（不写则是 1e12）的数字当作 Unix 毫秒，更小的正数当作 Unix 秒。数字字符串和 RFC3339 字符串也可以。解析成功后存成 Unix 秒的十进制字符串。字段缺失时，非密钥的到期字段可以省略；secret 的到期字段缺失则拒绝。省略之后如果 `expiresFrom` 是 `claims.exp`，再用 access token 的 `exp` 填上。解析失败整份文件拒绝。错误里有从 1 开始的账号序号和字段名，没有字段值。字段完全相同的重复账号只留第一条。

单文件最大 4 MiB，最多 500 个账号对象。管理端一次导入最多 50 个文件、合计 200 个账号。

导入不会把 realm 别名收成区域 id，也不会把文件里的 nickname 收成 email。区域仍按上面的 realm / domain 规则在运行时匹配。

### selection

`selection` 可选。不写等于 `ordered`。

| 值 | 行为 |
| --- | --- |
| `ordered` | 保持候选原来的顺序 |
| `quota_weighted` | 在去掉额度耗尽的账号之后，按剩余额度从高到低稳定排序。权重是各 entry 的 `remaining` 之和。全部 unlimited 的权重视为 +∞，排在有数字的前面。有 entry 但没有可用数字的权重是 0。没有 entry 的排在已知权重之后 |

某一条 entry 的 `remaining` ≤ 0、不是 unlimited、并且 `resetAt` 在未来：这把凭据进入冷却，直到这些 `resetAt` 里最晚的一个，最长 7 天，原因 `quota_exhausted`。全部账号都在冷却中时，这次没有可用凭据。内置 Grok 的选择循环不走这套规则。

### quota.aggregate

`quota.aggregate` 可选，两种模型都可以写。取值 `sum`、`min`、`none`。

| 值 | 行为 |
| --- | --- |
| `sum` | 把有限、非负的 `remaining` / `limit` / `used` 分别相加。`resetAt` 取最早的未来时间 |
| `min` | 取最小的有限非负 `remaining`，并带上那一条的 `limit`、`used`。相同剩余时偏好更早的未来 `resetAt` |
| `none` | 不写站点级汇总 |

账号池不写时默认 `sum`。`none` 时刷新账号不会改站点汇总。API Key 站点不写这项时，额度探测仍走原来的「最小剩余」汇总，不改道；只有清单显式写了 `sum`、`min` 或 `none` 才按上表处理。汇总里可以带 `aggregate`。

定时刷新和手动刷新站点对账号池走同一条账号刷新：账号最多 4 个同时跑，每个账号的网络往返有单独的时限，整站账号刷新也有上限，避免一个账号很多的站点吃掉后面站点的批次时间。每个账号拿刷新租约，按插件规则续期，把 `summarizeAccount` 的摘要写进该账号的 state。需要重新授权的账号会停用。暂时失败不会清掉该账号上次的额度明细。然后按上表做额度汇总，并更新站点上的账号数和模型数。单个账号刷新、启用、停用、删除之后也会重算这两个计数。模型同步仍由站点自己的模型列表能力决定。没写 `models.perAccount` 的账号池刷新记为 `partial`，`sync_message` 说明跳过了模型同步，账号数和已保存的模型数照常更新，不会因为「不支持模型同步」把整次刷新判失败。全部启用账号都失败时，站点同步消息按 account 计数，不叫 API Key。

健康检查不走下面的单个 `check` 钩子，也不做完整的账号刷新。它对每个启用的账号先保证令牌可用（到期才刷新），再调用一次 `summarizeAccount` 判断可用或重新授权。这次探测不回写 usage，也不重写站点额度。账号最多 4 个同时探测，并跟随调用方的取消；取消时还没探完的账号保持原状。至少有一个启用账号可用，站点就是健康的。重新授权失败会立刻停用该账号，同步状态写成 `reauth_required`；其它失败写成 `stale`，账号保持启用。站点连续 3 次健康检查不通过才标成不健康，和内置站点用的是同一条阈值。

### request

`request` 与 [请求头模板](#请求头模板) 共用同一套语法和剥离规则。账号池额外有两项：

| 字段 | 说明 |
| --- | --- |
| `upstreamStreamOnly` | 可选，默认 false。为真时，协议覆盖强制 `stream: true`。下游没有要流、上游又是 chat SSE 时，用现成的 SSE 聚合把响应收成非流式。内置 Grok 不读这项 |
| `baseURLFrom` | 可选。不写或空：不按账号改基址。`region`：用[区域](#regions-与-defaultregion)解析出的 `baseURL`，并且必须已经声明 `regions`。其它值拒绝 |

`api_key` 也可以写 `upstreamStreamOnly` 和 `baseURLFrom`。`baseURLFrom: "region"` 在没有 `regions` 时拒绝，所以实际上只有账号池能用 `region`。

头模板在账号池上的变量见[变量从哪来](#变量从哪来)。`Content-Type` 和 `Accept` 仍然不能写进模板。模板是静态字符串加占位符，不能按区域或按字段是否为空切换头。

### ui

`ui` 可选。管理端站点类型的 `accounts.ui` 用蛇形名：`row_pills`、`operations`、`texts`。三项都空则响应里不带 `ui`。

`rowPills` 最多 4 项。`field` 必须是已声明的非密钥字段。`labels` 可选，把字段值映射成文案，最多 8 项；键不能空、不超过 64 字节、不能含控制字符；值是本地化文案。

`operations` 不能重复。允许的值：

`add`、`reauth`、`refresh`、`delete`、`toggle`、`models`、`import`

`import` 要求 `import.enabled` 为真。

`texts` 的值是一个字符串（各语言共用），或 `{"zh","en",...}`（最多 8 种语言，每种不超过 400 字节）。键只能是：

`title`、`login_reauth_title`、`login_title`、`login_starting`、`login_instructions`、`login_authorize`、`login_waiting`、`cancel`、`region_title`、`region_description`、`region_continue`、`loading`、`add`、`import`、`refresh_all`、`unnamed`、`refresh`、`delete`、`reauthorize`、`toggle_label`、`empty`、`delete_title`、`delete_confirm`、`delete_delete`、`login_hint`、`import_title`、`import_dropzone`、`import_hint`、`import_selected_count`、`import_summary`、`import_imported`、`import_failed`、`import_unnamed`、`import_done`、`import_with_count`、`import_submit`、`import_toast_done`、`import_toast_failed`

### 钩子

账号池钩子和 `probe` 一样是分步的：返回 `{ request }`、`{ result }` 或 `{ error }` 之一，三者只能有一个。时限 200 ms，最多调用 6 次（最多 5 个 HTTP 请求）。单个请求体 64 KiB，交给插件的响应体 1 MiB，单次 HTTP 20 秒。内存 16 MiB，返回值 1 MiB。日志每次调用最多 20 条，每条 1024 字节。

`result.kind` 不是插件 kind，是这次返回值的种类：

| 钩子 | `ctx` | `result.kind` | `status` |
| --- | --- | --- | --- |
| `startLogin`、`pollLogin` | `siteType`、`baseURL`、`params`（登录表单，不是凭据）、`now`（毫秒） | `account_login` | `pending`、`complete` |
| `refreshTokens` | `siteType`、`baseURL`、`fields`（只有非密钥字段）、`now` | `account_refresh` | `ok`、`reauth` |
| `summarizeAccount` | 与 `refreshTokens` 相同 | `account_summary` | `ok`、`unavailable`、`reauth` |

`account_login` 的结果还可以有 `verificationURL`、`userCode`、`interval`、`credential`。`pending` 时，`verificationURL` 的主机必须在 `hosts` 里，否则这次登录失败，地址不下发。`interval` 缺省或小于 1 按 5 秒，大于 60 按 60 秒。`complete` 必须带 `credential`。

`account_refresh` 的 `ok` 必须带 `credential`。`reauth` 表示授权已失效，宿主把它当成需要重新授权，不把传输失败当成这个状态。`credential` 最多 16 个键，键必须是已声明的字段。

`credential` 里，secret 字段的值必须正好是 `{{secret.<同一字段名>}}`，由宿主从秘密袋取出。写成原始令牌会拒绝。非密钥字段是普通字符串，不能含 `{{secret.`，也不能含秘密袋里的值；`unix` 字段必须是非负十进制整数字符串。access token 字段必须出现并且非空。没写到的字段，刷新时保留上一份凭据里的值。

`account_summary` 的 `entries` 与[额度探测](./quota-probe.md)的 entry 相同。`remark` 和 `label` 可选。宿主仍会按秘密袋擦 `remark`、`label` 和 entry 的文案字段。`label` 只在 `credential.display` 含 `summary.label` 时写入 meta 的 `display`，并且不能超过 256 字节、不能含控制字符。`reauth` 是成功结果，不是钩子错误：宿主把凭据标成 `reauth_required`、`enabled` 设为 false，同步状态写成 `reauth_required`。`ok` 不会清掉已经标上的重新授权。网关看到上游 401 时，仍可以按账号凭据把这把标成需要重新授权。这不另起错误码。

`summarizeAccount` 对每个账号池包都必填。`startLogin` 与 `pollLogin` 只在 `login.kind` 为 `custom_poll` 时必填。`refreshTokens` 只在 `refresh.kind` 为 `custom_steps` 时必填。

`pollLogin` 每次从 `startLogin` 结束时的步骤基线重放。上一次轮询的响应不会留在 `steps` 里。Cookie 罐和秘密袋保留。

这些钩子不进 `xlyra-plugin test` 的样本，也不进 `xlyra-plugin run`。样本的 `hook` 仍然只能是 `probe`、`listModels`、`check`、`classify`、`rewritePayload`。

### 密钥不进 JS

秘密袋在宿主进程里，属于这一次登录、刷新或摘要，不在 goroutine 之间共享。

`login.secretPaths` 和 `refresh.secretPaths` 是对象：键是响应 JSON 的路径，值是 secret 字段名。路径是点分键加数字下标，例如 `data.accessToken`、`items[0].token`。键由 `[A-Za-z0-9_-]` 组成，下标是非负整数。不支持通配符。空段、`*`、`?`、空白会在校验清单时拒绝。值必须是已声明的 secret 字段。

响应交给 JS 之前，宿主按路径把字符串摘进秘密袋，`Body` 和解析后的 `JSON` 里该位置都换成固定占位 `{{redacted}}`。同一字段有多条路径时，按路径字典序，第一条非空字符串留在袋里，其余命中仍从文档里删掉。路径不存在不算错误，也不写袋。路径在但值不是非空字符串，这一步失败。声明了 `secretPaths` 时，响应必须是 JSON 对象；非 JSON（包括 HTML 错误页）、根是数组或标量，这一步失败，原文不交给 JS。唯一的例外是 `pollLogin` 发出的请求写了 `onNonJSON: "pending"`：响应不是单个 JSON 对象时，这一步当成 `pending`，正文不进 JS、不进错误、也不留在 `steps` 里。其它钩子即使写了这个字段也忽略它，仍然失败关闭。空正文、数组、标量、`null` 都算不是对象。响应头仍会去掉 `Set-Cookie`。

钩子在 `request` 里用 `{{secret.<field>}}` 引用袋中的值。`<field>` 是小写开头的 `[a-z][a-z0-9_]*`，必须是这次允许的 secret 字段，并且袋里已经有值。可以出现在：

- 请求体里的字符串
- query 的值
- 头的值

不能出现在 path、HTTP 方法、头名、query 的键、JSON 对象的键。头值代入后若含 CR 或 LF，这一步失败。其它 `{{...}}` 原样留下。替换前，URL 已经过 `hosts` 检查；替换后主机不得变化，path 里也不得出现密钥。日志记的是替换前的模板。

错误和日志会用秘密袋做替换，替换标记是 `[redacted]`。短于 4 个 Unicode 码位的值不参与这层擦除（一个 emoji 按一个码位计，不按字节）。声明路径上的值不论长短都会换成 `{{redacted}}`；同一个短值如果还出现在路径之外的日志或错误字符串里，擦不掉。请不要用短于 4 个字符的口令当 secret 字段。

登录 Cookie 存在宿主的 Cookie 罐里，按会话 id 放，不进 JS。

### 凭据类型

账号池凭据的 `credential_type` 是：

```text
account:<siteType>:<uuid>
```

`<siteType>` 是站点类型 ID（含一个 `/`，不含 `:`），写入前去掉首尾空白并改成小写。`<uuid>` 是标准 8-4-4-4-12 的十六进制 UUID。解析时，去掉 `account:` 前缀后，最后一个 `:` 之后是 uuid，其余是站点类型。解析失败的 `account:` 字符串不算账号凭据。内置的 `grok_sso:` 保持原样，不走这个前缀。

同一站点上 `UNIQUE(site_id, credential_type)` 靠 uuid 区分多账号。

### 登录会话

登录状态在进程内存里：自定义登录的 Cookie 罐、设备码、秘密袋都在发起 `startLogin` 的那一个进程。另一个实例上的轮询、进程重启、或者目录重载换掉了 runtime，会话都没了，轮询得到「会话不在这个进程」。

`custom_poll` 的会话从开始起 10 分钟后过期。设备码会话的过期时间来自提供方的 `expires_in`（缺省 600 秒，最长 1800 秒）。登录已经完成时，管理端在同一进程里还可以再取到结果，这份结果也保留 10 分钟。

### 出站

登录、刷新、摘要，以及 `models.perAccount` 的 `listModels`，发出的请求与这次的 `baseURL` 同源（协议、主机、端口；重定向也是，最多 10 次），并且主机在启用时确认的 `hosts` 里。`listModels` 这次的 `baseURL` 是账号区域的基址，区域解析和网关相同。区域 `baseURL`、设备码地址、`verificationURL` 同样要在 `hosts` 里。不通过则不把地址交给浏览器，也不把原文交给 JS。

网关转发仍用[出站限制](#出站限制)。账号凭据解析失败时换下一把，见[排错](#排错)。

### 当前限制

这些是框架现在做不到的事，对插件作者有影响的几条：

- 网关头模板不能写 `Content-Type` 和 `Accept`。这两项仍由协议适配器决定。
- `custom_poll` 的登录会话是 10 分钟。这是宿主的会话时钟，不会告诉上游。
- 短于 4 个 Unicode 码位的密钥值，出现在声明路径之外时不会被擦掉。
- 登录 Cookie 罐在进程内。另一个实例或重启之后，进行中的登录会话没有了。
- 管理端登录完成、导入之后、以及账号 Refresh 会立刻换令牌。`leadSeconds` 只在网关选路时生效。

## 启用与信任

启用 `site_provider` 有三条，缺一不可：

1. 包必须由受信公钥签名。未签名，或签名者不在受信列表里，返回 `js_plugin_untrusted_not_allowed`（HTTP 403）。`confirm_untrusted` 不能绕过。
2. 每次启用都要确认主机列表，包括升级后主机没变的那一次。请求体带 `confirm_hosts`，作为集合必须和清单 `hosts` 相同。没带、多了或少了，返回 `js_plugin_hosts_required`（HTTP 400），`error.hosts` 是清单里的完整列表。确认结果记成 `host:<主机名>`。
3. 已有站点使用旧的站点类型 ID 时，新版本不能改这个 ID。比较时忽略大小写。否则 `js_plugin_site_type_locked`（HTTP 409），响应带 `from`、`to`、`count`。

这种包不能绑定到单个站点。停用不受上面第 3 条限制。

## 钩子

下面五个是 `api_key` 和 `account_pool` 都可以导出的可选钩子。没导出就当没有这个钩子。账号池另外的 `startLogin`、`pollLogin`、`refreshTokens`、`summarizeAccount` 见[账号池站点](#账号池站点credentialmodelaccount_pool)，按 `login.kind` / `refresh.kind` 决定哪些必填。`meta.id`、`meta.kind`、`meta.apiVersion` 必须和清单一致。

时限和大小与同类钩子相同：

| 钩子 | 每次调用 | 说明 |
| --- | --- | --- |
| `probe`、`listModels`、`check` | 200 ms | 最多调用 6 次（最多 5 个 HTTP 请求）。单个请求体 64 KiB，交给插件的响应体 1 MiB，单次 HTTP 20 秒 |
| `classify`、`rewritePayload` | 50 ms | 在请求路径上。等待运行时的上限是 250 ms，钩子本身仍是 50 ms |
| 所有钩子 | 内存 16 MiB，返回值 1 MiB | 日志每次调用最多 20 条，每条 1024 字节 |

分步钩子的 `ctx` 是 `siteType`、`baseURL`、`credentialType`、`now`（毫秒）。没有密钥。`models.perAccount` 的 `listModels` 另外带 `account`，见 [按账号列模型](#按账号列模型)。`site_provider` 没有 `baseURLMode`，地址按 `as_is` 整理（去掉尾部 `/`）；没传地址时用 `officialBaseURL`。API Key 站点的 `listModels` 仍不带 `account`。

`listModels`、`check`、`classify` 的优先级：手工绑定的同类插件 > 站点类型自带钩子 > 通用适配器或默认分类。`probe` 不走这套绑定。站点上已经保存、并且能识别的 `quota_probe` 配置优先；没有配置，或者值是 `none`，又导出了 `probe` 时，自动用站点类型自己的钩子。

`listModels`、`check`、`probe` 失败（抛错、超时、返回 `{ error }`、主机不允许）时，这一次操作失败，不会悄悄改走通用适配器。

### 按账号列模型

`account_pool` 写了 `models.perAccount: true` 之后，宿主在这些时候为每个启用、且不需要重新授权的账号调用 `listModels`：登录完成、导入之后的刷新、账号 Refresh，以及站点的模型同步和 RefreshState。账号一个一个跑，沿用分步钩子的次数和超时（最多 6 次调用、单次 HTTP 20 秒）。一个账号失败不影响其它账号。全部这样的账号都失败时，这次站点同步失败。没有符合条件的账号时，同步返回空结果，不报错。

`ctx.account` 只有非密钥字段，再加上解析出的 `region`（凭据里已经有 `region` 时保留原值）。`ctx.baseURL` 是这个区域的基址，解析顺序和网关相同：显式 `region`，否则 `realm` 等于区域 id 或 `regions[].realms`，否则 `domain` 落在 `regions[].domains`，否则 `defaultRegion`。`ctx.credentialType` 是 `account`。

请求里用 `{{secret.access_token}}`，也可以用其它已声明的 `{{secret.<字段>}}`。宿主在主机检查通过之后、请求发出之前替换。JS 看不到令牌，出站只到 `hosts`。上游 HTTP 401 不把响应体交给钩子，这个账号被标成需要重新授权。

钩子返回的 `models` 按现有账号模型写入：每个模型 upsert 到该账号的模型行，再把各账号的并集写进站点模型表。站点模型的展示名是上游模型名。`capabilities` 按钩子返回的对象保存。`summarizeAccount` 仍然不返回模型。

没写 `models.perAccount` 的账号池站点不走这条路径，站点刷新记为 `partial` 并在 `sync_message` 说明跳过了模型同步。导出了 `listModels` 却没写 `perAccount` 的账号池清单不会被加载。`api_key` 的 `listModels` 行为不变。

### listModels

拉模型列表时调用。API Key 站点先看有没有绑定 `model_list`，没有再看本钩子，再没有就用通用适配器的 `GET models.path`，解析 OpenAI 形的 `data[].id`。账号池站点只在 `models.perAccount` 为 true 时调用本钩子，见 [按账号列模型](#按账号列模型)。

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

验 Key 和健康检查共用这个钩子，但账号池站点的健康检查不走这里：账号池按账号做摘要，见上文 quota.aggregate 后面的刷新与健康说明。API Key 站点先看有没有绑定 `credential_check`，没有再看本钩子，再没有就用通用适配器去请求模型列表（2xx 当成功）。

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
- `probe`、`listModels`、`check` 发出的请求要同时满足两件事：与站点 `base_url` 同源（协议、主机、端口相同；重定向也是，最多 10 次），以及解析后的 URL 通过上面的主机名检查。路径规则与[资源上限](./limits-and-security.md)里的插件 URL 相同，越界是 `js_url_rejected`。账号池 `models.perAccount` 的 `listModels` 改为与该账号的区域基址同源，主机仍必须在 `hosts` 里。令牌写在 `{{secret.access_token}}` 里，由宿主替换。

站点类型已停用或不在启用快照里时，没有白名单。类型名含 `/` 的出站会被拒绝。内置站点类型不查这张表。

钩子的重定向检查是同源，不会在每一跳上再调用一次主机白名单。同源已经把主机和端口钉在站点 `base_url` 上，而发请求前的 URL 已经查过白名单。

## 生命周期

停用后，这种站点的数据还在，但 `SiteTypeAvailable` 为假：不参与路由，也不出现在下游模型目录。站点列表的 `provider_status` 为 `unavailable`，界面显示「提供方插件未启用」。启用中的插件站点类型是 `enabled`。内置站点类型没有这个字段。

仍有站点使用该插件任一版本声明过的站点类型时，不能卸载整个插件，也不能删除它的最后一个版本，错误是 `js_plugin_in_use`（HTTP 409），响应带 `site_type`、`count`。先删掉这些站点。停用不受这个限制。删除的不是最后一个版本时，不做这项检查。

多实例部署下，启用、停用和删除大约每 5 秒同步一次，不用重启。

熔断对 `site_provider` 只累计调用和失败，不会停用版本。一个坏钩子不会被自动摘掉，需要管理员手动停用。其它 kind 的自动停用规则见[限制与安全](./limits-and-security.md#自动保护熔断)。

## 安全注意

插件拿不到密钥明文，也不能自己发请求。分步钩子只描述 `request`，鉴权头由 xLyra 加上。`rewritePayload` 的 `ctx` 里没有密钥、请求头或账号字段；密钥是在正文改写之后才放进请求头的。账号池钩子只能看到 `{{redacted}}` 和 `{{secret.<field>}}`，见[密钥不进 JS](#密钥不进-js)。

宿主写出 `probe`、`listModels`、`check`、`rewritePayload` 的日志时不脱敏，这些日志会进服务日志。`classify` 这次调用收集到的日志会被丢掉。账号池的 `startLogin`、`pollLogin`、`refreshTokens`、`summarizeAccount`，以及 `models.perAccount` 的 `listModels`，在写日志之前先按秘密袋替换；短于 4 个 Unicode 码位的值替换不到。`rewritePayload` 能看到用户消息正文，请不要把用户消息打进日志。

请只启用可信来源的插件。未受信的 `site_provider` 不能靠二次确认启用。

## 已知限制

账号池做不到的事见[当前限制](#当前限制)。下面这些对两种凭据模型都成立：

- 插件图标：`display.icon` 声明的 PNG 或 WebP 由 `GET /api/v1/plugin-icons/{pluginID}` 提供。这个接口在管理端会话路由组里，浏览器要带 Cookie `xlyra_admin_session`；停用后仍可访问，插件删除后返回 404。站点类型的 `icon_url` 是 `/api/v1/plugin-icons/<pluginID>?v=<包 SHA256 的前 16 位>`，换包后 `v` 会变。没有声明图标时 `icon_url` 仍为空，创建页用显示名的首字符做占位。创建页先查内置图标映射，插件类型没有映射，于是用 `icon_url`。
- `display.brandColor` 只校验 `#RRGGBB`，不会送到创建页。
- 创建页的额度探测下拉只对 `openai` 和 `anthropic` 出现，倍率只对 `openai`、`anthropic`、`google_gemini` 出现。插件站点类型的倍率能力固定为关。创建时写入的额度探测值是 `none`，服务端把它当成没配置；导出了 `probe` 就会自动探测。
- 显示名和副标题按界面语言展示，见 [display](#display)。`display_name` / `subtitle` 字符串仍按 `zh`、然后 `en`、再按语言标签字母序取第一个非空值。
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
| `plugin_header_template_failed` | 502 | 请求头模板渲染失败，请求未发出，不换密钥。错误信息有头名，不含密钥的值 |
| `request_param_validation_failed` | 400 | `requestParams.validation` 没通过。这是改写成功之后的参数校验 |
| `plugin_account_credential_invalid` | — | 账号凭据包解不开、缺少 access token、区域基址不可用，或账号池未配置。换下一把凭据，不标重新授权 |
| `plugin_account_refresh_failed` | — | 网关选路时刷新失败，或刷新器没配上。换下一把凭据。只有错误是需要重新授权时才标重新授权 |

管理端账号接口对插件账号池和内置账号池用同一组错误码：`account_login_start_failed`（502）、`account_login_failed`（400）、`account_refresh_failed`（400）、`account_import_failed`（400）、`account_import_unsupported`（404，这种站点类型没有打开导入）。

钩子运行失败仍使用 `js_timeout`、`js_shape_error`、`js_exception`、`js_url_rejected`、`js_plugin_response_too_large` 等类型，格式见[错误码](./errors.md)。
