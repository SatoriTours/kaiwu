# 任务产物直达与双合并操作实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. 先失败测试、最小实现，再自测和独立验收。当前用户已批准设计并要求开始开发，不再审批文稿或执行方式。

**Goal:** 直接打开任务真实网页／HTML产物，移除冗余修改入口，区分本地合并与合并并推送，取消独立验收必经步骤。

**Architecture:** 沿用 Go preview 能力网关和 agent/workbench/gitops 的显式审核、持久操作及单写入机制；不启用尚未验收的自动 delivery worker。Vue 精简结果操作，直接打开隔离预览；已有轮次历史及原任务对话保留。

**Tech Stack:** Go、Vue 3、TypeScript、SQLite、现有双向 WebSocket；不加运行依赖或自有 pi 扩展。

**Spec:** 用户于本会话确认的“看实际产物 → 不满意继续聊；满意后选择本地合并或合并并推送”方案，修订 `docs/superpowers/specs/2026-10-16-task-result-preview-design.md` 和 `docs/superpowers/specs/2026-10-12-task-merge-design.md` 的人工操作；既有自动交付规格未通过的 worker 继续禁用。

## 已批准交互与边界

1. 网页点击直接打开当前任务网页，HTML点击直接预览并加载相对静态资源。默认新标签页，原工作台与聊天不跳走。首次／变化的启动命令必须经明确授权，已授权且输入不变可一次点击启动并打开；“查看”或后台状态查询不自行调用模型、安装依赖或启动未知命令。
2. 无可用网页／HTML时显示真实“运行结果”，没有可查看产物不展示空网页按钮；可用的显式文件产物可下载。不能从 AI 任意 URL 或文件路径猜测可信入口，不打开旧线上版本冒充当前任务。
3. 删除“继续修改”按钮，用户继续在原任务聊天发送要求；保留草稿和原上下文恢复。
4. 两个替代操作：“合并到主分支”（必要时提交，然后仅本地合并）／“合并并推送”（本地集成后推送已审核的精确主分支 SHA）。已经合并则提供“推送到远程”，不重复 commit/merge。推送不能暗含 Tag、Release、自动修复、自动归档。
5. 审核展示本次文件、目标主分支、精确远程／待上传提交范围及可能触发的构建部署。操作结果分清本地合并、远程上传、CI，不用“已交付”替代这些事实。推送失败保留本地合并成功事实；网络未知先核查，重启不盲目重放。
6. 不强制先点“确认完成”才能合并；在明确审核合并时记录对所绑定最新已结束成果的认可。没有 Git 集成需要的任务仍有轻量“标记完成”；仅打开网页不认可结果。新源码或新轮次使旧审核失效，验证缺失不因认可而变为通过。
7. 大块固定“开发已结束”提示改成紧凑的实际轮次／改动／验证信息与操作。不把一轮结束说成整个开发完成。桌面、360/390px、键盘均可用。
8. 真实生产仓库／模型／远程 Actions 调用和部署不在本轮授权；测试用临时 Git／bare、假 pi／HTTP／transport。7331和既有凭据不变。
9. 用户追加明确要求：移除顶部置顶的任务概况／往期记录，每次用户提问对应对话流内一张AI回复（合并所有过程与最终消息），默认1–2句易懂的当前动作／结果摘要，不展示命令／实现细节；执行中在同一气泡显示loading，无回复时也有占位，结束／失败／等待用户时停止。点击摘要可展开原文、方案、工具、验证与历史记录，不删存储消息、不新增摘要模型调用。仅最新结果提供产物／Git／标记完成操作，历史不能被当作当前可运行产物。
10. 对话列表增加“全部／只看我的问题”纯显示筛选；不改模型上下文／存储／发送内容。点击历史问题返回完整上下文并定位该提问及回答，不被自动滚动到底覆盖。筛选期间保持后台执行、草稿和必要的待处理交互可达，切卡片不串消息。

## 全局约束和协作边界

- 当前唯一关联空间 `wks_1fe0f0d8c2bbce08`；协调 Agent `a32223bf-a426-42a7-80de-b1d43107e1f4`；cwd `/home/jimxl/projects/kaiwu`，分支 `main`，git-dir/index `.git`／`.git/index`，基线 `04d50f4`。
- 默认同空间串行写入，不新建 Workspace／worktree／分支，不 Git 提交、合并或推送本项目。测试临时仓库的 Git 操作仅限测试夹具。
- 原有子 Agent 后端等混合改动保留，基线清单及共享文件 diff 存于 `~/.local/state/agent-artifacts/kaiwu/2026-10-24-task-result-actions/`；不得回退／清理或混入本任务结论。
- 消费接口先完成并冻结，再交 UI；当前预览合同已冻结，预览 UI 与不重叠的远程合并后端可并行实现。同文件单写入者，共享 Go 写入和编译型独立验收串行；最终验收以冻结快照为准。主控负责计划、TODO和当前文档说明；执行 Agent 不改这些台账、不继续派生。
- 仅共享当前任务必要上下文；不写入凭据、真实会话。临时脚本用 mktemp，必要证据单份留上述 artifacts。

## Review Focus

- 预览命令变化、历史轮次、任务切换、重复点击及弹窗拦截：不执行未审核命令，不打开错误任务，不丢失聊天草稿。
- 静态路径的点段／编码分隔符／符号链接／FIFO及私密文件：拒绝越界和凭据访问，HTML脚本无法取得工作台权限。
- 本地已合并但远程未推送／推送拒绝／未知回执：保留明确阶段及原结果，重试不重复提交，不强推。
- 远程配置／目标／主分支／成果轮次变化及本地额外提交：旧审核失效，不能静默上传未审核历史或改写远程。
- 合并认可与轮次／验证状态：只认可审核绑定的结束结果，失败／等待交互不可被错误标完成，已有自动交付准入不被绕过。

## Task 1：后端产物与隔离直达预览

**Files:** `server/internal/preview/`、`server/internal/webapi/preview.go`及相应测试；必要时`server/internal/agent/launch.go`的产物说明；契约增量集中到`apps/web/src/types.ts`的TaskPreview，保留原subagents改动。

**Interfaces:** 保留 `previews/status|start|stop|open`、`preview.Request`及现有v1命令配置；TaskPreview向后兼容增加明确产物种类／展示入口字段。静态模式不启动额外 CLI，不依赖 Python。新标签页入口通过现有能力网关提供隔离HTML包装或等效sandbox，不让任意任务HTML直接继承工作台同源权限。

- [ ] 先增加失败测试：显式静态 HTML＋相对 JS/CSS/图片、无需命令进程；单一可确定 HTML 入口可直接发现，多个候选不能任意选；无产物返回真实unavailable而不是假网页；文件产物安全下载（仅显式声明）。
- [ ] 配置格式兼容v1；静态入口可用明确的v2 manifest（kind/root/entry），无配置时仅有界确定性发现任务HTML，不递归扫描整个项目或任意读取机器路径。明确不可自动确定的原因。
- [ ] 增加攻击与生命周期测试：路径遍历／编码／symlink／非普通文件／隐藏私密文件、Cookie／Authorization隔离、原登录撤销、manifest变化拒绝旧启动、历史和迟到响应、重复打开不重启。
- [ ] 实现最小入口、受控文件网关和一键可打开地址；命令首次授权指纹继续生效，不以一次点击放宽全部命令执行。
- [ ] 执行 `GOTOOLCHAIN=local go -C server test ./internal/preview ./internal/webapi ./internal/agent` 的相关测试，保留实际结果及hash。报告UI契约和既有兼容行为。

## Task 2：审核绑定的本地合并／远程推送

**Files:** `server/internal/workbench/git.go`及测试、`server/internal/agent/integration*.go`及测试、`server/internal/gitops/`、`server/internal/webapi/git.go`及必要socket路由、`server/internal/app/app.go`装配、`apps/web/src/types.ts`的Git／Merge类型。只有必要增量可触及有基线改动的共享文件。

**Interfaces:** 复用 `git/preview`、`git/merge`及已有操作查询。请求增加显式 mode（缺省仍为原本地合并），审核／operation固定相同mode、remote/ref/SHA、最新可认可roundId；本地/remote事实字段与phase向后兼容。远程动作不得借开启 delivery worker 实现。

- [ ] 先失败测试：local不调用任何写远程命令；push合并后精确SHA上传；already-merged只推送；重复operation不重复副作用；push拒绝仍保留MainAfterSHA；未知结果只核查；重启不重放。
- [ ] 将远程目标及待上传历史纳入只读审核和冻结指纹；仅允许受支持且实际匹配的远程，不继承任意pushurl／insteadOf／helper／hooks，不含force/mirror/tags；复用已存在的受控凭据能力，无凭据明确阻塞但不影响本地动作。不修改认证或从浏览器收集凭据。
- [ ] 共用原项目／任务租约和串行集成机制，持久化先于副作用；操作期间禁止同任务写入，活动自动交付不可绕过。真实可执行production路径不可为测试接受任意file://目标；临时bare通过窄测试注入。
- [ ] 用户明确确认的当前round与审核一起绑定；原子记录认可，不自动篡改checks，不要求先confirm_result。结果或Git输入改变拒绝旧审核。旧API无新字段保持原行为。
- [ ] 验证远程并发推进／拒绝、凭据缺失、敏感文件、不同任务争用、断线、重启、操作ID变参；CI未查不报告通过。必要CI观察复用只读能力，不能自动补workflow或触发真实网络测试。
- [ ] 开发者运行相关Go包、稳定后全量 `test ./...`、受影响包race／vet，报告退出码、hash、未验证项及UI完整契约。不得扩大成修复旧自动worker全部历史缺陷。

## Task 3：前端紧凑操作与直达入口

用户追加的聊天整理可先作为Task3a独立实现（ChatPanel/AssistantReply/chat-messages/专属样式测试，不改后端或types契约），Task3b消费后端契约后由同一UI执行者接续；同文件始终单执行端。

**Files:** `apps/web/src/ProgressCard.vue`、`ChatPanel.vue`、`App.vue`、`MergeDialog.vue`、`ResultPreview.vue`、`preview.ts`、必要Git显示／样式／类型，以及相关单元／浏览器用例。

**Consumes:** Task 1/2 完成报告中的实际契约，先读新增diff再实现，不先写另一套猜测契约。

- [ ] 先失败单元／浏览器测试：网页和静态HTML新标签页直接可见；原任务聊天和草稿保留；无配置不出现空网页；命令首次审核／输入变化，已审核输入一次点击启动；拦截弹窗可见安全重开链接。
- [ ] 移除所有冗余继续修改按钮，大提示变紧凑真实信息；保留运行结果／历史输出查看与轻量标记完成，不以页面打开替代认可。
- [ ] 同时提供本地与remote替代操作，已合并变仅推送；审核展示目标／待上传范围／副作用；失败阶段准确；刷新、迟到响应、重新登录不重发操作。
- [ ] 保留全部原任务历史／消息／交互／登录能力，更新确已被新设计替代的旧用例，不删除失败断言规避回归。
- [ ] `npm --prefix apps/web test`、`npm --prefix apps/web run build`、相关Playwright用例；临时回环Go＋假pi／临时bare，不占7331。截图只留必要桌面／360／390证据到共用artifacts。

## Task 4：独立功能验收与收口

- [ ] 开发停止写入后，以实际文件hash交独立功能验收角色；默认不改实现。
- [ ] 独立验证两类真实预览、原聊天继续修改、两个Git动作及already-merged、远程失败／未知回执／越权阻断、认可轮次绑定、桌面／手机／键盘及既有任务回归。无真实模型／生产GitHub操作。
- [ ] 普通缺陷返回原执行者修复并聚焦复验；同根因两轮不收敛暂停判断升级，不用重命名重置计数。
- [ ] 主控核对diff、证据、混合基线及测试hash，更新TODO／README／roadmap与原规格的修订说明。报告本地开发／独立验收／真实模型／实际7331部署分别状态，不提前宣称完整完成。

## 阶段记录

- 2026-10-24：设计批准，已核对当前空间／cwd／Git和最新6个profiles；本轮后端涉及凭据、持久副作用与跨模块恢复，选择复杂编码角色是实现复杂度原因，不是额度替补；UI用UI角色，独立验收用功能验收角色。具体provider/model见TODO派发记录。角色用量未知。
- 当前阶段：静态预览安全后端、Task2a原子认可、Task2b只读远程审核及F1–F3、Task3a交互已独立通过；不等于全部Task1/2/3完成。凭据桥真实401/冲突拒绝/管道单次读取已通过，R1.5总输入声明差异在下一开发片收紧并补证。
- Task3b-preview首次独立验收不通过：回退链接清空、新轮迟到、双击重复和命令生命周期证据缺口。原UI R2只读文件后返回空assistant，旧总结被重复通知，hash未变，未据此启动新验收；原执行端已关闭，同UI配置新上下文 `d25606b7-92ff-4df2-b173-e17e659f4aaa` 在原Workspace有界续接，仍计R2，不换模型或重置修复次数。
- Task2c/d源代码已实现审核后push，但首次独立验收不通过：最终合并证明、阶段/保存失败恢复及缓存有缺陷，HTTP/WS、活动远程进程清理与新总输入上限证据不足。主控另以真实临时Git证实已经合并后重复merge会改ORIG_HEAD。原89已接集中修复R1，唯一Go写者，不重推未知操作、不启用自动worker、不部署。
- Task3b-preview R2独立仍不通过：F1和命令F5主体已过，同卡新轮已隔离，但ABA切回后旧回执复活及120/250ms快速二击每次新开仍在。主控已暂停第三轮同策略微补，决定统一intent+单调失效，交复杂编码角色 `1f30f048-7130-4201-b564-3afeb9fc4c6f`（pi/open-design/deepseek-v4.1-flash，请求high）接R3，非额度替补/计数清零；原UI新旧两端均closed。UI先只纯TS源码/单元/typecheck，不启动Go/browser或Vite build/dist写入，待原89冻结Go后续接一次浏览器回归；再独立验收，不重复未变快照。
- R3纯TS首片已有单元/typecheck通过但未浏览器验证；主控读源码发现unknown状态核查被误当可重放、open-only回落Start、gesture分窗及死历史status=>Open，已同R3交回一次有界纯TS修正，不重置计数、不解Go冻结。ordinary Status不含操作回执，不得据stopped伪造“旧启动未执行”。
- 后端2c/d R1已Go冻结并有五包自测/vet/build及单点race，主控已核新hash，不等同独立通过。前端1f30f048现获准接续一次build+17445真实E2E；其结束后再交d7独立后端聚焦复验（含不确定进程组/恢复路径及真实persist故障链），保持Go编译与嵌入资源写入串行。
- 后端2c/d R2现由必要上下文隔离后的同功能配置a18独立聚焦D/E通过；原d7重复旧R1未作新证据。F3真实DOM同钟测量闭合既定去重缺口，旧无效计时判定撤回；F5组合重登录失败仍保留，不以单例通过冲销。主控核对范围与证据，未新开W2探针/延长窗口。
- 回到剩余主线：原89继续显式安全文件下载后端、独占Go；原1f30消费已冻结Task2实现双合并UI及F5源/证据诊断，只TS源/纯单测/typecheck，不写dist/启动Go/browser直到89真实冻结。下载前端待真实契约后由同一1f30接入，不猜API，不重开已过后端范围。两执行端同Workspace且不重叠写入，单份必要源码基线存artifacts/remaining-actions-baseline.tar.gz，未提交Git基线。
- 最新：双动作/下载UI已本地实现，下载Go R1已独立通过；569§15功能/质量核查发现夹具rate/ready两真缺陷，M2真实UI远端联调按授权边界记未验。主控仅四测试相关文件修复：真实401延迟0/250/400红→绿，正式`npm run test:chat-turn`自动全部19例串行小批fresh服务绿，保留10/min及8驻留，不升timeout/跳例/改生产认证。原F3产品已闭合，正式夹具仅采用既有可信DOM测量法，不改700ms。16源集合eb8e4902，569实际6+1+1窄验全绿、M1关闭、spec与quality分别通过，主控已核实际增量与日志；独立视觉受阻、已一次补问只读图像验收授权，未Git/7331/真模型/GitHub/CI。完整独立验收及部署仍未完成；详细hash/失败历史/角色用量见既有TODO及单份artifacts报告，不复制全量历史。
