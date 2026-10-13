# Go 服务基础：实际验证记录

状态：首批本机实现、独立验收与审查后复测已完成。初次暂缓的 3 项 Minor 后来获用户授权修正；当前 32 项顶层测试与复测通过，限定独立核对亦通过，结果见 [边界修正记录](go-foundation-polish.md)。没有 Git 提交/推送，没有常驻运行服务。

## 实际变更

- server 独立 Go module；config/httpapi/app 与命令入口。
- 回环监听配置、GET/HEAD 健康检查、JSON 错误、结构化日志。
- HTTP 期限和信号停止；关闭超时强制断开连接并报告失败。
- apps/desktop、apps/android、integrations/pi、contracts 仅说明入口。
- .gitignore、.editorconfig、模块导航及当前状态文档。

## 环境与版本

- OS/CPU：Linux amd64。
- 实际 Go：`go1.27.0-X:nodwarf5`，非稳定工具链。
- go.mod 语言基线：`go 1.25.0`；没有验证 Go 1.25 稳定工具链。
- 产品依赖：`github.com/go-chi/chi/v5 v5.2.3`；没有 WebSocket/数据库依赖。
- 使用本机已有 cc、Python 3；没有全局安装或客户端 SDK 下载。

## TDD 证据

所有 Go 命令使用 GOTOOLCHAIN=local：

| 阶段 | RED | GREEN |
| --- | --- | --- |
| 配置 | go test ./internal/config：Load/Config 未定义，退出 1 | 同命令退出 0 |
| HTTP 路由 | go test ./internal/httpapi：NewHandler 未定义，退出 1 | 健康/HEAD/404/405 路由测试退出 0 |
| 日志与恢复 | HTTP middleware 测试：observeAndRecover 未定义，退出 1 | HTTP 全套测试退出 0 |
| 启停 | go test ./internal/app：Run/newServer/serve 未定义，退出 1 | go test ./internal/app -count=10 退出 0 |
| 命令入口 | go test ./cmd/kaiwu-server：run 未定义，退出 1 | 同命令退出 0 |

覆盖：默认/非法/非回环地址、HEAD/错误响应、日志隐私、panic 前后写入与 flush、真实连接中止、取消前启动、端口占用、在途请求、关闭超时、意外 Serve 退出和退出码。

## 最终本机命令

以下在 server/ 执行，均实际退出 0：

- `GOTOOLCHAIN=local go test ./...`
- `GOTOOLCHAIN=local go test -json -count=1 ./...`：28 项顶层测试通过、4 个包通过、0 失败；子测试不重复计入 28 项。
- `GOTOOLCHAIN=local go vet ./...`
- `GOTOOLCHAIN=local go test -race ./...`：4 个包通过。
- `gofmt -l cmd internal`：输出为空。
- `GOTOOLCHAIN=local go mod tidy`、`GOTOOLCHAIN=local go mod verify`：all modules verified。
- `GOTOOLCHAIN=local go build -o <本步骤临时目录>/kaiwu-server ./cmd/kaiwu-server`
- `python3 scripts/smoke_test.py <本步骤临时目录>/kaiwu-server`：

```text
PASS SIGINT: health, exit=0, listener released
PASS SIGTERM: health, exit=0, listener released
```

烟测使用随机回环端口，禁用继承的 HTTP 代理；实际启动二进制，检查健康 JSON/响应头、结构化启停日志、退出码和监听释放。脚本 finally 回收进程，构建 shell trap 仅删除本步骤自己的临时目录。

## 独立验收

Paseo 子 Agent bd16bb0d-b90d-4098-a2d7-127aee2e0a62 按实际「方案设计，架构思考」配置完成只读验收：无 Critical/Important，本批次通过，3 项 Minor 暂缓。完整结果及未判断项见 [独立验收记录](go-foundation-review.md)。

审查结束后协调 Agent重新执行普通测试/race 的 -count=1、vet、格式、mod verify、临时构建和 SIGINT/SIGTERM 烟测，均实际退出 0；无产品代码修改。

初次发现：低 FD 时标准库 Accept 错误混入文本日志；CUSTOM /missing 返回 405 和不准确 Allow；HEAD 没有 GET 的 Content-Length。这些发现保留为历史证据。后续用户授权修正的 RED/GREEN、32 项测试、实际二进制结果及独立核对见 [修正记录](go-foundation-polish.md)。本文件中 28 项测试是首批当时的计数，不是当前数量。

## 尚未验证或实现

- 稳定 Go 工具链及 Windows/macOS 服务运行。
- PC 三平台原生 UI、Android 工程、SQLite 和更新。
- pi RPC/真实 Agent、模型切换、工具权限、任务闭环和进程树取消。
- 网络认证、持久化/恢复、WebSocket 事件和客户端重连。
- GitHub Actions、签名、Release。

健康检查只证明 HTTP 基础服务响应，不能代替这些结果。没有模型调用或额度测试，也没有部署到局域网/公网。
