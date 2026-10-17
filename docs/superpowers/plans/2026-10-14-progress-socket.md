# 结构化进展与双向 WebSocket Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans; current Agent serial implementation, one fresh read-only final reviewer.

**Goal:** 将已批准设计稿接入真实任务数据，业务使用双向 WebSocket 和可恢复增量事件。
**Architecture:** Go 保存轮次和验证后的报告，Store 生成有界增量历史，WebSocket 复用现有业务验证／回执。Vue 独立 transport 和进展卡组件接入现有页面。
**Tech Stack:** 现有 Go／SQLite／coder websocket、Vue／TS／Vite，不新增依赖。
**Spec:** ../specs/2026-10-14-progress-socket-design.md

## Global Constraints

原数据库／令牌／7331、独立会话／worktree／Git 审核保留。原文与 AI 报告分别保存，未核实成果不能显示已验证；不调用真实模型或擅自提交／推送。继续按已明确要求在当前项目目录串行实施，保留用户尚未提交的设计稿。

## Review Focus

1. 报告输出格式错误、包含截断 JSON／超长文本：原文仍可读取，不假装成果完整。
2. AI 自述成功但工具失败／工具引用不在本轮：结果不得显示验证通过。
3. 重连、切范围及退出后旧连接晚帧：不能串数据／清新草稿／重发副作用。
4. 慢连接／消息洪泛／事件窗口缺口：内存和写入有上限，恢复保持权威一致。
5. HTTP 断开及 WS 断开：已登记操作继续，隐藏页面的只读订阅停止。

### Task 1：轮次与报告
- [x] 新增 workbench/report.go 和 report_test.go：ParseReport 严格 JSON／字段上限；Round 独立保存，confirm_result 只确认最新已结束轮次。记录真实工具与 report 引用，旧 JSON 兼容。
- [x] RED 新测试，再实现；`go -C server test ./internal/workbench ./internal/agent` GREEN。追加当前 pi 请求约定，不新增模型调用，完成事件解析与多段回复分类。

### Task 2：增量状态与双向通道
- [x] 新增 workbench/sync.go、sync_test.go：SyncSince(revision) 返回增量或权威快照；字符串追加增量，256 项／2 MiB 环形历史；错误游标与重启回退快照。
- [x] 新增 webapi/socket.go、socket_test.go：鉴权连接、有限方法表、64 KiB 消息、有界请求队列、独立写入和 owner 生命周期、项目可见订阅 Git 检查。先 RED 后 GREEN。
- [x] `go -C server test ./internal/webapi ./internal/workbench` 和 race 通过；保留旧 HTTP 测试。

### Task 3：正式客户端
- [x] socket.ts／sync.ts 独立处理 request/response、事件、超时和断线；api.ts 选择登录后的 WS。App.vue 接入增量，不再在 token 更新时 HTTP 拉取；可见订阅替代浏览器 Git 轮询。
- [x] ProgressCard.vue／AssistantReply.vue／progress.css 实现已批准视觉；ChatPanel 保留短回复、长回复和过程默认折叠、Markdown／预览，确认结果单独执行命令。
- [x] 前端纯状态与真实 Go／假 pi 浏览器先 RED 后 GREEN，更新原 HTTP 专属拦截测试至对应 WS 行为。

### Task 4：验收与运行
- [x] 全 Go／race／vet、前端测试／构建、完整浏览器及窄屏；新上下文独立只读审查后修复阻塞项。
- [x] 更新 README／路线图／AGENTS／验证记录，构建新二进制；未触发真实模型。
- [x] 原 7331 优雅更新：用户明确批准中断后手动恢复；SQLite 已备份，旧令牌／登录／项目／消息保留，实际服务只读浏览器烟测通过。
