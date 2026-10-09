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
2. 管理员在"自动化"里新增一条**绑定**：它关注哪个 OAuth 账号、可以操作哪些 API 密钥、参数填什么。
3. 账号发生插件订阅的事件时，xLyra 在保存数据的同一个事务里把事件写入队列。
4. 后台取出事件，调用 `handle`；插件返回一组**动作**。
5. xLyra 逐个检查动作（权限已授予、目标在绑定范围内），然后执行，并把结果写进操作记录。

权限决定"能做哪类操作"，绑定决定"能作用于谁"。两者缺一不可。

## manifest

```json
"automation": {
  "subscribes": ["oauth.quota_synced"],
  "permissions": ["apikey.reset_usage"],
  "binding": {
    "subject": { "type": "oauth_connection", "providers": ["codex", "claude_code"] },
    "target": { "type": "api_key", "requires": "finite_total_quota" },
    "config": {
      "type": "object",
      "properties": {
        "resetOnRecovery": { "type": "boolean", "title": "额度提前恢复时也重置", "default": true }
      }
    }
  }
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `subscribes` | 是 | 订阅的事件，见下表，至少一个。同一个插件的事件必须属于同一种对象 |
| `permissions` | 否 | 会返回的动作类型。返回没有声明的动作类型会被拒绝 |
| `binding.subject.type` | 是 | 绑定到哪种对象，目前只有 `oauth_connection` |
| `binding.subject.providers` | 否 | 只允许这些 provider 的账号 |
| `binding.target.type` | 看动作 | 可操作的对象类型，目前只有 `api_key`。声明了 `apikey.reset_usage` 就必须写 |
| `binding.target.requires` | 否 | 对每个目标的要求。`finite_total_quota`：必须设置了有限的总额度 |
| `binding.config` | 否 | 管理员要填的参数，见下文 |

### 事件

| 事件 | 对象 | 何时发生 |
| --- | --- | --- |
| `oauth.quota_synced` | `oauth_connection` | 一个 OAuth 账号的额度同步并保存之后 |

### 权限与动作

| 权限 / 动作 | 作用 |
| --- | --- |
| `apikey.reset_usage` | 清零一个已绑定 API 密钥的已用量（`total`、`daily`、`weekly`） |
| `notify` | 在这个插件的操作记录里写一条通知 |

### 参数（binding.config）

`config` 是 JSON Schema 的一个**小子集**：顶层是 `object`，`properties` 里每一项是扁平的 `string`、`number`、`integer` 或 `boolean`，可以带 `title`、`description`、`default`、`enum`、`minimum`、`maximum`，顶层可以写 `required`。最多 16 项，不支持嵌套。

管理员保存绑定时，xLyra 按它校验并补齐默认值，多余的键会被拒绝。`handle` 通过 `ctx.config` 读到的就是校验后的结果。

## 输入

`ctx`：

| 字段 | 说明 |
| --- | --- |
| `event` | 当前事件类型 |
| `now` | 当前时间，Unix 毫秒 |
| `bindingId` | 这条绑定的 id |
| `config` | 管理员填写的参数 |
| `state` | 这条绑定上次返回的 `state`，第一次为空对象 |

`event`：

| 字段 | 说明 |
| --- | --- |
| `type` | 事件类型 |
| `subject` | 事件发生在谁身上：`type`、`id`、`provider`、`label`（比如账号邮箱） |
| `previous` | 变化之前的数据，第一次没有 |
| `current` | 变化之后的数据 |
| `targets` | 管理员绑定的对象和它们当前的用量，最多 200 个 |

`oauth.quota_synced` 的 `previous` / `current` 是账号保存的额度对象，里面的窗口形如 `weekly: { reset_at, remaining_percent, ... }`。`reset_at` 通常是 Unix 秒，也可能是毫秒或日期字符串，请自己做兼容。

`targets[]` 的每一项：

| 字段 | 说明 |
| --- | --- |
| `type`、`id`、`name` | 对象类型、id、名称。动作里的 `target` 用这里的 `id` |
| `usage` | API 密钥为 `totalUsed`、`totalLimit`、`dailyUsed`、`weeklyUsed` |

## 返回

```ts
{
  actions: [
    { type: "apikey.reset_usage", target: "<targets[].id>", scope: "total", idempotencyKey: "1791331200" },
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
| `target` | `apikey.reset_usage` | 必须是 `event.targets` 里的某个 `id` |
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

样本用 `ctx` 提供 `config` 和 `state`，`input` 是 `event`，`expect.result` 是期望的返回：

```json
{
  "name": "weekly window rolled over",
  "ctx": { "now": 2000000, "config": { "resetOnRecovery": true } },
  "input": {
    "type": "oauth.quota_synced",
    "subject": { "type": "oauth_connection", "id": "conn-1", "provider": "codex" },
    "previous": { "weekly": { "reset_at": 1000, "remaining_percent": 2 } },
    "current": { "weekly": { "reset_at": 9000, "remaining_percent": 100 } },
    "targets": [{ "type": "api_key", "id": "key-1", "name": "alice", "usage": { "totalUsed": 12 } }]
  },
  "expect": {
    "result": { "actions": [{ "type": "apikey.reset_usage", "target": "key-1", "scope": "total", "idempotencyKey": "9000000" }] }
  }
}
```

`xlyra-plugin init --kind automation` 会生成一个完整的示例：判断周额度窗口是否滚动，滚动了就清零绑定密钥的总用量。

## 管理员怎么使用

1. 上传 `.xlp`，在开发者页启用版本。弹窗会列出它声明的权限，勾选确认后才会启用。
2. 打开插件，在"自动化"区点"新增自动化"：选择 OAuth 账号，选择可操作的 API 密钥（只能选设置了有限总额度的），填写参数。
3. 之后账号每次同步额度，插件都会被调用。"最近操作"里能看到它做了什么、成功还是失败。

升级插件时，新版本启用后，已有的绑定会自动跟着新版本，不需要重新创建。权限需要重新确认。
