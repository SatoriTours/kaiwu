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

## 2026-10-20：MiniMax M3.1 Flash 配置更新

用户要求开悟 Web 使用 pi 中的新模型。本次为现有能力的配置更新，无产品代码修改。

- 实际运行入口已核实为 `http://192.168.2.91:7331`，运行修订号 `e43b8bf18b1339abe901819e70c825896148b38c`；此前 `.122` 地址是历史入口，不作为本次验收入口。
- 目标服务器 pi 1.3.0 的可用列表原有 MiniMax M2.7／M2.7-highspeed／M3，无 M3.1 Flash；服务器原无 `~/.pi/agent/models.json`。新增该文件，仅补充官方 `minimax-cn/MiniMax-M3.1-Flash` 定义，沿用原 MiniMax 登录，不写入或转移凭据。定义依据协调机器 pi 已更新的模型目录，使用 Anthropic 兼容端点、100 万上下文、文本／图片输入、`forceAdaptiveThinking` 及 low～max 映射。目标 pi 实际实现已核实支持相应字段。
- 经实际入口认证的 `GET /api/models?refresh=1` 返回新模型；通过现有 `set_default_profile` 命令保存默认模型和 `medium` 思考级别，`credentialSource=server_pi`，revision 1→2。重新读取快照及以只读 SQLite 查询 `state` 均确认已持久化。修改前后任务、会话内容相同；当时目标服务尚无会话。
- 目标服务器另启动临时隔离真实 pi RPC，只使用 MiniMax 凭据副本和新增模型定义，禁用扩展／MCP／工具／上下文并设置 `--offline`；仅执行 `get_state`，确认实际模型 ID、`medium`、100 万上下文和 adaptive thinking。子进程退出后删除其临时目录。
- 上述远程 Node 检查命令退出码均为 0。服务 PID 前后均为 54851，无需重建或重启；不打包当前目录中未完成的子 Agent 后端及其他混合改动。
- 远端私有证据：`~/.local/share/kaiwu/model-updates/2026-10-20T16-29-29.381Z/before.json` 与 `result.json`，保存旧默认和检查结果，不含凭据。

结论：配置及无模型请求的接入检查通过。未发送 `prompt`、未验证真实额度／推理／工具调用；未做浏览器视觉检查或独立 Agent 验收，不将元数据选择通过等同真实任务验收。未改协调会话／六角色配置、未安装升级工具、未执行 Git 提交／推送。新默认仅用于之后创建的会话，已有任务快照不追溯。

## 2026-10-21：失败排查与最小真实请求

用户反馈任务失败、质疑套餐／模型／调用地址，后明确要求继续验证；预先说明真实调用会消耗额度。本次不改服务器配置、不恢复／重试开发任务、不重启或部署。

- 原任务 pi JSONL 保存了4次 HTTP 429，`errorMessage` 为 Token Plan 速率限制（2062）；产品数据库仅存通用文案，错误透传修复已另行通过独立切片验收、未部署。
- 只读核对任务与服务器 pi 使用相同 Token Plan Key；官方余额接口返回成功，检查时当前窗口剩余93%，不支持“套餐已耗尽”的判断。
- 任务模型为 `MiniMax-M3.1-Flash`、`anthropic-messages`、国内站 Anthropic 兼容地址；隔离假Key／全局禁网及拦截fetch证实当前定义默认会请求 `max_tokens=524288`。该值过大是排查线索，并非已确认根因。
- **2026-10-21T04:56:25Z**：在目标服务器通过已安装 pi 1.3.0 的 `pi-ai` provider 库执行单次隔离真实请求；使用原任务同一Key和模型，`medium` 思考级别，单次显式覆盖 `maxTokens=8192`，`maxRetries=0`、35秒超时、无工具／个人上下文，只要求回复 `OK`。受控fetch只允许原MiniMax消息端点且最多1个请求。
- 实测1221ms正常结束，`stopReason=stop`，回复 `OK`；输入212、输出13（其中reasoning9）、缓存0，共225 token。本地模型价表估算0.0000394416美元，不代表套餐实际账单。远程命令退出0，实发1次，无配置写入。

结论：**目前该模型／接口／Key在8192预算的最小真实请求下可用**。未使用524288预算做同期对照、未走Web原任务／RPC开发闭环，因此不能证明原429一定由输出预算造成，也不能宣布原开发任务已恢复。参数预算与限流因果仍待进一步有界验证；本阶段没有将该预算写回服务器或已有任务。

## 2026-10-21：批准降低预算并接回原任务

用户对“降低默认输出预算后验证原任务”回复“好的，继续”，本次先说明会产生真实模型调用与原任务内修改，不提交／推送、不部署工作台。

- 原任务仍为失败且无待生效配置、原worktree无未提交文件；核对相同provider／模型／medium／session文件／执行ID后，将服务器 pi 模型定义及该原任务的隔离副本中 `MiniMax-M3.1-Flash.maxTokens` 从524288改为8192，其他模型／上下文上限／凭据不变。备份及脱敏前置快照保存到服务器私有 `model-updates/2026-10-21T11-29-35-392Z-budget/`。
- 经实际7331入口登录，刷新可用模型，通过现有 `set_next_profile` 选择**同一**profile，确保下一轮回收旧空闲pi并重新读取模型定义；再单次发送继续消息。未写SQLite、未注入RPC stdin、未重启工作台；临时检查登录已单独注销，不影响用户原登录。
- 继续消息明确限定原worktree、不提交合并推送、不安装工具链／不下载环境、不启动常驻服务或局域网端口；若缺Go只生成代码并报告阻塞。原pi JSONL和执行工作区身份保持不变。
- 2026-10-21T11:29:35Z～11:30:39Z，本轮8条真实assistant模型响应、13次工具调用，**没有error／429**。生成 `go.mod`、`main.go`、`main_test.go`、`README.md`、`kaiwu-preview.json`；服务端最终 `runState=idle`、任务 `waiting_user`、执行／轮次 `awaiting_user`，未标记完成。
- 真实阻塞：服务器没有Go编译器；已有Git包装脚本使用隔离后的HOME展开工具路径，实际CLI内调用指向不存在的位置（服务器自身HOME下的Git仍可只读检查，核对5个新文件均未跟踪）。没有为验证擅自安装软件或修改Git脚本。原任务编译／单测／HTTP入口均未验证，不能把括号配平扫描当构建成功。
- 本轮计量：非缓存输入7465、缓存读取38017、输出4473，共49955 token；本地模型价表估算0.0043547891美元，非实际套餐账单。命令退出0，工作台PID仍54851／active。脱敏操作回执、配置备份与观察结果留服务器私有目录，未将真实会话写入仓库。
- 源码SHA256前缀：`go.mod=e9651c5b`、`main.go=202c6c87`、`main_test.go=17d9c5f8`、`README.md=4e00b2c3`、`kaiwu-preview.json=0ed5b0cf`；这些是原产品任务工作区成果，不是开悟源码修复文件。

结论：**8192预算已持久生效，原Web任务通过同一pi会话完成真实模型与工具执行，本轮未再出现429**。这不是524288同期对照，仍不能排除原时段瞬时限流；代码成果因开发环境缺失停在待处理，未验收完成、未安装／提交／推送。该阶段若要继续运行验证，需另行授权安装用户级Go及修复Git包装脚本；后续授权与执行见下。工作台失败提示／布局的源码修复仍未部署。

## 2026-10-21：授权修复工具环境并验证原Go示例

用户明确同意安装用户级Go、修复Git脚本并继续跑通示例。修复前在pi隔离HOME中 `git --version`、`go version` 实测均退出127。

- 从官方 `https://go.dev/dl/?mode=json` 选择stable `go1.27.3`，下载Linux amd64归档70,588,678字节，逐字节SHA256核验 `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40` 一致，再解包到用户目录 `~/.local/share/kaiwu-tools/go/go1.27.3`；用户bin中的go/gofmt软链接指向该版本。未写系统目录／修改shell配置／现有服务PATH。
- Git包装脚本原先用HOME定位 `~/.local/share/kaiwu-tools/git/usr`，改为从自身launcher所在bin目录推导相邻share安装；保留原GIT_EXEC_PATH、模板、LD_LIBRARY_PATH行为，不改认证。脚本sha256 `5bab83b76afb37a113ab09c6ccce1930037501fac8f0b8b0013911bad87e5c64`；`sh -n`及普通／隔离HOME的git/go版本、GOROOT和原任务git status全部退出0。
- 工具备份、官方归档、安装校验与任务续接证据保存在服务器私有 `tool-updates/2026-10-21T11-52-09-937Z/`。不把工具二进制或凭据复制入开悟仓库。
- 经实际工作台 `answer_interaction` 回答原任务的待补充环境问题，receipt从recorded进入accepted，再在原pi会话执行验证。没有另建会话／worktree，没有重复发送旧开发指令或重启工作台。
- 原执行端实跑：`GOTOOLCHAIN=local go test ./...` 四例通过（0.003秒）、`go vet ./...` 退出0、`go build`成功；gofmt检查无输出，源码5文件hash与前阶段一致。
- 编译产物临时监听 `127.0.0.1:33725`：GET `/hello`为200、正文 `world`、Content-Type正确；HEAD `/hello`为200且无正文；GET `/unknown`和`/`为404。监听仅回环，未用7331；进程已停止、端口释放，临时构建产物已清理，未保持预览服务运行。
- 最终原任务 `needs_confirmation`、执行 `awaiting_review`、轮次finished，未代用户确认完成。没有提交、合并或推送，工作台程序未部署／重启。此轮6次真实模型响应无error：非缓存输入11051、缓存读取50021、输出2314，共63386 token；价表估算0.0037791787美元，非套餐账单。

独立验收（e7739791）通过：官方归档SHA／用户级安装与Git脚本改动核对、普通及隔离HOME工具可用；在独立临时HOME／缓存下build、vet、4项测试均退出0。编译产物在临时回环34621实测GET `/hello`为200＋world、HEAD无正文、未知路径404和Content-Type正确；进程、端口与自产临时目录均已回收，5源码hash不变。未增加Git提交，工作台PID54851／7331未变。主控复核脚本与5源码SHA256一致、该端口无监听。

结论：工具环境修复与原Go示例运行验收通过，原任务保留待用户确认，未代验收完成；不将回环测试冒充实际工作台预览入口／线上部署。MiniMax8192预算下本次任务已跑通，但无原大预算同期对照，不宣称429因果已证明；工作台失败提示／布局源码修复仍未提交／部署。
