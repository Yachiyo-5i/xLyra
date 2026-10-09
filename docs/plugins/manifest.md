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
| `kind` | 是 | 八种之一：`quota_probe`、`protocol`、`model_list`、`credential_check`、`site_detect`、`error_classifier`、`model_metadata`、`pricing_parse`，详见[选择类型](./kinds.md)。不支持自定义类型 |
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

## 包里的文件

`.xlp` 就是一个 zip，里面有：

```text
manifest.json
plugin.js              打包后的单文件 ES 模块
fixtures/*.json        至少一个
signature.json         可选，签名后才有
```

整个包不能超过 2 MiB，文件数不能超过 64 个，且至少要有一个 `fixtures/*.json`。
