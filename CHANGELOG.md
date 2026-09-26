# Changelog

## [1.13.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.13.0...v1.13.1) (2026-09-26)


### Bug Fixes

* 🐛 修复转 Responses 上游时多轮思考回放因缺少 summary 被拒的问题 ([fc72496](https://github.com/Yachiyo-5i/xLyra/commit/fc72496d6388be2a3ee52d8a8bb5008f0fa56858))

## [1.13.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.12.0...v1.13.0) (2026-09-25)

### Features

* 🎸 为 Codex OAuth 增加额度预估 ([d42f1ca](https://github.com/Yachiyo-5i/xLyra/commit/d42f1caec9624fd611fd2b0e0c04791d08308438))

### Bug Fixes

* 🐛 修复下游 completions 转 Gemini 时因用户标识等字段被拒的问题 ([02f3fb5](https://github.com/Yachiyo-5i/xLyra/commit/02f3fb57efc3e82c49e223e3a3ac9f253c57c557))
* 🐛 修复缓存观测数据的备份与升级问题 ([29196fc](https://github.com/Yachiyo-5i/xLyra/commit/29196fc2b8eca960bf583cac04f46f35a269eec4), [026d666](https://github.com/Yachiyo-5i/xLyra/commit/026d666c36fd5d9455c3a51ae5cadbaabb1fce58))
* 🐛 恢复总览页加载时的骨架屏 ([ec422ea](https://github.com/Yachiyo-5i/xLyra/commit/ec422eafa5a70892611cde732105957ec7bb4cdb))
* 🐛 修复超过 32 MiB 的网关请求被拒绝的问题，支持最大 256 MiB 请求体 ([c436f5e](https://github.com/Yachiyo-5i/xLyra/commit/c436f5eefac2790846f3d81a113e884706173c2c))

## [1.12.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.11.0...v1.12.0) (2026-09-23)


### Features

* 🎸 增强模型协议兼容与模型发现能力 ([ae510b5](https://github.com/Yachiyo-5i/xLyra/commit/ae510b58346b37e564726247baf2fefcccbd9b87))


### Bug Fixes

* 🐛 保留 Codex 回调提交的代理 ([551cbe1](https://github.com/Yachiyo-5i/xLyra/commit/551cbe1b925b6ab32008559f2de25515794377e7))
* 🐛 修复前端版本更新后页面未及时切换的问题 ([759a996](https://github.com/Yachiyo-5i/xLyra/commit/759a996465c1ca63d1af910767d9631e379a8ad1))
* 🐛 修复站点筛选配置与下拉显示问题 ([9e3fba9](https://github.com/Yachiyo-5i/xLyra/commit/9e3fba933718865df5e52dd49dd7d03e0d8255bc))
* 🐛 修正冷却 Key 的站点名称显示 ([97c9f9a](https://github.com/Yachiyo-5i/xLyra/commit/97c9f9a7b2c5af1709f3cd5266cbacbd8fe6f745))
* 🐛 修正模型测试的协议和密钥筛选 ([b2f33b1](https://github.com/Yachiyo-5i/xLyra/commit/b2f33b13d2c4c3bc71f07ca13c86aabf5126732f))
* 🐛 加速管理员初始化并改善页面加载缓存 ([7143f14](https://github.com/Yachiyo-5i/xLyra/commit/7143f14c8ad5f081e0104501be22f0901782d89a))
* 🐛 修复 Codex OAuth 令牌交换未遵循所选代理的问题 ([89d254a](https://github.com/Yachiyo-5i/xLyra/commit/89d254a072e11fdf089410f5a351aea8954f7439))
* 🐛 在 Codex 令牌交换前应用提交的代理配置 ([0c58d68](https://github.com/Yachiyo-5i/xLyra/commit/0c58d6842ae0bad4db7eb9e006d3d8a14c2585e5))
* 🐛 修复 Codex OAuth 令牌交换未经过站点代理的问题 ([6597abe](https://github.com/Yachiyo-5i/xLyra/commit/6597abea58d8181354cb38f9fdb59ef7ea756492))

## [1.11.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.10.0...v1.11.0) (2026-09-22)


### Features

* track upstream response models in request logs ([30dba61](https://github.com/Yachiyo-5i/xLyra/commit/30dba61bbacb32045d7626abf53a31c90b2a306a))


### Bug Fixes

* 🐛 修复站点同步丢失模型协议能力的问题 ([428e258](https://github.com/Yachiyo-5i/xLyra/commit/428e2581e20b5df1c47cbe52b39c1fc5be40f1a7))

## [1.10.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.9.1...v1.10.0) (2026-09-20)


### Features

* ✨ 为窗口 Tokens 增加多维明细，并展开节点卡片查看在途数据 ([12f92f1](https://github.com/Yachiyo-5i/xLyra/commit/12f92f11e28a7313d943b24bcea1adad807f17b2))
* ✨ 将流量驾驶舱改为 2.5D 拓扑并按厂商着色 ([d61bab3](https://github.com/Yachiyo-5i/xLyra/commit/d61bab3903bb3aa08eb741171dddd3e27fbafeaf))
* ✨ 按单条请求高亮拓扑，并以穿越网关的粒子表现请求往返 ([9cf0256](https://github.com/Yachiyo-5i/xLyra/commit/9cf0256b4110d582dc1f7e6e57c561a7a890eaa0))
* ✨ 统一驾驶舱圆角与间距，并精简窗口 Tokens 弹窗层次 ([5232aee](https://github.com/Yachiyo-5i/xLyra/commit/5232aeed501a417c593a2d3ac5c9b9fe5eeb3193))
* ✨ 让拓扑节点 hover 时浮起靠近网关，并避免座位跳动 ([e12139a](https://github.com/Yachiyo-5i/xLyra/commit/e12139a1a5f0ca19448dc77117208f4c9b9c15ba))


### Bug Fixes

* 🐛 修复流量驾驶舱合入后无法通过代码检查的问题 ([31ca646](https://github.com/Yachiyo-5i/xLyra/commit/31ca64666bbb56a3ec07d475f81a9361e0a38191))
* 🐛 修复路由页通道列表漏行，并按开启状态与优先级排序 ([c2a83df](https://github.com/Yachiyo-5i/xLyra/commit/c2a83dff480f2183fd1be17edd4247e4438edc11))
* 🐛 加快前端发版检测并避免旧 Service Worker 挡住更新 ([0af9b69](https://github.com/Yachiyo-5i/xLyra/commit/0af9b69371b8c34d12dba8ac24865a83c45314c3))

## [1.9.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.9.0...v1.9.1) (2026-09-20)


### Bug Fixes

* 🐛 修复 API 密钥排序保存失败并简化排序操作 ([a372dd2](https://github.com/Yachiyo-5i/xLyra/commit/a372dd2fb219037360023a766d7bbd28d4a4e886))
* 🐛 修复 DeepSeek 对话请求的历史消息标识兼容问题 ([beab4ca](https://github.com/Yachiyo-5i/xLyra/commit/beab4ca19325cc121855d2caad291efb61069771))
* 🐛 修正 GLM 国内站 Responses 请求端点 ([3459a1e](https://github.com/Yachiyo-5i/xLyra/commit/3459a1e6ce81d41dbd65f941e74f3a9205e18e07))
* 🐛 更新 Codex 图像生成默认宿主模型 ([2f1fb8e](https://github.com/Yachiyo-5i/xLyra/commit/2f1fb8ec542a48372fe0ba09dae3bbf2c3a209fe))
* 🐛 统一文本协议转换与模型可用协议展示 ([34a27ff](https://github.com/Yachiyo-5i/xLyra/commit/34a27ff4010d34297688884b7ce12464fd50a2b0))

## [1.9.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.8.1...v1.9.0) (2026-09-19)


### Features

* 🎸 新增 DeepSeek 账户余额探测 ([8018ad6](https://github.com/Yachiyo-5i/xLyra/commit/8018ad6247244a7577fda241ee0d42dc3b143386), [2ff22ed](https://github.com/Yachiyo-5i/xLyra/commit/2ff22ed93a5e160959c066312f28cdcfb9c5c22a))
* 🎸 新增 Moonshot 账户余额探测 ([33abd21](https://github.com/Yachiyo-5i/xLyra/commit/33abd210b4c4878124b7132ed0feda1aa876ab3c), [25e8a26](https://github.com/Yachiyo-5i/xLyra/commit/25e8a26146cd94f8cf3b3b824695ff18f2a47afb))
* 🎸 智谱模型列表改为动态拉取上游 /models ([140222a](https://github.com/Yachiyo-5i/xLyra/commit/140222a1972862bd586a672446899d80c6cf03b6), [25346c9](https://github.com/Yachiyo-5i/xLyra/commit/25346c92d66eaeaf8dc537cb7b77863004151c6b))


### Bug Fixes

* 🐛 修复 APIKey 权限变更后可用模型未及时更新的问题 ([dc976f6](https://github.com/Yachiyo-5i/xLyra/commit/dc976f643d9b9e47f5c2c7fb6479035eb71eff6f))
* 🐛 修复 Claude Code 和 Codex 自动同步版本后后续请求未使用新版本的问题 ([638224b](https://github.com/Yachiyo-5i/xLyra/commit/638224b69be8005539b84d0f2d552ae179e9a07b))
* 🐛 修复 Codex 账号图像模型协议缺失及权限校验异常导致无法生图的问题 ([2d6e3c8](https://github.com/Yachiyo-5i/xLyra/commit/2d6e3c89d09d527b8ef5341210f40648bd1b0231))
* 🐛 修复 DeepSeek 零余额和负余额更新异常并完善多币种余额展示 ([99d2298](https://github.com/Yachiyo-5i/xLyra/commit/99d2298c2166dff5370b183d2a5e20424974e973))
* 🐛 修复 GLM Coding Plan 积分制套餐额度探测失败 ([9730619](https://github.com/Yachiyo-5i/xLyra/commit/973061972c1c638af01702c581144247d9b03cb8), [7398c5c](https://github.com/Yachiyo-5i/xLyra/commit/7398c5c66382957651ffac408d35a4fa34d5c5cd))
* 🐛 修复 Kimi 兼容适配误改聊天输出上限和自定义站点地址的问题 ([5de8add](https://github.com/Yachiyo-5i/xLyra/commit/5de8add92e8cf172ee518090458cc8bd414e1b21))
* 🐛 修复 Kimi 开放平台 Anthropic Messages 端点缺少 /anthropic 前缀 ([56e63c9](https://github.com/Yachiyo-5i/xLyra/commit/56e63c93760ef6ae21baca8d7f3de0e901f55b76), [8f3c5b7](https://github.com/Yachiyo-5i/xLyra/commit/8f3c5b7b82b1b98a9aad33f6e14dfdd26889e5a9))
* 🐛 修复 Moonshot 零余额更新和欠费余额展示的问题 ([1fe6d26](https://github.com/Yachiyo-5i/xLyra/commit/1fe6d267a4595fb52784d547af3ec7d2a7e28824))
* 🐛 修复同名站点创建失败并统一错误与额度详情提示 ([b3d75aa](https://github.com/Yachiyo-5i/xLyra/commit/b3d75aac0a8477afd120ebc3b9e8d111d792c411))
* 🐛 修复密钥配置返回流程并支持 Gemini 组倍率 ([8fb0def](https://github.com/Yachiyo-5i/xLyra/commit/8fb0def0ed02292ab7c4b6d869ffe8a27012f5b6))
* 🐛 修复思考预算与输出上限冲突导致请求失败的问题 ([f66f357](https://github.com/Yachiyo-5i/xLyra/commit/f66f3575eb17b669044e835347f1f989caeded92))
* 🐛 修复智谱工具额度耗尽误停聊天并补全额度明细展示 ([0bbe10e](https://github.com/Yachiyo-5i/xLyra/commit/0bbe10eef113dacbb8ec050b022bd87ad8c2fa1c))
* 🐛 修复智谱模型列表与上游返回结果不一致的问题 ([eb3f8a2](https://github.com/Yachiyo-5i/xLyra/commit/eb3f8a2cd0efe3108c9d5174deab7c6ae745baff))
* 🐛 修复模型测试使用禁用协议并统一错误详情样式 ([d8057e6](https://github.com/Yachiyo-5i/xLyra/commit/d8057e61fd7a62cdd0636381ab5259b75bfe1957))
* 🐛 修复站点和 OAuth 页面恢复焦点后未自动更新数据的问题 ([7f4929a](https://github.com/Yachiyo-5i/xLyra/commit/7f4929a82b638891d6234fa908fc8040a1abf673))
* 🐛 支持 APIKey 手动排序并同步各设备和相关页面的显示顺序 ([b5dd58a](https://github.com/Yachiyo-5i/xLyra/commit/b5dd58ae6f761cc3a3be0fa1243c890d42fc0852))
* 🐛 统一各页面表格的列宽调整与表头吸顶表现 ([5f9f20e](https://github.com/Yachiyo-5i/xLyra/commit/5f9f20e426bc5465192eaab9b88be9a2b20394e7))
* 🐛 解决余额展示改动的合并冲突 ([acbaed5](https://github.com/Yachiyo-5i/xLyra/commit/acbaed52da06ca1b07ff11910df5f5993d7d8863))

## [1.8.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.8.0...v1.8.1) (2026-09-16)


### Bug Fixes

* 🐛 修复 Codex 图片模型下游请求无可用路由的问题 ([f418d1c](https://github.com/Yachiyo-5i/xLyra/commit/f418d1c34800f6a079e5bf4b00b4d61f187be540))
* 🐛 修复 DeepSeek 工具参数正则导致请求失败 ([bdb2d30](https://github.com/Yachiyo-5i/xLyra/commit/bdb2d30c43c5d89fa304416793d8f2d287b02e41))
* 🐛 修复 DeepSeek 工具参数正则导致请求失败的问题 ([581bcd0](https://github.com/Yachiyo-5i/xLyra/commit/581bcd08b6669b4b73d93d906bf36c723acc4038))
* 🐛 修复智谱和 DeepSeek Responses 端点地址 ([d746aa8](https://github.com/Yachiyo-5i/xLyra/commit/d746aa873d67b4c3fb552103c6a56d3f70918b50))

## [1.8.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.7.2...v1.8.0) (2026-09-14)


### Features

* 🎸 切换模型目录为自维护数据同步 ([9898c22](https://github.com/Yachiyo-5i/xLyra/commit/9898c22aa865609b4ca625f9d731dc1819cd45bc))
* 🎸 完成模型目录协议价格与路由配置接入 ([b1b183f](https://github.com/Yachiyo-5i/xLyra/commit/b1b183f3d0b99a449db063d10a29b80db9d120cb))
* 🎸 完成自维护模型目录同步闭环 ([bc6a813](https://github.com/Yachiyo-5i/xLyra/commit/bc6a813547a8aa801bc249cc45a8e0ba3e1b6f0d))
* 🎸 对齐模型目录价格与站点协议路由 ([cc345a4](https://github.com/Yachiyo-5i/xLyra/commit/cc345a4df3a59142adc4c10b32aef99537b474f3))
* 🎸 接入目录价格与协议能力计费 ([3a1f6e0](https://github.com/Yachiyo-5i/xLyra/commit/3a1f6e0781f446af0364e618a924a56e22ffd4a2))
* 🎸 支持按 API Key 配置模型协议能力 ([fa1fd81](https://github.com/Yachiyo-5i/xLyra/commit/fa1fd81e7aa4c06691731e2da629ad8fc6609dc4))
* 🎸 支持按API Key配置模型协议能力 ([76439fe](https://github.com/Yachiyo-5i/xLyra/commit/76439fe14eb60c8d1b56341e7597360a635da6d1))
* 🎸 移除外部模型信息来源 ([c14962d](https://github.com/Yachiyo-5i/xLyra/commit/c14962d2215965a0779a1015aaa7f2e0e5232ce5))


### Bug Fixes

* 🐛 优化上游链路复用与故障切换等待 ([cbe911f](https://github.com/Yachiyo-5i/xLyra/commit/cbe911fbdf63af6a828f34aa9dbbaf4c6d9debf1))
* 🐛 修复上游余额耗尽后的凭据冷却与恢复 ([7e823f6](https://github.com/Yachiyo-5i/xLyra/commit/7e823f6ec1301589607d5429c556cd528e4885c5))
* 🐛 修复标准模型键不一致导致价格未同步 ([a7369f8](https://github.com/Yachiyo-5i/xLyra/commit/a7369f8af937c2e7a91cef8e0aedc4d0df007e6f))
* 🐛 修正模型目录远程地址 ([bbe3a0c](https://github.com/Yachiyo-5i/xLyra/commit/bbe3a0cdc1d69a1063e4ba21f0aa850549f97abc))

## [1.7.2](https://github.com/Yachiyo-5i/xLyra/compare/v1.7.1...v1.7.2) (2026-09-11)


### Bug Fixes

* 🐛 修复 DeepSeek thinking 与工具调用在同一消息中被拆分导致 400 错误 ([543fd82](https://github.com/Yachiyo-5i/xLyra/commit/543fd82994cb3119d342277b1e2c79759928af59))
* 修复 DeepSeek thinking 与工具调用被拆分到两条消息导致 400 错误 ([1467211](https://github.com/Yachiyo-5i/xLyra/commit/1467211c8f26d050f84889ec360018978e711c71))

## [1.7.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.7.0...v1.7.1) (2026-09-11)


### Bug Fixes

* 🐛 修复 DeepSeek 思考内容在跨协议请求中的回传 ([3a0667e](https://github.com/Yachiyo-5i/xLyra/commit/3a0667ee4b2be7e70ad0e2c4bc6936a49fe18ffa))
* 🐛 修复 DeepSeek 思考内容跨协议回传 ([ea48907](https://github.com/Yachiyo-5i/xLyra/commit/ea48907c4846abc9643e85e4934f4f2ab452ed84))
* 🐛 修复模型体验会话 ID 必须为 UUID ([c099bfe](https://github.com/Yachiyo-5i/xLyra/commit/c099bfe049ceb57e95a485f229b1365c2f866280))
* 🐛 修复模型体验会话 ID 必须为 UUID ([2e82b8a](https://github.com/Yachiyo-5i/xLyra/commit/2e82b8ab549b96152886a8fb390d2a9c88d4951e))

## [1.7.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.6.0...v1.7.0) (2026-09-08)


### Features

* 🎸 GLM Code 站点支持 Coding Plan 额度探测 ([d7e87bd](https://github.com/Yachiyo-5i/xLyra/commit/d7e87bdb51c9dd73e023387320d2a75d1dfaffe6))
* 🎸 GLM Code 站点支持 Coding Plan 额度探测 ([f452cbc](https://github.com/Yachiyo-5i/xLyra/commit/f452cbc45cc1e9cf298fe5ba0b46817ee7d66d7a))
* 🎸 Kimi 与 GLM 订阅额度耗尽时自动暂停调用直至额度重置 ([260ddd4](https://github.com/Yachiyo-5i/xLyra/commit/260ddd4de89ee46c15fd95cbbed462cdb4a018cc))
* 🎸 Kimi 与 GLM 订阅额度耗尽时自动暂停调用直至额度重置 ([af9ee82](https://github.com/Yachiyo-5i/xLyra/commit/af9ee8233458550f3736d8f92300c0c6db1706a5))
* **server,web:** ✨ 推理强度档位规范、映射与前端选择器联动 ([55bf307](https://github.com/Yachiyo-5i/xLyra/commit/55bf3071b8d0cc70d6f098dcbda86769a83c9323))
* **server,web:** ✨ 推理强度档位规范、映射与前端选择器联动 ([dd7f524](https://github.com/Yachiyo-5i/xLyra/commit/dd7f52469534d3071cb406bf3db126ae1df81344))


### Bug Fixes

* 🐛 修复额度探测在中转站点误开启及订阅窗口信息异常的问题 ([7775b0d](https://github.com/Yachiyo-5i/xLyra/commit/7775b0db4cc8c8e477b5ca738906e171a94ed63f))

## [1.6.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.5.2...v1.6.0) (2026-09-06)


### Features

* 🎸 支持为 API 密钥设置计费倍率 ([cc82051](https://github.com/Yachiyo-5i/xLyra/commit/cc820512c18bc1b67cb206b5f17c785a0f1b3884))
* 🎸 统一数据库迁移执行与版本追踪 ([3f69d42](https://github.com/Yachiyo-5i/xLyra/commit/3f69d42a4b4a4dcf4132c1beeb217dd8660c5e7c))
* 支持 API 密钥计费倍率 ([86beca8](https://github.com/Yachiyo-5i/xLyra/commit/86beca87a4e6e6473d43a2a6523d39462c4b2521))


### Bug Fixes

* 🐛 将 OAuth 刷新并发修复合入主分支 ([b528957](https://github.com/Yachiyo-5i/xLyra/commit/b528957f07790893f6938d190bdcbe47146385e3))
* 🐛 避免并发 OAuth 刷新失败后重复请求上游 ([854adf7](https://github.com/Yachiyo-5i/xLyra/commit/854adf79160dcbade47fd8395ab444d1fff8b599))

## [1.5.2](https://github.com/Yachiyo-5i/xLyra/compare/v1.5.1...v1.5.2) (2026-09-05)


### Bug Fixes

* 🐛 补上 codex OAuth 刷新路径的 gpt-image-2 路由模型 ([20cc6be](https://github.com/Yachiyo-5i/xLyra/commit/20cc6bec92a7fb402eac48f46a5d0a5777d0424d))
* 🐛 补上 codex OAuth 刷新路径的 gpt-image-2 路由模型 ([be130d9](https://github.com/Yachiyo-5i/xLyra/commit/be130d9830be9645ed09af5cf8653989ecf4a18a))

## [1.5.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.5.0...v1.5.1) (2026-09-05)


### Bug Fixes

* 🐛 gpt-image-2 路由条目补 id 字段，避免被站点层解析丢弃 ([823db4c](https://github.com/Yachiyo-5i/xLyra/commit/823db4cbdf12336c667edf9472543e779ad155f2))
* gpt-image-2 路由条目补 id 字段，避免被站点层解析丢弃 ([a294099](https://github.com/Yachiyo-5i/xLyra/commit/a294099f377b4830a3545d43c4fd29f56108ba4c))

## [1.5.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.4.0...v1.5.0) (2026-09-04)


### Features

* 🎸 Codex 客户端版本动态化，模型列表不再用静态兜底 ([85a4894](https://github.com/Yachiyo-5i/xLyra/commit/85a4894bde184d0c21b7139b7c08ace48a3c3bed))
* Codex 客户端版本动态化，模型列表不再用静态兜底 ([c715ea0](https://github.com/Yachiyo-5i/xLyra/commit/c715ea051df181b3542aa2601648d7213d550c09))


### Bug Fixes

* 🐛 Codex 模型价格改为跟随价格目录自动更新，官方调价可自动生效 ([bb73757](https://github.com/Yachiyo-5i/xLyra/commit/bb7375765b5dfe2f2654c1a506197509d63c5cb3))
* 🐛 Codex 模型价格改为跟随价格目录自动更新，官方调价可自动生效 ([729e4db](https://github.com/Yachiyo-5i/xLyra/commit/729e4dbcee6f8e844547552e19a0518ac36c492a))

## [1.4.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.3.1...v1.4.0) (2026-08-27)


### Features

* 🎸 API 密钥轮换与分钟级到期 ([e61b215](https://github.com/Yachiyo-5i/xLyra/commit/e61b21526ae78e8c8f44779cb14c10629e551bef))
* 🎸 支持轮换 API 密钥，一键更换密钥并保留站点模型用量等全部配置 ([078bdc5](https://github.com/Yachiyo-5i/xLyra/commit/078bdc5c4dce849ed4a403f5a00224af7724fd72))


### Bug Fixes

* 🐛 密钥到期时间支持精确到分钟，到期后自动置为停用状态 ([02f3244](https://github.com/Yachiyo-5i/xLyra/commit/02f324498592502ca0b843a747cd1673e79a372f))

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
