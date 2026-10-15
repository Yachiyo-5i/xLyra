# 宿主 API v1

插件里可以直接使用下面这些全局对象，没有 `import`。它们都是**纯计算**，不访问网络、文件或环境变量。类型声明见 `types/xlyra.d.ts`。

参数类型不对或计算失败时，函数会**抛出普通的 JS 异常**，可以用 `try/catch` 接住；不接住则整个钩子调用失败，记为 `js_exception`。

## codec

| 函数 | 说明 |
| --- | --- |
| `codec.base64Encode(s)` | 标准 base64 编码 |
| `codec.base64Decode(s)` | 标准 base64 解码，结果必须是合法 UTF-8，否则抛异常 |
| `codec.base64URLEncode(s)` | URL 安全 base64，无填充 |
| `codec.base64URLDecode(s)` | 对应的解码 |

## crypto

| 函数 | 说明 |
| --- | --- |
| `crypto.sha256(message)` | 返回**十六进制**摘要 |
| `crypto.hmacSHA256(key, message)` | 返回 **base64**（标准编码）的 HMAC-SHA256 |

注意两个函数的输出编码不同。

## jwt

| 函数 | 说明 |
| --- | --- |
| `jwt.signHS256(payload, secret)` | 用 HS256 签发 JWT，`payload` 必须是对象 |
| `jwt.decodeHS256(token, secret)` | 校验签名并返回载荷；算法不是 HS256、签名不对、载荷不是对象时抛异常 |

这里的 `secret` 是你从别处得到的，**不是** xLyra 的上游密钥。插件拿不到上游密钥，所以基于 JWT 的上游鉴权目前不能由插件完成。

## utils

| 函数 | 说明 |
| --- | --- |
| `utils.uuid()` | 随机 UUID v4 |

## log

```ts
log.debug("message", { key: "value" });
log.info("message");
log.warn("message", { ... });
```

- 日志会跟着这次调用一起被收集，写入 xLyra 的服务日志；`xlyra-plugin run` 会把它们打印到标准错误。
- 每次调用最多保留 **20** 条，每条（消息加字段）最多 **1024** 字节，超出会截断或丢弃。
- **不要把响应里的敏感内容写进日志。**

## 语言环境

- 引擎是 moejs，ES 模块，支持 ES2023 的常用特性（如 `Array.prototype.at`、`Object.groupBy`），时区固定为 UTC。
- 可用的内置对象：`Date`、`Math`、`JSON`、`TextEncoder`、`structuredClone`、`globalThis` 等标准对象。
- `eval` 和 `new Function` 虽然存在，但调用会抛出 `EvalError`（动态代码被禁用）；`import()` 同样不可用。
- **没有** `fetch`、`XMLHttpRequest`、`process`、`require`、`console`、`URL`、`setTimeout`/`setInterval`。需要输出调试信息请用 `log`，URL 和查询串自己拼。
- 每次调用都在干净的环境里执行，**不要依赖模块级变量保存状态**：运行时会被池复用，状态不能当作跨调用的缓存。
