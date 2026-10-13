# 仓库模块划分与 Go 服务基础设计

状态：**书面规格、实施计划及执行方式已获用户确认；首批代码已实现，通过本机与独立验收；初次 3 项非阻塞边缘偏差后来获用户授权修正，当前状态见 [修正记录](../../development/go-foundation-polish.md)。**

> 后续决定更新：用户已明确自有后端全部 Go，并确认 RPC 模块边界为 `server/internal/pirpc/`。本文关于未来 TypeScript 扩展、`agent/pi` 位置和扩展工具桥的描述仅保留为当时方案历史，已被 [RPC 基础规格](2026-10-11-go-pi-rpc-design.md) 与 [当前架构](../../architecture.md) 的对应决定取代；已完成的 Go 基础实现与验收不变。

## 1. 目标与确认依据

开悟是个人开发工作台，当前先打磨 pi 主 Agent＋执行 Agent 的开发闭环。用户已同意模块化 Go 服务架构及 Go＋pi 接入基础的推进方向，并明确要求：先把 Go 搞定，在本项目内区分服务端、电脑端及未来安卓等各端。

本规格限定第一步：划分仓库模块，建立可构建、可测试、可在回环地址启动和停止的 Go 工程。pi RPC 接入是紧随其后的独立功能批次，不将工程健康检查冒充 Agent 接入成功。

成功标准：模块归属清楚；Go 基础入口实际可运行；后续接入 pi 和双端时不需要把服务代码从根目录重新搬走。

## 2. 方案选择

采用单仓库、按部署单元分模块：

- 相比所有 Go 文件放根目录，服务与客户端依赖、构建命令及产物的边界更明确。
- 相比分成多个仓库，协议变更、文档和跨端功能能在同一开发批次协调，当前不增加跨仓库发布成本。
- 不将服务端、所有客户端拆成微服务或通用插件平台；独立模块不等于独立仓库，也不等于进程间通信。

## 3. 仓库布局

```text
kaiwu/
  README.md
  AGENTS.md
  TODO.md
  .gitignore
  .editorconfig
  docs/
    product-requirements.md
    architecture.md
    roadmap.md
    research/
    superpowers/specs/
    superpowers/plans/
  server/
    README.md
    go.mod
    go.sum
    cmd/kaiwu-server/
      main.go
    internal/
      app/                 服务组装和启停生命周期
      config/              配置读取与校验
      httpapi/             HTTP 路由与响应
  apps/
    README.md
    desktop/
      README.md            Nim + Leaf 模块入口，本批次仅说明
    android/
      README.md            Kotlin/KMP/Compose 模块入口，本批次仅说明
  integrations/
    pi/
      README.md            可信 pi 扩展入口，本批次仅说明
  contracts/
    README.md              跨端协议归属与版本规则，本批次仅说明
```

### 模块依赖规则

- `server/` 是独立 Go module：`github.com/SatoriTours/kaiwu/server`。根目录不是 Go module；当前只有一个 Go module，不添加 `go.work`。
- `apps/desktop/` 后续独立管理 Nim/Leaf 构建，不直接引用 Go 的 `internal` 包，不修改 Leaf 仓库。
- `apps/android/` 后续管理 Android 工程；KMP 共享代码放在该模块内部，准确 Gradle 布局在安卓批次确定。
- 新增客户端使用 `apps/<端名>/`，不提前创建 iOS、Web 等未批准工程。
- `integrations/pi/` 仅放 TypeScript 可信扩展；Go 的 pi RPC 适配将来放 `server/internal/agent/pi/`，不能混在客户端或扩展目录中。
- `contracts/` 放产品协议说明、版本化 schema 和固定测试样例，不放密钥、真实会话，也不尝试共享 Go/Nim/Kotlin 业务源码。本批次只写归属说明，不虚构尚未设计的任务协议。
- `docs/` 是跨模块产品和架构记录；模块 README 是各模块的开发入口。
- 无源码或构建文件的模块明确标记为说明入口，不能报告为可构建工程。

## 4. 服务内部边界与后续架构

首批仅创建 `config`、`httpapi`、`app` 和命令入口，不为未来功能堆砌空包或空接口。

依赖方向：命令入口 → app → config/httpapi。配置包不依赖 HTTP；HTTP 层不读取环境变量、不管理进程；app 管理监听和启停，main 管理信号与退出码。

后续功能按职责增量加入：

| 未来职责 | 归属与边界 |
| --- | --- |
| 客户端命令和事件 | httpapi；只暴露开悟协议，不透传所有 pi 字段 |
| 任务协调 | task；配置快照、状态迁移、主/执行关系 |
| pi 适配 | agent/pi；RPC、命令关联、会话和运行状态 |
| 编排工具桥 | orchestration；接收可信扩展的派发/查询/结果回收 |
| 执行资源 | workspace/process；worktree、单写入锁、进程生命周期 |
| 存储与事件 | storage/events；持久化、审批记录、游标与补发 |

这些是责任归属，不是本批次已获执行的功能。先实现具体用例和测试，再在确有替换需要时提取接口。

## 5. Go 工程选择

已随本规格确认的版本基线：

- `go.mod` 声明 `go 1.25.0`，首批只使用该基线已有的标准库功能；这不是所有未来服务版本的永久支持承诺。
- 路由使用 `github.com/go-chi/chi/v5 v5.2.3`，实施时下载固定版本并生成 `go.sum`，不使用浮动 `latest`。
- 日志使用标准库 `log/slog`，不额外引入日志框架。
- WebSocket 后续使用已选的 `coder/websocket`；本批次没有事件接口，暂不下载或空接 WebSocket 依赖。
- 不新增数据库、配置框架、依赖注入框架、Node 包或客户端 SDK。

本机实际工具链为 `go1.27.0-X:nodwarf5`、Linux amd64，是非稳定版本样本。用它通过本机测试不能代替 Go 1.25 稳定工具链或其他系统验证。默认不安装/下载其他工具链；实施时使用 `GOTOOLCHAIN=local`，如现有工具链不能构建则报告阻塞。

chi 固定版本做过网络搜索核对；直接获取上游页面被当前网络工具的地址安全规则阻止。尚未实际下载模块或构建，依赖可获取性以实施阶段结果为准。

## 6. 配置、HTTP 与安全边界

### 配置

只接受 `KAIWU_LISTEN_ADDR`，默认 `127.0.0.1:7331`。本批次不添加配置文件、复杂命令行参数或动态热更新。

- 只允许数值回环 IP（IPv4 127.0.0.0/8 或 IPv6 ::1）加端口，不接受主机名或依赖 DNS 判断本地性。
- 生产入口端口范围为 1–65535；测试由生命周期组件直接绑定回环端口 0，避免固定端口冲突。
- 拒绝空主机、0.0.0.0、局域网 IP 和非法端口。环境变量不存在时使用默认值；显式设置为空视为配置错误。
- 当前未设计认证，不支持远程业务访问。不启用宽松 CORS，不返回凭据或本机路径。

### HTTP

唯一业务路由为 `GET /healthz`，返回 200 和固定 JSON：

```json
{"status":"ok","service":"kaiwu-server"}
```

这只证明 HTTP 服务能响应，不证明 pi、数据库、客户端、认证或任务系统就绪。`HEAD /healthz` 与 GET 使用相同状态和响应头，无响应体。

- 响应 `Content-Type: application/json`、`Cache-Control: no-store`。
- 未知路径返回 404；已知路径不支持的方法返回 405 和 `Allow: GET, HEAD`。
- HTTP 错误使用固定 JSON `{"error":{"code":"...","message":"..."}}`；首批代码为 `not_found`、`method_not_allowed`、`internal_error`，不泄露堆栈或内部细节。
- panic 恢复限于处理器边界：记录错误；在响应尚未写出时返回 500。进程启动失败不能被伪装为健康。
- 日志写 stderr，采用结构化 JSON；记录启动、实际监听地址、请求方法/路径/状态/耗时和停止结果，不记录请求内容、查询参数、认证信息。

请求流程：请求 → HTTP 记录与恢复 → chi 路由 → 健康处理器 → JSON 响应。路由可独立使用 `httptest` 验证。

## 7. 生命周期与失败处理

- main 读取并验证配置，再创建监听器；绑定成功后才记录启动成功。
- 应用接收 context、配置和 logger，支持注入取消信号；不通过关闭客户端连接停止服务。
- `http.Server` 明确设置 ReadHeaderTimeout=5s、ReadTimeout=15s、WriteTimeout=15s、IdleTimeout=60s、MaxHeaderBytes=1MiB。未来流式接口需要独立复核这些限制。
- SIGINT/SIGTERM 触发停止：调用 `Shutdown`，使用独立于已取消运行 context 的 5s 截止时间；超时则调用 `Close`，记录强制关闭并返回非零退出状态。
- 正常取消/优雅停止退出码为 0；配置错误、端口占用、意外 Serve 错误和关闭失败退出码非零。
- 启动失败关闭已获取资源；正常的 `http.ErrServerClosed` 不报告成意外故障。
- 测试和本机烟测结束后不残留服务。跨平台进程树取消不属于此 HTTP 骨架，不宣称已完成 Agent 取消能力。

## 8. 验收标准

在 `server/` 执行并据实记录：

1. `gofmt` 检查：无未格式化 Go 文件。
2. `GOTOOLCHAIN=local go test ./...`：配置、HTTP、生命周期测试通过。
3. `GOTOOLCHAIN=local go vet ./...`：通过。
4. `GOTOOLCHAIN=local go test -race ./...`：在本机工具链和 C 编译环境允许时执行；失败或无法执行单列，不用普通测试代替。
5. `GOTOOLCHAIN=local go build ./cmd/kaiwu-server`：可构建；验证产物放临时目录或忽略的构建目录，不提交。
6. 回环烟测：启动实际二进制，查询健康接口，发送终止信号，验证退出与监听释放。

必要测试场景：默认/显式配置、IPv4/IPv6 回环、禁止非回环、空值和非法端口；健康响应与 HEAD；404/405 与响应头；处理器 panic；动态监听、正常取消、端口占用、在途请求优雅停止和关闭超时资源回收。

本批次不调用 pi 或模型，不加载个人扩展；不声称通过跨平台原生 UI、真实 Agent 或 GitHub Actions 验收。正式服务部署系统及稳定工具链构建矩阵在后续批次设计。

## 9. 文档与授权边界

实施后更新根 README、server README、架构方向、初始化范围和路线图：分别记录已实现、未实现及实际命令结果，提供从仓库根目录进入各模块的导航。

忽略构建产物、运行数据、凭据和本地个人配置；不添加许可证，不修改 Leaf，不安装全局工具，不暴露局域网服务。

仓库 main 尚无提交。不得为创建 worktree 擅自提交；本批次小型初始化在用户确认执行方式后可由单一写入者在当前目录完成。如需编码委派，先读取 Paseo profiles，明确唯一写入者和目录；审查不并发写入。

本规格获批后才编写实施计划。实施计划需再由用户审核并选择执行方式；当前用户同意推进方向，不被解释为 Git 提交、合并、推送或发布授权。
