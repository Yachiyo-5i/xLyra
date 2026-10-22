# 清单 manifest.json

每个插件包都有一个 `manifest.json`。`xlyra-plugin build` 会自动填写 `sha256`，你不用手写。

```json
{
  "apiVersion": 1,
  "hostApi": 1,
  "id": "my-relay-probe",
  "kind": "quota_probe",
  "name": "My Relay probe",
  "description": "Quota probe for My Relay",
  "version": "0.1.0",
  "license": "MIT",
  "xlyra": ">=1.14.0",
  "quotaProbe": { "baseURLMode": "as_is" }
}
```

## 通用字段

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `id` | 是 | 3–64 位，只能含小写字母、数字、`.`、`-`，首尾必须是字母或数字。**不能以 `xlyra.` 开头**（保留给内置插件），不能是 `builtins` |
| `kind` | 是 | 十种之一：`quota_probe`、`protocol`、`model_list`、`credential_check`、`site_detect`、`error_classifier`、`model_metadata`、`pricing_parse`、`automation`、`site_provider`，详见[选择类型](./kinds.md)。不支持自定义类型 |
| `version` | 是 | 语义化版本，如 `0.1.0`。同一个 id 的同一个版本号不能用不同内容重复上传 |
| `apiVersion` | 是 | 钩子契约版本，目前只能是 `1` |
| `hostApi` | 是 | 你用到的宿主 API 最低版本，目前是 `1`。高于当前 xLyra 支持的版本会被拒绝 |
| `name`、`description` | 否 | 管理端展示 |
| `license` | 否 | 你为插件选的许可证，管理端展示，xLyra 不做判断 |
| `xlyra` | 否 | 兼容的 xLyra 版本。只支持 `>=x.y.z`，可用空格写多个条件。当前 xLyra 版本低于要求时，插件会被拒绝上传或加载（本地开发版本不检查） |
| `sha256` | 自动 | `{"plugin.js": "<hex>"}`，必须与包内 `plugin.js` 一致，否则拒绝 |

## quota_probe 部分

```json
"quotaProbe": { "baseURLMode": "trim_v1", "defaultBaseURL": "https://api.example.com" }
```

| 字段 | 说明 |
| --- | --- |
| `baseURLMode` | 站点地址传给插件前如何整理，不填等于 `as_is` |
| `defaultBaseURL` | 站点没配置地址时使用 |
| `replaces` | 只有内置插件能用，第三方设置会被拒绝 |

`baseURLMode` 取值：

| 值 | 效果 |
| --- | --- |
| `as_is` | 去掉尾部 `/`，其余不变 |
| `trim_v1` | 去掉尾部 `/`，再去掉末尾的 `/v1` |
| `trim_v1_fold` | 同上，但 `/v1` 不区分大小写 |
| `origin` | 只保留 `协议://主机:端口` |

## 站点类 kind 的 site 部分

`model_list`、`credential_check`、`site_detect`、`pricing_parse` 使用 `site` 部分，字段与 `quotaProbe` 相同（没有 `replaces`），`pricing_parse` 多一个 `pricingPath`：

```json
"site": { "baseURLMode": "trim_v1", "defaultBaseURL": "https://api.example.com" }
```

不写 `baseURLMode` 等于 `as_is`。`pricing_parse` 也使用 `site`，并且必须再写 `pricingPath`，见[价格解析](./pricing-parse.md)。`error_classifier`、`model_metadata` 没有额外的清单部分。

## automation 部分

`automation` 使用 `automation` 部分，声明订阅的事件、需要的权限，以及绑定表单：每个输入的数据类型、控件（`select`、`multi_select`、`radio`、`input`、`switch` 等）和文案。xLyra 按这份声明渲染表单，插件不写界面代码。详见[自动化插件](./automation.md#输入inputs)。

## protocol 部分

```json
"protocol": {
  "name": "acme_proto",
  "downstreamPath": "/v1/plugins/acme_proto",
  "endpointType": "acme_proto",
  "method": "POST",
  "auth": "bearer",
  "defaultBaseURL": "https://api.acme.example"
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 协议名 |
| `downstreamPath` | 是 | 声明用，第三方协议的实际下游路径固定是 `/v1/plugins/<slug>`，`slug` 由管理员分配 |
| `endpointType` | 是 | 端点类型名，用于限流、诊断与录制 |
| `method` | 否 | 上游 HTTP 方法，默认 `POST` |
| `auth` | 否 | `bearer`（默认）、`none`，或 `header:<Header-Name>` |
| `defaultBaseURL` | 否 | 上游没配置地址时使用 |

## siteProvider 部分

`site_provider` 用 `siteProvider` 声明一个站点类型，凭据模型是 `api_key` 或 `account_pool`。一个包只声明一个站点类型，`siteType` 的斜线左侧必须等于本包的 `id`。这种包不能再写 `quotaProbe`、`site`、`protocol`、`automation`。下面的例子是 `api_key`。`account_pool` 的字段、校验和钩子见[账号池站点](./site-provider.md#账号池站点credentialmodelaccount_pool)。

```json
"siteProvider": {
  "siteType": "acme.example/chat",
  "display": {
    "name": { "zh": "Acme", "en": "Acme" },
    "subtitle": { "zh": "API Key 站点", "en": "API key site" },
    "icon": "assets/icon.png",
    "brandColor": "#0052D9"
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
  "models": { "path": "/v1/models", "auth": "bearer" },
  "request": {
    "headers": {
      "Authorization": "Bearer {{cred.api_key}}",
      "X-Client": "xlyra"
    },
    "stripDownstreamHeaders": true
  }
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `siteType` | 是 | `<id>/<name>`。左侧等于清单 `id`（3–64 位，小写字母、数字、`.`、`-`，首尾是字母或数字）；右侧以小写字母开头，后面最多 31 位小写字母、数字或 `_` |
| `display.name` | 是 | 展示名。字符串，或按语言分的对象（最多 8 种语言，每种不超过 400 字节）。管理端按界面语言展示：精确匹配语言码，再匹配前缀（`zh-CN` 用 `zh`）；日文界面同时认 `ja` 和 `jp`。没有当前语言时用 `en`，再没有用 `zh`，还没有则用其余非空值。建议至少写 `zh` 和 `en` |
| `display.subtitle` | 否 | 副标题，规则与 `name` 相同 |
| `display.icon` | 否 | 包内相对路径，扩展名只能是 `.png` 或 `.webp`。文件必须存在，文件头要符合格式，不超过 64 KiB。声明之后，管理端用 `GET /api/v1/plugin-icons/{插件 id}` 取这张图，需要管理端会话。站点类型的 `icon_url` 带 `?v=`，值是包 SHA256 的前 16 位，换包后变化，用来让浏览器重新取图 |
| `display.brandColor` | 否 | `#RRGGBB` |
| `hosts` | 是 | 1–16 个小写 DNS 主机名，不能重复。不能带协议、端口、路径，至少含一个 `.`，不能是 IP，不能以 `.` 或 `-` 开头或结尾 |
| `credential.model` | 是 | `api_key` 或 `account_pool`。`api_key` 不能再写 `credential.fields`、`accessTokenField`、`refreshTokenField`、`expiresAtField`、`expiresFrom`、`identity`、`display`、`regions`、`defaultRegion`、`login`、`refresh`、`import`、`selection`、`ui`。`account_pool` 的 `models` 只能是 `perAccount: true`，或者整段省略 |
| `protocol.kind` | 是 | `openai_chat`、`openai_responses`、`anthropic_messages`、`google_gemini` 之一 |
| `protocol.path` | 是 | 以单个 `/` 开头的路径，不能含 `..` 或编码后的 `..` |
| `protocol.officialBaseURL` | 是 | `https` URL，必须有主机，不能有用户信息、查询或片段。主机（忽略大小写，端口不算）必须在 `hosts` 里 |
| `protocol.defaultEndpointTypes` | 是 | 至少一项，每项是 `openai`、`openai-response`、`anthropic-messages`、`google-gemini` 之一。只检查取值，不和 `protocol.kind` 做对应推断。通用适配器列模型时把它写进每个模型的 `supported_endpoint_types`；导出了 `listModels` 时以钩子返回的 `capabilities` 为准 |
| `protocol.requestParams` | 否 | 可选。允许的键是 `unsupported`、`allowed_values`、`defaults`、`fixed`、`forced`、`validation`、`supported`。`supported` 只被合并，不过滤参数。不认识的键会让这份规格校验失败，插件被跳过。各键是否改正文见[站点类型](./site-provider.md#requestparams) |
| `models.path` | 否 | 只用于 `api_key`。拉模型列表的路径，不写等于 `/v1/models`。写了就要符合 `protocol.path` 的路径规则。没有 `listModels` 钩子时，通用适配器用它。`account_pool` 写 `path` 会被拒绝 |
| `models.auth` | 否 | 只用于 `api_key`。不写或 `bearer` 表示 Bearer；也可以是 `header:<Name>`，`Name` 是非空的 HTTP 头字段名。只用于通用适配器的列模型、验 Key 和健康检查，不决定网关转发的头。`account_pool` 写 `auth` 会被拒绝 |
| `models.perAccount` | 否 | 只用于 `account_pool`。为 true 时必须导出 `listModels`，宿主按账号调用它并写入模型。不能和 `path`、`auth` 一起写。`api_key` 写了会被拒绝。省略 `models` 表示不做按账号同步。空对象 `{}` 会被拒绝 |
| `request.headers` | 否 | 最多 16 项。值是模板字符串，或 `{ "value", "when" \| "unless" }`。`when` / `unless` 互斥，只能是 `cred.<已声明字段>`；条件不成立就省略该头。头名是 HTTP token，忽略大小写后不能重复。不能是 `Host`、`Content-Length`、`Transfer-Encoding`、`Connection`、`Upgrade`、`TE`、`Trailer`、`Proxy-Authorization`、`Proxy-Connection`、`Cookie`、`Set-Cookie`、`Content-Type`、`Accept`，也不能是 `X-Forwarded-*`。`Authorization` 可以写。模板只允许 `{{cred.<field>}}`、`{{region.origin}}`、`{{region.referer}}` 和 `{{site.baseURL}}`，花括号内不能有空格。`region` 键必须在每个区域的 `attrs` 里。`api_key` 的 `<field>` 只有 `api_key`，且不能用 `region`。`account_pool` 的 `<field>` 是已声明的凭据字段名；网关运行时只填入非密钥字段、access token 字段和区域属性。字面量不能含 CR、LF 或其它控制字符。这里声明 `Authorization` 不会和 `models.auth` 冲突。规则见 [请求头模板](./site-provider.md#请求头模板) |
| `request.stripDownstreamHeaders` | 否 | 默认 `false`。为 `true` 时，转发前只保留模板声明的头，外加 `Content-Type` 和 `Accept` |
| `request.upstreamStreamOnly` | 否 | 默认 false。为 true 时强制上游 `stream: true`；下游没要流且上游是 chat SSE 时聚合响应 |
| `request.baseURLFrom` | 否 | 空，或 `region`。`region` 要求已声明 `regions`，用账号区域的 `baseURL` |
| `quota.aggregate` | 否 | `sum`、`min`、`none`。账号池不写默认 `sum`。API Key 不写则额度探测保持原来的最小剩余汇总 |
| `credential.fields` | 账号池必填 | 1–16 项。`name` 匹配 `^[a-z][a-z0-9_]{0,31}$`。`type` 是 `string`（默认）或 `unix`。至少一项 `secret: true` |
| `credential.accessTokenField` | 否 | 默认 `access_token`，必须是 secret 字段 |
| `credential.refreshTokenField` | `oauth2_refresh` 时必填 | 必须是 secret 字段 |
| `credential.expiresAtField` | 否 | 必须是 `type: unix` 的字段。不写则网关不按到期时间自动刷新 |
| `credential.expiresFrom` | 否 | 只允许 `claims.exp`，且必须有 `expiresAtField`。到期字段为空时才从 JWT `exp` 填 Unix 秒 |
| `credential.identity` | 账号池必填 | 一个字符串，或最多 8 个不重复的字符串。每项是 `claims.<路径>` 或 `field.<非密钥字段名>`。取第一个非空 |
| `credential.display` | 否 | 与 `identity` 相同的列表，另外可以有一条 `summary.label`。行上的显示名 |
| `regions` | 账号池必填 | 1–8 项。`id` 匹配 `^[a-z][a-z0-9_-]{0,15}$`。`baseURL` 是 `hosts` 里的 `https`。`realms`、`domains` 各最多 8 项。`attrs` 最多 2 项，键只能是 `origin`、`referer`，值 1–256 字节 |
| `defaultRegion` | 账号池必填 | 必须是某个 `regions[].id` |
| `login.kind` | 账号池必填 | `oauth2_device_code` 或 `custom_poll`。设备码要 `deviceCodeURL`、`tokenURL`、`clientID`；`grantType` 空着等于 `urn:ietf:params:oauth:grant-type:device_code`。`custom_poll` 不能写这些 OAuth 字段，并且 `secretPaths` 必须映射到 access token 字段 |
| `refresh.kind` | 账号池必填 | `oauth2_refresh`、`custom_steps` 或 `none`。`leadSeconds` 在 30–3600，不写时前两种用 300；`none` 不能写 `leadSeconds`、`secretPaths`、`tokenURL` |
| `import` | 否 | `enabled`、`containers`（最多 8）、`fieldMap`（每个字段 1–8 个别名）、`valueMap`（最多 8 个字段，每个 1–8 条精确替换）、`msThreshold`（不写等于 1e12，写出时为 1 到 1e15）。单文件 4 MiB、500 条 |
| `selection` | 否 | `ordered`（默认）或 `quota_weighted` |
| `ui` | 否 | `rowPills` 最多 4，字段必须非密钥。`operations` 为 `add`、`reauth`、`refresh`、`delete`、`toggle`、`models`、`import`。`texts` 的键见[账号池站点](./site-provider.md#ui) |

`probe`、`listModels`、`check`、`classify`、`rewritePayload` 都是可选导出。账号池还要导出 `summarizeAccount`；`login.kind` 为 `custom_poll` 时再导出 `startLogin`、`pollLogin`；`refresh.kind` 为 `custom_steps` 时再导出 `refreshTokens`；`models.perAccount` 为 true 时再导出 `listModels`。调用时机和返回值见[站点类型](./site-provider.md)。

启用这个类型时，包必须由受信密钥签名，并且每次启用都要再次确认完整的 `hosts`。确认结果记成 `host:<主机名>`。确认之后，站点 `base_url`、通用适配器发出的请求和网关转发的上游地址，主机都必须在这份列表里：按主机名精确匹配（不含端口，不做子域通配），重定向也不得离开列表。分步钩子在同源限制之外再做一次同样的主机检查。

## 包里的文件

`.xlp` 就是一个 zip，里面有：

```text
manifest.json
plugin.js              打包后的单文件 ES 模块
fixtures/*.json        除 site_provider 外至少一个
signature.json         可选，签名后才有
```

整个包不能超过 2 MiB，文件数不能超过 64 个。除 `site_provider` 外，至少要有一个 `fixtures/*.json`。`site_provider` 可以没有样本；有样本时每个文件都要带 `hook`，见 [fixtures](./fixtures.md#site_provider-样本)。
