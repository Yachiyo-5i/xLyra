# 快速开始

目标：给一个虚构的中转站写额度探测插件，并让管理员装上它。如果你要做的不是额度探测，先看[选择插件类型](./kinds.md)，并在 `init` 时用 `--kind` 指定。

## 1. 准备

只需要 `xlyra-plugin` 一个可执行文件，不需要 Node。

> 预编译版本还没有提供下载。目前请从源码构建（需要 Go）：
>
> ```bash
> cd server && go build -o xlyra-plugin ./cmd/xlyra-plugin
> ```

确认可用：

```bash
xlyra-plugin --version
```

会打印 `apiVersion`、`hostApi` 和引擎版本。

## 2. 创建工程

```bash
xlyra-plugin init --kind quota_probe my-relay-probe
cd my-relay-probe
```

目录里是：

```text
manifest.json            插件清单
src/index.ts             钩子实现
fixtures/example.json    测试样本
tsconfig.json
types/xlyra.d.ts         类型声明（和 npm 包 @yachiyo-5i/xlyra-plugin-sdk 一致）
```

插件 id 默认取目录名，也可以用 `--id` 指定。id 规则见[清单](./manifest.md)。

## 3. 写插件

打开 `src/index.ts`。额度探测只需要实现一个 `probe` 钩子：

```ts
import type { ProbeContext, ProbeDecision, ProbeStep } from "../types/xlyra";

export const meta = { apiVersion: 1, id: "my-relay-probe", kind: "quota_probe" };

export function probe(_ctx: ProbeContext, steps: ProbeStep[]): ProbeDecision {
  if (steps.length === 0) {
    return { request: { method: "GET", path: "/api/usage" } };
  }
  const resp = steps[0].response;
  if (resp.status !== 200 || resp.json == null) {
    return { error: `usage endpoint returned ${resp.status}` };
  }
  const limit = Number(resp.json.total);
  const used = Number(resp.json.used);
  return {
    result: {
      kind: "balance",
      entries: [{ label: "balance", unit: "usd", limit, used, remaining: Math.max(0, limit - used) }],
    },
  };
}
```

`probe` 会被调用多次：第一次 `steps` 为空，你返回要发的请求；xLyra 发完后带着响应再调用一次；直到你返回 `result` 或 `error`。详见[额度探测插件](./quota-probe.md)。

不要写成 `import x from "pkg"`，插件里不能有外部依赖，见[限制](./limits-and-security.md)。`import type` 可以用，打包后会消失。

## 4. 写测试样本并运行

`fixtures/example.json` 描述"上游返回什么，插件应该得到什么"，格式见 [fixtures](./fixtures.md)。运行：

```bash
xlyra-plugin test            # 跑一次
xlyra-plugin test --watch    # 文件变化后自动重跑
```

输出每个样本的通过或失败，失败时会指出不一致的字段。

## 5. 对真实站点试一次（可选）

```bash
export RELAY_KEY=sk-...
xlyra-plugin run --key-env RELAY_KEY --base-url https://relay.example
```

密钥从环境变量读取，只写进请求头，插件看不到。输出会列出实际发出的请求和最终结果。目前 `run` 只支持额度探测插件。

## 6. 打包与签名

```bash
xlyra-plugin keygen --out my-key.pem      # 只需第一次；记下输出的 public_key
xlyra-plugin pack --sign my-key.pem       # 生成 dist/<id>-<version>.xlp
xlyra-plugin verify dist/my-relay-probe-0.1.0.xlp
```

`pack` 在写文件前会做和服务端上传相同的检查，不合格不会生成包。签名是可选的，区别见[发布与安装](./publishing.md)。

## 7. 交给管理员

把 `.xlp` 文件（以及你的公钥）发给 xLyra 管理员。管理员在后台"开发者"页面上传并启用。额度探测插件还需要由管理员绑定到站点，详见[发布、签名与安装](./publishing.md)。
