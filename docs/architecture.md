# 架构方向与设计边界

状态：**总体方向与未决项汇总；仓库模块划分及首批 Go 规格/计划已获确认。** 已实现 Go 服务基础与内部 pi RPC 包，详见 [基础验证](development/go-foundation-verification.md) 与 [RPC 验证](development/go-pi-rpc-verification.md)，不把总体方向等同于完整功能授权。Web 已确认为主要开发入口，看板方案及 Vue／SQLite／本机令牌／独立会话首批服务已获批实现，见 [当前服务规格](superpowers/specs/2026-10-11-web-service-design.md)；执行编排与远程发布仍待后续设计。

## 已确认的运行拓扑

```text
Web：主要开发入口   PC：Nim + Leaf   Android：Kotlin + KMP + Compose
        \                 |                  /
         \       同机 / 局域网 / 已有 VPN    /
          ------------- Go 服务 -------------
                   |
          单执行节点的 Agent 与代码
          当前优先 pi，Codex 后续
                   |
           项目目录 / 隔离 worktree
```

Go 服务、Agent 与代码同机。客户端不承担代码执行节点职责。暂不引入中心服务、多节点注册、云账号或公网中继。

HTTP/WebSocket 方向为 net/http + chi + coder/websocket。首批已实现 GET/HEAD /healthz、JSON HTTP 错误与回环监听；语言基线 Go 1.25.0，固定 chi v5.2.3。后续 Web 批次已接入 coder/websocket v1.8.15、SQLite v1.46.1、认证和业务 API／事件。

## 已确认的仓库模块

- `server/`：独立 Go module；当前 config、httpapi、app 和命令入口已实现。
- `apps/desktop/`：Nim + Leaf；当前仅说明入口。
- `apps/android/`：Kotlin/KMP/Compose；当前仅说明入口。
- `apps/web/`：Vue 3＋TypeScript＋Vite 正式主工作台，由 Go embed 同源提供。`docs/design/` 中无依赖模拟原型保留为历史参考。
- `integrations/pi/`：pi 外部协议、启动配置与接入边界说明；不放 TypeScript 扩展，当前仅说明入口。
- `contracts/`：跨端产品协议/schema/合成样例；当前仅归属说明。
- `docs/`：跨模块规格、计划与实际记录。

根目录不是 Go module，不添加单模块 go.work；未来其他端放 apps/<端名>/。服务内部依赖为命令入口 → app → config/httpapi。后续按职责增量加包，不预建空接口；跨端共享协议，不共享异构语言的业务源码。

## 已确认的职责边界（未来功能细节待设计）

- **业务 API**：客户端命令、状态查询、审批回应和错误表达。
- **任务协调**：项目、配置快照、主/执行 Agent 关系、状态迁移。
- **pi 适配**：长期 RPC 子进程、命令关联、事件解析、会话/模型/取消能力。
- **编排工具桥**：让主 pi 调用 Go 的任务派发、查询和结果回收；不是只靠提示词模拟派发。
- **进程管理**：启动、stderr 日志、退出与跨平台进程树取消。
- **worktree 管理**：路径/分支、写入锁、保留/回收和人工 Git 操作。
- **存储/事件**：状态与历史持久化、游标、补发、幂等和恢复。
- **客户端**：对话、任务进度、人工确认和结果摘要；不做 IDE。

业务模型与 Agent 原始事件分开。不要把所有 pi 原始协议字段直接固定成多端公共 API，也不要先搭建通用适配插件平台再做第一条闭环。

## Web 主工作台提案

用户已确认 Web 作为主要开发入口，主布局为可手动添加／删除的功能专题列，卡片是任务、右上角有五种执行状态角标，专题归属与状态独立；右侧常驻聊天：不选卡片是项目对话，选中卡片是对应任务对话；PC 和 Android 不取消。看板／聊天是同一工作台，选择只改变展示与发送范围，不控制 Agent 生命周期。建议 Vue 3 + TypeScript + Vite 仅用于浏览器 UI 与静态构建，生产由同一 Go 服务提供页面/API/事件，不引入 Node 后端、SSR 或 pi 扩展。用户已确认 Vue 3＋TypeScript＋Vite；正式页面由 Go embed 同源分发。

用户已进一步确认：项目对话和每张任务卡片的对话，背后各自绑定独立代码 CLI 会话，目前均为独立 pi 会话。Go 维护 Conversation ID 到 pi 会话及运行实例的绑定；选择卡片切换聊天目标，消息、历史、模型上下文及事件分别归属原会话，其他会话的执行不因选择变化而停止。独立会话不意味着进程永久常驻，首批已实现首次发送启动、切范围不停、原会话文件校验；重启后发送新消息或继续开发时自动接回原会话（不重放旧指令）；最多 8 个实例。未开始任务和项目只读模式在私有 cwd 启动；卡片明确“开始开发”后原 session 在独立 worktree 启用写入／bash，Linux 进程组和持久归属记录保护同工作区单写入者，轮次结束待用户确认。可信编排桥与完整验收待实现；本批仍仅假 pi 验证，见 [验证记录](development/web-task-start-verification.md)。

产品协议需显式表达项目／任务 Conversation、CLI 会话绑定与消息归属，快速切卡片的迟到回复只能更新原范围。Web 共用产品任务协议、配置快照、审批、验证记录与服务端生命周期；浏览器断开不停止 Agent。认证/CSRF/Origin、事件游标和幂等、持久化、真实工具授权边界是业务接入前置设计，不能只补网页按钮。详见 [完整设计](superpowers/specs/2026-10-11-web-workbench-design.md)。

## 卡片 Git 集成（已实现）

Go `gitops` 负责默认主分支识别、工作区归属、真实修改／祖先关系、审核指纹及固定参数 Git 调用；`agent.Manager` 管理有界检查、同 Git common dir 的集成锁、任务保留权、空闲 pi 回收及服务器持有的异步操作。SQLite 保存兼容缺省的项目设置、Git 观察值与合并阶段／SHA；重启只核对证据，不重新执行提交或合并。

合并前可靠结束目标任务空闲 pi，保留 session ID／文件，接管任务 OS 锁。连续 Git 子进程每次先保存未知启动窗口，再记录实际进程组；不按数据库 PID 杀进程。同仓库两张卡片集成串行，其他 worktree 可继续开发。Git 不在 Store 全局锁内运行，检查结果不变不写库；结构编辑使用独立 structureRevision，避免后台观察使正在填写的表单无故过期。

Vue 卡片及聊天展示服务器检查结果，审核框只显示目标、SHA、身份与文件路径，不提供源码／diff。预检 ID 绑定内容、索引、文件模式、设置及两个 HEAD，确认后重核；请求丢响应沿用 operation ID。已合并由当前任务 HEAD 的祖先关系决定，不由 Agent 回复或旧操作回执决定。完整实现、测试及限制见 [验证记录](development/web-task-merge-verification.md)。此入口的审核不能替代 pi 原生 bash 的通用审批或系统沙箱。

## 固定自动交付闭环（规格／计划已审，基础已实现、闭环未完成）

用户已审核统一项目配置的开发交付[书面规格](superpowers/specs/2026-10-17-automated-delivery-design.md)，[实施计划](superpowers/plans/2026-10-17-automated-delivery.md)及串行实现方式已获审核。计划由纯 Go `delivery` 固定状态机协调原任务 pi、`gitops` 和只读 GitHub Actions REST 适配；任务分支检查通过后串行集成，主分支 CI 通过才标记技术交付。源码修复在原任务 worktree／会话进行，不把 Git／CI 生命周期交给浏览器或仅靠模型提示词。

交付记录、有限修复预算、配置授权快照和持久逻辑队列独立于现有轮次人工确认／实际 Git 状态；副作用意图先落库，重启先核查精确 refs／SHA／run attempt。Tasks 1–12 的配置／持久化、只读 Actions、凭据桥、受控远程 Git、原任务修复、FIFO 集成、崩溃核查与服务装配已实现并通过隔离测试。官方资料阻塞已解除；Task 13 界面及真实 Store／agent／delivery／webapi＋临时 bare＋假 pi／Actions 的隔离服务链路已经实现，交付设置界面已部署。Task 14 安全验收仍不通过，现用服务不装配真实交付 worker、启用保持禁用；真实模型／GitHub 未联调，计量残余及旧崩溃注入观察失败见[审查续接](development/web-automated-delivery-review-resolution.md)。历史实施见[执行记录](development/web-automated-delivery-ledger.md)。现有本地手动合并的授权与恢复规则继续有效。

## 任务成果与受控 HTTP 预览

Vue 在主内容区展示成果及真实任务效果，右侧保留原任务聊天。完成确认沿用最新已结束轮次的精确令牌，成功反馈来自持久任务／轮次状态；Git 状态与后续审核合并独立，无配置任务可查看真实工具输出并手工补充说明。

Go `preview.Manager` 负责受控 manifest、展示指纹与启动输入绑定、显式进程启动／停止、回环 socket 归属、资源界限与短时能力网关。通过 `agent.Manager.reserveTask` 安全回收空闲 pi 并转交原执行锁；新开发与合并在 Agent/Store 锁外停止预览，保留原 session 文件与结果。子进程继承 flock，正常退出主动释放容量；leader 身份保留至后代清理完成，避免 reap 后按复用 PID 发信号。异常启动归属未知时拒绝重复启动。

浏览器 iframe 与响应 CSP 均使用 sandbox allow-scripts，无同源权限；能力只允许当前受控任务的 GET/HEAD，并每请求核验原登录仍有效。工作台 Cookie／Authorization 不转发，Origin:null 仅用于能力路径的 opaque 资源。不开任意 URL 代理、不暴露新的局域网端口，不代表对本机任务代码提供系统沙箱。初版不支持 HMR／应用登录／任意根路径资源，30 分钟能力失效需手动刷新。详细约束与实际测试见[实施计划](superpowers/plans/2026-10-17-task-result-preview.md)和[验证记录](development/web-task-result-preview-verification.md)。

## pi 接入路线与本批次边界

**已确认：Go 直接管理 pi RPC 子进程；开悟自有后端全部使用 Go。** 此要求取代此前 TypeScript 可信扩展和 SDK 宿主的建议，不新增这些实现。pi 本身仍为第三方 CLI，不重写 Agent 或改变客户端语言。

- `server/internal/pirpc/` 已实现进程、JSONL、请求关联、有界原始事件和故障退出，并通过隔离 Go 假进程自测；独立验收已通过，首轮问题已修正复验；后续 Web HTTP/main 已通过 agent.Manager 接入独立RPC会话，非仍停留在基础包阶段，不先建设通用适配平台。
- RPC 只提供通信与进程状态，不把请求响应或 `agent_settled` 映射为产品任务验收成功。
- Go 拥有进程生命周期；stdout/stderr 持续消费，事件由服务内部接收，不由客户端连接直接拥有。
- 当前先用纯 Go 假进程测试；真实 pi 启动配置、凭据和会话持久化另行验证，不继承个人扩展。
- 后续模型可调用的编排工具优先研究 Go 实现的 MCP 服务，对接 pi 现有内置 MCP 能力；本批次不实现 MCP。MCP 不会自动接管 pi 原生工具的权限，完整授权边界另行设计。

本批次模块边界、具体生命周期、限额、故障策略与验收已随 [书面规格](superpowers/specs/2026-10-11-go-pi-rpc-design.md) 获用户确认；[实施计划](superpowers/plans/2026-10-11-go-pi-rpc.md) 及串行编码＋独立只读验收方式已批准；代码/本机结果见 [验证记录](development/go-pi-rpc-verification.md)。不使用终端屏幕抓取。证据见 [接入研究](research/agent-integration.md)。

## 必须保留的正确性边界

- 命令被接受、单次 Agent 运行结束、整个开发任务验收完成，是不同状态。
- 配置模板、任务快照、任务内模型覆盖，是不同数据。
- 客户端连接生命周期与服务器任务生命周期不能混为一体。
- 缺失凭据、provider 不可用、模型不支持、工具失败、进程退出、状态无法恢复，应分别可观测。
- 合并/推送/破坏性操作不能只靠 Agent 自律；需要可信执行边界和人工确认记录。
- worktree 隔离不等于进程沙箱；不能声称 Agent 无法访问其他路径或凭据。
- 同工作区禁止并发写，串行控制须由服务实现，不只写成提示词。
- 取消不是发送一个 abort 就宣称所有后代进程已停止。

## 尚未确定，不能擅自补完

1. Go 服务正式支持的系统、CPU 架构、最低版本。
2. 已确认 Go 1.25.0 语言基线、chi v5.2.3；稳定工具链及完整兼容矩阵尚未验证，WebSocket v1.8.15、SQLite v1.46.1 已固定；pi 实际联调及 Leaf/Kotlin/Gradle/Compose 项目版本待确认。
3. 首批本机令牌登录已确认并实现；远程认证、设备配对、TLS、凭据管理与 Android 联调方式待定。
4. SQLite、按新用户意图接回会话、合并核查、轮次报告和用户结果确认已实现；正式 Web 使用双向 `/api/socket`，256 项／2 MiB 增量窗口、实例标识及分块快照恢复，初次认证与身份检查保留 HTTP；长期历史容量、跨版本迁移与协议兼容仍待设计。
5. 协调工具接口、结果回传、重试、幂等、任务限额和等待用户的规则。
6. pi 模型变更的精确生效边界、会话恢复与审批能力。
7. 通用工具／命令授权与 worktree 回收；项目自动初始化 Git、卡片显式审核提交并普通合并已确认实现，不授权自动推送。
8. Leaf 后台网络队列到 UI 线程的安全投递和重绘；如需改 Leaf，另行授权。
9. Android SQLite 访问库、共享模块、自动更新源、签名、安装交互。
10. 三平台 PC 构建/原生窗口验收、多端发布流水线及许可证。
11. Web Vue／依赖／Go embed 打包及首批登录方案已确认实现；浏览器支持矩阵、远程安全与长期协议兼容策略待验收。

Go 起步工程已在本机验证，但这些未决项仍需后续设计。基础工程不能替代完整产品、真实 Agent、双端或发布验收。
