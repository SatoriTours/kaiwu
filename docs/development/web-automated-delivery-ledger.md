# 固定自动交付执行记录（基础层，未完成产品闭环）

日期：2026-10-17。

## 授权和当前状态

用户已审核[规格](../superpowers/specs/2026-10-17-automated-delivery-design.md)、[14项计划](../superpowers/plans/2026-10-17-automated-delivery.md)，选择当前会话串行实现、最后独立只读验收。实现仍在当前 checkout，不创建新 Paseo 空间／Git worktree。2026-10-18 用户要求核对并提交后继续，已有后端成果已本地提交 `1c76c0a`；仍不合并／推送或更新 7331。

**Tasks 1–12 后端已实现并通过限定隔离测试；Task 13 主体界面与真实服务浏览器 10 场景通过（前端27/27、完整 Chromium42/42），配置未知回执等续接项尚未关闭；Task 14 独立审查已进行但不通过。** 本轮已修复真实 pi 模型对象握手、私密历史／DataDir、真实 index／commit tree、预览交权与缺省身份等回归，仍有 2 Critical、14 Important（含部分修复项）及 1 Minor 未关闭；见[实际验证](web-automated-delivery-verification.md)、[原审查](web-automated-delivery-review.md)与[裁决／下一步](web-automated-delivery-review-resolution.md)。**不得以 Go／浏览器绿灯替代安全验收，不具备真实联调或上线条件。** 真实模型／GitHub／Actions 未联调，7331 未更新。

## Task 1：领域规则

新增 `server/internal/workbench/delivery_types.go`、`delivery_policy.go`、`delivery_policy_test.go`：

- 定义项目授权／检查／workflow／周期／步骤／CI／修复和无凭据摘要。
- 缺省关闭；保留效果验收，默认 3 次修复、3600 秒活动期限、300 秒触发窗口、1800 秒 CI 期限。
- 验证 GitHub 仓库地址、分支、唯一检查／workflow/job、有限超时和固定凭据引用；拒绝凭据 URL、错误主机／协议、越界工作目录。
- 即使未启用的配置草稿也拒绝含凭据 URL，避免将秘密带进数据库／摘要。
- active 阶段包括 blocked/paused/needs_check，不把它们误认作已经释放集成隔离。

TDD：缺失类型／方法的 RED → GREEN；额外的禁用草稿凭据 URL 回归测试先失败再修复。首次 SSH 正例测试错误地沿用 HTTPS 凭据引用，已纠正 fixture，协议不匹配仍明确拒绝。

## Task 2：存储、迁移与原子记录

新增 `workbench/migration.go`、`delivery_store.go`、`delivery_steps.go`、`delivery_sync.go`、`delivery_store_test.go`，针对性修改原 `store.go` 与 `types.go`：

- SQLite schema 1 → 2 迁移，新增独立配置／周期／步骤／CI／修复／队列表，不把日志塞进全量状态。
- 仅 enabled 项目的显式开始开发在原业务事务中保存周期、配置快照、用户消息／轮次、状态投影和同 operationId 回执。
- 历史任务迁移不启动任务，模板变化不追溯已有周期；旧登录记录和原有业务状态保留。
- 周期 CAS、active task 唯一约束、步骤输入指纹、观察证据及有界分页；快照最近 20 步且每条截取小摘要。
- SQL trigger 注入事务失败，验证状态、步骤、周期和操作回执不会部分提交；同操作重放、同键异参、过期版本均有测试。
- Store 的 JSON clone 会丢失带 omitempty 的空 map；新摘要 map 与现有 map 一样不使用 omitempty，修复由实际失败定位。
- 相同步骤回执重放测试揭示了比较前未归一化存储序号的问题；测试 RED 后修复，重放不增加步骤行。

表和类型存在不代表队列调度、Actions 判定或模型修复已经实现。该版本尚未部署到用户数据目录，迁移只在隔离测试数据库运行。

## Task 9：修复闭环、效果验收与安全停机

新增 `delivery/control.go`、`delivery/repair.go`，改 `delivery/stages.go`、`delivery/manager.go`、`delivery/types.go`；新增 `workbench/delivery_control.go`、`workbench/delivery_repair.go` 及两个包的测试：

- `ClassifyFailure`：本地检查失败可修复；CI 失败结论先剥 `:not_repairable` 后缀再按脱敏后的有界证据分类——权限／Secret／限流／审批／超时／取消／冲突等一律 blocked（`ci_*`），仅纯源码失败可修复；`[已脱敏令牌]` 不触发 Secret 模式。
- 修复证据仅在观察码为 `failure` 时抓取，默认 64KiB 截断、指纹化后附进原会话的系统修复 prompt（截 4000 字）；预算 3 次／激活 3600 秒由 claim 扣减、persist 记录与 fake Clock 判定；同指纹且候选未变直接停（`repair_loop`）。
- `stageRepairing` 等到 `ID==repair.MessageID` 轮次确实 finished 才清旧本地／CI 证据回 `local_checking`，旧结果永不代新 push 验收。
- 验收令牌单次消费并与周期绑定；源码变更后旧 token 拒绝、历史认可保留；无人工模式技术成功直达 `succeeded` 且不调用 ConfirmResult、任务不自动 completed。
- `Manager.Control` 支持 pause/resume/cancel/extend_budget：回执同事务保存重放；cancel 先核验写入者租约；pause 不撤销已发出的 push；extend_budget 只追加显式快照额度并记审计步骤，不改历史计数与原 policy 快照。
- GREEN 期间发现并修复关键缺陷：`repair()` 在 claim 后只把新鲜版本填回陈旧副本做 CAS，覆盖丢失了 claim 扣掉的 `RepairsUsed`，预算永不耗尽；已改为以 fresh 交付周期为基底再改阶段。测试定位修复循环的 settle 断言改为按轮次身份逐轮收尾，不依赖可能与机器滞后的“最新行”。

实际验证：`go test ./internal/delivery -count=1`、相关四包回归、全仓 `./...` 15 包、`delivery/workbench -race`、gofmt／vet 全部通过；真实模型／GitHub／Actions 未联调。完整裁决见 `.superpowers/sdd/2026-10-17-automated-delivery/progress.md`。

## Task 10：持久 FIFO、候选同步与主分支隔离

新增 `workbench/delivery_queue.go`、`gitops/delivery_merge.go`、`delivery/integration.go` 及三包测试，并改 `workbench/git.go`、`workbench/delivery_store.go`、`workbench/delivery_control.go`、`gitops/status.go`：

- 集成队列按两条 lane 键持久化：canonical Git common dir（同 clone 单写入者）与 GitHub repository ID＋目标主分支（跨 clone 共享远程主线）。票据单调 FIFO、重启不丢、等待 CI／验收不占 OS 锁、其他任务可继续开发与推送任务分支。
- 旧手动合并与交付互斥：任务有活跃交付周期时禁止手动手动合并入口；任务合并中禁止开始交付。
- 释放隔离只允许已核验（主 CI 通过）或显式取消并落“未验证”警告步骤；显式解除仍保留警告。
- `SyncCandidate` 由 Go 在任务分支普通合并最新可靠主分支，冲突中止且不自动选择 ours/theirs；`IntegrateCandidate` 只在本地主分支仍等于已验证 SHA 且候选包含它时快进，未知本地领先提交不上传也不被覆盖。
- 机器激活规格阶段：preparing_integration→integrating→integration_checking→pushing_main→main_ci→succeeded；主 CI 失败把证据交回原任务会话修复且保持隔离，不放行下一任务；验收语义修正为放行集成（succeeded 只在主 CI 通过后到达）。
- 测试内置真实本地 Git 仓库与隔离 fake 远程／Actions；主分支失败隔离、跨 clone FIFO、重启持久、冲突不自动解决、未知领先不上传均有子测试。

实际验证：`go test ./internal/workbench ./internal/gitops ./internal/delivery -count=1`、全仓 `./...` 15 包、`-race ./internal/delivery ./internal/workbench ./internal/gitops ./internal/agent`、gofmt／vet 全部通过；真实 GitHub 推送／Actions 未联调。

## Task 11：每步骤崩溃核查与服务生命周期

新增 `delivery/recovery.go`、`recovery_test.go`、`recovery_linux_test.go`、`recovery_other_test.go`，并给任务／主分支推送阶段加持久 intent：

- 不可逆远程写入前先落 `running` intent 步骤（Ref、写入前基线、拟推送 SHA、StartedAt），成功后同 ID 改写为 `observed` 回执；阶段自身在重复写入前先核对遗留 intent，避免重启后重复推送。
- `Manager.Recover`：只对记录＋只读观察唯一可证的窗口补记（落地即补回执并推进阶段、明确未写入则交回原阶段重试），其余一律进入 `needs_check` 并保留全部逻辑槽；授权失效／配置变化直接阻塞，不恢复写入。
- 崩溃窗口不重置预算：恢复后的 CI 截止时间由 intent 的 StartedAt 推算；每次推送重新签发验收令牌，候选变化即令旧验收过期。
- commit 窗口仅在“工作区 HEAD 等于记录结果且其唯一父提交等于记录基线”时补记。
- 真实崩溃测试：Linux 上以子进程运行真实状态机与真实 bare 远程，在 intent／推送后／回执后三个窗口 SIGKILL，父进程重开同一 SQLite 与远程后恢复，验证不重复推送、未落地恰好重试一次、队列槽保留、旧 CI 继续收尾；非 Linux 平台显式标注 unsupported。
- 未按数据库 PID 杀进程；`Close` 停新步骤并释放租约，服务自有进程回收沿用既有 agent process-owner 机制。

实际验证：`go test ./internal/delivery -count=1`、关键故障测试 `-count=10`、全仓 `./...` 15 包、`-race ./internal/delivery ./internal/workbench ./internal/gitops`、gofmt／vet 全部通过；真实 GitHub／Actions 未联调。

## Task 12：业务 Socket、认证授权与生产装配

新增 `webapi/delivery.go`、`webapi/delivery_test.go`，改 `webapi/handler.go`、`webapi/socket.go`、`app/app.go`、`app/app_test.go`、`delivery/types.go`：

- `DeliveryCommander` 只暴露 Preflight／Accept／Control／Wake：没有自由合并、没有任意 argv 代执行、没有任意 credential env 名参数。
- 固定路由 `/api/delivery/{policy,preflight,accept,control,status,steps}`，socket 同名方法映射；policy 查询与保存走 Store；status 只投影安全摘要（无工作区路径、无凭据）；steps 限 1–100；accept／control 仅在非回放时唤醒调度器。
- `set_next_profile`：真实 catalog 校验后调用 `Store.RequestNextProfile`，下一轮生效，不打断当前请求、不改项目模板。
- `app.Run` 生产装配：凭据齐备（API 令牌＋Git 令牌/SSH）时构建 Resolver→RemoteRunner＋Actions Client→delivery.Manager（AgentPort=agent.Manager，SystemClock），开放业务前先 `Recover`，关闭顺序 API→delivery→preview→agent→Store；凭据缺失则交付功能整体 disabled，基础服务照常启停，无生产 fake 开关。
- 凭据仅从服务环境读取，不写入配置、数据库或 snapshot。
- 测试：`TestDeliverySocketAuthorizationAndReplay`（401／403／过期会话／跨项目 404／操作回放不重复唤醒／preflight 零副作用／steps 越界 400／socket 往返）、`TestDeliveryAppLifecycle`（有凭据：装配且先恢复、snapshot 无凭据、关闭后数据库可重开；无凭据：503 且基础服务正常）。
- 如实记录的流程偏差：本任务未单独执行 RED 步骤（实现与测试同批次完成）。

实际验证：`go test ./internal/webapi ./internal/app -count=1`、全仓 `./...` 15 包、`-race ./internal/app ./internal/webapi ./internal/workbench`、gofmt／vet 全部通过；未启动或重启 7331。

## 实际验证

- 初始 `GOTOOLCHAIN=local go -C server test ./...`：通过（基线缓存）。
- 初始 `npm --prefix apps/web test`：16/16 通过，仅基线；本批没有改前端业务代码。
- Task 1、2 分别执行 `go test ./internal/workbench`：通过。
- 基础层最终 `GOTOOLCHAIN=local go -C server test ./...`：通过，包含 agent/webapi/app/preview 等原有回归。
- `GOTOOLCHAIN=local go -C server test -race ./internal/workbench`：通过。
- `GOTOOLCHAIN=local go -C server vet ./internal/workbench`：通过。
- 新增 7 项顶层交付规则／存储测试，另含输入表驱动子测试；真实 GitHub／模型／Actions、浏览器新流程与最终独立审查均未执行。

完整临时输出和任务 brief 在本计划专属 `.superpowers/sdd/2026-10-17-automated-delivery/`，不清理此目录。Task 3 历史取证阻塞已解除，当前从 Task 13 继续，不重复 Tasks 1–12。

## 历史阻塞：Task 3 官方接口契约（已解除，见文末 Task 3–7）

按已批准计划，Actions 实现前必须核对官方 run/job/log/attempt API 和固定版本；不能仅凭搜索摘要编造协议。

本次实际尝试只读抓取：

- `https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2022-11-28`
- `https://docs.github.com/en/rest/actions/workflow-jobs?apiVersion=2022-11-28`
- `https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json`

均由工具安全检查拒绝：域名被本机网络解析为 `198.18.0.0/15` fake-IP 范围（docs 为 `198.18.25.74`，raw 为 `198.18.5.131`）。未修改 SSRF 允许范围、代理设置或系统网络，没有绕过安全检查，没有调用真实 GitHub API。

继续条件：通过工具支持的明确可用代理获取官方资料，或用户提供可校验来源的官方 REST OpenAPI 文件／完整端点文档；取得资料后先核对契约，再按原计划实施。此暂停不是撤销实施授权，不需要重做需求或重新审核原计划。

## 已作裁决与代价

1. 在用户已审核的当前 main checkout 执行，无新提交：遵守当前 Workspace 和计划授权。代价是共享文件要串行，最终审查必须参考保留的 baseline.patch，不能仅看提交范围。
2. `task-done` 的 commits range 没有新提交，不作变更归属证据；最终使用工作树差异。代价是本计划 scratch 必须保留，不能按技能模板删除。
3. 可配置额度加有限输入上界：最多 7 天和 100 次，默认仍为 1 小时／3 次，避免溢出或无界策略。若需更大特殊额度，应另行设计而非静默无限。
4. “外部 port 阻塞时 Store 仍响应”验证留给拥有该 port 的 Task 7；Task 2 的 Store 不运行外部操作。代价是当前不声称已证明未来 worker 调度响应性。

后续产品方向仍是团队协作和通用可配置编排；当前优先完成这条固定流程，不将基础层误报为阶段总验收通过。

## Task 3–7：执行层实现（隔离测试，未联调）

Task 3：新增 `internal/githubactions`（runs/jobs/logs/workflows/client），只读观察。首次尝试抓取官方文档被本机 fake-IP 安全检查拒绝，后经工具安全通道成功获取官方契约并记录于 `testdata/contracts.md`；默认 API 版本固定 `2026-03-10`。304 仅证明未变化，永不充当新成功；分页未遍写完整即 blocked；日志 8 MiB 截断、64 KiB 脱敏摘要、无 URL 字段。

Task 4：新增 `internal/githubauth`（凭据解析与 helper 桥）。凭据仅通过继承 fd 交给受控 git helper，helper 拒绝 store/erase、host 固定 github.com；grant 增加额外 `Files` 字段（仅新增字段）。未实现任何凭据落盘。

Task 5：`internal/gitops/remote.go`：只读 Observe/Fetch/受约束非 force Push，推送结果一律「未确认即需 Observe」；transport 注入点仅测试可用，生产装配必须为 nil。复用既有进程组 helper，未新建 platform 文件。

Task 6：`agent/delivery_lease.go`（写权租约与 `AcquireDeliveryLease`，修正原双重 filepath.Dir 造成的全家共享锁 bug）、`gitops/candidate.go`（候选冻结／提交，检查后变化一律拒绝提交）、`delivery/checks.go`（argv 直执行、白名单环境、300 秒上限、8 MiB 输出、256 KiB 失败摘要，租约拒绝即中止）。

Task 7：新增 `internal/delivery/{types,manager,policy,stages}.go` 单 worker 状态机：恢复只读预检与安全基线 → 原会话单次派发 → 本地检查（持有任务租约）→ 候选冻结与提交（无有效变更不建空提交，提交失败一律不重试）→ 精确推送任务分支（先 CAS 后执行，结果以 Observe 为准）→ 只读 CI 门禁（304 不算成功、等待不写终态、超预算 CI 截止转 blocked）。授权门禁在每阶段副作用前重读现行策略：停用／撤销／无效／保护范围指纹变化即转 blocked。CI 通过且 RequireAcceptance 时生成验收令牌进入 awaiting_acceptance，未获人工确认不推进。

Task 8：新增 `workbench/delivery_repair.go`（PrepareDeliveryRepair／ClaimDeliveryRepair／MarkRepairDispatching／RequestNextProfile）与 agent `delivery_repair.go`／`model_next.go`，扩展 `Round`（Profile/Origin/DeliveryID）和 `Conversation.PendingProfile`：

- 原会话修复：准备＝CAS 校验冻结版本＋共用额度检查＋同指纹无变更停止；claim＝单事务扣共用 RepairsUsed、追加系统消息（Role=system、Kind=automatic_repair，不伪造人类消息）、建修复轮次（Origin=system_repair）并把会话转为 starting；重复派发同一 job 幂等，不重扣不重发；dispatch 状态在每次模型 Call 前落库。
- 下一轮模型：RequestNextProfile 只挂 PendingProfile，不中断运行请求，不回写模板／任务画像；下一轮 run() 边界先回收旧空闲实例（Close+Wait 核实、释放租约、等待消费者），再以新 Profile launch 同一 session 文件；get_state 回报可核对的 model/provider 时严格核对，失败不发送本轮请求且保留 PendingProfile，成功才写新画像并清 Pending。
- pirpc 无 set_model/set_thinking；fake 契约新增 get_state model/provider 回显与失败注入；本地／任务 CI／主 CI 共用 3 次额度、重启不清零。

- 预检为只读（Inspect＋workflow 存在性），不宣称写权限，不调用 Git 网络。
- 阶段链之间插入 40ms 节奏（仅为外部可见性，与截止时间无关）；deadline 全部由持久记录加注入时钟驱动，无真实 sleep。
- 该阶段 `webapi` 的 start_task 仍走原直发路径；本应归属 Task 12 的 Wake 分流在 2026-10-18 真实串联中发现遗漏并补上，见下文。

## 2026-10-18：核对、基线提交与 Task 13 接续

- 核对实际 Git 与文档：20 个已修改、63 个未跟踪文件。排除两项内容为 `cycle-two` 的历史 `extra-*.txt` 测试残留，81 个源码／测试／设计文件本地提交为 `1c76c0a`，未推送。残留保留，不作破坏性清理。
- 新鲜基线验证：`GOTOOLCHAIN=local go -C server test ./...` 通过；`npm --prefix apps/web test` 原有 16/16 通过。旧记录中 Task 3 仍阻塞的入口已修正，保留当时取证历史。
- 遵循当前项目实际 UI profile，将 Task 13 前端有界实现交给 Paseo 当前 Workspace 的 Pi `cliproxy/gemini-3.8-flash-high`；主控负责 Go 夹具／后端串联／文档和验收，文件范围不重叠。与早先 inline 方式的差异记入 SDD ledger，不另建 Workspace/worktree。
- 新增 `server/internal/delivery/testdata/webfixture/`：实际 Store／agent.Manager／delivery／webapi、临时真实 Git/bare、隔离假 pi、只接管 GitHub HTTP 的测试 transport。Go 夹具不编入生产；新增 mjs 仅启动测试进程，17332 回环端口，既有 17331 浏览器服务保留。
- `TestDeliveryStartDispatchOwnedByWorker` 先失败（HTTP 直接 Send／worker 缺失仍接纳）→ 修正已启用任务只 Wake，缺 worker 返回 503 且不保存周期。
- `TestDeliveryDispatchPrecedesWorkspacePreparation` 先失败（`workspace_missing`）→ queued 先允许原 Agent.Send 创建任务工作区，提前冻结主分支基线；后续检查仍要求真实归属／租约。
- 新夹具完整服务链先失败（停在 integrating）→ 追溯真实 Agent 租约仅接受 task/project_task，修正 integration 阶段错误的 `integration` 参数；不是放宽租约。
- `TestDeliveryPolicyReadOverSocketAndPreflightWireContract` 分别在缺只读映射／大写字段失败 → 增加固定 policy GET 查询及 lowerCamelCase 预检字段；拒绝额外／重复查询参数。
- `TestDeliveryCandidateWritesRequirePause` 在七个活动阶段先失败 → 普通消息／resume 必须先暂停交付，不允许并发改候选；paused 后仍按旧会话规则接回。
- `GOTOOLCHAIN=local go -C server test ./internal/delivery/testdata/webfixture -count=1 -v`：正常、人工效果验收、修复一次成功、3 次额度停止、主分支 CI 修复五场景通过。核对真实 bare 的 main SHA、任务／main push 数量及不伪造任务 completed；这些是假模型／假 Actions，不是 GitHub 联调。
- 最终 Go 全量／race／vet／mod verify／build、六条真实服务夹具的 race、两次信号烟测、关键崩溃与新回归 `-count=10` 均通过。前端27/27、完整 Chromium42/42通过，新截图仅合成项目。独立审查未通过且仍有阻塞，不据此声明 Task 13–14 整体验收完成；详见验证与审查裁决。没有启动或重启 7331，也未使用任何真实远程凭据。

## 2026-10-30：C2 远程执行边界修复与独立复验

- 当前主控在原 local checkout 串行修复，Workspace `wks_9e9b9697cff7f277`、main、HEAD `d916c2ae08d6a6e3f0293667f6d92ad229cf41f7`、git-dir `.git`／index `.git/index`；保留既有混合改动。未新建空间／worktree／分支，未提交／推送／部署。
- 新增 `gitops/remote_boundary_test.go`：真实 production 子进程与临时 Git 仓库，仅由本地 shim 截获远程命令、不连接外网；合成无关服务 token 原先继承、冻结远程配置被绕过及越界 ref 均先失败。补多行 URL 伪造记录的独立失败复现。
- 修复 `command.go`／`remote.go`：远程子进程按环境白名单继承，resolver 明确授权值另外追加；有仓库标识的自动交付目标每次真实远程命令前重验 github 地址、唯一冻结 URL、pushurl、双向 rewrite 与 helper。配置改用 NUL 记录，避免多行值伪造远程。Observe／Fetch／Push 限冻结主分支与任务分支，拒绝后报告未启动进程；手动目标沿用其原审核路径。旧只读权限测试夹具补齐明确 TaskRef，保留实际远程拒绝语义。
- 定向 RED→GREEN 与多行 URL RED→GREEN 均已留存；最终 Git／凭据两包完整回归退出 0，定向 Git race 退出 0。交付包在 `regression2.log` 完整回归退出 0。首轮沙箱监听失败及恢复测试瞬态失败、旧夹具失败均保留，未以一次后续通过宣称所有间歇已根除；没有全仓测试或真实远程联调结论。
- 唯一证据目录 `~/.local/state/agent-artifacts/kaiwu/2026-10-30-delivery-remote/`：四个文件的 `source.sha256`、8 份原始日志（含失败）。源码已冻结；按最新功能验收 profile 原配置复用独立验收者 `afcd88fc-aae5-4501-88cd-2d4aa7ab3879`，只读核关键路径与三包回归，结果待回执。
- 此批只处理 C2 的生产执行边界；I1 的 SSH／认证完整接线、C3 保护定义及其他交付缺口继续开放。worker 不装配／不启用，实际服务不更新，不能据此放行真实上传。
- 独立首轮三包回归退出 0，但凭据隔离整体不通过：仓库 `http.sslVerify=false`＋`http.proxy` 的本地 MITM 探针可收到合成凭据；全局 `sslVerify=true` 无法覆盖 URL 特定值。`verify.md`、`probe-mitm-repo-config.log` 和第一轮源码快照完整保留，非真实 token／GitHub 联调。
- 第二轮修复新增公共 HTTP 传输覆盖检查，拒绝仓库及其生效 include 中全部 `http.*`（含 URL 特定项、CA、代理、重定向和 Header），同时接入手动审核与真实手动远程入口。仓库自定义 CA／代理会明确阻塞，须移除并审核服务端受控传输设置，不自动信任。另以真实 `git credential fill` 复现仓库 `core.askPass` 脚本会运行，`Args` 清空该项后不再执行。
- 新增 HTTP 10 种配置 × 自动／手动真实 production 入口拒绝，以及 10 种手动审核拒绝；每个手动场景先有合法控制、include 场景核配置确已生效。HTTP／askpass 先失败再通过，最终定向 race、vet、diff 检查退出 0。第二轮 5 文件哈希保存在同目录 `source-r2.sha256`，`http-red/green/race/vet.log` 保留；独立角色同端复验增量及最终三包，暂不关闭 C2。
- 第二轮独立复验完成：F1 HTTP 传输覆盖和仓库 askpass 执行问题在限定范围关闭，C2 远程执行边界切片通过。最终 Git／交付／凭据三包回归与同三包 vet 退出 0，独立真实 Git 探针确认禁用 askpass 后受控 helper 仍可提供合成凭据。报告 `verify-r2.md` SHA256 `5525e2437da04d99d9c7484ef684032013d136a50dc7d9e5c3aa8631b2eec681`；主控已读报告并核 `source-r2.sha256` 5 项全部匹配。报告一处代码文件名误写，实际 `rejectHTTPTransportOverrides` 位于 `server/internal/gitops/remote.go:135`，原报告未覆盖。
- 主控补查手动调用层 `go -C server test ./internal/agent -run 'Push|Merge' -count=1` 退出 0（14.767s），`agent-regression.log` 与 `command-status.txt` 已保存。没有新增实现变更；无需重复 UI／全仓测试。独立验收 Agent idle、activeTurn=null、无待权限／待写工具；本轮探针已释放，Go 临时资源随测试结束，未新增常驻服务。
- 结论仅为 Linux 隔离场景的 C2 执行边界切片通过；配置核查与子进程启动间的同用户改动窗口、I1 SSH／认证完整接线、C3 及其余交付项仍开放。没有真实 GitHub／模型／网络授权或验收，不关闭整体安全门禁、不启用 worker、不部署。

## 2026-10-30 续接：I1 证据收口与 C3 workflow 冻结首片

### 归属与边界

- 用户要求继续未完成任务。主控 `5723a5b9-5cd4-4a39-bc16-bb661c08c899` 在已核实的当前 Workspace `wks_ad57499790f82e3e`／原 cwd／main 直接串行实现；HEAD 仍为 `d916c2ae08d6a6e3f0293667f6d92ad229cf41f7`，git-dir/index 为 `.git`／`.git/index`。共享 local checkout 不隔离，继承混合成果、旧截图与未知文件均保留。未建空间、分支或 worktree，未提交、推送、部署或调用真实业务模型。
- 最新 profiles 全部 notes 已核；独立功能验收使用配置 `pi/magpie/group/deepseek-flash-4.1`（未指定不存在的 thinking 设置），在同一 Workspace 创建 `e9d11055-879c-49db-87bb-a10660caad59`。仅只读核查、必要测试与写证据，不派生、不改源码；SSH 与随后 C3 首片由同一验收者串行验收。独立完成后已核实际运行模型为同配置 `magpie/group/deepseek-flash-4.1`，effective thinking=off、无待权限；本阶段分项消耗未知，累计日志零价不作实付或免费结论。

### I1 SSH 收尾

- 原 `2026-10-30-delivery-ssh/verify.md` 已证明 SSH 接线基线独立通过，包括真实 OpenSSH 的临时回环 sshd／known_hosts／agent／key、Git→SSH 链路。原报告与 source.sha256 完整保留，此前 README／路线图尚未消费该结果。
- 本轮按原两项观察补回归：含 `%` 的 key 路径明确拒绝，避免 `-i` 的 `%%` 并非实际可用转义；普通空格、单引号、shell 表达式仍作为单个字面参数。最终远程入口直接规范化至 GitHub HTTPS／SSH 白名单，拒绝 file／ext／外站及不安全 HTTP，不只依赖上层构造者。
- 真实生产 group-fate 测试以测试专属 git shim 把合成白名单 URL 映射到临时 bare，仍实际执行 Git／租约／进程组，不再为测试放行 file URL。原 helper 401 测试直接使用 Args＋remoteEnv 访问回环，避免生产新 URL 门禁让该测试空过。
- 两项 RED 已在 `followup-red.log` 留存；首次回归遇 SSH fixture 关闭 stdin 导致 SIGPIPE，`followup-green.log` 保留；补 fixture 消费 stdin 后聚焦 3 次及凭据／Git／app 三包最终回归退出 0。最终 8 文件清单 `followup-source.sha256`。独立增量复验通过：三包回归、凭据／Git race、vet 及 5 类 overlay 断言（拒绝前无进程／合法路径仍可用等）通过；报告同目录 `followup-verify.md`，主控已实读核对，不覆盖原已通过报告。
- 明确限制：百分号 key 路径不再假装可用，需用户将受控密钥放在不含百分号的路径；不自动复制或改写密钥。未访问个人凭据、真实 GitHub 或 7331。

### C3 第一子片：实际 workflow 定义

- 不重开已审核设计。已实现实际 `.github` Git 定义指纹（包含 workflows、本地 actions、路径、blob、mode 及 symlink Git 项）：在原会话派发前持久保存到已有 `Delivery.ProtectedFingerprint`；本地／集成检查前后比对 tree；精确候选／主提交在后续阶段及恢复／CI 接纳前重核。旧活动周期缺少基线时明确阻塞，Store 禁止同周期清空或替换该基线。
- 保护整个 `.github` 是保守范围，issue 模板等变化也要求人工审核；没有新 UI、策略字段或数据库迁移。显式检查脚本配置／定义冻结及任意测试语义仍未实现，**不关闭完整 C3**。
- RED：`red.log`（派发前未持久化）和 `store-red.log`（基线可改写）。最终新回归覆盖编辑／删除／新增／已提交／mode／软链早拒绝、检查时改写不进入修复、精确提交／旧周期门禁、Store 不可重基；`green2.log` 退出 0。
- 最终 `go test ./internal/delivery ./internal/workbench -count=1`、两包新路径定向 race、两包 vet、真实 Go＋临时 SQLite／bare＋假 pi／Actions 的 webfixture 服务链及 server build 退出 0，日志 `regression2.log`／`race.log`／`vet.log`／`service-chain.log`／`build.log`；最终 8 文件 `source.sha256`。
- 首次 `regression.log` 保留 `TestDeliveryCrashAfterMainPushBeforeReceipt/external_move_parks_the_cycle_instead_of_guessing`：期望 needs_check，实际 blocked/recovery_uncertain。撤除本轮 C3 的 overlay 聚焦对照 5 次通过，不能据此证明原失败无关或间歇已消除；随后因新增 Store 保护代码执行的最终两包回归通过，不抹去首轮失败。既有恢复与 worker 调度屏障仍属开放问题。
- 源码已停写，独立验收中。主控另提出本地 refs/replace 可能影响 ls-tree 精确对象读取的待证风险，交同验收者一次定点核查，不把静态假设写成已证实问题；如证实，由原主控修复并聚焦复验。
- 唯一证据目录：SSH 复用 `~/.local/state/agent-artifacts/kaiwu/2026-10-30-delivery-ssh/`；C3 首片 `~/.local/state/agent-artifacts/kaiwu/2026-10-30-delivery-definitions/`。普通实现 1 轮，独立验收尚未裁决；当前无全仓 Go／浏览器全量、真实模型、GitHub 或部署通过结论。临时 overlay／构建二进制已清理，未启动常驻服务。

### 独立首轮结果与 R1 聚焦修复

- SSH 收尾通过，具名报告 `delivery-ssh/followup-verify.md`；C3 首片不通过（原报告称“受阻”，实际为已复现的安全缺陷）：同目录 `verify.md` 与两份 replace 探针日志已留存。原基线、工作 tree、基线不可重写等已验证项保留，不把独立报告结束当验收通过。
- 真实根因：Git 默认应用本地 `refs/replace`，可把 weak commit／tree／subtree 的读取替换成旧定义，但 replace refs 不随推送上传，远端仍收到真实削弱对象。独立临时 worktree→bare 探针证实该差异；同用户还可在其他 Git 读取路径使用 replace，本轮不扩称全仓 Git 审阅安全已修复。
- 原主控 R1 仅改 `workflow_protection.go` 与其测试：fingerprint 的 Git 命令显式加全局 `--no-replace-objects`，读取实际提交及全部子树对象。新真实 Git 回归覆盖 commit／root tree／subtree／blob replacement，共享项目与任务两目录、原始 commit／tree、精确 push SHA 门禁，以及真实 worker 拒绝且无 push／repair。blob replacement 原本即被树条目 OID 检查拒绝，未误称修前四项都可绕过。
- RED `replace-red.log` 中 commit／tree／subtree 明确失败；修后全部 workflow 测试及定向 race／vet 退出 0（`replace-green.log`／`replace-race.log`／`replace-vet.log`）。`source.sha256` 保留首轮，最终新清单 `source-r1.sha256` 的 8 项仅两项变化。未改全局 Git 环境／Args、旧崩溃测试或计时断言，不声称恢复间歇已消除。
- 首轮独立还保留一次 `recovery_test.go:68 landed_push` 的 1ms deadline 差异；全包隔离对照均通过不能证明完全无关，不据此说稳定全绿。此项与已有 I12 恢复／worker 调度屏障一起继续开放，未为凑绿放宽测试。
- R1 独立聚焦复验已通过，报告 `verify-r1.md` SHA256 `39ea6196fc3856baa543e80f2f18340bc7e39981b6d30f0325f8d43bb5ede545`；原绕过探针转绿，四类 replace 探针先证明 replacement 实际生效，再证明门禁始终读取真实 weak 对象。机器级 A/B 在仅去掉 flag 时推进至 awaiting_acceptance/push=1，当前实现 3/3 以 `protected_definitions_changed` 拒绝、push=0／repairs=0；新缺的 worker BlockCode 精确断言由独立探针覆盖。定向测试、race、vet 均通过；两份最终源码清单与实际全部匹配。
- 主控证据纠正：独立原报告“单独 commit 替换同时端到端绕过候选”不准确；R1 报告已具名更正，真实可利用的流水线路径是 tree／subtree 级替换。机器级 `newMachine` 的 remoteState 是内存 transport，awaiting_acceptance 前 push=1 对应任务分支，**不是已实际写入远端主分支**；报告该句夸大不采纳。原 shell 临时 bare 探针可另证真实 Git 的 replacement 不随推送传输，但不等同于产品主分支闭环或真实 GitHub 验收。主控已实读 `r1_probe2_test.go` 与 RED/GREEN 日志核对，不以总结替代原始证据。
- C3 workflow 定义冻结第一子片与 SSH 收尾均为限定通过；完整 C3／整体交付仍未完成。其他 Git 审阅／历史路径是否受 replace 影响仍未审，不扩大为全局 Git 安全已修复。
- 资源收尾：四份必要原始探针已单份保存为 `delivery-definitions/probes/`，`probes.sha256` 核验通过；其余可丢弃 overlay／中间副本所在 `/tmp/kaiwu-fu-verify` 已定点清理，确认无该路径进程引用、无残留服务。独立 Agent 已经官方 `kill_agent` 关闭并核 status=closed、无待权限；原生会话持久路径与历史保留，未归档 Workspace、未结束主会话。无可靠净内存采样，不报告释放量。

### 2026-10-30 C3 显式检查定义：限定独立验收通过

- 用户“继续”沿用已批准主线；主控连续实现，未新建空间／分支／worktree。HEAD 仍为 `d916c2ae08d6a6e3f0293667f6d92ad229cf41f7`，共享 main、`.git/index`；原约 76 项混合成果保留。本次新增 4 个代码／测试文件，并局部修改既有领域类型、策略校验、worker 门禁、设置向导和既有测试入口，未触及 7331。
- Ruling：每项检查新增可选 `definitionPaths`，明确相对**项目根目录**（不随检查 Dir），每项最多 32、总计 256 个普通跟踪文件；去重排序，不支持目录、symlink、submodule、Git 内部路径、父级／绝对路径或非规范路径。始终保护 `.github`。不猜任意 shell／npm／make 的传递依赖，调用者须显式列全；留空仅有原 workflow 保护，不能声称已保护本地脚本。旧无附加路径周期沿用原 baseline，不静默为活动周期增加保护。
- 派发前用主分支实际对象读取冻结，检查前后与 candidate／main 精确 SHA 门禁均纳入声明的路径／mode／blob OID；配置列表进入授权 fingerprint。缺失文件或读不到普通文件即阻止派发；检查期间制造特殊文件而导致最终 tree 为空时，再独立捕获 workspace，无法核查即停机，不能进入自动修复。明确仅是门禁定义防弱化，不是宿主／检查进程沙箱或依赖闭包证明。
- RED：`checks-red.log` 证明编辑后的弱脚本原本推进到 awaiting_acceptance；`checks-web-red.log` 证明旧表单往返丢失保护列表；`checks-post-red.log` 证明检查运行中换成 symlink 时未走保护定义错误。均保留。
- GREEN：`checks-target.log` 通过脚本编辑／已提交／mode／删除／symlink／subtree replace、缺失与目录派发前阻止、期间改写／symlink、正常功能、精确候选／主分支门禁以及原 workflow 回归；`checks-policy-bounds.log` 通过路径校验、列表上下界和去重。完整 delivery＋workbench 本轮通过（`checks-regression.log`），相关 race／vet 通过；不能据此关闭历史 I12 恢复间歇。
- Web：`checks-web-regression.log` npm test、`checks-web-build.log` vue-tsc＋Vite 构建通过。真实 Go 隔离 fixture＋Chromium 的设置向导填写／保存／Store 读回／弹窗重开通过（不是服务重启或数据库关闭重开）（`checks-browser.log`，1 个组合场景）；1440／360px 截图在 `checks-browser/`，主控已实际查看手机图。仅 loopback 测试服务，无真实模型或 GitHub，结束后 17332 无监听。测试不覆盖已部署 7331。
- 源码清单 `checks-source.sha256`（19 项）已冻结，构建入口／当前 assets 另见 `checks-build.sha256`。原 `source-r1.sha256` 保留为历史，不再是本次源码清单；独立验收只用新 manifest 和未变证据，不重复 SSH。全仓／CI 未运行。独立验收使用同主控模型 Pi 原生 child（不是 Paseo Tab）：run `be73eb3e-253d-4f1a-987e-c799bccc4c7c`，`openai-codex/gpt-6.1-sol:high`；只读仓库、源码未变化。已通过，报告 `checks-verify.md` SHA256 `0fa8f42789c6b8c5242ff8ed807105febc91dc4f326dbc77d66b7ac1a42985a0`。
- 独立验收自行通过限定 Go／前端测试及 actual Git 对象／字面特殊文件名／legacy baseline／真实 Recover／集成检查前后 overlay 探针，三项阶段探针均 blocked/protected_definitions_changed、push=0／repair=0、主 HEAD 不变；原始探针构造失败及修正后日志均保留，不能当产品缺陷红绿。复用完整回归、race／vet／Web 构建和实际 Go 浏览器交互证据，并实际读图核对 1440／360px；无新增 Critical／Important 或视觉阻塞，不称真实崩溃重启或远程主分支端到端已验收。
- 主控已实读具名报告并核源码 19／19、构建 3／3、`checks-verify-evidence.sha256` 全部匹配，dirty 仍 85 项、diff whitespace 检查通过。原失败、R1 与 SSH 报告保留。C3 在 workflow＋显式声明文件的机械保护范围内闭合；不证明未声明依赖或任意检查语义，既有恢复调度间歇和整体安全门禁仍开放。
- 收尾：独立原始探针与 overlay 已保存并逐份 cmp 匹配，测试配置单份留存 `checks-playwright.config.mjs`，本任务 `/tmp/kaiwu-c3-checks-verify`、临时测试配置和重复测试文本已定点清理。17332 无监听，归属路径无存活进程。原生 run status 已核 State=complete、Process terminal=observed、Active async capacity=0；保留工具托管的 retention 目录与原生历史，不删除会话或停止主控。无净内存数据，不报告释放量。

### 2026-10-30 恢复调度／push 窗口：自测及初验过程（最终 R1 见后）

- 编号纠正：先前台账把恢复／external_move 间歇称为“I12-family”，原具名审查实际 **I11 为恢复前屏障及非 push intent／写权重建，I12 为暂停／撤销后启动副作用**。本批只处理 I11 的调度屏障、push 核查一致性及对应间歇；完整 I11、I12 均不能整体关闭。
- 根因已用确定性 RED 复现，不是靠扩大时限或放宽状态断言：`New` 后 `Tick` 可立即执行；worker pending-intent 将外部 main 移动写成 blocked 而 `Recover` 为 needs_check；正常 push receipt 使用当前时间、恢复补记使用 intent 时间，输入同一仍有 1ms／注入 7ms 差异；恢复和 Tick 可在远程观察上重叠。另补 RED 证明只读第一页遗漏第 100 项后 running intent、多条恢复复用旧 CAS 版本、CAS 冲突被当“跳过成功”并打开初始屏障、门禁 block 的 CAS 失败被吞。
- 实现：`New` 保持 dormant，首轮成功 `Recover` 后才开启 worker；Recovery／事件和定时 scan／显式 Tick 共用可取消等待的 runGate，Close 等公共 pass 收尾。初始冲突／失败不放行；每次成功补记沿用返回新版本。完整步骤历史有界分页（最多 64×100，不能完整核查即 fail closed，不将“没读到”当“没 intent”）；worker 观察不确定 push 与 Recover 统一为 needs_check＋uncertain 回执，读／补记失败不允许重发。正常回执与恢复都以同一持久 intent.StartedAt 计 CI 截止（保守包含 push 耗时，不重新赠送预算）。
- 旧模拟崩溃测试的 running intent 只证明“已落意图”，不证明“已到实际崩溃边界”；清除 hook 时 live worker 还可能写 receipt／完成 CI。修为在 dormant 新 Manager 上顺序驱动真实阶段到指定 hook，明确验证真实测试 transport 写入次数，主 CI 保持 in-progress 后再恢复；不把 succeeded 也算断言通过。真实 Linux SIGKILL 测试前两窗口改由临时 bare remote transport 在实际 push 前／命令返回后杀本进程，回执后窗口保持主 CI 等待，重启前核 main ref 的实际 SHA；不靠看见 running intent 猜 push 已发生。
- 证据根 `~/.local/state/agent-artifacts/kaiwu/2026-10-30-delivery-recovery/`：`red2.log`（首个 `red.log` 为测试包装类型编译错误，保留但不冒充产品 RED）、`concurrency-red.log`、`history-red.log`、`conflict-red.log`、`gate-cas-red.log` 及各 GREEN。首次修屏障后旧测试提前到 succeeded 的 `original-windows.log` 保留；精确窗口修正后 `windows-green.log` 5 次、`repeat.log`（窗口／屏障／竞争／deadline 组合）5 次通过。
- 回归：`regression.log` delivery／workbench／app 全包通过（70.987／30.581／0.576s）；`race.log` 限定恢复及真实 SIGKILL 路径 race 通过，`vet.log` 三包通过。误用不存在 `./cmd/kaiwu` 的 `build.log` 保留；正确 `./cmd/kaiwu-server` 构建见 `build2.log`。无 Web 改动，不重做 C3／SSH 验收；全仓／Actions、真实 GitHub／模型未运行。
- HEAD 仍 `d916c2ae08d6a6e3f0293667f6d92ad229cf41f7`，共享 main / `.git/index`，dirty 90 项（原 85 项保留）；本批新增 `recovery_serialization_test.go`，改 4 个生产文件与相关恢复测试／fixture。15 项 `source.sha256` 已冻结。生产调用端本已在装配后显式调用 Recover，测试 fixture 补同一启动契约；不启用 worker 或改动 7331。无本批独立裁决前不宣布修复验收通过。复用前批只读验收者／同一模型，原生 resume 新 run `66cc9eb0-6a05-4bd3-bc7a-634d62b6c8d0` 已接收，报告另绑 `delivery-recovery/verify.md`，旧 C3 报告保留；源码停写，等原生事件，不轮询。

### I11 push 子片：独立 F1 修复与 R1 复验通过

- 初验 `delivery-recovery/verify.md` 为 **FAIL／1 Important**，原报告、`source.sha256`、独立两次产品失败及旧证据清单原样保留。F1：live `resolvePendingIntent` 在首条同 kind not_started／observed 后返回，后续 uncertain 可被跳过；第一条未开始＋第二条未知时实际 fixture stage 仍记 main_ci／新 push=1，而 Recover 为 needs_check／新 push=0。未证明此特定历史由全新普通 UI 自然生成，不冒称真实 GitHub 写入，也不称问题由本批新引入。
- 父端先加 RED：`f1-red.log`，task/main × live/recovery 对照；未知组合仍精确要求 needs_check＋recovery_uncertain＋对应 uncertain 回执、push/repair=0。补 all-observed、all-unstarted、observed／unstarted 顺序互换，暴露重复 observed 补记还会延长 CI 起点，均修复而非降低断言。
- 修复仅再改 `recovery.go` 与 `recovery_serialization_test.go`：live 先读取、分类完整同类集合；未知压过已知，已知回执沿用 fresh CAS、但不提前推进周期，未知逐条 parking；正常 batch 保留最早 observed 的 running anchor，最后一个 CAS 原子落该回执和 CI 阶段，避免中途崩溃后“已关闭所有记录却还在 pushing”造成重推。全未开始要先落 intent 后再从 fresh stage 重新核 gate，不沿用旧版本。恢复重复 observed 不延长同一 CI 阶段预算；已持久 needs_check／recovery_uncertain 不因其他记录在后续 Recover 变得可观察而静默恢复。没有扩大为暂停／撤销窗口修复。
- 最终覆盖：7 种双 intent 组合 × task/main × live/recovery 共 28 子例；另 4 项在真实 Store 最终 phase CAS 前制造版本冲突，核 metadata receipt 已落、anchor 仍 running、phase 仍 pushing，之后 live／Recover 都只补记、无新 push／预算重置。证据 `f1-green.log`（首批24项）、`f1-anchor.log`、`f1-independent-probe-green.log`（父端原样重跑原独立 overlay，非另一次独立裁决）。多未知扩展的 `f1-multi-uncertain-red.log`、`...-green.log` 失败均保留：后者含测试把 external inject 计数错算新 push、以及 startup 只先停车首条的过强回执断言；最终明确 live 全部停车、startup 先停车任一未知并保留人工核查，注入本身不算服务 push，`...-final.log` 四项通过。最初 F1“第一条未开始／后续未知”精确断言未放宽。
- 最终状态 `source-r1b.sha256` 15/15 匹配（相对初验仅上述2项变化），`source-r1.sha256` 为中间草稿保留、不用于最终裁决。新证据 `f1-final-regression.log` delivery 全包 96.635s PASS；`f1-final-race.log` 含完整新 batch／CAS 及既有恢复／SIGKILL 路径 67.278s PASS；`f1-final-vet.log` 三包 PASS；`f1-final-build.log` 正确服务目标构建 PASS，均留 exit=0。未改 workbench／app，复用前批完整两包回归；未改 Web、SSH、C3，不重跑已验收路径。仍未全仓／Actions／真实 GitHub／模型／7331验证，未提交部署，worker 禁用。

- **最终裁决：I11 恢复调度／push 核查子片独立 R1 PASS，原 Important F1 限定关闭，无新增 Critical／Important。** 复用验收者原生 run `8f6a9b63-467a-4d7f-97a4-b3effc09ebe0`，报告 `delivery-recovery/verify-r1.md` SHA256 `c1849f1d455250beeb2c6583887e40f0a5778fde13b54adc88f2eb845e66dc44`；原 FAIL 报告 SHA256 `a34fcfefc13bf2e06867ceea7393c5281680f86700fdf8f4978288e5681ed66e` 未变。验收者自行原样重跑失败 probe（1.982s），28 batch＋4 anchor＋2 deadline（23.375s），另独立 6 项三记录／最早 anchor 非首行／首 metadata CAS 和 final CAS 探针（4.673s）全通过；不冒称重跑全包／race／真实 bare 重启。
- 原会话实读报告并核最终 `source-r1b.sha256` 15/15、`verify-r1-evidence.sha256` 全匹配；HEAD／dirty90／无 staged 保持，diff whitespace 检查通过。最终源码与清单 SHA256 `6aa4a35f2cdfa62dbd7d8413fdea4636eae7c2bcf635630e05e9c116b715d4d2` 对应裁决；旧失败和中间草稿都保留，不能当作最终版本。只收口已证明的根因及子片，不保证所有恢复间歇永不再现。
- 资源已收尾：原生最新 run 为 complete，**Process terminal: observed、active async capacity=0**；会话历史和 retention 管理目录均保留，不发冗余 stop、不强杀 Pi。两份临时 probe／overlay 已逐文件与持久证据比较一致，删除本批 `/tmp/kaiwu-recovery-verify`、`/tmp/kaiwu-recovery-r1-verify` 以及校验过的 `/tmp/kaiwu-recovery-build.vopufj` 二进制／目录；未发现这些路径的活进程，无新常驻服务，`cleanup.log` 记录 exit=0。未清全局缓存或未知材料；7331、个人凭据及真实 GitHub／模型均未触及，未提交／部署。

### 下一步

- SSH、workflow 及显式检查定义／设置三项限定范围已收口，证据已保存；后续直接复用未变结论，不重复验收，不把本地交付切片当全仓稳定通过。
- 本批 I11 调度／push 窗口子片已独立通过；随后仍优先 I11 非 push 写权／intent 与 I12 暂停撤销窗口，再继续其余 CI／集成审查项。整体安全门禁不通过，worker 不启用。子 Agent 真实派发仍缺执行写权限决定，不用只读概览冒充真实编排。PC／Android 与团队协作／通用可配置编排方向保留，当前不扩大并行实现。

### 本批提交交付（用户“提交代码”）

- 已核 GitHub `SatoriTours/kaiwu` 实际默认 `main`，远端基线 `4f36acc`；本地 `d916c2a` 为已获另一明确“提交代码”授权的预览修复（原验证记录第175行），尚未推送且旧部署失败。该已授权祖先随本主线接受最终全量门禁，不绕过旧失败；不收编子 Agent 概览、schema3、旧截图或测试遗留。
- 本任务关联：当前 `wks_ad57499790f82e3e` 与历史 C2 `wks_9e9b9697cff7f277` 均指向此共享 cwd／main／`.git`、`.git/index`，Git 实际仅该一个 checkout；独立验收均零源码写入、已退出。自动交付四批成果在此统一核对，证据仍按前述根目录引用。共享目录其他任务及完整 I11／I12 未完，不能归档任何关联空间。
- 仓库无适用 GitHub Actions 工作流；既有 reference-transaction 钩子对最终提交的干净 archive 运行完整 Go／前端／Chromium门禁并按既有配置部署，不能直接用混合工作目录构建。钩子不改认证／保护，不跳测试；worker 不启用。本次命令现授权提交／推送／既有部署，但不授权业务模型或任务自动 GitHub 写入联调。最终 SHA、全量门禁及实际入口待核，当前不报交付完成。
