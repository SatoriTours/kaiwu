# Agent 与 Leaf 接入研究摘要

状态：初始化讨论阶段的**只读文档/源码核对**。尚未运行产品接入、真实模型请求或三平台构建。下面的版本是观察样本，不是已经选定的项目依赖或永久兼容承诺。

## pi：当前优先

观察的本机版本：pi 1.0.4。依据其 README、CLI integration、JSON、RPC、RPC commands、RPC extension UI、SDK、extensions、message types、session format、configuration、models、providers、MCP、TUI 文档及 `agent-session.js` 实现。

当前用户要求开悟自有后端全部 Go。Go 直接管理 RPC 的模块边界已确认；下述 SDK/扩展能力仍是第三方技术事实，不代表授权采用 TypeScript 宿主或自有扩展。具体基础设计见 [已确认规格](../superpowers/specs/2026-10-11-go-pi-rpc-design.md)，[实施计划](../superpowers/plans/2026-10-11-go-pi-rpc.md) 已获批，纯 Go 基础包及隔离假进程结果见 [验证记录](../development/go-pi-rpc-verification.md)；研究与假进程自测仍不代表真实 pi 兼容验收完成。

### 可用接口与注意事项

- 长期 `pi --mode rpc` 子进程，通过 stdin/stdout JSONL 双向通信，适合 Go 进程管理。
- 命令响应与异步事件分别处理，通过请求 ID 关联。
- `prompt` 成功仅代表接受、排队或扩展处理，不保证执行成功或任务结束。
- RPC 包含状态/历史、可用模型、模型/思考级别变更、取消、会话切换等接口。
- stdin/stdout 仅按 LF 分帧，可去掉前置 CR；不要使用识别 Unicode 分隔符的拆行方式。JSON 文本内容可能包含这些字符。
- stdout 仅为协议，日志走 stderr；持续消费，不让管道背压卡住进程。
- 正式任务需要记录会话 ID/位置和历史，不照搬 `--no-session` 示例作为持久化策略。
- `agent_end` 后可能有重试、恢复、压缩或排队工作；扩展文档指出 `agent_settled` 是最终通知边界。RPC 实际事件与运行状态组合需测试，不以任一单事件直接宣布产品任务成功。

### Go RPC 基础设计前的补充核对

以下为本机 1.0.4 安装产物的只读核对，不是运行验收或已批准规格：

- `dist/modes/rpc/rpc-mode.js` 将 session 事件经 `toJsonEvent` 输出到 RPC，包括 `agent_settled`；收到该事件仍不代表产品任务验收成功。`prompt` 在 preflight 成功后返回 disposition，可能为接受、排队或立即处理。
- 同一实现监听 stdin 的 `end` 并执行 shutdown，调用 runtime dispose 后退出。因此 stdin 是进程生命周期资源，不能由客户端断线顺带关闭；实际退出与后代进程回收仍需运行测试。
- `dist/modes/rpc/jsonl.js` 仅按 LF 切记录，剥除行尾 CR；流结束时还会交付没有最终 LF 的剩余文本。Go 端需要区分完整末记录与截断 JSON，不能把 Unicode 分隔符当换行，也不能未经说明将上游 EOF 行为换成另一套规则。
- `dist/modes/rpc/rpc-types.d.ts` 区分普通 command/response 与 extension UI request/response；后者按服务端提供的 ID 回应，不是普通命令生成新 ID 再等待 response 的流程。
- 自带 `rpc-client.js` 启动后等待 100ms 并检查进程是否退出，不等于协议握手；它还累积完整 stderr，并忽略非 JSON 行。Go 接入不应照搬这些行为：协议就绪、资源上限、污染错误和诊断内容边界需要单独设计。
- CLI 的 `--no-extensions` 关闭自动发现和内置扩展，但显式 `-e` 仍可加载；`--no-mcp` 关闭内置 MCP，`--no-context-files` 关闭上下文文件发现，`--no-approve` 忽略项目本地文件。`--offline` 仅关闭启动网络操作，不是禁止模型请求或进程沙箱。后续真实启动配置不能靠单一参数声称完全隔离。

本轮只读证据：`package.json` SHA-256 为 `248decde30021109984def7ddc8f7bf4caf21ef04910a1303ab19604206c1bd5`；`rpc-mode.js` 为 `631697cd35928fc827f4a423538c43ff227b8cbf63c2a2f11060616f55eba6db`。测试应使用合成数据和 Go 假子进程，不依赖这台机器的 pi 安装或个人环境。

### 模型、扩展与权限

- `setModel()` 检查凭据、更新 Agent 模型状态并追加会话记录，只有显式 persist 选项才更新全局默认。
- 这不证明任意 provider 切换或并发请求边界都安全；要测试当前请求不中断、下一轮调用应用新模型，以及实际模型/待生效状态记录。
- `pi.registerTool()` 可让 TypeScript 扩展注册编排工具，但该自有扩展路线已因纯 Go 后端要求被排除；不能继续作为开悟实现计划。
- `tool_call` handler 可以阻止执行，RPC 可转发支持的 confirm/select/input/editor 等 UI 请求并按 ID 响应。
- 普通默认工具不因此自动获得产品审批策略。权限扩展、工具限制、扩展信任和绕过路径需要设计。
- 自定义终端组件不能直接通过 RPC 在 Leaf/Android 原样呈现；需要映射为产品 UI。
- SDK 是进程内 TypeScript API。若用 SDK，需要额外 Node/Bun 宿主；不是可直接链接的 Go SDK。

### 纯 Go 编排桥的后续候选（不在本批次实施）

本机 `docs/mcp.md` 说明 pi 可通过 stdio 或 streamable HTTP 接入 MCP 服务，读取隔离 Agent 目录中的 `mcp.json` 并向模型暴露工具。这提供 Go 实现派发/查询/结果回收工具服务的候选路径，不需要新增自有 TypeScript 扩展或 SDK 宿主。

仅为文档可行性依据，尚未实现或运行。具体 MCP 版本、Go 依赖、认证、任务身份、请求幂等、权限和生命周期需独立设计。Go MCP 服务可以约束自身提供的操作，但不能自动拦截 pi 原生 bash/read/write 等工具；真实执行前还必须设计工具路由/限制与可信执行边界。现阶段不启用个人 MCP 配置、不运行 `pi mcp list`，也不自动安装 MCP 工具。

### 实施前核验清单

1. 固定兼容版本，核对该版本的命令/事件 schema。
2. 隔离全局/项目配置、扩展、凭据引用与会话路径。
3. 测试半行、跨读取、多个消息、Unicode、大消息、错误记录及 stdout 污染。
4. 测试命令关联、接受后失败、排队、重试和准确结束边界。
5. 测试人工确认、模型变更、取消与进程退出/恢复失败。
6. 明确哪些测试不会调用模型，真实调用另行获准。

文档入口：https://github.com/earendil-works/pi

## Leaf：双端之一的前置能力

观察提交：`07f2a0dc7d5b3925652d18428f45e04e3a2fcdfb`。核对时本机与官方默认分支最新提交相同；后续开发需复核当前状态，不保证此结论一直有效。

- Nim 业务通过 Rust 动态库桥接 GPUI/GPUI Kit。
- Nim 拥有应用状态与响应缓冲区，GPUI 在 UI 线程调用 Nim 回调；后台网络线程不得直接操作 UI 闭包。
- `Runtime.refresh` 存在，但不等于有完整跨线程 UI 唤醒接口。
- Rust desktop 中的 200ms timer 仅用于 `LEAF_WATCH_STATUS` 开发诊断。Nim status 分支读取 watch 错误，Rust 仅错误变化时 notify，不能借它冒充产品网络事件刷新。
- 在当时查看的公开接口中，未找到完整的后台消息入队/UI 调度/窗口重绘组合。需要补证或设计，不等于不可实现。
- 候选方向：后台网络收消息并排队，UI 线程消费、更新 Nim 状态、请求重绘。若要改 Leaf，必须另行授权。
- 文档 SDK 范围为 Linux x86_64、Windows x86_64、macOS Intel/Apple Silicon；最低系统与本项目架构范围还需确认。
- Linux 有较多原生窗口验证材料，Windows/macOS 原生验收证据仍需补齐；不宣称三端已经验证。

来源：

- https://github.com/SatoriTours/Leaf/tree/07f2a0dc7d5b3925652d18428f45e04e3a2fcdfb
- 上述提交的 `src/leaf/gpui_bridge.nim`、`src/leaf/gpui_api.nim`、`src/leaf/desktop.nim`、`crates/leaf-gpui/src/desktop.rs`。
- Leaf 的 installation、packaging、verification、architecture 和 nim-api 文档。

## Codex：后续接入参考，不是当前实施任务

观察版本：codex-cli 0.162.0；官方协议以 `rust-v0.162.0` tag 核对。

- `codex app-server` 提供长期进程，默认 `stdio://`；CLI 命令带 experimental 标识。
- 协议含 initialize/initialized、thread/start、thread/resume、turn/start、model/list。
- thread/start 可设置模型/provider/cwd/审批/sandbox；dynamicTools 明确为实验字段。
- 服务端主动工具调用 `item/tool/call` 有 thread/turn/call ID、工具和参数；返回 contentItems 与 success。这是后续主协调工具桥的候选路径。
- 命令/文件修改/权限审批为服务端主动请求，宿主按请求 ID 回复。
- turn/start.model 覆盖当前及后续 turns；不能据此保证运行中 turn 内下一次底层模型请求能应用变更。不要擅自放宽产品已确认的模型切换规则。
- thread/resume 存在不等于所有未完成工具或审批可以无损恢复；失败必须可见，不悄悄新建会话冒充延续。

来源：

- https://github.com/openai/codex/releases/tag/rust-v0.162.0
- https://github.com/openai/codex/tree/rust-v0.162.0/codex-rs/app-server-protocol/src/protocol

## 证据边界

这些事实用于降低设计盲点，不能替代运行测试、认证方案、工具安全边界或客户端验收。联网搜索曾混入第三方摘要与未验证的恢复建议，最终以实际本机文档和官方固定版本源码为准。没有更改 Leaf、安装产品依赖或调用模型来验证这些结论。
