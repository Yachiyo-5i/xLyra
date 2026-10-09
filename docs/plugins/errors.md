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
| `kind "..." is not supported in this build (supported: ...)` | `kind` 不是支持的八种之一，错误信息里会列出所有支持的类型 |
| `result.xxx: ... is not one of ...` | 返回的值不在允许的取值里（如 `status`、`class`） |
| `result.models[N].id: "..." was not in the input` | `model_metadata` 返回了输入里没有的模型 |
| `input is required` | 一次性函数类型的样本缺少 `input` |
| `selftest: ...` | 样本没通过 |
| `signature: ...` | 签名无效，或包在签名后被改动 |
| `plugin version already exists with different package content` | 同 id 同版本已上传过，但内容不同；请提高版本号 |

## 启用时的错误

| 错误码 | 含义 |
| --- | --- |
| `js_plugin_confirm_required` | 包未签名或签名者不在受信列表里，需要管理员二次确认 |
| `js_plugin_kind_not_connected` | 类型已定义但 xLyra 尚未接入，不能启用。目前八种类型都已接入，只有以后新增的类型在接入前才会出现 |
| `js_plugin_pricing_review_required` | 绑定价格解析插件时没有确认预览结果 |
