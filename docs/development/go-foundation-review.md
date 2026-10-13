# Go 服务基础：初次独立验收结果

历史记录：以下是首次审查的发现和判定。用户随后已批准修正 M1/M2/M3；当前代码及复测结果见 [边界修正记录](go-foundation-polish.md)。不以更新后的代码冒充当时没有问题。

来源：Paseo 子 Agent bd16bb0d-b90d-4098-a2d7-127aee2e0a62，原协调 Agent 4d13b530-a48e-4980-831d-671d0fff4713；同一 local workspace wks_0a6880df1b60b7e7。

验收配置：实际 pi / openai-codex/gpt-6.1-sol / high，对应「方案设计，架构思考」profile。报告通过 CLI logs 获取；本文件由原协调 Agent 保存，审查者未修改项目。

## 结论

**本批次 Go 服务基础通过独立验收，附 3 项非阻塞 Minor。** 没有 Critical 或 Important。不等于完整产品、首版、跨平台或真实 Agent 验收，也不是 Git 提交/合并/推送授权。

确认的优点：模块及固定依赖一致；数值回环限制；已取消 context 不启动；panic/响应提交/Flush 边界；请求日志保密性；在途请求、超时关闭与 Serve 失败可观测且资源回收。

## 待处理 Minor

### M1：标准库临时 Accept 错误未接入 JSON 日志

- 位置：server/internal/app/app.go:38–46，http.Server 未配置 ErrorLog。
- 审查者在仅测试子进程 RLIMIT_NOFILE=32、45 个回环连接时复现默认文本 Accept error；释放连接后 SIGTERM 正常退出 0。
- 当前影响：异常时 stderr 混入非 JSON 行；不改变 HTTP/RPC 安全边界或控制状态。
- 后续建议：将 ErrorLog 接入 slog，添加故障格式测试。
- 协调判定：保持 Minor，暂缓；当前尚无消费这些日志的产品协议，不能因此声称所有 stderr 都是 JSON。server README 已提示限制。

### M2：未注册方法访问不存在路径返回 405 和虚假的 Allow

- 位置：server/internal/httpapi/router.go:35–38。
- 审查者与协调 Agent 均复现 CUSTOM /missing → 405、Allow: GET, HEAD；chi 在未知方法时先进入 MethodNotAllowed。
- 当前影响：边缘错误分类/提示不准确，与未知路径 404 的目标偏差；不会允许访问或执行未注册接口。
- 后续建议：区分未知路径和已存在路由的错误方法，添加组合测试。
- 协调判定：保持 Minor，暂缓；扩展业务 API 前需要补齐方法/路径组合和 route-aware Allow。

### M3：HEAD 未提供与 GET 相同的 Content-Length

- 位置：server/internal/httpapi/router.go:50–58、router_test.go:43–50。
- 协调 Agent 二进制复现 GET /healthz Content-Length=41；HEAD 没有 Content-Length，正文为空；两者 Content-Type/Cache-Control 一致。
- 当前影响：HTTP 合法行为，但比规格“相同响应头”的宽泛承诺弱。
- 后续建议：补完整头部一致性，或单独确认规格只约定产品显式头。此批不擅自修改已确认规格。
- 协调判定：保持 Minor，暂缓；不阻塞健康检查或客户端连接。

初次验收时未实施这些建议。用户随后授权了独立的小范围修正批次；是否已修复及限定核对状态以边界修正记录为准。

## 审查者实际执行

所有 Go 命令使用 GOTOOLCHAIN=local GOFLAGS=-mod=readonly：

- go test -count=1 ./...：退出 0，4 包、28 项顶层测试。
- go test -race -count=1 ./...：退出 0，4 包。
- go vet ./...、go mod verify：退出 0。
- gofmt -l cmd internal：退出 0，输出为空。
- go test ./internal/app -count=20：退出 0。
- 专属临时目录 go build：退出 0。
- scripts/smoke_test.py：退出 0，SIGINT/SIGTERM 健康、正常退出、监听释放。
- 二进制补充探测：非法配置、GET/HEAD/404/405、日志隐私，并复现 M2/M3。
- 低 FD 回环故障探测：复现 M1，正常回收。

环境：go1.27.0-X:nodwarf5 Linux amd64。未执行关闭失败专门故障注入，其错误保留仅源码核对；未安装其他工具链、调用模型、创建 Agent/workspace 或提交。

## Declined to judge 与协调处理

以下不是审查遗漏的隐藏通过项；协调方接受其批次边界并明确保留：

1. 稳定 Go 1.25/Windows/macOS：未验证，不能作兼容承诺。
2. pi/任务闭环/模型切换/工具权限/进程树取消：未实现，下一批按独立规格推进。
3. PC/Android/Leaf UI/SQLite/更新：当前仅说明入口，首版目标保留。
4. 认证/TLS/远程/存储恢复/WebSocket：未实现，维持回环限制，不启用远程。
5. 任意不响应 context 的 handler、Hijack/WebSocket 资源：不能从 http.Server.Close 推断全面回收；当前没有对应端点或能力承诺。
6. Actions/签名/Release/许可证：未实施，仍需专门方案和授权。

## 协调最终复核

审查结束后，无产品代码修改；重新实际执行普通测试 -count=1、race -count=1、vet、格式、mod verify、临时构建及 SIGINT/SIGTERM 烟测，均退出 0。自行二进制复现 M2/M3；M1 按源码与审查者故障证据确认，未再次耗费 FD 做同样实验。

临时构建目录已回收；保留本地未提交成果和所有审查记录，不删除主目录、原会话或任何 workspace。
