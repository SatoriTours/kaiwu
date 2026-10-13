# Web 工作台正式工程与独立会话

状态：用户已确认原型设计，并明确采用 Vue 3＋TypeScript＋Vite、SQLite、本机令牌登录、pi 首次发送按需启动／切卡片不停／重启显式恢复、回环监听。本规格将这些决定落实为首个服务版本，不重新设计主界面。

## 交付与边界

正式页面放在 apps/web，Go 同源提供页面、API 和 WebSocket。无演示任务／假回复／手动状态切换；空项目从添加 Git 目录开始，专题、任务、聊天保存到服务器 SQLite，刷新不丢。配置从服务器环境指定 pi 的绝对路径、provider、model、thinking；创建任务冻结这些值的快照，不把凭据下发浏览器。

首个服务版本完成真实 pi 的项目／任务独立对话通路。为避免尚无可信授权／执行桥时写共享 checkout，各会话仅开放 pi 的 read/grep/find/ls，禁止 bash/write/edit、扩展、MCP、项目配置与自动上下文发现。不提供虚假的“开始开发”按钮。任务执行 Agent／Go 编排桥／worktree 写入锁／实际验证与结果验收仍是下一条链路，不能把只读聊天结束显示成开发任务完成。

PC 三平台和 Android 保留首版目标，后端全部 Go。测试仅用隔离假进程；真实模型调用另说明。未经明确授权不操作当前仓库提交／合并／推送，不安装全局工具，不开放局域网服务。

## 数据和一致性

SQLite 使用纯 Go 驱动、单连接、事务和版本化 schema。最小存储为完整产品快照及操作回执：Project(id/name/path)、Topic(id/projectId/name/protected)、Task(id/projectId/topicId/title/request/status/configSnapshot)、Conversation(id/projectId/taskId/sessionId/runState/messages)。标识使用随机 UUID 风格稳定 ID，与名称无关；项目／每张卡片创建一个独立 Conversation。

写入在服务锁内复制快照、验证、提交事务后发布新 revision。新增／删除专题、新建任务和发送／恢复均使用 operationId；重复相同操作返回原回执，复用键但内容不同返回冲突。看板结构操作的预期 revision 不匹配返回 409，客户端重新读取并保留表单。发送／恢复按稳定会话 ID、项目／任务归属及忙碌状态判定，不因其他会话的流式 revision 变化误拒绝。删除专题仅转移 task.topicId，不删会话、状态或结果；other 不可删除。失效专题创建回退 other 并返回提示。

单实例拥有数据库；同文件第二个实例不能越过版本条件覆盖前者。快照最多 100 项目、每项目 100 专题／1000 任务、每会话 2000 消息、每条 5000 字用户文本／1 MiB 助手文本；到限拒绝并解释，不静默截断历史。幂等回执最多 10000，满时拒绝新操作而非遗忘旧键。消息 pending/accepted/complete/failed/uncertain 与任务五态分离。初始化将未结束会话标为 interrupted，保存消息，不自动重放。

## API 和安全

根页面可读取登录表单，业务和事件必须认证。KAIWU_WEB_TOKEN 至少 32 字节；POST /api/login 校验令牌，设置随机 HttpOnly/SameSite=Strict Cookie（仅回环 HTTP）；会话 12 小时，重启失效，退出注销。令牌不放 URL、日志、前端构建或 localStorage。Host 仅允许配置的回环 IP／localhost 与准确端口；Origin 必须与请求 host 同源，写入还需固定 X-Kaiwu-Client 头，JSON Content-Type、大小限制、未知字段拒绝；不开放 CORS。登录限速。CSP、nosniff、禁嵌入；消息用 Vue 文本插值，不渲染原始 HTML。

GET /api/snapshot 返回当前权威版本与数据；POST /api/commands 接收 {operationId,revision,type,projectId,topicId,taskId,conversationId,name,path,title,request,text}，服务校验目标归属，返回 {revision,createdId,notice}；GET /api/operations/:id 查询回执。GET /api/events 为经身份／Origin 检查的 WebSocket，只通知 revision；连接即发送当前 revision，客户端收到后重取快照，弥合读取／订阅窗口。慢客户端合并通知，不向浏览器透传原始 pi 事件／stderr。未知 API 返回 JSON，不落入 SPA。页面深链接回 index，缺失静态文件为 404。

## pi 生命周期

创建卡片／选择／返回项目仅改变对象和展示，不启动 CLI。首次明确发送时服务确认有效冻结配置，在该 Conversation 私有目录启动 pi，使用固定 session-id 和 session-dir，并通过系统说明传入项目绝对路径。模型未配置时创建的空快照不自动改用默认模型。进程环境显式构造：隔离 HOME／Agent 配置目录，只传 PATH／平台必需变量与已配置 provider API Key；不读取个人 OAuth 配置或扩展。Program／provider／model／thinking 为服务配置，不由普通消息任意覆盖。

每会话至多一个运行实例，一次在途 prompt；忙时拒绝并保留草稿，不插队或打断。不同会话各自运行；全服务最多 8 个驻留实例，达到上限给出原因，本批不偷偷回收／重建。首次握手允许 pi 尚未创建会话文件，只锁定准确 ID／私有目录内预期路径；首轮结束检查有效 header。已有上下文恢复必须事先校验文件存在／header ID，启动后再次校验 get_state，缺失或损坏拒绝恢复。消息接受与真正结束分开；持续消费 text_delta/message_end/agent_settled，仅 prompt 确认与 settled 均满足才释放轮次；事件结束不验收任务。未知投递或进程失败保留记录并标中断，禁止自动重发。恢复需要显式操作，复用同一 pi session-id，失败不能悄悄新建替代上下文。

进程由服务根 context 拥有，HTTP 断开／WebSocket 关闭不取消它。服务停止显式回收所有直接 pi 子进程，沿用 pirpc 的限时回收语义，不宣称后代进程沙箱能力。只读工具模式不提供 shell，仍不等于文件访问沙箱。

## 浏览器交互和验收

沿用已确认两栏／窄屏两视图，Vue keyed cards 避免原型整体重建丢焦点。独立缓存 conversation 的历史、草稿版本、阅读位置、未读与提交状态；异步回执按捕获 ID 合并，用户可切卡片继续输入。离线禁写且保留草稿，重连按权威快照恢复，不自动重发副作用命令；发送回执丢失通过原 operationId 查询。未发草稿在隐藏范围也有离开提示，退出清空身份内容缓存。

验收：SQLite 重开／删除专题不丢任务会话、幂等／冲突、身份／CSRF／Host／WS、安全静态分发；隔离假 pi 的 A/B 并发、迟到回复、接受后失败、进程退出／恢复与服务退出；正式构建页面登录→登记临时仓库→专题／任务→对话→刷新→切范围／重连，以及 1440/1024/768/360px。分别报告 Go、浏览器、假进程和未做的真实模型／其他浏览器测试。

团队协作与通用可配置编排引擎仍是未来方向，当前优先个人 Web 入口。
