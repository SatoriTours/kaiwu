# 托管项目、AI 配置与运行资源开发计划

> 执行方式：获批后由当前 Codex 实现负责人连续实现、自测和修复，使用 executing-plans；按完整切片安排独立验收，不按文件派发。本文仅为计划，不授权代码实现、目录迁移、提交、部署或安装虚拟化工具。

**目标：** 用户通过名称管理项目，由 kaiwu 管理代码、配置、会话和资源位置，并逐步建立真正的项目执行隔离。

**架构：** 保留 Go 服务、SQLite、Vue 3 和当前 pi 接入。引入项目路径解析、配置解析和资源归属模块；新项目采用托管布局，旧项目沿用原路径。沙箱与测试环境分阶段接入，未具备隔离能力时不得标记为已隔离。

**技术栈：** Go、SQLite、Vue 3／TypeScript／Vite、现有 pi RPC；不新增自有 JS 后端或 pi 扩展。

**设计依据：** 本文件下方“设计基线”固化本次对话；此前的五点总体方向已获确认，具体字段和阶段拆分属于本计划建议，待评审。现有背景见 `docs/architecture.md`、`docs/product-requirements.md`。

## 设计基线与授权边界

已确认：隐藏创建项目时的文件夹概念；项目独立拥有代码、AI 配置、会话与成果；全局 AI 配置归 kaiwu，支持未来适配器；共享基础镜像、隔离可变实例；统一存储和清理。

建议采用项目容器结构，优于直接把项目根当作 Git 根：工作区、会话不进入代码仓库。相比把所有项目私有数据继续放全局目录，更便于按项目统计、备份和迁移。

```text
~/projects/<创建时生成的稳定目录名>/
  repo/                    主仓库
  worktrees/<task-id>/      任务工作区
  .kaiwu/
    identity.json          项目 ID 和布局版本
    config/                项目规则、方案、技能及本机凭据引用
    snapshots/             任务配置快照
    sessions/              原生会话及隔离的适配器运行目录
    artifacts/             成果、附件和验收证据
    logs/                  限期日志
    tmp/                   可回收临时文件
```

本计划采用最近一次细化中的外层 `.kaiwu/config` 作为项目配置唯一来源，取代早先两个 `.kaiwu` 同时管理配置的提案。可分享规则后续显式导出到仓库，不自动同步两套配置。

全局配置建议放 `~/.config/kaiwu`；全局环境镜像／实例放 `~/.local/share/kaiwu/environments`；缓存放 `~/.cache/kaiwu`。现有 `KAIWU_DATA_DIR` 和 SQLite 位置保持兼容，不能在升级时静默迁移。以上 `~` 均指服务器运行账户。新路径通过配置模块集中解析，允许管理员覆盖根目录。

项目 ID 永久稳定；显示名可改；目录名创建后固定。目录名允许安全 Unicode 字符，不依赖拼音服务；排除路径分隔符、控制字符、点路径和平台保留名，碰撞附加短 ID。已有目录不接管、不覆盖。

普通配置继承：内置默认 → 全局方案 → 项目覆盖 → 任务冻结快照 → 允许的会话调整。凭据仅引用；权限取已授权范围内的收紧结果。适配器类型与模型提供商分别建模。文件为配置真源，SQLite 存引用、版本和任务快照，不能存在两个可独立编辑的真源。

## 全局约束

- 当前只实现 pi；其他 Agent 仅保留模型边界，不预建通用插件框架。
- 延续原任务会话、独立 worktree、同工作区单写入者、主分支串行集成和原恢复校验。
- 切项目、关闭浏览器不停止进程；无新意图不重放旧指令。
- 不自动提交、克隆、推送、迁移旧目录或安装全局工具。
- 真实模型测试须先说明消耗；协议用假 pi，系统隔离用真实本地进程验证。
- 主目录存在其他工作成果；实施前核对最新 diff，只修改获准相关部分，不清理未知文件。
- Web 优先，PC 三平台及 Android 目标保留；不扩展 IDE。
- 当前计划不改变尚未验收的自动交付 worker 状态。

## 未决事项与执行门禁

1. **新项目开放时机：** 建议与沙箱一同开放，旧项目兼容运行；用户尚未回答该建议。阶段 A/B 可以独立实现和测试，但生产开放方式须确认。
2. **沙箱后端：** 容器／系统沙箱具体选择须只读核查实际部署主机能力后形成技术裁决；不以目录隔离代替。缺能力时显式阻塞，不静默回落宿主执行。
3. **测试环境：** Android 和 Windows 的运行后端、镜像来源、硬件能力与授权需专项确认；阶段 D 先交付资源模型，不能据此宣称真实虚拟机接入完成。
4. **配置／凭据：** 继续兼容已授权的服务器 pi 登录来源；新增凭据存储方式和导入操作需专项设计，不能复制整个个人配置目录。

## 当前代码落点

- `server/internal/workbench/types.go`：当前 Project 有 ID、Name、Path、ConversationID；Profile 含 Provider、Model、Thinking、CredentialSource。
- `server/internal/workbench/migration.go`、`store.go`：持久模型和状态操作。
- `server/internal/config/workbench.go`：KAIWU_DATA_DIR 和 pi 配置来源。
- `server/internal/webapi/project.go`、`handler.go`、`socket.go`：路径校验与命令入口。
- `server/internal/agent/launch.go`、`workspace.go`：当前会话和工作区依赖全局 DataDir。
- `server/internal/projecttools`、`preview`、`gitops`：终端／附件／预览／Git 的路径和权限消费者。
- `apps/web/src/ProjectMenu.vue`、`App.vue`、`types.ts`：项目入口和协议模型。

## 审查重点

1. 并发同名创建、断电和重试：只生成一个可恢复的项目，绝不覆盖已有目录（A）。
2. 符号链接替换及路径穿越：校验与实际打开一致，不能越过托管根（A/C）。
3. 旧项目恢复与改名：原 session、worktree、消息、登录和待确认结果不丢失（A/B）。
4. 恶意项目配置：不能扩大权限、加载未授权扩展或得到其他项目凭据（B/C）。
5. 清理与运行并发：在用会话、环境、任务成果不被回收（D/E）。

## 阶段 A：托管项目与旧项目兼容

**文件：** 新增 `server/internal/projectstorage/layout.go`、`layout_test.go`、`create.go`、`create_test.go`；修改上述 config、workbench、webapi 和 ProjectMenu 文件；新增 `apps/web/tests/managed-projects.browser.spec.ts`。

**接口建议：** `projectstorage.Layout{Root, Repo, Worktrees, Private string}`；`Resolve(root, directoryName string) (Layout, error)`；`Create(ctx context.Context, projectID, directoryName string) (Layout, error)`。Project 增加 `StorageMode`（legacy/managed）、`StorageRoot`、`DirectoryName`、`LayoutVersion`；原 Path 保持实际仓库位置，避免误把容器根传给 Git。

- [x] 先增加创建、同名并发、非法名称、符号链接、已有目录和重试失败测试，确认新增行为的断言先失败。
- [x] 扩展配置和数据库迁移：旧项目默认 legacy，原 Path 原样保留；升级不扫描／接管用户整个 projects 目录。
- [x] 创建流程持久记录 operationId 与项目 ID，独占预留目录、写身份、初始化 repo、完成数据库登记；跨文件系统／SQLite 的失败通过操作状态恢复，不假装原子事务。
- [x] 创建失败保留原意图与材料，不删除未知目录；刷新／重启后列出未完成创建，仅显式点击以原 operationId 重试。缺 identity／Git ready marker 时报告需核查，不接管未知目录。
- [x] 新增名称创建命令，沿用现有 socket 幂等回执；门禁内界面不显示必填路径。旧 API 留兼容，生产新建托管项目门禁仍关闭，不扩大路径授权。
- [x] 增加改名操作：只改 Name；验证运行中改名不变更任何 ID、路径和会话。
- [x] 浏览器验证创建、同名自动消歧、刷新后显式恢复、切项目迟到回执、桌面和手机布局；见文末本地限定验收范围。

**验收：** 无模型请求即可完成真实临时 Git 创建；旧项目仍可打开和接回；普通新建流程无路径输入。尚不宣称安全沙箱。

## 阶段 B：配置、会话及工作区归属

**文件：** 新增 `server/internal/projectstorage/resolve.go`、`resolve_test.go`，`server/internal/aiconfig/types.go`、`resolve.go`、`resolve_test.go`；修改 `agent/launch.go`、`workspace.go`、`manager.go`、workbench 模型；按实际路径消费者补齐 projecttools／preview／gitops。

**实际接口：** `ResolveProject(ref projectstorage.ProjectRef, legacyDataDir string) (Layout, error)`（由 `Project.Storage()` 转换，避免依赖循环） 统一 legacy/managed 分支；`aiconfig.Resolve(global, project Config) (Resolved, error)` 返回解析值、来源及版本散列。Config 含 schemaVersion、adapter、profile、rules、skills、credentialRef；Resolved 不包含凭据正文。具体权限类型复用现有授权模型，不并行建立宽松权限系统。

- [x] 增加继承、显式禁用、非法 schema、凭据字段拒绝及旧任务快照不变测试；行为红绿与初始编译红分开记录。
- [x] 文件配置原子写入并以版本散列拒绝并发覆盖；全局方案与项目覆盖有明确来源显示。
- [x] 任务创建冻结配置及技能正文；模型调整沿用原后续轮次机制，不回写模板、不打断当前请求。
- [x] managed 原生会话、任务工作区、附件、成果下载与服务侧材料按项目解析；legacy 仍解析旧位置，恢复、合并与预览／下载均接入。轮次／工具记录继续由全局 SQLite 按项目／会话 ID 索引，不搬迁数据库；日志／tmp 预留目录不等于新增日志持久化或清理功能。
- [x] 扫描 DataDir 路径拼接调用，消费者检查清单与命令证据记录在唯一外部 status.md；API 不采信前端路径。
- [x] fake pi 验证 A/B 配置、HOME 和会话不串用；原讨论切入 worktree、服务重启再恢复仍用同一原生身份；跨项目附件 ID 不能从另一布局读取。

**验收：** 旧项目恢复回归通过；新项目所有私有材料可按项目定位；配置更改不追溯已有任务，跨项目请求被拒绝。文件归属不等同 OS 隔离。

## 阶段 C：执行沙箱与资源限制

**门禁：** 先核查实际 `.91` 部署主机能力并评审具体后端方案；无授权不安装、不启动新宿主服务。以下是验收需求，后端确定后补充准确文件和操作级子计划再实现。

**建议落点：** 新增 `server/internal/executionenv`，由 agent、projecttools 终端和 preview 共用。输入为项目／任务 ID、挂载授权、环境白名单和资源预算，输出受管进程句柄及可验证的隔离模式。

- [ ] 定义明确的 managed-sandbox 与 legacy-host 模式；沙箱不可用时报错，禁止静默回落。
- [ ] 隔离可写工作区、HOME、临时目录及凭据；凭据按需提供，扩展和个人环境变量不自动继承。
- [ ] 主分支和共享 Git 管理元数据通过受控操作使用；验证 worktree 的共享元数据不会突破授权。
- [ ] 同步约束终端、预览和构建子进程，不能只约束模型工具入口。
- [ ] 加入全局驻留上限与按项目公平排队；CPU、内存、进程数、磁盘和网络限制按核验后的后端能力实施，不能只写配置不执行。
- [ ] 使用真实无害探针验证跨项目读写、宿主 HOME 读取、进程访问、符号链接逃逸、子进程回收及资源超限；保留失败证据。

**验收：** 两个真实并发项目中，A 无法访问 B 的私有数据或写入其工作区；切页面不停止任务；服务恢复核验资源归属。真实 pi 模型联调另行说明费用后执行。

## 阶段 D：测试环境和资源登记

**建议文件：** 新增 `server/internal/resources/types.go`、`store.go`、`store_test.go`，后端明确后新增 `server/internal/environments`。

资源记录至少包含 ID、ProjectID、TaskID、Kind、Location、State、Owner、Rebuildable、Retention、LeaseVersion。镜像可全局共享只读，实例及可写磁盘归项目；单实例默认独占租约。

- [ ] 先验证资源归属、租约竞争、进程重启后的占用核查与未知资源保留。
- [ ] 实现环境模板、镜像、实例分离；状态为 creating/ready/starting/running/stopping/stopped/failed/deleting，失败可查询、不虚报成功。
- [ ] 项目可申请实例、任务租用；不足时排队，不抢占其他任务实例；停止保留磁盘，删除单独授权。
- [ ] 针对 Android／Windows 分别补充后端子计划、硬件前提、镜像来源、授权和真实启动验收，不用 fake driver 代替真机能力结论。

**验收：** 固定数据验证资源模型；每种环境分别完成实际启动、连接、停止、重新启动及项目间修改隔离，分别报告通过或阻塞。

## 阶段 E：存储、归档和迁移

**建议文件：** 新增 `server/internal/projectstorage/usage.go`、`cleanup.go`、对应测试；新增 `apps/web/src/ProjectStorage.vue`；迁移命令在专项操作计划审核后增加。

- [ ] 存储统计区分代码／会话／成果／缓存／环境；保留“未知或统计中”，不以估算冒充精确值。
- [ ] 清理先生成带版本的候选清单，执行时重验租约和引用；新启动占用与删除互斥；默认仅回收可重建且无引用资源。
- [ ] 归档移出活跃列表并禁止新任务；有运行任务时提示处理，不隐式杀进程或删除数据。
- [ ] 删除展示实际资源范围并显式确认；只操作归属明确的资源，保留未知路径。
- [ ] 旧项目迁移单独授权：停写核验、备份、转移、修复 Git worktree 关联、恢复 session 路径、验证后切换登记；源数据保留至验收，不自动清理。
- [ ] 备份包包含 SQLite 中该项目记录、配置、会话和资源清单；恢复验证 ID 冲突、凭据引用、worktree 与原生会话一致性。

**验收：** 在用资源不能清理；迁移中断可恢复；迁移后原任务继续原上下文；单纯复制项目目录不被标为完整备份。

## 验证与交付顺序

1. 每个实现切片先写关键行为失败测试，再实现，运行受影响 Go 包与前端测试；不为文档本身增加程序测试。
2. 路径／并发重点运行 `timeout 180 go -C server test -race ./internal/projectstorage/... ./internal/aiconfig/... ./internal/agent/...`（新增包出现后执行）。
3. 前端执行 `timeout 180 npm --prefix apps/web test`、`timeout 180 npm --prefix apps/web run build`，新增浏览器用例接入现有真实 Go＋临时 SQLite 夹具。
4. 独立验收关键路径、必要回归及 UI 实际交互／视觉；原会话复核 diff 和证据。模型继承与验收能力不明确时报告阻塞，不换 provider。
5. 更新 README 与 roadmap 的真实进度；证据沿用单份台账，区分协议、系统沙箱、真实模型和虚拟机结果。
6. 仅收到明确提交／部署授权后执行相应全量门禁：Go 全量、前端、浏览器、Actions 覆盖及实际 `.91:7331` 核查；当前计划不触发这些操作。

## 计划状态

- [x] 已确认总体五点方向并形成开发计划文件。
- [x] 已核对当前项目、配置、会话和工作区的主要代码入口。
- [x] 2026-11-01 用户要求按本计划开始开发；A/B 开发获准，未决项的专项门禁仍保留。
- [x] 阶段 A 门禁内本地实现与独立功能／回归／视觉验收通过；生产开放、配置归属和 OS 隔离不在本结论内。
- [x] 阶段 B 配置、会话及工作区归属本地实现与独立限定验收；生产及 OS 隔离门禁不变。
- [ ] 阶段 C 后端裁决、子计划、实现与真实隔离验收。
- [ ] 阶段 D 各环境专项计划与验收。
- [ ] 阶段 E 生命周期、备份及获准迁移。

团队协作与通用可配置编排继续作为未来方向，本计划只保留项目身份和资源归属边界，不提前实现多租户或通用工作流。

## 执行记录（2026-11-01）

- 首轮完成阶段 A；后续按用户“继续”推进并完成 B，不把 A/B/C/D/E 一并标成完成。托管目录含 repo／worktrees／外层 .kaiwu，旧项目仅补 legacy 元数据、不移动文件。配置根新增 KAIWU_PROJECTS_ROOT（默认服务器账户 ~/projects）；KAIWU_DATA_DIR／SQLite 不迁移。
- SQLite schema 4 新增持久创建意图；文件系统完成屏障后才原子提交项目／会话／回执。目录名创建时附短 ID、显示名可改。失败不删除材料；无 Git ready 标记的中断初始化需人工核查，不假成功。
- 生产 ManagedProjects 门禁未接任何启用环境变量，实际服务仍关闭新入口；本地专用 Go＋临时 SQLite／Git 夹具验证名称创建。后续 B 已接入服务管理的配置／会话／附件路径消费者；不宣称进程已被 OS 隔离。
- 独立初审发现三个 Important：持久化屏障不足、失败重试丢失 operationId、交付／合并可抢 pending ID；均有红绿测试及修复。另补首项目快照自动选择回归和刷新后 pending 显式恢复。
- 最终 Go 全量通过；projectstorage／workbench／webapi／config 四包 race 通过；前端默认测试及生产构建通过；新增 4 项浏览器自测通过。独立 6 项旧入口／菜单／会话回归及 1 项真实 Git 失败后刷新恢复通过；桌面／手机四图视觉初验及 390px 恢复列表独立视觉补验通过。
- 证据、源码 hash 与最新结论统一保存在 `~/.local/state/agent-artifacts/kaiwu/2026-11-01-managed-projects/status.md`，含独立 review／acceptance、红绿及失败日志。既有回归早期重生成过项目菜单截图，后续使用独立证据路径，未将无关既有修改收编或提交。
- 未执行真实模型调用、断电实机／硬件存储实验、OS 沙箱或虚拟机验收，未提交、推送、部署到 .91:7331。测试服务已由既有生命周期退出。后续 B 结果见下；C 仍须 .91 主机能力核查、后端裁决及准确子计划，D/E 专项门禁不变。

### 阶段 B 本地限定验收

- 配置：`KAIWU_CONFIG_DIR` 默认服务器账户 `os.UserConfigDir()/kaiwu`，不移动 `DataDir`／SQLite。`ai.json` schema 1 是默认配置真源，内置→全局→项目覆盖，`[]` 显式禁用；SQLite 默认 Profile 仅为投影，旧任务不批量补写新快照。每次新建冻结解析值、来源／版本、规则和批准的单文件 `SKILL.md` 正文。未承诺冻结技能正文引用的外部脚本、网络资源或任意相对资源包；不自动导入个人技能／扩展目录。
- 安全：仅支持 pi 与空／`server_pi` 凭据引用，项目不得切换全局凭据提供商或增加全局未批准技能；managed CLI 不继承宿主 API Key。独立审查发现“空 provider + server_pi”可能复制全部凭据，已在 Resolve 与 launch 最前端拒绝，并以合成双提供商／双连接回归证明只复制所选一项，不改模型目录发现的全量枚举逻辑。
- 路径：统一解析器对 managed 核验 identity、Git ready 与非链接目录；session/HOME/TMP、任务 worktree、附件 API 与提示、工具 scope、Git ownership、preview target／成果下载都采用项目布局。Git 临时 index 在 Git 私有目录；轮次日志与结果元数据仍由全局 SQLite 按稳定身份索引。legacy 的原会话、工作区与附件位置不变。
- Web：项目菜单「AI 配置」支持全局／当前托管项目、模型覆盖、规则／技能启用禁用、来源／版本与 CAS 保存；只影响未来新任务／项目对话。Vue Proxy 直接 structuredClone 导致的 busy 卡死已复现并修复，构造异常亦进入 finally。
- 最终证据：`b-go-all-after-review.log` Go 全量通过；`b-race.log` 七包及修复后的 `b-race-after-review.log` 四包 race 通过；`b-vet.log`、默认前端测试／构建通过；新增浏览器 5/5、旧入口 6/6。独立关键路径及配置浏览器通过，390/1440 两图实际语义读图通过；`b-acceptance.md` 是保留的历史不通过，B-I1 修复后的最终结论为 `b-reacceptance.md` 限定通过。
- 不代表真实模型／断电实机／其他服务器 OS／沙箱／部署验收；终端、预览和交付子进程的宿主环境限制仍属 C。A/B 已完成，下一步须先核查 `.91` 能力并裁决 C 后端与子计划；不擅自安装、开放托管生产入口或部署。

### 阶段 C：`.91` 只读预检与待选方案

2026-11-01 用户同意继续能力核查与沙箱方案。**本轮只做预检／候选设计，不是 C 实现授权或验收通过**；没有安装、修改 sysctl／cgroup／配额、创建 systemd 单元、重启服务、读取凭据或运行真实模型。下一步涉及临时单元的实际验证需按下述范围确认。

#### 实测事实

- 目标是既有 `jimxl@192.168.2.91:22`，不是开发机 `.122`。Arch Linux、内核 `6.18.25-x64v3-xanmod1`，运行在外层容器中；可见 16 个 CPU、约 46.8 GiB 内存，这不是给开悟独占的资源承诺。
- 现有用户级 `kaiwu-server.service` 保持 active，预检前后 MainPID 均为 4734；服务 UID 1000、有效 capabilities 为 0、`NoNewPrivileges=yes`。当前 `MemoryMax=infinity`、CPU 配额无限、`Delegate=no`，不能把服务现有配置称为任务资源隔离。
- 已有 `unshare` 与 systemd 261。以 `setpriv --no-new-privs unshare` 运行有界无害进程，user／mount／PID／network／IPC／UTS 命名空间及私有 proc 创建成功，exit 0。仅证明该 SSH 上下文在相同 NNP 条件下有基础能力，不等于所有服务上下文或沙箱逃逸测试通过。
- 未找到 bubblewrap、Podman、Docker；Docker／Podman／containerd 服务 inactive，未连接 socket 触发激活。不得假设已具备 OCI 运行环境。
- cgroup v2：用户管理器有 cpu／memory／pids 控制器；当前 app.slice 只启用 memory／pids，现用服务下未提供 cpu.max。权限检查不能替代创建独立任务组、实际限额和超限回收试验，本轮没有写任何控制器。
- 当前文件系统为 Btrfs；目标未找到 btrfs CLI，硬磁盘配额状态／操作能力未证实。`RLIMIT_FSIZE`、I/O 限速或事后目录统计都不能冒充项目总写入硬上限；不得为补此项擅自启用整盘 quota／FUSE／特权服务。
- 初始 SSH PATH 未找到 node／pi，不代表服务缺少运行时。只对探针使用既有部署 PATH 后定位 Node `v22.23.3`、pi 包装入口、Go／Git 工具路径。Node 帮助含 `--use-env-proxy`，但未验证所有提供商 SDK 的代理兼容性；未执行 pi／个人扩展或读取 auth。
- 已安装 systemd.exec 文档明确：用户级服务的 RootDirectory 支持依赖用户命名空间，并隐式启用 PrivateUsers。因此原生 systemd 沙箱是有效待验证候选，不能因未安装 bubblewrap 就断言必须安装新运行时。RootImage 等另有 mountfsd 前提，本轮不采用也不启动。

#### 有限候选与建议（尚未选定）

1. **先验证现有 systemd 原生用户级沙箱（当前建议）。** Go 通过用户管理器建立独立短寿命任务单元，用私有 root／用户／进程／网络边界与 cgroup 预算约束整个进程树。优点是不先安装新运行时，复用既有服务管理器；代价是需验证本机用户级设置的组合、RPC／PTY 通道和每次执行的单元身份，不能把文档支持当作实际成功。初步通过后再形成完整实现子计划。
2. **bubblewrap＋systemd。** bubblewrap 负责明确的挂载／命名空间，systemd 负责资源组／回收；适合原生设置组合不满足时重新裁决。需要单独批准安装与版本选择，也一样没有自动获得硬磁盘预算。不会静默从方案 1 切到此方案。

Podman／Docker 需要更多镜像、映射、存储和外层容器前提，本阶段不优先；不从零手写 unshare 沙箱来节省一个依赖。这里将早期“倾向 bubblewrap”的建议修正为先验证原生 systemd，依据是后续取得的 261 本机文档，而非已获用户后端批准。

#### 两种候选都不能省略的边界

- 只挂载任务 worktree、此会话 HOME／tmp／凭据和批准的只读工具链／配置快照，保留原生会话与工作区的原路径身份；不挂载整个服务器 HOME、SQLite、其他项目或全局凭据目录。
- 项目主仓库、公共 Git 元数据及 `.git` 关联不整块可写暴露。首片应由可信后端受控完成提交／主分支操作；不靠 PATH 中的 git 包装脚本充当权限边界。具体只读 Git／任务索引策略需真实回归后写入实施计划。
- 网络默认隔离，不用 host network 绕过模型联网。模型及刷新地址通过服务器授权的有限出口，依赖下载有独立允许规则；DNS／IP／重定向重验，宿主／LAN 访问不因代理开启而自动授权。预览应走受控通道，不暴露沙箱端口，也不能照搬宿主 `/proc/net` 证明。
- pi、终端、预览和构建共用执行边界及预算，不保留其中某条 managed 宿主直跑路径。关闭网页不停止服务器任务。
- 单元／cgroup 归属使用持久执行 ID、InvocationID／实际资源标识核验，不仅使用可复用 PID；启动／停止／恢复有界，未知资源保留并阻塞重启写入，不杀其他任务。
- CPU／内存／进程／总写入／网络能力逐项实际验证。硬磁盘限制等必需能力未证实时拒绝 managed 执行，不把能力不足降为警告或静默回落。全局上限与项目公平队列是后续实现内容，本轮未创建调度器。

#### 历史提议：预检后的临时试验范围（已按下节执行）

建议先允许一次**有界、无模型的临时 systemd 测试单元**验证：只使用本任务临时材料，确认用户级私有 root／proc／网络、独立资源预算与完整退出；不修改／重启现用 kaiwu-server，不安装、不启用整盘配额、不开放生产。失败留证并报告，不反复尝试未知提权或切换后端。确认原生路线可行后补准确文件、操作级 C 子计划并评审；硬磁盘预算若仍缺能力，保持明确门禁另行确认存储前提。

证据沿用唯一外部 `status.md`：`c-host-inventory.log`、`c-kernel-probes.log`、`c-runtime-preconditions.log`、`c-systemd-manual.log`。`c-systemd-contract.log` 保留首次 Python 不在 SSH PATH 的读取失败，不伪装成内核不支持；随后使用已有 gzip 取得相关文档。此次仅能力与方案记录，无须重跑 A/B 产品测试，也不重新声明其实现已上线。

### 阶段 C：获准临时原生沙箱试验结果

用户回复 `ok` 批准上述有界、无模型临时单元。实际 `.91` 现场结果：**systemd 原生隔离基座的本次受控试验通过；C 产品实现／完整安全验收仍未完成。** 试验没有安装软件、启用磁盘 quota、读取个人凭据或重启 kaiwu-server。

- 临时静态 Go payload 只放系统临时目录，运行在 RootDirectory 中；只挂载自己的只读二进制及可写合成工作区。配置 PrivateUsers／PrivatePIDs／PrivateNetwork；实测 PID／网络 namespace、NNP 和清空 capabilities 生效，未单独读取 uid_map 验证用户映射。通过 stdin/stdout 完成合成一轮；这不是 pi RPC／PTY／真实模型兼容性验收。
- 合成 B／HOME canary 在沙箱外，宿主预先验证存在；沙箱不能读取对应绝对路径及工作区中的逃逸 symlink。授权工作区可以写入，root 非授权位置写入失败；不据此宣称所有逃逸手法或实际项目并发攻击已测。
- CPU：实读 `cpu.max=25000 100000`，`cpu.stat.nr_throttled=22`，不是只查看 unit 配置。
- 内存：实读 `memory.max=100663296`、swap=0；仅此单元内的合成超额分配子进程被杀，`memory.events.oom_kill=1`。进程／线程：`pids.max=32` 且 `pids.events.max=4`，有实际资源拒绝。
- 首轮唯一失败是探针从 `/proc/net/dev` 枚举网卡，该路径在 ProcSubset=pid 下不存在。失败日志保留；未放宽任何隔离／限额属性。只把网卡枚举改为 Go 的 netlink 接口，复验确认只有 lo、私有 PID／network namespace 均不同于宿主，payload 失败清单为空、六项 kernel 断言均通过，controller exit 0。ProcSubset 对非简单运行时仍有兼容性风险，产品接线必须另验，不能照抄为所有 CLI 的默认配置。
- 两轮均在主程序退出前启动 setsid 后代，随后整个所属 cgroup 消失；临时 fixture 删除。结束后两个唯一 unit 均 LoadState=not-found，目录不存在。现有服务的 MainPID 4734、InvocationID `f46971e9ded5467694a1ebff4a1e7bf0`、active 及无限 CPU／内存配置保持，未停止其他资源。

证据：`c-transient-run.log` 为历史首轮失败；`c-transient-recheck.log` 为最终成功；`c-transient-cleanup-check.log` 为退出复核；payload／controller 的 `.txt` 源码及内容 hash 均存同一外部证据目录。一次普通探针修复后停止扩展，未新增常驻监控或全仓测试。独立证据核对与本轮最终状态见唯一 `status.md`，不将源码阅读冒称再次现场执行。

**下一步建议：** 以原生 systemd 作为 C 后端方案基础，再评审准确的产品接线子计划。硬磁盘总写入限额仍未证实；pi 工具链／配置路径、Git 公共元数据保护、受控模型出口、终端／预览／构建、持久归属恢复及公平队列尚未接入。两者缺口都不得靠 host fallback 或提前开启 managed 入口绕过。本次 `ok` 不授权产品代码实现、安装或部署；C/D/E 清单不提前勾选。

### 阶段 C：原生路线子计划与首批基础代码

用户在临时基座试验及独立证据核对后要求“继续”。本轮沿原生 systemd 路线细化为 [`2026-11-01-managed-sandbox-systemd.md`](2026-11-01-managed-sandbox-systemd.md)，由原实现负责人后续连续实现／自测、独立验收，不切碎派发。

子计划覆盖 C 的统一进程边界、Go 持久控制桥与重连、Git 公共元数据保护、单提供商凭据、终端／预览／构建、预算／队列及两项目验收，已通过独立设计修订检查。用户随后“你继续完善”后，首批纯准入／固定 bridge 参数／只读 sandbox.json Go 基础库及行为测试已实现，自测通过、基础层独立修复复验通过；尚未接入 Agent／终端／preview，不代表实际执行已经隔离。磁盘只读专项未取得项目级硬总额证据；TLS代理／下载出口尚未实现或启用。无新现场单元、真实模型、安装、提交或部署，C 清单继续不勾。所有历史失败、代码 hash、自测与独立结果仍索引在同一外部 status.md。

#### C 后续：持久归属与桥协议基础切片

用户“继续”后，已新增 `executionenv/ledger.go`、`workbench/runtime.go` 及 `execbridge/`，运行账本 schema 4→5；真实临时 SQLite 验证多实例单写者、CAS、unknown 占位、owner 删除／重绑拒绝与 commit-intent 先于唯一 wire send。ACK 丢失及发送前崩溃恢复只查询，不重放执行／prompt。双 nonce Ed25519 握手、Linux SO_PEERPIDFD／SCM_RIGHTS、sealed memfd／既有 flock OFD 传递有本机协议／内核测试。

独立初审 `c-runtime-review.md` 发现唯一 Important R-I1（5 秒 socket 预算未覆盖 context-aware peer 检查），真实红测已保留；两个握手入口改为共享 5 秒 child context，socket、Pin、两次 Inspect 同预算，无超时 goroutine。修后全量 Go（未变 cache）、三包 race（显式本机 peer probe）及 vet 通过，原外部 overlay 作者复跑通过；原审查者已独立重跑原 overlay 并补共享 deadline／第二次 Inspect 取消／成功 channel 生命周期探针，race/vet 通过，R-I1 关闭；本轮持久元数据／协议／宿主事务限定通过。

当前只是元数据／协议／宿主事务基础，不是完整 C。真实 unit Inspector／Start、私钥与 bootstrap 耐久管理、桥端执行接收循环、spool／RPC 重接、sealed config 业务解码／namespace attestation／反向 FD 交接仍待做，其他消费者／Git／出口／队列及真实双项目验收未完；磁盘 hard quota 仍 unknown。C/D/E 不勾选，未启现场 unit、模型或生产部署。后续细节及唯一证据索引仍见 [C 子计划](2026-11-01-managed-sandbox-systemd.md)。

#### C 桥端执行与重接的本地切片

继续补齐 `execbridge` 的 Prepare/真实一次 child exec/有界 spool/wire Remote/原 lease-spool OFD 反向交接，Go 全量/race/vet 自测及独立本地切片验收通过（并发、故障、FD 和生命周期探针均通过，无已复现 Important/Critical）。真实 SQLite＋宿主 helper 骤退＋task helper 组合验证原任务/租约/会话归属继续，重复启动/输入、旧 epoch 和缺口成功均拒绝；能力/配额/unit observation 明确为 synthetic，不替代 OS 沙箱证据。生产 gate、quota unknown 和 CLI/native/完整 RPC/其余 C 门禁均保留；详见 C 子计划最新切片与唯一 status.md，C/D/E 不勾。

#### C 只读 unit ownership Inspector 的本地切片

新增严格只读 systemd show＋Linux proc/cgroup2/pidfd 归属 Inspector 和 peer 适配，作者全量/race/vet 及独立关键／故障探针通过；本地 ownership 切片限定通过，无已复现 Important/Critical。未知、不一致或失踪不推断 gone，磁盘观测保持 unknown，不冒充沙箱准入；真实 unit 完整组合和 native Start/Attestor/CLI／其余 C 仍缺，生产门禁不变，C/D/E 不勾。见 C 子计划最新切片。

#### C 持久启动材料与 host 回接的本地切片

新增 per-run Ed25519 私有 key／只读公开 bootstrap 的耐久发布、稳定目录 inode 的 host socket 回接及早期 hardening API；实际 host helper 骤退后相同 key/inode 和真实 Unix 鉴权恢复，初审发现 NNP per-thread 缺口（B-I1，不通过），已改为验证可信 pre-exec NNP 继承前提；修后 Go 全量/race/vet 及独立新合同正反向窄复验通过、B-I1 关闭，本地切片限定通过。不是原生 CLI/Start 完成；完整 RPC/NativeAttestor/其余 C 与管理员硬配额、现场集成仍未满足，C/D/E 和生产 gate 保持未通过。详见子计划最新切片及剩余门禁。

C 后续补桥侧公共 bootstrap 安全读取（`execbridge/bootstrap_*.go`），本地自测与独立窄审查通过，无已复现 Important/Critical；尚未完成 CLI/Start/NativeAttestor/完整 RPC，不改变生产与硬配额门禁。

C 后续已串起鉴权后的 PREPARE／FD／attestation 回执，真实临时 SQLite＋Unix／SCM＋helper 组合自测与独立限定验收通过。只有显式耐久 commit-intent 后才执行，回执丢失保留 Session/lease；不是 main/CLI/Start/NativeAttestor 或实际沙箱完成，C 与生产 gate 不变。

C 后续 BridgeRuntime 已承担跨控制连接的 Session/lease 所有权，并在 PREPARED 回执丢失时保留非 nil Session，重接仅查询；真实Unix/helper生命周期自测与独立限定验收通过。main/CLI/Start/NativeAttestor/完整RPC和其它C门禁仍未完成。
