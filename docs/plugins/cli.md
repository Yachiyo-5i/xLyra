# 命令行工具 xlyra-plugin

开发者本地测试和线上运行用的是**同一份运行时代码和同一个引擎版本**，所以本地通过的样本，上传后也会通过。打包用内置的 esbuild，不需要安装 Node。

```bash
xlyra-plugin --version
```

打印 CLI 版本、`apiVersion`、`hostApi` 和引擎版本。

所有带 `[dir]` 的命令，目录默认是当前目录。

## init

```bash
xlyra-plugin init [--kind quota_probe|protocol] [--id ID] [--name NAME] <dir>
```

在 `dir` 里创建新工程（目录必须不存在或为空）。`--kind` 默认 `quota_probe`，可以是 `xlyra-plugin kinds` 列出的任意一种；每种类型的模板都带有可直接通过的示例样本。`--id` 默认由目录名推导，不合规会报错并提示用 `--id`。生成的工程可以直接 `test` 通过。

## kinds

```bash
xlyra-plugin kinds
```

列出当前版本支持的所有 `kind`，包括工作方式（族）、导出的函数、凭据，以及管理员怎么让它生效。选择方法见[选择类型](./kinds.md)。

## build

```bash
xlyra-plugin build [--out dist] [dir]
```

把 `src/index.ts`（或 `src/index.js`）用 esbuild 打成单文件 ES2023 模块，写出：

```text
dist/manifest.json     已填好 sha256
dist/plugin.js
dist/fixtures/*.json
```

构建后还会像服务端那样加载一遍，所以清单错误、`meta` 不匹配等问题在这里就能发现。外部依赖和动态 `import()` 会被拒绝，见[限制](./limits-and-security.md)。`build` 不会删除 `dist/` 里已有的 `.xlp`。

## test

```bash
xlyra-plugin test [--watch] [dir]
```

构建后逐个运行 `fixtures/*.json`，打印每个样本的结果；有失败则退出码为 1。`--watch` 监听 `manifest.json`、`src/`、`fixtures/` 和 `tsconfig.json` 的变化并自动重跑。

## run

```bash
xlyra-plugin run --key-env VAR [--base-url URL] [--site-type T] [--credential-type C] [dir]
```

对真实站点发一次额度探测。

- `--key-env`：存放密钥的环境变量名，**必填**。密钥只会写进 `Authorization` 头，插件看不到。
- `--base-url`：站点地址，会按清单的 `baseURLMode` 整理；省略则使用 `defaultBaseURL`。
- 实际发出的每个请求会打印到标准错误，插件的日志也会打印，最终结果以 JSON 输出到标准输出。

支持会向站点取数据的类型：`quota_probe`、`model_list`、`credential_check`、`site_detect`。`site_detect` 不带凭据，不需要 `--key-env`，请求也不会有 `Authorization` 头。`protocol` 和一次性函数类型请用 `test` 验证。

## pack

```bash
xlyra-plugin pack [--sign key.pem] [--out file.xlp] [dir]
```

构建并生成 `.xlp`，默认写到 `dist/<id>-<version>.xlp`。写文件前会做和 `verify` 相同的检查，不合格就不生成。`--sign` 用私钥签名。

## verify

```bash
xlyra-plugin verify <package.xlp>
```

对已有的包做服务端上传时的全部检查：结构、清单、sha256、签名、加载、运行全部样本。通过时打印 id、版本、kind、样本数、签名者指纹和包摘要。

## keygen / sign

```bash
xlyra-plugin keygen [--out key.pem]
xlyra-plugin sign <package.xlp> --key key.pem [--out signed.xlp]
```

`keygen` 生成 ed25519 私钥（PEM，权限 `0600`），并输出 `public_key`（base64）和 `fingerprint`。**私钥请自己妥善保管，不要提交到仓库。** `sign` 给已有的包附上签名；`pack --sign` 是同样的事。
