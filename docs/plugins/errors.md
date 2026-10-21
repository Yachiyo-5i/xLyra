# 错误码

插件运行失败时，错误会带上一个稳定的类型，出现在 xLyra 的服务日志、额度刷新的错误信息和网关结果里。

| 类型 | 含义 | 常见原因 |
| --- | --- | --- |
| `js_timeout` | 钩子超过时限被中断 | 死循环、重计算；额度探测 200 ms，协议 50 ms |
| `js_heap_limit` | 超过内存上限被中断 | 构造了巨大的字符串或数组 |
| `js_shape_error` | 返回值结构不对 | 多了不认识的字段；`probe` 同时返回了 `request` 和 `result`；必填字段缺失或类型错误；`entries` 项没有 `label` |
| `js_exception` | 插件抛出了未捕获的异常 | 访问了 `undefined` 的属性；宿主函数参数错误且没接住 |
| `js_pool_exhausted` | 运行时池里没有可用实例 | 并发太高而单次调用太慢 |
| `js_url_rejected` | 请求被判定为越界 | `path` 不合法或指向站点之外 |
| `js_plugin_response_too_large` | 上游响应过大 | 超过 1 MiB（探测）或 4 MiB（协议） |
| `js_internal` | xLyra 内部错误 | 不是插件的问题，请反馈 |

## 报错信息的读法

格式是 `类型: 详情`，例如：

```text
js_shape_error: result.entries[0].label: expected string
js_shape_error: request: unknown field "url"
js_exception: boom
js_timeout: interrupted: timeout
```

详情里的路径（`result.entries[0].label`）指向返回值里出问题的位置。

## 上传和校验时的错误

`xlyra-plugin pack`、`verify` 以及管理端上传会给出这些常见错误：

| 信息 | 原因 |
| --- | --- |
| `invalid plugin id` | id 不符合规则 |
| `uploaded plugins cannot use the xlyra. prefix` | id 以 `xlyra.` 开头 |
| `plugin.js sha256 does not match the manifest` | 手动改过 `plugin.js` 而没有重新 `build` |
| `meta.id ... does not match manifest` | `meta` 与清单不一致 |
| `at least one fixtures/*.json is required` | 没有测试样本 |
| `apiVersion N is not supported` / `hostApi N is not supported` | 声明的版本比 xLyra 支持的高 |
| `kind "..." is not supported in this build (supported: ...)` | `kind` 不是支持的十种之一，错误信息里会列出所有支持的类型 |
| `result.xxx: ... is not one of ...` | 返回的值不在允许的取值里（如 `status`、`class`） |
| `result.models[N].id: "..." was not in the input` | `model_metadata` 返回了输入里没有的模型 |
| `input is required` | 一次性函数类型的样本缺少 `input` |
| `selftest: ...` | 样本没通过 |
| `signature: ...` | 签名无效，或包在签名后被改动 |
| `plugin version already exists with different package content` | 同 id 同版本已上传过，但内容不同；请提高版本号 |

## 启用时的错误

| 错误码 | 含义 |
| --- | --- |
| `js_plugin_confirm_required` | 包未签名或签名者不在受信列表里，需要管理员二次确认。不适用于 `site_provider` |
| `js_plugin_untrusted_not_allowed` | `site_provider` 未由受信密钥签名。`confirm_untrusted` 不能绕过，需先导入对应公钥。HTTP 403 |
| `js_plugin_hosts_required` | 启用 `site_provider` 需要确认主机列表。响应 `error.hosts` 为完整列表。HTTP 400 |
| `js_plugin_kind_not_connected` | 类型已定义但 xLyra 尚未接入，不能启用。目前十种类型都已接入，只有以后新增的类型在接入前才会出现 |
| `js_plugin_pricing_review_required` | 绑定价格解析插件时没有确认预览结果 |
| `js_plugin_permissions_required` | 启用的 `automation` 版本声明了权限，但管理员没有授予。需要在启用请求里带上 `grant_permissions` |
| `js_plugin_automation_invalid` | 新增或修改自动化绑定时，所选对象或参数不符合插件声明的输入，错误信息里会说明原因 |
| `js_plugin_site_type_locked` | 启用的 `site_provider` 新版本改了站点类型 ID，而旧类型仍有站点在用。HTTP 409，响应带 `from`、`to`、`count` |

## 卸载时的错误

| 错误码 | 含义 |
| --- | --- |
| `js_plugin_in_use` | 卸载插件，或删除它的最后一个版本时，仍有站点使用它声明的站点类型。HTTP 409，响应带 `site_type`、`count`。先删除这些站点再卸载。停用不受这个限制 |

## 网关转发时的错误

这些错误出现在网关结果里。HTTP 都是 502，请求不会发到上游，也不会换下一把密钥重试：

| 错误码 | 含义 |
| --- | --- |
| `plugin_rewrite_failed` | 站点类型的 `rewritePayload` 失败（抛错、超时、形状不对、改了 `model` 或 `stream`、正文超过 1 MiB）。错误信息里带插件 id |
| `upstream_host_not_allowed` | 插件站点类型的上游主机不在启用时确认的列表里，或该类型已停用 |
| `plugin_header_template_failed` | 插件站点的请求头模板渲染失败（变量不存在，或结果含 CR、LF、其它控制字符）。错误信息里有头名，不含密钥的值 |

## 账号池凭据

这些错误出现在网关为插件账号池解析凭据时。这一次凭据失败，并尝试下一把。全部失败时请求失败。

| 错误码 | 含义 |
| --- | --- |
| `plugin_account_credential_invalid` | 凭据包解不开、缺少 access token、区域基址不可用，或该站点类型的账号池没配上。不标重新授权 |
| `plugin_account_refresh_failed` | 距到期不足 `leadSeconds` 时刷新失败，刷新没有返回字段，或刷新器没配上。只有刷新错误表示需要重新授权时，才把这把凭据标成要重新授权 |

上游 401 和传输失败是否换凭据，按凭据类型是不是账号凭据判断（含 `account:<siteType>:<uuid>`，以及内置的 `grok_sso:`）。API Key 不走这条。

`summarizeAccount` 返回 `status: "reauth"` 时不是上面的网关错误。宿主把这把凭据标成 `reauth_required` 并停用，不另起错误码。

管理端账号接口（插件账号池与内置账号池共用）：

| 错误码 | HTTP | 含义 |
| --- | --- | --- |
| `account_login_start_failed` | 502 | 开始登录失败 |
| `account_login_failed` | 400 | 轮询登录失败 |
| `account_refresh_failed` | 400 | 刷新账号失败 |
| `account_import_failed` | 400 | 导入文件被拒绝 |
| `account_import_unsupported` | 404 | 这种站点类型没有打开导入 |
