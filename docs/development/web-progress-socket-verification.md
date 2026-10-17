# 结构化进展与双向 WebSocket 验证（2026-10-14）

用户批准 [交互设计](../design/progress-workbench.html) 后明确要求继续实现，按 [规格](../superpowers/specs/2026-10-14-progress-socket-design.md) 和 [计划](../superpowers/plans/2026-10-14-progress-socket.md) 在本目录串行完成。本批没有 Git 提交、合并、推送或真实模型调用，没有新增依赖或自有后端语言。

## 实际实现

- `workbench/report.go` 保存独立轮次、报告、实际工具依据；严格校验报告字段／大小，原始文本保留。无效最终报告清除旧成功摘要并提示回退；模型／进程失败和服务重启结束活动轮次，保留原报告及工具。
- pi 在原调用的系统提示中收到结构化末尾块约定，不额外请求模型总结。多段助手输出保留，过程和长回复折叠，原文可展开。
- 右侧聊天顶部实现方案／成果／验证／记录、历史轮次、阻塞／问题及结果确认；项目概览按实际任务状态计算。成果标为 AI 报告，验证独立显示本轮工具返回／失败／缺少依据，工具完成不代表语义验证通过。确认最新已结束结果不影响 Git，继续修改产生新轮次。
- `/api/socket` 复用现有认证、同源检查、业务验证及 operationId 回执；模型、任务命令、Git 审核／合并／回执走双向请求／响应。单帧输入 64 KiB、有限方法表、有界队列、5 秒写入超时；操作属于服务，断线不杀任务或自动重发。Web handler 停止等待 RPC 和 Git 观察协程退出。
- Store 提供字段增量，文本追加仅发送新文本；256 项／2 MiB 内存补发窗口，缺口或实例变化使用权威快照。超过 256 KiB 的快照分块、UTF-8 边界安全、收齐后原子替换，不因总历史超过 32 MiB 永久重连。长期全量状态的存储／序列化内存开销仍存在，未增加历史分页或清理策略。
- HTTP 保留登录／退出、首次快照、异常断线的轻量身份检查；被撤销的 Cookie 即使只收到浏览器 1006 也返回登录。正式页面停止 token 更新后的 HTTP 全量拉取及浏览器定时 Git 轮询，服务按可见项目订阅观察 Git；旧 HTTP 和 `/api/events` 兼容。

## 测试证据

环境：Linux，本机既有 Node/npm/Chromium，Go 本机工具链，测试缓存位于 `/tmp`。使用临时 SQLite、Git 和假 pi，不读取个人扩展或发送模型请求。

- `GOTOOLCHAIN=local ... go -C server test -json ./...`：165 项顶层测试通过、6 项预期 Skip、0 失败。
- `go -C server test -race ./...`：全部通过；轮次重启和分块协议最后追加后，`go -C server test -race ./internal/workbench ./internal/webapi` 再次通过。
- `go -C server vet ./...`：通过。
- `npm --prefix apps/web test`：8/8 通过，包含不可变增量、断档／危险路径和分块顺序／完整性。
- `npm --prefix apps/web run build`：类型检查、生产构建及 Go embed 静态资源复制通过。
- `npm --prefix apps/web run test:browser`：22/22 通过（26.4 秒）；含完整看板／登录／模型／Markdown／Git 回归、双向通道、轮次确认及 1440/1024/768/360px 与 390px 新进展布局。实际页面截图：[桌面](../design/web-service/progress-desktop.png) · [手机](../design/web-service/progress-mobile.png)。
- `go -C server build -o bin/kaiwu-server-progress-socket ./cmd/kaiwu-server`：通过。

新增回归覆盖：先合法再损坏的最终报告（RED→GREEN）、失败轮次结束（RED→GREEN）、大于 32 MiB 的 Unicode／转义快照分块及重组（缺实现 RED→GREEN）、真实 WebSocket 分块传输、服务关闭、模型强制重新检测路由（RED→GREEN）、轮次重启、证据引用缺失／工具失败、旧轮次不能确认、最新确认不改 Git、继续修改和历史切换、断线期间撤销登录。

原 HTTP 专属浏览器故障注入已迁移到真实 WebSocket 帧，业务仍到真实 Go 服务；阅读位置和缺模型固定数据对 HTTP bootstrap 及 WS 权威同步保持一致。测试过程曾发现旧 HTTP 注入被权威快照覆盖及未等首次同步导致晚帧测试竞争，已修正测试条件，不以删掉故障断言通过验收。

## 独立审查

单个新上下文只读 reviewer 审查规格、实现与风险重点；Paseo 本地 daemon 不可用，实际使用协作入口。未修改文件、调用真实模型或操作线上服务。

首轮无 Critical，3 Important／1 Minor：损坏最终报告保留旧摘要、离线期间登录撤销后无限重连、超过 32 MiB 快照无法恢复、失败轮次仍为 running。四项均已修复，并以以上回归核对；没有把开发者自验称为独立复审。

## 运行状态与限制

`server/bin/kaiwu-server-progress-socket` 已部署到原 `http://192.168.2.122:7331`。启动前核对原服务为 `kaiwu-server-pi-models`，1 个项目／1 个任务，1 个活动 pi 会话，无活动合并。用户明确选择“现在重启，之后手动恢复”后，优雅 SIGTERM 停止并备份 SQLite，沿用原进程环境、令牌、数据库和固定端口。

实际更新核对：健康检查正常，旧 Cookie 仍有效，项目／任务及消息原文保留；活动会话从 1 变 0，未自动恢复／重发。真实 7331 Chromium 只读烟测通过：原 Cookie 恢复登录、任务结构化页签、WebSocket sync 与 git/refresh、桌面／390px 手机输入框，无横向溢出、无页面错误；没有发送 commands 或模型请求。临时烟测 Cookie 文件已删除，私有数据库备份位于 `.kaiwu/backups/`。

报告质量仍依赖 pi 遵循格式；格式不合规会诚实回退原文。已有运行中的 pi 进程需安全重启／显式恢复后才获得新启动提示。当前没有验证真实模型报告、跨平台原生端或 Actions。团队协作和通用可配置编排引擎保留未来方向，本批不扩大范围。

后续用户实机反馈暴露旧任务的中断状态、无报告回退与手机信息密度问题，修正与最新截图见 [任务状态与详情验证](web-task-state-clarity-verification.md)。本文件记录此前批次，不代替后续验证。
