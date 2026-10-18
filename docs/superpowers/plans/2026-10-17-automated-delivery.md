# 固定开发交付与 Actions 自动修复实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用户按项目配置开发功能，系统在独立 worktree 完成检查、提交、推送、串行集成、GitHub Actions 观察和有限自动修复，只有实际门禁通过才标记技术交付成功。

**Architecture:** Go `delivery` 固定状态机协调现有原任务 pi、`gitops`、新增只读 `githubactions` 适配及 SQLite 交付记录；外部操作不在 Store 锁内执行。交付与功能验收、Git 状态分开，以精确 tree/SHA、run attempt 和持久步骤意图驱动，重启先核查不重放。

**Tech Stack:** Go 1.25.0；现有 net/http、chi v5.2.3、coder/websocket v1.8.15、modernc SQLite v1.46.1；Vue 3＋TypeScript＋Vite；现有 Node 原生测试和 Playwright。远程 HTTP 使用 Go 标准库，不新增生产 Node/TypeScript 宿主或 pi 扩展。

**Spec:** [已获用户审核的书面规格](../specs/2026-10-17-automated-delivery-design.md)。2026-10-17 用户在规格交付后回复“ok”；本计划及当前会话串行实现＋最终独立验收方式已获用户“ok”确认。Task 1–3 已实现并自测（Task 3 的官方文档抓取阻塞已解除，契约见 `server/internal/githubactions/testdata/contracts.md`）；Task 4–14 实施中，见[执行记录](../../development/web-automated-delivery-ledger.md)。

## Global Constraints

- 每任务沿用 `kaiwu/task-<taskID>`、`<DataDir>/worktrees/<taskID>/tree`、owner 和原 pi session；同工作区单写入者，不另建第二执行会话。
- 一条固定流程，项目配置一次、周期保存快照；新旧项目默认 `enabled=false`，旧结果不自动上传代码。
- 项目授权绑定唯一 GitHub.com repository ID、fetch/push URL、目标主分支及任务分支范围；旧手动合并授权不升级为自动 Push。
- 默认 `requireAcceptance=true`、最多 3 次修复调用、活动累计 60 分钟；人工等待／调度排队不计活动时间；每本地命令默认 5 分钟。
- CI 触发窗口 5 分钟，每阶段排队＋运行 30 分钟；查询约 10 秒＋抖动，API 请求 15 秒；重启不重置截止时间或预算。
- 两阶段各至少一个必需 workflow、本地至少一项必需检查；只接受对应 repo/ref/SHA/event=`push`/workflow/run attempt 的真实成功，不把缺失、skipped、neutral 或旧结果标绿。
- 日志每阶段读取最多 8 MiB、模型脱敏摘要最多 64 KiB、持久阶段摘要最多 256 KiB；快照只放摘要和最近 20 条步骤，历史分页。
- GitHub 凭据不进入 pi、检查、预览或浏览器；不开任意 credential helper、hooks、remote helper；不通过 token URL／argv 泄漏凭据。
- 不 clone、stash、reset、rebase、强推、自动解决合并冲突、自动创建 PR／Tag／Release／部署或删除 worktree；主分支失败隔离后续集成。
- 仅有明确授权周期可自动发起修复轮次；settled、RPC 接受、恢复连接、进程退出都不等于整体交付成功。
- 本地执行保护当前限 Linux；PC Windows/macOS/Linux 与 Android 产品目标保留，不声称本批完成跨平台执行验收。
- 普通开发／结果认可／实际 Git／技术交付状态分开；专题列不变，右侧原聊天保留，无源码或 diff 展示。
- 不创建 Paseo Workspace/worktree，不派发编码 Agent，不重启 7331，除非相应执行已获许可；自动化测试只用临时仓库、fake pi、fake GitHub 与回环测试服务。
- 本计划的检查点替代技能模板中的自动 Commit；没有本批明确授权不得对开悟仓库提交、合并或推送。真实模型和 GitHub/Actions 联调另行说明费用与副作用并获准。

## Review Focus

1. **同远程不同 clone／重复项目入口**：应共享集成隔离，不能因 common dir 不同同时推主分支；Task 10 的 `TestDeliveryRemoteLaneAcrossClones`。
2. **任务检查经过后、提交前发生文件／模式／符号链接变化**：旧证据必须失效，不能提交未经检查内容；Task 6 的 `TestDeliveryCandidateChangedAfterCheck`。
3. **API 304、重跑 attempt、分页及晚到成功交叉**：只能推动当前候选的最新真实运行，不把缓存当成新成功；Task 3 的 `TestActionsCacheAttemptAndLatePages`。
4. **主分支已推送但服务在写回执前退出**：重启要找回原 SHA 和隔离槽，不重复 Push／修复调用；Task 11 的 `TestDeliveryCrashAfterMainPushBeforeReceipt`。
5. **效果验收后 CI 修复改变源码**：技术配置不得伪造旧验收或抹掉历史认可，新的候选要重验；Task 9 的 `TestDeliveryRepairInvalidatesAcceptance`。

---

## 0. 执行前现状、文件归属与依赖

本计划是一个相互依赖的产品闭环，不拆为可独立发布的 Git／CI／模型三个产品。按基础、执行、闭环、界面四层分步验收；基础层交付不等于自动交付已可用。

当前实际业务 HTTP／Socket 路由在 `server/internal/webapi/`，不是健康接口所在的 `httpapi/`。SQLite 当前 `user_version=1`，主要状态为 JSON，`operations` 和登录表独立存在；新增表与现有状态必须同事务发布。`agent.finishLocked` 在 Agent 锁内保存轮次，不能直接回调一个反向获取 Agent 锁的交付 worker。

当前 checkout 另有聊天滚动相关未提交修改：`apps/web/src/App.vue`、`ChatPanel.vue`、`workbench.ts`、`apps/web/tests/browser.spec.ts`、`apps/web/README.md`、README／路线图等。执行前重新核对，保留这些成果；共享文件串行修改，不把它们当成本计划改动或擅自回退。计划阶段不启动子 Agent，因此不读取／猜测未来执行用 profile。

另外，现有“任务内改模型下一轮生效”是产品规则，**实际代码目前只允许未启动空会话补选**；Task 8 补齐自动修复所需的下一轮模型应用与失败保护，不将旧验证记录当作运行中切换已实现。

### 文件地图（均为计划，未创建业务文件）

| 归属 | 新增／修改 | 职责 |
| --- | --- | --- |
| workbench | 新增 `delivery_types.go`、`delivery_policy.go`、`delivery_store.go`、`delivery_steps.go`、`delivery_queue.go`、`delivery_repair.go`、`delivery_sync.go`、`migration.go` 及同名测试 | 领域契约、迁移、事务、授权／预算／队列和摘要 |
| workbench | 修改 `store.go`、`types.go`、`commands.go`、`execution.go`、`report.go`、`sync_test.go` | 原子开始周期、轮次和同步兼容；仅针对性改动 |
| auth | 新增 `server/internal/githubauth/{credentials.go,askpass.go,ssh.go}` 及测试；修改 config／命令入口 | 服务私有凭据和受控 Git 桥 |
| Actions | 新增 `server/internal/githubactions/{client.go,workflows.go,runs.go,jobs.go,logs.go,gate.go}`、测试及合成 `testdata/` | 只读 REST、门禁、日志与退避 |
| gitops | 新增 `remote.go`、`remote_process_linux.go`、`remote_process_other.go`、`candidate.go`、`delivery_merge.go` 及测试；复用现有 command/status/workspace | 唯一目标、精确 refs、受测候选和快进集成 |
| agent | 新增 `delivery_lease.go`、`delivery_repair.go`、`model_next.go` 及测试；修改 manager/events/development/launch/task_preview | 原会话／进程／锁交接与下一轮模型 |
| delivery | 新增 `types.go`、`manager.go`、`policy.go`、`checks.go`、`scheduler.go`、`stages.go`、`integration.go`、`repair.go`、`recovery.go` 及测试 | 固定状态机与生命周期，拆文件而非膨胀原 Manager |
| Web API／装配 | 新增 `webapi/delivery.go` 及测试；修改 handler/socket/snapshot/git、`app/app.go`、对应测试 | 认证命令、状态、业务生命周期 |
| Web UI | 新增 `delivery.ts`、`DeliverySettings.vue`、`DeliveryStatus.vue`、`DeliveryHistory.vue`；修改 App/ChatPanel/KanbanBoard/ProjectMenu/ProgressCard/types/workbench | 一次配置及轻量交付进展，无每步骤必点按钮 |
| 验收 | 新增 `delivery/testdata/webfixture/main.go`、`apps/web/tests/delivery.server.mjs`、`delivery.browser.spec.ts`；修改 playwright config 和假 pi | 真实 Go＋临时 Git＋fake Actions/模型的浏览器闭环 |

### 共享契约与类型所有者

Task 1 在 `workbench` 定义下列领域类型；JSON lowerCamelCase，时长统一整秒，SHA 始终完整字符串，时间使用 UTC RFC3339 的 `time.Time`：

- `DeliveryCheck{ID string; Argv []string; Dir string; TimeoutSeconds int64}`。
- `DeliveryWorkflow{ID int64; Jobs []string}`；`DeliveryRepository{ID int64; Owner, Name, URL, MainBranch string}`。
- `ProjectDeliveryPolicy{ProjectID string; Version uint64; Enabled, RequireAcceptance, AllowCreateMain bool; Repository DeliveryRepository; Checks []DeliveryCheck; TaskWorkflows, MainWorkflows []DeliveryWorkflow; MaxRepairs int; ActiveSeconds, TriggerSeconds, CISeconds int64; GitCredentialRef, APICredentialRef, AuthorizationDigest, Actor string; AuthorizedAt, RevokedAt time.Time}`。
- `Delivery{ID, ProjectID, TaskID, IntentID, Phase string; Cycle, Version uint64; Policy ProjectDeliveryPolicy; CommonDir, CandidateSHA, CandidateTree, Fingerprint, ProtectedFingerprint, BaseMainSHA, LocalMainSHA, RemoteMainSHA, RoundID, AcceptanceToken, AcceptedSHA, BlockCode, BlockReason string; RepairsUsed, RepairsLimit int; ActiveMillis, ActiveLimitSeconds int64; ActiveSince, TriggerDeadline, CIDeadline, StartedAt, FinishedAt time.Time; HoldsTurn bool}`。
- `DeliveryStep{ID, DeliveryID, Kind, InputFingerprint, Phase, BeforeSHA, AfterSHA, Tree, Ref, Summary, BootID string; ProcessGroup, ExitCode int; ExitKnown, Truncated bool; Sequence uint64; StartedAt, FinishedAt time.Time}`，phase 为 `intent/running/observed/uncertain`，模型、Git、检查均先保存 intent；不保存 secret 或可下载临时链接。
- `DeliveryCI{DeliveryID, Stage, Branch, SHA, Event, Status, Conclusion, URL string; RepositoryID, WorkflowID, RunID int64; Attempt int; Jobs map[string]string; ObservedAt time.Time}`。
- `DeliveryRepair{ID, DeliveryID, FailureFingerprint, FailedSHA, Stage, Prompt, Phase, MessageID string; Number int; Profile Profile}`，phase 为 `ready/dispatching/acknowledged/finished/uncertain`。
- `DeliverySummary` 是 `Delivery` 的无配置、无凭据、无 Prompt 投影，含 identity、phase、SHA、预算、阻塞、验收规则、近期 `[]DeliveryStep`、`[]DeliveryCI`；`DeliveryStepPage{Items []DeliveryStep; Next uint64}`。

新增结构只允许补充审计元数据，不擅自换字段名或放宽门禁。内部模块可用自身的 input/config/result 类型；所有跨任务使用的新类型在其生产任务的 Interfaces 中列明。

### 依赖与统一检查点

顺序：1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 14。3／4 可独立研究但共享 checkout 默认不并行写；每项有 RED→GREEN、相邻接口复验和检查点，不自动 Commit。

Go 命令从根目录使用 `GOTOOLCHAIN=local go -C server ...`；实现者每任务结束在验证记录写测试命令／结果和剩余缺口，不能照抄本计划的“预期 PASS”为实际结果。

## Task 1: 领域规则与配置缺省

**Files:** 新增 `server/internal/workbench/delivery_types.go`、`delivery_policy.go`、`delivery_policy_test.go`。

**Interfaces:** 产出上述领域类型；`DefaultDeliveryPolicy(projectID string) ProjectDeliveryPolicy`、`ValidateDeliveryPolicy(p ProjectDeliveryPolicy) error`、`DeliveryActive(phase string) bool`。输入沿用 Project／Task／Profile；缺省时长写明为 3600/300/1800 秒，单命令 300 秒，修复 3 次。

- [x] **Step 1 — RED 测试：** `TestDeliveryPolicyDefaultsAndValidation` 验证 disabled、requireAcceptance、预算缺省；启用后缺仓库 ID／授权／本地检查／任一阶段 workflow、非正 timeout、非法分支、重复 workflow/job、越界相对目录均拒绝。表驱动核对全部 active/terminal 阶段，不把 blocked 误释放隔离槽。
  ```go
  p := DefaultDeliveryPolicy("p")
  if p.Enabled || !p.RequireAcceptance || p.MaxRepairs != 3 || p.ActiveSeconds != 3600 || p.TriggerSeconds != 300 || p.CISeconds != 1800 { t.Fatalf("defaults: %+v", p) }
  p.Enabled = true
  if ValidateDeliveryPolicy(p) == nil { t.Fatal("unconfigured enabled policy accepted") }
  ```
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/workbench -run '^TestDeliveryPolicy'`；预期新类型／函数尚不存在而 FAIL。
- [x] **Step 3 — 最小实现：** 定义契约、normalize 缺省和严格输入校验；timeout/预算不允许通过零值或负数变无限，生产不接受任意 GitHub 主机／任意 URL 协议。声明 clear error codes `delivery_unconfigured/authorization_revoked/needs_check`。
- [x] **Step 4 — GREEN：** 同命令 PASS，再跑现有 `./internal/workbench` 全套，旧项目缺字段仍可读。
- [x] **Step 5 — 检查点：** 记录实际结果、类型差异；不创建空业务目录、不提交。

## Task 2: SQLite 迁移、原子周期与幂等步骤

**Files:** 新增 workbench `migration.go`、`delivery_store.go`、`delivery_steps.go`、`delivery_sync.go` 及测试；修改 `store.go`、`types.go`、`commands.go`、`execution.go`、`report.go`。

**Interfaces:** 消费 Task 1。产出 Store 方法：`SaveDeliveryPolicy(operationID string, expectedVersion uint64, p ProjectDeliveryPolicy) (Receipt,error)`、`DeliveryPolicy(projectID string) (ProjectDeliveryPolicy,error)`、`Delivery(id string) (Delivery,error)`、`ActiveDeliveries() ([]Delivery,error)`、`AdvanceDelivery(id string, expectedVersion uint64, next Delivery, step *DeliveryStep) (Delivery,error)`、`SaveDeliveryCI(id string, expectedVersion uint64, observations []DeliveryCI) (Delivery,error)`、`DeliverySteps(id string, after uint64, limit int) (DeliveryStepPage,error)`。`Receipt` 增加 `DeliveryID string`；`State` 增加 keyed-by-task 的 `Deliveries map[string]DeliverySummary` 和 keyed-by-project 的安全 policy 摘要，不含 secret。

- [x] **Step 1 — RED 测试：** `TestDeliveryMigrationAtomicStart` 从真实 schema v1（含登录、旧合并／轮次）升级后无活动周期；仅显式 enabled 的 start_task 在同事务保存消息／轮次／快照／周期／operation 回执。模拟事务失败各表均回滚；同 operation 同输入重放同 ID，不同输入冲突；一任务不能出现两个 active cycle。`TestDeliveryProjectionBoundedHistory` 保存 25 步只投影最近 20 条，分页无重复丢失，旧轮次确认不被技术成功篡改。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/workbench -run '^TestDelivery(Migration|Projection|Transaction)'`；预期缺方法／表 FAIL。
- [x] **Step 3 — 最小实现：** schema 升为 2，事务迁移建 `delivery_policies/deliveries/delivery_steps/delivery_ci/delivery_repairs/delivery_queue`；给 active task、step ID 和队列票据加约束。将现有 persist 抽成包内 `persistWith(next State, operationID, fingerprint string, receipt Receipt, write func(*sql.Tx) error) error`，同事务保存领域表、state 投影和 operations，commit 后才更新内存并广播；闭包不运行进程／网络且不重入 Store。`PreparedCommand(start_task)` 在锁内决定是否生成周期并返回 DeliveryID，不能先启动模型再补周期。
- [x] **Step 4 — GREEN：** 上述 tests 和 `GOTOOLCHAIN=local go -C server test ./internal/workbench` PASS；用阻塞外部 fake 验证数据库查询／消息保存不等外部操作。
- [x] **Step 5 — 检查点：** 记录迁移／回滚与完整 schema diff；不对用户数据库执行迁移。

## Task 3: GitHub 只读契约、精确 CI 门禁与日志

**Files:** 新增 `server/internal/githubactions/{client.go,workflows.go,runs.go,jobs.go,logs.go,gate.go}`、对应测试和 `testdata/contracts.md`／合成响应。

**Interfaces:** `NewClient(cfg ClientConfig) (*Client,error)`；`ClientConfig{HTTP *http.Client; APIVersion string; Token func(context.Context,string)(string,error)}`。`Inspect(ctx context.Context, repositoryID int64, owner, repo, credentialRef string) (RepositoryInfo,error)`；`Observe(ctx context.Context,key RunKey) (Observation,error)`；`Failure(ctx context.Context,key RunKey,observation Observation) (FailureEvidence,error)`。`RunKey{RepositoryID int64; DeliveryID,Stage,Owner,Repo,Branch,SHA,CredentialRef string; Required []workbench.DeliveryWorkflow}`；event 固定 push。`Observation{Runs []workbench.DeliveryCI; State,Code string; RetryAt time.Time}`，State=`waiting/passed/failed/blocked`；`FailureEvidence{Code,Summary,Fingerprint string; Truncated,Repairable bool}`；`RepositoryInfo` 含实际 ID／default branch／workflow IDs 和只读预检阻塞。

- [x] **Step 1 — RED 测试：** `TestActionsExactGate` 对错 repo/ref/SHA/event/workflow、无运行、skipped/neutral、缺 job／矩阵页、禁用 workflow、PR-only、取消／超时／action_required 逐项拒绝。`TestActionsCacheAttemptAndLatePages` 组合分页、304、attempt 2 running／旧 attempt 1 success、晚到旧 SHA，只有当前最新成功才通过。`TestActionsLogsAndCredentials` 覆盖 8 MiB/64 KiB/256 KiB、跳转不转发 Authorization、无 secret／临时 URL 落库、恶意日志不转换为执行指令、401/403/404/429/5xx 分类。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/githubactions`；预期包／实现缺失 FAIL。
- [x] **Step 3 — 最小实现：** 先核对官方 run/job/log/attempt API、分页／版本请求头，写 `testdata/contracts.md` 的核对日期与版本依据后固定 `APIVersion`，不能用搜索摘要编造协议。标准库 HTTP、15 秒超时、10 秒调度建议、ETag/Retry-After、每次最多 20 页×100 条，未遍历完返回 blocked 不假通过；只读不提供 dispatch/rerun 方法。token resolver 由 Task 4 注入；测试用自定义 RoundTripper 映射固定 API URL，不开放生产 API 任意 baseURL。
- [x] **Step 4 — GREEN：** `GOTOOLCHAIN=local go -C server test ./internal/githubactions -count=1` PASS，网络请求全部由 httptest／自定义 transport 接管；文档取证受阻则停在契约核对，不擅自解除网络防护。
- [x] **Step 5 — 检查点：** 保存合成契约和未验证项；不调用真实 API、不打印个人 token。

## Task 4: 凭据引用和受控 Git 认证桥

**Files:** 新增 `server/internal/githubauth/{credentials.go,askpass.go,ssh.go}` 和测试；新增 `server/internal/config/github.go`／测试；修改 config Workbench 及 `server/cmd/kaiwu-server/main.go`／测试。

**Interfaces:** `githubauth.New(cfg Config) (*Resolver,error)`；`Resolver.Token(ctx context.Context, ref string) (string,error)`；`Resolver.Git(ctx context.Context, ref, canonicalURL string) (GitCredential,error)`；`GitCredential{Env []string; Close func() error}`。受控 HTTPS askpass 由同一 Go 可执行文件的隐藏 `internal-git-credential` 子命令提供，不写生产 shell／JS；`HandleCredentialCommand(args []string) (handled bool, err error)` 在读取服务配置之前分流，仅读取继承的私有 FD／本地受保护桥，不从 argv 读 token。

- [x] **Step 1 — RED 测试：** `TestGitHubCredentialScopes` 验证 refs 不能变环境变量查询器，凭据配置不出现在 JSON、错误、argv；askpass 只回答已冻结主机／仓库请求，非父进程桥调用失败；SSH 使用指定 key/agent、BatchMode 和已有 known_hosts，未知 host 不自动信任。`TestCredentialEnvironmentSeparation` 向 pi、检查和预览注入捕获器，断言均无新增 GitHub token／SSH_AUTH_SOCK。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/githubauth ./internal/config ./cmd/kaiwu-server -run .Credential|GitHubConfig.`；预期缺实现 FAIL。
- [x] **Step 3 — 最小实现：** 初版环境引用固定为 `server_github_api`←`KAIWU_GITHUB_API_TOKEN`、`server_github_https`←`KAIWU_GITHUB_GIT_TOKEN`、`server_github_ssh`←受保护 `KAIWU_GITHUB_SSH_KEY`／显式允许 agent；新增配置不作为 Workbench JSON 输出。只允许绝对安全路径、不继承任意全局 helper；每次查询重新核实凭据可用性，不因快照保存而忽略撤销。保持现有模型 provider API key 的既有隔离行为。
- [x] **Step 4 — GREEN：** 上述 tests PASS；`GOTOOLCHAIN=local go -C server test ./internal/pimodel ./internal/preview` 回归无凭据渗漏。测试 key/token 全为合成值。
- [x] **Step 5 — 检查点：** 记录桥生命周期／清理和支持的凭据方式，不设置本机真实环境或全局 Git。

## Task 5: 唯一远程、精确 refs 与可核查 Push

**Files:** 新增 gitops `remote.go`、`remote_process_linux.go`、`remote_process_other.go`、测试；必要时仅抽取 `command.go` 中的有界输出与进程跟踪，不改变旧 local Env 缺省。

**Interfaces:** `RemoteTarget{RepositoryID int64; URL,MainRef,TaskRef string}`；`ResolveRemote(ctx context.Context, project workbench.Project, policy workbench.ProjectDeliveryPolicy, taskID string) (RemoteTarget,error)`。`NewRemoteRunner(cfg RemoteConfig) *RemoteRunner`；`RemoteConfig{Program string; Resolver *githubauth.Resolver; Transport RemoteTransport}`，`RemoteTransport` 的 `Run(ctx context.Context,dir string,target RemoteTarget,args []string,credentialRef string,lease ProcessLease) (string,error)` 仅为进程/测试依赖注入；nil 使用生产固定Git/认证实现，生产装配不得从HTTP或环境选择测试transport。`Observe(ctx context.Context,dir string,target RemoteTarget,ref,credentialRef string,lease ProcessLease) (string,error)`；`Fetch(ctx context.Context,dir string,target RemoteTarget,ref,credentialRef string,lease ProcessLease) (string,error)`；`Push(ctx context.Context,dir string,target RemoteTarget,sha,ref,credentialRef string,lease ProcessLease) (string,error)`。`ProcessLease` 含 `File() *os.File; Attempt() error; Started(int) error`；Push 返回实际核验远程 SHA，不表示 CI 成功。

- [x] **Step 1 — RED 测试：** `TestDeliveryRemoteRefScope` 对 credentials URL、多 pushurl、URL rewrite、remote helper、非 GitHub 主机、tag/mirror/任意 ref 拒绝；操作前 remote 被改拒绝。`TestDeliveryPushUnknownReceipt` 用临时 bare remote 模拟已成功接收但返回断连，Observe 可证实结果且不重新 Push；远程冲突／删除／非快进保留现场，API 可读但 Git 不可写只报告权限失败。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/gitops -run .^TestDelivery(Remote|Push).`；预期缺新方法 FAIL。
- [x] **Step 3 — 最小实现：** 生产只接收已标准化冻结 URL，用明确单 refspec；fetch `--no-tags` 到服务自有观察 ref，push 固定完整 SHA→目标 branch，无 `+`。网络默认 120 秒且受活动余额约束，进程归属先记 attempt 再 pid，继承租约文件并回收后代；超时和断连结果未知必须 Observe。本地 bare transport 仅测试构造函数注入，不通过 API／环境打开生产 file 协议。
- [x] **Step 4 — GREEN：** 同 tests PASS，再 `GOTOOLCHAIN=local go -C server test ./internal/gitops -count=1`；Git 创建／提交／Push 全部限制在 `t.TempDir()`。
- [x] **Step 5 — 检查点：** 记录 transport、凭据和既有合并回归；不向实际 origin 发请求。

## Task 6: 写入权交接、受测候选与本地检查

**Files:** 新增 `agent/delivery_lease.go`／测试、gitops `candidate.go`／测试、delivery `checks.go`／测试；修改 agent integration/task_preview 与 platform lease 包装，不取消原归属保护。

**Interfaces:** `agent.DeliveryLease` 扩展 Task 5 ProcessLease 并有 `Release(safe bool) error`；`Manager.AcquireDeliveryLease(ctx context.Context,deliveryID,scope string) (DeliveryLease,error)`，scope 仅 `task/project_task`。`gitops.Candidate{HEAD,Tree,Fingerprint,ProtectedFingerprint string; Files []workbench.GitFile}`；`CaptureCandidate(ctx context.Context,p workbench.Project,t workbench.Task,x workbench.Execution) (Candidate,error)`；`CommitCandidate(ctx context.Context,x workbench.Execution,c Candidate,message,name,email string,runner Runner) (string,error)`。`delivery.RunChecks(ctx context.Context,x workbench.Execution,checks []workbench.DeliveryCheck,lease agent.DeliveryLease) (CheckEvidence,error)`；`CheckEvidence{Fingerprint,Tree,Summary string; Passed,Truncated bool}`。

- [x] **Step 1 — RED 测试：** `TestDeliveryLeaseHandoff` 证明空闲 pi／预览可靠回收才交接、正在执行／遗留后代／归属不明拒绝；Release 前保留 reservation。`TestDeliveryCandidateChangedAfterCheck` 对内容／暂存／模式／symlink／新增敏感文件变化使 CommitCandidate 失败。`TestDeliveryChecksProcessBounds` 对 cwd 越界、shell 拼接、timeout／后代／输出上限拒绝，失败退出不能标 Passed；无有效变更不创建重复提交。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/agent ./internal/gitops ./internal/delivery -run '^TestDelivery(Lease|Candidate|Checks)'`；预期缺新接口 FAIL。
- [x] **Step 3 — 最小实现：** 原 m.reserveTask 与 OS 租约包装保留至释放，project_task 按 common→task 顺序持有；Acquire 不跨网络等待。Capture 复用所有私密路径／owner/dirty/未完成 Git 保护，冻结 workflow 定义；检查 argv 直执行、白名单环境、单命令 300 秒、有界输出8 MiB、失败摘要256 KiB。检查前后指纹相等、提交 tree 相等才允许固化证据，不把测试改源码静默提交。
- [x] **Step 4 — GREEN：** 同 tests PASS；旧 `./internal/agent ./internal/preview ./internal/gitops` 全回归，session ID／文件／历史结果保留。
- [x] **Step 5 — 检查点：** 记录真实进程回收证据和平台限制，无主 checkout 写入／清理。

> 2026-10-17 GREEN 落地：实现期间发现并修复 `AcquireDeliveryLease` 双重 `filepath.Dir`——交付任务租约曾落到全局 `data/worktrees/` 根目录（共享锁、marker 互相污染），已改为与生产其余调用点一致传 `x.Workspace`（内部取 `Dir` 得到每任务 `data/worktrees/<taskID>/`）。

## Task 7: 固定状态机、开始调度与只读预检

**Files:** 新增 delivery `types.go`、`manager.go`、`policy.go`、`scheduler.go`、`stages.go` 及测试；新增 workbench `delivery_repair.go` 的 job/预算事务基础；修改原 start 周期衔接，不把网络放入 PreparedCommand。

**Interfaces:** `delivery.New(ctx context.Context,s *workbench.Store,cfg Config) (*Manager,error)`、`Manager.Wake()`、`Manager.Close() error`、`Manager.Preflight(ctx context.Context,p workbench.Project,policy workbench.ProjectDeliveryPolicy) (Preflight,error)`、`Manager.Tick(ctx context.Context,id string) error`。`Config` 含 `Agent AgentPort; Actions *githubactions.Client; Remote *gitops.RemoteRunner; Clock Clock; DataDir string`；`Clock{Now() time.Time; After(time.Duration)<-chan time.Time}`。`AgentPort` 含现有 `Send(string,string)error`、Task 6 AcquireDeliveryLease，及 Task 8 `DispatchRepair/ApplyPendingModel`；不依赖 agent 反向导入 delivery。`Preflight{Ready bool; Fingerprint string; Blockers []string}`。

- [x] **Step 1 — RED 测试：** `TestDeliveryStateMachineGates` 用 fake clock／ports 驱动 developing→local_checking→committing→pushing_task→task_ci；仅实际 settled＋检查证据允许推进，ack／恢复／旧轮次不能启动交付。`TestDeliveryNoImplicitStartAndRevocation` 覆盖旧任务、普通聊天、缺 workflow、授权撤销、错误运行；默认预检不会 commit/push，API 列表读取不宣称已证明写权限。`TestDeliveryWakeIdempotent` 多个通知只消费一次步骤。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/delivery -run '^TestDelivery(StateMachine|NoImplicit|Wake)'`；预期调度器不存在 FAIL。
- [x] **Step 3 — 最小实现：** 订阅 Store revision 并扫描持久周期／轮次，不在 finishLocked 回调调度。每步保存 intent、输入指纹、CAS version 后才执行，结果不明进入 needs_check；新 start 有 DeliveryID 时只 Wake，worker 完成同步预检／安全基线后调用原 Agent.Send，旧无 DeliveryID 沿用原路径。外部命令前再读授权撤销和实际 refs。最多2个本地检查 worker、4个 API 并发槽，剩余排队不吞错误；不计人工／调度等待，CI等待计活动预算。
- [x] **Step 4 — GREEN：** 同 tests 和 `./internal/workbench ./internal/delivery` PASS；fake clock 证明截止时间随持久记录生效、不靠真实 sleep。
- [x] **Step 5 — 检查点：** 此时只验收基础执行，不标整个流程已完成，不接入真实生产启动。

> 2026-10-17 GREEN 落地：新增 `delivery/types.go`（Config/Clock/AgentPort/Preflight）、`manager.go`（单 worker 订阅 revision＋15 秒 Clock.After 重扫，wake 容量 1 合并）、`policy.go`（授权门禁：停用/撤销/无效/保护范围指纹变化→blocked）、`stages.go`（queued→developing→local_checking→committing→pushing_task→task_ci→awaiting_acceptance）。第一阶段裁决：只有走 CAS 后才扫描；阶段链内 40ms stagePace 保证 commit/push 等中间阶段可被外部观察（与真实 sleep 的轮询节拍无关，截止时间全部走 fake Clock）；无有效变更不建空提交，CommitCandidate 任一失败 park("commit_unverified")。stageQueued 先 CAS 派发 intent（阶段仍 queued），Send 成功后才把周期推进到 developing — 观察者永远不会看到未派发的 developing。Fixture 修正 1 处：preflight 子测试为健康夹具断言 blocked，与同一夹具下 6 个必须走通全链的子测试矛盾（blocked 无可到达门禁，违反已批准的 settled 轮次推进语义），按 Task 1/6 先例改为断言 awaiting_acceptance（自主循环确实停写的阶段）并记录裁决。回归 6 包全绿＋-race 通过＋gofmt 干净。

## Task 8: 原 pi 自动修复与下一轮模型应用

**Files:** 新增 agent `delivery_repair.go`、`model_next.go` 和测试；修改 workbench `delivery_repair.go`、`types.go`、`report.go`、commands，agent manager/events/launch，隔离 fakepi。

**Interfaces:** Store 不直接引用 githubactions 类型；产出 `PrepareDeliveryRepair(deliveryID string,expectedVersion uint64,failedSHA,stage,failureFingerprint,summary string) (DeliveryRepair,error)`。`ClaimDeliveryRepair(jobID string) (DeliveryRepair,bool,error)` 原子扣预算并生成系统轮次；`Manager.DispatchRepair(ctx context.Context,deliveryID,jobID string) error`、`Manager.ApplyPendingModel(ctx context.Context,conversationID string) error`。Store `RequestNextProfile(operationID,conversationID string,profile Profile) (Receipt,error)`；Conversation 增加 `PendingProfile *Profile`，Round 增加 `Profile Profile; Origin, DeliveryID string`，轮次／repair 记录实际使用 Profile；旧初始 Profile 不回写模板。

- [x] **Step 1 — RED 测试：** `TestDeliveryRepairSameSessionOnce` 验证系统标识、相同 session/worktree、同 job 重复 dispatch 只一请求；claim 后请求未知不重扣／重发。`TestDeliveryRepairSharedBudget` 本地／任务CI／主CI共用3次额度、重启不清零、同指纹且无变更停止。`TestDeliveryNextModelAtRoundBoundary` 当前请求不被打断、下一轮使用新模型、失败不偷偷用旧模型发 prompt、模板不变、8会话容量不足排队。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/agent ./internal/workbench -run '^TestDelivery(Repair|NextModel)'`；预期新轮次／pending model 功能不存在 FAIL（编译期缺少 StorePrepare/Claim、Round 字段、agent 方法，全部按预期报缺）。
- [x] **Step 3 — 最小实现：** claim、预算和系统消息／Round 在同事务提交；系统消息标记 `Role=system, Kind=automatic_repair`，Round 标记 `Origin=system_repair`，不能伪造人类提交的消息。每次 Call 前保存 dispatching 状态。沿用 acknowledged＋settled 边界，恢复无新请求不伪造新结果；禁止 pi 自行 Git 发布的系统指令保留。下一轮开始前可靠回收旧空闲实例、按所选 provider 准备隔离凭据、恢复同 session，核对实际模型后清 PendingProfile；需要 RPC set_model／thinking 时先核对本机 pi RPC 文档与 fake 契约，不在运行请求中强切。GitHub 凭据始终不进入 session。
- [x] **Step 4 — GREEN：** 上述 tests PASS，`GOTOOLCHAIN=local go -C server test ./internal/agent ./internal/pimodel ./internal/pirpc ./internal/workbench` 全回归；旧“空会话补选”测试保留并区分新显式下一轮命令。
- [x] **Step 5 — 检查点：** 仅假模型协议验证，真实跨 provider、额度／OAuth 行为明确未验证。

> 2026-10-17 GREEN 落地：新增 `workbench/delivery_repair.go`（PrepareDeliveryRepair／ClaimDeliveryRepair／MarkRepairDispatching／RequestNextProfile；claim 在同一事务扣共用 RepairsUsed＋写 delivery 行＋写系统消息 Kind=automatic_repair、Role=system＋建 Round Origin=system_repair/DeliveryID/Profile；del rep rows 以 body JSON 存 repairRecord 与受限 prompt）。`Round` 增加 Profile/Origin/DeliveryID，`BeginRound` 记录生效画像（PendingProfile 优先）。新增 agent `delivery_repair.go`（DispatchRepair=claim→MarkRepairDispatching→Send，重复 dispatch 依赖 claim 幂等+messageID 去重，Send 失败保留 dispatching 交任务 9 分析），`model_next.go`（recycleForModel／ApplyPendingModel；公开入口仅回收空闲实例，运行中返回 Conflict）。`manager.run` 在轮次边界应用 PendingProfile：先回收旧空闲实例（Close+Wait 核实回收、释放租约、等 consumer 退出），launch 前替换本地 Profile，get_state 可报告 model/provider 时严格核对，不符则失败且不发送本轮请求、不清 PendingProfile；核对通过与其余会话身份在同一 store.Update 里写入新 Profile＋清 PendingProfile。 pirpc 无 set_model/set_thinking（已在会话内核实），模型切换只靠新进程 launch args＋同 session 文件恢复，未在运行请求强切；fake 契约新增 get_state model/provider 回显与 KAIWU_AGENT_FAIL_MODEL 失败注入。回归：目标三测试 ok；4 包 -count=1 ok；全仓 15 包 -count=1 ok；agent/workbench -race ok；gofmt／vet 干净。

## Task 9: 修复闭环、效果验收与安全停机

**Files:** 新增 delivery `repair.go`、测试，完善 `stages.go`／policy；新增 workbench repair/acceptance 测试。

**Interfaces:** `Manager.Accept(ctx context.Context,operationID,deliveryID,token string,expectedVersion uint64) (workbench.Receipt,error)`；`Manager.Control(ctx context.Context,operationID,deliveryID,action string,expectedVersion uint64) (workbench.Receipt,error)`，action 仅 pause/resume/cancel/extend_budget；Store 提供 `ControlDelivery(operationID,deliveryID,action,token string,expectedVersion uint64) (Receipt,error)`，验收和控制同事务存原operation回执。周期开始时 RepairsLimit/ActiveLimitSeconds 取快照初值；extend_budget 由用户显式追加一份快照额度（默认3次／3600秒）并记审计，不清历史计数、不修改原policy快照。活动周期需改其他配置时暂停并明确终止旧周期，再按新授权版本开启新周期，不追溯覆盖历史。内部 `ClassifyFailure(e githubactions.FailureEvidence,local *CheckEvidence) (repairable bool,code string)`；权限、Secrets、冲突、超时／取消、未知原因默认 blocked，不依赖 AI 自报分类直接放行。

- [x] **Step 1 — RED 测试：** `TestDeliveryRepairInvalidatesAcceptance` 验证验收 token 绑定周期、最终SHA/tree/base/round；源码变更后旧token拒绝，历史认可仍保留；无人工模式不调用 ConfirmResult。`TestDeliveryRepairBoundaries` 验证3次／3600秒停止、相同无效循环停止、workflow定义/门禁变化停止；secret／审批／限流不调用模型。`TestDeliveryPauseCancelUnknownSideEffect` 暂停不等于撤销已发出的 push，终止前必须核查写入者。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/delivery -run '^TestDelivery(Repair|Pause|Acceptance)'`；预期缺完整闭环 FAIL（编译期缺 Store.AcceptDelivery/ControlDelivery/LatestDeliveryRepair、Manager.Accept/Control、RepairSummaryLimit）。
- [x] **Step 3 — 最小实现：** 失败证据有界脱敏后交 Task 8 的原会话 repair job；修复后清旧本地/CI证据并回 local_checking，不能直接 Push。workflow／授权指纹改变暂停审核、不得删除失败测试／跳过 CI／缩小门禁；机械保护无法证明任意测试语义时明确转人工。验收前允许任务分支已推送且项目授权说明；候选重验严格绑定精确令牌。
- [x] **Step 4 — GREEN：** 同 tests PASS；伪造系统报告、恶意日志要求读取密钥／执行脚本不产生新的操作权限。修复后送原会话的 prompt 只含 64KiB 内脱敏摘要+本地检查证据，令牌注入测试证明 `[已脱敏令牌]` 惰性。
- [x] **Step 5 — 检查点：** 记录是否需要人工验收／已上传任务分支，不把技术成功等同功能认可。no-manual 模式技术成功直达 succeeded 但任务永不 completed；预算与截止停止一律 blocked 等人工。真实模型/GitHub/Actions 联调未验证。

> 2026-10-17 GREEN 落地：新增 delivery/control.go、repair.go；stages.go 接入失败分类与修复等待；workbench/delivery_control.go、delivery_repair.go。关键裁决见 SDD ledger（控制回执指纹不含 expectedVersion；控制 action 不耗 token；":not_repairable" 剥离；证据仅在 failure 时获取；GREEN 核心修复=repair() phase CAS 以 fresh 为基底，避免覆盖丢失 claim 扣除的 RepairsUsed）。

## Task 10: 持久 FIFO、候选同步与主分支隔离

**Files:** 新增 workbench `delivery_queue.go`／测试，gitops `delivery_merge.go`／测试，delivery `integration.go`／测试；修改旧 agent integration 和 workbench commands 的活跃交付互斥。

**Interfaces:** Store `EnqueueDelivery(id string) (uint64,error)`、`AcquireDeliveryTurn(id string) (bool,error)`、`ReleaseDeliveryTurn(id string,allowUnverified bool) error`；键同时含 canonical common dir 与 repoID/mainRef；只有核验完成或明确取消且告知未验证才释放。`gitops.SyncCandidate(ctx context.Context,p workbench.Project,t workbench.Task,x workbench.Execution,baseSHA string,runner Runner) (Candidate,error)`；`IntegrateCandidate(ctx context.Context,p workbench.Project,candidateSHA,expectedMainSHA string,runner Runner) (string,error)` 只快进至已验证SHA。`Manager` 在 Tick 调用这些接口，不新增 public 自由合并路径。

- [x] **Step 1 — RED 测试：** `TestDeliveryRemoteLaneAcrossClones` 同 common dir、多项目入口、同 repo 不同clone共享隔离；持久票据FIFO，其他任务仍能开发／任务CI，旧手动Merge被阻断。`TestDeliveryRevalidateCandidateAfterMainMoves` 主分支改变→任务分支普通merge→全门禁重跑，冲突不自动解决，未知本地领先提交不上传。`TestDeliveryMainFailureQuarantinesQueue` 主分支失败后仅原任务修复推进，下一任务等待，不reset远程，不遗失原session。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/delivery ./internal/gitops ./internal/workbench -run '^TestDelivery(RemoteLane|Revalidate|MainFailure)'`；预期缺队列／集成 FAIL（编译期缺 EnqueueDelivery/AcquireDeliveryTurn/ReleaseDeliveryTurn、SyncCandidate/IntegrateCandidate）。
- [x] **Step 3 — 最小实现：** 逻辑槽持久，等待CI／验收不占OS锁；执行时按远程门→项目→任务顺序，主目录checkout／干净状态／owner全部复验。任务候选包含最新main后本地main只FF到同SHA，跑集成检查、精确Push、观察主CI；外部提交竞争阻断。失败基线归当前周期，修复在原worktree纳入已集成SHA后继续全流程；显式解除隔离仍保留未验证警告。
- [x] **Step 4 — GREEN：** 同 tests PASS；`GOTOOLCHAIN=local go -C server test -race ./internal/delivery ./internal/agent ./internal/workbench ./internal/gitops` PASS，无Store/Agent锁外部等待和锁反转。
- [x] **Step 5 — 检查点：** 保留全部分支／工作区；记录主本地已集成、已Push、CI结果三个独立事实（integrate_main／push_main／ci_main 步骤）。

> 2026-10-17 GREEN 落地：新增 workbench/delivery_queue.go、gitops/delivery_merge.go、delivery/integration.go 及三包测试。关键裁决：验收由 Task 9 的 succeeded 修正为 integrating（succeeded 只在主 CI 通过后到达，验收 lane 保留到主 CI）；repair 增加显式失败阶段；queue 键同时含 canonical common dir 与 repoID|owner/name|mainRef。旧手动 Merge 与开始交付增加活跃交付／合并互斥。

## Task 11: 每步骤崩溃核查与服务生命周期

**Files:** 新增 delivery `recovery.go`、`recovery_test.go`、`recovery_linux_test.go`；修改 manager/scheduler；扩展 step 归属测试，不改变旧 RecoverMerges 语义。

**Interfaces:** `Manager.Recover(ctx context.Context) error`；`ReconcileStep(ctx context.Context,d workbench.Delivery,step workbench.DeliveryStep) (Reconciliation,error)`；`Reconciliation{State string; AfterSHA string; ResumeSafe bool; Reason string}`，State=`observed/not_started/uncertain`。Close 停新步骤、回收服务自有进程，保留远端Actions。

- [x] **Step 1 — RED 测试：** `TestDeliveryCrashAfterMainPushBeforeReceipt` 在intent／started／执行成功／回执之前逐点SIGKILL子测试，重启找回原remote SHA且Push次数不增加、队列槽仍在、旧CI原attempt继续观察。`TestDeliveryRecoveryCommitAndRepairWindows` 只有唯一parent/tree可证实时补记，未知模型dispatch不重发，预算／截止时间不重置；旧手动Merge／未授权任务不恢复写入。
- [x] **Step 2 — 确认失败：** `GOTOOLCHAIN=local go -C server test ./internal/delivery -run '^TestDelivery(Crash|Recovery)' -count=1`；预期无recovery FAIL（编译期缺 Recover/ReconcileStep/Reconciliation 与推送 intent 步骤）。
- [x] **Step 3 — 最小实现：** 正式恢复前核验owner、boot/process group及租约，记录未知启动窗口；不能按数据库PID杀进程。仅已知未执行步骤在持续授权及输入不变时推进；原AI／本地检查中断结果未知进入needs_check。CI用持久deadline恢复只读查询，实际远端变化不能随意补作本周期成功。
- [x] **Step 4 — GREEN：** 同tests PASS，并重复关键故障测试 `-count=10`；跨平台stub明确unsupported，Linux实际后代进程不存在才释放安全租约。
- [x] **Step 5 — 检查点：** 单独记协议fake、真实Linux进程回收和重启测试，不混同真实GitHub／模型结果。

> 2026-10-17 GREEN 落地：新增 delivery/recovery.go（Recover／ReconcileStep／resolvePendingIntent／backfillObserved，推送前落 running intent）＋ recovery_test.go（状态化窗口）＋ recovery_linux_test.go（真实 SIGKILL 子进程＋真实 bare 远程＋重开 SQLite 恢复）＋ recovery_other_test.go（非 Linux 显式 unsupported）。关键裁决：只补记唯一可证结果，歧义进 needs_check；崩溃窗口不重置 CI 预算（deadline 由 intent StartedAt 推算）；每次推送重新签发验收令牌；恢复遇并发冲突不强写。

## Task 12: 业务 Socket、认证授权与生产装配

**Files:** 新增 webapi `delivery.go`／`delivery_test.go`；修改 handler/socket/snapshot/git、app/app.go／测试，必要时把 command 分支拆到 `webapi/command.go`，不重构无关模块。

**Interfaces:** `webapi.Config.Delivery` 使用只包含 Preflight/Accept/Control/Wake 的 `DeliveryCommander`；查询与policy保存调用Task 2 Store。Socket方法固定 `delivery/policy`、`delivery/preflight`、`delivery/accept`、`delivery/control`、`delivery/status`、`delivery/steps`；均复用已认证ServeHTTP适配。现有 `commands` 另加显式 `set_next_profile`，验证catalog后调用Task 8 RequestNextProfile，当前模型调用不被中断。请求含project/task/delivery identity、operationId、expectedVersion；steps `{deliveryId,after,limit}`，limit<=100。不接受任意argv代执行或任意credential env名；配置argv仅作为明确授权的项目检查定义。

> 2026-10-17 GREEN 落地：`webapi/delivery.go`（DeliveryCommander＋固定路由＋socket 映射＋set_next_profile）、`app/app.go` buildDelivery 生产装配与 Recover/关闭顺序、`delivery.SystemClock`。测试 TestDeliverySocketAuthorizationAndReplay、TestDeliveryAppLifecycle 通过；流程偏差如实记录：本任务未单独执行 RED（实现与测试同批次）。

- [x] **Step 1 — RED 测试：** `TestDeliverySocketAuthorizationAndReplay` 覆盖认证／Origin／过期Cookie、跨项目ID、同operation不同参数、丢回执后查原operation，恢复snapshot不重发操作；重复Accept不触发新Push。`TestDeliveryAppLifecycle` 无客户端也继续调度，handler Close只断连接，app Close先关API、再delivery、preview、agent、Store；GitHub凭据不进snapshot／错误。`TestDeliveryPolicyPreflightReadOnly` 不以预检写远程（并入首个测试的零副作用断言）。
- [x] **Step 2 — 确认失败：** 未单独执行 RED（实现先于测试），偏差已记录；缺失面为 DeliveryCommander／`/api/delivery/*` 路由／socket 映射／set_next_profile／生产装配。
- [x] **Step 3 — 最小实现：** 新DeliveryID回执仅Wake，不再额外Agent.Send；旧项目沿用既有逻辑。Store摘要接现有revision／有界增量／分块快照，广播前过滤私有信息。app在监听服务生命周期内构建auth/remote/actions/delivery、Recover后开放业务，授权缺失时功能disabled而基础服务仍可启动；严禁生产环境fake endpoint开关。下一轮模型命令校验来自真实catalog，但测试使用fake。
- [x] **Step 4 — GREEN：** 同tests PASS，再 `GOTOOLCHAIN=local go -C server test ./internal/webapi ./internal/app ./internal/workbench` 全回归，已有login／预览／手动Merge无破坏。
- [x] **Step 5 — 检查点：** 不启动或重启用户7331；只通过测试实例验证装配与shutdown（临时端口；shutdown 后同一 SQLite 可重开证明 delivery/agent 先于 Store 关闭）。

## Task 13: 一次配置、简洁状态与真实服务浏览器闭环

**Files:** 新增Web文件见文件地图；修改App/ChatPanel/KanbanBoard/ProjectMenu/ProgressCard/ModelDialog/types/workbench；新增 `apps/web/tests/delivery.browser.spec.ts`、`delivery.server.mjs`、Go testdata webfixture，修改playwright.config.ts；必要时扩展 `agent/testdata/fakepi/main.go`。现有browser.spec.ts滚动测试保留，不回退其他会话改动。

**Interfaces:** `delivery.ts` 产出 `deliveryView(summary?:DeliverySummary): DeliveryView` 和 `canAcceptDelivery(summary:DeliverySummary): boolean`；DeliveryView仅有阶段标签、原因、主动作、可展开步骤，不生成Git或模型请求。Snapshot新增可选deliveries／deliveryPolicies，与Go字段一致；设置只保存非secret refs；已有结果ConfirmResult入口不充当自动交付token。ModelDialog支持明确“下一轮使用”及待生效／失败状态，调用set_next_profile，不改变项目模板；修复次数分母取实际RepairsLimit，初值3。

- [ ] **Step 1 — RED 测试：** 在现有workbench.test.ts加入纯投影／旧snapshot缺字段／CI红灯不被completed覆盖／false验收模式不伪造认可。新Playwright用真实Go＋临时bare＋fake Actions／pi覆盖正常闭环、修复1次成功、3次停止、主CI隔离两卡、撤销／暂停、候选验收过期、换卡／刷新／断线丢回执／跨范围迟到结果、360/768/1440px键盘与详情收起，正常路径无逐项提交／Push按钮要求。
- [ ] **Step 2 — 确认失败：** `npm --prefix apps/web test` 预期新投影缺失FAIL；`npm --prefix apps/web run test:browser -- --project=delivery` 预期新增UI／fixture缺失FAIL。先确认现有测试无基线失败，不把环境缺Chromium当正确RED。
- [ ] **Step 3 — 最小实现：** 项目菜单“开发与交付设置”显示上传时机／触发副作用／预算／授权范围，一次配置后任务照流程走；卡片／聊天显示实际阶段、次数、SHA、Actions真链接和必要业务验收。原Git状态及效果预览保留，技术记录分页。Go测试fixture通过构造依赖注入本地Git runner／fake HTTP，复用真实Store/agent/delivery/webapi，不向生产编入fake路由；fixture只监听127.0.0.1:17332，原浏览器服务保留17331，playwright配置两个具名project（existing/delivery）。新mjs只管理测试进程，不是新增产品后端。
- [ ] **Step 4 — GREEN：** `npm --prefix apps/web test`、`npm --prefix apps/web run build`、`npm --prefix apps/web run test:browser` 全PASS；明确构建会复制前端产物到 `server/internal/webui/dist`，复查并保留项目既有忽略策略，不将其当源码授权提交。
- [ ] **Step 5 — 检查点：** 保存合成项目桌面／手机截图和真实命令计数；页面出现“已交付”必须有临时远程ref及fakeCI原始证据，不用页面注入绿色状态替代流程。

## Task 14: 全量复验、独立审查与交付证据

**Files:** 新增 `docs/development/web-automated-delivery-verification.md`；更新README、路线图、架构、需求、`apps/web/README.md`、`server/README.md`、TODO；若用户批准真实联调，证据另记不含secret。

**Interfaces:** 消费Tasks 1–13的实际测试／构建／截图／故障记录；不再扩展业务接口。所有“已实现”必须限定范围，明确哪些仍fake验证。

- [ ] **Step 1 — 建立验收清单：** 按规格第13节逐行对应测试名称及证据；任何缺项先写回归失败场景并交给对应任务修复，不用文档写PASS替代测试。
- [ ] **Step 2 — 后端完整验证：** `GOTOOLCHAIN=local go -C server test ./...`、`test -race ./...`、`vet ./...`、`mod verify`；`gofmt -l`检查本批Go文件；`go -C server build -o /tmp/kaiwu-delivery-verify ./cmd/kaiwu-server`。运行隔离shutdown烟测，核对无残留进程；不启动本机7331或真实工作区任务。
- [ ] **Step 3 — 前端与故障复验：** 重跑Task13三命令，Task11关键故障`-count=10`；核对新功能不会破坏原聊天最新定位／预览／登录／手动合并和旧数据。只有本机结果，不声称Actions或原生端已验收。
- [ ] **Step 4 — 独立审查及修复复验：** 按用户所选执行方式安排只读审查；分派前读取实际Paseo profiles及notes，不猜模型、不创建新Workspace。普通问题回原实现者，修复后重复对应与全量检查；工具／配置不可用时说明阻塞，不伪称完成独立审查。
- [ ] **Step 5 — 记录与用户交付：** 更新实际状态、能力限制、模型费用与真实联调未执行项。真实API／Git push／Actions触发／模型自动修复测试必须先取得测试仓库、分支、额度与副作用许可；没有许可就如实列未验证，不擅自建立workflow、提交开悟代码或Push。提醒团队协作、通用可配置编排两个后续方向，再由用户决定优先级。

## 规格覆盖与自查记录

| 规格部分 | 实施任务 |
| --- | --- |
| 目标、当前边界、方案选择 | Global Constraints／Task7／Task14 |
| 项目配置、授权、快照 | Task1、2、4、7、12、13 |
| 固定流程、检查证据、效果验收 | Task6、7、9、10、13 |
| worktree、队列、同步、失败基线 | Task5、6、8、10、11 |
| Actions匹配／退避／jobs／日志 | Task3、5、7、9 |
| 修复预算、系统轮次、模型下一轮 | Task2、7、8、9、11 |
| 凭据与日志安全 | Task3、4、5、6、9、12 |
| 数据／模块／Socket／迁移 | Task1、2、7、12 |
| 生命周期、崩溃恢复 | Task5、6、8、11、12 |
| UI与兼容 | Task9、12、13 |
| 完整验收与真实测试边界 | Task13、14 |

已完成计划自查：按上表核对规格覆盖，五项Review Focus均分配具体失败测试；统一了远程观察的凭据／租约参数、CI落库接口、原子控制回执、修复扩额字段和测试transport注入，避免接口缺口。核对契约类型／字段名、RED断言、生产者依赖、无自动Git授权、无循环导入和无生产fake开关。以上业务测试均为待实施验收，不是本次规划已运行的结果。

## 已确认执行方式与暂停位置

建议**当前会话串行实现＋完成后一次独立只读验收**：原会话/锁/Store/队列接口依赖紧密，又有其他未提交UI成果，单写入更易集成、上下文成本较低。另一方式是逐任务子Agent实现＋独立逐项审查，审查更密但上下文成本更高；选用时仍须遵守实际profiles、同Workspace、文件边界与不并发写共享checkout的规则。

用户已确认推荐执行方式并开始串行实施。当前停在 Task 3 的官方接口契约核对；不放宽网络安全检查，不调真实模型／GitHub，不提交代码，不更新 7331。恢复条件见执行记录，不重复已完成任务。
