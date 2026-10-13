# Go 基础：三项边界修正

状态：用户已确认短设计；三项修正、32 项测试与本机复测、限定范围独立核对均已完成。原审查会话无法续接，实际验收由同一 workspace 的只读替代 Agent 完成。未提交或推送。

这是已存在 HTTP/启停流程的小范围修正，不新增子系统，不另写架构规格或长实施计划。初次发现见 [独立验收记录](go-foundation-review.md)。

## 实际修改与回归证据

### M1：临时 Accept 日志

- 修改 server/internal/app/app.go：在 Serve 启动前将 http.Server.ErrorLog 接入 slog.NewLogLogger(logger.Handler(), slog.LevelError)。
- TestTemporaryAcceptErrorUsesJSONLog 使用一次临时 net.Error 的 Listener，然后交给真实回环 Listener；不消耗全机 FD，不修改资源限制。
- RED：原实现输出默认文本 Accept error，捕获的 JSON 仅有 server stopped，回归测试失败。
- GREEN：临时 Accept 错误作为 ERROR JSON 进入同一 sink，后续真实请求成功、正常取消结束；测试通过。

### M2：未知方法与未知路径

- 修改 server/internal/httpapi/router.go：MethodNotAllowed 回调先用新的 chi RouteContext 对当前 GET 路由进行匹配；保留 chi 的 RawPath/Path 语义。
- TestUnknownMethodRouting 固定 CUSTOM /missing、/healthz/、/%68ealthz → 404 且无 Allow；CUSTOM /healthz → 405、Allow: GET, HEAD。
- RED：三个不存在路径均返回 405；测试失败。
- GREEN：上述组合全部通过。
- 当前只注册 GET/HEAD。未来添加其他方法的业务路由时必须同步调整方法匹配和 Allow；本次不搭通用路由发现框架。

### M3：HEAD 表示头

- 修改 writeJSON：先编码固定 JSON 并保留原换行，再设置 Content-Length；GET 写正文，HEAD 仅写同样的表示头。
- TestHealthHeadContentLength 固定健康表示为 41 字节，GET/HEAD Content-Length 均为 41，HEAD 正文为空。
- RED：原实现的 recorder GET/HEAD 都没有显式 Content-Length；测试失败。
- GREEN：长度与正文断言通过；实际二进制也验证一致。
- 增加 TestJSONEncodingFailureBeforeHeaders，防止改为预编码时错误被吞掉而留下空 200：原实现 RED（200 而非 500），修复后 GREEN（安全 JSON 500）。注册接口使用固定可序列化结构；该测试只在处理器内部注入不可序列化值。

## 实际验证

所有 Go 命令在 server/，使用 GOTOOLCHAIN=local GOFLAGS=-mod=readonly；本机仍为非稳定 go1.27.0-X:nodwarf5 Linux amd64。

- 四个回归测试各自 RED→GREEN，未以网络或编译环境失败替代行为失败。
- 每项修正后 go test ./... 全套通过。
- 最终 go test -count=1 ./...：4 包通过。
- go test -json -count=1 ./...：32 项顶层测试、4 包、0 失败。
- go test -race -count=1 ./...：4 包通过。
- go test ./internal/app -count=20：通过。
- go vet ./...、go mod verify、gofmt 检查：通过，无格式输出。
- 专属临时目录 go build：通过，产物已由本步骤 trap 回收。
- scripts/smoke_test.py：SIGINT/SIGTERM 两次通过，健康、退出码 0、监听释放。
- 实际二进制补充探测：

```text
PASS CUSTOM /missing: status=404, length=59, body_bytes=59
PASS CUSTOM /healthz: status=405, length=71, body_bytes=71
PASS GET /healthz: status=200, length=41, body_bytes=41
PASS HEAD /healthz: status=200, length=41, body_bytes=0
```

## 限定范围独立核对

先尝试复用原子 Agent bd16bb0d-b90d-4098-a2d7-127aee2e0a62，Paseo send 返回 status=error、Pi RPC process is closed；inspect 确认原会话 error，未实际执行本轮核对。

因此创建只读替代子 Agent 27abc3d6-d302-4b49-85f4-88d89718aecc，名称“核对 Go 服务边界修正”；原 workspace wks_0a6880df1b60b7e7 与主目录不变，没有新 worktree。再次读取实际 profiles，使用「方案设计，架构思考」的 pi / openai-codex/gpt-6.1-sol / high，不臆造 mode/features。

仅核对 app.go/app_test.go、router.go/router_test.go 的 M1–M3 修正及相关回归，不重复全部架构研究。已通过 inspect 验证替代 Agent 的 ParentAgentId 为原协调 Agent，配置与实际 profile 一致；通过 Paseo wait 等待完成后获取报告，没有轮询运行状态。

独立结论：**M1/M2/M3 均已真正修复，限定核对通过，未发现代码回归；Critical/Important 无。**

审查者实际执行（GOTOOLCHAIN=local GOFLAGS=-mod=readonly）：

- 四个定向回归 -count=1 -v：通过。
- go test -count=1 -v ./...：32 项顶层测试、4 包通过。
- go test -race -count=1 ./...、go vet ./...、go mod verify：通过。
- go test ./internal/app -count=20：通过。
- 临时目录 go build、scripts/smoke_test.py：通过，两种信号正常停止。
- 实际二进制 6 种方法 × 5 种路径共 30 组探测：通过；健康及错误路径 GET/HEAD 表示头一致（排除自动 Date）。
- 受审源码、go.mod/go.sum 前后 SHA-256 一致；无项目修改、未安装/提交/推送、无残留临时目录。

报告另列一个 Minor：server README 当时读到的内容仍为“尚未修复”。协调方在保存结果时已同步 README，并实际重新读取确认：当前已说明三项修正及后续方法路由约束。该项现已处理，无待修复的本批次问题。

独立审查未重新制造 RED（只读），仅确认当前 GREEN；RED→GREEN 来自协调方当时实际测试。未耗尽真实 FD，使用受控临时 Accept 注入；未验证稳定 Go、Windows/macOS 或未来其他方法的路由。

## 仍未完成的产品范围

未接 pi、未做客户端/数据库/认证/远程/更新/Actions；没有调用真实模型或安装全局工具。稳定 Go 及跨平台兼容仍未验证。后续按独立设计批次推进，不将本次修正视为这些能力获批。
