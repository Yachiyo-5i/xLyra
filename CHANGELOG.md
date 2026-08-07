# Changelog

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
