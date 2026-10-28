# 项目工具与聊天附件实施计划

> **For agentic workers:** 使用 executing-plans，由当前会话连续实现、自测，再独立只读验收。不创建工作区、不提交、不部署。

**Goal:** 将已确认的紧凑 V3 原型接入真实项目文件、服务器终端和会话附件。
**Architecture:** Go 管理目录边界、隔离附件和终端生命周期；复用现有登录、同源验证及 Agent 工作区租约。Vue 顶栏切换主视图，右侧聊天保持原会话。
**Tech Stack:** Go 1.25、Vue 3、TypeScript，现有 WebSocket 和 Linux PTY。
**Spec:** `docs/design/project-tools-attachments.html`（V3）及本会话批准。

## Global Constraints
- 保留当前目录其他未提交成果，不覆盖、不提交、不推送，不更新 7331。
- 文件浏览不是源码编辑器；禁止路径逃逸、Git 内部文件和符号链接，上传不覆盖已有文件。
- 聊天附件单独保存，不自动写入项目；绑定会话，最多 8 个、单个 8 MiB，总计 24 MiB；只接受安全内联图片，其他文件仅下载。
- 终端是服务器账号权限，不是沙箱；显式启动和结束，断开客户端不杀任务；服务关闭可靠回收。Linux 以外明确阻塞，不虚报跨平台支持。
- 人工写入／终端与同工作区 pi／合并互斥；不自动发送旧消息或消耗模型额度。

## Review Focus
- 路径穿越、符号链接、跨项目／会话附件引用。
- 上传超限、中断和重复命名不覆盖未知文件。
- 切项目／卡片时异步响应不能串入新范围。
- 终端断线／重连不重放输入、持有进程归属与锁，服务关闭清理。
- 附件发送失败保留草稿，发送成功仅清理同一版本；图片真实通过 RPC images 发送。

## Tasks
- [x] 1. Go 文件与附件边界：新增 `server/internal/projecttools/{files,attachments}.go` 和测试，先红后绿验证目录列表、读取、上传、新目录、图片 MIME、会话归属和上限。
- [x] 2. Go 人工操作租约与终端：新增 `agent/project_tools.go`、`projecttools/terminal*.go`；PTY 进程组、有限输出、显式结束、跨断连持有。先测真实隔离临时目录命令及锁互斥。
- [x] 3. API 与聊天发送：新增 `webapi/project_tools.go`，在既有认证后注册；Message/Command 增加附件元数据；Agent prompt 加图片和附件路径。测试认证、越权、附件持久化与 RPC 结构。
- [x] 4. Vue 文件／终端视图、紧凑顶栏与附件草稿：新增独立组件及 helpers，接入 App/ChatPanel，测试草稿隔离和真实浏览器关键路径。
- [x] 5. 验证与交付：Go 全套、Web 测试／构建、浏览器桌面手机交互、独立验收；更新 README／roadmap 和开发记录，明确未做真实模型及部署。

## Ledger
- 初始勘查完成；已发现另一组子 Agent 功能未提交改动，保留并只做必要增量。
- 流程约定优先：复用当前主目录，计划内执行／验收不再次审批；无 Git 写入交付授权。

- 自测：Go 全量通过；Web 单元／构建通过；工具浏览器＋既有聊天抽屉共 4 项通过；Go 工具与 API 定向 race 通过。首轮独立关键功能验收通过；Important 整改后正在增量复验，独立视觉因验收模型能力阻塞。
- 终端退出缺陷：复现后台子进程持有 PTY，确认 bash 交互初始化重启 job control；显式 set +m、非阻塞 PTY、独立 wait 与读循环后回归通过。
- Ruling: 首版使用 Linux 命令行 PTY，不提供全屏终端编辑器；其他平台明确拒绝。既有跨平台客户端目标不变。若用户需要全屏 TUI，需后续补终端仿真组件。
- 附件配额：单文件 8 MiB、单消息 8 项／24 MiB、服务存储 1024 项／256 MiB；草稿移除不自动删除已上传数据，旧数据不擅自清理。

- 首轮验收及处理见 `docs/development/web-project-tools-verification.md`；增量manifest `manifest-revised.json`，不清理／覆盖原轮次证据。Go vet 已恢复通过。

- 独立增量复验：38/38哈希一致、Go8项／vet／helper5次／浏览器4项通过，Important均修复或按证据撤回。Task5保留未勾选：独立视觉能力阻塞，不能宣称完整验收。已更新README与roadmap；未部署、提交或调用真实模型。

- 最终补验：已实测同一DeepSeek通道支持图像；修复有效图片暴露的blob/CSP预览缺陷并保留安全策略。独立4项浏览器回归＋4张图片语义补验通过，原视觉阻塞解除。Task5按本批约定范围完成；未测状态与未部署明确记录，不将其当线上交付。共享模型配置未变。
