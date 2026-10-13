# 开发路线图与续接清单

## 当前实际状态

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
- [ ] pi 真实模型调用、自动主协调编排、完整任务验收与更多故障验证。
- [ ] 提交构建与版本 Tag/Release 流水线设计、初始化和实际运行验证。

Go 服务基础阶段 32 项顶层测试及历史信号烟测通过，详见 [基础验证](development/go-foundation-verification.md)。纯 Go RPC 包已实现并自测，当前 84 项顶层测试通过；独立首轮发现的 2 项 Important、1 项 Minor 已修正并复验通过，见 [RPC 验证](development/go-pi-rpc-verification.md) 与 [审查](development/go-pi-rpc-review.md)。原生客户端和 integrations/pi 仍是说明入口；Web／HTTP 已接独立 pi RPC 及卡片 worktree 执行，隔离假进程验证通过，真实模型与自动编排未验收；用户已要求运行局域网预览服务（指定私网 IP＋令牌）；2026-10-13 按用户本次明确授权建立本地代码基线提交，尚未推送。初始化授权本身不包含后续提交／推送、Tag、Release 或修改 Leaf。

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
