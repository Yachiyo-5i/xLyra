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

`site_provider` 用 `siteProvider` 声明一个 API Key 站点类型。一个包只声明一个站点类型，`siteType` 的斜线左侧必须等于本包的 `id`。这种包不能再写 `quotaProbe`、`site`、`protocol`、`automation`。

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
  "models": { "path": "/v1/models", "auth": "bearer" }
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `siteType` | 是 | `<id>/<name>`。左侧等于清单 `id`（3–64 位，小写字母、数字、`.`、`-`，首尾是字母或数字）；右侧以小写字母开头，后面最多 31 位小写字母、数字或 `_` |
| `display.name` | 是 | 展示名。字符串，或按语言分的对象（最多 8 种语言，每种不超过 400 字节） |
| `display.subtitle` | 否 | 副标题，规则与 `name` 相同 |
| `display.icon` | 否 | 包内相对路径，扩展名只能是 `.png` 或 `.webp`。文件必须存在，文件头要符合格式，不超过 64 KiB |
| `display.brandColor` | 否 | `#RRGGBB` |
| `hosts` | 是 | 1–16 个小写 DNS 主机名，不能重复。不能带协议、端口、路径，至少含一个 `.`，不能是 IP，不能以 `.` 或 `-` 开头或结尾 |
| `credential.model` | 是 | 只能是 `api_key` |
| `protocol.kind` | 是 | `openai_chat`、`openai_responses`、`anthropic_messages`、`google_gemini` 之一 |
| `protocol.path` | 是 | 以单个 `/` 开头的路径，不能含 `..` 或编码后的 `..` |
| `protocol.officialBaseURL` | 是 | `https` URL，必须有主机，不能有用户信息、查询或片段。主机（忽略大小写，端口不算）必须在 `hosts` 里 |
| `protocol.defaultEndpointTypes` | 是 | 至少一项，每项是 `openai`、`openai-response`、`anthropic-messages`、`google-gemini` 之一。只检查取值，不和 `protocol.kind` 做对应推断。通用适配器列模型时把它写进每个模型的 `supported_endpoint_types`；导出了 `listModels` 时以钩子返回的 `capabilities` 为准 |
| `protocol.requestParams` | 否 | 可选。允许的键是 `unsupported`、`allowed_values`、`defaults`、`fixed`、`forced`、`validation`、`supported`。`supported` 只被合并，不过滤参数。不认识的键会让这份规格校验失败，插件被跳过。各键是否改正文见 [API Key 站点类型](./site-provider.md#requestparams) |
| `models.path` | 否 | 拉模型列表的路径，不写等于 `/v1/models`。写了就要符合 `protocol.path` 的路径规则。没有 `listModels` 钩子时，通用适配器用它 |
| `models.auth` | 否 | 不写或 `bearer` 表示 Bearer；也可以是 `header:<Name>`，`Name` 是非空的 HTTP 头字段名 |

`probe`、`listModels`、`check`、`classify`、`rewritePayload` 都是可选导出，调用时机和返回值见 [API Key 站点类型](./site-provider.md)。

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
