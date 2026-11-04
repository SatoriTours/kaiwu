# 原生 systemd 托管执行实施子计划

> **For agentic workers:** 使用 `executing-plans`；当前实现负责人连续完成、自测和修复，独立验收完整切片。步骤用于追踪，不代表逐文件派发、建立新 worktree 或授权提交。

**Goal:** 为原计划 C 建立 Go 实现的统一受管执行边界；在真实隔离、归属、资源及网络门禁全部满足后，才能提议开放 managed 项目。

**Architecture:** Linux 用户级 systemd 瞬态单元负责 namespace、只读根和 cgroup；Go `executionenv` 统一准入、生命周期和归属，Go `execbridge` 在单元内承接 pi RPC、PTY 或动态预览。保留原会话文件／任务 worktree／业务 WebSocket，不增加 pi 扩展或 JS 宿主。宿主模式只供原 legacy 项目，不是沙箱失败的降级路径。

**Tech Stack:** Go，现有 SQLite、`golang.org/x/sys`、systemd 用户管理器、现有 pi RPC、Vue 3。

**Spec:** `docs/superpowers/plans/2026-11-01-managed-projects.md` 的设计基线、全局约束及阶段 C；A/B 现有行为继续有效。

**状态：独立设计修订检查通过，门禁内首批 Go 基础代码已实现／自测，基础层独立限定复验通过。** 先前“继续”只批准计划细化；用户随后“你继续完善”后推进纯准入、systemd 参数、只读配置加载及只读磁盘核查。尚未接入实际执行，不视为安装、存储改造、额外现场单元或产品部署授权。原 C 六项验收要求均保留；下述顺序不把局部基础层改称 C 完成。

## 全局约束与已经掌握的事实

- 保留当前 main／checkout，单实现写入者；不迁移现有项目、DataDir、SQLite，不提交、推送、安装、重启或修改 `.91:7331` 服务。
- `.91` 的 systemd 261 用户瞬态单元已实测有限私有 root/PID/network、NNP、capabilities=0、CPU 节流、96MiB OOM、任务数上限和整组退出；只用了静态合成 Go payload，不等于 pi／SDK／PTY 兼容或 C 全面安全验收。
- PrivateUsers 已配置，未独立读取 uid_map；ProcSubset=pid 隐藏 `/proc/net`，不能原样设为产品默认。隔离后的完整私有 proc 为建议基线，必须确认不露出宿主 PID，sysctl／cgroup 不可写。禁止为兼容改回宿主 proc/network。
- 最多 8 个驻留 pi 会话；切范围、关浏览器不释放任务。未要求自动空闲回收；不因公平排队擅自驱逐空闲 pi。
- 配置快照、批准 SKILL.md、所选提供商凭据继续 B 的范围；规则或任务文本不能扩大挂载／网络／凭据。跨提供商显式切换须在本轮结束后切换，不打断当前模型请求。
- CPU、内存、任务数与磁盘预算必须是实际内核／存储约束；不把 RLIMIT_FSIZE、I/O 限速、软统计或已知主机总容量当作磁盘硬总额／独占预算。
- 本子计划不开放生产 ManagedProjects，不新增启用环境变量；真实模型及现场部署须另说明／授权。非 Linux 托管服务器 fail closed，不影响 PC 三平台和 Android 客户端目标。

## 实施前门禁：哪些可以编码，哪些尚未具备

| 门禁 | 当前状态 | 处理方式 |
|---|---|---|
| namespace／CPU／内存／pids | 有限现场证据通过 | 实现真实检查和启动后核验，不能照搬一次成功常量 |
| 磁盘总量硬限额 | **阻塞** | 生产准入必须拒绝；先另行批准存储专项预检，不能写一个永远返回支持的适配器 |
| 受控模型出口／SDK | **未实现／未验** | 默认 deny-all；外部真实任务不得以离线夹具替代联网能力验收 |
| 原生运行时与控制桥 | **未实现** | 先隔离临时目录用 fake pi／本地 HTTP stub，不能读取个人配置 |
| 完整两项目安全验收／部署 | **未完成** | 由独立验收核关键路径；部署和开放仍单独授权 |

门禁内可以实现“拒绝不满足条件的真实准入＋执行基础层＋隔离夹具”；这不构成整 C 可交付。若磁盘／出口仍阻塞，汇报 C 未完成，不仅勾选软件测试。

### 磁盘专项的有限选项

首选由管理员提供**项目级持久硬配额**。当前 Btrfs 只有文件系统事实，没有 quota 启用／权限／可用预算证据；普通目录不是独立 qgroup。后续只读核查须先确认子卷归属、配额是否已启用、是否能为新项目分配且禁止任务更改上限、快照／共享 extent 的计费方式；不能自动把 A 的普通目录变成子卷或迁移旧数据。

若没有现成能力，再集中确认“管理员准备新项目的有界存储”或“另批持久镜像／FUSE 等组件”；不静默切换、不先开启整盘 quota。每项目硬上限必须覆盖其所有可写 repo/worktree、HOME、session、日志、附件／产物、缓存及 tmp，临时 root／控制桥 spool 也须有独立硬上限；不能把写入转移到未计费全局目录绕过。预算数值由明确全局配置提供，不从可见 46.8GiB 内存推导默认磁盘额度。

## 已核实的代码落点与新职责

| 文件 | 计划改动 |
|---|---|
| 新增 `server/internal/executionenv/{types,admission,spec,ledger,scheduler}.go` 及各自 `_test.go` | 统一能力、可信挂载／预算、持久归属、项目公平准入；不成为通用容器插件框架 |
| 新增 `executionenv/{systemd_linux,systemd_other,systemd_args,inspect_linux,gitview_linux}.go` 及测试 | 有界 systemctl/systemd-run 适配、identity 检查、只读公共 Git 视图；其他 OS 拒绝 |
| 新增 `server/internal/execbridge/{server,bootstrap_linux,protocol,pi,pty_linux,preview}.go` 及测试 | 单元内 Go supervisor；有界鉴权 IPC、stdio、终端 resize 和预览转发；不承载产品业务状态 |
| `server/cmd/kaiwu-server/main.go` | 在普通服务初始化前识别内部 bridge 子命令；只有可信 bootstrap FD，不从浏览器接收任意挂载／unit property |
| `server/internal/pirpc/{types,process,client,requests}.go`，新增 `managed_transport_test.go` | 分离 RPC 协议与进程后端，保留原 Start；接入可重新连接的受管 transport |
| `server/internal/agent/{launch,manager,lease_linux,lease_other,workspace}.go`，新增 `sandbox_test.go`、`sandbox_recovery_test.go` | 构建可信 Spec；managed 不走宿主；unit 归属替代“代理进程 PID=真实写者”的假设 |
| `server/internal/projecttools/terminal{,_linux,_other}.go`，新增 `managed_terminal_test.go` | PTY 在单元内创建；浏览器断线不停止，实际 resize 和退出状态走桥 |
| `server/internal/preview/{manager,process_linux,process_other}.go`，新增 `managed_preview_test.go` | 动态命令在沙箱，私有 loopback 由桥转发；static/files 不启动多余进程 |
| `server/internal/gitops/{command,workspace,merge,candidate}.go` 及安全回归 | 可信 Git 不信任任务可写配置／指针／hooks；保留显式审核提交合并 |
| `server/internal/workbench/migration.go`，新增 `runtime.go`／`runtime_test.go`；`types.go` | 新增运行归属表／准入视图，不把 SQLite 改成 AI 配置真源；按实施时最新 schema 递增，避免抢其他成果的版本 |
| `server/internal/config/workbench.go`，新增 `execution.go`／`execution_test.go`；`app/app.go` | 从全局 configRoot 的 sandbox.json 读管理员预算／批准工具链／出口声明，注入共享 controller；不加载项目提供的服务配置 |
| `server/internal/webapi/{project_tools,preview,socket}.go` 及相关测试；`apps/web/src/{types.ts,App.vue}` | 沿现有业务通道暴露队列／阻塞原因；不增固定看板状态列或独立连接协议 |
| 新增 `server/internal/executionenv/network/{policy,broker,tls,download}.go` 及测试 | 仅按批准的目的与类型开放出口；与默认 deny-all 一并验收，不提供通用任意 CONNECT 隧道 |

修改前再核对共享 diff；准确方法名以上述拟新增契约为准，不盲用本文行号或覆盖其他迁移。实现中若发现必须改变已批准接口／安全取舍，集中报告，不借此重构无关 subsystem。

## 最小契约（拟新增）

```go
// executionenv；不依赖 agent / pirpc / preview，避免循环依赖。
type Mode string // 只接受 "legacy-host" / "managed-sandbox"
type Owner struct { ProjectID, TaskID, ConversationID, ExecutionID string }
type Identity struct { RunID, BootID, Unit, InvocationID, ControlGroup string }
type Budget struct { CPUQuotaUSec, MemoryBytes, PidsMax, DiskBytes int64 } // CPU 以100ms周期计
type MountGrant struct { Source, Target string; ReadOnly bool; Device, Inode uint64 }
type NetworkGrant struct { Mode, PolicyID, PolicyVersion string } // deny / broker
type Spec struct {
    Owner Owner
    Kind string // pi / terminal / preview；不接受任意客户端类型
    Program, Dir string
    Args, Env []string
    Budget Budget
    Mounts []MountGrant
    Network NetworkGrant
    WriterLease *os.File // 可信端持有的现有flock，非JSON输入；只读讨论可nil
}
type Observation struct {
    State string // owned-running / owned-gone / unknown
    Identity Identity
    ActualBudget Budget
    GroupPopulated bool
}
type Transport interface {
    Stdin() io.WriteCloser
    Stdout() io.ReadCloser
    Stderr() io.ReadCloser
    Detach() error
}
type Exit struct { Code *int; Reaped, GroupGone bool; Err error }
type Controller interface {
    Check(context.Context, Spec) error
    Start(context.Context, Spec) (Handle, error)
    Inspect(context.Context, Identity) (Observation, error)
    Attach(context.Context, Identity) (Handle, error)
    Stop(context.Context, Identity) (Exit, error)
}
type Handle interface {
    Identity() Identity
    Streams(context.Context) (Transport, error)
    Resize(context.Context, uint16, uint16) error // 非 terminal 拒绝
    Wait(context.Context) (Exit, error)
}
```

契约分别使用标准库 context/os/io；`Transport` 提供独立 stdin/stdout/stderr，终态从 Handle.Wait 获取，不是把关闭 stdout 当 Stop。`MountGrant` 与 `NetworkGrant` 只能来自可信 builder／批准策略，禁止客户端原样输入或无类型 property map。`RunID` 是每次进程生命期的新身份，不能和可跨重启保留的业务 ExecutionID/SessionID 混用。

## 任务 1：真实准入与失败关闭

- [ ] 在 `executionenv/admission_test.go` 添加 `TestManagedNeverFallsBackToHost`、`TestMissingHardDiskOrNetworkBlocksBeforePrepare`、`TestLegacyKeepsOriginalPathAndSession`。缺能力在复制凭据、创建工作区／进程前拒绝；只有明确的只读能力查询可执行。
- [ ] 红测：`timeout 120 go -C server test ./internal/executionenv ./internal/config -run 'ManagedNever|MissingHard|LegacyKeeps' -count=1`。初次缺类型的编译失败只记脚手架红，随后需留行为红。
- [ ] `Check` 只做无副作用的能力／授权／预算准入，不要求尚未准备的运行材料存在；`Start` 再固定全部文件身份并核验，不把凭据复制放在首轮准入前。实现严格 sandbox.json schema／绝对路径／正数预算／未知字段拒绝。全局生产预算必须明确配置；96MiB/25%/32 只用于已验证的探针，不当真实模型默认。配置不扩大项目 A/B 批准范围。
- [ ] 实现非 Linux／无 user manager／控制器缺失／未知 quota／未批准出口时的可操作错误；没有开关能从 managed 切 legacy。单元测试用注入能力数据，不宣传为真实存储适配器。
- [ ] 同命令绿测，绑定测试 hash；全量 C 完成前保持外部门禁关闭。

## 任务 2：原生单元与可信启动

- [ ] 先测 `TestUnitArgsAreNotShell`、`TestPercentSpaceUnicodePaths`、`TestMountReplacementFailsBeforeUntrustedExec`、`TestStartTimeoutDoesNotLaunchTwice`。覆盖 systemd specifier `%`、空格、引号、冒号与非法路径；不能只保证 Go argv 没经 shell。
- [ ] 红／绿命令：`timeout 120 go -C server test ./internal/executionenv -run 'UnitArgs|PercentSpace|MountReplacement|StartTimeout' -count=1`。
- [ ] 有界 argv 直接调用现有 systemd 工具，不动态 shell 拼接；禁用 unit 命令环境扩展，静态 Go bridge 最先禁止 dump、清除自身继承环境，子进程只使用 Spec 白名单；不能误以为 `Environment=` 空赋值会清空 user manager 环境。凭据、指令、IPC secret 不放 argv、unit 属性或 journal。
- [ ] 每次 RunID 派生全新 unit 名，先持久保留启动意图；unit 使用私有 root/user/PID/net/mount/devices、NNP、cap-drop、只读 system/cgroup/sysctl、cgroup 预算及 KillMode=control-group。长寿命 pi 不套探针 20s RuntimeMax；只给启动／停止／调用设置有界 timeout。
- [ ] 静态可信 Go bridge 先运行，核对所有授权挂载的 dev/inode、实际只读标记及 namespace，再允许执行任务程序。Root、bootstrap／control 及 Go bridge 本体来自任务不可写位置；攻击者替换工作区路径只能使启动失败，不能在验证前运行其程序。
- [ ] 镜像绝对路径时不能照搬探针的 ProtectHome=yes：该项会使 /home 下的白名单嵌套 bind 也不可达。拟用 ProtectHome=tmpfs 的空只读 HOME 父层，仅显式绑定批准工具链／本会话子路径，绝不改成整个 HOME read-only；必须在目标261版本上实测该组合，不支持就阻塞。工具链只读白名单绑定真实 Node/pi 包、批准 shell/Git/Go 及必需公共 loader/libs；不挂整个 HOME、用户 npm/Go 缓存、Docker socket、systemd bus、全局 SQLite 或个人扩展。每项目缓存入其计费写区；禁止用全局可写缓存泄漏其他项目源码。
- [ ] kernel 校验不满足、InvocationID 不明或启动回执丢失：记 unknown，先 Inspect；不再次启动、不 fallback、不删材料。实际现场组合验证仅在专项授权后运行。

## 任务 3：持久归属、可接回的 Go 控制桥

- [ ] 先测 `TestPIDReuseNeverStopsForeignUnit`、`TestCrashBetweenStartAndReceiptKeepsUnknown`、`TestLeaseSurvivesHostDeath`、`TestAttachDoesNotReplayPrompt`、`TestBridgeOverflowCannotInventSuccess`；覆盖“代理退出但 unit 后代仍活”和主机 boot 改变。
- [ ] 红／绿命令：`timeout 180 go -C server test ./internal/executionenv ./internal/execbridge ./internal/workbench -run 'PIDReuse|CrashBetween|LeaseSurvives|AttachDoes|BridgeOverflow' -count=1`。
- [ ] SQLite 新表记录 RunID、owner、Spec hash、unit、BootID、InvocationID、cgroup、phase 与版本；starting→running→stopping→stopped 或 unknown。物理 SessionID/SessionFile 不替换。记录不完整保留，不能认定“进程不存在”。
- [ ] 不能把 `systemd-run --pipe` 的短寿命代理 PID 写成任务 PID；单元内 Go bridge 提供有界 Unix IPC 和实际子进程状态。停止须匹配持久 Identity；整个所属 cgroup 空且已退出才释放写入锁和计费槽。
- [ ] 按下文 I1 启动事务实现：初始只有只读公共 bootstrap，主机验 MainPID/pidfd/cgroup，bridge 验固定公钥签名；鉴权后才 SCM_RIGHTS 交接配置、租约和私有 spool FD。禁止假设 ExtraFiles 能穿过 systemd manager。宿主签名私钥只放 DataDir 私有 runtime（0600、不进 AI 配置／浏览器／日志／任务挂载）；bridge 最早设置 RLIMIT_CORE=0、PR_SET_DUMPABLE=0。
- [ ] 写锁用鉴权 Unix SCM_RIGHTS 交给 bridge 持有同一 open-file description，无“先解锁再重开”空档；宿主副本与单元副本可并存。bridge 关闭后仍要核 cgroup，不能只凭 flock 可取判定无后代。legacy 保留原 PGID lease，不跨模式复用其语义。
- [ ] bridge 不承载模型推理；持续消费 RPC stdout，使用有界序号 spool（物理文件放项目硬配额内但不挂给任务，宿主鉴权后仅交给 bridge 文件 FD），宿主只 detach 连接不 kill。重新连接从已确认水位恢复；溢出／缺口明确 interrupted/unknown，不补造成功／完成结果，不重发未确认 prompt。
- [ ] `pirpc` 保留现有 Start 及 legacy 生命周期，新增 `StartWithTransport(ctx context.Context, t executionenv.Transport, opts Options) (*Client, error)`；managed 不复用 legacy process.stop 的 closeStdin/kill 语义，Detach 不向真正 pi stdin 传播 EOF，仅显式 Stop 经 controller 结束单元；连接 epoch 区分 RPC request IDs，迟到旧 response 不匹配新请求。启动回调区分首次 Start 与 Attach，不重复登记新进程。原业务 WebSocket／operationId 幂等继续适用。
- [ ] 服务启动只 Inspect，不启动模型或重发历史意图；新消息／继续开发才自动 Attach 或在旧资源确认退出后以原 SessionFile 重新 Start。旧资源仍 active 时接回桥，不擅自杀掉正在进行的模型请求。

## 任务 4：挂载、Git 与凭据的闭合边界

- [ ] 先测 `TestManagedCannotReadSiblingProject`、`TestGitViewCannotWriteCommonMetadata`、`TestTrustedGitIgnoresTaskHooksAndConfig`、`TestOnlySelectedCredentialMountExists`、`TestDiscussionReadOnly`，包含目录／链接替换和 `/proc/*/fd` 路径。
- [ ] 红／绿命令：`timeout 180 go -C server test ./internal/executionenv ./internal/gitops ./internal/agent -run 'ManagedCannot|GitView|TrustedGitIgnores|SelectedCredentialMount|DiscussionReadOnly' -count=1`。
- [ ] 尽可能镜像原绝对路径，维持原 session/worktree 内容引用；只读讨论仅挂本项目 repo 只读与自身会话。开发只挂自己的 task tree 可写、该会话 HOME/JSONL、批准快照/附件、独占 tmp／cache；不挂整个项目容器或 sessions 上层。
- [ ] 会话目录若包含 B 的旧 agent 子目录，必须用本轮独占凭据视图遮蔽，不因父目录 RW 露出旧提供商。仅所选提供商 auth/models 进入当前视图；auth refresh 写入也受预算。终端和预览默认不挂模型凭据。
- [ ] 按下文 I2 的精确映射实现；worktree `.git` 指向的公共目录不能整体可写挂载。构造只读公共 objects/refs 的最小 Git 视图、独占可写 index/本 worktree admin；私有 admin 是副本，绝不反向同步到可信 Git 元数据。不得借“git 包装命令禁止”冒充权限边界。
- [ ] Git status/diff 和私有 index 可用；git add 新 blob 写入私有 object store（GIT_OBJECT_DIRECTORY），canonical objects 仅作只读 alternates，否则只读 common 会让 add 无法正常工作。直接修改公共 refs/objects、提交／worktree 管理被 OS 拒绝；原显式审核→后台提交合并仍处理真实工作区文件。可信后台以冻结身份／路径运行 Git，屏蔽不可信全局/系统配置、hooks、外部 diff/textconv/fsmonitor；不能采信任务改写的 `.git` 指针、config.worktree 或标记作为授权。
- [ ] 同提供商模型变更沿用下一轮机制；跨提供商切换在旧轮结束、旧 unit 核退后，以原 SessionID/SessionFile 和新单提供商视图重建，保留原槽与写锁。说明这是显式配置调整，不是自动空闲回收。未授权技能资源不额外挂载。

## 任务 5：RPC、PTY、预览共用边界与预算

- [ ] 先测 `TestManagedRPCUsesBridge`、`TestTerminalPTYPrivateAndResizeWorks`、`TestPreviewOnlyViaAuthenticatedRoute`、`TestClosingBrowserDoesNotStopUnit`、`TestStaticPreviewStartsNoUnit`。
- [ ] 红／绿命令：`timeout 180 go -C server test ./internal/pirpc ./internal/agent ./internal/projecttools ./internal/preview -run 'ManagedRPC|TerminalPTY|PreviewOnly|ClosingBrowser|StaticPreviewStarts' -count=1`。
- [ ] 在实际三处直接 exec 入口替换 managed 分支；pi 的 bash/build 后代天然留在单元 cgroup。终端 PTY 在单元私有 devpts 内创建，resize 经桥 ioctl，Stop 验真实整组退出；保持原 terminal writer-group 授权，不让终端与 pi 意外并发写同 tree。
- [ ] 动态预览只监听私有 loopback；Go bridge 只转发已审核 manifest 对应子进程端口，宿主经现有认证预览入口访问，不加宿主监听端口／host network。不提供任意 URL、任意端口的桥转发 API。保留 GET/HEAD、无 WebSocket/HMR/登录等原边界；static/files 沿原安全文件路径，不拿额外写锁。
- [ ] pi／终端／预览是独立可查资源，预算按实际存活单元计，不因“模型报告完成”或关闭页面退还；项目硬配额覆盖其总写入，避免拆进程乘算预算。异常退出仍不抹掉已有需要确认结果。

## 任务 6：受控出口（设计提案，须纳入本次方案确认）

- [ ] 先测 `TestDefaultNetworkDenied`、`TestDNSRebindingAndRedirectDenied`、`TestConnectCannotDomainFront`、`TestTaskCannotExpandProviderPolicy`、`TestModelSDKWithSyntheticHTTPS`、`TestDownloadsNeedSeparateGrant`；禁止直连宿主、RFC1918、link-local、metadata、Unix socket 转义及不受控代理。
- [ ] 红／绿命令：`timeout 180 go -C server test ./internal/executionenv/network ./internal/execbridge -run 'DefaultNetwork|DNSRebinding|ConnectCannot|ExpandProvider|SyntheticHTTPS|DownloadsNeed' -count=1`。
- [ ] 方案建议：私有网络只经本 invocation 的只读 Unix socket 到可信 Go 出口 broker，单元内 Go bridge 暴露 loopback HTTP proxy 给支持代理的工具。outer broker 绑定实际 unit/peer 身份；Unix socket 本身不是任意网络能力。
- [ ] 模型与 OAuth refresh 用冻结的提供商 origin＋method/path 策略，按下文 I3 逐请求校验；下载改为可信 Go fetcher 的精确不可变 grant，不向模型单元提供通用 registry／下载代理；项目配置不能扩大。DNS 在可信端解析、过滤所有 IPv4/IPv6／映射地址并固定实际 dial 目标；连接与重定向每次重验，拒绝私网／本机目标。未支持提供商必须报不支持，不盲从模型文件内任意 baseUrl。
- [ ] **不允许盲转 CONNECT：** 加密隧道仅检查 CONNECT 主机不能保证 HTTP Host／SNI，CDN domain fronting 可越过声明边界。拟在 Go broker 终结此受控 TLS，再向批准 origin 做正常 TLS 验证；每 invocation 公共 CA 只注入该会话／工具 trust 配置，私钥仅放 DataDir 私有 runtime 文件并绑定 RunID，以便宿主恢复后继续服务原信任证书，绝不挂给任务、安装到宿主／浏览器／全局 trust store。不记录 Authorization、Cookie 或请求正文。
- [ ] 按 I3 增补 Host/SNI 冲突、路径／query 规范化、下载凭据泄漏和重定向负测。网络代理不记录或 spool 任何请求／响应内容，仅保存策略 ID、状态码、计数与错误码；RPC 项目内 spool 不宣称具有任意秘密 DLP 能力。
- [ ] 这是一项需要确认的安全取舍：若不接受会话级 TLS 代理，或某提供商 certificate pinning／SDK 忽略代理／trust 配置，保持该提供商 blocked，另议原生 API gateway；不暗自扩大 host network、关闭上游证书验证或允许通用隧道。
- [ ] 编码前定位目标已安装 Node/pi 版本及实际 SDK／CLI 接口，用隔离 HOME、合成凭据与本地 HTTPS provider stub 验 RPC、SSE、refresh 和取消；Node 声称支持 proxy flag 不是 SDK 验证。真实模型另说明额度后获准再测。

## 任务 7：项目公平准入与最终独立验收

- [ ] 先测 `TestResidentLimitIncludesIdleCLI`、`TestProjectRoundRobinFIFO`、`TestBudgetNotReleasedBeforeGroupGone`、`TestQueueSurvivesSocketReconnectWithoutReplay`、`TestServerRestartDoesNotReplayQueue`。
- [ ] 红／绿命令：`timeout 180 go -C server test ./internal/executionenv ./internal/agent ./internal/webapi -run 'ResidentLimit|RoundRobin|BudgetNotReleased|QueueSurvives|DoesNotReplayQueue' -count=1`。
- [ ] 维护驻留 pi 上限 8，与 CPU/内存/pids 预留总预算分别核验；终端/预览计入资源总预算而不是绕过它。非空项目队列 round-robin，项目内 FIFO；轮到的大请求暂不满足预算时保留顺序等待，不让后续小任务无限绕过。驻留槽未释放时队列可能暂停，明确说明不保证在无人释放资源时仍可推进。
- [ ] 排队请求按 operationId 去重，只有准入后才发 prompt。关闭浏览器不撤销队列；显式取消可撤销尚未发出的请求，已不确定发送的不假称撤回。服务重启把未发送队列规范化为需新意图，不自动重放；收到新消息／继续后重新准入，不重复历史 prompt。
- [ ] 沿现有 socket 状态投影显示等待／阻塞原因，卡片仍用既有状态角标，未准入不假显示正在执行；提供取消等待，不增加固定状态列。新增 UI 若需要新组件或改变已确认布局另说明。
- [ ] 补齐全量／race／前端与现有 browser 回归；新增 managed sandbox browser fixture 验队列、切项目、断线、取消、结果确认不串域。截图用外部目录，390/1440 实际交互及视觉分别验。
- [ ] 获批现场验收后，创建两个本任务真实临时 Git 项目同时运行无害任务；验证跨项目读写/HOME/进程/symlink/Git admin/代理越权均拒绝，允许目录、RPC、PTY resize、预览可用；实触 CPU、OOM、pids、**磁盘总额**和并发预算。不能只重跑静态 helper 或 fake 能力报告。
- [ ] 宿主 Go 服务崩溃/重启的受控夹具必须证明原 unit 归属、租约、事件缺口和 Attach／原 SessionFile 继续行为；无需修改现用 kaiwu-server。未知 unit 保留阻塞，不批量杀进程。
- [ ] 独立验收关键功能／负面安全路径／必要回归，主控核 diff/hash。只有硬磁盘预算、出口、运行时和两项目验收均真实通过，原计划 C 才可勾完成；之后仍需另批生产开放／部署。D/E 不自动开始。

## 验证命令与证据口径

实现受影响包时以各任务红绿为起点，不在缺包时把不可执行命令宣称通过。收口按当时实际包名运行：

```sh
timeout 240 go -C server test ./...
timeout 240 go -C server test -race ./internal/executionenv/... ./internal/execbridge/... ./internal/pirpc ./internal/agent ./internal/projecttools ./internal/preview ./internal/gitops ./internal/workbench ./internal/webapi
timeout 120 go -C server vet ./internal/executionenv/... ./internal/execbridge/... ./internal/pirpc ./internal/agent ./internal/projecttools ./internal/preview ./internal/gitops ./internal/workbench ./internal/webapi
timeout 180 npm --prefix apps/web test
timeout 180 npm --prefix apps/web run build
```

真实 systemd 集成用例必须显式 opt-in（仅测试读取 `KAIWU_TEST_SYSTEMD=1`、明确目标及临时资源上限），默认 go test 不启动现场单元；跳过不能记成 OS 验收通过。浏览器复用既有 Go/SQLite 夹具并新增本任务 sandbox 配置，不盲改共享 Playwright 端口。单元／协议测试、真实 OS 集成、视觉、真实模型、最终部署分栏记录；环境／授权不足记阻塞，不用内部测试替代。单份状态索引继续为 `~/.local/state/agent-artifacts/kaiwu/2026-11-01-managed-projects/status.md`，保存首败、修复、实际预算、RunID/InvocationID、hash 后再释放仅本任务资源；不为 C 创建第二份状态台账。

## 方案批准时的一次性确认项

1. 批准上述原生 systemd＋Go bridge 的门禁内实现范围（统一 RPC/PTY/预览、持久归属、Git 私有视图、资源队列）；仍不允许安装／生产部署／真实模型。
2. 是否接受**只限隔离会话、无全局 trust 修改的 TLS 出口代理**；不接受则先保持 deny-all，调整对应 gateway 方案后再批准这一部分。
3. 是否另批下一次**只读磁盘能力专项核查**：只查现有子卷/quota/权限/预算，不安装、不启 quota、不变更目录、不迁移。查明后再集中确认需要管理员操作的精确范围；不拿本计划批准当存储改造许可。

团队协作与通用可配置编排继续后置；本子计划仍服务 Web＋pi 闭环，不扩大为编排引擎、OCI 平台或多租户系统。

## 独立审查修订：安全接线的规范性约束

以下分别关闭初审 I1/I2/I3 的设计缺口；是任务 2–6 的实现约束，不是已验证代码。初审不通过报告与旧计划快照保留在同一证据目录。实际系统组合仍须专项验证，尤其 SO_PEERPIDFD、PrivateUsers 映射及私有 devpts；不支持即阻塞。

### I1．不依赖 FD 穿透 manager 的启动事务

1. 宿主先持久保留 RunID/Owner/Spec hash、稳定 rendezvous 目录 inode；创建 per-run Ed25519 宿主签名私钥（仅 DataDir 私有文件）。把公钥、RunID/Spec hash 及 Unix 地址写进**非秘密**公共 bootstrap，只读挂入单元。rendezvous 只挂其专属目录且只读，目录只含公共描述和 Unix socket；私钥、全局 DataDir、租约路径和 spool 不在该目录。宿主重启只重建 socket 条目，不替换已挂载目录 inode。
2. systemd 只执行固定静态 Go bridge 和公共 bootstrap 路径；不传秘密 stdin、额外 FD 或真实任务命令。bridge 在读配置／连接前设置 core hard/soft=0、PR_SET_DUMPABLE=0 并清空 ambient env；随后连接 rendezvous。此阶段绝不执行 pi／shell／manifest。
3. 宿主以 accept 端的 SO_PEERCRED **宿主 PID 视角**及 SO_PEERPIDFD 固定连接者；两次核 unit 的 BootID/InvocationID/实际 cgroup/MainPID，要求 peer 确为已启动 bridge 的 MainPID，而非同 UID 的其他进程／该单元的任务子进程。PIDFD 不可用或身份变化则拒绝。主机与 bridge 各出随机 challenge；宿主对双方 challenge＋RunID＋Spec hash 签名，bridge 用已挂载公钥验证宿主。因此不是“先拿到秘密才可拿到秘密”的循环，也不要求新宿主 PID 在子 PID namespace 可见。
4. 双向身份成立后，宿主通过此 Unix 连接 SCM_RIGHTS 交接 sealed 配置 memfd、同一 OFD 的现有 flock FD，以及 quota 内私有 spool FD。bridge 检查类型、数量、dev/inode、CLOEXEC，读完关闭配置 FD，核所有挂载／namespace/预算，再回复 PREPARED。租约 FD／控制连接／spool FD 仅由 bridge 持有，不给任务子进程；子进程仅有明确 stdio/PTY FD。不存在先解锁再 acquire 的交接空档。
5. 宿主先持久记 commit-intent，再发送唯一 EXEC_COMMIT(RunID, launch-sequence=1)。bridge 本生命期只执行一次，重复 commit 返回原状态不 spawn；不允许单元 Restart 自动重建 bridge。宿主收到状态后登记 running；回执丢失只 Attach 查询，不再次执行。PREPARED 前或 commit 前崩溃不得执行任务；commit 后未确认保留 unknown，不自动重发业务 prompt。启动等待有界，过期只清理已核实本次单元。
6. 正常宿主 detach 或死亡：bridge 保持真实 pi stdin 和模型子进程、有限 spool，不因连接 EOF 而退出。新用户意图到来时宿主先备好监听，对已验证 bridge 用 pidfd 发送专用 reconnect signal，触发一次有界握手（最多3次、总计5秒）；不建宿主后台轮询。重新握手验证相同 Identity／公钥和新 challenge。活 bridge 回传 lease/spool 的同一 OFD 副本，宿主**不重新 acquire flock**、不复用旧 PGID marker；之后按水位 Attach，不重放 prompt。
7. Bridge、宿主 Stop/Wait/Detach 幂等：Detach 只断通道；Stop 只针对完整匹配的当前 unit；只有组空才记 stopped／退锁。连接消失、代理 Wait 成功、pi agent_end 都不是整组退出。桥缺口、spool 部分写入、启动未知态不生成成功结果。

新增任务 3 用例：`TestBootstrapEachCrashPointNeverRunsUnlocked`、`TestSameUIDCannotWinBootstrap`、`TestAttachRecoversLeaseWithoutReacquire`、`TestDetachDoesNotSendPIEOF`。用模拟 unit/peer 测状态机；SO_PEERPIDFD/SCM_RIGHTS/私有 namespace 的真实组合另作 opt-in，无现场授权不运行。

### I2．Git 双视图固定映射及可信 Git 寻址

- sandbox task tree 的 `.git` 文件以原内容、只读单文件挂载保护；它仍指向原绝对 canonical admin 路径，但**该路径在 sandbox 只映射私有 admin 副本**。公共 objects/refs 只读；任务另有可写私有 object store，以受控环境作为新 blob 存储、以只读 canonical objects 为 alternates。canonical admin 本体不挂给任务；副本的 index/config.worktree/HEAD/commondir/alternates 即使被改，均不回写 canonical。用户要提交／合并仍用既有显式审核入口，不承诺任意 shell git commit 能写共享对象。
- canonical CommonDir、WorktreeGitDir、工作树 dev/inode、分支及基准来自服务持有的工作区创建／Owner 登记；不是运行时从可写 `.git` 重新发现。新增 `gitops.ManagedTarget{CommonDir, WorktreeGitDir, WorkTree, Branch string; Device, Inode uint64}` 和 `ManagedRunner(target ManagedTarget) Runner`：托管调用显式 `--git-dir=<canonical WorktreeGitDir> --work-tree=<已固定tree>`，以可信环境指定 GIT_COMMON_DIR 和 GIT_INDEX_FILE；执行期间保持目录 FD，并核前后 identity。原 legacy Args/Runner 不机械替换，授权 remote helper 的 bridgeArgs 不破坏。
- `.git` 为链接、指针异常或工作树身份不符时，后台在执行前返回安全错误；即使检查后遭替换，也不让 Git 按该文件发现目标。任务私有 admin、GIT_*、环境和配置不进入可信 Runner。后端固定 canonical admin 本身只允许服务生成的、经校验的 commondir/HEAD；未知配置保留报错，不“清洗后继续”冒充已授权。
- 复用现有 Args/Env 已有 hooks/fsmonitor/credential.helper/global/system-config 屏蔽，并严格限制 canonical 本地配置：仅服务生成的 repo format、bare/filemode/logallrefupdates、批准分支／remote 声明等非执行字段；include/includeIf、外部 filter/process、diff/textconv、merge driver、可执行 sshCommand、alternate 重定向和用户 hooks 均不允许进入托管本地写操作。remote 网络操作仍走原独立批准桥，不把其凭据给任务。diff 显式 `--no-ext-diff --no-textconv`，merge 只允许内置策略，不启用 attributes 指定的外部程序。
- 增加修改 `.git` 为另一真实仓库／symlink，修改私有 HEAD/index/config/commondir/alternate，放置恶意 attributes/filter/merge/helper 的实际 Git 测试：后台必须拒绝非法状态或仍仅操作原授权 canonical target；marker 程序不执行，另一项目及主 refs 不变。合法审核→显式提交→普通合并仍通过，不把加强寻址变成破坏原交付行为。

### I3．逐请求授权、下载与凭据隔离

- 初版只支持明确验证过的提供商策略，HTTP/1.1＋SSE；HTTP/2-only、Upgrade/WebSocket、嵌套 CONNECT、未知 OAuth/SDK 保持 blocked，不自行放通。必须记录实际 Node/pi/SDK 版本和 API、refresh、trust、取消能力矩阵。
- CONNECT authority 必须是冻结策略的 ASCII 规范化 host:443；拒绝 IP literal、userinfo、歧义端口、碎片。TLS SNI、CONNECT authority、请求 Host（将来支持 H2 时包括 :authority）、absolute-form URI 的 origin 必须完全一致；有冲突直接拒绝。上游 Host/SNI 由策略生成，不转发客户端 authority；上游 Proxy=nil，按已过滤 DNS 结果 dial 固定 IP 并正常验证域名证书，不受任何代理环境影响。
- 每个 provider policy 明确 methods、精确路径或受限路径段、允许 query keys/values 和内容大小；本期路径拒绝任何 percent-encoded 分隔符／重复编码、反斜杠、控制字符、dot-segment 和歧义路径，禁止先 clean 后放行。不符合已支持路径语法的 provider 报不支持。query 严格一次解码、重复 key 拒绝；仅允许策略指定参数，例如冻结 api-version 或 alt=sse；query/body 中必需的 API key／refresh token 显式标敏感，不进入日志／错误正文。模型字段由冻结的 provider/model policy 限定，不能通过改任务 models.json 变更出口授权。
- 模型／refresh 请求默认**拒绝所有重定向**，不自动重放带凭据请求；若未来某 SDK 必须跳转，另定义精确目标／方法与凭据策略再验收。每次新连接／请求复核 policy 版本、DNS 所有结果、实际目的地址（含 IPv4-mapped/IPv6、私网/link-local/loopback/metadata）；复用连接也不能绕过 policy 失效。
- **下载不是第二条通用代理。** 管理员批准的是冻结、不可变的 `DownloadGrant{ID, URL, SHA256, MaxBytes}` 公共产物；由可信 Go fetcher 只按 ID 查表并构造无客户端头／body／query 的 GET，禁止任意 URL、额外参数、认证头／Cookie／模型请求正文、所有重定向及继承代理。任务不能通过把 key 编进 URL/header/body 传出，grant 不包含模型密钥；支持新依赖必须先批准新 grant，不宣称普通 npm install/任意 registry 网络已可用。
- 下载经独立受限本地 bridge 路由只接收 grant ID，服务端验证大小／SHA256 后交付本项目计费产物；默认缓存只读、按项目归属。私有下载认证另行设计，不复用所选模型凭据；终端／预览仍没有模型凭据挂载。broker 仅处理已批准模型／refresh origin，所有其他外部连接拒绝。
- 网络日志、错误与 trace 只记 policy/grant ID、状态码／字节数／安全错误码；不保存请求 URL/query/header/body 或 response 内容，不为排障打开抓包／明文 spool。真实提供商凭据仍只来自已批准 server_pi 所选项，不复制个人全目录。这个限制不冒称能对任务自身打印任意私密内容做通用 DLP。

新增任务 6 用例：冲突 CONNECT/SNI/Host／absolute URI、编码 slash/dot/重复 query、伪造 model、redirect+Authorization、下载请求夹带 Authorization/Cookie/body/query、mapped-IP/本机目标、过期 policy、TLS pinning/忽略 proxy 的 SDK 拒绝。合成 HTTPS stub 测授权请求与 SSE/refresh/cancel 正向；任何真实费用请求另批。

## 门禁内首批代码进度（2026-11-01）

用户“你继续完善”后已开始本地基础代码与只读磁盘专项，不再停留于计划。当前新增 executionenv 的纯 Check/ModeForProject、固定 Go bridge 的 BuildUnitArgs，以及 config.LoadExecution 的 sandbox.json 安全只读加载；行为红绿、自测 Go 全量／两包 race／vet 通过，基础层独立限定核查通过；初审两项失败及修复证据保留。**任务1/2的完整消费者接线未完成，七项任务不提前勾选。**

这批库尚未接入 agent／终端／preview，没有真实能力 observer、unit Start、Go bridge 或存储驱动；Check 成功只说明可信观测输入满足策略，BuildUnitArgs 只是序列化，不能据此声称实际 namespace/cgroup 已建立。Config 只能声明请求预算，不产生 capability。参数编解码及 ProtectHome=tmpfs 的261实际组合仍须专项验证。暂拒 bind path 中 colon/backslash，不猜测多层转义；后续需支持时补对应版本真实解析证据。

磁盘只读证据 c-disk-readonly.log：两个候选路径同在 Btrfs dev189/subvol268，均为普通目录；未解析出 UUID，未扫描未知 sysfs；现有 btrfs/quota/xfs_quota/setquota 工具缺失。不能断言 quota 未启，只能说没有可用的项目级硬配额证明；不新增工具、不启 quota、不迁移目录。实际存储专项仍阻塞。当前服务 active/PID4734/InvocationID 不变；无新现场单元、真实模型、CA安装或生产部署。

初始规范性设计及独立检查已保留 hash；实际代码 API 进度与差异：Spec 增加 Mode/ProjectRoot 用于纯准入；Capabilities/DiskObservation/NetworkObservation 是可信后端输入，不是客户端配置接口。Controller/Handle/Transport 与 WriterLease 将在真实 bridge 接线时实现，当前未提供虚假的可启动 backend。所有实际证据与后续阻塞继续只写原 status.md。

### 基础层独立初审后的安全修订

原9文件初审保留两项实际失败：配置根祖先 symlink 与 encoding/json 的 Unicode simple-fold 重复字段覆盖。已改 Unix 从 `/` 用逐组件 `openat(O_DIRECTORY|O_NOFOLLOW)` pin 目录，再相对最终 fd 非阻塞/no-follow 打开并 fstat 配置；不对检查过的原绝对路径重开。固定 schema 对象字段限定 ASCII 后去重，禁止 long-s／Kelvin-sign 等覆盖，value 中中文仍可用。祖先重绑定、最终链接、真实 FIFO、Unicode 与原 overlay 均有回归。

同时参数加入 PrivateIPC 和 RestrictAddressFamilies 的 AF_UNIX/INET/INET6/NETLINK 白名单，避免只用 PrivateNetwork 漏掉 SYSV IPC／AF_VSOCK。准入新增 PrivateIPC/SocketFilter 必需观测，未来 backend 必须给真实证明，不能从这个参数字符串测试或旧临时探针推断已支持。首败与修后 Go 全量／race／vet、代码 hash、独立窄复验统一索引于原 status.md。

### 持久归属与控制桥协议进度

用户随后“继续”，新增 `executionenv/ledger.go`、`workbench/runtime.go` 与 `execbridge/`：

- 按实际最新 schema 4 增至 5，同库 runtime_runs 记录 owner／Spec hash／预算／unit identity／阶段／版本／唯一启动序号；partial unique 约束独立 Store 的会话与任务单写端，unknown 保留占位，State 写事务拒绝删除／重绑活跃归属。已有迁移测试只同步当前/未来版本断言，不重写其他共享成果。SessionID/SessionFile/消息保持原表原值。
- 固定长度双 nonce、Ed25519 domain-separated 签名握手；host pin peer 两次核完整 identity，bridge 只信 bootstrap 公钥。调用有 deadline/cancel，失败与 detach 关闭本连接，不碰 pi stdin。密钥耐久管理／bootstrap 发布还没有实现。
- Linux `SO_PEERCRED`＋`SO_PEERPIDFD` 实际固定 peer FD，核本用户、MainPID、完整归属及存活；不重新打开可复用 PID。`KAIWU_TEST_PEERPIDFD=1` **仅是测试专用本机 socket/kernel opt-in**，不启动 systemd、不是生产开门。本机 kernel 测试通过，但 UnitPeerInspector 为合成状态，没有实际 systemctl 适配器或 PrivateUsers 组合证据。
- 鉴权后的真实 Unix SCM_RIGHTS 传 sealed config memfd／可选既有 flock／spool FD；核类型、个数、dev/inode、seals、CLOEXEC 和 spool RW/非append；坏／截断帧关闭已收到的全部 FD。测试证明原 host FD／control 关闭后 receiver 保持同一 flock OFD，最后一份关闭才可重新锁；不能代替真实宿主死亡与跨 namespace 验收。反向归还／sealed config 业务解码／挂载 attestation 未实现。
- `CommitPrepared` 消费真实 SQLite Journal，必须先持久 commit-intent 才一次 Commit；ACK 丢失保留 unknown，`RecoverLaunch` 只有 Status/Observe，不重发启动或 prompt。通过真实临时 SQLite 的断点及次数回归；Endpoint 是隔离模拟，尚无桥端 child exec-loop，不能称实际任务执行已完成。

独立初审发现认证 5 秒预算未传给 Pin/Inspect（R-I1），已补真实阻塞红测并在两个入口使用同一 child context；独立原 overlay、第二次 Inspect 取消、成功返回 channel 生命周期与关键 race/vet 复验通过，R-I1 关闭。作者修后 Go 全量、三包 race（含显式本机 peer probe）、vet 通过。最终仅本轮持久元数据／协议／宿主事务限定通过。原默认测试不运行现场 unit；native Start/Inspector、桥端 once-exec/spool、RPC/PTY/preview、Git／出口／队列和真实两项目验收仍未完成，C 不勾选、硬磁盘门禁不放宽。证据继续沿唯一外部 status.md。

### 桥端本地执行／spool／wire 后续切片

用户再次“继续”后，`execbridge/` 已新增严格 sealed 配置消费、桥端 `Session` 和真实 stdio 子进程、有限耐久 `Spool`、鉴权连接上的 `Serve/Remote`，以及反向 lease/spool SCM_RIGHTS。`Remote` 已实现宿主 `Endpoint`，不再只用 mock 验启动事务；`RecoverLaunch` 明确拒绝 gap 状态。

- Prepare 必须 hash 绑定已认证 bootstrap，schema/duplicate/Unicode field aliases/unknown/trailing 严拒；明确要求可信 `Attestor`（当前无生产实现）和完整 `Check`，quota 未证实在 spool 写入/child 前拒绝。只支持 pi stdio，terminal/preview 暂拒绝；不从非空 spool 自行重建死 bridge。
- 当前连接 token＋epoch 和输入序号同时核验；commit 生命期只一次。输入 ticket 先耐久后一次写，不记录 prompt 正文，不重发不确定输入。真正 stdin 不使用连接 context 启动子进程；detach/EOF/cancel 不 kill/close stdin，direct child 结束也不退 lease。仅可信完整归属的整组 gone proof 能释放。
- spool 单写端，固定限额与 RunID、CRC/连续序号、dirty→data→clean 各 Sync；有限 frame/batch/索引，不轮转覆盖历史。满/损坏/短写/Sync/输入不确定转 sticky gap，继续 drain 后续输出而不制造 exit0 成功；gap 仍可鉴权接回查询与取回原 FD。若故障介质连 gap 写入都拒绝，不宣称绝对断电持久保证；live gap 状态与禁止从旧 spool 复活的门禁保留。
- 真实 Unix 协议/SCM 测试核同一 flock OFD 反向交接无重新 acquire。组合夹具使用真实临时 SQLite、鉴权 wire、实际退出且跳过 defer 的 synthetic host 子进程与实际 task 子进程；原任务不 EOF/不重启、租约持续、恢复只查询且 SessionID/File/messages 原样。测试直连 helper 的 ExtraFiles 不是 FD 穿透 manager 的假设。
- 上述 isolation/quota/unit identity 都是明确 synthetic observation；子进程是选定 Go 测试 helper，无 pi/模型/个人凭据/现场 unit。已核实 task exec 不继承 synthetic ambient secret 或 config/lease/spool FD。输入超时夹具只调自身 pipe 容量，并使用 SyscallConn 保持 Go deadline，不改 sysctl。

Go 全量、三包 race（含显式本机 peer pidfd）、vet 自测通过；独立本包 test/race/vet、16 并发 commit/input、前门、实际 Sync 顺序、错误 tag FD 清理及退出后未 drain 的关闭探针（race count=20）通过，未发现已复现 Important/Critical。仅桥端本地进程／stdio／spool／协议／FD 回接限定通过，Windows/Darwin 两次仅跨编译。此前真实红及夹具错误日志均保留，不把 10 秒旧 fixture “green”当作 deadline 证据。此层仍**不是完整 C**：bridge CLI 入口/最早 hardening、native Attestor/Inspector/Start、耐久私钥/bootstrap、完整 pirpc Transport/request-ID epoch、PTY/preview/Git/出口/队列及真实双项目现场未完，磁盘仍未知、生产门禁关闭。七任务不提前勾选。

### 只读 native ownership Inspector 后续切片

`executionenv/inspect*.go` 与 `execbridge/systemd_peer*.go` 新增原生只读归属核验，未改既有基础／桥端源码。`NewSystemdInspector` 接受可信宿主选择的绝对 systemctl 路径，构造不调用 systemd；Inspect 只对完整持久身份执行两次有限 `--user show`，固定本 UID 的本地 bus 与明确洁净环境，不 start/stop/reset/修改 unit。严格字段、同一 5 秒预算、有限 stdout/stderr 处理。

Linux 实现以逐组件 no-follow/CLOEXEC pin 并核 procfs/cgroup2 类型、MainPID pidfd、procPID/cgroup 目录的 fresh dev/inode 及存活，核 BootID、主进程 cgroup 归属、populated/not-frozen 和 cpu.max/memory.max/swap0/pids.max；两次 manager/kernel 信息一致才产生 `OwnedRunning`。仅接受本模板 100ms CPU period，其他不支持或不确定全部拒绝；不从 unit/cgroup 消失生成 `OwnedGone`，不据此释放占位或 lease。`ActualBudget.DiskBytes=0` 表示未知，局部观测不是完整 Budget／Capabilities、挂载或 namespace attestation，更不能当硬磁盘准入。peer 适配仍使用原 SO_PEERPIDFD 核真正连接者，不以 numeric PidfdOpen 降级 socket pin。

作者全量 Go、三包 race-count1（peer opt-in）及 vet 通过；独立关键 test/race/vet、共享 context／晚取消、输出上限、真实 helper 超时和 128 轮 FD 清理探针通过，无已复现 Important/Critical，本地 ownership 切片限定通过。Windows/Darwin 两包四次仅跨编译通过。默认测试不调用真实 systemctl：show 用直接 Go helper 子进程核 argv/无 ambient secret/70KiB 拒绝，unit/kernel 正向组合为显式 fixture；nofollow/FIFO/大小/rename 使用真实临时文件，本机 proc/PIDFD 为只读真实核验。这不是现场 unit、PrivateUsers 或实际硬预算组合验证。初实现 bytes.Buffer 接口提升绕过 stdout 上限已由真实超限行为红测修正；后加变动 kernel snapshot 测试曾漏接 fixture 回调，首轮全量/race 失败保留，修正测试后复跑通过，不宣称修复了不存在的生产 snapshot 缺陷。

C 七任务不提前勾；实际 systemctl+unit+kernel+peer 整链、Start/Attestor/CLI hardening、私钥/bootstrap 耐久、完整 RPC、PTY/preview/Git/出口/队列/双项目及磁盘硬配额仍未完。未 SSH/新建 unit/改变服务、quota 或系统参数，没有模型／部署／生产开门。

### 持久 bootstrap／host rendezvous／早期硬化 API 后续切片

11 个新增 `execbridge/{material*,rendezvous*,harden*}.go` 文件完成一组本地生命周期，不改已验基础／Session／Inspector 源码。`PrepareRunMaterial` 要求调用者已保留 SQLite runtime 且 runtime parent 为既存 private0700，逐组件 no-follow/CLOEXEC 与 parent flock；owner marker 绑定部分 runtime identity／Spec hash，key0600 与 public-only 子目录分开，public.json 无任务命令／凭据。独占写和文件／目录／parent Sync 后以 ready 记录公开正文 hash 及目录 dev/inode。重开不得旋转 key、重建已发布目录、换 inode、接受不符 spec／损坏 key／链接／硬链接／过宽权限或未知公开文件；失败保留，不猜修。返回的签名 key 副本由宿主调用方管理，不进入 JSON、任务挂载或本轮证据归档。

`ControlListener` 另持 host-control flock（不是 task writer lease），私有注册记录核 socket/目录 dev/inode；仅无活 host 且 socket 属于本 run 才换 socket 条目，绝不换公开目录。未知 entry／registration／残留 stage 拒绝保留，不凭 connection-refused 删除。注册按独占 stage→Sync→rename→目录 Sync；Close 只关闭自己的 socket／目录 dupFD／host-control 锁，不 unlink 新资源或停止 unit。真正 host helper 进程 `os.Exit` 跳过 defer 后重新开同一 key、同一目录 inode、重绑 socket，真实 Unix 鉴权仍接受旧公钥；observer 仍 fixture，不是 unit/跨 namespace 证明。监听器独持目录 FD，material Close 不使其地址失效。

`HardenBridge` 只供专用桥最早调用。独立初审 B-I1 真实复现了 PR_SET_NO_NEW_PRIVS 仅影响调用线程、其他或随后 Go worker 仍为0，初版同线程检查不足，本轮初审不通过。修复不在 Go 内尝试补线程：NNP=1 必须由可信 manager（模板已有 NoNewPrivileges=yes）或受控 pre-exec 在 Go executable 的 exec 之前建立；API 仅验证当前及真实 procfs 中有限稳定线程集合均为1，否则拒绝，不自行 PR_SET_NNP。随后设 RLIMIT_CORE 硬软0／PR_SET_DUMPABLE0／Clearenv，并复核继承态。正向隔离 launcher 以标准 exec 替换进程，核调用线程、新 worker、后续 exec 及其 worker 都继承1；普通进程和仅当前线程1的伪前提真实拒绝。只在私有 helper 子进程测试，未对主 Agent／服务／测试父进程应用；不声称清空已存在的全部内存副本，非 Linux 失败关闭。没有接入主 CLI 或默认假 capabilities 来冒充 native Run loop/Start 已完成。

初审 B-I1 不通过及原两组行为负测保留；修后作者全量 Go、三包 race-count1（peer opt-in）、vet 通过，独立窄复验通过，B-I1 已关闭，仅本地 bootstrap／host-rendezvous／硬化 API 切片限定通过。旧合同负测仍 exit1，但 helper 从成功后发现 worker0 的47变为 API 前提拒绝的45，不把旧 overlay 虚报为 green；独立新合同 plain/mixed/preexec、后续 worker/exec/FD 与有限 census 探针通过。有限 census 不能证明此前谁设置 NNP，也不代替可信 manager/pre-exec 启动责任。真实红、初实现失败和 TempDir fixture 权限修正均留证。完整 C 仍未完，真实现场组合、NativeAttestor、Start/CLI/完整 RPC、Git／出口／队列等仍缺；磁盘 quota 仍 unknown，C 七任务及生产 gate 不开放。

#### 完整 C 的剩余工作与外部前提

- 本地可继续的批准范围：native 入口／启动与 attestation、完整 RPC、可信 Git、有限出口及调度接线，不以当前库或合成观测代替实现。
- 管理员存储前提未满足：目前只是共享 Btrfs subvol 中普通目录。需先核准实际 quota 状态和权限，再明确新托管项目的独立子卷／项目总量硬限制方案，覆盖 repo/worktrees/.kaiwu 及 bridge spool；不以本地文件长度或静态 policy 当 quota，也不擅自对共享 FS 启 quota、安装或迁移旧项目。具体主机操作另批。
- 真实集成前提未满足：新增现场 unit/重接/PrivateUsers/MainPID/挂载与 hard budget 组合需另行有界、无模型试验授权；真实模型及生产部署仍各有门禁，不因“继续完成”自动放开。

### 桥侧公共 bootstrap 读取

新增 `execbridge/bootstrap_{linux,linux_test,other}.go`：可信 startup 提供 canonical 绝对公开目录；no-follow pin、0700 归属、只允许 public.json/Unix socket，文件0444／普通／单链接／16KiB上限及严格JSON，读取后复核目录和文件 dev/inode。取消、链接、可写配置、未知公开文件和超限均拒绝。文件模式不是只读 mount 证明，返回值不授权未来 pathname dial、task exec 或替代 NativeAttestor。尚未接 main/CLI/完整启动握手。作者 Go 全量、execbridge race/vet 及独立窄审查通过；独立特殊文件、真实目录／文件替换及 FD 清理探针通过，无已复现 Important/Critical，仅本地 loader 限定通过，完整C门禁不变。

### 鉴权后 PREPARE 握手与 SQLite 本地组合

`execbridge/prepare.go` 新增 HostChannel.Prepare／BridgeChannel.AcceptPrepare，串起已鉴权 channel、version/full identity/FD 声明、accept-files、真实 SCM_RIGHTS、PrepareSession attestation/Check 及 prepared 回执。rights 平台实现仅抽出锁内 helper，使整个握手由 channel ioMu 独占；同一5秒／更短parent预算覆盖所有阶段。此 exchange 必须在 Serve/OpenRemote 前完成。

PREPARED 不是执行许可：不执行 task、不发 EXEC_COMMIT、不自行写业务数据库。实际临时 SQLite＋真实 Unix／SCM／helper 组合核验 ReserveStarting→wire prepared→调用者 AdvancePrepared→现有 CommitPrepared 耐久意图→Remote commit，保持原 SessionID/File。缺 quota 在 spool/child 前拒绝，nil attestor、坏 FD、foreign offer 和取消均拒绝。Session 已建立而回执丢失时返回 **非 nil Session＋error**，调用者须保留原 lease/spool，不能把 error 当退锁许可；后续只能 query-only attach 或可信 whole-group gone 清理。测试核丢回执时租约仍持有且没有启动。

全量 Go、三包 race-count1（peer opt-in）、vet 自测及独立限定验收通过；声明／乱序回应／SCM 后剩余预算／FD 拒绝清理／失联后只查询回接探针通过，无已复现 Important/Critical。Windows/Darwin 两次仅跨编译通过。完整 identity/capability/rendezvous inode 在组合夹具仍为明确 synthetic，并非真实 unit 或硬配额证明。未接 main/CLI/NativeAttestor/Start、完整 RPC 与其它 C，生产门禁保持关闭。

### BridgeRuntime 单一会话生命周期

新增 `execbridge/runtime.go` 和 Linux 实测，作为 Session 的常驻 owner，调用既有 AuthenticateBridge／AcceptPrepare／Serve。首次 prepare 串行且并发请求拒绝，**非 nil Session＋error 也立即保留**，错误只结束控制连接；之后新鉴权连接直接 Attach／Status／RecoverFiles，不再 prepare 或重放 commit/prompt。只接受可信精确 whole-group-gone 关闭资源，没有按连接 context 自动销毁 Session 的 Close。constructor 仍要求可信 bootstrap/Attestor，专用进程 hardening 和入口顺序须未来 CLI 兑现，不伪造实现。

真实 Unix／SCM／helper 子进程验证 before→detach→新连接 exit 的单次执行和原 stdin 保留；丢 PREPARED 回执后 host lease 副本关闭，runtime 仍持锁且只查询回接不启动，组空证明后释放。能力／unit identity／gone proof 仍为 synthetic fixture；不是实际沙箱。作者 Go 全量、execbridge race-count1、vet 及独立限定验收通过；独立并发准备／关闭、连接取消、无效鉴权和租约探针 race count=10 通过，无已复现 Important/Critical。尚无实际CLI/Start/NativeAttestor/自动signal重连/完整RPC等接线，C及生产门禁仍关闭。
