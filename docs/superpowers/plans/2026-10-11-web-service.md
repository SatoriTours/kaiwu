# Web 正式工程与独立会话实施计划

> 使用 superpowers:executing-plans 串行执行，完成后复用一个只读审查 Agent。当前仓库无 HEAD，Paseo daemon 停止；不制造提交或 worktree，不声称使用无法查询的 profiles。原会话始终协调交付。

**Goal:** 将已确认看板界面做成 Go 同源托管、SQLite 保存、可路由独立 pi 对话的实际 Web 工程。
**Architecture:** Vue 管理界面；Go workbench 管理事务快照／回执，agent 管理 pirpc 会话，webapi 管理认证／命令／事件／静态资源。依赖调用单向，不把原始 CLI 协议固定为公共 API。
**Tech Stack:** Vue 3、TypeScript、Vite、Go 1.25 基线、modernc SQLite、coder/websocket；实际精确版本写入锁文件。
**Spec:** [正式服务规格](../specs/2026-10-11-web-service-design.md)。用户已确认技术与工程策略，继续实施，不要求重复确认界面。

## 全局限制与 Review Focus

- 无 Node 后端／自有 pi 扩展；仅回环访问，不调用真实模型进行测试，不执行当前仓库 Git 提交。
- 首批 pi 对话只读；真实执行编排／worktree／审批链路不以聊天成功冒充完成。
- 特别检查：事务失败不发布；操作键重复不重复发送；错误 project/task 归属拒绝；客户端断线不终止进程；重启不重放在途消息。
- 无 HEAD 的辅助脚本不可用，以 `.superpowers/sdd/2026-10-11-web-service/progress.md` 手工记录实际 RED/GREEN 与偏离，不删除未提交成果。

### Task 1：SQLite 产品状态与幂等

Files: `server/internal/workbench/{types,store,commands}.go` 与 `store_test.go`。
Interfaces: `Open(path, Profile) (*Store,error)`；`Snapshot() State`；`Command(Command) (Receipt,error)`；`Update(func(*State) error) error`；`Subscribe() (<-chan uint64,func())`；`Close()`。

- [x] 写并运行缺失模块 RED：重开保存专题／任务／Conversation、删除移入other不丢草稿／历史、重复操作回执相同、错误归属和版本冲突不写入、回执复用异内容冲突、重启标中断。
- [x] 实现纯模型、SQLite schema／事务、版本和限制，运行 `go test ./internal/workbench` GREEN。

### Task 2：安全 HTTP 与静态分发

Files: `server/internal/webapi/{handler,auth,commands,events,static}.go` 与 `handler_test.go`。
Interfaces: `New(Config, *workbench.Store, Commander) http.Handler`；Commander `Send(conversationID,messageID string) error`、`Resume(conversationID string) error`。

- [x] 测试未登录／错误令牌／Origin／Host／未知API／静态路径、登录Cookie、命令幂等；运行 RED。
- [x] 实现认证、统一错误、命令校验、revision通知和SPA分发，运行 GREEN。

### Task 3：独立 pi 生命周期

Files: `server/internal/agent/{manager,launch,events}.go` 与 `manager_test.go`；`server/internal/config/config.go`、`server/internal/app/app.go` 集成。
Interfaces: `New(ctx,store,Config) *Manager`；`Send/Resume/Close`。测试注入RPC接口／隔离Go假进程，不读取个人配置。

- [x] 先测试 A/B 归属、重复发送／busy、接受不等于结束、失败／uncertain不重发、重启显式恢复、关闭客户端不关闭worker、Close回收，观察 RED。
- [x] 实现按需进程、稳定session、隔离环境、事件适配；配置选择保持旧健康服务测试可用；运行包测试及全套 Go GREEN。

### Task 4：Vue 工作台与构建

Files: `apps/web/package.json`／lock／Vite／TS 配置，`src/{App,KanbanBoard,ChatPanel,TaskDialog}.vue`、`src/{api,workbench,types}.ts`、`src/style.css`，`tests/workbench.test.ts`。
Interfaces: 使用 Task 1／2 的公开 camelCase JSON 和 revision；状态按Conversation ID独立；构建产物复制到 Go webui 可embed目录。

- [x] 测试迟到接受／新草稿、重复选择、版本／断线错误，先RED。
- [x] 实现正式空状态／登录／添加项目、看板／专题／卡片、任务创建、独立聊天、显式恢复及事件刷新；移除演示控件，禁用未实现执行动作。
- [x] `npm test`、`npm run build`，严格类型检查／静态构建GREEN。

### Task 5：服务集成与浏览器验收

Files: `apps/web/tests/browser.spec.ts`、`playwright.config.ts`；`server/internal/webui`／`server/cmd/kaiwu-server`；README／roadmap／新验证记录。

- [x] 用临时SQLite／临时Git仓库／假pi运行实际服务，浏览器先测登录→建专题任务→选择／历史／迟到消息→刷新／重连，观察失败后完成集成。
- [x] 四尺寸、长文本、键盘／组合输入、专题删除和不同会话消息；真实构建页取截图并人工查看。
- [x] Go test/race/vet/构建、npm test/build、浏览器、链接检查；记录确切输出与未验证项。
- [x] 一个只读 Agent 独立验收，修复 Important 并复验；保持代码／记录可继续开发，不提交、不推送、不遗弃后续执行链路。


## 完成记录与实际偏离

已执行以上五项。本批实际证据与剩余边界见 [验证记录](../../development/web-service-verification.md)。webapi 先集中在 handler.go，而非预设多个文件；测试通过隔离 Go 进程注入而非内存 mock。Go 共 94 项顶层通过、2 项助手入口 Skip；状态3项与正式浏览器7项通过。没有运行真实模型、执行任务/worktree、远程或原生端验收；不把这些后续能力勾选为完成。未提交或推送代码。
