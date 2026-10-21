# 资源上限与安全规则

## 为什么插件拿不到密钥、也不能自己发请求

插件运行在沙箱里，只做纯计算。

- 钩子的参数里没有密钥明文。普通分步钩子的 `ctx` 只有站点类型、地址和凭据**类型**。账号池的 `refreshTokens` / `summarizeAccount` 另外带非密钥字段；`startLogin` / `pollLogin` 带的是登录表单参数。
- 插件只能"描述"请求（`request` / `buildRequest` 的返回值），真正发请求的是 xLyra 的 Go 代码，由它把鉴权头加上。账号池用 `{{secret.<field>}}` 引用宿主秘密袋，见下文。
- 插件设置的 `Authorization`、`Cookie`、`Proxy-Authorization`、`Host` 等头会被丢弃。
- 响应里的 `Set-Cookie` 不会交给插件。

这样即使第三方插件有恶意代码，也无法读取或外发上游密钥。`automation` 插件是唯一能让 xLyra 代发请求的类型（`http.request`），目的地被限制在管理员填写的地址，见下文"自动化插件的权限"。

## URL 规则

插件提供的 `path` 会拼在站点地址后面，并且必须满足：

- 以单个 `/` 开头，不能以 `//` 开头；
- 不能含 `\`、`#`、`://`；
- 不能含 `.` 或 `..` 路径段（包括被百分号编码后的形式）；
- 最终 URL 必须与站点地址**同协议、同主机、同端口**，不能带用户名密码。

不满足时报 `js_url_rejected`。重定向也受同样的限制，并且最多 10 次。

## 请求头

- 最多 32 个，总大小（名加值）不超过 8 KiB。
- 被丢弃的：`Authorization`、`Proxy-Authorization`、`Cookie`、`Host`、`Content-Length`、`Transfer-Encoding`、`Connection`，以及所有 `X-Forwarded-*`。

## 资源上限

| 项 | 上限 |
| --- | --- |
| 分步类型（`quota_probe`、`model_list`、`credential_check`、`site_detect`）每次钩子调用 | 200 ms |
| `automation` 每次 `handle` 调用（不在请求路径上） | 200 ms |
| 协议和 `error_classifier` 每次调用（在请求路径上） | 50 ms |
| `model_metadata`、`pricing_parse` 每次调用 | 200 ms |
| 每次调用的内存分配 | 16 MiB |
| 钩子返回值大小 | 1 MiB |
| 分步类型的钩子调用次数 | 6 次（最多发 5 个 HTTP 请求） |
| 额度探测单个请求体 | 64 KiB |
| 额度探测发给插件的响应体 | 1 MiB |
| 协议响应体 | 4 MiB |
| 单个 HTTP 请求的超时 | 20 秒 |
| 日志 | 每次调用 20 条，每条 1024 字节 |
| 请求头 | 32 个，共 8 KiB |
| 插件包 | 2 MiB，64 个文件 |
| `site_provider` 的 `display.icon` | 64 KiB，只能是 PNG 或 WebP |
| 账号池 `credential.fields` | 1–16 项 |
| 账号池 `regions` | 1–8 项；每项 `realms`、`domains` 各最多 8；`attrs` 最多 2 项，键只能是 `origin`、`referer`，值最多 256 字节 |
| 账号池 `identity` / `display` | 各最多 8 个来源 |
| 账号池 `refresh.leadSeconds` | 30–3600，不写时为 300（`refresh.kind` 为 `none` 时不能写） |
| 账号池自定义登录会话 | 进程内，10 分钟。设备码会话按提供方 `expires_in`，缺省 600 秒，最长 1800 秒 |
| 账号池登录轮询间隔 | 缺省或小于 1 按 5 秒，大于 60 按 60 秒 |
| 账号池导入，单个文件 | 4 MiB，最多 500 个账号对象 |
| 账号池导入，一次请求 | 最多 50 个文件，合计 200 个账号 |
| 账号池 `import.containers` | 最多 8 项；每个 `fieldMap` 键 1–8 个别名 |
| 账号池 `import.valueMap` | 最多 8 个字段，每个 1–8 条；键和值最多 64 字节 |
| 账号池 `import.msThreshold` | 不写等于 1e12；写出时为 1 到 1e15 |
| 账号池摘要 `label` | 最多 256 字节，不能含控制字符 |
| 账号池 `ui.rowPills` | 最多 4 项；每项 `labels` 最多 8，键最长 64 字节 |
| 账号池额度耗尽冷却 | 到 `resetAt`，最长 7 天，原因 `quota_exhausted` |
| 秘密擦除 | 短于 4 个 Unicode 码位的值不从日志和错误字符串里擦掉 |

超时或内存超限时，运行时会被**立即中断**并丢弃，不会影响其他请求。

## 打包限制

`xlyra-plugin build` 会拒绝：

- 外部依赖（`import x from "lodash"` 这类裸导入）；
- 动态 `import()`；
- 找不到入口（`src/index.ts` 或 `src/index.js`）。

只能 `import type`，以及相对路径的本地文件。需要的工具函数直接写进自己的源码里。

## 自动化插件的权限

`automation` 插件能让 xLyra 做事，所以多了两道限制，二者缺一不可：

- **权限**：插件在 manifest 里声明会返回哪几类动作，管理员启用版本时逐项确认。没声明或没授予的动作一律不执行。
- **范围**：动作只能作用于管理员在绑定里选定的对象（`target` 必须是 `ctx.inputs` 里某个 `api_key` 对象的 id）。插件不能指定任意密钥或账号。

动作集合由 xLyra 定义，目前只有 `apikey.reset_usage`、`notify` 和 `http.request`。`http.request` 是插件唯一能让 xLyra 代发的出站请求：目的地必须与管理员在绑定里填的某个 `url` 输入同源，需要管理员授予权限，请求由 xLyra 的出站客户端发出（拦截云元数据等非公网地址），插件看不到响应，完整地址和请求头不会写入操作记录。

## site_provider 的主机列表

`site_provider` 在清单 `hosts` 里声明这个站点类型要用的主机。管理员每次启用都必须确认这份完整列表，确认结果记成 `host:<主机名>`。未由受信密钥签名的包不能启用。

这份列表只约束插件站点类型（类型名里带 `/`，并且当前处于启用快照里）。内置站点类型不查这张表。

- 创建或更新站点时，`base_url` 的主机必须在列表里，否则拒绝保存。
- 通用适配器在列出模型、校验 Key、健康检查和同步模型之前检查请求地址的主机。不在列表里就不发出请求。它使用的客户端还会限制重定向。
- 网关把请求转到上游之前，用最终的上游 URL 再查一次主机。站点模型测试走同一条限制。不通过则不发出请求，返回 502，错误类型是 `upstream_host_not_allowed`，并且不会更换凭据重试。上游客户端同样拒绝离开列表的重定向。

匹配的是主机名本身：去掉端口、忽略大小写、必须和列表中的某一项完全相同。不做子域匹配，也不支持通配符。重定向最多跟随 5 次。目标协议必须是 `https`，或者与最初那次请求的协议相同，但不能从 `https` 降到 `http`。

插件被停用或卸载之后没有可用的白名单。类型名里带 `/` 的出站会被拒绝。

`site_provider` 的 `probe`、`listModels`、`check`，以及账号池的 `startLogin`、`pollLogin`、`refreshTokens`、`summarizeAccount` 和 `models.perAccount` 的 `listModels`，在发请求前除了与这次的 `baseURL` 同源，还会再做一次上面的主机名检查。按账号的 `listModels` 用账号区域的基址，不是站点行上的 `base_url`。站点类型名里带 `/` 时，手工绑定的分步插件也做这一次检查。内置站点类型（类型名不含 `/`）的额度探测仍只受同源限制。详见[站点类型](./site-provider.md#出站限制)。

## 账号池的密钥

账号池的 access token、refresh token 和其它 `secret: true` 字段留在宿主的秘密袋里。JS 看到的响应里，`secretPaths` 指到的位置是 `{{redacted}}`，`Body` 和解析后的 JSON 一起改写。钩子要用这个值时，在请求体字符串、query 值或头值里写 `{{secret.<field>}}`。path、方法、头名、query 键、JSON 对象键里出现这个占位符，请求不会发出。替换前 URL 已经过 `hosts`；替换后主机不得变化。

声明了 `secretPaths` 时，非 JSON 响应整步失败，原文不交给 JS。`pollLogin` 的请求可以写 `onNonJSON: "pending"`，这时不是单个 JSON 对象的响应当成 pending，正文仍然不交给 JS。其它钩子忽略这个字段。路径不存在不算错误。路径在但值不是非空字符串，这一步失败。

日志和错误字符串会把袋里的值换成 `[redacted]`。短于 4 个 Unicode 码位的值不参与这层替换。登录 Cookie 罐在进程内存里，按会话存放，不进 JS，也不跨实例。目录重载换掉 runtime 后，进行中的会话同样丢掉。

头模板 `{{cred.<field>}}`、`{{region.origin}}`、`{{region.referer}}` 和上面的 `{{secret.<field>}}` 不是一回事。网关转发只把非密钥字段、access token 和区域属性放进变量表。条件头只看 `cred.<字段>` 是否为空，没有通用表达式。

## 插件图标

`display.icon` 的字节由 `GET /api/v1/plugin-icons/{pluginID}` 返回。路径在需要管理端会话的路由组里，不匿名公开。响应按文件头设置 `Content-Type`（`image/png` 或 `image/webp`），并带 `X-Content-Type-Options: nosniff`、`Content-Security-Policy: default-src 'none'`、`Cache-Control: private, max-age=86400`。`ETag` 是包的 SHA256。插件停用后图片仍在；插件删除后是 404。查询参数 `v` 只给浏览器换缓存用，服务端不读它。

## 自动保护（熔断）

管理员启用插件后，xLyra 会持续统计运行情况，异常时自动**停用该版本**并写入日志：

- 分步类型插件：连续失败 20 次。
- 协议插件：5 分钟内调用超过 50 次且错误率超过 50%，或 1 分钟内被中断超过 10 次。

熔断只会停用，不会自动回滚到旧版本，由管理员决定下一步。

## 编写建议

- 对上游返回的每个字段都做类型检查，不要假设字段一定存在、一定是数字。
- 数字字符串用 `Number()` 转换，再用 `Number.isFinite()` 判断。
- 不要在日志里输出完整响应体。
- 失败时用 `{ error: "..." }` 返回清楚的原因，不要抛异常。
