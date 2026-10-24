# 选择插件类型（kind）

`manifest.json` 里的 `kind` 决定了插件做什么、要导出哪些函数、返回什么结构。**kind 由 xLyra 定义，开发者不能自己发明新类型**：每种类型的返回值怎么读、结果交给谁，都是 xLyra 里写死的，这样才能保证插件拿不到密钥、不能改选路和计费。

在命令行里也可以查看：

```bash
xlyra-plugin kinds
```

## 当前支持的类型

| kind | 解决什么问题 | 导出的函数 | 凭据 | 作用域与生效方式 |
| --- | --- | --- | --- | --- |
| [`quota_probe`](./quota-probe.md) | 查余额、额度、套餐 | `probe` | 站点 API Key | 站点：启用后绑定到站点 |
| [`protocol`](./protocol.md) | 接入接口格式非标准的上游（仅非流式） | `decodeRequest`、`buildRequest`、`parseResponse`；可选 `signRequest`、`parseError` | 站点 API Key | 下游路径：启用后分配 `/v1/plugins/<slug>` |
| [`model_list`](./model-list.md) | 站点拉模型列表的接口格式特殊 | `listModels` | 站点 API Key | 站点：启用后绑定到站点 |
| [`credential_check`](./credential-check.md) | 判断密钥是否有效、站点是否可用 | `check` | 站点 API Key | 站点：启用后绑定到站点 |
| [`site_detect`](./site-detect.md) | 根据地址识别是不是某类站点 | `detect` | 无 | 全局：启用即对所有站点的识别生效 |
| [`error_classifier`](./error-classifier.md) | 上游错误格式特殊，需要告诉 xLyra 这是哪一类失败 | `classify` | 无 | 站点：启用后绑定到站点 |
| [`model_metadata`](./model-metadata.md) | 上游模型名对应的名称与能力 xLyra 不认识 | `describeModels` | 无 | 全局：启用即对所有站点生效 |
| [`pricing_parse`](./pricing-parse.md) | 站点公布的价格表格式特殊 | `parsePricing` | 站点 API Key（由 xLyra 取价格表） | 站点：启用后预览核对，再绑定到站点 |
| [`automation`](./automation.md) | xLyra 里发生某件事时自动做点什么，比如额度重置后清零密钥用量 | `handle` | 无 | 对象：启用时授予权限，再按插件声明的表单绑定到 OAuth 账号、API 密钥或站点 |
| [`site_provider`](./site-provider.md) | 声明一个新的站点类型：API Key，或账号池 | 可选 `probe`、`listModels`、`check`、`classify`、`rewritePayload`。账号池另有 `summarizeAccount`，以及按清单必填的 `startLogin`、`pollLogin`、`refreshTokens`。`models.perAccount` 时 `listModels` 按账号调用 | `api_key` 用站点 API Key。`account_pool` 用 `account:<siteType>:<uuid>`，密钥不进 JS | 站点类型：启用需要受信签名并确认主机；按站点类型自动生效 |

### 作用域是什么意思

每种类型都有一个固定的作用域，由 xLyra 决定，开发者不能改。

- **站点**：只对管理员选定的站点生效，其他站点完全不受影响。一个站点同一种类型最多绑定一个插件，选另一个插件会替换它。没有绑定时，一切和没有这个插件时一样。
- **下游路径**：启用后由管理员分配 `/v1/plugins/<slug>`，客户端访问这个路径时才会走该插件。
- **对象**：管理员把插件绑定到某个对象（目前是 OAuth 账号），并选择它可以操作的目标和参数。同一个对象可以有多条绑定，不受“一种类型一个插件”的限制。
- **全局**：启用即对所有站点生效。这两种类型（`site_detect`、`model_metadata`）都只会"补充"：识别时只能指向 xLyra 已有的站点类型，模型元数据只补充缺失的字段、不覆盖已有的值。
- **站点类型**：启用需要受信签名并确认主机列表，之后按声明的站点类型自动生效，不能逐站点绑定。
- 插件被停用或卸载后，对应的绑定自动失效，站点回到默认行为，不需要手动清理。

## 我该选哪一个

| 你的情况 | 选择 |
| --- | --- |
| 我要接一个接口格式非标准的上游，让用户能调用它的模型 | `protocol` |
| 这个站点查余额的接口比较特殊 | `quota_probe` |
| 这个站点拉模型列表的接口格式特殊（分页、嵌套、不是 `/v1/models`） | `model_list` |
| 默认的密钥校验或健康检查不适用于这个站点 | `credential_check` |
| 想让 xLyra 自动识别这类站点 | `site_detect` |
| 上游报错的格式特殊，限流错误被当成了普通错误 | `error_classifier` |
| 上游的模型名或能力 xLyra 不认识 | `model_metadata` |
| 站点公布的价格表是特殊格式 | `pricing_parse` |
| 上游需要特殊请求头、请求签名、请求改写 | 暂时没有对应类型，计划作为 `protocol` 的可选钩子 |
| 账号额度重置后自动清零密钥用量、站点余额不足时推送通知（Bark、Webhook 等，由插件自己发请求） | `automation` |
| 我要声明一种新的 API Key 站点类型，或一种带登录、刷新、多账号的站点类型 | `site_provider`（`credential.model` 为 `api_key` 或 `account_pool`） |
| 我想加新功能、计费规则、选路策略 | 插件做不到，请向 xLyra 提需求 |

一个站点可能需要多个插件，比如一个 `protocol` 加一个 `quota_probe`，它们各自打包、各自上传。

## 五种工作方式

类型按工作方式分成五族，同一族的写法相同。

### 分步查询：`quota_probe`、`model_list`、`credential_check`、`site_detect`

函数会被**反复调用**。每次返回下面三者之一：

- `{ request }`：请你让 xLyra 发一个 HTTP 请求，响应会出现在下次调用的 `steps` 里；
- `{ result }`：完成；
- `{ error: "原因" }`：失败。

最多调用 6 次（发 5 个请求），每次 200 ms。写法详见[额度探测插件](./quota-probe.md)，其他三种只说明与它的区别。`site_detect` 不带凭据，请求不会有 `Authorization` 头。

### 请求转换：`protocol`

每次客户端请求都会调用，时限 50 ms，只处理非流式请求。详见[协议插件](./protocol.md)。

### 一次性函数：`error_classifier`、`model_metadata`、`pricing_parse`

纯函数：xLyra 给你输入，你返回结果，**不发任何 HTTP 请求**。其中 `error_classifier` 在请求路径上，时限 50 ms；另外两个 200 ms。

### 事件驱动：`automation`

xLyra 在某件事发生后把事件放进队列，后台依次交给插件，插件返回**声明式动作**，由 xLyra 校验权限和目标范围后执行。不在请求路径上，时限 200 ms。详见[自动化插件](./automation.md)。

### 站点类型：`site_provider`

声明一个新的站点类型。`credential.model` 为 `api_key` 时用一把站点 Key；为 `account_pool` 时由 xLyra 执行多账号的登录、刷新、导入和选路，插件只描述，密钥不进 JS。可选导出 `probe`、`listModels`、`check`、`classify`、`rewritePayload`。账号池必定导出 `summarizeAccount`；`custom_poll` 再导出 `startLogin` 和 `pollLogin`；`custom_steps` 再导出 `refreshTokens`。`models.perAccount` 为 true 时必须导出 `listModels`，宿主按账号调用它并写入模型，令牌用 `{{secret.access_token}}`，不进 JS。这些账号钩子的返回 kind 是 `account_login`、`account_refresh`、`account_summary`，`listModels` 仍是 `model_list`，不是新的插件 kind。启用需要受信签名，并且每次启用都要确认主机列表。钩子按站点类型自动生效，不能逐站点绑定。停用后该类型站点保留数据但不参与路由。详见[站点类型](./site-provider.md)。

所有类型共用同一个原则：**插件只"描述"想做什么，真正的执行永远在 xLyra 里**。`request` 是描述，`actions` 是描述，`classify` 的结果也是描述。

## 每种类型都会写清楚的内容

每个类型的文档都用同样的结构：什么时候被调用、用什么凭据、是否只读、导出哪些函数、输出的字段、时限、怎么写 fixtures。

## 需要的类型不在表里

请到仓库提 issue，说明场景和上游返回的样例。新类型需要 xLyra 本身增加支持。
