# Changelog

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
