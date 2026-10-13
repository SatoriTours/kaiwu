# 仓库模块划分与 Go 服务基础 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 项目 Paseo 编排及用户授权边界优先；不自动提交。

**Goal:** 在单仓库中区分服务端、客户端与 pi 扩展，交付可测试、可回环启动和优雅停止的 Go 服务基础。

**Architecture:** 服务端独立 Go module，命令入口组装 config、httpapi、app。其他端仅建立说明入口，不安装客户端工具链。pi、WebSocket、数据库和认证不属于本批次。

**Tech Stack:** Go 语言基线 1.25.0，chi v5.2.3，标准库 net/http、log/slog；使用现有本机工具链，测试脚本仅使用现有 Python 3 标准库。

**Spec:** `docs/superpowers/specs/2026-10-11-go-foundation-design.md`（已获用户“好的，继续”确认）。

**执行状态：** 用户已确认推荐执行方式；三个任务、本机及初次独立验收已完成，当时 3 项非阻塞 Minor 暂缓。用户随后授权小范围修正，三项已通过回归与限定独立核对；见 `docs/development/go-foundation-polish.md`。初次证据见 `docs/development/go-foundation-verification.md` 和 `docs/development/go-foundation-review.md`。未提交或推送。

> 后续决定更新：自有后端全部 Go；本计划中的 pi 扩展入口只记录首批说明目录的历史用途，不授权后续 TypeScript 开发。当前接入目录职责和 RPC 包位置见 [架构](../../architecture.md) 与 [RPC 基础规格](../specs/2026-10-11-go-pi-rpc-design.md)。本计划已执行的 Go 基础及既有 Python 烟测记录不改写；本批次新 RPC 测试全部用 Go。

## Global Constraints

- Go module 为 `github.com/SatoriTours/kaiwu/server`；`go 1.25.0`；根目录没有 go.mod/go.work。
- 仅新增 Go 依赖 `github.com/go-chi/chi/v5 v5.2.3`，生成 go.sum，不使用 latest。
- 所有 Go 命令使用 `GOTOOLCHAIN=local`；不安装全局工具或其他 Go 工具链。
- `KAIWU_LISTEN_ADDR` 默认 `127.0.0.1:7331`；仅数值回环 IP、端口 1–65535；显式空值为错误。
- GET/HEAD `/healthz`；JSON 健康对象固定为 `{"status":"ok","service":"kaiwu-server"}`；不得表示 pi 或任务已就绪。
- HTTP 超时：ReadHeaderTimeout=5s，ReadTimeout=15s，WriteTimeout=15s，IdleTimeout=60s，MaxHeaderBytes=1MiB；Shutdown 超时=5s。
- PC Nim+Leaf 三平台及 Android Kotlin/KMP/Compose 仍保留首版目标；本批次只是模块说明入口。
- 不调用真实模型、不加载个人扩展、不实现 pi RPC、不修改 Leaf、不暴露非回环服务。
- 不提交、合并、推送、Tag、Release、选许可证或清理已有成果。
- 空仓库没有 worktree 基线。执行前核对 Git 和其他写入者；本批次仅允许一个编码 Agent 写当前 checkout，审查只读且在编码停止后进行。
- 实施计划须获用户审核并选择执行方式后执行；该审核已完成。实现与验收结果以独立验证记录为准，不以计划勾选替代证据。

## Review Focus

1. 看似本地的主机名/IPv4 映射 IPv6/zone 地址：不得借名称解析或格式歧义允许非回环绑定；任务 1 固定测试。
2. 已取消 context：不得短暂启动监听器或留下服务；任务 3 固定测试。
3. 处理器写出响应后 panic：不得继续追加伪装成功的 JSON 500；任务 2 固定测试。
4. 请求包含查询参数、请求体、Authorization：日志不得记录这些内容，日志状态必须与客户端一致；任务 2 固定测试。
5. 取消时有在途请求、关闭超时或 Serve 已异常退出：不能报告正常完成或让控制 goroutine 永久阻塞；任务 3 固定测试。

## 文件职责

| 文件 | 职责 |
| --- | --- |
| `server/go.mod`, `server/go.sum` | 独立模块与固定依赖 |
| `server/internal/config/config.go`, `config_test.go` | 配置读取、校验和表驱动测试 |
| `server/internal/httpapi/router.go`, `router_test.go` | 路由、健康/错误响应及协议测试 |
| `server/internal/httpapi/middleware.go`, `middleware_test.go` | JSON 请求日志、panic 恢复和测试 |
| `server/internal/app/app.go`, `app_test.go` | HTTP Server 默认值、启动/关闭和生命周期测试 |
| `server/cmd/kaiwu-server/main.go`, `main_test.go` | 环境、信号、stderr logger、退出码 |
| `server/scripts/smoke_test.py` | 实际二进制回环/信号烟测，不调用模型 |
| `.gitignore`, `.editorconfig` | 精确忽略构建/运行数据及私密配置；基本格式 |
| 各模块 README 与现有 docs | 导航、命令、边界、实际结果 |

新增文件均在本仓库内。测试放在被测包中；不新增泛用测试框架。

---

### Task 1：仓库模块入口与配置

**Files:**
- Create: `.gitignore`, `.editorconfig`, `server/go.mod`, `server/internal/config/config.go`, `server/internal/config/config_test.go`。
- Create: `server/README.md`, `apps/README.md`, `apps/desktop/README.md`, `apps/android/README.md`, `integrations/pi/README.md`, `contracts/README.md`。

**Interfaces:**
- Consumes: 注入的 `lookupEnv func(string) (string, bool)`，不依赖真实环境测试。
- Produces: `type Config struct { ListenAddr string }`；`Load(lookupEnv func(string) (string, bool)) (Config, error)`；`(Config).Validate() error`。
- `Load` 调用 Validate。后续 app.Run 再校验配置，防止直接构造 Config 绕过限制。

- [x] **Step 1：创建模块与说明入口。** go.mod 仅声明指定 module、Go 基线和 chi 固定依赖；暂不下载依赖。模块 README 明确“本批次仅说明/尚无可构建客户端”；根目录不创建其他端的构建文件。忽略 `/server/bin/`、`/server/kaiwu-server`、`/.kaiwu/`、模块下 `.kaiwu/`、`.env`/`.env.*`（保留 `.env.example`）、明确命名的本地凭据文件；不使用全局 `*.json`/`*.db` 之类会隐藏测试样例的规则。

- [x] **Step 2：写配置失败测试。** 使用表驱动，固定名称和断言：
  - `TestLoadDefault`：未设置环境时 ListenAddr == `127.0.0.1:7331`。
  - `TestLoadLoopback`：`127.0.0.1:7332`、`127.0.0.2:1`、`[::1]:65535` 有效。
  - `TestLoadRejectsUnsafeAddress`：空字符串、`:7331`、`0.0.0.0:7331`、`192.168.1.2:7331`、`localhost:7331`、`[::]:7331`、端口 `0`/`65536`/`+7331`、前后空格、缺端口全部报错。
  - 同一拒绝表覆盖 `[::ffff:127.0.0.1]:7331`、`[::1%lo]:7331`：明确拒绝 IPv4-mapped 与带 zone 的 IPv6 表示，避免不同 OS 绑定语义分歧。
  - `TestValidateDirectConfig`：直接构造非回环 Config 也报错；校验不执行 DNS 查询。

  默认值测试的核心断言（测试中为配置包导入 `testing`）：

  ```go
  func TestLoadDefault(t *testing.T) {
      cfg, err := Load(func(string) (string, bool) { return "", false })
      if err != nil || cfg.ListenAddr != "127.0.0.1:7331" {
          t.Fatalf("Load() = %+v, %v", cfg, err)
      }
  }
  ```

- [x] **Step 3：记录 RED。** `cd server && GOTOOLCHAIN=local go test ./internal/config`；预期因 Config/Load 尚未定义而失败。仅模块下载/网络错误不算有效 RED。

- [x] **Step 4：实现 config.go。** 使用 `net.SplitHostPort`、`net/netip.ParseAddr` 和十进制数字端口检查；不静默 trim 输入。只允许非 mapped、无 zone 的 `Addr.IsLoopback()` 地址。错误说明配置项和原因，不输出环境原值。

- [x] **Step 5：记录 GREEN。** `cd server && GOTOOLCHAIN=local go test ./internal/config`；预期全部通过。检查文件清单没有客户端 SDK、密钥或根 Go module；不提交。

### Task 2：HTTP 协议、日志与错误边界

**Files:**
- Create: `server/internal/httpapi/router.go`, `router_test.go`, `middleware.go`, `middleware_test.go`。
- Create/update: `server/go.sum`, `server/go.mod`。

**Interfaces:**
- Consumes: 标准库 `*slog.Logger` 和固定 chi 依赖。
- Produces: `httpapi.NewHandler(logger *slog.Logger) http.Handler`。
- 包内函数：`observeAndRecover(logger *slog.Logger, next http.Handler) http.Handler`，便于注入 panic 处理器测试；响应包装器提供 `Unwrap() http.ResponseWriter`，不声称已支持 WebSocket。

- [x] **Step 1：写 HTTP 失败测试。**
  - `TestHealthGet`：200、JSON 对象恰为固定健康对象、Content-Type=`application/json`、Cache-Control=`no-store`。
  - `TestHealthHead`：200、与 GET 相同的 Content-Type/Cache-Control、响应体为空。
  - `TestNotFound`：未知路径 404，error.code=`not_found`，无堆栈。
  - `TestMethodNotAllowed`：POST `/healthz` 405，Allow=`GET, HEAD`，error.code=`method_not_allowed`。
  - 对健康/错误响应验证合法 JSON、不含 CORS 放行头；HEAD 的未知路径和不支持方法无响应体。

  GET 健康响应测试（包内测试导入 `encoding/json`、`io`、`log/slog`、`net/http/httptest`、`testing`）：

  ```go
  func TestHealthGet(t *testing.T) {
      w := httptest.NewRecorder()
      h := NewHandler(slog.New(slog.NewJSONHandler(io.Discard, nil)))
      h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
      if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" ||
          w.Header().Get("Cache-Control") != "no-store" {
          t.Fatalf("response: status=%d headers=%v", w.Code, w.Header())
      }
      var body map[string]string
      if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body) != 2 ||
          body["status"] != "ok" || body["service"] != "kaiwu-server" {
          t.Fatalf("body=%s", w.Body.String())
      }
  }
  ```

- [x] **Step 2：记录 RED。** `cd server && GOTOOLCHAIN=local go test ./internal/httpapi`；预期 NewHandler 未定义。先保留该失败证据，再获取 chi，避免把缺依赖当行为失败。

- [x] **Step 3：实现 router.go。** 获取固定 chi：`GOTOOLCHAIN=local go mod download github.com/go-chi/chi/v5`。显式注册 GET/HEAD，设置 chi NotFound/MethodNotAllowed；错误 message 分别为 `route not found`、`method not allowed`、`internal server error`。使用固定结构编码 JSON，不依赖 map 拼接 HTML；统一设置响应头，HEAD 不写响应体。

- [x] **Step 4：验证路由 GREEN。** 临时实现最小 NewHandler 路由，不声称日志已完成；运行 `GOTOOLCHAIN=local go test ./internal/httpapi -run 'TestHealth|TestNotFound|TestMethodNotAllowed'`，预期通过。

- [x] **Step 5：写 middleware 失败测试。**
  - `TestPanicBeforeWrite`：panic 恢复为 500，error.code=`internal_error`，日志不包含 panic 中的私密文本。
  - `TestPanicAfterWrite`：已写 200/部分响应后 panic，包装器不能追加 JSON 500；记录错误并重新抛出 `http.ErrAbortHandler`，由 net/http 中止连接，避免客户端收到拼接成功结果。
  - `TestRequestLogPrivacy`：发送带 `?secret=query-secret`、`Authorization: Bearer header-secret` 和 `body-secret` 的请求；JSON 日志含 method/path/status/duration，不含这三个秘密。
  - `TestRequestLogStatus`：分别请求 200/404/405/500，日志 status 与实际响应一致；测试使用互斥保护的缓冲 writer，避免并发测试日志采集本身发生 race。

- [x] **Step 6：记录 middleware RED。** `GOTOOLCHAIN=local go test ./internal/httpapi -run 'TestPanic|TestRequestLog'`；预期包装函数未定义或断言失败。

- [x] **Step 7：实现 middleware.go 并接入 NewHandler。** 请求日志使用 slog JSON，记录 method/path/status/duration；不记录查询、请求体、headers 或 panic 值。跟踪首次 WriteHeader/Write；未提交响应时 panic 写安全 500，已提交时中止连接。原始 `http.ErrAbortHandler` 不转成 JSON 500；依旧回收日志状态。

- [x] **Step 8：验证 GREEN 与依赖。** `GOTOOLCHAIN=local go test ./internal/httpapi`、`GOTOOLCHAIN=local go mod tidy`、`GOTOOLCHAIN=local go mod verify`；预期通过，go.mod 仍只有固定 chi 产品依赖。不提交。

### Task 3：生命周期、命令入口与最终验收

**Files:**
- Create: `server/internal/app/app.go`, `app_test.go`, `server/cmd/kaiwu-server/main.go`, `main_test.go`, `server/scripts/smoke_test.py`。
- Modify: `server/README.md`, `README.md`, `docs/architecture.md`, `docs/initialization-scope.md`, `docs/roadmap.md`, `TODO.md`。

**Interfaces:**
- Consumes: `config.Config.Validate()` 和 `httpapi.NewHandler(*slog.Logger) http.Handler`。
- Produces: `app.Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error`。
- 包内测试接口：`newServer(handler http.Handler) *http.Server`；`serve(ctx context.Context, srv *http.Server, listener net.Listener, shutdownTimeout time.Duration, logger *slog.Logger) error`。serve 拥有传入 listener；Run 校验配置并绑定 listener，委托 serve。
- 命令内测试接口：`run(ctx context.Context, lookupEnv func(string) (string, bool), stderr io.Writer) int`；main 创建信号 context 并调用 run，os.Exit 仅在 main。

- [x] **Step 1：写生命周期失败测试。**
  - `TestServerDefaults`：逐项断言设计的 5s/15s/15s/60s/1MiB 默认值。
  - `TestServeHealthAndCancel`：测试自行绑定 `127.0.0.1:0`、请求健康接口、取消 context、serve 在截止时间内返回 nil，端口关闭。
  - `TestRunRejectsUnsafeConfig`：直接非回环 Config 返回错误，不产生启动成功日志。
  - `TestRunCanceledBeforeStart`：已取消 context 返回 nil，无启动成功日志、无绑定；可在一个已占用的回环地址上调用证明没有尝试绑定。
  - `TestRunPortInUse`：保持真实回环 listener，占用地址上 Run 返回错误，保留原 listener 可用性，不记录启动成功。
  - `TestServeGracefulInflight`：用 channel 控制在途处理器；取消后请求不被立即中断，释放后正常收到响应，serve 返回 nil。
  - `TestServeShutdownTimeout`：短测试 timeout（50ms），阻塞处理器等待 Request.Context 取消；serve 返回非 nil，连接断开，处理器退出。清理函数兜底释放所有 channel/资源，不使用固定 sleep 判断请求是否已开始。
  - `TestServeUnexpectedExit`：主动关闭 listener 使 Serve 失败；serve 返回非 nil 而不是无限等待 context。

  默认 Server 的测试（包内测试导入 `net/http`、`testing`、`time`）：

  ```go
  func TestServerDefaults(t *testing.T) {
      srv := newServer(http.NotFoundHandler())
      if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 15*time.Second ||
          srv.WriteTimeout != 15*time.Second || srv.IdleTimeout != 60*time.Second ||
          srv.MaxHeaderBytes != 1<<20 {
          t.Fatalf("incorrect HTTP server limits: %+v", srv)
      }
  }
  ```

- [x] **Step 2：记录 RED。** `cd server && GOTOOLCHAIN=local go test ./internal/app`；预期 Run/newServer/serve 未定义。

- [x] **Step 3：实现 app.go。** Run 在获取资源前处理已取消 context；校验配置、绑定、记录启动。newServer 设置规定超时。serve 用缓冲 channel 接收 Serve 结果，在 ctx 取消时用独立 context 调用 Shutdown，超时 Close；等待 Serve goroutine 退出，保留意外 Serve/Shutdown/Close 错误。正常 ErrServerClosed 不作为故障，但不能吞掉关闭超时。只声明监听/连接回收，不承诺强行停止不响应 context 的任意 Go handler。

- [x] **Step 4：生命周期 GREEN。** `GOTOOLCHAIN=local go test ./internal/app -count=10`，预期全部通过且不留下测试 listener/goroutine；检查重复测试没有固定端口依赖。

- [x] **Step 5：写命令入口失败测试。** `TestRunInvalidConfig`：lookup 返回空值，退出码=1、stderr 为 JSON 错误日志；`TestRunCanceled`：默认配置+已取消 context，退出码=0；`TestRunPortInUse`：占用回环地址时退出码=1。运行 `GOTOOLCHAIN=local go test ./cmd/kaiwu-server` 记录 RED。

- [x] **Step 6：实现 main.go。** slog.NewJSONHandler(stderr)，Load→app.Run→返回 0/1；配置和运行错误写 stderr，日志不输出完整环境。main 用 signal.NotifyContext 订阅 os.Interrupt/syscall.SIGTERM；保存 run 返回码，显式调用 stop() 后才 os.Exit(code)，不能依赖被 os.Exit 跳过的 defer 释放信号订阅。运行 `GOTOOLCHAIN=local go test ./cmd/kaiwu-server` 记录 GREEN。

- [x] **Step 7：写并执行实际二进制烟测。** Python 标准库脚本接受二进制路径，在回环临时端口分别启动两次，分别测试 SIGINT/SIGTERM。启动前用 socket 选取空闲端口并关闭保留 socket；若绑定窗口发生冲突明确失败，不重试掩盖错误。有截止时间地轮询健康并同时检查进程退出；断言固定 JSON/响应头，发信号后等待退出码 0 和连接失败。urllib 使用 `ProxyHandler({})` 禁用继承代理，保证请求直达回环；stderr 写入本脚本拥有的临时文件，避免无人消费 PIPE。所有探测/等待均有截止时间，在 finally 中终止/回收子进程，失败只打印有限 stderr。build 产物放临时目录；不留下监听或产物。

- [x] **Step 8：全量验证。** 从仓库根执行：

```bash
cd server
# 以下命令逐条执行、分别记录退出码，不用末尾成功掩盖前序失败。
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race ./...
fmt_files="$(gofmt -l cmd internal)" && test -z "$fmt_files"
GOTOOLCHAIN=local go mod verify
build_dir="$(mktemp -d)"
GOTOOLCHAIN=local go build -o "$build_dir/kaiwu-server" ./cmd/kaiwu-server
python3 scripts/smoke_test.py "$build_dir/kaiwu-server"
# 仅清理由本步骤创建的临时构建目录；不得清理现有用户成果。
```

预期每条通过；race 或本机工具链失败单列报告并定位，不切换工具链/升级依赖绕过。验证后回收本步骤的临时目录，检查无残留服务和仓库内二进制。

- [x] **Step 9：同步文档和记录。** 根 README 导航到各模块，server README 包含 `cd server`、配置、安全限制、构建/测试/启动和停止命令；其他 README 保持说明入口状态。架构文档标注本规格已确认的布局，初始化方案和路线图只勾选真实完成项。记录本机实验工具链、所有实际命令/结果、未验证稳定工具链/跨平台 UI/pi/Actions。保存结果到 `docs/development/go-foundation-verification.md`，不写真实会话或凭据。

- [x] **Step 10：只读验收与交付。** 编码停止后按所选执行方式验收。若采用 Paseo 独立验收，读取当时 profiles，用「方案设计，架构思考」实际配置；明确只读范围及测试命令，禁止提交/安装/创建 worktree/再次派发。汇总实际结果、未验证项、Git 状态和下一批 pi 接入建议。提醒团队协作与通用可配置编排引擎仍为未来方向，不纳入本批次。不提交。

## 自查与执行交接

- 规格第 1–4 节由任务 1 模块入口、任务 3 导航记录覆盖；第 5–6 节由任务 1/2 覆盖；第 7–8 节由任务 3 覆盖；第 9 节由全局约束和任务 3 覆盖。
- 包名、Config、NewHandler、Run 和命令入口 run 在所有任务保持一致。
- 五项 Review Focus 均有命名测试；正式稳定工具链和跨平台验证明确不冒充本机结果。
- 计划只定义当前三个包的实现，不包含未来任务/持久化/客户端代码。
- 已按用户确认方式由当前 Agent 串行实现三个任务，再通过 Paseo 独立验收。各任务共享接口且代码量小，没有每步重复创建编码 Agent。未创建/归档 worktree 或修改 Git 历史。
