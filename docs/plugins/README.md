# xLyra 插件开发文档

xLyra 的 JS 插件只做**纯计算**：把上游的响应翻译成 xLyra 能用的结构，或把下游请求翻译成上游请求。插件的类型（kind）由 xLyra 定义，开发者从[现有类型](./kinds.md)里选择；九种类型都已接入 xLyra。HTTP 请求、密钥、冷却、计费都留在 xLyra 的 Go 代码里，插件拿不到密钥，也不能自己发请求。

当前版本：`apiVersion 1`，`hostApi 1`。

## 阅读顺序

| 文档 | 内容 |
| --- | --- |
| [快速开始](./quickstart.md) | 从 `init` 到上传启用的完整流程 |
| [选择插件类型](./kinds.md) | 九种 kind 的用途、状态，以及怎么选 |
| [额度探测](./quota-probe.md) | `quota_probe` |
| [协议](./protocol.md) | `protocol` |
| [模型列表](./model-list.md) | `model_list` |
| [密钥校验](./credential-check.md) | `credential_check` |
| [站点识别](./site-detect.md) | `site_detect` |
| [错误分类](./error-classifier.md) | `error_classifier` |
| [模型元数据](./model-metadata.md) | `model_metadata` |
| [价格解析](./pricing-parse.md) | `pricing_parse` |
| [自动化](./automation.md) | `automation`：响应 xLyra 事件，返回由 xLyra 执行的动作 |
| [清单 manifest.json](./manifest.md) | 字段、校验规则 |
| [fixtures 测试样本](./fixtures.md) | 样本格式与比对规则 |
| [宿主 API](./host-api.md) | 插件里可以调用的全局函数 |
| [资源上限与安全规则](./limits-and-security.md) | 超时、内存、URL 与请求头限制，为什么拿不到密钥 |
| [错误码](./errors.md) | 运行时错误类型 |
| [命令行工具](./cli.md) | `xlyra-plugin` 全部子命令 |
| [发布、签名与安装](./publishing.md) | 打包、签名、管理员上传与启用 |
| [版本与兼容](./compatibility.md) | 版本字段与升级规则 |

## 示例

仓库里的内置插件就是官方示例，写法覆盖单请求、多步回退、金额换算和协议透传：

- 额度探测：`server/internal/jsplugin/builtin/xlyra.quota.*`（其余类型暂无内置示例，请用 `xlyra-plugin init --kind <类型>` 生成的模板）（`src/plugin.js` 是源码，`fixtures/` 是样本）
- 协议：`server/internal/jsplugin/builtin/xlyra.protocol.typesafe-systemone`

内置插件用到的辅助函数在 `builtin/_shared/helpers.js`，由构建脚本拼进 `plugin.js`。你自己的插件需要的话，直接写在自己的源码里。

## 不支持的事

- 流式响应：协议插件只处理非流式请求。
- 由插件决定选路、价格、计费或冷却。`automation` 只能返回 xLyra 开放的几种动作，且需要管理员授予权限、绑定对象。
- 访问环境变量、文件系统或任意主机。
- 租户上传插件，插件只能由管理员上传。
