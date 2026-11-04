# 卡片子 Agent 公共切片（Task 1）实现报告

角色：常规编码实现者（公共记录 + 只读查询）。范围：只实现 Task 1；不实现真实启动桥、MCP 或浏览器写入口。本轮为**修正第 1 轮**（fix round 1，事件幂等缺陷）。

- 时间：2026-10-18（CST）
- cwd `/home/jimxl/projects/kaiwu`，查看时 main，`HEAD=d6f861401af8813f118903b33f44e44b3d0138b6`（不是工作内容快照；继承的混合未提交成果原样保留）
- 未提交、未合并、未推送、未部署、未建空间、未安装依赖、未调用模型、未加载个人扩展、未启用 MCP
- 未修改：agent launch/manager、交付逻辑、前端、README、roadmap、旧验证报告、7331 运行实例、`server/fakepi`、`server/internal/delivery/extra-*.txt`
- 参考基线：`/tmp/kaiwu-subagents-baseline-2134`（逐文件核对；本任务改动仅下列文件）

## 修改清单与内容 hash（sha256，修正第 1 轮后的最终值）

新增：

| 文件 | sha256 |
| --- | --- |
| `server/internal/workbench/subagents.go` | `33fddb31deb71add0bb414b645cfc394504fbcb39dde631b6d8551a621270791` |
| `server/internal/workbench/subagents_store.go` | `c1685de1dc1f99ef371262a6646def7b0d0e7a1d0396fd2a4a79835b92bdb1b5` |
| `server/internal/workbench/subagents_test.go` | `524b0b2c2cdcff2cc17ece28f56010314403832ee99af11cfbc78aa3dc172e7b` |
| `server/internal/webapi/subagents.go` | `8a6b5436f01de9fd59223c25f6aad2a38293701abcb25c3a4e54b6d45661b100` |
| `server/internal/webapi/subagents_test.go` | `795cad120553776401a7a30fb9483d159219ff545505f7403db87de7b1a19677` |
| `docs/development/web-task-subagents-contract.md` | `6154f9b751405c30fc7f0181ac76ffb3dd5a46fe127165cd32bb4dd6bf8e89d9` |

修改：

| 文件 | 改动 | sha256 |
| --- | --- | --- |
| `server/internal/workbench/types.go` | `State.Subagents map[string]SubagentSummary`（轻量摘要投影） | `91b95a0b06b654f3242c5f3a5e27db30ad073664b717d2ccdcc9c3421d5cee7f` |
| `server/internal/workbench/store.go` | `Subagents` 初始化；重启把 `starting`/`running` 归 `unknown`（`version+1`、写 `finishedAt`），终态保持 | `a8e5139bf8155f578af639d2522b36ca3706d0ea7a00abc242aa19c0966415e4` |
| `server/internal/workbench/migration.go` | schema 2→3：`subagent_runs`、`subagent_events`（含 `fingerprint` 列）与索引；`version>3` 拒绝 | `fc4358a2ff0044e459226ed7d6b44cd7af4e6c9c1fb574c544974355b826686b` |
| `server/internal/webapi/handler.go` | 新增 `POST /api/subagents/detail`（只读） | `dc3798f8c90acdc50a2ffb7fd1e733d22195b92167636e2930efaf7ee49c1bd2` |
| `server/internal/webapi/socket.go` | socket 固定方法 `subagents/detail`（复用现有认证/CSRF/登录失效检查） | `ac839e1cdfe789d12297d09531e1425b47374133ab9b40f276ddac13452e9e30` |
| `server/internal/workbench/delivery_store_test.go` | 边界例外：`schema != 2` → `schema != 3`（1 行，主控已接受） | `94beab76133f5073d7363b593710ea848338aaa34a1185a68bd8c21e4da2b314` |

对外契约见 `docs/development/web-task-subagents-contract.md`（JSON 为真实 HTTP/socket 响应摘录）。

## 实现要点

- `CreateSubagentRun`：校验 run ID/枚举/大小与 parent project/conversation/task/session/round 归属（`Missing` 拒绝跨范围），`TaskID` 由会话推导后参与指纹；同 ID 同归一化内容幂等（不推进 revision），同 ID 异内容 `conflict`；每会话 1000 / 全库 10000 上限按 DB 计数拒绝新建且不删除历史。
- `AppendSubagentEvent`：**先判幂等、后判状态迁移与内容规则**；事件指纹取自调用者原文（不含服务端 `sequence`/`at`，调用者传入的二者一律被覆盖）；同 ID 同原文重放返回当前摘要且不推进 `version`/revision（含被截断事件、已推进/终态状态事件），同 ID 异原文 `conflict`。
- 有界截断：单事件正文 16 KiB、每 run 累计 1 MiB、toolId/toolName 超长按 UTF-8 边界裁剪并置 `truncated=true`；截断后仍保存后续状态与结束证据。
- 事件数硬上限 2000：超限后非状态事件丢弃、不录入、不留指纹（置 `truncated`），重复发送只会再次丢弃，不声称可判别其同 ID 冲突；状态转换仍写入摘要，并在 run 行保留**受限的已接受状态事件指纹（≤4 条）**，使终态重放幂等且记录不无限增长。
- 原子性：摘要、事件行、run 行（累计字节/溢出指纹）在同一 `persistWith` 事务提交；触发器注入测试证明详情写失败时摘要不前进。
- `SubagentDetail`：同 project+conversation 才能读；`limit` 默认 50/上限 100，负值或超限 400；`sequence` 单调分页无重放；纯读，不推进 revision。
- 只读传输：`POST /api/subagents/detail` 与 socket `subagents/detail`，沿用 Cookie/Host/Origin/X-Kaiwu-Client/登录失效检查，未知字段与错误类型拒绝；无创建/修改/控制入口，`POST /api/commands` 的 `create_subagent` 等返回 400 且不产生记录。
- 泄漏面：snapshot/patch 只含摘要（测试断言 state 表、patch 流、重放窗口均不含 request 与事件正文）。

## 修正第 1 轮（fix round 1）

主控证据成立，两个缺陷同源：**幂等判定被放在错误位置，且比较了错误的内容**。

1. 根因 A：`AppendSubagentEvent` 先校验状态迁移、再查已有 `event_id`。已接受 `status=running` 后原样重放被判 `running→running` 非法；`completed` 原样重放被终态规则挡住 → 违反“同 ID 同内容幂等”。
2. 根因 B：DB 保存的是**截断后**内容，去重却用 `subagentEventFingerprint(已截断存储值)` 与调用者原文指纹比较 → 任何 >16 KiB、toolId/toolName 超长或累计预算裁剪过的已录入事件，原样重放都 409。
3. 根因 C（同源边界）：达到 2000 事件上限后终态只写 summary、无事件指纹，成功终态的重放无法被判为幂等。

修正（未删除任何幂等约束）：

- `subagent_events` 增加 `fingerprint TEXT NOT NULL` 列，保存**调用者原文指纹**；幂等查表按该列比较，不再反推截断后内容。
- 幂等判定前置到状态/枚举/内容校验之前（仅保留 run 归属与事件 ID 合法性校验）：对已接受事件，原样重放永远返回当前摘要且不推进 revision/version；不同原文仍 409。
- run 行 `subagentRecord.Overflow` 保存上限后仍被接受的状态事件指纹，**硬上限 4 条**（状态机下 run 内最多两次状态迁移），偏移淘汰保证不无限增长；被丢弃的非状态事件不录入、不留指纹，契约明确该语义。
- `sequence`/`at` 仍由服务端分配并覆盖调用者传入值。
- schema 版本仍为 3（未发布），列定义直接落在同一 schema 3 内；由修正前提交的本机 schema-3 测试库需重建，生产/7331 未部署该版本。

失败最小复现（修正前，红）：

| 命令（cwd `server`，`-count=1`） | 关键输出 | 退出码 |
| --- | --- | --- |
| `GOTOOLCHAIN=local go test ./internal/workbench -run TestSubagentAcceptedEventReplayStaysIdempotent` | `subagents_test.go:110: append run-replay/r-run: conflict: 子 Agent 状态不能从 completed 变为 running`（FAIL） | 1 |
| `GOTOOLCHAIN=local go test ./internal/workbench -run TestSubagentTruncatedEventReplayStaysIdempotent` | `subagents_test.go:206: truncated event t-huge rejected on replay: conflict: 子 Agent 事件标识已用于不同内容`（FAIL） | 1 |

新增/加强测试（绿）：

- `TestSubagentAcceptedEventReplayStaysIdempotent`：`running`/`completed` 原样重放不推进 version/revision；原始大 Unicode（20k 字）、超长 toolId/toolName（300 字）、累计预算裁剪事件原样重放幂等；先断言存储内容确实被截断，否则该用例不成立；上述每类不同原文仍 `conflict`。
- `TestSubagentTruncatedEventReplayStaysIdempotent`：只覆盖截断重放（独立定位根因 B）。
- `TestSubagentOverflowStatusReplayIsIdempotentAndBounded`：上限后 `running` 溢出事件原样重放幂等；同 ID 改状态（`starting→failed` 本会看起来合法）必须 `conflict`；上限后终态写入摘要且重放幂等、revision/version 不变；溢出指纹 ≤4 条、run 行 ≤2048 字节、不为溢出事件写事件行；上限后非状态事件丢弃、不录入、重复发送不同内容仍只是丢弃（不声称冲突判别）。

## 红绿测试证据

第 0 轮（初次实现，红）：`go test ./internal/workbench` 编译失败 `[build failed]`（新公共契约未定义）；实现 workbench 后 HTTP/socket 未实现时 `go test ./internal/webapi -run Subagent` 得到 `404 API 不存在` / socket `400 不支持的实时请求`（exit 1）。

修正第 1 轮后的最终快照（未再改动）：

| 命令（cwd `server`） | 关键输出 | 退出码 |
| --- | --- | --- |
| `GOTOOLCHAIN=local go test ./internal/workbench ./internal/webapi -count=1` | `ok internal/workbench 24.220s` / `ok internal/webapi 8.596s` | 0 |
| `GOTOOLCHAIN=local go test -race -count=1 ./internal/workbench ./internal/webapi` | `ok 35.530s` / `ok 17.914s` | 0 |
| `GOTOOLCHAIN=local go vet ./internal/workbench ./internal/webapi` | 无输出 | 0 |
| `GOTOOLCHAIN=local go build ./...` | 无输出 | 0 |

`-run Subagent -v`（绿）覆盖：`TestSubagentAcceptedEventReplayStaysIdempotent`、`TestSubagentTruncatedEventReplayStaysIdempotent`、`...CreateIsIdempotentAndRejectsConflicts`、`...CreateValidatesOwnershipAndEnums`、`...EventIdempotencyTransitionsAndTerminalStability`、`...DetailScopedPagingAndLimits`、`...EventBoundsTruncateAndKeepTerminalState`、`...OverflowStatusReplayIsIdempotentAndBounded`、`...EventCountCapKeepsTerminalState`、`...RestartMarksActiveUnknownAndKeepsTerminal`、`...SnapshotAndPatchesCarrySummaryOnly`、`...AreNotCreatedByChatOrCommands`、`...AdmissionLimitsKeepHistory`、`...AppendRollsBackWithDetailWrite`、`...Schema2MigrationPreservesExistingData`、`...SchemaNewerThanSupportedRefused`、`TestSubagentDetailHTTPAuthScopeAndBounds`、`TestSubagentDetailSocketMethod`。

测试均为真实临时 SQLite（`t.TempDir()`）+ 认证 httptest HTTP/socket，未调用模型、未重复既有浏览器套件。

## 边界例外（主控已接受）

`server/internal/workbench/delivery_store_test.go` 的 1 行既有版本号期望（`schema != 2` → `schema != 3`）不在派发文件边界内，但与已批准的“增量迁移到 schema 3”直接冲突，属必要配套；主控已接受该行，本轮不回退。**说明修正**：继承的是混合未提交基线，禁止整文件回退或 `checkout/reset`；若日后确需改动该文件，只能在本行做精确点恢复，不得覆盖/搬移其它未提交成果。未发现其它同类冲突。

## 未解决 / 未测

- Task 2（三级只读 UI）、Task 3（独立验收）、Task 4（真实启动桥）均未开始；本切片完成不等于真实 pi 子 Agent 派发接通。
- 子 Agent 执行写权限仍待用户答复；未实现 launch、MCP、worktree/租约、父会话回收、8 驻留总限接入。
- 未做真实模型调用、未做浏览器/截图/视觉验收、未更新 7331 部署。
- `completed` 仅表示运行结束，不代表用户验收通过；`unknown` 语义为待核查，非准确零。
- 未验证项：真实子进程事件流、运行期故障（非崩溃/重启）、跨进程并发写同一 SQLite（沿用现有单实例假设）。
- 达到事件上限后被丢弃的非状态事件无法判别同 ID 异内容冲突（契约已明确，非缺陷）。

## 运行/写工具状态

所有本地测试与校验命令已结束；无后台进程、无定时轮询、无监听器；未创建/修改/归档 Workspace，无运行中或待处理的写工具或子 Agent。
