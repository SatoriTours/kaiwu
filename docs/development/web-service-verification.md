# 正式 Web 服务首批验证

2026-10-11。用户已确认看板＋右侧聊天方案、Vue 3＋TypeScript＋Vite、SQLite、本机令牌登录、独立 pi 首次发送启动／切卡片不停／重启显式恢复。

本批交付正式工程与 Go 业务接入；真实模型与执行任务闭环未验收。原型测试／截图保留为历史，不计入以下正式服务结果。

## 实际实现

- Vue 生产页面：功能专题列、卡片、项目／任务聊天、登录／项目登记、空状态、表单、独立草稿／阅读位置、后台回复标记、手机看板／聊天切换。
- Go `workbench`：SQLite schema 版本、事务快照、稳定对象 ID、操作回执／幂等、结构操作 revision 冲突、项目／任务归属校验；删除专题仅迁移分组。
- Go `webapi`：本机令牌 Cookie、Host／Origin／CSRF、大小限制、静态分发／深链接、WebSocket revision 通知和根生命周期。
- Go `agent`：独立 RPC 实例、私有 cwd／HOME／Agent 目录、只读工具、流式回复落库、明确恢复、原会话文件／header／握手 ID 校验、有界进程回收。
- 首启兼容 pi 延迟落盘：握手锁定预期文件和 ID，首轮结束校验文件；已有历史恢复拒绝缺失／空／错误 header，不能换成新上下文。轮次必须同时得到 prompt 确认和 settled 才结束。
- 响应丢失先查原回执，不自动重发；显式“确认上一操作”以原键／内容确认或重试。迟到快照、发送回执、表单结果均按身份代次隔离。
- 创建时无模型配置的会话拒绝偷偷改用默认模型；项目／任务配置快照不追溯修改。

## 环境与依赖

本机 Linux amd64，Go `go1.27.0-X:nodwarf5`，Node `v24.20.0`，系统 Chromium。Go module 基线保持 1.25.0；没有稳定 Go 1.25 或其他系统验收结论。

依赖锁定：Vue 3.5.43、Vite 8.3.4、plugin-vue 6.0.10、TypeScript 5.9.3、vue-tsc 3.3.13、Playwright 1.64.0、modernc SQLite 1.46.1、coder/websocket 1.8.15。SQLite 选择仍支持 Go 1.25 基线的版本，没有跟随要求更高工具链的最新版本。

## 实际命令与结果

Go 命令统一 `GOTOOLCHAIN=local GOCACHE=/tmp/kaiwu-go-cache GOMODCACHE=/tmp/kaiwu-go-mod-cache`；从仓库根目录执行。

| 检查 | 实际结果 |
| --- | --- |
| `go -C server test -json ./...` | 94 项顶层测试通过，2 项助手入口预期 Skip，0 失败；最终 JSON 留在本机 `/tmp/kaiwu-service-final-go.json` |
| `go -C server test -race ./...` | 全包通过；最后加入恢复回归与空配置保护后，另复跑 agent／webapi race 通过；发送幂等／忙碌／重启未知投递补测后 workbench race 通过 |
| `go -C server vet ./...` | 通过 |
| `go -C server mod tidy`／`mod verify` | 完成；all modules verified |
| `go -C server build -o /tmp/kaiwu-web-service-final ./cmd/kaiwu-server` | 通过，产物在临时目录 |
| `npm --prefix apps/web test` | 3/3：范围草稿／迟到确认、旧快照、共享刷新等待队列 |
| `npm --prefix apps/web run build` | vue-tsc 严格检查及生产构建通过，资源复制到 Go embed 目录 |
| `npm --prefix apps/web run test:browser` | 正式服务 Chromium 7/7，真实 Go＋临时 SQLite／Git 项目＋隔离 Go 假 pi |
| Go／前端格式、本地 Markdown 链接 | gofmt 无输出、Prettier check 通过、155 个本地 Markdown 链接有效 |

浏览器 7 项覆盖：

1. 登录→添加项目／专题／任务→A/B 独立草稿和迟到回复→项目独立对话→刷新恢复→删除专题归入其他、历史保留；1440／1024／768／360px，无页面横向溢出，手机顶部不折成竖排。
2. 未知静态文件／未登录 API 与 SPA 深链接区分。
3. 退出后迟到快照不能恢复项目页面。
4. 服务器已接受但发送响应丢失，查询回执且只发一条 prompt，之后可正常发送。
5. 退出再登录后旧发送回执不能清掉新身份草稿。
6. 长对话上翻至 300，切回短项目对话再返回，仍为 300。
7. 中文组合输入与普通 Enter 不提交，Ctrl+Enter 只发送一次。

[桌面截图](../design/web-service/desktop.png) · [手机截图](../design/web-service/mobile.png)。截图来自隔离验收数据，无真实项目聊天日志。截图已人工查看，原型旧图未覆盖。

## 独立验收与修复证据

复用只读审查会话 `kanban_review`。Paseo daemon 未运行且无法取得有效 profiles，本批没有声称按保存 profile 派发；主会话串行编码，原生只读 Agent 审查。仓库尚无 HEAD，未为创建 worktree 擅自提交，所有改动留在原目录。

首轮审查发现并复现：回复超限后消费者退出导致恢复卡死；矛盾 taskId／conversationId 被接受；只传 session-id 可能新建上下文；pi 在项目 cwd 的启动迁移可能写配置；清理失败未上报；迟到身份结果／未知回执锁定；阅读位置被滚底覆盖。均已修复并复验。另修正假进程提前写 header 掩盖的真实 pi 懒落盘差异；只核对本机 pi 源码／SessionManager，不调用模型。

审查者实际独立完成：四个新增业务包 `-race -count=1`、状态测试 3/3、类型检查、正式 Go＋Vue＋假 pi 浏览器首版 4/4；三组 `/tmp` 前端边界反例复验；新 Manager＋重开 SQLite 后原会话恢复／不重发／随后再发送的隔离回归。后者已保存为项目 [restore_test.go](../../server/internal/agent/restore_test.go)。独立结论 Critical 0、Important 0；最后缺构建／空模型保护追加源审无新增 Important，快捷键配置检查也已补齐。审查者未独立重跑扩充后的 7 项浏览器集，7/7 为协调者最终实测。

## 未验证与下一条链路

未调用真实模型、未进行真实 pi 模型端到端、Firefox／Safari、Windows／macOS／Android、远程 TLS／设备配对或 GitHub Actions 验证。只读工具不是文件访问沙箱；服务只管理直接 pi 子进程，不承诺后代进程隔离。

当前任务角标与聊天运行状态分开，聊天结束不代表开发完成。下一条仍是 pi 主协调／执行 Agent、Go 工具桥、独立 worktree 单写入者、实际构建测试证据、审批和结果验收。团队协作与通用可配置编排引擎保留为未来方向，当前优先个人 Web 流程；PC 三平台和 Android 首版目标保留。

没有 Git 提交／合并／推送、Tag／Release、全局工具安装或局域网监听。临时浏览器服务停止，原会话及项目目录保留，未归档工作区。
