# Runner 容器

Runner 是 Agent 的运行容器。xLyra 通过它使用 Agent，Agent 通过它向 xLyra 要模型。镜像：`yachiiiiyo/xlyra-agent-runner`。

## 最快的做法：加进 xLyra 的 compose

把下面这段加到 xLyra 的 `docker-compose.yml` 里，和 `xlyra` 服务放在同一个文件，它们会自动在同一个网络里，互相用服务名访问。

```yaml
  xlyra-agent-runner:
    image: yachiiiiyo/xlyra-agent-runner:latest
    container_name: xlyra-agent-runner
    restart: unless-stopped
    environment:
      RUNNER_INTERNAL_TOKEN: 换成一串足够长的随机字符
      XLYRA_BASE_URL: http://xlyra:5801
    volumes:
      - ./xlyra-agent:/data/xlyra-agent
      - ./xlyra-agent-runner:/data/xlyra-agent-runner
    ports:
      - "3211:3211"
    networks:
      - xlyra
```

然后执行：

```bash
docker compose up -d
```

生成随机 Token 可以用：

```bash
openssl rand -hex 32
```

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `RUNNER_INTERNAL_TOKEN` | 是 | 无 | Runner 与 xLyra 共用的密钥。xLyra 的 `/settings/agent` 里要填同一个值 |
| `XLYRA_BASE_URL` | 是 | 无 | xLyra 的访问地址，只写到端口，例如 `http://xlyra:5801`。同一个 compose 里用服务名；不在同一台机器时写可访问的 IP 或域名 |
| `PORT` | 否 | `3211` | Runner 监听端口。改了之后，xLyra 里填的 Runner 地址也要跟着改 |
| `XLYRA_AGENT_VERSION` | 否 | `latest` | 安装哪个版本的 Agent。想固定版本就写具体版本号，如 `1.2.3` |
| `AGENT_MANAGED` | 否 | `true` | `true` 表示由 Runner 安装和升级 Agent；`false` 表示你自己管理 Agent，设置页里不会出现升级按钮 |
| `NPM_REGISTRY` | 否 | `https://registry.npmjs.org` | 下载 Agent 用的 npm 源。服务器访问不了官方源时改成镜像源 |

没有设置 `RUNNER_INTERNAL_TOKEN` 或 `XLYRA_BASE_URL` 时，容器会启动失败，看日志就能发现。

## 数据目录

| 容器内路径 | 建议挂载 | 里面是什么 |
| --- | --- | --- |
| `/data/xlyra-agent` | `./xlyra-agent` | Agent 的配置、所有对话记录、工作区文件、记忆和 Skills |
| `/data/xlyra-agent-runner` | `./xlyra-agent-runner` | Runner 自己的数据，包括 Agent 的安装版本 |

这两个目录都要挂载出来，否则容器重建后对话记录、记忆和已安装的 Agent 都会丢失。备份时备份这两个目录即可。

## 容器里有什么

Agent 在容器里执行命令，所以镜像预装了常用工具：`bash`、`git`、`curl`、`jq`、`ripgrep`、`python3` 和 `pnpm`。没有编译器，需要的话请基于该镜像自行扩展。

## 单独部署时要注意

如果 Runner 和 xLyra 不在同一个 compose 里：

- 把 `XLYRA_BASE_URL` 写成 Runner 能访问到的 xLyra 地址。
- 在 xLyra 的 `/settings/agent` 里填 Runner 的地址时，也要写成 xLyra 能访问到的 Runner 地址，不能写 `127.0.0.1`（那会指向 xLyra 容器自己）。
- 两个方向都要能通：xLyra 访问 Runner 的 3211 端口，Runner 访问 xLyra 的 5801 端口。

## 升级

- Agent 本身：在 `/settings/agent` 页面点升级，见[设置页](./settings.md#升级-agent)。
- Runner 镜像：`docker compose pull xlyra-agent-runner && docker compose up -d xlyra-agent-runner`。数据目录保留，对话记录不受影响，升级时正在运行的对话会中断。
