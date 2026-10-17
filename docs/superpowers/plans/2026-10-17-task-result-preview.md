# 任务效果预览实施计划

> **For agentic workers:** 按本项目 Paseo profiles 分工执行，任务间接口如下；用户已于 2026-10-17 要求开始修改。先写失败测试，再实现、集成与独立验收。未获 Git 提交、合并、推送授权。

**Goal:** 开发结束后可查看真实任务效果／运行结果，验收后确认完成并保留完成反馈。
**Architecture:** Go 管理用户显式启动的任务预览进程与受限 HTTP 网关；Vue 在看板位置打开效果、右侧保留原任务聊天。沿用现有轮次确认、认证、WebSocket 请求和工作区身份。
**Tech Stack:** Go 标准库及已有 SQLite/WebSocket；Vue 3/TypeScript/Vite；不新增依赖。
**Spec:** ../specs/2026-10-16-task-result-preview-design.md

## 全局约束与本批实施决定

- 原会话 e2a5cc41-db4a-41a6-b882-55a1ada60e42 协调；执行 workspace wks_14c980b5a0d2d1ef，路径 /home/jimxl/.paseo/worktrees/3e51f239/task-result-preview，分支 feat/task-result-preview。实际基线含主目录既有未提交源码快照，见 /tmp/kaiwu-preview-baseline。不得提交或覆盖既有改动。
- 后端全部 Go，沿用中文回复，不调用真实模型；协议／浏览器测试使用隔离假 pi 与真实本地 HTTP 示例进程。
- 任务根目录可提供 `kaiwu-preview.json`：`{"version":1,"command":["python3","server.py","--host","127.0.0.1","--port","{port}"],"path":"/hello"}`。argv 执行、不经 shell；严格限制字段／长度；端口由 Go 分配，至少一项含 `{port}`，必须只监听回环。启动界面展示实际命令，点击启动即明确执行该命令。无配置不猜命令、不调用模型，提供成果与补充说明。
- 预览仅允许从核验的任务 worktree 启动；Linux 新进程组，清理整个组。端口就绪必须核对 `/proc` socket 归属预览进程组，之后再检查 HTTP；端口被外部进程占用或不属于预览组不得代理。每次转发重核归属。
- 最多 4 个运行预览，启动超时 10 秒，诊断日志上限 64 KiB。每执行空间有 OS flock，子进程继承锁，异常服务退出后不凭保存 PID 杀旧进程、不重复启动。正常服务退出清理预览；关闭浏览器、切卡片、确认完成不停止。只显式停止或服务退出回收。
- 本批 HTTP 预览为 GET/HEAD，只读，不支持 WebSocket/HMR、跨源应用、任意外部代理或应用登录。HTML/资源使用网关路径，根绝对资源路由可能不兼容；显示范围说明，不声称所有框架均可预览。
- 工作台 Cookie/Authorization 不转发，任务 Set-Cookie/外部重定向不放行。iframe sandbox 仅 allow-scripts，不允许 same-origin/top-navigation/popups，响应 CSP 同步 sandbox。预览令牌为随机短时能力，30 分钟，绑定当前登录散列并每请求检查其仍有效；仅允许已管理端口，不能作为工作台认证。允许网关资源 Origin:null 以支持 opaque iframe 模块加载，其余 API 不放宽认证／Origin。
- 不持久化可回放的启动命令操作；同进程 operationId 防重并绑定输入，断线只查询状态，不自动重发启动。重启后不自动恢复预览进程、不调用模型，进程锁防重，用户明确再启动。任务/round/manifest 改变需刷新，旧轮次不能启动最新效果冒充历史。

## API 合同（前后端固定接口）

WebSocket method `previews/status`, `previews/start`, `previews/stop`, `previews/open` 均 POST，Body `{projectId,taskId,roundId,operationId?,manifestId?}`；start/stop 要 operationId；start 还必须提交 status 展示的 manifestId（配置 SHA256），配置变化或缺少指纹时拒绝启动并要求刷新。全部正常主应用认证/CSRF，socketCall 白名单添加对应路径。返回 `TaskPreview` JSON：
`{taskId,roundId,state:"unavailable"|"stopped"|"starting"|"running"|"failed",message,manifestId?:string,command?:string[],path?:string,url?:string,logs?:string}`。
status 只读，不启动；open 只在 running 时签发受限 URL，不启动；URL 用 `/preview/<token>/...`，客户端只接受这个前缀。stop 不改任务确认状态；有预览进程仍可 merge 时需主动协调停止，避免写入并发，后端须阻止活动开发／合并期间 start，集成操作检查预览状态。
CLI/库结果使用已有 round/report/tools/messages，无预览 manifest 时显示暂无网页效果，仍展示真实运行结果。start 只针对最新已结束轮次，明确已确认轮次可再次预览；busy、缺工作区、旧轮次、身份不匹配拒绝。

## 任务 1：Go 受控任务预览（后端 Agent）

**Files:** 新增 server/internal/preview/*、server/internal/webapi/preview.go 及测试；修改 webapi/handler.go、socket.go、app/app.go；需要时修改 agent/launch.go 仅补充 manifest 指令。不要修改前端或文档。后端 Agent 负责精确定义并实现本计划 API。

- [x] 首先写出并运行失败测试：真实 HTTP 子进程启动/响应/停止；无配置/坏配置；非任务工作区/旧轮次/忙碌；重复 start 不起第二进程；日志界限/超时/端口归属；主进程关闭和原子锁防重；无登录/已撤销登录/过期 token；Cookie 不泄露到 upstream、iframe 隔离及拒绝外部重定向。
- [x] 实现 Manager 的 Status/Start/Stop/Open，归属检查、资源限制与 Close；使用真实 Go helper 子进程模拟网页，不依赖模型和用户环境。
- [x] 接通正常认证 API/socket、gateway 和服务退出；确认 merge 不与预览写入并行。无配置 API 返回 unavailable 不报通用500。
- [x] `GOCACHE=/tmp/kaiwu-go-cache GOTOOLCHAIN=local go -C server test ./...`、`go test -race ./internal/preview ./internal/webapi`、`go vet ./...` 通过，记录 RED→GREEN 证据与实际限制。

## 任务 2：Vue 成果、预览与完成反馈（前端 Agent）

**Files:** apps/web/src/ProgressCard.vue、ChatPanel.vue、App.vue、types.ts、task-state.ts、progress.css；新增 ResultPreview.vue/preview.ts，必要 app CSS 与浏览器/状态测试。不要修改 server 或文档。
**Consumes:** 上述 API 合同。App 为唯一请求集成者，Preview 视图负责加载/状态/iframe；不会把原始 Agent URL 装进 iframe。

- [x] 先写失败浏览器／状态测试：待验收主动作看效果/运行结果、确认完成持久反馈与后续 merge、刷新及旧轮次、切任务异步隔离、手机返回、无预览、失败重试、断线不自动重发启动。
- [x] 结果区改名确认完成，默认查看效果为主，继续修改只聚焦原输入；完成后持续显示已完成、保留成果/预览和合并入口；无检查不得变成验证通过。
- [x] 看板区切换真实预览，桌面保持聊天，手机独立结果视图；iframe sandbox=allow-scripts，网关路径校验；启动前显示命令，运行中刷新/停止，轮次来源标识。无 manifest 显示真实成果/输出以及缺少网页效果。禁止自动发消息索取配置。
- [x] 运行前端状态、build 和浏览器回归；协议测试用真实后端及隔离假 pi，真实 preview HTTP helper。截图只使用合成验收项目，不能包含真实会话。

## 任务 3：集成与独立验收（协调 Agent + 方案设计 profile）

- [x] 核对前后端协议、iframe 路径和鉴权、一轮结果确认闭环；对本批变更逐文件审查。
- [x] 完整 Go/race/vet、前端状态/build/Chromium，检查正常网页 GET、无预览、历史轮次、异步切换、确认刷新、手机无横向溢出。
- [x] 由“方案设计，架构思考”配置独立检查本批增量与验收结果，问题交回对应开发 Agent；复杂问题才升级高消耗。
- [x] 核对主目录文件与快照未被外部改变，再精确回传本批增量，不把快照覆盖回去。更新 README、路线图和验证记录。
- [x] 核对 7331 无进行中工作后更新预览版本，保留数据库/令牌/会话，健康与真实页面只读烟测；若有运行任务则等待或告知，不终止用户任务。没有模型测试就如实记录。

## 审查重点

1. 不可信任务内容不能读取工作台 Cookie 或调用工作台 API；沙箱 Origin:null 仅限预览能力路径。
2. 外部服务抢占端口或服务异常退出后，不能显示无关应用或重复起写入者。
3. 旧轮次/切任务迟到响应不能更换当前预览与确认目标。
4. 确认完成不能改变验证证据或自动合并；刷新仍给用户明确的完成反馈。
5. 断线与服务重启不能重放 start/send 操作，旧任务无预览配置仍能查看并确认已有成果。
