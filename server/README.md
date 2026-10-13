# Go 服务端

独立 Go module：`github.com/SatoriTours/kaiwu/server`。根目录不是 Go module。

## 实际状态

配置、健康检查、JSON 日志、启停生命周期和基础测试已实现并通过独立验收（基础阶段 32 项顶层测试）。内部 `internal/pirpc/` 已实现纯 Go RPC 传输与直接子进程生命周期；当前共 84 项顶层测试通过，RPC 独立验收通过（首轮问题已修正复验）。当前另已实现 Web 首批业务：SQLite、令牌认证、同源页面、WebSocket、项目／任务独立 pi RPC 会话。真实模型联调与任务执行编排尚未验收，见 [Web 启动说明](../apps/web/README.md) 与 [本批验证](../docs/development/web-service-verification.md)。

语言基线：Go 1.25.0；直接依赖：`chi v5.2.3`、`coder/websocket v1.8.15`、`modernc.org/sqlite v1.46.1`。本机验收使用 `go1.27.0-X:nodwarf5`（Linux amd64），不冒充 Go 1.25 稳定工具链及其他系统验证。命令指定 `GOTOOLCHAIN=local`，不自动安装新工具链。

## 命令

从仓库根目录先进入模块：

```bash
cd server
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go test -race ./...
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go mod verify
# 输出应为空；需要修改格式时使用 gofmt -w cmd internal。
gofmt -l cmd internal

mkdir -p bin
GOTOOLCHAIN=local go build -o bin/kaiwu-server ./cmd/kaiwu-server
python3 scripts/smoke_test.py bin/kaiwu-server
./bin/kaiwu-server
```

`bin/` 不纳入 Git。也可用 `GOTOOLCHAIN=local go run ./cmd/kaiwu-server` 开发启动；信号烟测使用编译后的真实二进制，不使用 go run 包装进程。

另开终端：

```bash
curl --noproxy '*' http://127.0.0.1:7331/healthz
```

响应：`{"status":"ok","service":"kaiwu-server"}`。仅说明 HTTP 基础服务能响应，不代表其他组件就绪。

## 配置与安全

基础监听配置 `KAIWU_LISTEN_ADDR`，默认 `127.0.0.1:7331`。用户已指定本机启动固定沿用 `7331` 端口；端口占用时先核对已有服务，不自行换端口。例如：

```bash
KAIWU_LISTEN_ADDR=127.0.0.1:7331 ./bin/kaiwu-server
```

- 默认数值回环；用户明确选择私网 IP 时要求有效令牌认证工作台，1–65535 端口。
- 主机名、IPv4-mapped IPv6、IPv6 zone、未认证私网地址、公网／通配地址、空值、端口 0 均拒绝。
- 设置 `KAIWU_WEB_TOKEN`（至少 32 字节随机值）启用本机认证工作台；未设置时仅健康检查。用户现已授权指定私网 IP 的局域网监听，必须启用令牌认证；公网／通配监听仍拒绝，TLS／设备配对另行设计。
- GET/HEAD `/healthz`；Web 模式增加 `/api/login`、`/api/logout`、`/api/snapshot`、`/api/commands`、`/api/operations/:id`、`/api/events`；不放行跨域。
- 日志写 stderr，记录方法、路径、状态、耗时和生命周期；不记录请求内容、查询参数、Authorization 或 panic 原值。

## 停止与错误

Ctrl+C（SIGINT）或 SIGTERM 发起优雅停止，最长等待 5s；超时强制关闭连接并以非零退出码报告失败。

配置错误、端口占用、意外 Serve 失败和关闭错误也是非零退出。HTTP 客户端断线不拥有服务生命周期。

HTTP 默认限制：ReadHeaderTimeout 5s、ReadTimeout/WriteTimeout 15s、IdleTimeout 60s、MaxHeaderBytes 1MiB。WebSocket 已绑定服务根 context，拥有独立 5s 写入／ping 和 20s 心跳；客户端断线不拥有 pi 生命周期。强制关闭连接不等于能够杀死任意不响应 context 的 Go 处理器；Linux 开发会话额外管理独立进程组，停止与异常退出回收同组命令子进程；它不是系统沙箱，不承诺回收主动脱离组的进程。

## 已修正的边界与后续约束

- 临时 Accept 错误接入同一 slog JSON sink，有一次临时错误＋真实回环 Listener 的回归测试，不修改全机 FD 限制。
- CUSTOM /missing 返回 404 且无 Allow；CUSTOM /healthz 仍为 405。匹配保留 chi RawPath/Path 语义。
- GET/HEAD 健康表示的 Content-Length 均为 41；HEAD 正文为空。JSON 先编码再提交响应，编码失败不能留下空 200。

以上路由描述属于基础 HTTP 阶段；当前已新增 Web API，未知 API 保持 JSON 错误，页面深链接走 SPA。详见 [初次验收](../docs/development/go-foundation-review.md) 与 [修正/回归记录](../docs/development/go-foundation-polish.md)。

## 验证与后续

- [实际验证记录](../docs/development/go-foundation-verification.md)
- [规格](../docs/superpowers/specs/2026-10-11-go-foundation-design.md)
- [实施计划](../docs/superpowers/plans/2026-10-11-go-foundation.md)

`internal/pirpc/` 已按获批 [规格](../docs/superpowers/specs/2026-10-11-go-pi-rpc-design.md) 和 [计划](../docs/superpowers/plans/2026-10-11-go-pi-rpc.md) 实现；自有后端及 RPC 测试全部 Go；后续 Web 批次新增 SQLite／WebSocket 两项直接依赖。`integrations/pi/` 仍仅为说明，没有 TypeScript 扩展或 Node SDK 宿主。

- `Start(lifetimeCtx, LaunchSpec, Options)`：显式绝对程序/目录及非 nil 环境；`get_state` 握手不是模型或任务就绪证明。
- `Call`：普通带 ID 请求；响应不等于任务完成。取消不 abort/重发，返回 NotSent/Uncertain/Acknowledged 语义；不接受 `extension_ui_response`。
- `NextEvent`：内部单消费者有界 FIFO，超限故障，已入队事件可排空；不是双端广播服务。
- `Close`/`Wait`：独立于调用者断线的直接子进程清理；检查 Cause/CleanupError/Reaped/Forced，不能仅检查 Wait error。未确认回收保留 stopping Client；Wait 超时仍返回当前快照。
- `Diagnostics`：显式读取 stderr 尾部副本，可能含秘密，不自动写日志；`Stats` 提供忽略响应计数。

RPC 基础批次仅用隔离 Go 假进程测试，不依赖 pi 安装；当前 Web 已经经 Manager 接入此包，仍只用假进程进行运行验收。实际命令与未验证项见 [RPC 验证记录](../docs/development/go-pi-rpc-verification.md)，独立验收见 [审查记录](../docs/development/go-pi-rpc-review.md)。独立只读会话生命周期已实现；Linux 开发进程组已通过隔离进程测试；真实模型联调、模型切换、MCP/权限、主动脱离进程组的后代及原生客户端尚未验收。

## Web 服务配置

`KAIWU_DATA_DIR` 为绝对数据目录，默认当前工作目录下 `.kaiwu/`。`KAIWU_PI_PROGRAM` 可指定 pi 程序绝对路径，未设置时自动从服务 PATH 的绝对目录查找。模型可在 Web 的“默认模型”／聊天“选择模型”中从实际 pi 列表选择；环境默认 `KAIWU_PI_PROVIDER`／`KAIWU_PI_MODEL`／`KAIWU_PI_THINKING` 仍可选。检测与选择沿用服务器 pi 的 auth.json／models.json（`PI_CODING_AGENT_DIR` 或 `~/.pi/agent`），仅在私有副本中使用；禁用扩展及命令凭据，不回写个人设置。无可执行 pi 或未选模型时仍可管理看板，发送前提示对应缺项。见 [完整启动步骤](../apps/web/README.md)。

生产构建先运行仓库根目录的 `npm --prefix apps/web run build`，再构建 Go；静态资源通过 `internal/webui` 嵌入。仅有 `.gitkeep` 时 Go 仍可编译，但页面返回“请先构建”说明；健康检查不证明 UI 已构建。令牌、数据库和 pi 日志不能提交。

服务停机先取消会话所有权、关闭 WebSocket、回收 pi 及开发进程组，再关闭存储。任务 OS 锁和持久进程组归属记录阻止跨宿主双写；强杀后恢复必须确认旧组已无活动进程，启动窗口归属未知时拒绝执行，不根据保存的 PID 直接 kill。未确认回收／清理出错向 Run/main 返回失败，不把 HTTP Shutdown 成功当作任务完成。
