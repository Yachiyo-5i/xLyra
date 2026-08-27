# Changelog

## [1.3.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.3.0...v1.3.1) (2026-08-27)


### Bug Fixes

* 🐛 优化 Playground 操作布局并完善代码复制与生图结果信息展示 ([1d37ac0](https://github.com/Yachiyo-5i/xLyra/commit/1d37ac03d37fd058bd691c764697f2f1ad68797a))
* 🐛 修复弹窗面板内下拉列表无法滚动导致选项显示不全的问题 ([8b71f29](https://github.com/Yachiyo-5i/xLyra/commit/8b71f291c60b97c93d8cf8aae9e364145836b942))

## [1.3.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.2.3...v1.3.0) (2026-08-24)


### Features

* 🎸 新增模型体验记录跨浏览器持久化与断点续传 ([74815a5](https://github.com/Yachiyo-5i/xLyra/commit/74815a54a1ed1561c4ff5f8d05dd506eebe59ad7))


### Bug Fixes

* 🐛 修复备份恢复未覆盖模型体验会话记录与图片附件的问题 ([8225a03](https://github.com/Yachiyo-5i/xLyra/commit/8225a036773d567844e16d94a8d5ffe8ae2a4cbf))
* 🐛 修复大文件备份还原限制并完善后台恢复体验 ([48fb46d](https://github.com/Yachiyo-5i/xLyra/commit/48fb46d783cbedc19ff94973ea89d2dee2d4abbd))
* 🐛 修复站点与 API 密钥提交阻塞并完善后台同步体验 ([7516eb4](https://github.com/Yachiyo-5i/xLyra/commit/7516eb4ab429314d32de423e8c32671b9c51e38d))
* 🐛 修正订阅限额冷却校准的查询写法以符合项目数据库访问规范 ([a7e48d4](https://github.com/Yachiyo-5i/xLyra/commit/a7e48d4906918fe018d93db744c768dc80481f95))
* 🐛 修复 TokenPlan 周限额冷却时间计算错误 ([5d963e1](https://github.com/Yachiyo-5i/xLyra/commit/5d963e1151884e61acdc381ce6bb8d9e24e73622))

## [1.2.3](https://github.com/Yachiyo-5i/xLyra/compare/v1.2.2...v1.2.3) (2026-08-23)


### Bug Fixes

* 🐛 修复推理强度错误提示并兼容完整枚举 ([fe411f2](https://github.com/Yachiyo-5i/xLyra/commit/fe411f271e54b84f799de4b9b732796246d17f77))
* 🐛 修复跨协议请求的缓存用量回传与上游协议选择 ([5bf3af7](https://github.com/Yachiyo-5i/xLyra/commit/5bf3af7ade801294be920f1862c35164d52335f3))
* 🐛 减少页面认证初始化等待并防止登录状态被错误缓存 ([5cba9ea](https://github.com/Yachiyo-5i/xLyra/commit/5cba9ea27f3cfb34f39aee9f070ddd263d14c361))

## [1.2.2](https://github.com/Yachiyo-5i/xLyra/compare/v1.2.1...v1.2.2) (2026-08-18)


### Bug Fixes

* 🐛 修复 Grok 请求在不同协议间的参数兼容问题 ([3d91bb2](https://github.com/Yachiyo-5i/xLyra/commit/3d91bb293d2d0c83dd7c013111add8cba388afff))
* 🐛 修复中等屏幕下仪表盘指标卡布局不一致和内容换行的问题 ([f882361](https://github.com/Yachiyo-5i/xLyra/commit/f882361b8be73d9f29d240c724a62ab653ff1f88))
* 🐛 修复费用构成浮层层级及移动端无法关闭的问题 ([fabf448](https://github.com/Yachiyo-5i/xLyra/commit/fabf4481878f713b8636f6029cff13e1bb6c724d))
* 🐛 完善上游站点套餐额度详情与移动端查看体验 ([f5b622f](https://github.com/Yachiyo-5i/xLyra/commit/f5b622f810fb76a0357decc449601c1418be9ddf))
* 🐛 用量分析新增昨天时间范围与单日按小时查看，筛选切换即时生效 ([aa70a7b](https://github.com/Yachiyo-5i/xLyra/commit/aa70a7b7381b425986163fb1bce9ab1b7f5c4c8e))
* 🐛 用量分析新增昨天时间范围与单日按小时查看，筛选切换即时生效 ([a2684b5](https://github.com/Yachiyo-5i/xLyra/commit/a2684b536d441dd72c541f3bda6196d20a4c345c))
* 修复 Grok Responses 跨协议参数兼容问题 ([d55a605](https://github.com/Yachiyo-5i/xLyra/commit/d55a605fa66377125074b56c9acd4405a5184b49))

## [1.2.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.2.0...v1.2.1) (2026-08-16)


### Bug Fixes

* 🐛 修复推理强度选择错误及失败请求无法显示模型的问题 ([e8f097b](https://github.com/Yachiyo-5i/xLyra/commit/e8f097b937f6773c26459c0b838e93c1cd93f212))

## [1.2.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.1.3...v1.2.0) (2026-08-15)


### Features

* 🎸 新增用量分析独立页面与接口 ([481ac74](https://github.com/Yachiyo-5i/xLyra/commit/481ac7445aff0bc010c6a8bb96498abe2617dc45))


### Bug Fixes

* 🐛 优化前端页面交互与展示体验 ([c460b06](https://github.com/Yachiyo-5i/xLyra/commit/c460b06520da7de56c9a66209e788467b0b7fc1e))
* 🐛 修复 Codex 上游拒绝新版提示缓存参数的问题 ([f6e99a9](https://github.com/Yachiyo-5i/xLyra/commit/f6e99a912e3bacdaaac456bbee416be606ab0822))
* 🐛 修复 Codex 上游拒绝新版提示缓存参数的问题 ([ac4be61](https://github.com/Yachiyo-5i/xLyra/commit/ac4be61c3805232605e96a96fafcbce852a6babb))
* 🐛 修复服务模式和推理强度识别及长上下文计费展示 ([74e0109](https://github.com/Yachiyo-5i/xLyra/commit/74e01096a1cb8ac312a239a0002262ed6d9eb0f9))
* 🐛 修复缓存用量统计与缓存写入汇总缺失 ([c2dfa88](https://github.com/Yachiyo-5i/xLyra/commit/c2dfa88298980d2ca72b34783cc386eca28a7a83))
* 🐛 修复缓存观测亲和在多凭据轮转时永久失效及相关问题 ([ba40d7f](https://github.com/Yachiyo-5i/xLyra/commit/ba40d7fa433929df57695241a5d7868a0ab0c639))
* 🐛 修复缓存路由观测、凭据缓存域更新和历史缓存费用统计异常 ([a2be711](https://github.com/Yachiyo-5i/xLyra/commit/a2be71184e9825a2756242eefdc0fba9b76dd49d))
* 🐛 修复请求切换记录不准确并优化切换过程展示 ([ad5e9d7](https://github.com/Yachiyo-5i/xLyra/commit/ad5e9d7b92583f266125428dee37ed4b6b19f3fd))
* 🐛 修正缓存观测的亲和判定与过期边界 ([4ab1815](https://github.com/Yachiyo-5i/xLyra/commit/4ab18156a71126f70c5fca093f96edd7e56a8ce4))
* 🐛 升级 Go 版本至 1.26.6 修复标准库安全漏洞 ([6593a94](https://github.com/Yachiyo-5i/xLyra/commit/6593a94beabb0359024060ea22f65960b9aab829))
* 🐛 合并主分支并解决缓存统计与请求记录冲突 ([485abf3](https://github.com/Yachiyo-5i/xLyra/commit/485abf380f03bc8dc4d197e72fdd2bea74710664))
* 🐛 增加多轮请求的缓存命中观测 ([b6245c3](https://github.com/Yachiyo-5i/xLyra/commit/b6245c3c0213c7e08b1a1d06547f6ff68ff53c86))
* 🐛 精简 Dashboard 页面并减少首屏加载的数据量 ([a58fd3d](https://github.com/Yachiyo-5i/xLyra/commit/a58fd3ded5dfaa880fd7369b69ff3ff5a42c83cd))
* 🐛 补齐缓存写入的结构化成本统计 ([b0fd71f](https://github.com/Yachiyo-5i/xLyra/commit/b0fd71f7591b116ab4532c5f085837531a506a97))
* improve gateway failover diagnostics ([34a6275](https://github.com/Yachiyo-5i/xLyra/commit/34a6275bb24f7bcd76459054fb17f48368947295))


### Performance Improvements

* ⚡️ 加快大数据备份恢复并展示实时进度 ([708eea7](https://github.com/Yachiyo-5i/xLyra/commit/708eea78e0b407e63e018f5b5eb5955efe329737))

## [1.1.3](https://github.com/Yachiyo-5i/xLyra/compare/v1.1.2...v1.1.3) (2026-08-13)


### Bug Fixes

* 🐛 修复多级代理连接 Codex OAuth 时命名空间工具调用被拒绝的问题 ([02d51a0](https://github.com/Yachiyo-5i/xLyra/commit/02d51a041e80f56514dd7c82d151e31839914efb))
* 🐛 修复站点自定义请求头删除后仍被保留的问题 ([07cc9b2](https://github.com/Yachiyo-5i/xLyra/commit/07cc9b25458772f6c6b6821c355ff7959b3373d5))

## [1.1.2](https://github.com/Yachiyo-5i/xLyra/compare/v1.1.1...v1.1.2) (2026-08-12)


### Bug Fixes

* 🐛 优化模型体验区的附件粘贴和生图参数选择体验 ([f58756b](https://github.com/Yachiyo-5i/xLyra/commit/f58756bc5664e4a657f714f8500e73d93d60b6fb))
* 🐛 修复流式响应在业务输出前无法自动切换路由的问题 ([6e15c32](https://github.com/Yachiyo-5i/xLyra/commit/6e15c3200d489a8ec23b7be8ad5ac880ccbacbf2))

## [1.1.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.1.0...v1.1.1) (2026-08-11)


### Bug Fixes

* 🐛 修复流式响应空输出被误判并确保过载后正常切换的问题 ([4cf4019](https://github.com/Yachiyo-5i/xLyra/commit/4cf4019fd760e783b8f4370fe944fa8c96319c43))
* fail over pre-output response overloads ([add5a77](https://github.com/Yachiyo-5i/xLyra/commit/add5a77e11b4392310413218c68f7017895734e5))
* fail over pre-output response overloads ([d7af1f2](https://github.com/Yachiyo-5i/xLyra/commit/d7af1f2b6ca3cce73356f75e8b0649f49ac6c062))
* preserve pre-output stream failure semantics ([c1816f4](https://github.com/Yachiyo-5i/xLyra/commit/c1816f445b264d7a49126b113f52293c08854288))

## [1.1.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.0.6...v1.1.0) (2026-08-10)


### Features

* ✨ 增强请求日志列表与详情 ([ab152bb](https://github.com/Yachiyo-5i/xLyra/commit/ab152bbe43d5500a01375e1b298034145161343a))
* ✨ 增强请求日志列表与详情 ([d58ac9c](https://github.com/Yachiyo-5i/xLyra/commit/d58ac9cd755c719ece7fdaed996bf7da8296979b))


### Bug Fixes

* 🐛 优化请求日志的计费展示与移动端布局 ([a18832e](https://github.com/Yachiyo-5i/xLyra/commit/a18832eafe03f6fb01100faaab6c53ef6643fb91))
* 🐛 修复 OpenCode 冷门模型无法路由的问题 ([578f65f](https://github.com/Yachiyo-5i/xLyra/commit/578f65f881aca82633e8f9d7ffae18e8d4d2c91a))
* 🐛 修复超长工具调用标识冲突导致的请求失败 ([35735fa](https://github.com/Yachiyo-5i/xLyra/commit/35735fa02b0a1867416a9ef783ea938291310026))

## [1.0.6](https://github.com/Yachiyo-5i/xLyra/compare/v1.0.5...v1.0.6) (2026-08-09)


### Bug Fixes

* 🐛 优化 Playground 模型选择体验 ([3bfbb73](https://github.com/Yachiyo-5i/xLyra/commit/3bfbb7305cf6e22f1dce6453607f8dc19941a152))
* 🐛 修复上游非成功响应被误判为语义失败导致触发错误冷却的问题 ([b2febf3](https://github.com/Yachiyo-5i/xLyra/commit/b2febf3d0006e494151be2d7e18b2b1eb6118e8b))
* 🐛 修复自动备份未清理历史版本导致存储空间占满的问题 ([b413996](https://github.com/Yachiyo-5i/xLyra/commit/b41399666c0688fa22e54100fd0c93c7c9ff21eb))
* **gateway:** classify semantic upstream failures ([7b4f449](https://github.com/Yachiyo-5i/xLyra/commit/7b4f449181c5a186bb500ffd68f4267724c36f98))
* **gateway:** 正确处理 2xx 响应中的语义上游失败 ([bd59404](https://github.com/Yachiyo-5i/xLyra/commit/bd59404e89474cbb5ede860cbd89b4a3aa3c81a3))

## [1.0.5](https://github.com/Yachiyo-5i/xLyra/compare/v1.0.4...v1.0.5) (2026-08-07)


### Bug Fixes

* 🐛 修复跨协议请求因缺少输出长度导致上游调用失败 ([eec7dd1](https://github.com/Yachiyo-5i/xLyra/commit/eec7dd166b61a44dd6eda50f982641457646f16b))

## [1.0.4](https://github.com/Yachiyo-5i/xLyra/compare/v1.0.3...v1.0.4) (2026-08-07)


### Bug Fixes

* 🐛 支持下游密钥可重置总限额并保留累计消耗 ([b1ece74](https://github.com/Yachiyo-5i/xLyra/commit/b1ece74a663993d1ed446c5944fdbd0c431ef18e))

## [1.0.3](https://github.com/Yachiyo-5i/xLyra/compare/v1.0.2...v1.0.3) (2026-08-06)


### Bug Fixes

* 🐛 修复 MiMo TTS 模型无法通过语音合成接口调用的问题 ([c8d3ed8](https://github.com/Yachiyo-5i/xLyra/commit/c8d3ed8de0d134d5fe0b3569be391e0658d354f2))
* 🐛 修复小米 MiMo V2.5 语音合成模型的调用兼容问题 ([4f7aaf1](https://github.com/Yachiyo-5i/xLyra/commit/4f7aaf1639faabdaa11f3ec3d1dc45de724fb9b2))
* 🐛 避免订阅额度耗尽后持续请求上游 ([ed384ec](https://github.com/Yachiyo-5i/xLyra/commit/ed384ec004ad56b45d57e36d9d38da6b5c7de31e))

## [1.0.2](https://github.com/Yachiyo-5i/xLyra/compare/v1.0.1...v1.0.2) (2026-08-05)


### Bug Fixes

* 🐛 Anthropic 类型站点支持余额探测配置与 Apikey 组倍率，新增 Apikey 时明文显示输入内容 ([0ee215c](https://github.com/Yachiyo-5i/xLyra/commit/0ee215c87a73d6e800057bd023ff305e2bda0f3f))
* 🐛 修复 Anthropic 模型缓存用量统计错误及模型映射后缓存失效的问题，并优化请求列表缓存数据展示 ([b4ae4b0](https://github.com/Yachiyo-5i/xLyra/commit/b4ae4b076b0748c43db71f116e8dc125813c47d3))
* 🐛 修复 Codex 等 OAuth 站点模型价格未随标准价格变更实时同步的问题 ([4064a10](https://github.com/Yachiyo-5i/xLyra/commit/4064a10f26523fe86f7939175defe06b99eba67d))

## [1.0.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.0.0...v1.0.1) (2026-08-05)


### Bug Fixes

* 修复 OAuth 账号站点的模型价格未随标准价格变更自动更新的问题 ([542e90e](https://github.com/Yachiyo-5i/xLyra/commit/542e90e5540ef68d5df4dc2d07421a5f89ab87a0))
* 修复下游 responses 协议转发上游任意协议时响应内容可能丢失空格的问题 ([5542358](https://github.com/Yachiyo-5i/xLyra/commit/55423582501bdebb9c6af6fc84dc22e712a47cb1))

## 1.0.0 (2026-08-04)


### Features

* initial public release ([d6ee5c5](https://github.com/Yachiyo-5i/xLyra/commit/d6ee5c5e1b4049f9283b8f3bf2393c52d291851a))
