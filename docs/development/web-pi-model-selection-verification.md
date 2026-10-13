# pi 自动检测与 Web 模型选择验证

日期：2026-10-13。用户反馈执行卡片提示“服务尚未配置 pi 路径和模型”，要求自动发现已安装的 pi，并查询其支持的模型，在 Web 自行选择。

## 实际实现

- 未设置 `KAIWU_PI_PROGRAM` 时，从服务 `PATH` 的绝对目录查找可执行 pi；显式绝对路径优先，跳过相对目录和不可执行文件。不运行 shell、不扫描其他用户或任意 nvm 版本、不安装工具。
- 有 pi 但未指定模型时服务仍可启动。`GET /api/models` 沿用认证、Host、Origin 和禁止缓存规则，启动隔离短进程，仅执行 `get_state` 与 `get_available_models`，无 `prompt`。返回 provider、id、name、reasoning、contextWindow 白名单，缓存 30 秒，重新检测明确使缓存失效。
- 模型来自真实 pi 可用列表，不硬编码模型名单，不将“有凭据”解释为实际请求成功。复制服务器 pi Agent 目录中的 auth.json／models.json 到私有检测目录，不复制 settings、历史、扩展、MCP、skills 或上下文。禁止配置中的 `!command`，不执行命令凭据。正常退出后清理临时凭据；未确认回收时保留目录，避免删掉仍在运行的进程资源。
- 服务器 pi Agent 目录采用 `PI_CODING_AGENT_DIR`，未设置时使用服务用户的 `~/.pi/agent`；需为绝对路径。模型检测不修改个人源文件，不传原始凭据、API 地址或完整模型对象给浏览器。
- Web 顶栏“默认模型”设置未来项目／任务的默认快照，SQLite 保存并在重启后保留；设置优先于之后启动时的环境默认。已有会话不随默认设置变化。
- 聊天输入框下方“pi · 选择模型”可为已有空项目／任务会话补选，无需重建项目。卡片缺模型时点击“开始开发”先打开同一窗口，保存后再明确开始；草稿保留，不自动调用模型。
- 首次启动前可以调整会话模型；有消息、会话文件、执行记录或正在运行时拒绝修改。运行中／已使用会话的下一轮模型切换未在本批实现。
- 保存前窗口明确说明沿用服务器 pi 登录。启动该会话时只复制所选提供商的凭据和模型定义到私有 Agent 目录，权限 0600，保留该目录后续的 OAuth 更新；不回写个人 pi，也不写入产品数据库。原环境配置模式仍按已有隔离方式工作。
- 非推理模型拒绝非关闭的 thinking；推理模型可选级别，但最终级别兼容性仍由 pi 和具体模型决定，不声称全部级别已验证。

## 实际执行结果

- `GOTOOLCHAIN=local go -C server test ./...` 通过；后续包含所有新增测试的 `test -race ./...` 通过。
- `GOTOOLCHAIN=local go -C server vet ./...` 通过；Go 格式化完成。
- 新增测试覆盖 PATH 自动检测／手工覆盖／相对目录与执行权限、凭据提供商隔离／命令拒绝／个人文件不变／已刷新凭据保留、RPC 元数据白名单／退出清理／缓存副本、模型 API 认证／伪造模型及 thinking 拒绝、空会话补选／快照隔离／重启持久化。
- `node apps/web/tests/workbench.test.ts`：6/6 通过。
- `npm --prefix apps/web run build`：类型检查及 Vite 构建通过。
- `npm --prefix apps/web run test:browser`：最终 20/20 通过，包括原看板／聊天／Markdown／worktree／Git 集成回归、缺模型打开选择／草稿保留、提供商筛选／搜索／默认与当前会话独立保存／启动后冻结、错误／空列表／重新检测及 360px 窗口。使用真实 Go 服务与隔离 Go 假 pi，无模型请求；测试 pi 目录显式隔离，不读取个人凭据。首轮旧行为断言、测试登录限流及下拉框标签识别已修正后复测。
- 本机真实 pi 元数据测试：`KAIWU_TEST_REAL_PI=... KAIWU_TEST_PI_DIR=... go -C server test -run '^TestInstalledPiCatalog$' -v ./internal/pimodel` 通过。当前可用 43 个模型：deepseek 2、minimax-cn 3、openai-codex 9、opencode-go 29。此处 openai-codex 是 pi 内置 provider，不是新增 Codex CLI 适配。
- 更新 `192.168.2.122:7331` 到 `server/bin/kaiwu-server-pi-models`。更新前验证准确进程、无运行对话／合并及子进程，优雅停止后备份 SQLite 至私有 `.kaiwu/backups/`。沿用原环境、端口、访问令牌和目录；旧 Cookie 重启后有效，项目／任务／消息保持，真实模型 API 返回同样 43 项，检测子进程已退出。
- 已部署页面额外用 Chromium 验证桌面和 360px 手机模型窗口、真实 43 项与 DeepSeek 筛选；取消窗口，没有替用户保存默认或会话模型，没有发送聊天。私有截图位于 `.kaiwu/pi-model-picker-desktop.png` 和 `.kaiwu/pi-model-picker-mobile.png`，不入库。

## 未验证与边界

未发送真实模型请求、未验证额度／网络／OAuth 刷新或真实开发任务。本批使用个人凭据的隔离副本，OAuth 过期及多个会话并发刷新同一账号的行为仍须真实联调；源配置改变不自动覆盖已创建会话的凭据快照。扩展注册的 provider、命令凭据及未传递的环境变量不能保证出现在列表。

本批串行实现、自验，未进行独立 Agent 验收（当前 Paseo CLI 未能连接管理 daemon），未创建额外 worktree（仓库尚无提交基线）。未对开悟仓库提交／合并／推送，未修改 Leaf、安装全局工具或启动新端口的常驻服务。
