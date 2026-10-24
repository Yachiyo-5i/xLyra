# Changelog

## [1.16.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.15.2...v1.16.0) (2026-10-24)


### Features

* ✨ 404 页：等宽展示字号数字加 Title 2 标题，改用语义文字层级 ([583f721](https://github.com/Yachiyo-5i/xLyra/commit/583f72159b31f30e41a426be5593de122da83257))
* ✨ adapter 注册表支持动态模块替换，新增通用 API Key 适配器 GenericProvider，插件站点类型不再按模型名推断端点 ([86cdac1](https://github.com/Yachiyo-5i/xLyra/commit/86cdac1e15652a9eed8b0492fae76fbcc6af7820))
* ✨ Agent 设置：状态点改用系统色，悬浮状态卡换成 Liquid Glass，次要按钮统一为 secondary ([cadd36c](https://github.com/Yachiyo-5i/xLyra/commit/cadd36c3e1697b90ddda555a6cbdc4d94e81e3fe))
* ✨ API 密钥：网关地址移到标题旁、表格放进卡片并重排列宽，移动端卡片与状态分段条 ([ab4ddc8](https://github.com/Yachiyo-5i/xLyra/commit/ab4ddc8901817141a8475c5382d262361fc3cbeb))
* ✨ Card 布局统一：ItemCard / CardHeading / CardMetrics / CardMetric / CardFooter / CardAction ([1998a85](https://github.com/Yachiyo-5i/xLyra/commit/1998a854135ebab4c94c5af5ada56b03e9c26de2))
* ✨ Claude Code 客户端伪装：对齐真实 CLI 指纹（请求头、billing 块、账号级内存身份、Node TLS 指纹） ([f813ec6](https://github.com/Yachiyo-5i/xLyra/commit/f813ec6f7f7717c5f5bb96848e7bd1de138653a7))
* ✨ Grok 账号同步官方套餐剩余额度，并在账号与站点额度列展示 ([0f5b811](https://github.com/Yachiyo-5i/xLyra/commit/0f5b8114b1ee65f72a6d3e10af9bb7b4089b2d90))
* ✨ Grok 账号额度输出标准 entries，前端用 QuotaEntries 渲染 ([16663ba](https://github.com/Yachiyo-5i/xLyra/commit/16663ba9526c6b4b8320b9af8fc0ffff466d0a9d))
* ✨ JS 插件目录多实例同步：数据库代号加轮询，启用/停用/删除/协议路径变更数秒内同步到所有实例 ([0f9deb8](https://github.com/Yachiyo-5i/xLyra/commit/0f9deb884edfe5240cc47469a6c22dffd6db8481))
* ✨ jsplugin 新增密钥袋纯函数库：secretPaths 响应脱敏、{{secret.x}} 请求占位符替换、字符串/错误擦除（尚未接线） ([b7e4fec](https://github.com/Yachiyo-5i/xLyra/commit/b7e4fec713b720a480bc3e6533bf2a347b58c08b))
* ✨ OAuth 接入前增加风险免责声明弹窗，需勾选确认后才能继续 ([75dc00c](https://github.com/Yachiyo-5i/xLyra/commit/75dc00c2c7d2498b1cf786c19020291d098f1829))
* ✨ OAuth：卡片统一为上方 3×2 指标、下方额度，来源改为横向 Tabs 且只列出已添加的类型 ([19c7bec](https://github.com/Yachiyo-5i/xLyra/commit/19c7bec702389687ce797625ff9b2c3a4773225f))
* ✨ quota.aggregate（sum/min/none）：API Key 插件显式配置时改道站点额度汇总，默认行为不变 ([ae65260](https://github.com/Yachiyo-5i/xLyra/commit/ae65260f54368fe344345c7af284ce8b3142abcf))
* ✨ site_provider 的 classify 钩子参与上游错误分类（手工绑定优先，失败时保持原分类） ([542f821](https://github.com/Yachiyo-5i/xLyra/commit/542f821f949aa3cd40281a7a28a54f9c3c145025))
* ✨ site_provider 的 fixtures 按 hook 分发自检，xlyra-plugin run --hook，脚手架示例与钩子 TS 类型声明 ([8dd7a9c](https://github.com/Yachiyo-5i/xLyra/commit/8dd7a9c9a48acb871ece8452ab3621a6d2345515))
* ✨ site_provider 的 listModels / check 钩子按站点类型自动绑定（模型同步、凭据校验、健康检查），熔断对其只记指标不停用 ([22e5987](https://github.com/Yachiyo-5i/xLyra/commit/22e59871c564739db88cde63d2d63584f5cd5df4))
* ✨ site_provider 的 probe 钩子为插件站点类型自动开启额度探测；修复成功探测被熔断器记为失败的问题 ([471d701](https://github.com/Yachiyo-5i/xLyra/commit/471d701405e355954731b2cbaca8f068ef238778))
* ✨ site_provider 的 rewritePayload 钩子改写上游请求体（model/stream 不可变，失败 502 且不发请求，早于 Claude 伪装） ([31363c5](https://github.com/Yachiyo-5i/xLyra/commit/31363c52e476b8b890ba6ee58ce3955365333148))
* ✨ site_provider 请求头模板（request.headers / stripDownstreamHeaders）：在透传之后、签名之前单点应用，不改协议适配器接口 ([7b0ccda](https://github.com/Yachiyo-5i/xLyra/commit/7b0ccda7215c07a5100ba2b5583cf17965ee63c4))
* ✨ site_provider 账号池 manifest（credential.model=account_pool）校验、站点类型描述与启用闸门（登录/刷新/网关接线待后续） ([d8ac07c](https://github.com/Yachiyo-5i/xLyra/commit/d8ac07cd621924b913af1706a0cf1cfda15a9317))
* ✨ 个人信息设置：改用语义 token 与新字号层级，会话表格圆角同心 ([c658a1f](https://github.com/Yachiyo-5i/xLyra/commit/c658a1f948b6a39132e92b3a0a78e681be5068ae))
* ✨ 仪表盘：指标卡按容器宽度排布、列表与图表卡片按新设计调整，修复资源图表在移动端不显示 ([0603af2](https://github.com/Yachiyo-5i/xLyra/commit/0603af2f26b73e06060e8571470e11d77aa7936e))
* ✨ 仪表盘：站点可用性不再撑高所在行，系统资源总览去掉指标底色铺满卡片 ([75202d7](https://github.com/Yachiyo-5i/xLyra/commit/75202d7e58a23052ca46945ce47133314e84b5fc))
* ✨ 会话取真实客户端 IP(优先 CF-Connecting-IP),个人设置会话表格新增设备列(解析 UA 显示浏览器与系统) ([2a60400](https://github.com/Yachiyo-5i/xLyra/commit/2a60400104e33e2e47b982ba024e396450a699aa))
* ✨ 全局材质改为半透明提亮 + 全局星空背景，统一弹窗与浮层材质 ([f11508e](https://github.com/Yachiyo-5i/xLyra/commit/f11508e49b73d58bb1a783bcd6ff563358bf4ca2))
* ✨ 全局设置：二级导航与通用设置页对齐新设计 ([84ba7f7](https://github.com/Yachiyo-5i/xLyra/commit/84ba7f72892652568ce9be1f272e3b6e2eb53d00))
* ✨ 全局限流设置：旧 token 换成语义类，重置改为 secondary ([cc60d23](https://github.com/Yachiyo-5i/xLyra/commit/cc60d230d408f826997aa638af824b0dddb24bcf))
* ✨ 公共组件：模型抽屉、品牌标记、PWA 更新提示等清理旧 token，提示条改用 Liquid Glass ([7540659](https://github.com/Yachiyo-5i/xLyra/commit/7540659c90c4de560d6f6aa4d2c0022a35def507))
* ✨ 分组配置设置：工具条换成 TableToolbar，列表放进厚材质卡片，状态改用 Badge ([6a9878a](https://github.com/Yachiyo-5i/xLyra/commit/6a9878ae62868f18eb2a3404796a0b76b5aa59c7))
* ✨ 前端：插件账号池类型的抽屉（行 pills/操作/区域/设备码登录对话/图标） ([77d5bad](https://github.com/Yachiyo-5i/xLyra/commit/77d5bada7bbcafb4276080ace4c4388350a2c63b))
* ✨ 前端支持 site_provider 插件：启用时确认主机列表、新错误码提示、插件详情站点类型区块、站点列表的提供方未启用徽标与类型墙插件标签 ([6a83369](https://github.com/Yachiyo-5i/xLyra/commit/6a833696e2f92ce19bed7253e0429f3c4e0c60b9))
* ✨ 前端视觉重构为单一深色主题（Apple HIG + Liquid Glass） ([ae2e405](https://github.com/Yachiyo-5i/xLyra/commit/ae2e405c716e0a2fe1b625b61655d6c97f8ef667))
* ✨ 前端账号池文案与站点判断改为通用表述，去掉重复的登录文案键 ([ff1e7eb](https://github.com/Yachiyo-5i/xLyra/commit/ff1e7ebb740e0ac1d075255981be08c464764bfe))
* ✨ 协议规格增加运行时覆盖表（整份原子替换、复用内置解析校验、受限字段白名单），为插件站点类型提供协议规格做准备 ([2bc798d](https://github.com/Yachiyo-5i/xLyra/commit/2bc798de2202c4500b8287e6213a5013c7acca76))
* ✨ 图标改为深色底，清理 logo 里残留的透明格子 ([347d30a](https://github.com/Yachiyo-5i/xLyra/commit/347d30a279286f5f57aa108adb641cfffcaa41f6))
* ✨ 声明式账号导入解析（containers + fieldMap），尚未接线 ([da7191a](https://github.com/Yachiyo-5i/xLyra/commit/da7191aabb2fe51071281279d595c4009664e548))
* ✨ 审计：移动端卡片改用 mat-regular，旧 token 与次要按钮对齐新设计 ([e3f023d](https://github.com/Yachiyo-5i/xLyra/commit/e3f023dc3626bfebbd40b307783cad37a1d7571f))
* ✨ 密钥门户设置：模块开关行改为 fill 底、去掉描边，重置改为 secondary ([3b888b2](https://github.com/Yachiyo-5i/xLyra/commit/3b888b2b7a24464282ed8529844e147c6d6beb8e))
* ✨ 对齐门户页视觉（卡片、图表、请求记录、手机卡片列表） ([1b67b0c](https://github.com/Yachiyo-5i/xLyra/commit/1b67b0cb94751e69d6074e9f9bf1ab7aaf8271c9))
* ✨ 已启用的 site_provider 插件在目录重载后自动接入站点类型、协议覆盖表与 adapter 动态模块（含多实例同步） ([d3ffff4](https://github.com/Yachiyo-5i/xLyra/commit/d3ffff4f56cbec9e42cf91163f6a142d4613b238))
* ✨ 开发者（插件）：已上传插件表格放进厚材质卡片，旧 token 与次要按钮对齐新设计 ([d2fccbc](https://github.com/Yachiyo-5i/xLyra/commit/d2fccbc8c7ab30100ab71b1158fae6dcfd20d814))
* ✨ 把聊天 SSE 聚合拆到 protocol_openai_chat_sse.go ([3d9a3e5](https://github.com/Yachiyo-5i/xLyra/commit/3d9a3e5033e3bffa73aad46efc57b0c57789aede))
* ✨ 抽出凭据解析与 Grok 账号池，统一账号接口和抽屉 ([184708e](https://github.com/Yachiyo-5i/xLyra/commit/184708e0625c31d37f03a54ce7c949a1d8d872c2))
* ✨ 插件图标服务：GET /api/v1/plugin-icons/{pluginID}（管理端会话、按魔数判类型、ETag、安全头），站点类型 icon_url 指向它 ([951f415](https://github.com/Yachiyo-5i/xLyra/commit/951f41515cbdcc4c18b169b151161b270a598526))
* ✨ 插件站点类型生命周期：停用后不参与路由与下游模型目录、provider_status、卸载保护、站点类型 ID 锁定 ([a7d4b23](https://github.com/Yachiyo-5i/xLyra/commit/a7d4b23b848ffb69dcd92f9184f6e50736a52d33))
* ✨ 插件站点类型的出站主机白名单生效：站点创建/更新、通用适配器、网关转发（含重定向）按精确主机名校验 ([bc096a5](https://github.com/Yachiyo-5i/xLyra/commit/bc096a598fa89ba764dfefbbd2c11370b578e7c7))
* ✨ 插件管理的近 24 小时统计改为彩色圆点加成功/失败次数（绿/黄/红/灰），悬停说明统计口径 ([af8c5c0](https://github.com/Yachiyo-5i/xLyra/commit/af8c5c0025553531c76bea61a3b08e34f79af8c8))
* ✨ 插件详情的站点提供方只显示当前版本一份（有启用版本用启用版本，否则用最新版本），并标注版本状态 ([3b6a7ca](https://github.com/Yachiyo-5i/xLyra/commit/3b6a7cadfe309dc742183e22d3f0a58276a164eb))
* ✨ 插件账号池后台：定时刷新与健康检查覆盖插件账号、额度汇总与计数回写、账号行剩余/上限 ([ec1992e](https://github.com/Yachiyo-5i/xLyra/commit/ec1992ef0d0fb1ba8ca9f192dcd9da000643de33))
* ✨ 支持跨协议转换模型原生 web_search（请求/响应/流式/历史折叠/用量统计） ([6a3f380](https://github.com/Yachiyo-5i/xLyra/commit/6a3f3802ac5e47184fa973053cace4b04b1cf43a))
* ✨ 收尾：加载占位、鉴权错误页、表头小标题与残留的 section-soft / glass-panel 写法对齐新设计 ([fce0887](https://github.com/Yachiyo-5i/xLyra/commit/fce088768460d3e2c0c15a30361b110dfcb101de))
* ✨ 新增 accountauth 包：声明式 OAuth2 设备码与 refresh_token 引擎、JWT 声明读取（出站校验、不跟随重定向、错误不含响应体；尚未接线） ([1d17a43](https://github.com/Yachiyo-5i/xLyra/commit/1d17a43b1277517b51a78f58dc0065d6b84e107c))
* ✨ 新增插件类型 site_provider（API Key 站点类型）：manifest 校验、必须受信签名、启用时确认主机列表、CLI 脚手架 ([5206685](https://github.com/Yachiyo-5i/xLyra/commit/5206685f1d60595d4b14ea14ed35f247704b1e8f))
* ✨ 模型：展开卡片、移动端列表与目录表面清理旧 token，状态色改用系统色 ([7cbd056](https://github.com/Yachiyo-5i/xLyra/commit/7cbd056f4ae7e14e23a056eeb37420ccbf1378e9))
* ✨ 模型价格：表格放进厚材质卡片，旧 token 清理，移动端卡片改用 mat-regular ([808ec8a](https://github.com/Yachiyo-5i/xLyra/commit/808ec8a0aa421fccbb6b1ae52a52c236e2b10e70))
* ✨ 模型体验：输入框右上方加一只会动的小螃蟹（Clawd） ([60f2a62](https://github.com/Yachiyo-5i/xLyra/commit/60f2a62540f4dfa2c5e02424ddae37184d23bf01))
* ✨ 模型体验：输入框浮起质感；移动端沉浸式头部、侧边抽屉、新的模型与推理强度选择器和附件菜单 ([9f3fa20](https://github.com/Yachiyo-5i/xLyra/commit/9f3fa20a300d59f10a466db1ce7484ae605e1f04))
* ✨ 流量驾驶舱视觉重做：宇宙网络数据流（星空背景、天琴座星系中枢、发光节点、渐变航道与彗星） ([ab70160](https://github.com/Yachiyo-5i/xLyra/commit/ab70160af845f4e58b265cab7c92d7c47ec59b31))
* ✨ 渠道拆分弹窗：旧 token 与次要按钮对齐新设计 ([a890cfa](https://github.com/Yachiyo-5i/xLyra/commit/a890cfa71add6b62af3f1767a8eab9a4a0d36a4a))
* ✨ 用量分析：桌面与移动端共用 KPI 卡，趋势维度改为分段控件，筛选与图例调整 ([30a1e9b](https://github.com/Yachiyo-5i/xLyra/commit/30a1e9b3fc2039e70a46d38d41a2cce8552d3f8a))
* ✨ 站点：新增、API Key、模型、测试等抽屉与弹窗清理旧 token，状态色改用系统色 ([dc33ec5](https://github.com/Yachiyo-5i/xLyra/commit/dc33ec5e2b00f80cff73a8eee69bd82d68b4e907))
* ✨ 站点：表格放进厚材质卡片、操作列固定在右侧；新增移动端筛选区 MobileFilterBar ([4f78d1e](https://github.com/Yachiyo-5i/xLyra/commit/4f78d1eca5e542d42e61a639e3213708b2a5402a))
* ✨ 站点类型元信息收敛为注册表，凭据分类按前缀统一判断 ([89d0536](https://github.com/Yachiyo-5i/xLyra/commit/89d0536cfb61692057951359eeaf2682fd8470ce))
* ✨ 站点类型徽标按 logo 主色着色；开发者页操作改为图标，版本菜单补图标与停用项 ([10877f3](https://github.com/Yachiyo-5i/xLyra/commit/10877f38ef133bad3f758a7c770503b527eed5fe))
* ✨ 站点类型注册表支持合并插件站点类型（ID 格式校验、原子快照），站点类型归一化统一为小写 ([913574f](https://github.com/Yachiyo-5i/xLyra/commit/913574f8e75646949737ddad552a5d7cf8afb6a3))
* ✨ 站点级额度展示按 unit（credits/percent）走通用路径 ([e750714](https://github.com/Yachiyo-5i/xLyra/commit/e750714d455d57daf3c4b86425f6298337b44cc6))
* ✨ 系统代理设置：列表放进厚材质卡片，工具条换成 TableToolbar ([88f4f72](https://github.com/Yachiyo-5i/xLyra/commit/88f4f720f78f7ff098883add7c2b2d0daaedd91d))
* ✨ 统一 /accounts/* API 接入插件账号池（pluginAccountAPI、运行时适配、导入、刷新租约），移除启用闸门 ([83f770c](https://github.com/Yachiyo-5i/xLyra/commit/83f770cce1711c2f8d62172ae36bf353f3e59656))
* ✨ 网关侧插件账号池：通用凭据解析、401/传输切换、upstreamStreamOnly 与 SSE 聚合、quota_weighted 选择、额度耗尽冷却 ([740b4ef](https://github.com/Yachiyo-5i/xLyra/commit/740b4ef1a8249d93ff4c28ccb0d692edf07384c8))
* ✨ 覆盖层按端切换：桌面 Sheet / 配置 Dialog 在移动端变为 Draw，确认弹窗保持居中 ([d467679](https://github.com/Yachiyo-5i/xLyra/commit/d4676790f71b087a8791bb320a50614bf8ac747e))
* ✨ 覆盖层统一：Dialog 居中、Draw 底部、Sheet 右侧，页脚与关闭方式由组件定义 ([0ffacd8](https://github.com/Yachiyo-5i/xLyra/commit/0ffacd8b3935bff586120d287427c2a5a09c7829))
* ✨ 请求：筛选区合并为两行、时间范围改为筛选胶囊，表格放进卡片；移动端卡片与汇总行重做 ([f6f311d](https://github.com/Yachiyo-5i/xLyra/commit/f6f311d757a7d639f8ab791711ea43f5b8c0d0a0))
* ✨ 账号导入文案键加入 ui.texts 白名单，并写明账号池没有入库示例 ([d659662](https://github.com/Yachiyo-5i/xLyra/commit/d659662d334fc01e72bdb50311be396ce29e2823))
* ✨ 账号摘要写入状态与失败时保留额度的辅助函数改为通用命名，健康探测使用解析后的站点配置 ([12544e0](https://github.com/Yachiyo-5i/xLyra/commit/12544e024f7fe0a1c7204239d0edae36d1d88689))
* ✨ 账号池凭据类型 account:&lt;siteType&gt;:&lt;uuid&gt; 的解析与通用判断（内置 Grok 前缀语义不变） ([f12082a](https://github.com/Yachiyo-5i/xLyra/commit/f12082aa267f7a1f040b126a62148441e055d8be))
* ✨ 账号池插件按账号模型同步（models.perAccount + listModels，令牌经占位符注入不进 JS），并按 realm/domain 解析区域、修正 custom_poll 轮询重放 ([3801a83](https://github.com/Yachiyo-5i/xLyra/commit/3801a836f340ea38c7a9088d4989ca8ac141d023))
* ✨ 账号池插件表达力补齐：区域属性与条件头、身份回退/display/expiresFrom、导入 valueMap/msThreshold、摘要 reauth、轮询非 JSON 容错 ([90b978a](https://github.com/Yachiyo-5i/xLyra/commit/90b978a9e8b6a7fb6b4bdf2dc5a9b2793b2a1b74))
* ✨ 账号池插件运行时：设备码/自定义登录、令牌刷新、账号摘要钩子（密钥经 secretPaths 摘除，不进 JS；尚未接网关与 HTTP API） ([2bfbc98](https://github.com/Yachiyo-5i/xLyra/commit/2bfbc987c362e79c0e035fc1c852cef49dabbbcf))
* ✨ 账号池插件链路接通：网关刷新在租约内写回密文、leadSeconds 提前刷新、accounts.ui 暴露给前端、移除启用闸门，含端到端泄漏扫描测试 ([0358086](https://github.com/Yachiyo-5i/xLyra/commit/03580863362569e46f521cea92b0fc9265455d9e))
* ✨ 路由、模型、审计、模型价格的移动端筛选改用 MobileFilterBar ([b59a3f9](https://github.com/Yachiyo-5i/xLyra/commit/b59a3f9b9129d35243b4aab67bc635b65e0e9602))
* ✨ 路由：展开卡片、移动端列表与详情清理旧 token，去掉小节标题的宽字距 ([a33a4ab](https://github.com/Yachiyo-5i/xLyra/commit/a33a4abe529c67d1aa65e7baf43fb6452bce5ae0))
* ✨ 近黑色调与组件顶光，天琴座登录注册页 ([c4215db](https://github.com/Yachiyo-5i/xLyra/commit/c4215db9a79c9c0b17d916b3386fc20383810598))
* ✨ 配置备份：手动 / 自动切换改为强调色分段控件，页面级危险操作改为 destructive-soft，提示块去描边 ([9b787cc](https://github.com/Yachiyo-5i/xLyra/commit/9b787cc359a5a3a3ddc3081746098a9258392c12))
* ✨ 额度探测 entries 增加可选的 kind（balance/window/package）与 name 字段 ([be17289](https://github.com/Yachiyo-5i/xLyra/commit/be1728992f95e20903e2085df1e65857f33cd948))


### Bug Fixes

* 🐛 DeepSeek v4 的 max_tokens 兜底只保留给 Messages 协议并调大到 32768，chat/responses 不再强补 ([ffa4356](https://github.com/Yachiyo-5i/xLyra/commit/ffa4356e555103c4794c471ec5153bc80e70caf2))
* 🐛 Grok 新周期用量为 0 时额度被当作获取失败，按 0% 已用处理 ([7a19f2c](https://github.com/Yachiyo-5i/xLyra/commit/7a19f2cf2cc07af73a9de6e17d7549745871074e))
* 🐛 Responses 下游对接 Anthropic 上游：thinking 签名经 encrypted_content 往返，缺签名的 thinking 块与工具调用轮降级处理；工具 input_schema 顶层 oneOf/anyOf/allOf 合并规整 ([f433a61](https://github.com/Yachiyo-5i/xLyra/commit/f433a6180facf9ed7b095d11c66bb493c960d079))
* 🐛 Responses 转 chat 时合并并行工具调用为同一条 assistant 消息，修复部分上游对并行工具调用拆分的校验 ([eb843dd](https://github.com/Yachiyo-5i/xLyra/commit/eb843dd2cd69fd24d8df5820d253a7445504dcf7))
* 🐛 xlyra-plugin 帮助文本中的 --kind 列表改为从支持的 kind 表派生 ([3e07932](https://github.com/Yachiyo-5i/xLyra/commit/3e0793257c1871adfb895477da33a0d1a0218d76))
* 🐛 xlyra-plugin 打包时带上 manifest 声明的站点类型图标，缺图标的包上传不再校验失败 ([584a3da](https://github.com/Yachiyo-5i/xLyra/commit/584a3da430eeaafb420b846df23dcd99172e1af3))
* 🐛 修正 OpenCode 请求头、默认工具和模型端点映射 ([d2b6be4](https://github.com/Yachiyo-5i/xLyra/commit/d2b6be4d9345cea211ce3f10cb7d124c3fad1481))
* 🐛 内置模块与站点类型计数改为当前内置集合，并补上插件账号维护测试与注释 ([7635066](https://github.com/Yachiyo-5i/xLyra/commit/7635066496174a16e543005741d304a8ecafab65))
* 🐛 升级 Go 至 1.26.10，Dockerfile 固定 golang:1.26.10-alpine，README 同步版本要求 ([29c554c](https://github.com/Yachiyo-5i/xLyra/commit/29c554c2aa9148938eed2db5d31390fa54de6cff))
* 🐛 升级 zod 修复前端 npm audit 高危漏洞 ([3473ca3](https://github.com/Yachiyo-5i/xLyra/commit/3473ca3ad85a1778e0340afafb4d647ab16207b3))
* 🐛 升级 zod 至 4.6.7，修复前端生产依赖 npm audit 的高危漏洞 ([243ea93](https://github.com/Yachiyo-5i/xLyra/commit/243ea9384d6679497505aeab9ba1c84807a413c3))
* 🐛 复审修复：失败刷新保留 usage、轻量并发健康检查、perAccount 校验与账号文案 ([a866355](https://github.com/Yachiyo-5i/xLyra/commit/a866355d1c8c93a272080482890c2d1c0a5d475c))
* 🐛 外壳内容区改为内容 + 页脚两行网格，整页高度页面重新撑满，页脚固定在底部 ([5cce3bb](https://github.com/Yachiyo-5i/xLyra/commit/5cce3bbe53cd1891d14e7bce4508a160f441dcdd))
* 🐛 导入 valueMap 匹配忽略大小写，与内置 realm 归一一致 ([d1016ae](https://github.com/Yachiyo-5i/xLyra/commit/d1016ae4c127ce0355c8256b5c2618029b4d2639))
* 🐛 开发者页：站点查询与其他页面共用缓存 key 导致读到对象，打开时报 sites is not iterable ([343a083](https://github.com/Yachiyo-5i/xLyra/commit/343a0838aa47343f4bdd6442e6bf4888d32a921d))
* 🐛 悬停浮层同一时间只显示一个，修复额度列等位置同时出现两个浮窗 ([0546350](https://github.com/Yachiyo-5i/xLyra/commit/0546350c076d205a4e56ca5739b5a84f938f736e))
* 🐛 手机端抽屉：关闭按钮移到左上角，账号与 API Key 抽屉操作改为标题行图标，新增 API Key 全部刷新 ([b13126c](https://github.com/Yachiyo-5i/xLyra/commit/b13126c1bb8c643f8e1d3e52fb54ad97bf9ddfbb))
* 🐛 手机端抽屉二级页面的返回并入左上角按钮，不再与关闭按钮同时出现 ([87f59fb](https://github.com/Yachiyo-5i/xLyra/commit/87f59fbcf386c505e046550e1019170828faea66))
* 🐛 插件停用后站点图标仍然显示（回退到插件图标接口），提供方不可用时图标灰化；账号弹窗桌面端导入按钮去掉图标，保持桌面纯文字、移动端纯图标 ([2b9f74c](https://github.com/Yachiyo-5i/xLyra/commit/2b9f74c52d67ce8d7d4969f94f01f4238badc135))
* 🐛 插件列表与受信公钥列表接口在请求被客户端取消时不再记 ERROR 日志，也不再返回 500 ([4cd60aa](https://github.com/Yachiyo-5i/xLyra/commit/4cd60aaac8ffb95235fb060804a5caf19e181e16))
* 🐛 插件提供方不可用时，站点刷新与健康检查改为跳过并说明原因，不再自动停用站点或累计失败冷却 ([56e70eb](https://github.com/Yachiyo-5i/xLyra/commit/56e70eb977dece104abe58a8b0e1ebdd8b31b759))
* 🐛 插件站点类型的显示名与副标题按界面语言展示，非中文界面不再隐藏插件副标题 ([d822a27](https://github.com/Yachiyo-5i/xLyra/commit/d822a27e9aee437526591111c013236460f879f1))
* 🐛 插件账号刷新失败记账语义：取消不再掩盖上游明确的重新授权、换票后失败不清除原有重新授权、落库失败保留上游错误；写库加有限超时并核对租约；提供方不可用的健康检查显式标记跳过；模型行缺失保持原行为；登录前预检区域容量；账号接口提示提供方未启用 ([9686467](https://github.com/Yachiyo-5i/xLyra/commit/96864678dce9045075851755f393d0d20b0e97a4))
* 🐛 插件账号刷新换到的新令牌在同一租约内原子落库（取消、探针交错、类型被改均不丢失或覆盖）；模型开关失败不再静默成功；待定登录区域加上限 ([b86db01](https://github.com/Yachiyo-5i/xLyra/commit/b86db0188b5e16dfd65a25c6c550fd5869b929dc))
* 🐛 插件账号池站点零账号时保持正常状态；登录、导入、刷新时把可读账号名写入凭据显示名与掩码，请求日志与测试不再显示内部 uuid ([8dcd39e](https://github.com/Yachiyo-5i/xLyra/commit/8dcd39e031cb488367370f7bf64f725ab66df154))
* 🐛 清理不再使用的旧样式 ([3365c03](https://github.com/Yachiyo-5i/xLyra/commit/3365c0349d4538ff3573337a2f40fa8ef5ed6cae))
* 🐛 登录注册页：收起时不能再上滑，展开表单时字标只略微上移、星座留在上方 ([c981939](https://github.com/Yachiyo-5i/xLyra/commit/c981939f806280df36853fb677fbc9b90d977dd6))
* 🐛 目录同步启动重载失败后继续重试；登录会话加上限并在回收时擦除令牌；账号池出站请求接入拨号前内网与元数据地址拦截 ([d9b367e](https://github.com/Yachiyo-5i/xLyra/commit/d9b367e8ff67ac5f327665e2f9eaf64be52bfea6))
* 🐛 移动端 playground 左侧抽屉改为贴边全高的侧边栏，修复 PWA 顶部留空 ([c100096](https://github.com/Yachiyo-5i/xLyra/commit/c10009642a0b8a271b491c3f63db390cf968f6fd))
* 🐛 移动端底部胶囊在宽屏手机上居中，并去掉胶囊下方的模糊与渐暗 ([a00c9ac](https://github.com/Yachiyo-5i/xLyra/commit/a00c9ac5a777835045fce38e351719f355f5fba1))
* 🐛 移动端底部胶囊收起后回到屏幕左侧，而不是停在居中列的左端 ([f4dc83f](https://github.com/Yachiyo-5i/xLyra/commit/f4dc83fa025c0534d1a785f22a6a9273da234cfd))
* 🐛 自动化事件的到期与重试时间只用数据库时钟，不再混用应用时钟，消除时钟偏差导致的事件延迟与测试偶发失败 ([4cc27fd](https://github.com/Yachiyo-5i/xLyra/commit/4cc27fdb7d387fdc730c63d72399cc5b37c1d704))
* 🐛 补回 OAuth 导入的 JSON 拖放、嵌套对话框和刷新图标 ([da67ae6](https://github.com/Yachiyo-5i/xLyra/commit/da67ae6b9fff63f9be3cb3cb7272c3d34364d241))
* 🐛 调度器、同步 worker 与路由器共用同一个站点服务实例，插件站点类型在后台刷新与健康检查中不再报 unsupported site_type ([a613d01](https://github.com/Yachiyo-5i/xLyra/commit/a613d0153293b8e97f9ade1eec1417f9f6b6c0bf))
* 🐛 调用方取消后仍写入超时健康快照和插件账号刷新结果 ([45263f7](https://github.com/Yachiyo-5i/xLyra/commit/45263f7888604209d8227dd2845c3fd8b129ad82))
* 🐛 账号抽屉的文案前缀和区域字段收窄到 Grok 账号类型 ([6e2b0d5](https://github.com/Yachiyo-5i/xLyra/commit/6e2b0d5ed48af38e36002757f28da514cc03b7a1))
* 🐛 账号池 token 刷新改用数据库租约表，修复多实例并发刷新竞争；site_credentials 的 meta/secret/类型更新改为按列写入避免丢失更新 ([09ec04a](https://github.com/Yachiyo-5i/xLyra/commit/09ec04aedfbbbd432c1da31c03715a7cf76304de))
* 🐛 账号池出站守卫保留环境代理并放行代理自身地址；错误文本清洗覆盖单引号 URL 与 userinfo；补强图标回退与守卫的真实请求测试 ([da92c7f](https://github.com/Yachiyo-5i/xLyra/commit/da92c7fb8402e927870e62747ff3dd4c04915136))
* 🐛 账号池插件站点刷新后补上标准模型价格回退与无定价警告（与通用刷新路径共用同一段逻辑） ([b0561a1](https://github.com/Yachiyo-5i/xLyra/commit/b0561a181cf665a796553782285934e3b9321629))
* 🐛 配置备份：导入按钮不再用红色，自动备份的三个开关固定为一行两个 ([bc47b98](https://github.com/Yachiyo-5i/xLyra/commit/bc47b985ead470fa109fb0f53b85cae003581cc9))

## [1.15.2](https://github.com/Yachiyo-5i/xLyra/compare/v1.15.1...v1.15.2) (2026-10-15)


### Bug Fixes

* 🐛 修复生成的插件类型里泛型被逗号截断的问题 ([c084bba](https://github.com/Yachiyo-5i/xLyra/commit/c084bba532c2d657217a2e6ca61be095f35071d8))

## [1.15.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.15.0...v1.15.1) (2026-10-15)


### Bug Fixes

* 🐛 automation 插件支持站点对象与通用 HTTP 请求，表单控件由 manifest 声明 ([e087657](https://github.com/Yachiyo-5i/xLyra/commit/e087657697a42c16d436144fb65c526dc589d7f0))

## [1.15.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.14.2...v1.15.0) (2026-10-15)


### Features

* 🎸 JS 插件运行时：沙箱、包格式、签名、类型注册表、catalog、automation 运行时与内置插件包 ([0c3c4cc](https://github.com/Yachiyo-5i/xLyra/commit/0c3c4cc4a7f2eee1e47115ac3945c919200b94a4))
* 🎸 xlyra-plugin 命令行工具：init、build、test、run、pack、verify 与各类型脚手架 ([f91ac3a](https://github.com/Yachiyo-5i/xLyra/commit/f91ac3abb3344530f17c75e5ee18ed4e09265e12))
* 🎸 发布插件 SDK 包 @yachiyo-5i/xlyra-plugin-sdk ([978922b](https://github.com/Yachiyo-5i/xLyra/commit/978922badb7af0fc5d0512fc0e17adbd20acb38f))
* 🎸 开发者设置页：插件列表、版本管理、试跑与入口 ([4456a8e](https://github.com/Yachiyo-5i/xLyra/commit/4456a8e348da58220d61280ebe4b1ead523e26b5))
* 🎸 开发者页 automation 配置与权限授予 ([78e498c](https://github.com/Yachiyo-5i/xLyra/commit/78e498ce56f6328839bb82b07308e743ae796fe6))
* 🎸 开发者页中英日文案 ([7a0ddfe](https://github.com/Yachiyo-5i/xLyra/commit/7a0ddfe53a4817ac8243499775226b3bb2b7dd7c))
* 🎸 开发者页数据层与基础组件：插件接口、本地化文本、权限、多选与弹窗视口适配 ([a53b902](https://github.com/Yachiyo-5i/xLyra/commit/a53b902774cb4ed1b718145b91ac582b9540cd2c))
* 🎸 开发者页移动端卡片与版本操作菜单 ([642a3c7](https://github.com/Yachiyo-5i/xLyra/commit/642a3c743a14435205fd1c4de6d57b1ebcc366c0))
* 🎸 开发者页站点多选绑定 ([c1efa97](https://github.com/Yachiyo-5i/xLyra/commit/c1efa9738ab7b8266241f037b0b8c429a4bedb34))
* 🎸 插件管理 API、受信公钥 API 与 automation 事件接入（含额度同步事件） ([6f4b141](https://github.com/Yachiyo-5i/xLyra/commit/6f4b1416458af17b6bc51213a2071af49dab5047))
* 🎸 新增 JS 插件存储与挂载模型 ([5868987](https://github.com/Yachiyo-5i/xLyra/commit/58689875a6b113361558888d54d6a08d56208b98))
* 🎸 新增插件自动化存储 ([a7b633f](https://github.com/Yachiyo-5i/xLyra/commit/a7b633fbfb0fda52f397948dc8adcfed0139e933))
* 🎸 站点与路由接入 JS 插件：额度探测内置化、站点类型插件绑定、错误分类 ([4709653](https://github.com/Yachiyo-5i/xLyra/commit/4709653107f3ed639bed20922f03d16db8243c26))
* 🎸 网关接入 JS 协议插件：端点注册表、协议适配器、签名与错误改写、SystemOne 内置化 ([180c128](https://github.com/Yachiyo-5i/xLyra/commit/180c128e6fbe83bbe33bdccfacb966938118a08e))


### Bug Fixes

* 🐛 .gitignore 的 dev* 不再忽略开发者设置页，并放行 docs/plugins ([874db6d](https://github.com/Yachiyo-5i/xLyra/commit/874db6d1ca642af0d036291784c7ceee5963b267))
* 🐛 Codex 刷新连接时合并而非覆盖已有 metadata，保留 quota 等同步字段 ([8126df8](https://github.com/Yachiyo-5i/xLyra/commit/8126df88c1058175e9667c9522018ec14a97b5fd))
* 🐛 server-run 不再依赖被忽略的 dev-server.sh ([e514dbe](https://github.com/Yachiyo-5i/xLyra/commit/e514dbe1040e5a7d6beec4e3333723c78beb39d5))
* 🐛 全新数据库首次启动时建表改用简单协议 ([7eba484](https://github.com/Yachiyo-5i/xLyra/commit/7eba484f8803071609aa97193cb8d5d5492738e8))
* 🐛 升级 Go 至 1.26.9 及 x/net、x/text，修复 govulncheck 报告的漏洞 ([6663578](https://github.com/Yachiyo-5i/xLyra/commit/666357866fec17ed71995456ebc6ab531871380b))

## [1.14.2](https://github.com/Yachiyo-5i/xLyra/compare/v1.14.1...v1.14.2) (2026-10-05)


### Bug Fixes

* 🐛 按模型应用国模请求体与参数处理 ([a8a6fce](https://github.com/Yachiyo-5i/xLyra/commit/a8a6fce2f9e20f2a529762536f5b5a1391a1e341))

## [1.14.1](https://github.com/Yachiyo-5i/xLyra/compare/v1.14.0...v1.14.1) (2026-09-30)


### Bug Fixes

* 🐛 修复 TypeSafe 模型协议与品牌图标无法识别的问题 ([35c6d53](https://github.com/Yachiyo-5i/xLyra/commit/35c6d539d392946ed09bd49f41a4720ed9eeb2a9))

## [1.14.0](https://github.com/Yachiyo-5i/xLyra/compare/v1.13.2...v1.14.0) (2026-09-30)


### Features

* 🎸 接入 TypeSafe Jev 决策模型并完善品牌展示 ([f00769e](https://github.com/Yachiyo-5i/xLyra/commit/f00769eaefe2526446f44dd2c18c5667eace3711))


### Bug Fixes

* 🐛 让模型缓存价格严格跟随上游目录同步 ([34dd364](https://github.com/Yachiyo-5i/xLyra/commit/34dd36498748773672f113eb948099baefd5f710))

## [1.13.2](https://github.com/Yachiyo-5i/xLyra/compare/v1.13.1...v1.13.2) (2026-09-29)


### Bug Fixes

* 🐛 Playground 按模型站点选择 Gemini 协议 ([52b466f](https://github.com/Yachiyo-5i/xLyra/commit/52b466fb3513f3600cac737b912d5470f091bf73))
* 🐛 修复 Antigravity 额度持久化与面板展示 ([e04bdef](https://github.com/Yachiyo-5i/xLyra/commit/e04bdef1da6c38d486e29220b04088b6527f618b))
* 🐛 修复 Gemini 历史系统消息发送失败 ([3c3e597](https://github.com/Yachiyo-5i/xLyra/commit/3c3e59783300fb46da252597fe8fa20e91a8d770))
* 🐛 修正 Gemini/Antigravity thinkingBudget 字段名 ([870fa9a](https://github.com/Yachiyo-5i/xLyra/commit/870fa9aabbc603ed9f700a0a191f9882ec1ba94f))
* 🐛 将 Antigravity -tiered 归入标准 canonical ([654b98a](https://github.com/Yachiyo-5i/xLyra/commit/654b98a45193463039781a7755b8c930102ad273))

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
