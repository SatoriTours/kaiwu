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
