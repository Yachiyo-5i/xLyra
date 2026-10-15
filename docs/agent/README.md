# xLyra Agent 使用文档

Agent 是 xLyra 里的对话式助手：在 `/agent` 页面里说一件事，它会自己读写文件、运行命令、搜索和访问网页，并把过程展示出来。它用的模型来自 xLyra 里已经配置好的站点，不需要另外填模型密钥。

## 由哪几部分组成

| 部分 | 是什么 | 在哪里配置 |
| --- | --- | --- |
| xLyra | 网关本体，提供 `/agent` 和 `/settings/agent` 页面，并给 Agent 提供模型 | 本项目 |
| Runner 容器 | 一个独立的 Docker 容器，里面装着 Agent 并负责运行、升级它 | [Runner 容器](./runner.md) |
| Agent | 一个 npm 包（`@yachiyo-5i/xlyra-agent`），由 Runner 自动安装和升级，不需要你单独安装 | 无需配置 |

使用前需要先部署 Runner，再在 xLyra 里填上 Runner 的地址和 Token。

## 配置顺序

1. [部署 Runner 容器](./runner.md)：设置 Token，并让它能访问 xLyra。
2. [在 `/settings/agent` 连接 Runner](./settings.md)：填 Runner 地址和同一个 Token，保存。
3. 在同一页面决定 Agent 可以使用哪些站点和模型（默认全部可用）。
4. 打开 `/agent`，开始对话，用法见[使用 Agent 工作台](./workspace.md)。

## 阅读顺序

| 文档 | 内容 |
| --- | --- |
| [Runner 容器](./runner.md) | 镜像、compose 示例、环境变量、数据目录 |
| [Agent 设置页](./settings.md) | `/settings/agent` 每一项的含义与保存方式 |
| [使用 Agent 工作台](./workspace.md) | `/agent` 页面的对话、权限模式、附件，以及右上角的配置 |
| [常见问题](./troubleshooting.md) | 入口不见了、状态不可达、升级失败等 |

## 安全须知

Agent 能在 Runner 容器里执行命令、读写文件和访问网络，请只给可信的管理员使用，并注意：

- Runner 的 3211 端口不要直接暴露到公网，只让 xLyra 能访问到。
- Runner Token 相当于 Runner 的管理密码，请使用足够长的随机值，不要和其他系统共用。
- Agent 的文件都在 Runner 的数据目录里，容器之外的机器文件默认碰不到。
