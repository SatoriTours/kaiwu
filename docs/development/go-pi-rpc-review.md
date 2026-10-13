# Go pi RPC 独立只读验收

状态：Go pi RPC 基础包范围的独立验收通过。首轮 2 Important、1 Minor 均已实际修正，同一 Agent 限定复验通过，无新阻塞问题；不是完整产品或真实 pi 验收。

## 审查席位与授权

- 原协调：`4d13b530-a48e-4980-831d-671d0fff4713`；local workspace `wks_0a6880df1b60b7e7`。
- 复用相关 idle 子 Agent：`27abc3d6-d302-4b49-85f4-88d89718aecc`，同 checkout，已有父子关系。
- 已复核实际 profile「方案设计，架构思考」的用途为设计与代码后测试验收；provider pi、model openai-codex/gpt-6.1-sol、thinking high、无指定 mode；复用 Agent 配置一致，mode default。
- 本批次 main 无 HEAD，不能提供 BASE/HEAD diff；以完整当前文件及冻结哈希检查。没有为了审查创建提交或 worktree。
- 编码已停止；仅独立读取和隔离测试，不得改 checkout/index/分支，不得派发子 Agent、安装工具、启动真实 pi/模型产品联调或监听服务。审查模型本身的使用属于已批准独立验收，不混同产品联调。

## 请求内容

核对获批 [规格](../superpowers/specs/2026-10-11-go-pi-rpc-design.md) 和 [计划](../superpowers/plans/2026-10-11-go-pi-rpc.md)，关注 `server/internal/pirpc/` 全部代码/测试、纯 Go 模块边界，以及 [ledger](go-pi-rpc-ledger.md)／[实际验证](go-pi-rpc-verification.md)。现有 HTTP/main 不应被改成启动 pi，healthz 不能冒充 Agent 就绪。

按规格 §8 的 13 类验收，重点复核：

1. 环境/参数显式隔离，Start 失败回收及无证明时保留 handle。
2. get_state 握手、仅四字段就绪及与模型/任务的不同边界。
3. LF/CRLF/EOF、UTF-8、严格对象与精确 raw 字节限额。
4. 32 并发 ID/乱序关联、完整独立记录；未知/重复/迟到响应统计。
5. prompt disposition 不等同任务完成、agent_end/agent_settled 不映射业务验收。
6. UI 请求事件保留但不自动批准，普通 Call 拒绝 UI response。
7. 请求容量直至返回；queued 取消物理删除/字节与槽位恢复，写后取消 Uncertain。
8. 无自动 abort/重发；响应已确认不能被取消/退出覆盖。
9. 事件数量/字节上限、FIFO 与溢出后终态排空。
10. stderr 持续排空、正常 stderr EOF、尾部副本及默认 error/log 隐私。
11. 真实写阻塞期限、stdout EOF/broken pipe、不留健康僵尸。
12. 唯一 Wait、直接子进程停止/强杀、输出排空与最后响应（包括末条无 LF）。
13. Close/Wait/owner context 生命周期、drain/Kill/Wait 失败可检查、不虚构 Reaped/stopped。

这些是请求关注点，最终以完整规格而非此摘要限制判断。对考虑后排除的行为必须逐条列出理由，不能静默丢弃。

审查独立执行必要普通/race/vet/重复测试，记录实际命令/输出、未执行项，按 Critical/Important/Minor 给文件行号、影响、建议及清晰验收结论。不要自行修复或写报告文件；结果回原协调会话，由其保存和裁定。

## 首轮实际结果：暂不通过

Critical 无；Important 2；Minor 1。原协调核对冻结 SHA 前后全部匹配；审查会话返回 idle 后才恢复串行编码。

- **Important 1，client.go:56–57／requests.go:49（首轮行号）**：Start 的 StartupTimeout 被普通 CallTimeout 再次缩短。独立夹具 120ms 状态响应、StartupTimeout=1s、CallTimeout=20ms，普通/race 各 3 次复现错误 deadline。主目录新增 `TestStartupTimeoutIndependentOfCallTimeout`，RED 3 次后改为握手沿用 Start context；普通 Call 仍套 CallTimeout。普通/race 各 3 次通过。
- **Important 2，lifecycle.go:58–68／client.go:135–143（首轮行号）**：显式关闭期间 readFailure 仅处理自然退出 drain，坏 JSON/事件溢出被呈现为 nil Close、无 Cause、最终 EOF。主目录新增 `TestShutdownFaultsRemainObservable`，Close 与 owner cancel 两种路径均 RED 3 次；修正为真实读协议故障在任何 stopping 路径保留/追加，预期自身 os.ErrClosed 仍过滤。普通/race 各 3 次通过，owner cancel 的首因不覆盖。
- **Minor 1，roadmap.md:18（首轮行号）**：旧摘要混淆基础阶段 32 测试和 pi 仅说明入口；已改为历史基础与当前 RPC 分开，不改历史验收。

首轮独立实际通过：pirpc 全部普通/race（50 顶层＋1 Skip）、全项目 vet、mod verify、格式空输出、关键普通 10 次 11.625s/race 10 次 21.582s、原 HTTP 无监听测试选集、native build。未重跑含监听的完整 ./...，未把主协调日志当作其独立执行。补充 OS 临时副本复现测试普通/race 各 3 次均退出 1，无 race 报告；临时副本/构建/助手已回收，checkout 和 Git 未改。

原协调修复后：84 顶层＋1 Skip，完整普通/race/vet/格式/mod/native build 与关键 10 次通过，新回归 race 10 次 3.804s；详细见 [verification](go-pi-rpc-verification.md)。冻结新 SHA 后复用同一席位复验，不重新跑无限审查循环。

## Declined-to-judge 及协调裁定

全部接受其排除理由，且不转写为已通过：

| 独立审查未判断 | 协调裁定 |
| --- | --- |
| 真实 pi 1.0.4／身份／实际兼容 | 未授权真实 pi，保留待联调 |
| 凭据／模型／扩展／权限／任务成功 | 未实现或未授权；握手/响应不证明 |
| 后代树／detached 工具／沙箱 | 本包只回收直接子进程，范围排除 |
| 稳定 Go 1.25／Windows／macOS | 仅非稳定 Linux 工具链，后续独立验证 |
| OS 真实罕见 Wait/Kill/关闭故障矩阵 | 当前仅受控夹具，不能冒充 OS 全面验证 |
| 服务信号／任务 HTTP／双端／存储远程发布 | 本轮未启动服务，其余为后续范围 |

## 限定复验：通过

同一 Agent 已逐项确认两项 Important 与 Minor 均解决，未发现新阻塞；新 SHA 校验前后全部匹配、Git 未变、助手无残留。未写文件或创建新 Agent/临时副本。

独立实际执行（server/，`GOTOOLCHAIN=local GOFLAGS=-mod=readonly`）：

```bash
go test -v ./internal/pirpc -run '^(TestStartupTimeoutIndependentOfCallTimeout|TestShutdownFaultsRemainObservable)$' -count=10 -timeout 60s
# 同上加 -race
go test ./internal/pirpc -run '^Test(Start|Call|Client)' -count=3 -timeout 120s
# 同上加 -race
go vet ./internal/pirpc
```

全部退出 0：新回归普通 2.495s/race 3.758s；相关回归普通 4.824s/race 8.563s。相关普通关闭/取消、自然退出末响应、drain timeout、强杀、无回收证明均保持通过。

未重做全项目测试/构建/信号烟测；真实 pi、稳定/跨平台、后代树等首轮未验证项保持原裁定。报告中的 Provider retry（WebSocket closed）已自行重试后完成，未当作产品 RPC 测试。

协调方按冻结新 SHA 核对，保留全部未提交成果；本轮没有 Git 集成或固定入口归档。
