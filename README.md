# 开悟 · kaiwu

从想法到作品，也是在创造中更新自己对世界的认知。

开悟是一个本地优先、面向开源社区的 AI 开发工作台。先做好个人开发体验：用户提出需求，主 Agent 拆解、派发并跟踪，执行 Agent 完成实际开发与验证，结果回到原对话。

## 当前状态

- **固定自动开发交付流程（2026-10-17，规格／计划已审核，Tasks 1–12 已实现并通过隔离测试，闭环未完成）**：项目一次配置，任务独立 worktree 开发，本地检查、自动提交／推送任务分支、CI、串行集成及主分支 CI；代码问题有限自动修复，效果验收按项目配置。见[设计](docs/superpowers/specs/2026-10-17-automated-delivery-design.md)与[计划](docs/superpowers/plans/2026-10-17-automated-delivery.md)。配置校验、SQLite 原子周期、Actions 只读客户端、凭据桥、远程 Git 收据、写权租约／候选冻结／本地检查与单 worker 状态机、原会话自动修复与下一轮模型切换、失败分类与修复闭环、持久 FIFO 集成队列与候选重验、主分支快进集成与主 CI 隔离、每步骤崩溃核查（intent／回执、唯一可证补记、真实 Linux SIGKILL 重启恢复）、Web 交付 Socket（固定方法、认证／Origin／会话、操作回放、preflight 只读、set_next_profile）与生产装配（auth／remote／actions／delivery 注入、启动先恢复、关闭顺序、凭据不入库）均已通过 Go 全量／race／vet 审查；界面与真实浏览器闭环（Tasks 13–14）未实施，真实模型／GitHub／Actions 未联调。见[执行记录](docs/development/web-automated-delivery-ledger.md)，7331 未更新，不授权本开发会话执行远程写入。

- 已创建本地 Git 仓库、GitHub 公开仓库与 Paseo 项目入口；2026-10-13 按用户授权建立首个本地代码提交；2026-10-17 用户明确授权提交当前已完成代码并推送 GitHub 的 `main` 分支。
- **Go 服务基础已实现，通过本机与独立验收；3 项边界修正也已通过独立核对**：回环配置、HTTP 健康检查、结构化日志、优雅停止和故障测试。
- Go 基础阶段的 32 项顶层 Go 测试、race、vet、格式、依赖校验、构建及 SIGINT/SIGTERM 烟测通过。详见 [验证记录](docs/development/go-foundation-verification.md)、[初次独立验收](docs/development/go-foundation-review.md) 与 [边界修正记录](docs/development/go-foundation-polish.md)。本机使用非稳定 Go 工具链，不代表稳定工具链或其他系统验收。
- 电脑端、安卓端和产品协议目录目前仍为说明入口；Web 正式工程、数据库、认证与 WebSocket 已实现；卡片启动独立工作区开发已接通，真实模型联调与主协调编排仍待验收。
- **纯 Go RPC 基础包已实现并通过本机及独立验收**：`server/internal/pirpc/` 提供显式启动、握手、JSONL、请求关联、取消分类、有界事件/stderr 和直接子进程回收。RPC 基础批次 84 项顶层测试通过（助手入口另有 1 项预期 Skip），race/vet/格式/依赖校验/构建及关键故障重复测试通过；首轮 2 Important/1 Minor 已修正并复验通过，详见 [RPC 验证](docs/development/go-pi-rpc-verification.md) 与 [审查记录](docs/development/go-pi-rpc-review.md)。[规格](docs/superpowers/specs/2026-10-11-go-pi-rpc-design.md) 与 [计划](docs/superpowers/plans/2026-10-11-go-pi-rpc.md) 均已批准；RPC 基础阶段仅健康检查；当前 Web 服务已接独立 pi 会话管理。
- 聊天交互已按反馈调整：Enter 发送、Shift + Enter 换行；连续过程更新合并并显示摘要，展开查看原文，Markdown 排版与完整网址链接保留。每轮消息附中文及格式要求，已有英文历史保留原文。[验证记录](docs/development/web-task-result-preview-verification.md)。
- 当前优先 **把 pi 接入做精**；Codex 后续接入。用户已要求启动预览服务，当前后台监听 `192.168.2.122:7331`，令牌认证有效；pi 路径已自动检测，模型可在 Web 自选；详见 [运行说明](apps/web/README.md)。
- **Web 作为主要开发入口，优先打磨完整流程**；PC 三平台与 Android 保留首版目标。[看板＋右侧聊天原型](docs/design/web-prototype.html) 已实现：专题增删、删除后任务归入“其他”、任务创建与五种角标、项目／任务独立历史和草稿。直接用浏览器打开，无需启动服务。
- 当前原型 15 项纯状态测试、44 项 Chromium 交互检查通过，独立验收无 Critical／Important（保留 1 项焦点 Minor），覆盖 1440/1024/768/360px；[桌面预览](docs/design/web-desktop.png)、[手机预览](docs/design/web-mobile.png) 与 [本批验证记录](docs/development/web-kanban-prototype-verification.md)。此前聊天居中布局的 20 项结果仅见 [历史记录](docs/development/web-design-verification.md)。
- **正式 Web 服务首批已实现**：Vue 3＋TypeScript＋Vite，Go 同源托管，SQLite 保存项目／专题／任务／消息，本机令牌登录，WebSocket 更新与断线回执确认。项目／每张卡片分别按首次发送启动独立 pi RPC 会话，切卡片不停；服务重启后在新消息或继续开发时自动接回原会话，不重发旧指令。
- 添加项目只需服务器目录，保存时自动创建目录／初始化 Git，无需勾选；远程地址可选，填写时仅配置 `origin`。登录状态持久保留 30 天，刷新和服务重启后恢复，退出会撤销。已有文件和远程配置保留，不自动提交／克隆／推送。[初始化验证](docs/development/web-project-initialization-verification.md) · [登录与自动初始化修正](docs/development/web-login-and-auto-init-verification.md)。
- 正式页面 7 项 Chromium 场景、3 项前端状态检查通过；该首批 Go 94 项顶层测试通过，race／vet／构建通过。验证使用实际 Go 服务与隔离假 pi，不调用模型；此前首批仅开放只读对话；现已新增卡片／聊天启动任务、独立 worktree 与结果待确认，通用编排工具桥仍未实现。[启动说明](apps/web/README.md) · [桌面预览](docs/design/web-service/desktop.png) · [手机预览](docs/design/web-service/mobile.png) · [本批验证](docs/development/web-service-verification.md)。

- **卡片开始开发已实现（2026-10-12）**：点击卡片／任务对话中的播放图标（悬停提示“开始开发”），或在该任务聊天单独发送“开始开发”，同一 pi 会话保留上下文并切入独立 Git worktree；切卡片不启动／停止进程。空仓库不自动提交，重复启动受保护，恢复保留原工作区；轮次结束显示“需要确认”，不代替用户验收。
- Web 界面按最新反馈收紧：48px 顶栏、较小品牌与控件、统一轻量专题列和卡片。当前执行模式限 Linux；真实模型调用尚未测试，运行中的预览现已支持 Web 模型选择（见后续记录）。[本批验证](docs/development/web-task-start-verification.md)。
- **聊天格式与草稿预览已实现（2026-10-12）**：消息和结果摘要渲染 Markdown、表格、任务清单；代码块支持常用语言高亮与复制，工具记录折叠展示 JSON。输入框旁的眼睛图标可切换预览，保留原文与独立草稿；沿用 WebSocket 更新和重连。当前 Chromium 回归 15/15、前端状态 5/5、Go 回归通过，实际 7331 局域网 HTTP 预览／复制已验证，未调用模型。[本批验证](docs/development/web-chat-markdown-verification.md)。
- **卡片主分支集成已实现（2026-10-12）**：默认自动识别并记住主分支，新卡片 worktree 从该分支创建；卡片／聊天显示实际 Git 状态，合并图标或任务聊天“合并到主分支”打开审核。确认后可提交任务修改并普通合并，保留分支／worktree／原 pi 会话；冲突、过期审核和遗留写入者阻止集成，断线不终止、重启不重放。高级设置可选覆盖主分支及提交身份。当前 Go 151 项顶层测试、race／vet、前端 6/6、Chromium 18/18 通过；7331 已更新并验证旧登录／数据保留，真实模型仍未调用。[设计](docs/superpowers/specs/2026-10-12-task-merge-design.md) · [计划](docs/superpowers/plans/2026-10-12-task-merge.md) · [验证与限制](docs/development/web-task-merge-verification.md)。

- **pi 自动检测与 Web 模型选择已实现（2026-10-13）**：服务自动从 PATH 查找 pi，真实 RPC 查询当前可用模型；顶栏可保存默认模型，聊天可为未启动的空会话补选，默认配置重启后保留，已有快照不追溯。7331 已更新并验证旧登录／看板数据保留；真实 pi 查询到 43 个可用模型，Chromium 20/20、前端 6/6、Go race／vet／构建通过，未发送真实模型请求。[操作与验证](docs/development/web-pi-model-selection-verification.md)。

- **结构化进展与双向 WebSocket 已实现（2026-10-14）**：聊天顶部提供方案／成果／验证／记录，支持历史轮次、简短回复与展开原文；AI 成果声明与本轮实际工具依据分开，最新结果由用户明确确认，Git 合并状态独立。正式页面业务改用 `/api/socket` 请求／回执和增量同步，断线不重发副作用；事件缺口回退分块权威快照。使用假 pi 验证，不增加额外总结调用；7331 已按用户确认重启并验证旧登录／数据保留，中断会话在新消息／继续开发时自动接回；详见 [本批验证](docs/development/web-progress-socket-verification.md)。

- **任务状态与详情可读性已修正（2026-10-14）**：列表／详情统一区分中断与结果确认；恢复不代表完成，已有结果可离线确认，恢复失败也保留结果。旧任务显示原始要求、最近回复和实际工具／Git 信息，结果确认在首屏，中断时直接发送或继续开发，手机默认收起详细报告以保留聊天空间。7331 已更新，旧任务实机检查通过，Chromium 23/23。[本批验证](docs/development/web-task-state-clarity-verification.md)。

- **会话自动接回已实现（2026-10-15）**：中断后直接发消息或点继续开发，系统检查并接回原 pi／工作区，保留执行身份和记录；去掉手动恢复前置步骤，不自动重放中断指令。7331 已更新，Chromium 24/24 和实际页面只读烟测通过。[验证](docs/development/web-auto-reconnect-verification.md)。

- **回复语言修正（2026-10-16）**：项目／任务 pi 启动与恢复统一追加系统语言要求，当前界面为简体中文，正文、过程更新、摘要和报告自然语言均要求使用简体中文；代码及协议值保留原样。7331 已更新，会话／存储回归及构建通过，未调用真实模型验证。[验证记录](docs/development/web-reply-language-verification.md)。用户反馈确认结果后缺少实际效果与操作反馈，已另提供[任务结果与效果预览设计](docs/superpowers/specs/2026-10-16-task-result-preview-design.md)及[可点击原型](docs/design/task-result-preview.html)。后续正式实现见下一项。

- **任务成果与效果预览已实现（2026-10-17）**：开发结束先查看任务工作区的真实 HTTP 效果或运行结果，再“确认完成”；确认后持续显示已完成并保留效果／修改／合并审核入口。Go 管理用户显式启动的预览进程、配置指纹、执行锁及受限网关，原 pi 会话与结果保留；无配置时明确暂无网页预览，不自动调用模型。桌面保留右侧聊天，手机提供返回入口。7331 已更新并核对旧数据保留，27 项浏览器／13 项前端状态、完整 Go／race／vet 与独立验收通过；未调用真实模型。详细边界见[本批记录](docs/development/web-task-result-preview-verification.md)，[桌面](docs/design/web-service/task-result-desktop.png)／[手机](docs/design/web-service/task-result-mobile.png)为合成项目的实际服务截图。

- **顶栏项目菜单已优化（2026-10-17）**：项目切换、Git 设置／刷新与添加项目集中到轻量菜单；默认模型、连接状态和退出统一靠右，支持键盘及窄屏长名称。7331 已更新并通过实际桌面／手机只读烟测，原数据与登录凭证数量保留；前端状态 16/16、Chromium 32/32、Go 全量与构建通过，未调用真实模型。[本批验证](docs/development/web-project-menu-verification.md)。

- **聊天打开时定位最新消息（2026-10-17）**：首次打开、刷新、切回项目／任务或手机聊天时自动滚至最新内容；停留时手动上翻不被新回复打断，回到底部继续跟随。7331 已更新并通过真实旧任务桌面／手机只读烟测，数据与登录保留；前端 16/16、Chromium 32/32、Go 全量及构建通过，未调用真实模型。[本批验证](docs/development/web-chat-latest-scroll-verification.md)。

## 模块划分

| 目录 | 责任与实际状态 |
| --- | --- |
| [server/](server/README.md) | 独立 Go module；HTTP／认证／SQLite／WebSocket／pi 会话管理 |
| [apps/desktop/](apps/desktop/README.md) | Nim + Leaf 电脑端，当前仅说明 |
| [apps/android/](apps/android/README.md) | Kotlin/KMP/Compose 安卓端，当前仅说明 |
| [apps/web/](apps/web/README.md) | Vue 3＋TypeScript＋Vite 主工作台；由 Go 同源提供 |
| [integrations/pi/](integrations/pi/README.md) | pi 外部协议/配置接入说明；不放 TypeScript 扩展，当前仅说明 |
| [contracts/](contracts/README.md) | 跨端产品协议和固定样例，当前仅归属说明 |
| [docs/](docs/) | 需求、架构、计划与实际验证记录 |

未来其他端放在 `apps/<端名>/`，不提前创建未批准工程。根目录不是 Go module，各端独立管理工具链，通过产品协议协作。

## 本机开发 Go 服务

```bash
cd server
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go run ./cmd/kaiwu-server
```

用户已指定本机服务固定使用 `7331` 端口，后续启动／重启沿用。默认地址 `127.0.0.1:7331`，当前局域网地址 `192.168.2.122:7331`；按实际监听地址检查 `/healthz`。Ctrl+C 停止前台服务。

健康检查仅返回 `{"status":"ok","service":"kaiwu-server"}`，不是 Agent/数据库/客户端就绪证明。默认仅回环；现已按用户要求支持指定私网 IP＋令牌的局域网访问，不开放公网／通配监听；完整命令和配置见 [server README](server/README.md)。

## 已确认技术方向

| 部分 | 技术与范围 |
| --- | --- |
| 服务端 | 自有后端全部 Go + net/http + chi；coder/websocket＋modernc SQLite；pi 作为第三方 CLI 管理 |
| PC | Nim + Leaf，首版目标 Windows、macOS、Linux |
| Android | Kotlin 原生 Android + KMP + Compose Multiplatform；SQLite、App 自动更新 |
| Web | 主要开发入口，优先完整任务流程；Vue 3＋TypeScript＋Vite 响应式浏览器 UI，共用 Go 后端 |
| Agent | 当前阶段 pi 主 Agent＋pi 执行 Agent，Codex 延后 |
| 拓扑 | 单执行节点，Go 服务、Agent、代码/worktree 同机；客户端连接服务 |

平台目标不是构建或验收结论。Go 基线和 chi 固定版本已确认；正式部署平台、远程认证／TLS、其他端依赖与更新方案仍需设计。

## 在这个目录继续开发

1. 阅读 [AGENTS.md](AGENTS.md)。
2. 阅读 [背景与需求](docs/product-requirements.md)。
3. 阅读 [架构方向与待定事项](docs/architecture.md) 和 [路线图](docs/roadmap.md)。
4. 阅读 [Agent / Leaf 接入研究](docs/research/agent-integration.md)，不要把接口存在当作接入完成。
5. 当前批次见 [Go 规格](docs/superpowers/specs/2026-10-11-go-foundation-design.md)、[实施计划](docs/superpowers/plans/2026-10-11-go-foundation.md) 与 [初始化实际范围](docs/initialization-scope.md)。

不依赖旧聊天或“项目初始化助手”目录。只有明确获批的批次可以实施，不能将总体方向视为全部功能授权。

## 首条开发闭环（部分实现）

需求 → 项目沟通／手动创建任务 → 卡片原 pi 会话在隔离 worktree 开发 → 记录实际工具调用与摘要 → 用户确认结果。卡片可经具体审核提交并合并至本地主分支；自动拆解派发、Go 编排桥和完整验收仍待实现，推送仍需另行授权。

Web、PC 与 Android 首版均以任务流程为中心，暂不做源码/diff、编辑器或 IDE。Web 优先验证完整体验，不能替代原生端验收。团队协作与通用可配置编排引擎保留为未来方向。

## 仓库与许可证

- GitHub：https://github.com/SatoriTours/kaiwu
- 品牌标识：中文「开悟」（悟，不是物），英文 `kaiwu`。
- 项目计划开源；**许可证尚未选择**。仓库公开不等于已有许可。
- 域名暂缓，`kaiwu.studio`、`kaiwu.life` 仅为偏好候选，未购买或确定主域名。
