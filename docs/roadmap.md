# 开发路线图与续接清单

## 当前实际状态

- [x] 任务失败具体原因保留与聊天局部布局修复：后端及UI独立切片验收通过，覆盖错误可读／脱敏限长／新轮清理、短气泡、状态分离、桌面及360／390px；本批进入代码交付，运行版本以实际入口修订号为准；曾出现2项既有版本浏览器失败，最终候选仍须通过部署门禁。用户后续批准持久降低MiniMax默认及原任务预算至8192，原会话8次真实响应与13工具无429、生成Go示例；用户授权用户级Go安装／Git脚本修复后，4测试／构建／真实回环HTTP及工具环境独立验收通过，原任务待用户确认；无原预算同期对照，不宣称限流根因已证明。[本批台账](../TODO.md#当前缺陷任务失败原因与聊天局部布局)。

- [x] 运行版本与本地自动部署通用能力：运行程序嵌入提交号、构建时间，顶栏展示版本及最近部署结果；本地 Git 主分支更新后，从干净归档测试、构建、串行部署并核对实际入口。148项脚本、真实停写检查、干净归档完整测试链及桌面／360px独立验收通过。目标地址、令牌和启用状态仅保存在被忽略的本机配置，不启用产品内尚未验收的自动交付 worker。[通用说明](development/local-deployment.md)。

- [x] 全站UI风格统一：自定义下拉与灰绿交互配色，桌面／360／390px展开视觉和键盘／表单独立验收通过；D1桌面手机控件泄漏、D2焦点蓝环、D3空列表Esc误关弹窗均关闭。构建及受影响回归通过，用户已授权本批提交并推送GitHub，未部署，7331保持原版本，不混入暂停的子Agent后端。[实施](development/web-ui-consistency.md)／[验收](development/web-ui-consistency-acceptance.md)。

- [x] 2026-10-18 收口：7331 已恢复最后验证程序 `ddc811f`，备份／原数据身份保留及独立实际入口烟测通过；自动交付仍禁用。
- [x] 前两批交互＋成果历史收口：独立候选排除第三批7共享修改＋11新增；前端29＋18、构建、同步修正独立复验及重建产物8＋1浏览器通过。Go全量旧崩溃注入窗口失败留档，独立裁决不阻塞禁用worker的限定发布，不称全套通过。22:43已更新7331，逐资源一致、schema2及原消息／会话／19条登录保留，最终独立实际入口烟测通过；功能提交 `1c9fcc7` 已推送 GitHub `main`；无 Actions workflow／运行记录，不称 CI 通过。不直接构建当前混合主目录上线。
- [ ] 第三批子Agent概览：公共后端自测，UI及真实启动桥未完成；全部安全停写，成果原地保留，本轮不纳入提交／部署。待本轮交付后另协调续接，不自动恢复。

以下旧条目按各批次保留；当前部署以 README 收口总览及实际服务证据为准。

- [x] 任务聊天内标准用户交互：六类字段／组合卡片、独立草稿历史、幂等答复和原会话续接、原生协议、可恢复等待及预算修复完成并获独立切片验收；18项交互单元／29项基线／8项真实Go＋固定假pi浏览器及桌面360px键盘视觉通过。[验收](development/web-agent-interaction-acceptance.md)。尚未真实模型／扩展联调、未部署当前网页。
- [x] 当前共享快照受影响浏览器回归收尾：已证实长测试共享服务累计8个驻留导致首败，保持产品上限／全部断言不变，按独立服务生命周期1＋42共43项通过；成果历史1440／390／360视觉补验通过。不是同服务连续43通过，也不等于真实模型／扩展／现用网页部署完成。[分组证据](development/web-agent-interaction-regression.md)／[协调记录](development/web-agent-interaction-verification.md)。

- [x] 自动交付设置界面按用户反馈精简为三项交付设置和分步表单，真实草稿保存、旧参数与身份保留、固定底栏及禁用启用通过独立复验；代码 `ddc811f` 已推送 main 并更新7331，真实页面／登录与数据保留烟测通过。真实自动交付安全门禁仍未通过，不因此开放。[改版验证](development/web-delivery-settings-redesign.md)。

- [x] 2026-10-18 用户要求部署：Web `f7c0232` 已运行于原指定 `192.168.2.122:7331`；备份／迁移与原数据／登录保留、实际桌面／手机烟测通过。仅界面及已验证修正部署，自动交付 worker 未装配、真实自动上传未启用，安全阻塞仍需修正。[部署与回滚注意](development/web-automated-delivery-deployment.md)。

- [x] 名称：开悟 / kaiwu。
- [x] 本地 main Git 仓库、origin HTTPS 和 GitHub 公开仓库。
- [x] Paseo 项目及两个固定 local 沟通入口。
- [x] 确认 Go、Nim + Leaf、Android Kotlin 技术方向。
- [x] 确认个人工作台、单执行节点、联网范围、配置快照、模型变更与 worktree 原则。
- [x] 当前优先 pi；确认 pi 主协调＋pi 执行的首条开发闭环。
- [x] 用户确认新增 Web 版，作为主要开发入口，优先完整流程；PC/Android 首版目标保留。
- [x] 功能专题看板＋右侧双范围聊天原型已实现：专题增删、其他收纳、任务创建与五态角标、独立草稿／历史／阅读位置及迟到消息隔离。
- [x] 本批 15 项状态测试、44 项 Chromium 检查通过，覆盖 1440/1024/768/360px；独立验收无 Critical／Important，1 项焦点 Minor 保留。[当前验证](development/web-kanban-prototype-verification.md)。旧截图与 20 项结果仅作为历史保存。
- [x] 确认项目对话与每张任务卡片分别绑定独立 pi 会话；消息及模型上下文隔离，真实 RPC 生命周期已实现，真实模型联调尚未执行。
- [x] 执行 [专题看板原型计划](superpowers/plans/2026-10-11-web-kanban-prototype.md) 的本地实现与浏览器验证；该原型是历史阶段；后续正式工程及 RPC 生命周期已实现。
- [x] 将背景/需求、架构方向、研究和待办保存到本项目目录。
- [x] 小范围代码初始化方案审核、实施计划与执行。
- [x] 可构建/测试/本机启动的 Go 起步骨架（本机及独立验收通过；3 项边界已修正并通过限定独立核对）。
- [ ] PC 与 Android 的实际工程及必要初始化组件。
- [x] Web 正式工程、Go 认证／SQLite／事件／独立只读对话接入，浏览器首批验证通过；执行任务闭环另列。
- [x] 按用户反馈调整添加项目：可创建服务器目录并初始化 Git，远程地址可选，只设置 origin，不覆盖已有远程，不自动提交／克隆／推送。
- [x] 添加项目改为自动初始化 Git（无需勾选且兼容旧表单）；Web 登录持久保留 30 天，刷新／服务重启后恢复，退出／到期／访问令牌变化后失效。
- [x] 卡片／任务聊天“开始开发”统一入口、原 pi 上下文切换到任务独立 worktree、Linux 进程组回收、幂等与显式恢复、实际工具记录／结果待确认；假 pi＋临时 Git 验证通过。
- [x] 卡片主分支集成：自动识别默认主分支、可选覆盖、实际 Git 状态、合并图标／聊天入口、显式提交并普通合并、幂等及重启核查；不自动解决冲突／推送／清理 worktree。[验证与限制](development/web-task-merge-verification.md)。
- [x] 按用户反馈收紧 Web 顶栏、字号、按钮、专题列与卡片，桌面／手机浏览器回归通过。
- [x] 聊天 Markdown、表格／清单、代码高亮与复制、输入草稿预览和工具 JSON 展示；沿用 WebSocket，Chromium 15/15 及实际局域网 HTTP 验证通过，见 [验证记录](development/web-chat-markdown-verification.md)。
- [x] pi 程序 PATH 自动检测、真实可用模型查询、Web 默认／空会话模型选择和重启保留；7331 已更新，真实 43 项模型元数据及 Chromium 20/20 验证通过，未发送模型请求。[验证](development/web-pi-model-selection-verification.md)。
- [x] 结构化方案／成果／验证／记录、历史轮次、用户确认结果；双向 WebSocket 业务及增量补发／分块快照、身份失效恢复已实现，验证见 [本批记录](development/web-progress-socket-verification.md)。
- [x] 修正任务中断／确认状态、旧数据兼容、恢复失败结果保留及手机详情关键内容／恢复入口，见 [验证](development/web-task-state-clarity-verification.md)。
- [x] 中断后在新消息／继续开发时自动接回原会话和工作区，去掉手动恢复前置步骤，保留防重放和身份核验；[验证](development/web-auto-reconnect-verification.md)。
- [x] 项目／任务回复统一使用当前系统语言（简体中文），覆盖过程更新与报告，7331 已更新；协议回归通过，真实模型输出尚未验证。[验证](development/web-reply-language-verification.md)。
- [x] 用户评审设计后已要求开始实现：任务成果／受控 HTTP 效果预览、确认完成后的持久反馈与后续入口，含原 pi／执行锁交接、配置指纹与能力网关；[实现与验收记录](development/web-task-result-preview-verification.md)区分本机验证、独立审查和 7331 更新状态。
- [x] 顶栏项目切换改为轻量菜单，集中 Git 设置／刷新与添加项目，全局操作靠右；支持键盘、保留任务／草稿及窄屏长名称。7331 已更新，前端 16/16、Chromium 32/32、Go 全量与实际页面只读烟测通过；[验证记录](development/web-project-menu-verification.md)。
- [x] 聊天首次打开、刷新与切回时定位最新消息，取代旧阅读位置恢复；手动上翻暂停跟随，回到底部恢复。7331 更新、前端 16/16、Chromium 32/32、Go 全量及实际桌面／手机只读烟测通过；[验证](development/web-chat-latest-scroll-verification.md)。
- [ ] 固定自动开发交付闭环：项目配置与授权快照、原任务 worktree／pi、自动检查／提交／推送、串行集成、精确 SHA 的 GitHub Actions 与有限自动修复。用户已审核[书面规格](superpowers/specs/2026-10-17-automated-delivery-design.md)，[实施计划](superpowers/plans/2026-10-17-automated-delivery.md)及串行实现方式亦已审核；Tasks 1–12 后端已实现并通过隔离测试，官方接口取证阻塞已解除；2026-10-18 已提交后端基线 `1c76c0a` 并继续 Task 13 界面／浏览器闭环，Task 14 整体独立验收待完成，见[执行记录](development/web-automated-delivery-ledger.md)；不等于真实远程写入或模型测试授权。
- [ ] pi 真实模型调用、自动主协调编排、完整任务验收与更多故障验证。
- [ ] 提交构建与版本 Tag/Release 流水线设计、初始化和实际运行验证。

Go 服务基础阶段 32 项顶层测试及历史信号烟测通过，详见 [基础验证](development/go-foundation-verification.md)。纯 Go RPC 包已实现并自测，当前 84 项顶层测试通过；独立首轮发现的 2 项 Important、1 项 Minor 已修正并复验通过，见 [RPC 验证](development/go-pi-rpc-verification.md) 与 [审查](development/go-pi-rpc-review.md)。原生客户端和 integrations/pi 仍是说明入口；Web／HTTP 已接独立 pi RPC 及卡片 worktree 执行，隔离假进程验证通过，真实模型与自动编排未验收；用户已要求运行局域网预览服务（指定私网 IP＋令牌）；2026-10-13 按用户明确授权建立本地代码基线提交；2026-10-17 用户另行授权提交当前已完成代码并推送 GitHub 的 main 分支。本次授权不自动包含未来批次提交／推送、Tag、Release 或修改 Leaf。

## 推荐推进次序（具体实施待审核）

### 0. 可继续开发的工程入口

首批工程入口已按 [Go 规格](superpowers/specs/2026-10-11-go-foundation-design.md) 和 [实施计划](superpowers/plans/2026-10-11-go-foundation.md) 实现；本机结果见 [初始化范围](initialization-scope.md)。独立验收已完成；初次边界发现见 [验收记录](development/go-foundation-review.md)，后续修正见 [回归记录](development/go-foundation-polish.md)。Go pi RPC 已按获批 [规格](superpowers/specs/2026-10-11-go-pi-rpc-design.md) 和 [计划](superpowers/plans/2026-10-11-go-pi-rpc.md) 实现，通过隔离 Go 假进程自测，独立验收已通过（含首轮问题修正复验）；[验证记录](development/go-pi-rpc-verification.md) 不代表真实 pi 或双端结果。

### 1. pi 接入基础

- 自有后端全部 Go，直接管理第三方 pi RPC，不新增 TypeScript 扩展或 SDK 宿主。
- 纯 Go 基础包已实现进程、JSONL、命令关联、事件及故障处理；pi 1.0.4 RPC schema 是有限基线，尚未运行真实 pi 模型联调；HTTP 已接项目／任务只读对话入口。
- 持久会话、状态/历史、流式文本与工具事件。
- 精确结束边界、模型变更、人工确认、取消与退出。
- 使用隔离假进程做协议/故障测试，再经许可进行真实 Agent 调用。

### 2. 主协调＋执行 Agent 闭环

- 配置方案及快照。
- 编排工具、派发/查询/结果回收；优先研究 Go MCP 服务，不引入自有 TypeScript 扩展，权限与工具路由须独立验收。
- 隔离 worktree、写入锁及实际构建/测试回报。
- 卡片产品入口的提交／合并审核、操作回执已实现；通用 Agent 工具审批、推送与破坏性操作授权仍待设计。
- 阻塞、失败、取消、重复请求和执行进程退出的验收。

### 3. Web 优先与多端可用、恢复

- Web 优先：项目看板＋右侧双范围聊天；新建／选择卡片、范围独立草稿／历史、任务执行与验收按需查看，不再以聊天详情为主页面。
- [Web 设计](superpowers/specs/2026-10-11-web-workbench-design.md) 分为 W0 交互原型（已实现）、W1 协议/界面工程、W2 真实闭环、W3 可靠性、W4 远程发布；真实业务依赖前面 pi/主执行闭环。
- PC：Nim Leaf，补证网络/UI 接入及 Windows、macOS、Linux 原生体验。
- Android：Kotlin 原生 + KMP + Compose、SQLite、更新逻辑。
- 对话、进度、审批、模型切换、结果摘要。
- 认证/配对/TLS、断线重连、状态恢复与事件补发。

Web 主要入口及体验优先级已确认；首批 Vue＋SQLite＋令牌＋会话服务已获批并实现，执行工作区已接通，下一条是经授权的真实模型联调／可信编排桥，不是取消 PC/Android 或把它们排除在首版。认证边界在业务写操作暴露前落实，远程发布另行验收。

### 4. 发布与首版验收

- 三平台 PC、Android 和已确认服务平台的构建产物。
- Web 静态分发、协议兼容、缓存/更新和真实浏览器矩阵验收。
- 自动构建、Tag/Release、版本一致性、签名/完整性校验及更新。
- 独立报告静态检查、本机测试、跨平台原生验证和真实 Actions 结果。
- 不借验收擅自推送或发布。

## 下一阶段提醒

用户要求阶段验收/路线图回顾时提醒：

1. 团队协作平台。
2. 通用可配置编排引擎。

Codex 及其他 Agent 按后续需要接入。多执行节点、源码/diff、编辑器/IDE与交互终端另行评估，不默认补进当前范围。

## 开发记录约定

每次里程碑完成后更新 README 的实际状态和本清单，并记录：改动、实际测试命令/结果、未验证项、用户批准的变更和下一步。不将本清单中的计划勾选为已完成，也不依赖旧会话自动记忆。
