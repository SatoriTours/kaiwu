# 当前改版：紧凑灰绿登录页

- [x] 用户查看 `docs/design/login-redesign.html` 及桌面／手机截图后明确批准开发；只改登录入口，保留认证与30天登录，不提交／推送／部署，不调用真实模型。
- [x] UI实现与自测：`c51d8ad2-e7d3-4547-b3a9-3ce3276f43a4`，实际配置 `pi/cliproxy/gemini-3.8-flash-high`、thinking high；App.vue／style.css与专用测试配置、截图完成，npm单元35／build／登录浏览器9／failure浏览器5报告通过。App.vue hash `0a442a59`、style.css `fbce14af`；已停止写入。原会话已核对实际diff与hash，其余混合改动保留；全仓diff-check现有types.ts尾空行为无关基线。
- [x] 独立功能验收与第1轮复验通过：`00e5014d-044b-4266-99c2-fe3c3c1d8f73`（`pi/open-design/deepseek-v4.1-flash`、配置thinking high，运行态off）。实跑 `cd apps/web && npx playwright test -c playwright.login.config.ts` 退出0、10 passed；最小复验探针退出0、1 passed，证明首次真实401无红字、提交后401显示“会话已过期”、按钮恢复、再Enter进入工作台，current-password断言通过。上一轮独立实跑30天cookie／刷新／跨context／退出撤销、网络／500提示、focus-visible 2px、真实快速Enter单POST、无存储泄漏及工作台烟测证据保留；桌面／360／390无溢出和短视口可滚，主控已目视actual桌面／手机与设计一致。测试仅临时回环服务＋假pi，服务已停止。
- [x] 原会话已核对实际diff／最终hash／限定diff-check及独立报告。P1静默回跳与autocomplete恢复均已修复并复验；本地实现完成，未提交／推送／部署，7331未更新。既有socket撤销用例单独grep缺前置session夹具 `browser-session.json`（ENOENT）属未验限制，其他退出撤销证据已通过，不伪称全套通过。完整复验报告 `/tmp/kaiwu-login-acceptance.md`；关键证据已汇总本节，设计与实际截图见 `docs/design/login-redesign*.png`。
- [x] 用户后续明确“提交代码”，授权本批commit／main推送／既有部署及安全归档；核实 GitHub `SatoriTours/kaiwu` 默认main，origin及本地基线同为`700b98a`，Actions工作流数0，不适用。仅提交登录页2实现／2测试配置／5设计资产及3份现有文档；同目录暂停子Agent后端、旧截图、二进制和测试遗留不纳入。通过既有reference-transaction钩子对候选干净归档做部署门禁，不直接构建混合目录上线。最终SHA／部署结果与证据保存于 `~/.local/state/agent-artifacts/kaiwu/2026-10-24-login-redesign/`；共享目录有其他未提交／未跟踪／忽略成果，空间保留，不擅自清理以归档。

本任务仅关联现有空间 `wks_5663ffa6688b6c19`，协调者 `f6df7bdb-8ebc-47f4-bb5a-41b40ce87d27`，cwd `/home/jimxl/projects/kaiwu`，分支 `main`，git-dir/index `.git`／`.git/index`。阶段：实现及独立验收完成，普通修复1轮已关闭；App.vue `cadbc3c6`，style.css `fbce14af`不变，spec `72bcc601`；开发build及专用10项通过，主控核diff/hash/限定diff-check通过。验收通道此前503 `workspace_billing_scope_unavailable`／HTTP2 GOAWAY，非额度耗尽；原会话同模型有界恢复一次，保留已完成9项证据，收敛剩余聚焦检查；验收图像能力受限，主控已实际目视桌面／手机截图补齐。角色token／费用未知。设计稿为本任务成果，暂停子Agent后端／旧截图／测试残留不是本任务范围。不新增或归档空间。

# 当前缺陷：任务失败原因与聊天局部布局

- [x] 核对截图、现有数据流与样式：pi errorMessage 未被保留；任务错误仅在条件化折叠区显示；短用户消息气泡整行拉伸。
- [x] 限定方案：保留具体错误，在所属聊天直接展示失败提示；短消息收紧，长内容／手机不溢出。不触发模型、提交或部署。
- [x] 用户追加授权只读服务器日志取证：2026-10-20T16:49:00Z～16:49:14Z，该任务原 pi JSONL 连续 4 条 HTTP 429／rate_limit_error，MiniMax Token Plan 速率限制（2062）；无工具执行。Web DB 仅保存通用错误，服务仍 active，未重启／重试／改模型。真实会话留服务器，不复制入仓库。
- [x] 用户质疑模型／套餐后追加只读核验：任务与服务器 pi Key 指纹一致、均 Token Plan Key；官方余额只读接口 status_code=0，当前窗口剩余 93%，不能认定额度耗尽。任务模型 `MiniMax-M3.1-Flash`、中国站 Anthropic 兼容端点；服务器 pi 1.3.0 用假 Key／合成消息／全局禁网＋拦截 fetch 的离线请求证实 `max_tokens=524288`、adaptive medium。过大输出预算可能触发 TPM，仅为待验证假设；未改配置、未真实请求。
- [x] 后端执行与自测：`46bd7ddc-e4c0-4504-b6e4-7bbdc175fe92`，仅 `server/internal/agent/`；原会话已核对实际 diff。具体原因安全提取／限长／脱敏并写现有双错误通道；新轮次清旧错误。agent 包、race、构建与限定 vet 报告通过，后续独立验收已通过（见下）。测试hash见执行端完成报告，实际生产未更新。
- [x] UI 备用实现与自测完成：原 Gemini 503 后，经用户明确授权由 `46bd7ddc-e4c0-4504-b6e4-7bbdc175fe92`（pi/opencode-go/deepseek-v4.1-flash）接续8文件；失败卡进入消息流、短气泡fit-content、状态独立，未改MiniMax配置。专用9单元／5浏览器、npm test/build、8交互／4UI一致性报告通过；版本套件2项is-outdated失败有基线复现，不在本批修复。实际diff与SHA256已核对，原会话保留独立验收关注项（发送／中断具体原因是否被隐藏）。
- [x] 后端切片独立功能验收通过：`e7739791-8372-4466-8a57-7e429db68fde`（pi/opencode-go/glm-5.3-flash），13 项聚焦测试、agent 整包、vet 退出 0；原因双通道／脱敏限长／兜底／新轮次清理／已有结果保留覆盖。原会话实际 diff、验收范围、SHA256 与 `git diff --check` 已核对。7 文件 SHA256 前缀：failure.go `dd8f9154`、failure_test `3cf4f52b`、failure_events_test `46e60752`、events `dac8be81`、manager `3d8aaef9`、model_next `3485f59a`、manager_test `7ac94071`。观察项：含 `{` 的不可解析纯文本回退通用原因（安全兜底）；真实上游根因验证与 UI 不在通过范围。
- [x] UI 独立验收通过：原验收 `e7739791-8372-4466-8a57-7e429db68fde` 实跑 `cd apps/web && node tests/failure.test.ts`（9/9）、`npm test`（29＋6）、`npx playwright test --config=playwright.failure.config.ts`（5/5）及临时DOM／视觉探针，退出均0。1440／390／360截图目视与无横向溢出、短气泡、角色状态分离、具体／空原因、任务隔离、待确认结果保留有证据。原会话核对8文件SHA256与diff-check一致。发送／中断当前后端只有固定状态句，未发现具体诊断被隐藏；保留低危文案观察：发送未达时“pi 未提供具体原因”可优化为中性措辞。截图与测试产物位于/tmp，非线上验收。
- [ ] 本地修复及独立切片验收完成，未提交／部署；当前真实网页未更新。既有版本浏览器2项is-outdated失败保留，不宣称全套通过。实际 MiniMax 429根因仍未确认；用户后续明确要求验证，2026-10-21T04:56:25Z用同Key／模型／国内端点、medium、8192预算单次真实最小请求成功：1221ms、回复OK、225 token（212输入／13输出含9推理），无重试／工具／配置写入，非Web任务恢复。未做524288同期对照，不据此认定预算因果。证据见模型选择验证补记。
- [x] 用户批准降低默认预算并验证原任务：服务器定义＋原任务隔离副本均持久化8192（备份留服务器），同一模型／medium、原session／worktree经现有set_next_profile同配置回收后继续。8次真实模型响应／13工具，本轮无429，生成5文件；最终waiting_user／awaiting_user，非完成。工作台未重启／部署，Git未提交。非缓存输入7465、缓存38017、输出4473，本地价表估算$0.0043547891，非实付；详见模型选择验证最新补记。
- [x] 用户后续明确授权环境修复：从官方校验SHA256后用户级安装Go1.27.3，修复Git包装脚本从自身路径定位工具而非HOME；server／pi隔离HOME下git/go均可用。未改系统目录／认证／PATH／工作台程序。备份证据留服务器tool-updates/2026-10-21T11-52-09-937Z。
- [x] 经原待答交互入口继续原pi：4项Go测试／vet／build及真实回环GET/HEAD/404通过，临时程序停止、端口释放；原5文件未改、未提交。任务needs_confirmation／awaiting_review，未代用户确认。此轮6次模型响应无error，非缓存输入11051、缓存读取50021、输出2314，价表估算$0.0037791787非账单。
- [x] 环境与Go示例独立验收通过（e7739791）：官方归档校验／用户安装位置／Git脚本diff与隔离HOME工具可用核对，独立go build/vet/test退出0、4测试通过；真实回环34621的GET/hello200正文world、HEAD空体／未知404及Content-Type通过。测试进程／端口与临时产物已回收，5源码hash及Git无提交不变，工作台54851仍active。主控再次核对wrapper和5源码SHA256、服务PID与34621已释放。
- [ ] 交付边界：原产品任务保留needs_confirmation／awaiting_review待用户确认，不新建Paseo空间，不将其5文件混入开悟源码。MiniMax输出预算8192已生效、本轮真实调用恢复且示例运行验收通过，但未做原大预算同期对照；工作台失败提示／布局源码仍未提交／部署。

- [x] 2026-10-23 用户“提交代码”（重复口令合并一次）授权本批整合／push／部署；核实GitHub `SatoriTours/kaiwu` 默认main，远端及本地基线均e43b8bf。Actions工作流数0，不适用，沿用已安装的提交钩子干净归档部署，不伪称CI通过。
- [x] 19文件限定暂存，无既有暂存内容；排除暂停子Agent实现、旧截图、假进程二进制和测试遗留。源码hash与独立验收一致。暂存树641e5a55导出的测试快照：agent包、专用单元9项、npm单元29＋6、前端构建及专用浏览器5项通过。归档浏览器首次漏传GOFLAGS导致VCS标记失败，按既有部署脚本传入-buildvcs=false后通过，无实现改动，不掩盖失败。
- [ ] 交付执行：本批commit／push、干净归档完整部署门禁及实际7331入口核查；最终SHA和结果另存当前空间外。关联Paseo空间仅当前wks_6b8c83a60fa23afb，子Agent46bd7ddc／e7739791／4c2fea93同checkout且已停止。没有新增关联Workspace。服务器Go示例为原产品测试任务，保留原worktree与待确认状态，不进入开悟仓库。共享checkout尚有其他任务未提交／未跟踪／忽略成果，不满足安全归档，保留空间，不删除、stash或代交。

协调空间：`wks_6b8c83a60fa23afb`，cwd `/home/jimxl/projects/kaiwu`，`main`，git-dir/index `.git`／`.git/index`。现有子 Agent 后端、截图等无关混合改动原地保留。角色消耗未知，修复轮次 0。

# 当前设计：固定开发交付与 Actions 自动修复

- [x] 核对现有 Git／worktree／会话／恢复机制，确认当前仅有本地审核集成。
- [x] 用户将 Push 入口需求扩展为项目配置的统一流程，要求 Actions 检查及自动修复；按 Architectural 路径推进。
- [x] 比较手动串联、仅提示词驱动、Go 固定状态机；用户确认方向并同意整理书面设计。
- [x] 编写并自查[规格](docs/superpowers/specs/2026-10-17-automated-delivery-design.md)，同步需求／架构／README／路线图；不提交、不写业务代码。
- [x] 用户在规格交付后回复“ok”，审核书面规格及其中的默认规则、凭据与授权边界。
- [x] 编写并自查[14项实施计划](docs/superpowers/plans/2026-10-17-automated-delivery.md)，明确接口／失败测试／恢复边界及实际模型切换缺口。
- [x] 用户回复“ok”审核计划并选择当前会话串行实现＋最终独立只读验收。
- [x] Task 1、2：配置／领域校验、schema v2、周期／步骤／快照事务与回执；Go 全量、workbench race／vet 通过，见[执行记录](docs/development/web-automated-delivery-ledger.md)。
- [x] Task 3：官方契约取证阻塞已解除；Actions 只读客户端与合成契约测试已实现。
- [x] Task 4–12：凭据桥、远程 Git、原会话修复／模型下一轮、FIFO 集成、崩溃恢复、业务 Socket 与生产装配；隔离 Go 测试通过。
- [x] 2026-10-18 核对未提交成果：本地提交 `1c76c0a`，81 文件；Go 全量与前端原有 16 项复跑通过，未推送。两项 extra-*.txt 历史测试残留保留但未提交。
- [ ] Task 13：前端按实际 Pi Gemini UI profile 实现，主控已验前端27/27、Chromium42/42（含10项真实服务交付场景）；真实Go＋临时bare＋假pi／Actions六场景race通过。配置保存未知回执等边界仍待收口，不能把页面功能与隔离测试当作整体可上线。
- [ ] Task 14：独立后端审查已完成但不通过；主控修复 2 Critical、3 Important，证伪 1 Important，另 1 项部分修复。仍有 2 Critical／14 Important／1 Minor，见[续接清单](docs/development/web-automated-delivery-review-resolution.md)。Go 全量／race／vet／构建／烟测与六场景夹具 race 已复验；不宣称真实闭环已可用，不启用真实上传，7331 未更新。
- [ ] 真实 GitHub／Actions／模型联调另行说明费用、副作用并获准。

# 当前批次：正式 Web 服务

- [x] 用户确认 Vue 3＋TypeScript＋Vite、SQLite、本机令牌和 pi 独立会话生命周期。
- [x] Go 持久化／幂等回执、认证／同源静态资源／WebSocket、独立 RPC 管理。
- [x] Vue 专题看板、项目／任务聊天、独立草稿／阅读位置、手机视图、断线确认。
- [x] 真实 Go＋隔离假 pi 浏览器 7 项场景通过；真实模型未调用。
- [x] 最终 Go race/vet、独立复验与 [证据归档](docs/development/web-service-verification.md)。
- [ ] 下一条：pi 执行 Agent、worktree 单写入者、Go 编排工具桥、审批／实际验证／结果验收。
- [ ] 用户要求每张卡片有“开始开发”按钮，任务聊天也可说“开始开发”；[双入口与执行生命周期方案](docs/superpowers/specs/2026-10-12-task-start-design.md) 已整理，待审核，尚未编码。

以下保留历史批次记录，不代表当前服务状态：

# 当前状态：Go pi RPC 基础已验收，Web 看板交互原型已实现

## 当前设计修订：Kanban 主界面＋右侧双范围聊天

- [x] 用户最新确认功能专题列可手动添加／删除；每个卡片是任务，右上角显示未开始／执行中／需要确认／执行完成查看结果／执行失败。此前固定状态列取消；右侧项目／任务聊天规则不变。
- [x] 用户确认增加“其他”专题；删除专题时任务全部移到其他，不删任务、不改状态／对话／结果、不停止执行。
- [x] 用户要求开始开发；本批收敛为本地专题看板交互原型，不隐式推进生产工程或真实模型联调。
- [x] 据最新决定统一正文图示、专题管理、其他收纳与五种角标；移除旧状态列／验收后搬列规则。
- [x] 核对当前文件、Git 与同目录写入者；main 无基线，本会话是当前唯一 running 会话，尚未派发编码。
- [x] 核对实际 Paseo profiles：前端用途配置现名“opencode deepseek”，架构／验收配置保留；不根据旧“Gemini”名字猜模型。
- [x] 编写并自查 [限定原型计划](docs/superpowers/plans/2026-10-11-web-kanban-prototype.md)：纯状态测试、真实 DOM 检查、一次独立验收；入口同步。
- [x] 用户明确“继续开发”；按现有原型计划继续实现。默认 Paseo daemon 不可用，当前 Agent 串行编码＋工具提供的只读独立验收；未声称使用不可读取的 profile。
- [x] Task 1：稳定专题／任务／Conversation、独立草稿与迟到记录的 RED→GREEN；15 项状态测试通过。
- [x] Task 2：专题看板、右侧聊天、浏览器行为／布局 RED→GREEN 与新截图；44 项交互检查通过，含审查发现的手机阅读位置及重复标签回归。
- [x] Task 3：本批实际记录、一个只读 Agent 独立验收与修复复验、问题反例修复及本地交付；0 Critical／0 Important／1 焦点 Minor，历史 20 项检查不复用。

## Web 主工作台原设计与历史原型（2026-10-11）

- [x] 用户确认新增 Web 版，作为主要开发入口，优先完整流程；PC/Android 保留首版目标。
- [x] [功能与交互设计提案](docs/superpowers/specs/2026-10-11-web-workbench-design.md)：信息架构、核心流程、状态、审批/模型/恢复边界、技术建议与 W0–W4 分期。
- [x] 历史模拟原型曾演示任务创建、对话、审批、模型待生效、离线只读和结果验收；其 [历史记录](docs/development/web-design-verification.md) 单独保留，HTML 现已换为专题看板原型，不复用旧功能或测试声明。
- [x] README、需求、架构、路线图与客户端入口同步。
- [x] Chromium 原型 20 项交互检查、1440px/360px 截图核对、脚本语法与 Markdown 本地链接检查；[实际记录](docs/development/web-design-verification.md)。不代表生产业务验收。
- [ ] 用户审核具体交互方案与前端选型；认证/持久化/可信执行边界另行设计。
- [ ] 生产 Web 工程、Go 业务 API、真实 pi 闭环与跨浏览器验收；不由设计文档隐式授权实现或发布。

## 已完成批次：Go pi RPC 基础

用户已审核 [RPC 基础规格](docs/superpowers/specs/2026-10-11-go-pi-rpc-design.md) 并回复“继续”，确认具体生命周期、限额及验收范围；自有后端全部 Go。[实施计划](docs/superpowers/plans/2026-10-11-go-pi-rpc.md) 及串行编码＋独立验收方式已批准；Tasks 1–4 已实现并自测通过，Task 5 独立验收已通过，首轮 2 Important/1 Minor 已修正复验。不启动产品真实 pi/模型联调，不提交或推送。

## 已完成批次：Go 服务基础

状态：用户已确认规格、计划及当前 Agent 串行实现＋独立验收方式。模块与 Go 代码已实现并通过初次独立验收；3 项边界修正已获用户确认并通过本机 32 项测试、复测及限定独立核对；不提交、不安装全局工具、不调用真实模型。

## 设计流程

- [x] 核对项目文档、文件和 Git 状态：仓库只有文档，main 尚无提交。
- [x] 明确本轮功能范围和成功标准：先划分仓库模块并建立 Go 服务基础，pi 接入随后推进。
- [x] 比较 3 个推进方案；用户同意推荐方向，并要求先完成 Go。
- [x] 用户随书面规格确认详细配置、HTTP、生命周期与验收设计。
- [x] 当前没有需要浏览器可视化的问题，不启用可视化辅助。
- [x] 写入 docs/superpowers/specs/2026-10-11-go-foundation-design.md，未经授权不提交。
- [x] 自查规格的占位项、一致性、范围与歧义。
- [x] 用户审核书面规格并回复“好的，继续”。
- [x] 编写并自查 docs/superpowers/plans/2026-10-11-go-foundation.md。
- [x] 用户审核实施计划及确认推荐执行方式。
- [x] Task 1：模块入口与回环配置，RED/GREEN 已记录。
- [x] Task 2：HTTP、日志、panic 边界，RED/GREEN 已记录。
- [x] Task 3 编码：启停、退出码及二进制烟测，RED/GREEN 已记录。
- [x] 本机全量测试、race、vet、格式、依赖校验、构建与信号烟测。
- [x] 同步各模块入口、README、架构、初始化范围和路线图。
- [x] Paseo 独立只读验收：无阻塞问题；3 项 Minor 保留，完整结果已保存。
- [x] 最终普通/race 测试、构建/信号烟测复核通过；保留未提交成果，不合并/推送。

## 已确认的小范围收尾

- [x] 核对独立审查结果、源码并自行复现 M2/M3。
- [x] 提出 bounded 修正方案：Server ErrorLog 接入 slog；未知方法/不存在路径返回正确 404；HEAD/GET 显式 Content-Length 一致。
- [x] 用户确认该短设计并回复“继续”，不另写架构规格或长实施计划。
- [x] 每项增加失败回归测试、最小修复，32 项全套测试/race/vet/二进制烟测通过，并更新问题历史与当前状态。
- [x] 限定独立核对：原验收会话 RPC 已关闭，替代只读 Agent 27abc3d6-d302-4b49-85f4-88d89718aecc 验收通过；文档滞后已同步。
- [x] 最终复核和本地交付；仍不提交/推送、不调用真实模型。

## 本批次设计流程：Go pi RPC 接入基础

状态：Architectural 路径；pi 1.0.4 RPC schema 已随规格确认作为有限协议基线，不代表真实兼容验收。以下设计/计划任务不构成编码授权。

- [x] 核对项目文档、已有服务结构和 Git 状态；main 仍无提交。
- [x] 本批次暂无需要浏览器展示的问题，不启用可视化辅助。
- [x] 核对本机 pi 1.0.4 的文档、RPC 类型、客户端、RPC mode、JSONL 分帧和隔离参数；未启动 pi，未验证真实运行。
- [x] 复述目的、约束与成功标准；用户确认模块边界，并进一步明确自有后端全部 Go。
- [x] 比较接入方案：采用 Go 直管 RPC；排除自有 TypeScript SDK 宿主和终端抓取。
- [x] 同步当前需求与架构；旧 Go 基础规格/计划注明扩展建议已被取代，保留历史。
- [x] 写入候选生命周期、超时/取消/背压、故障策略和假进程验收的完整待审规格；未经授权不提交。
- [x] 用户随书面规格审核具体模块、数据流、限额和退出策略。
- [x] 自查规格的占位项、一致性、范围与歧义；明确零值 Options 使用默认值、写入总期限和取消后的载荷释放。全仓库本地 Markdown 链接检查 56 项，损坏 0；未执行代码测试。
- [x] 用户审核并批准书面规格，回复“继续”。
- [x] 编写实施计划并自查覆盖、接口一致性、5 项 Review Focus 与测试归属；计划阶段本地 Markdown 链接检查 65 项、损坏 0，规格/计划状态一致；RPC 包尚未创建，全部代码/测试待执行。
- [x] 用户回复 ok，审核实施计划并确认当前会话串行编码＋完成后独立只读验收。
- Tasks 1–5 已完成；逐项 RED/GREEN 见 [ledger](docs/development/go-pi-rpc-ledger.md)。

## 获批计划执行

- [x] Task 1：协议、显式参数、有界 JSONL 的 RED/GREEN。
- [x] Task 2：事件 FIFO 与 stderr 尾部的 RED/GREEN。
- [x] Task 3：Go 假进程、自管管道和直接子进程回收的 RED/GREEN。
- [x] Task 4：握手、关联、取消、预算和统一终态的 RED/GREEN。
- [x] Task 5：全量验证、实际记录和独立只读验收；84 顶层通过＋1 预期 Skip，首轮问题已修正复验。
- [ ] 真实模型、权限、持久化、主执行闭环和双端继续单独推进，不由本清单隐式授权。

## 已知边界

- 自有后端全部 Go，不新增 TypeScript 扩展或 SDK 宿主；pi 仍是第三方 CLI。
- pi 优先，单执行节点；Go 服务负责生命周期，双端是客户端。
- PC：Nim + Leaf，目标 Windows/macOS/Linux；Android：Kotlin + KMP + Compose。
- 执行任务使用独立 worktree，同工作区单写入者。
- 配置模板、任务快照、任务内模型覆盖分别管理。
- 接受命令、Agent 运行结束、任务验收成功不能混淆。
- 客户端断线不能隐式取消服务器任务。
- 合并、推送及破坏性操作需人工确认；worktree 不是安全沙箱。
- 当前空仓库无 worktree 基线，不为创建隔离空间擅自提交。
