# Go pi RPC 执行记录

获批 [规格](../superpowers/specs/2026-10-11-go-pi-rpc-design.md) 与 [计划](../superpowers/plans/2026-10-11-go-pi-rpc.md)。用户回复 ok，批准当前会话串行编码＋完成后独立只读验收。

## Setup / pre-flight

- main 无 HEAD；主 checkout 非 worktree。开始时 Paseo 同目录只有协调 Agent `4d13b530-a48e-4980-831d-671d0fff4713` running，旧审查 idle/error，没有第二编码者。
- 基线：server 下 `GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test ./...` → 4 包通过。
- Ruling: 延续获批的 unborn-main 单写入执行，不建 worktree、不提交；Git BASE/commit 型 task-start/task-done 改为计划 brief 与持久 ledger。代价：没有提交 diff，独立验收核对完整文件与测试，不虚构 SHA。
- scratch：`.superpowers/sdd/2026-10-11-go-pi-rpc/`，忽略目录只保存本批次合成测试输出；持久摘要在本文。

| producer → consumer | 核对 |
| --- | --- |
| Task 1 → 2 | Event、Error/sentinel 一致，FIFO 不验证业务 schema |
| Task 1 → 3 | LaunchSpec/错误一致；显式空 Env 保持非 nil |
| Tasks 1/2/3 → 4 | LF reader、两类 record、FIFO/tail、唯一 Wait 与自管读端一致 |
| Task 4 → 5 | Wait transport result 与产品任务成功分开；真实 pi 未授权 |

## 实际 RED → GREEN

| Task | RED 证据／原因 | GREEN 与实际实现 |
| --- | --- | --- |
| 1 协议/参数 | `task1-protocol-red.log`，缺新符号 | 显式 launch/默认限额、命令/响应/事件、状态四字段、固定错误/Delivery；全套 5 包与 pirpc vet 通过 |
| 1 JSONL | `task1-jsonl-red.log`，缺 reader | LF/CRLF、有界 raw payload、分片 UTF-8、精确限额、EOF 完整/截断；全套通过 |
| 2 缓冲 | `task2-red.log`，缺 FIFO/tail | 数量/字节双限 FIFO、可取消等待、终态排空、固定 stderr ring/副本；全套与 pirpc race 通过 |
| 3 进程 | `task3-red.log`，缺进程单元 | 自管 os.Pipe、显式 Env/Dir、唯一 cmd.Wait、幂等关闭、grace/Kill/有界回收；全套通过，最初 process race 10 次通过（42.274s） |
| 4 启动 | `task4-start-red.log`，缺 Client/Start | get_state 握手、失败独立清理、早期事件；Start 普通/race 与全套通过 |
| 4 请求 | `task4-requests-red.log`，并发容量/待写字节/ID 耗尽返回 nil | Call 返回前持有容量；物理删除排队取消、写时预算、唯一 ID、关联/拒绝/取消与忽略计数；请求 race 5 次通过（3.438s） |
| 4 生命周期 | `task4-lifecycle-red.log`，无 metadata 日志、真实写阻塞只发生 Call deadline、4 种末响应被 process_exit 覆盖 | metadata-only 日志；每记录写 watchdog/关闭自有 stdin；自然退出先禁止新请求，再有界排空最后记录，最后终止未答请求；Client 普通/race 与全套通过 |
| 4 回收复核 | `task4-reap-red.log`，无 ProcessState 却发布 stopped，Wait 返回 nil/Reaped=false | 区分 waiter 通知与回收证明；无证明保留 stopping，不发布完成；有截止 Wait 返回当前快照。回归、全套及 pirpc race 3 次（8.191s）通过 |

所有 RED 文件均在上述 scratch；缺实现/行为断言是真实失败，不将语法/依赖失败算 RED。最终完整验证见 [verification](go-pi-rpc-verification.md)：82 顶层通过、1 预期 Skip；关键场景普通 10 次 11.592s、race 10 次 21.467s 通过。

## 裁定与复核

- Ruling: helper 显式 `GORACE=atexit_sleep_ms=0`，避免 race runtime 的额外 1s 退出等待被测试短 grace 误判。只取消测试诊断退出延迟，不关闭 race 检测。
- Ruling: 已退出子进程仍可能有管道中的末记录，不能用 cmd.Wait 到达直接清空 registry。自然退出只失败尚未开始写的请求，已写请求待 drain；显式 Close/owner cancel/故障立即解除 pending。
- Ruling: stdout EOF 与进程退出的通知有竞态，首个 EOF 保留为 I/O cause；若确认未强杀的自然退出，追加 ProcessExit 类别，不覆盖首次原因。停止后的实际协议故障可追加，清理错误单独保存。
- Ruling: 原计划 private kill 注入只能在交给 Client 前设置。Kill 失败＋之后真实回收使用 Go helper；Wait 无 ProcessState 使用自有 pipe＋固定 processExit 的小范围注入，不伪造已回收的真实 PID或泄漏子进程。
- Ruling: Wait error 仍只表达等待者未完成；未完成时返回部分 ExitResult 快照，以便检查 PID/Cause/CleanupError。已完成结果检查 Reaped/Forced/错误字段，不把 nil Wait error 当任务成功。
- 验收补充：协议单元 RED 已覆盖的坏字段/UTF-8/截断/超限，通过 `framing_client_test.go` 与扩充握手矩阵再核对真实管道组装；`reap_failure_test.go` 是新增回收反例。没有新语言或公共任意回调接口。
- 回收复核追加 `task4-reap-pending-red.log`：无回收证明关闭输出后，registry 中已写请求仍未完成；修正为在失去全部响应通道时显式 ErrCleanup 解除 pending，而不是等 Call 自己到期。随后完整普通/race/vet/构建与重复测试重新通过。
- 排队取消测试收紧为 2 个 Call 容量、仅大记录＋256 字节预算；阻塞 writer 下 20 次取消必须每次都允许登记，避免仅检查队列长度而漏掉字节/槽位泄漏。
- 最终构建使用本步骤 mktemp；仅自有 binary/空目录已删除，未清理其他未提交成果。无真实 pi 产品联调、提交、推送或服务监听。

## 当前进度／独立验收

Tasks 1–4 complete；Task 5 首轮独立验收发现 2 Important/1 Minor，已实际修正并重新全量自测，同一 Agent 限定复验已通过；Tasks 1–5 complete。

- Important 1 根因：requests.go 无条件把 CallTimeout 叠加在 Start 的 StartupTimeout context 上。`review-startup-red.log` 普通 3 次失败；仅对普通 Call 增加调用预算后，普通/race 各 3 次通过。
- Important 2 根因：readFailure 用自然 drain 标记决定是否保存关闭期真实故障。`review-shutdown-red.log` Close/owner cancel 的坏 JSON/事件溢出均失败；取消此错误过滤、保留预期自身闭管过滤后，普通/race 各 3 次通过，首因保留。无用的自然-drain 字段移除，registry 是否延后仍由 beginStopMode 的局部参数决定。
- Minor：同步 roadmap 顶部历史 32 项基础与当前 RPC 状态，不改历史验证结论。
- 修复后完整普通/race/vet/格式/依赖/native build/关键普通与race重复10次通过；84 顶层、1 Skip、0 fail。新增回归 race 10 次 3.804s，见 verification-review-fixes.log。
- Declined-to-judge 的 6 类均接受范围理由、保留未验状态，逐条裁定见 review，不静默丢弃。
- 编码再次停止，新 SHA 在 scratch code-before-rereview.sha256；同一子 Agent 限定复验，不增评审席位。独立新回归普通/race各10次（2.495s/3.758s）、相关回归各3次（4.824s/8.563s）、pirpc vet通过，首轮各问题已关闭，无新阻塞。
- 最终收尾核对：新冻结 SHA 全匹配；全套 `go test -count=1 ./...` 再次 5 包通过（pirpc 1.729s）；本地 Markdown 链接 91 项、损坏 0。相关审查 idle，无匹配助手候选；Git 仍 No commits yet on main，.git-dir/.git-common-dir 同为 .git，主目录完整保留。
- 收尾裁定：沿用用户不提交/推送且当前无功能分支/基线的限制，finishing 菜单中的 Git 集成不执行；保留 unborn main 主目录成果，不创建/归档/清理 workspace。不自动推进真实 pi/模型、团队协作或通用可配置编排。

派发前实际读取 `.daemon.agentProfiles` 及 notes：「方案设计，架构思考」用途包含代码后的测试验收，provider=`pi`、model=`openai-codex/gpt-6.1-sol`、thinking=`high`，未指定 mode。相关旧审查 `27abc3d6-d302-4b49-85f4-88d89718aecc` 为 idle，同目录、原会话子 Agent，实际配置一致、mode=default；优先复用，不另建工作区。计划仅要求只读核对，不准其继续派发或启动产品真实 pi。审查请求与实际结果见 [review](go-pi-rpc-review.md)。
