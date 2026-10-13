# SDD ledger — plan: docs/superpowers/plans/2026-10-11-go-foundation.md

## 执行约定

用户已确认规格、实施计划及推荐的当前 Agent 串行实现＋独立验收方式。

- BASE：unborn main（没有基线提交）；工作目录 /home/jimxl/projects/kaiwu。
- Pre-flight：Task 1 输出 config.Config/Load/Validate，Task 3 消费相同接口；一致。
- Pre-flight：Task 2 输出 httpapi.NewHandler，Task 3 消费相同接口；一致。
- Ruling: 不运行依赖提交范围的 task-start/task-done/review-package 脚本，使用本文件记录逐任务证据 — 空仓库且用户未授权提交，不能为了脚本制造提交 — 代价：审查须核对实际文件清单而非提交 diff。
- Ruling: 按已批准计划在主 checkout 单写入执行，不创建 worktree — 空仓库没有隔离基线，已核对 Paseo 当前目录无其他编码 Agent — 代价：首批改动依靠文件范围和人工审核隔离；后续有基线后使用托管 worktree。
- 仅保留本仓库文档、模块和测试；不调用真实模型、不暴露远程服务、不提交或推送。

## 任务状态

- Task 1: complete（模块入口与配置）。RED：`go test ./internal/config` 因缺少 Load/Config 退出 1；GREEN：同命令及 `go test ./...` 退出 0（GOTOOLCHAIN=local）。模块说明入口、精确忽略规则已创建；未提交。
- Task 2: complete（HTTP）。路由 RED：缺少 NewHandler；路由 GREEN：健康/HEAD/404/405 测试通过。middleware RED：缺少 observeAndRecover；GREEN：全部 HTTP 测试通过。`go mod tidy`、`go mod verify`、`go test ./...` 退出 0（GOTOOLCHAIN=local）。
- Task 3: 编码及本机验证 complete；独立验收 complete（无 Critical/Important；3 项 Minor 暂缓）。app RED 缺少 Run/newServer/serve；GREEN `go test ./internal/app -count=10` 通过。命令 RED 缺少 run；GREEN 命令包及全套通过。最终 fresh `go test -json -count=1 ./...`：28 项顶层测试、4 包、0 失败；race/vet/格式/mod verify/build/SIGINT+SIGTERM 烟测均退出 0。临时构建目录已回收，无残留本步骤服务。

## 独立验收对应关系

- 原协调 Agent：4d13b530-a48e-4980-831d-671d0fff4713。
- 原 local workspace：wks_0a6880df1b60b7e7；路径 /home/jimxl/projects/kaiwu；基线 unborn main。
- 只读验收子 Agent：bd16bb0d-b90d-4098-a2d7-127aee2e0a62，名称“验收开悟 Go 服务基础”，同一 workspace。
- 已通过 inspect 验证 ParentAgentId 是原协调 Agent，不是独立或其他项目会话。
- 实际读取本机 Paseo 保存的 daemon.agentProfiles（只提取 profile 字段，不输出凭据）；所选“方案设计，架构思考”：provider=pi，model=openai-codex/gpt-6.1-sol，thinkingOptionId=high，modeId/features 未设置，不臆造。
- 使用 CLI agent run --background 派发，报告要求见 go-foundation-review-request.md；不轮询运行状态，等待完成通知。
- 验收已返回并保存到 go-foundation-review.md；审查者 idle。独立普通/race/vet/构建/烟测及生命周期重复 20 次通过。
- 协调方已在审查后重新执行普通/race 测试 -count=1、vet、格式、依赖、构建及信号烟测；全部退出 0。未修改产品代码，无需重复派发审查。
- 未创建执行 worktree，不归档原 workspace；保留全部未提交成果。

## 实施裁定

- Task 2: Ruling: 在 Unwrap 之外实现 FlushError，并增加 TestPanicAfterFlush — ResponseController 直接绕过包装器会让已提交状态丢失，panic 可能错误追加 500 — 代价：多维护一个标准库 flush 转发方法；未增加 WebSocket 功能。

## 最终审查处理

- Final: minor (deferred): M1 临时 Accept 错误使用标准库文本 ErrorLog；故障时 stderr 不是全 JSON，后续需接入 slog。
- Final: minor (deferred): M2 CUSTOM /missing 返回 405 并错误声明 Allow；后续业务 API 前补齐方法/路径组合。
- Final: minor (deferred): M3 HEAD 缺 GET 的 Content-Length；HTTP 合法，但完整响应头一致性承诺未全部兑现。
- Final: Ruling: 保留稳定 Go/Windows/macOS 未验证状态 — 本轮仅实验工具链 Linux 样本 — 误判代价：其他平台或稳定构建可能失败，不能发布兼容承诺。
- Final: Ruling: pi/任务闭环/模型切换/工具权限/进程树取消不计入本轮通过 — 没有对应实现 — 误判代价：把骨架误当能安全执行开发任务。
- Final: Ruling: PC/Android/Leaf UI/SQLite/更新仍为未实现首版目标 — 只有说明入口 — 误判代价：把空入口当可运行客户端。
- Final: Ruling: 认证/TLS/远程/持久化恢复/WebSocket 另行设计，维持回环限制 — 本轮无这些机制 — 误判代价：未授权暴露或恢复有副作用工作。
- Final: Ruling: 不承诺终止任意 handler 或回收 Hijack/WebSocket 资源 — Close 只覆盖其拥有的 HTTP 连接，当前无对应端点 — 误判代价：新增端点可能泄漏独立资源，届时必须补专属生命周期。
- Final: Ruling: Actions/签名/Release/许可证仍未实施 — 没有发布方案与相应授权 — 误判代价：错误发布或许可声明。
- Final: Ruling: 不套用合并/PR菜单、不删除工作区/执行记录，保留本地未提交成果 — unborn main 没有功能分支且项目不默认走 PR，用户未授权提交 — 代价：后续创建隔离基线需用户单独批准首个提交。
- Task 3: complete（包含独立审查、最终复测、文档与本地交付；无 Git 操作）。

## 用户授权的后续 bounded 修正批次

用户在三项短设计后回复“继续”，授权小范围修正，不另写规格/长计划；初次 Minor 暂缓行是历史记录，不是当前未解决状态。

- M1 fixed：TestTemporaryAcceptErrorUsesJSONLog，RED（标准库文本错误未进入 JSON sink）→ ErrorLog 接入 slog → GREEN。
- M2 fixed：TestUnknownMethodRouting，RED（CUSTOM 的未知路径均 405）→ 按同一 RawPath/Path 匹配已有 GET 路由 → GREEN。
- M3 fixed：TestHealthHeadContentLength，RED（HEAD 无显式长度）→ 预编码/设置长度 → GREEN；TestJSONEncodingFailureBeforeHeaders 同样实际 RED→GREEN，避免空 200。
- 每项后全套 GREEN；最终 32 项顶层测试、4 包、0 失败，race/vet/依赖/格式/生命周期 20 次、构建/信号与真实路由探测通过。
- Ruling: 使用一次临时 net.Error 加真实回环 listener 验证 Accept 日志 — 无需耗尽 FD 或修改进程资源限制即可触发标准库同一分支 — 代价：不替代真实 FD 耗尽的系统级测试。
- Ruling: 原审查 Agent send 返回 Pi RPC process is closed，inspect 为 error；在原 local workspace 创建只读替代子 Agent 27abc3d6-d302-4b49-85f4-88d89718aecc，不重启旧进程或复制执行空间 — 原会话无法复用 — 代价：额外一次审查上下文；未声称修复 Paseo 本身。
- 替代 Agent ParentAgentId 已验证；实际配置仍为最新读取的“方案设计，架构思考” pi/openai-codex/gpt-6.1-sol/high。
- 限定独立验收通过 M1/M2/M3；32 项/4 包、race/vet/mod verify、生命周期 20 次、构建/信号及 30 组方法/路径探测通过；只读无文件变化。
- 审查者提及读取时 server README 尚未同步，协调方已在保存结果时更新并重新读取确认；无剩余本批次问题。
- bounded 批次 complete；完整证据见 go-foundation-polish.md。仍未提交/推送，所有原空间及记录保留。
