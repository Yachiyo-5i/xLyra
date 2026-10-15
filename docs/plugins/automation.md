# 自动化插件（automation）

用于"xLyra 里发生了某件事，需要自动做点什么"的场景。比如：OAuth 账号的周额度重置后，自动清零分给别人用的 API 密钥的已用额度。

你只负责**判断要不要做、做什么**。真正的操作由 xLyra 校验权限、检查幂等后执行，插件拿不到密钥，也不能直接改数据库。

## 导出

```ts
export const meta = { apiVersion: 1, id: "<与 manifest 一致>", kind: "automation" };
export function handle(ctx, event) { ... }
```

这是**事件驱动**的函数：不发请求，不在请求路径上，时限 **200 ms**。xLyra 把事件放进队列，由后台循环依次交给插件处理。

## 它怎么工作

1. 管理员启用插件版本时，确认它声明的**权限**（能做哪几类操作）。
2. 管理员在"自动化"里新增一条**绑定**：按插件在 manifest 里声明的**输入**填表，比如选一个 OAuth 账号、选若干 API 密钥、填几个参数。表单是 xLyra 按这份声明画出来的，文案也由插件自己写。
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
    { "name": "resetOnRecovery", "type": "boolean", "title": "Also reset on early recovery", "default": true }
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

`schedule.tick` 适合"到点就检查"的事，而不是"某件事变了才做"的事。它的 `current` 是事件对象此刻的样子：对 OAuth 账号来说是 `now`（Unix 毫秒）、`status`、`last_sync_at` 和最近一次同步保存的 `quota`；对 API 密钥是 `now` 和用量字段。没有 `previous`。它是轮询，不是精确的定时器；xLyra 停机之后重启，每条绑定只会补发**一次**，不会把错过的间隔都补上。

### 权限与动作

| 权限 / 动作 | 作用 |
| --- | --- |
| `apikey.reset_usage` | 清零一个已选 API 密钥的已用量（`total`、`daily`、`weekly`）。声明它就必须有一个 `api_key` 类型的输入 |
| `notify` | 在这个插件的操作记录里写一条通知 |

### 输入（inputs）

每个输入有一个 `name`（小写字母开头，只含字母和数字，在 `ctx.inputs` 里用它取值）和一个 `type`。类型分两类：

**对象**：管理员从 xLyra 已有的对象里选。

| `type` | 选的是什么 | 交给插件的 `fields` |
| --- | --- | --- |
| `oauth_connection` | OAuth 账号 | `provider`、`status` |
| `api_key` | 下游 API 密钥（不含内部密钥） | `totalUsed`、`dailyUsed`、`weeklyUsed`，设置了有限总额度时还有 `totalLimit` |

**参数**：`string`、`number`、`integer`、`boolean`，管理员直接填写。

| 字段 | 适用 | 说明 |
| --- | --- | --- |
| `title`、`description` | 全部 | 输入的标题和说明 |
| `placeholder` | 全部 | 选择器或输入框为空时的提示 |
| `emptyText` | 对象 | 没有可选的对象时的提示 |
| `required` | 全部 | 是否必填。对象默认必填，参数默认不必填 |
| `multiple` | 对象 | 可以选多个，`ctx.inputs` 里拿到数组 |
| `eventSubject` | 对象 | 标记事件发生在谁身上，**必须有且只有一个**，且不能是 `multiple`。事件会送到选了这个对象的绑定 |
| `providers` | `oauth_connection` | 只允许这些 provider 的账号 |
| `requires` | `api_key` | 对每个对象的要求。`finite_total_quota`：必须设置了有限的总额度 |
| `default`、`enum`、`minimum`、`maximum` | 参数 | 默认值、可选值、数值范围 |

事件的类型要和 `eventSubject` 输入的对象类型一致：`oauth.quota_synced` 的事件对象必须是 `oauth_connection`；`schedule.tick` 不限类型。

对象选择器里，不满足 `providers` 或 `requires` 的对象仍会列出，但不能选，并显示原因。保存时 xLyra 会再检查一次，同一个对象也不能在同一个绑定里被选两次。

### 表单文案

`title`、`description`、`placeholder`、`emptyText`，以及 `form` 里的四项，都可以写成两种形式：

```json
"title": "API keys"
"title": { "zh": "API 密钥", "en": "API keys", "jp": "API キー" }
```

写成字符串，所有语言都显示它；写成对象，页面按当前语言取：先找完全匹配（如 `zh-CN`），再找语言（`zh`），再找 `en`，最后用列出的第一项。每条文案不超过 400 字节，一个对象最多 8 种语言，语言标签只能是 `zh`、`zh-CN` 这样的形式。

没有写的文案用 xLyra 的默认文案；对象选择器里"不能选的原因"、保存按钮、操作记录这些由 xLyra 提供，不在插件的控制范围内。

## 输入

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

除这些字段以外的任何字段都会被拒绝（`js_shape_error`）。

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
| 可以要的对象 | 只有 OAuth 账号（`oauth_connection`）和下游 API 密钥（`api_key`）。`inputs` 里写别的 `type` 上传会被拒绝 |
| 事件 | `oauth.quota_synced` 和 `schedule.tick`。没有请求路径上的事件，也没有站点、API 密钥、OAuth 账号状态变化的事件 |
| 动作 | `apikey.reset_usage` 和 `notify`。不能改价格、路由、计费，也不能停用密钥或站点、新建或删除任何东西 |
| 表单 | 由 `inputs` 画出：对象选择器加扁平参数，参数只支持 `string`、`number`、`integer`、`boolean`，最多 16 项，不支持嵌套、列表或自定义控件。文案可以自定义，布局和控件样式不行 |
| 读取数据 | 只能用事件和 `ctx.inputs` 里带的内容：前后快照和对象的 `fields`。不能主动查询别的数据，也不能发请求 |

对象类型是 xLyra 逐个开放的：每种类型都要提供选项列表、校验、交给插件看的字段，以及对象被删除时怎么处理。为什么这一版只有账号和密钥：站点这类对象目前没有对应的事件和动作，开放后只能配合"定时事件加通知"使用，价值有限，等有真实场景再加更稳妥。

### 需要更多能力时

如果你的需求超出了上面的范围，比如：

- 想让管理员选站点等别的对象，或操作别的对象；
- 想订阅别的事件（比如 API 密钥用量越过阈值、账号状态变化）；
- 想要新的动作；
- 参数表单需要嵌套、列表或别的控件；

请到仓库提 [issue](https://github.com/Yachiyo-5i/xLyra/issues)，写清楚三件事：你要解决的场景、你希望在什么事件发生时做什么、需要读到哪些数据。新增对象类型、事件和动作都需要 xLyra 本身增加支持，而且每一项都会扩大插件能影响的范围，所以我们会按真实场景逐项评估，而不是一次性开放。

## 管理员怎么使用

1. 上传 `.xlp`，在开发者页启用版本。弹窗会列出它声明的权限，勾选确认后才会启用。
2. 打开插件，在"自动化"区点新增按钮，按表单填写：选对象、填参数。不能选的对象会显示原因。
3. 之后事件发生在你选的对象上时，插件就会被调用。"最近操作"里能看到它做了什么、成功还是失败。

升级插件时，新版本启用后，已有的绑定会自动跟着新版本，不需要重新创建，权限需要重新确认。如果新版本改了 `inputs`，旧绑定里对不上的内容在下次编辑时需要重新选择。
