# Go pi RPC 本机验证

状态：Tasks 1–5 已完成，本机及独立只读验收通过；首轮问题已修正复验。依据获批 [规格](../superpowers/specs/2026-10-11-go-pi-rpc-design.md)、[计划](../superpowers/plans/2026-10-11-go-pi-rpc.md)；执行裁定见 [ledger](go-pi-rpc-ledger.md)，审查另见 [review](go-pi-rpc-review.md)。

## 实际环境与范围

- Linux amd64，`go version go1.27.0-X:nodwarf5 linux/amd64`；**非稳定工具链**。
- 命令在 `server/`，`GOTOOLCHAIN=local GOFLAGS=-mod=readonly`。
- 现有 chi 依赖不变，没有新后端语言或依赖；新增 RPC 产品代码及测试全部 Go。
- `internal/pirpc/` 提供具体 pi RPC 通信/生命周期，不是通用 Agent 框架；HTTP/main 未调用此包，健康检查未变。
- 所有子进程测试都是本测试二进制＋临时 cwd＋显式 helper 环境。未启动真实 pi 作为产品被测进程，未加载个人扩展/凭据或执行产品模型请求。

## 实际命令与结果

下列为最终代码冻结前实际执行，逐项退出码 0；完整无秘密输出在忽略的 `.superpowers/sdd/2026-10-11-go-pi-rpc/verification-review-fixes.log`。

| 命令 | 结果 |
| --- | --- |
| `go test -count=1 ./...` | 5 包通过，pirpc 1.727s |
| `go test -race -count=1 ./...` | 5 包通过，pirpc 3.728s |
| `go vet ./...` | 通过 |
| `gofmt -l cmd internal`，检查输出为空 | 通过，空输出 |
| `go mod verify` | all modules verified |
| `go test ./internal/pirpc -run 'TestProcess\|TestCall\|TestClient' -count=10 -timeout 300s` | 通过，11.605s |
| 上项加 `-race` | 通过，21.585s |
| 新增独立启动/关闭故障回归 `-race -count=10` | 通过，3.804s |
| `go build -o <本步骤 mktemp>/kaiwu-server ./cmd/kaiwu-server` | 通过；仅此二进制及自有空临时目录已删除 |
| `go test -json -count=1 ./...` | 84 项顶层通过、1 项预期 Skip、失败 0 |

84 项包含基础包原有 32 项与 RPC 52 项；Skip 是无 marker 时不进入子进程模式的 `TestPiRPCHelperProcess`。顶层计数不包含子测试，不把 fixture 的测试框架退出算额外产品验收。

## 实际覆盖

- 显式 env/绝对路径、Options 默认与负值、上下文已取消时不创建进程。
- `get_state` 握手、四字段校验（缺失/类型/负数/分数）、先事件后响应、拒绝/无响应/早退/关闭 stdin 的失败清理。
- 32 并发请求、独立 JSONL、唯一 ID、乱序与事件交错；未知/迟到/重复响应计数；缺字段/command mismatch 终止。
- LF/CRLF、分片 Unicode、大记录、精确字节限额、EOF 无 LF；污染/非法 UTF-8/截断/超大 stdout 终止。
- 请求容量、待写 raw 字节、ID 耗尽；真实 pipe 阻塞屏障下反复取消，NotSent、物理删除且预算恢复；已写取消 Uncertain，不重发/abort。
- response 已确认不被再次完成覆盖；prompt started/queued/handled 仅接受；agent_end/agent_settled/UI request 原样交付。
- stderr 大量分片、64KiB 尾部副本、提前 stderr EOF 后 stdout 可用；默认 error/log 不泄漏合成标记。
- 事件数量/字节溢出显式失败；终态前排空已入队 FIFO；真实写阻塞触发 WriteTimeout 并回收。
- stdout EOF/broken pipe、正常/强制停止、并发 Close、等待者取消不撤销清理、owner cancel。
- 1MiB 末事件＋最后响应紧接退出 0/7（含 EOF 无 LF）不丢失；进程 Wait 不关闭自管输出读端。
- 有界输出排空、受控 drain 超时、Kill 失败保留 handle 后真实回收；Wait 无 ProcessState 的固定故障数据不发布 stopped，超时 Wait 仍提供当前清理快照。
- StartupTimeout=1s/CallTimeout=20ms 的 120ms 握手可以成功，普通 Call 仍受 20ms 限制；关闭期间坏 JSON/事件溢出在 Close/Wait/FIFO 可见，owner cancel 首因保留。
- Response/Diagnostics/ExitResult 不复用可变字节或退出码指针；worker join/唯一进程 Wait 是收尾证据，不用瞬时全局 goroutine 数作为唯一依据。

## 未执行／不能替代

- 稳定 Go 工具链、Windows/macOS 运行或跨平台构建；未安装/下载新工具链。
- 本批次未重跑已有 Python SIGINT/SIGTERM 烟测；基础阶段历史烟测见 [记录](go-foundation-polish.md)。RPC 的 Go 假进程测试不能替代真实服务信号验收。
- 真实 pi/模型/凭据联调、会话/历史持久化、模型切换、Go MCP/工具权限、后代进程树。
- HTTP 任务入口、编排闭环、数据库、认证、WebSocket/重连、双端 UI/打包、GitHub Actions。
- Git 提交/合并/推送、Tag/Release、许可证；无常驻产品服务或测试 helper 保留。

独立审查使用获批的 Paseo 审查 Agent 模型，与产品真实 pi/模型联调是不同事项；首轮独立发现 2 Important/1 Minor，已复现修正并重新全量自测；同一 Agent 限定复验通过。其实际普通/race新回归各10次、相关回归各3次和pirpc vet均通过；详见 review，不能把协调方全项目构建/测试当作其独立执行。
