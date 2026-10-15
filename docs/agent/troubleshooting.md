# 常见问题

## 左侧没有 Agent 入口

入口只在已配置 Runner 地址时显示。到 `/settings/agent` 填写 Runner 地址和 Token 并保存，见[设置页](./settings.md#runner-连接)。如果配置过又消失，检查是否点过「清空配置」。

## Runner 状态显示「不可达」

xLyra 连不上 Runner。依次检查：

1. Runner 容器是不是在运行：`docker ps`。
2. 地址是不是写对了。xLyra 和 Runner 在同一个 compose 里时写服务名，如 `http://xlyra-agent-runner:3211`；不要写 `127.0.0.1`。
3. 两个容器是不是在同一个 Docker 网络里；不在同一个 compose 的话，需要自己建立互通。
4. 防火墙有没有挡住 3211 端口。

## 状态灯一直是「未知」，或者对话提示未授权

多半是 Token 不一致，Runner 拒绝了 xLyra 的请求。Runner 容器里的 `RUNNER_INTERNAL_TOKEN` 和 `/settings/agent` 里填的必须完全相同，改了容器的环境变量要重新创建容器才生效。

## Runner 容器启动后马上退出

没设置必填项。`RUNNER_INTERNAL_TOKEN` 和 `XLYRA_BASE_URL` 都必须有，用 `docker logs xlyra-agent-runner` 看具体缺哪个。

## 对话发出去没反应，或提示模型不可用

- 检查 Agent 能用的[站点与模型范围](./settings.md#站点与模型范围)，选的模型是否在范围内、站点是否启用。
- 在 xLyra 里确认该模型对应的站点有可用的密钥，没有冷却或失效。Agent 的请求和其他请求走同一套路由。
- Runner 访问不到 xLyra 也会这样，检查 `XLYRA_BASE_URL` 是否是 Runner 容器里能访问到的地址。

## 升级失败

页面会显示失败原因，修好后点「重试升级」。常见原因是 Runner 所在机器访问不了 npm 官方源，这时设置 `NPM_REGISTRY` 为可访问的镜像源，重新创建 Runner 容器后再升级。

## 对话记录、记忆丢了

数据都在 Runner 的两个数据目录里。如果容器重建后丢失，说明这两个目录没有挂载出来，见 [Runner 容器](./runner.md#数据目录)。

## 想重新开始

先停掉 Runner 容器，备份后删除 `./xlyra-agent` 和 `./xlyra-agent-runner` 两个目录，再启动。会清空所有对话、记忆、Skills 和已安装的 Agent，Runner 会重新安装。
