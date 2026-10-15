# 自动化插件（automation）

用于"xLyra 里发生了某件事，需要自动做点什么"的场景。比如：OAuth 账号的周额度重置后，自动清零分给别人用的 API 密钥的已用额度。

你只负责**判断要不要做、做什么**。真正的操作由 xLyra 校验权限、检查幂等后执行，插件拿不到密钥，也不能直接改数据库。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "automation" };
export function handle(ctx, event) { ... }
```

这是**事件驱动**的函数：自己不发请求（要往外发就返回 `http.request` 动作，由 xLyra 代发），不在请求路径上，时限 **200 ms**。xLyra 把事件放进队列，由后台循环依次交给插件处理。

## 它怎么工作

1. 管理员启用插件版本时，确认它声明的**权限**（能做哪几类操作）。
2. 管理员在"自动化"里新增一条**绑定**：按插件在 manifest 里声明的**输入**填表，比如选一个 OAuth 账号、选若干站点、填几个参数。**表单完全由 manifest 驱动**：插件只声明"要什么数据、用什么控件、文案是什么"，xLyra 负责取出可选的对象、渲染控件、校验并保存，插件不需要也无法写任何界面代码。
3. 事件发生在某个对象上（比如一个 OAuth 账号）时，xLyra 在保存数据的同一个事务里，把事件放进**选了这个对象**的那些绑定的队列。
4. 后台取出事件，调用 `handle`；插件返回一组**动作**。
5. xLyra 逐个检查动作（权限已授予、目标是管理员选过的对象），然后执行，并把结果写进操作记录。

权限决定"能做哪类操作"，管理员在绑定里选的对象决定"能作用于谁"。两者缺一不可。

## manifest

```json
"automation": {
  "subscribes": ["oauth.quota_synced"],
  "permissions": ["apikey.reset_usage"],
  "form": {
    "title": { "zh": "周额度自动重置", "en": "Weekly quota reset" },
    "addLabel": { "zh": "新增", "en": "Add" }
  },
  "inputs": [
    { "name": "account", "type": "oauth_connection", "title": { "zh": "OAuth 账号", "en": "OAuth account" },
      "providers": ["codex", "claude_code"], "eventSubject": true },
    { "name": "keys", "type": "api_key", "title": "API keys", "multiple": true, "requires": "finite_total_quota" },
    { "name": "resetOnRecovery", "type": "boolean", "control": "switch", "title": "Also reset on early recovery", "default": true }
  ]
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `subscribes` | 是 | 订阅的事件，见下表，至少一个 |
| `permissions` | 否 | 会返回的动作类型。返回没有声明的动作类型会被拒绝 |
| `inputs` | 是 | 绑定时管理员要填的内容，按顺序画成表单，最多 16 项，见下文 |
| `schedule.everyMinutes` | 订阅 `schedule.tick` 时必填 | 定时事件的间隔，5 到 10080 分钟。没有订阅定时事件时不能写 |
| `form` | 否 | 表单四处文案：`title`（区块标题）、`description`（说明）、`addLabel`（新增按钮）、`emptyText`（还没有绑定时的提示）。不写就用 xLyra 的默认文案 |

### 事件

| 事件 | 对象 | 何时发生 |
| --- | --- | --- |
| `oauth.quota_synced` | `oauth_connection` | 一个 OAuth 账号的额度同步并保存之后 |
| `schedule.tick` | 任意（跟随事件对象） | 按 `schedule.everyMinutes` 对每条绑定定时发出，不管有没有变化 |

`schedule.tick` 适合"到点就检查"的事，而不是"某件事变了才做"的事。它的 `current` 是事件对象此刻的样子：对 OAuth 账号来说是 `now`（Unix 毫秒）、`status`、`last_sync_at` 和最近一次同步保存的 `quota`；对 API 密钥是 `now` 和用量字段；对站点是 `now`、`enabled`、`siteType` 和 `quota`（站点最近一次额度探测的汇总，没探测过则没有这个字段）。站点 `quota` 里常用的有：`status`（`ok` 才可信）、`remaining_min`（最紧张窗口的剩余额度，sub2api 站点就是余额）、`unit`、`unlimited`、`entries`。没有 `previous`。它是轮询，不是精确的定时器；xLyra 停机之后重启，每条绑定只会补发**一次**，不会把错过的间隔都补上。

### 权限与动作

| 权限 / 动作 | 作用 |
| --- | --- |
| `apikey.reset_usage` | 清零一个已选 API 密钥的已用量（`total`、`daily`、`weekly`）。声明它就必须有一个 `api_key` 类型的输入 |
| `notify` | 在这个插件的操作记录里写一条通知 |
| `http.request` | 向管理员在绑定里填的地址发一个 HTTP 请求，通知、Webhook 都靠它，见[出站请求](#出站请求httprequest)。声明它就必须有一个 `url` 类型的输入 |

### 输入（inputs）

每个输入描述绑定表单里的**一项**，由三部分组成：

1. **要什么数据**：`type`（以及 `multiple`、`providers`、`requires`、`enum` 等），决定管理员能选什么、填什么，也决定校验；
2. **用什么控件画**：`control`，可选，不写按类型取默认；
3. **显示什么文案**：`title`、`description`、`placeholder`、`emptyText`，见[表单文案](#表单文案)。

每个输入有一个 `name`（小写字母开头，只含字母和数字，在 `ctx.inputs` 里用它取值）和一个 `type`。表单按 `inputs` 的顺序画，最多 16 项。

#### 数据：type

**对象**：管理员从 xLyra 已有的对象里选。xLyra 取出可选的列表（连同名称、品牌图标），插件不用提供。

| `type` | 选的是什么 | 选项里显示 | 交给插件的 `fields` |
| --- | --- | --- | --- |
| `oauth_connection` | OAuth 账号 | 邮箱和 provider 图标 | `provider`、`status` |
| `api_key` | 下游 API 密钥（不含内部密钥） | 密钥名和用量 | `totalUsed`、`dailyUsed`、`weeklyUsed`，设置了有限总额度时还有 `totalLimit` |
| `site` | 站点（不含已删除的） | 站点图标和名称 | `siteType`、`enabled` |

**参数**：`string`、`url`、`number`、`integer`、`boolean`，管理员直接填写。`url` 是 http(s) 地址（不能带用户名密码），保存时由 xLyra 校验；它同时是 `http.request` 允许访问的目的地。

| 字段 | 适用 | 说明 |
| --- | --- | --- |
| `title`、`description` | 全部 | 输入的标题和说明 |
| `placeholder` | 全部 | 选择器或输入框为空时的提示 |
| `emptyText` | 对象 | 没有可选的对象时的提示 |
| `required` | 全部 | 是否必填。对象默认必填，参数默认不必填 |
| `multiple` | 对象 | 可以选多个，`ctx.inputs` 里拿到数组 |
| `eventSubject` | 对象 | 标记事件发生在谁身上，**必须有且只有一个**。事件会送到选了这个对象的绑定。可以同时是 `multiple`：一个绑定关注多个对象，每个事件（包括每次 `schedule.tick`）只针对其中一个，`event.subject` 就是它。绑定的 `state` 是这些对象共用的，请按对象 `id` 分开记，并清掉已不在 `ctx.inputs` 里的对象 |
| `control` | 全部 | 用什么控件画，见下节。不写则按类型取默认控件 |
| `providers` | `oauth_connection` | 只允许这些 provider 的账号 |
| `requires` | `api_key` | 对每个对象的要求。`finite_total_quota`：必须设置了有限的总额度 |
| `default`、`enum`、`minimum`、`maximum` | 参数 | 默认值、可选值、数值范围 |

事件的类型要和 `eventSubject` 输入的对象类型一致：`oauth.quota_synced` 的事件对象必须是 `oauth_connection`；`schedule.tick` 不限类型。

对象选择器里，不满足 `providers` 或 `requires` 的对象仍会列出，但不能选，并显示原因。保存时 xLyra 会再检查一次，同一个对象也不能在同一个绑定里被选两次。

#### 控件：control

| 输入 | 可选控件（第一个是默认） |
| --- | --- |
| 对象 | 单选 `select`；`multiple` 时 `multi_select` |
| `boolean` | `checkbox`、`switch` |
| 带 `enum` 的参数 | `select`、`radio` |
| `string` | `input`、`password`（输入时隐藏，绑定列表里只显示 ••••••）、`textarea` |
| `url` | `input`、`password` |
| `number`、`integer` | `input` |

声明了不适合该输入的控件，上传会被拒绝。`minimum`、`maximum`、`placeholder` 等会按控件用上；控件的外观和布局由 xLyra 决定，插件不能自定义样式。

下面是一份完整的输入声明（节选自 `site-balance-alert`），管理员看到的是：一个多选站点（带图标）、一个文本框、一个数字框、一个整数框：

```json
"inputs": [
  { "name": "sites", "type": "site", "multiple": true, "eventSubject": true,
    "title": { "zh": "站点", "en": "Sites" } },
  { "name": "barkUrl", "type": "url", "required": true,
    "title": { "zh": "Bark 推送地址", "en": "Bark URL" },
    "placeholder": "https://api.day.app/xxxxxxxx" },
  { "name": "threshold", "type": "number", "required": true, "minimum": 0, "default": 5,
    "title": { "zh": "余额阈值", "en": "Balance threshold" } },
  { "name": "repeatHours", "type": "integer", "minimum": 0, "maximum": 720, "default": 0,
    "title": { "zh": "重复提醒间隔（小时）", "en": "Reminder interval (hours)" } }
]
```

### 表单文案

`title`、`description`、`placeholder`、`emptyText`，以及 `form` 里的四项，都可以写成两种形式：

```json
"title": "API keys"
"title": { "zh": "API 密钥", "en": "API keys", "jp": "API キー" }
```

写成字符串，所有语言都显示它；写成对象，页面按当前语言取：先找完全匹配（如 `zh-CN`），再找语言（`zh`），再找 `en`，最后用列出的第一项。每条文案不超过 400 字节，一个对象最多 8 种语言，语言标签只能是 `zh`、`zh-CN` 这样的形式。

没有写的文案用 xLyra 的默认文案；对象选择器里"不能选的原因"、保存按钮、操作记录这些由 xLyra 提供，不在插件的控制范围内。

## handle 收到什么

`ctx`：

| 字段 | 说明 |
| --- | --- |
| `event` | 当前事件类型 |
| `now` | 当前时间，Unix 毫秒 |
| `bindingId` | 这条绑定的 id |
| `inputs` | 管理员填的内容，键是 manifest 里声明的输入名。对象输入是一个 `AutomationEntity`（`multiple` 时是数组），参数是它的值 |
| `state` | 这条绑定上次返回的 `state`，第一次为空对象 |

对象在每次事件到来时按**当前**状态重新读取，所以 `fields` 里的用量总是最新的。已被删除的对象不会出现。

`AutomationEntity`：

| 字段 | 说明 |
| --- | --- |
| `type` | `oauth_connection` 或 `api_key` |
| `id` | 动作里的 `target` 用这个 |
| `name` | 显示名：账号邮箱、密钥名 |
| `fields` | 见上文"输入"里各类型的字段 |

`event`：

| 字段 | 说明 |
| --- | --- |
| `type` | 事件类型 |
| `subject` | 事件发生在谁身上：`eventSubject` 输入选的那个对象，也是一个 `AutomationEntity` |
| `previous` | 变化之前的数据，第一次没有 |
| `current` | 变化之后的数据 |

`oauth.quota_synced` 的 `previous` / `current` 是账号保存的额度对象，里面的窗口形如 `weekly: { reset_at, remaining_percent, ... }`。`reset_at` 通常是 Unix 秒，也可能是毫秒或日期字符串，请自己做兼容。

## 返回

```ts
{
  actions: [
    { type: "apikey.reset_usage", target: "<某个 api_key 对象的 id>", scope: "total", idempotencyKey: "1791331200" },
  ],
  state: { lastReset: 1791331200 },
}
```

| 字段 | 说明 |
| --- | --- |
| `actions` | 必填，最多 32 个，可以为空 |
| `state` | 可选，**整体替换**这条绑定保存的状态，不超过 8 KiB；不写表示保持不变 |

动作的字段：

| 字段 | 适用 | 说明 |
| --- | --- | --- |
| `type` | 全部 | `apikey.reset_usage` 或 `notify` |
| `target` | `apikey.reset_usage` | 必须是 `ctx.inputs` 里某个 `api_key` 对象的 `id` |
| `scope` | `apikey.reset_usage` | `total`、`daily` 或 `weekly` |
| `idempotencyKey` | `apikey.reset_usage` | 必填，不超过 128 字节。同一目标、同一 `scope` 重复提交相同的 key，只会执行一次 |
| `level` | `notify` | `info`（默认）或 `warn` |
| `message` | `notify` | 必填，不超过 500 字节 |
| `method` | `http.request` | `GET`、`POST`（默认）、`PUT`、`PATCH`、`DELETE` |
| `url` | `http.request` | 必填。它的来源（协议、主机、端口）必须与某个 `url` 输入的值相同，路径和查询串可以自己拼 |
| `headers` | `http.request` | 可选，字符串到字符串，最多 16 个。`Host`、`Content-Length`、`Transfer-Encoding`、`Connection`、`Proxy-Authorization`、`X-Forwarded-*` 会被丢弃 |
| `body` | `http.request` | 可选，文本，不超过 64 KiB；`GET` 不发送。发 JSON 时自己 `JSON.stringify` 并设置 `Content-Type` |
| `idempotencyKey` | `http.request` | 必填，规则同上。同一个事件对象、同一个地址重复提交相同的 key，只会发送一次 |

除这些字段以外的任何字段都会被拒绝（`js_shape_error`）。

### 出站请求（http.request）

插件本身没有 `fetch`，要往外发东西（推送通知、调用 Webhook）就返回 `http.request` 动作，由 xLyra 代为发出。**怎么发、发给谁、发什么内容都由插件决定**，xLyra 不认识 Bark、Telegram 之类的具体服务，只负责：

1. **目的地受管理员控制**：`url` 的来源（协议、主机、端口）必须与某个 `url` 输入的值一致。管理员在绑定里填了什么地址，插件就只能访问那个地址所在的服务；不能发往其他地方，所以插件无法把站点数据外发到任意主机。
2. **权限**：必须声明并获得 `http.request` 权限，启用版本时管理员要确认。
3. **只发不收**：插件看不到响应。响应状态不是 2xx 时，动作按失败处理并重试；成功后状态码记入操作记录。
4. **去重**：按 `idempotencyKey` 去重（范围是同一个事件对象加同一个地址），事件重试不会重复发送。
5. **不跟随跨地址的重定向**：服务端把请求重定向到别的协议、主机或端口时，请求被拒绝，不会发往新地址；同一来源内的重定向正常跟随。
6. **限制**：请求超时 10 秒，走与上游相同的出站客户端（拦截云元数据等非公网地址）；请求体不超过 64 KiB。

管理员填的地址里常含密钥（如 Bark 的设备 Key）。操作记录里只保存请求的来源、方法和请求体开头，不保存完整地址和请求头；错误信息里也不含地址。但插件自己能读到 `ctx.inputs` 里的地址，所以只应启用你信任的插件。

下面是用 Bark 推送一条通知的写法：

```ts
{
  type: "http.request",
  method: "POST",
  url: ctx.inputs.barkUrl as string,            // 管理员填的 https://api.day.app/<设备Key>
  headers: { "Content-Type": "application/json; charset=utf-8" },
  body: JSON.stringify({ title: "余额不足", body: "relay 剩余 1.2 usd", level: "timeSensitive", group: "xlyra" }),
  idempotencyKey: "1",
}
```

换成 Telegram、飞书或自建 Webhook，只是换 `url`、`headers` 和 `body`，不需要 xLyra 改任何东西。

### 幂等键怎么选

事件可能因为重试而重复交给你。给"同一件事"始终返回同一个 `idempotencyKey`，xLyra 就不会重复执行。对额度重置，用上游的**下一次重置时间**最合适：同一周期无论同步多少次，key 都一样；到了下一个周期 key 才会变。

## 失败与重试

- `handle` 抛出异常或返回不合规的结果：最多尝试 **4 次**，间隔 5 秒、30 秒、5 分钟，仍失败则记为失败，并写入操作记录。
- 动作执行失败（比如数据库暂时不可用）：同样重试。已经成功的动作因为幂等键不会重复执行。
- 插件被停用、绑定被删除：队列里该绑定的事件会被跳过。
- 已完成和失败的事件、操作记录保留 14 天。

xLyra 以单进程运行，事件由一个后台循环按顺序处理。

## 写 fixtures

样本用 `ctx` 提供 `inputs`（就是 `ctx.inputs`）和 `state`，`input` 是 `event`，`expect.result` 是期望的返回：

```json
{
  "name": "weekly window rolled over",
  "ctx": {
    "now": 2000000,
    "inputs": {
      "account": { "type": "oauth_connection", "id": "conn-1", "name": "owner@example.com", "fields": { "provider": "codex" } },
      "keys": [{ "type": "api_key", "id": "key-1", "name": "alice", "fields": { "totalUsed": 12 } }],
      "resetOnRecovery": true
    }
  },
  "input": {
    "type": "oauth.quota_synced",
    "subject": { "type": "oauth_connection", "id": "conn-1", "name": "owner@example.com", "fields": { "provider": "codex" } },
    "previous": { "weekly": { "reset_at": 1000, "remaining_percent": 2 } },
    "current": { "weekly": { "reset_at": 9000, "remaining_percent": 100 } }
  },
  "expect": {
    "result": { "actions": [{ "type": "apikey.reset_usage", "target": "key-1", "scope": "total", "idempotencyKey": "9000000" }] }
  }
}
```

`xlyra-plugin init --kind automation` 会生成一个完整的示例：判断周额度窗口是否滚动，滚动了就清零选中密钥的总用量。

## 当前的限制

`automation` 是第一版，能做的事是有意收窄的。下面这些是写死在 xLyra 里的，插件的 manifest 改不了：

| 方面 | 现状 |
| --- | --- |
| 可以要的对象 | 只有 OAuth 账号（`oauth_connection`）、下游 API 密钥（`api_key`）和站点（`site`）。`inputs` 里写别的 `type` 上传会被拒绝 |
| 事件 | `oauth.quota_synced` 和 `schedule.tick`。没有请求路径上的事件，也没有站点、API 密钥、OAuth 账号状态变化的事件 |
| 动作 | `apikey.reset_usage`、`notify` 和 `http.request`。不能改价格、路由、计费，也不能停用密钥或站点、新建或删除任何东西 |
| 表单 | 由 `inputs` 声明画出：对象选择器加扁平参数，参数只支持 `string`、`number`、`integer`、`boolean`，控件只能在上面[控件表](#控件control)里选，最多 16 项，不支持嵌套、列表、条件显示或自定义控件。文案可以自定义，布局和控件样式不行 |
| 读取数据 | 只能用事件和 `ctx.inputs` 里带的内容：前后快照和对象的 `fields`。不能主动查询别的数据，也读不到外发请求的响应 |

对象类型是 xLyra 逐个开放的：每种类型都要提供选项列表、校验、交给插件看的字段，以及对象被删除时怎么处理。站点这类对象目前没有对应的变化事件，只能配合"定时事件加通知"使用；动作也只有清零密钥用量和两种通知。其余对象、事件、动作等有真实场景再逐个开放。

### 需要更多能力时

如果你的需求超出了上面的范围，比如：

- 想让管理员选别的对象（比如渠道、模型），或操作别的对象；
- 想订阅别的事件（比如 API 密钥用量越过阈值、账号状态变化）；
- 想要新的动作；
- 表单需要嵌套、列表、条件显示，或上面控件表之外的控件；

请到仓库提 [issue](https://github.com/Yachiyo-5i/xLyra/issues)，写清楚三件事：你要解决的场景、你希望在什么事件发生时做什么、需要读到哪些数据。新增对象类型、事件和动作都需要 xLyra 本身增加支持，而且每一项都会扩大插件能影响的范围，所以我们会按真实场景逐项评估，而不是一次性开放。

## 管理员怎么使用

1. 上传 `.xlp`，在开发者页启用版本。弹窗会列出它声明的权限，勾选确认后才会启用。
2. 打开插件，在"自动化"区点新增按钮，按表单填写：选对象、填参数（表单是插件的 manifest 声明出来的，每个插件的表单不同）。不能选的对象会显示原因。
3. 之后事件发生在你选的对象上时，插件就会被调用。"最近操作"里能看到它做了什么、成功还是失败。

升级插件时，新版本启用后，已有的绑定会自动跟着新版本，不需要重新创建，权限需要重新确认。如果新版本改了 `inputs`，旧绑定里对不上的内容在下次编辑时需要重新选择。

## 示例：站点余额不足时用 Bark 通知

`site-balance-alert` 用 `schedule.tick` 定时读取站点的额度探测结果，余额降到阈值及以下时，由插件自己拼好 Bark 请求，用 `http.request` 发出。

**manifest 怎么写**：订阅 `schedule.tick` 并声明 `schedule.everyMinutes`；权限只要 `http.request`；输入是[控件](#控件control)一节里那份——多选站点（`eventSubject`）、Bark 地址（`url`）、阈值（`number`）、重复提醒间隔（`integer`）。管理员在表单里看到的就是这四项，绑定时不需要别的配置。

**怎么避免重复通知**：
- 在 `state` 里按站点 id 记"这一轮是否已通知"，余额仍不足时不再发送；回到阈值以上才重新布防。
- 需要时可以用 `repeatHours` 设置仍不足时的提醒间隔，默认 0 即不重复。
- `idempotencyKey` 用"该站点第几次提醒"，事件因重试重复交给插件时 xLyra 不会重复发送。
- 额度探测失败（`quota.status` 不是 `ok`）、无限额度、没有数据时既不发送也不改状态。

**Bark 的细节在插件里**：请求格式、级别（`timeSensitive`）、分组都写在插件的 `src/index.ts` 里，xLyra 不关心。想换推送渠道，只改那一段。
