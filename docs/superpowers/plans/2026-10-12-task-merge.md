# 卡片 Git 状态与主分支合并 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 每张卡片保留独立 worktree，自动显示实际合并状态，并可通过图标或任务聊天审核后提交并合并到默认主分支。

**Architecture:** 在既有 Go 服务中增加 Git 检查组件，任务生命周期仍由 agent.Manager 管理。SQLite 保存主分支选择、检查结果与合并操作阶段；Manager 串行集成同一仓库，可靠回收目标任务空闲 pi 后接管工作区写入租约。Vue 展示服务端预检，确认请求只引用预检 ID，沿用认证、幂等回执和 WebSocket。

**Tech Stack:** Go、os/exec、Linux flock／进程组、现有 modernc SQLite；Vue 3＋TypeScript＋Vite；Git CLI；隔离假 pi／临时 Git fixtures／Playwright。不新增依赖。

**Spec:** [已确认设计](../specs/2026-10-12-task-merge-design.md)。执行前完整阅读规格与本计划。

**执行状态（2026-10-12）：** 用户已批准，本批实现完成；实际测试分组、审查修复与未验证组合见 [验证记录](../../development/web-task-merge-verification.md)。下列原始步骤保留供追溯，具体场景以验证表为准，不把计划清单当作全部组合已测试的证据。

## 本批执行结果

- [x] 任务 1：持久 Git 状态、登记／回执及兼容迁移。
- [x] 任务 2：默认主分支、工作区归属及真实 Git 检查。
- [x] 任务 3：空闲会话接管、任务／项目写入者保护。
- [x] 任务 4：审核绑定、提交／普通合并及证据恢复。
- [x] 任务 5：认证 API、可选设置与有界检查。
- [x] 任务 6：卡片／聊天图标、审核交互与独立范围。
- [x] 任务 7：完整回归、独立只读审查及修复、文档、7331 部署／烟测。

原始步骤中的自动化场景按验证记录分组执行；完整故障组合／压力矩阵没有全部注入，未以实施完成代替这些证据。

## Global Constraints

- 使用当前 Agent 串行实现，完成后独立只读验收；不并发写主 checkout。当前仓库尚无初始提交，不为本次开发创建基线提交或工作树。
- 不对当前开悟仓库提交、合并、推送、Tag 或 Release；测试提交／合并只发生在可清理的临时 Git 仓库。
- 新建项目默认主分支 `main`；已有项目自动识别并记住，覆盖项只在高级设置中出现。
- 普通合并能快进时快进，分叉无冲突时创建合并提交；不 squash、不 rebase、不推送、不清理任务分支／worktree。
- 无本地 Git 身份时确认框明确显示 `Kaiwu <kaiwu@localhost>`；不修改全局 Git 配置。
- 活动项目每 `3 秒`检查；进入项目、重新可见、执行／合并结束、手动刷新立即检查。结果不变不增加 revision。
- 不调用真实模型；服务仍使用现有数据／认证和 `192.168.2.122:7331`。客户端断开不终止服务器 Git 操作。
- 保留专题看板／右侧聊天、五种执行角标、Markdown 和预览；不做源码／diff 展示、编辑器或 IDE。
- 自动审批不代替最终用户对具体提交／合并的确认；任务普通结束不自动提交。

## Review Focus

1. 文件名含换行、空格、前导 `-` 或无效 UTF-8：Git 使用 NUL 分隔与参数数组；不可正确展示或限定的输入明确阻止写操作。任务 2、4 验证。
2. 文件内容改变但 porcelain 状态仍相同：旧预检必须失效，不能只比较状态字符串或应用 revision。任务 2、4 验证。
3. 项目目录包含服务数据或嵌套仓库：不得把 SQLite、令牌、pi 会话或新嵌套仓库装进任务提交；阻止并显示原因。任务 2、4 验证。
4. Git 配置／attributes 指定外部 merge driver、filter、签名或 hook：预检不能意外执行外部程序；遇到无法安全禁用的配置明确阻止操作。任务 2、4 验证。
5. 慢 Git／大量卡片／重复刷新：有界检查、取消和共享请求，不能持续写 SQLite 或饿死普通聊天；重启不重放 Git。任务 5、6 验证。

## 文件与接口归属

| 文件 | 责任 |
| --- | --- |
| `server/internal/workbench/git.go`、`git_test.go` | Git 产品类型、主分支设置、合并登记／幂等、短事务阶段更新与重启标记 |
| `server/internal/workbench/types.go`、`store.go`、`commands.go`、`execution.go` | 兼容新增 JSON 字段，所有任务写入口检查集成保留权 |
| `server/internal/gitops/{command,workspace,branch,status,preview,merge}.go` 及对应测试 | 固定参数 Git 调用、归属校验、分支识别、状态／指纹、实际提交合并；不依赖 HTTP／pi |
| `server/internal/agent/{integration,integration_recovery}.go` 及测试 | 空闲 pi 排空、项目／任务保留权、异步集成、退出与恢复 |
| `server/internal/agent/lease_linux.go`、`lease_other.go`、`workspace.go`、`manager.go` | 复用持久进程归属／OS 锁，任务基线取默认主分支，合并保留期间拒绝启动 |
| `server/internal/webapi/git.go`、`git_test.go`、`handler.go`、`project.go` | 认证状态检查、预检／确认／查询／高级设置与新建 main |
| `apps/web/src/{git,types}.ts`、`GitStatus.vue`、`MergeDialog.vue` | 显示类型、明确短语识别、轻量状态与审核对话框 |
| `apps/web/src/{App,KanbanBoard,ChatPanel,TaskDialog}.vue`、`style.css` | 轮询生命周期、图标／聊天入口、高级设置、范围隔离与紧凑样式 |
| `apps/web/tests/{workbench.test.ts,browser.spec.ts,server.mjs}` | 纯状态／真实 Go 与假 pi 浏览器回归 |
| `README.md`、`AGENTS.md`、`docs/{roadmap,architecture,product-requirements}.md`、`apps/web/README.md` | 只按实际通过情况更新状态与运行说明 |

### Task 1：可恢复的 Git 产品状态

**Interfaces:** `Project` 新增 `DefaultBranch, MainBranchOverride, CommitName, CommitEmail string`，JSON 使用相应 camelCase；`State` 新增 `TaskGit map[string]GitStatus`、`Merges map[string]MergeOperation`。类型在 `workbench/git.go` 定义：

- `GitStatus{TaskID, State, MainBranch, MainSHA, TaskSHA, Reason, CheckedAt string; DirtyFiles []GitFile; CanMerge bool}`；State 为 `not_started/no_changes/dirty/unmerged/merged/unknown`。
- `GitFile{Path, Status string}`，路径按原始 Git 名称保留，展示使用 Vue 文本节点。
- `MergePreview{ID, ProjectID, TaskID, MainBranch, MainSHA, TaskSHA, Fingerprint, CommitName, CommitEmail, Reason, ExpiresAt string; Files []GitFile; NeedsCommit, CanMerge bool}`。
- `MergeRequest{OperationID, ProjectID, TaskID, PreviewID, CommitMessage string}`；`MergeOperation{ID, ProjectID, TaskID, PreviewID, CommitMessage, Phase, TaskBeforeSHA, TaskAfterSHA, MainBeforeSHA, MainAfterSHA, Fingerprint, Error, StartedAt, FinishedAt string; Preview MergePreview}`。
- `Store.BeginMerge(req MergeRequest, preview MergePreview) (MergeOperation, bool, error)`：bool 表示 replay；`Store.FindMerge(req MergeRequest) (MergeOperation, bool, error)` 在预检已过期时也能查同一已登记请求；`Store.AdvanceMerge(id string, mutate func(*MergeOperation) error) error`；`Store.SaveGitStatus(status GitStatus) error` 仅语义变化写库。

`Command` 增加 `MainBranchOverride, CommitName, CommitEmail string` 用于 `update_project_git`。`queued/preparing/committing/committed/merging` 为活跃保留阶段，终态清除产品保留权；`needs_check` 不重放，实际 OS 租约和进程归属检查仍可阻止后续写入。

- [ ] 写失败测试 `TestGitStateBackwardCompatible`：旧 JSON／登录 cookie 仍有效，新增 map 非 nil，位置构造 Project 改为 keyed。
- [ ] 写失败测试 `TestMergeAdmissionAndReplay`：登记后 start/send/resume 被拒绝，同 ID 同内容 replay，同 ID 异内容冲突，别的任务不受影响；阶段 `queued/preparing/committing/committed/merging/succeeded/failed/needs_check`。
- [ ] 写失败测试 `TestMergeRestartNeverReplays`：进行中操作重开库后标 `needs_check`，保留预检及 SHA，不执行 Git；`TestUnchangedGitStatusDoesNotBumpRevision` 忽略仅 CheckedAt 的变化。

  核心断言：`if got.Merges[op.ID].Phase != "needs_check" { t.Fatal("restarted merge was not marked uncertain") }`；相同状态保存前后 `if after.Revision != before.Revision { t.Fatal("unchanged Git status bumped revision") }`。
- [ ] 运行 `go -C server test ./internal/workbench -run 'TestGit|TestMerge|TestUnchangedGit'`，确认新测试先失败。
- [ ] 实现上述类型／短事务；合并登记和回执原子保存，复用 operations 表幂等；保持现有 schema 1 JSON 兼容。设置通过 `update_project_git` command 保存，主分支值的 Git 有效性由任务 5 的服务端预检负责。
- [ ] 运行 `go -C server test ./internal/workbench`，要求全通过；只记录结果，不 Git commit。

### Task 2：主分支识别与实际状态检查

**Interfaces:** `gitops.DetectMain(ctx context.Context, source string) (string, error)`；`ResolveMain(ctx context.Context, project workbench.Project) (string, error)`；`ValidateWorktree(ctx context.Context, source, tree, branch string) error`；`Check(ctx context.Context, project workbench.Project, task workbench.Task, execution workbench.Execution) (workbench.GitStatus, error)`；`Preview(ctx context.Context, project workbench.Project, task workbench.Task, execution workbench.Execution) (workbench.MergePreview, error)`。

- [ ] 写临时仓库失败测试 `TestDetectMain`：本地 origin/HEAD→main→master→非任务当前分支，空仓库记未诞生分支，detached／任务分支不能误识别；冻结目标删除后不 fallback。
- [ ] 写 `TestGitStatusTracksActualHistory`：初始同 SHA=`no_changes`，修改／暂存／新文件=`dirty`，提交=`unmerged`，外部 merge=`merged`，之后修改／新提交恢复待处理；部分 merge／cherry-pick／squash 不误绿。
- [ ] 写 `TestGitStatusRejectsChangedOwnership`：验证原 owner.json 的 task/source/branch/BaseSHA/Ready、canonical 路径和 common dir；丢文件、切分支、链接重定向、未完成 Git 操作均 `unknown`／禁合并。
- [ ] 写 `TestPreviewBindsContentAndScope`：相同 status 下改内容使指纹不同；新文件、删除、暂存与工作树不同内容全部覆盖；换行／前导 `-` 正确限定；私密路径、服务目录、嵌套仓库、子模块和不支持的特殊文件不能被静默漏掉。

  测试文件名包括 `"two words.txt"`、`"line\nbreak.txt"`、`"--flag.txt"`；无效 UTF-8 必须返回禁用原因。断言 `if old.Fingerprint == fresh.Fingerprint { t.Fatal("changed content accepted under old preview") }`；状态机关键断言为 `if got.State != "dirty" || got.CanMerge && blocked { t.Fatal("incorrect dirty or blocked state") }`。
- [ ] 写 `TestGitInspectionDoesNotRunExternalPrograms`：恶意 hook/filter/driver/签名配置创建哨兵命令，检查与预检后哨兵不存在；需要外部工具的仓库返回具体禁用原因。
- [ ] 运行 `go -C server test ./internal/gitops` 确认先失败，再实现 NUL 解析、固定参数、剔除 GIT_* 环境、禁 hooks／prompt／pager／签名与交互、命令 20 秒超时。检查与指纹必须有文件／输出上限，超过时返回需核查，不截断后成功。
- [ ] 指纹覆盖主／任务 HEAD、branch、index／工作文件内容和模式、新文件集合、目标设置／身份；SHA-256，按 NUL 安全排序编码。预检 ID 在 Manager 生成，检查组件不保管授权。
- [ ] 在 `agent/workspace.go` 复用 ValidateWorktree，新工作区基线使用 ResolveMain；原 owner 的工作区优先复用。新建项目 `git init --initial-branch=main --template=`。
- [ ] 运行 `go -C server test ./internal/gitops ./internal/agent ./internal/webapi`，既有 worktree 假进程回归保持通过。

### Task 3：接管空闲写入者与仓库集成锁

**Interfaces:** Manager 增加 `reserveTask(ctx context.Context, taskID string) (release func() error, err error)`；内部只在已登记 MergeOperation 后调用。Linux 增加 `acquireProjectMergeLease(commonDir string) (*executionLease, error)`，锁和 owner marker 放在 canonical Git 公共目录的 `kaiwu-merge.lock`／`kaiwu-merge-owner.json`，使相同仓库不同入口或不同服务数据目录共享锁。

- [ ] 写 `TestMergeReservationRejectsBusyWriter`：starting/running／活跃 prompt 拒绝集成，不取消运行；空闲实例 Close＋Wait＋consume drain 后可接管原 execution.lock。
- [ ] 写 `TestMergeReservationPreservesSession`：集成释放后发送仍使用原 SessionID／SessionFile 和 worktree，不重放旧请求、不创建新上下文。
- [ ] 写 `TestProjectMergeLockCoversRepositoryAliases`：同 common dir 的两个入口互斥、两个卡片合并串行，其他 worktree 开发不被暂停。
- [ ] 写 `TestMergeLeaseRefusesOrphanOrUnknownOwner`：回收失败／旧进程组未退出／Group=0 未知窗口均拒绝新写入，绝不按存储 PID 杀进程。

  核心断言：`if _, err := acquireProjectMergeLease(commonDir); err == nil { t.Fatal("concurrent repository writer accepted") }`；恢复后的会话 `if after.SessionID != before.SessionID || after.SessionFile != before.SessionFile { t.Fatal("merge replaced original context") }`。
- [ ] 运行上述测试确认先失败；实现固定项目锁→任务保留权顺序。Manager.mu 不跨 Close／Wait／Git 调用；Store.mu 不调用 Manager。保留期间 begin 和产品 start/send/resume 双重拒绝。
- [ ] 将现有 executionLease 通用化路径选择但保留原格式兼容，Git 子进程组启动／回收由任务 4 调用 lease.attempt／started；两个租约都记录归属。非 Linux 返回执行不可用，仍允许只读状态检查。
- [ ] 运行 `go -C server test -race ./internal/agent`，要求原崩溃写入者保护与新增租约测试通过。

### Task 4：服务器持有的提交、合并与故障恢复

**Interfaces:** `Manager.PreviewMerge(ctx context.Context, projectID, taskID string) (workbench.MergePreview, error)`；`Manager.StartMerge(req workbench.MergeRequest) (workbench.MergeOperation, bool, error)`；`Manager.RecoverMerges(ctx context.Context) error`；`gitops.CommitTask(ctx context.Context, workspace, message, name, email string, runner Runner) (string, error)`；`gitops.MergeTask(ctx context.Context, project workbench.Project, preview workbench.MergePreview, taskSHA string, runner Runner) (string, error)`。

`Runner` 为 `func(ctx context.Context, dir string, args ...string) (string, error)`；Manager 提供有进程组、输出上限、20 秒超时和双租约归属记录的运行器，不使用请求 context 持有写操作。读检查使用 gitops 内部同样受限的运行器。

- [ ] 写 `TestMergeTaskEndToEnd`：脏任务经确认创建一次普通提交并快进；主分支分叉无冲突则生成合并提交；分支／worktree／原会话文件保留。
- [ ] 写 `TestMergeRejectsUnsafeMainAndStalePreview`：主目录脏／checkout 非目标／目标在别处 checkout／HEAD 或内容改变／过期预检均拒绝，不 stash／switch／reset／清锁。
- [ ] 写 `TestMergeFailurePreservesPartialResult`：提交成功而合并冲突／超时／外部改动时记录 TaskAfterSHA，后续重试不重复提交，主／任务文件保留；普通冲突在 merge-tree 检查阶段阻断。
- [ ] 写 `TestFirstMergeIntoUnbornMain`：空目录首次提交可进入未诞生 main；原项目种子文件／无关根历史明确阻塞且两边文件保留。
- [ ] 写 `TestMergeIdempotencyAcrossRestart`：同 operation ID 丢响应／过期预检／重启返回原操作；不同内容拒绝；注入各阶段崩溃，只有可由 SHA／实际历史证明的结果补完成，否则 `needs_check`，不自动执行 Git。

  核心断言：`if got.Phase != "succeeded" || status.State != "merged" { t.Fatal("actual merge not verified") }`；重试后 `if afterCommitCount != beforeCommitCount { t.Fatal("retry created another commit") }`；提交已完成的故障场景要求 `if got.TaskAfterSHA == "" { t.Fatal("lost committed task result") }`。
- [ ] 运行 `go -C server test ./internal/agent ./internal/gitops -run 'TestMerge|TestFirstMerge'`，确认先失败，再实现预检缓存：随机 ID、5 分钟有效、最多 256 个、服务器端保存内容；StartMerge 先查既有操作再验证缓存，登记后在服务器 owned context 执行。
- [ ] 依任务 3 接管写入者后重新算指纹；提交前记录 `committing`，成功持久化 SHA 后再 `merging`。CommitTask 按确认范围 Git add、普通 commit（显示身份和说明），不得使用 shell 或 `-a` 漏新文件；前置核对暂存／未暂存范围仍一致。
- [ ] MergeTask 对固定 SHA 检查祖先关系／merge-tree，再复核干净主目录，普通 merge 强制 `--ff --no-edit --no-gpg-sign` 并禁配置覆盖；不自动合并无关历史。最终调用 Check 决定实际状态，不仅看 exit 0。
- [ ] Manager.Close 取消／回收 Git 进程组、等工作单元退出后才关闭 Store；仅可靠回收后移除归属 marker。重启 RecoverMerges 只核查，不重新执行；审计保存预检和结果 SHA，不记录凭据或完整命令输出。
- [ ] 运行 `go -C server test -race ./internal/agent ./internal/gitops ./internal/workbench`，要求全部通过。

### Task 5：认证 API、分支设置与有界检查生命周期

**Interfaces:** webapi 增加 `GitCommander` 接口：上述 PreviewMerge／StartMerge，加 `RefreshGit(ctx context.Context, projectID string) error`、`UpdateProjectGit(ctx context.Context, command workbench.Command) (workbench.Receipt, error)`。通过 `Config.Git GitCommander` 注入，避免破坏既有 Commander fake。在 app.Run 绑定 manager，重启检查纳入 manager 生命周期。

API：`POST /api/git/refresh {projectId}`；`POST /api/git/preview {projectId,taskId}`；`POST /api/git/merge MergeRequest` 返回 202 与操作；`GET /api/git/operations/{id}`；`/api/commands` 的 `update_project_git` 走服务端 Git 验证再短事务保存。Git 能力不要求配置模型；写集成限 Linux。

- [ ] 写 `TestGitEndpointsRequireAuthAndOrigin`：未登录、跨站、未知字段、跨项目 task／operation 均拒绝；客户端任意路径／SHA 不被接受；未配置模型仍可检查／合并。
- [ ] 写 `TestProjectGitSettingsOptionalAndValidated`：空覆盖沿用冻结默认，合法自定义本地分支可用，非法／任务分支／不存在目标拒绝，改配置使旧预检失效，原任务基线不动。
- [ ] 写 `TestGitRefreshIsBoundedAndChangeDriven`：相同项目共享正在执行的检查，全服务最多 2 个检查工作单元、每项目最多 1 个；超时／目录不可访问及时返回，不循环增长 revision；不同聊天命令仍能保存。
- [ ] 写 `TestMergeOutlivesRequestAndStoreClosesLast`：确认后断开 HTTP／WS 不取消，应用停止才结束工作单元；已登记但尚未派发的操作在重启后可查询且不自动重放。

  核心断言：无模型确认接口 `if response.Code != http.StatusAccepted { t.Fatal("Git operation incorrectly requires model") }`；只读刷新稳定时 `if after.Revision != before.Revision { t.Fatal("polling wrote unchanged state") }`；跨项目 `if response.Code != http.StatusNotFound { t.Fatal("foreign task exposed") }`。
- [ ] 运行 `go -C server test ./internal/webapi ./internal/app -run 'TestGit|TestProjectGit|TestMergeOutlives'` 确认先失败，再实现接口／路由／snapshot Git map／capabilities。刷新只接受已登记项目，默认分支首次识别保存失败时不继续集成。
- [ ] 运行 `go -C server test -race ./internal/webapi ./internal/app`，要求全通过。

### Task 6：轻量状态、合并审核与聊天入口

**Interfaces:** 新 `git.ts` 导出 `isMergeRequest(text: string): boolean`、`gitStatusLabel(status?: GitStatus): string`；扩展 types.ts 对应任务 1 字段与 MergePreview／MergeOperation。`MergeDialog.vue` props 为 `preview, taskTitle, saving, operation?, error`，events 为 `close`、`confirm: [commitMessage: string]`；`GitStatus.vue` 只负责文字／原因／检查时间，不发起 Git。

- [ ] 在 workbench.test.ts 写失败测试：明确短语仅允许“请”前缀及末尾 `。！!.`；否定、问号、引用、多行和长描述不触发；状态标签六种完整且缺值不显示已合并。

  核心断言：`assert.equal(isMergeRequest("请提交并合并到主分支。"), true)`、`assert.equal(isMergeRequest("不要合并到主分支"), false)`、`assert.equal(isMergeRequest("合并到主分支？"), false)`、`assert.equal(isMergeRequest("合并到主分支\n"), false)`。识别前只允许首尾水平空格，不把换行去掉后误当命令。
- [ ] 在 browser.spec.ts 写失败场景：卡片／聊天图标打开同一审核框；有修改显示“提交并合并”及身份／范围；取消不提交，确认后实际主分支出现成果，任务独立目录仍在。
- [ ] 写浏览器场景：聊天指令不调用 pi prompt；无卡片提示选择；确认操作目标冻结，切卡片／切项目／注销后的晚响应不污染新范围；重复点击禁用，失联查询同 ID。
- [ ] 写浏览器场景：外部 Git merge 后 3 秒检查周期内显示已合并；新增修改恢复待提交；未配置模型仍能查看 Git；冲突／主目录脏显示原因；高级设置默认收起且添加项目无必填分支。
- [ ] 运行 `npm --prefix apps/web test`，先确认新增纯状态测试失败，再实现 git.ts／GitStatus／MergeDialog 与小型 SVG 合并图标。确认框仅展示文本／文件名，不扩展源码或 diff。
- [ ] 在 App.vue 接入按 project/authEpoch 的刷新循环：setTimeout 3000 ms，不重叠；窗口隐藏／注销／卸载取消，重新可见／项目变化立即请求；snapshot 单调合并，旧范围结果丢弃。任务执行／合并阶段结束触发刷新。
- [ ] 合并聊天拦截放在模型可用性判断之前；预检失败保留原草稿，确认接收后仅清原任务草稿。确认框关闭不终止已登记操作，可从任务处重新查看阶段。高级设置支持主分支覆盖和提交身份，保存错误保留输入。
- [ ] fixture 浏览器服务提供已提交 main 及独立合并样例，测试不依赖真实项目／全局 Git 身份；保留现有空仓库启动场景及 15 项 Markdown／聊天回归。
- [ ] 运行 `npm --prefix apps/web test`、`npm --prefix apps/web run build`、`npm --prefix apps/web run test:browser`，要求全通过；窄屏／键盘焦点／icon title 可用。

### Task 7：整体核验、文档与固定端口预览

- [ ] 运行 `GOTOOLCHAIN=local GOCACHE=/tmp/kaiwu-go-cache GOMODCACHE=/tmp/kaiwu-go-mod-cache go -C server test ./...`；必要的本地 socket 检查使用已授权环境，区分权限失败与产品失败。
- [ ] 运行同环境 `go -C server test -race ./...` 和 `go -C server vet ./...`；`gofmt -l` 必须无新增不合规文件。前端已通过后仅在改动／失败时重跑，不无理由反复测试。
- [ ] 使用 `go build` 验证 Windows amd64／macOS arm64 编译，输出到临时目录；仅报告编译，不能代替跨平台 UI 或 Linux 写入运行验收。
- [ ] 安排独立只读验收，重点检查任务 1–6 的锁顺序、真实 Git 判定、提交范围、崩溃记录、原会话保留与预检 TOCTOU；不分派并发实现。按项目规则需要外部 Agent 时先读 Paseo 编排规则和实际 profiles，不猜模型。发现问题先补失败测试再修复。
- [ ] 写 `docs/development/web-task-merge-verification.md`，逐项对应规格验收表，注明通过／限制／未验证，记录实际命令与结果；更新 README／路线图／架构／需求／AGENTS 和 Web 运行说明，不将 Git 集成等同模型验收。
- [ ] 前端生产构建后再构建 Go 到新二进制，沿用已授权的 7331、现有令牌和数据库，确认旧服务归属后优雅重启；不覆盖正在运行的二进制，不输出令牌，不在真实项目执行测试合并。
- [ ] 局域网登录／刷新／状态显示 smoke check，真实模型仍不调用；给用户访问地址和实际完成范围。保留团队协作、通用编排引擎为未来方向，本批不扩大范围。

## 实施前自审结论

规格 1 对应任务 1／2／5／6，规格 2 对应任务 2／6，规格 3 对应任务 4／5／6，规格 4–5 对应任务 3／4，规格 6 对应任务 1／4／5／6，规格 7 对应各任务测试及任务 7。五项 Review Focus 已分配具体测试，无未归属的实现子系统。

执行方式保留当前 Agent 串行实现＋独立只读验收。本计划已获用户审核并按此方式执行；不要求再次选择已确定的方法。开悟当前仓库未提交／合并，7331 运行记录见验证文件。
