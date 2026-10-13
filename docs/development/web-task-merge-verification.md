# 卡片 worktree 与主分支集成验证（2026-10-12）

用户已批准 [设计](../superpowers/specs/2026-10-12-task-merge-design.md)、[实施计划](../superpowers/plans/2026-10-12-task-merge.md) 和当前 Agent 串行实现＋独立只读验收。当前仓库仍是未提交的 main，没有为隔离工作创建基线提交；本批未提交／合并／推送开悟仓库，未调用真实模型。测试中的 Git 副作用仅在临时仓库。

## 实际实现

- 自动识别并持久保存本地主分支：本地 origin/HEAD 对应分支→main→master→非任务当前分支；新建仓库为 main。高级覆盖与提交身份可选；自动识别失败时仍可手动指定已有本地分支。新任务从目标主分支创建 worktree，已有任务基线／目录不变。
- 卡片与任务聊天显示未开始／无改动／待提交／未合并／已合并／需核查，执行角标仍独立。依据当前文件状态及任务 HEAD 是否可从主分支到达；外部 Git 与产品入口使用同一判定。合并后再修改／提交、主分支外部重置均恢复待处理；部分 cherry-pick／squash 不标完整已合并。
- 卡片／任务聊天合并图标和明确短语进入同一审核框。确认展示目标、SHA、路径、身份与说明，有修改则显式提交并合并。原始文件名用 NUL 解析、Vue 文本节点显示，无源码／diff；默认身份缺失时展示 `Kaiwu <kaiwu@localhost>`，不改全局配置。
- 服务端审核绑定两个 HEAD、内容、索引、文件模式、身份及设置；锁内重算，暂存后复核实际 blob／mode／范围。普通 Git 合并优先快进，分叉先 merge-tree 检查冲突，再复核主目录；保留分支／目录，不自动 push／stash／reset／abort／选择冲突版本。
- 同 canonical Git common dir 的项目集成锁及任务 OS 租约；先可靠结束空闲 pi 和消费协程，再接管工作区，保留原 session。目标任务执行中不强制取消，其他卡片独立开发。每次 Git 启动前持久保存未知窗口，再记录实际进程组；异常归属阻止后续写入。
- SQLite 短事务登记合并意图／回执，服务器生命周期持有操作；HTTP／WS 断开不取消。相同 ID 返回原操作，不同参数冲突；重启标需核查，取得项目→任务租约后只核对 SHA，不重放。提交成功而合并失败保留任务 SHA 和成果。
- 活动项目 3 秒检查，进入／重新可见／任务结束／手动刷新立即核对；隐藏／退出／卸载停止浏览器循环。服务最多 2 个检查工作单元，每项目最多 1 个刷新，8 秒检查超时、Git 单命令 20 秒上限；不变结果不写库。结构命令使用可选 structureRevision，兼容旧客户端全局 revision。
- 私密路径包括引入提交历史中后来删除的文件；额外保护实际配置的服务数据根目录及项目相对路径映射的 worktree 副本。外部 filter／merge driver／分支 mergeOptions、子模块和特殊文件阻止产品集成；禁 hooks、交互签名、签名验证、外部 diff／textconv，不使用 shell 执行 Git。

## 实际测试

Go 命令均设置 `GOTOOLCHAIN=local GOCACHE=/tmp/kaiwu-go-cache GOMODCACHE=/tmp/kaiwu-go-mod-cache`；隔离 HTTP／WebSocket／浏览器测试需要本机套接字权限。沙箱拒绝套接字时未当成产品故障，授权后执行。

| 检查 | 最终结果 |
| --- | --- |
| `go -C server test -json ./...` | 151 项顶层测试通过，5 项助手入口预期 Skip，0 失败 |
| `go -C server test -race ./...` | 全部通过 |
| `go -C server vet ./...`、相关目录 `gofmt -l` | 通过，无格式输出 |
| `npm --prefix apps/web test` | 6/6 通过 |
| `npm --prefix apps/web run build` | TypeScript／Vite 通过，Go embed 资源已复制 |
| `npm --prefix apps/web run test:browser` | Chromium 18/18，真实 Go＋临时 Git＋假 pi，未调用模型 |
| Windows amd64／macOS arm64 `go build` | 编译通过；不代表原生 UI 或这些平台的 Git 写入运行验收 |
| Linux Go 生产构建 | `server/bin/kaiwu-server-task-merge`；运行烟测另记下节 |

新增测试先观察缺少接口／功能失败再实现。回归中实际复现并修复了后台 Git 检查使看板表单过期、内容不变状态不变时审核遗漏、已提交私密路径、暂存后未经审核变化、过期配置、晚预检跨聊天范围、丢响应且回执暂时不可查时生成新 ID 等问题。夹具修正与产品修正分别记录，没有用修正测试断言掩盖产品错误。

| 设计验收场景 | 自动化证据与范围 |
| --- | --- |
| 默认主分支、高级覆盖、冻结基线 | `TestDetectMain`、`TestProjectGitSettingsOptionalAndValidated`、`TestMergeTaskBranchesRemainIsolatedAndMainBaseFrozen`；默认 main、覆盖有效性／清空／自动识别失败后的手动恢复，设置不重建任务 |
| 两卡独立及串行合并 | `TestMergeDivergenceAndConcurrentCards`：第一卡合并后第二卡旧审核失效，重新审核后普通双亲合并，两个成果与目录保留；公共目录锁单元检查 |
| 未提交／新文件／暂存／私密路径 | gitops 内容指纹、实际 staged blob＋mode 回归；空格／换行／前导 -；中间提交的已删除敏感文件、自定义服务目录及历史副本均阻止 |
| 无改动、空仓库、种子文件 | 状态初始 no_changes；`TestFirstMergeIntoUnbornMain`、`TestUnbornMainAcceptsAlreadyCommittedTask`；种子文件保留并阻止覆盖 |
| 快进／普通分叉／冲突 | EndToEnd、ConcurrentCards、FailurePreservesPartialResult：真实文件与 SHA／双亲提交断言，冲突留下任务提交，重新审核不重复提交 |
| 按钮／聊天／外部 Git | 浏览器真实审核、取消无副作用、确认主目录出现成果；Go 实际外部 merge 更新 ancestry；明确短语与否定／问句／引用／多行分开 |
| 部分成果及合并后新增 | PartialCherryPickAndSquashStayUnmerged、GitStatusTracksActualHistory、浏览器合并后新文件 3 秒周期变待提交并刷新恢复 |
| 脏主目录、异常归属 | UnsafeMainAndStalePreview、ChangedOwnership 与已有 worktree 校验回归；错误拒绝写入、原文件保留 |
| 空闲／忙碌／遗留写入者 | BusyWriter、PreservesSession、原有 host-kill worktree 回归、项目未知／活进程 marker 拒绝；恢复检测活 marker 保持 needs_check |
| 重复、丢响应、过期 | Workbench 回执／Restart、旧指纹拒绝、浏览器相同 operation ID 重试；不自动重新确认另一批修改 |
| HTTP 生命周期、重启 | MergeOutlivesRequestAndStoreClosesLast、IdempotencyAcrossRestart；应用关闭先等待 Manager 再关 Store，恢复不执行 Git |
| UI／聊天回归 | 原 15 场景＋3 合并场景；范围草稿、键盘、窄屏、Markdown／复制／预览、WS／登录恢复继续通过 |

自动化覆盖重点行为，不宣称每个文件系统故障、每一条进程指令崩溃窗口或所有 Git 配置组合都做了注入测试；大量卡片／慢文件系统压力及 Firefox／Safari 仍未独立验证。

## 独立只读审查与修复

依据实施计划和 requesting-code-review 技能安排单个新上下文只读审查；Paseo 无 profiles 且本地 daemon 不可用，使用可用协作工具的明确 `gpt-6-astra` 元数据选型。审查未修改主目录、操作真实项目或调用模型。原始评级为 6 Important、1 Minor、无 Critical；最后一项按可能误杀其他进程的实际影响提升为 Important，一并修复：

1. 连续 Git 启动窗口沿用旧 PGID：每次 attempt 持久重置 Group=0，记录失败不启动；永久回归先失败后通过。
2. 敏感文件提交后删除绕过净 diff：检查全部引入提交路径；永久回归和审查 overlay 回归通过。
3. 审核后只改暂存执行位仍提交：审核存储 Modes，复核真实索引模式；审查回归通过。
4. Git 签名验证可执行外部 GPG、分支选项改变合并：禁验证签名及日志签名展示，实际 merge 显式 no-verify-signatures，拒绝 branch.mergeOptions；哨兵不执行。
5. 自定义服务数据名称漏防：规范化实际 DataDir，保护工作区内根路径及项目相对副本／历史；两个永久回归通过。
6. 恢复仅按 SHA 接受尚存活的旧写入者：按项目→任务核实租约，活／未知 marker 不补成功；永久回归通过。
7. 原 Minor、提升为 Important：回收 leader 后按数字 PGID 清理有复用竞争。改为 Linux Waitid(WNOWAIT) 保留 leader 身份至组清理结束，取消回调串行且只调用一次，再 Cmd.Wait；确定性测试核对清理时 leader 仍为 zombie、晚清理被拒绝。未实际制造 PID 复用竞争。

审查者独立复验了前四项修正；后两项及提升项由当前 Agent 补永久回归与完整 Go／race／浏览器复验。未安排第二次独立审查，不把主 Agent 复验称为二次独立验收。

## 实现取舍记录

1. 经授权直接在 unborn main 串行实现、不提交基线；保留文件级实施台账，代价是没有提交级回滚／审查范围。
2. 新增可选 structureRevision，将后台观察与结构编辑版本分开，旧客户端仍可用全局 revision；后续客户端须遵循新字段语义。
3. 预检增加 blob／mode 证明，提交阶段复核实际暂存范围；隐式换行等内容转换可能被阻止，需要先手动提交再审核合并，不默许审核外字节。
4. Git 检查／预检接受可选服务根路径，Manager 必须传实际 DataDir；增加 canonical 路径检查成本，避免只依靠目录名称。
5. 已固定的 `golang.org/x/sys v0.37.0` 改为直接依赖用于 Linux Waitid，没有新增版本／安装；其他系统仍为只读检查与编译验证。
6. 计划中的场景分组落在多个单元／集成／浏览器测试；不以相同函数名代替实际断言，不宣称完整崩溃矩阵已验证。
7. 独立审查未重新运行浏览器／模型／平台／压力／完整崩溃矩阵；采用当前 Agent 的 Chromium 18 场景和 Go／race 作为当前 Linux 预览证据，保持模型未配置、非 Linux 写入不可用。代价是其他浏览器、平台及极端负载／时序仍须另行验收。

## 限制与保留范围

“已合并”仅证明当前完整任务提交进入本地主分支历史，不证明测试／功能验收、已 push 或后续没有 revert。外部 squash／rebase 需要人工判断；产品不能凭空 diff 假装完整合并。服务锁不能排除用户从外部同时操作 Git，也不是系统沙箱；普通 pi bash 的通用授权桥未实现。

写入执行／集成限 Linux。冲突、无共同历史、未诞生主目录仍有种子文件、特殊文件／子模块、外部过滤器或内容转换等保留成果并提示人工处理，不自动改全局配置、清 Git 锁或删除工作区。真实 pi 模型、自动主协调、PC 三平台原生 UI、Android 和发布流水线未验收；团队协作与通用可配置编排引擎保留未来方向。

## 固定端口部署与实际烟测

核对旧 PID／二进制归属后优雅停止 `kaiwu-server-markdown`，以 `server/bin/kaiwu-server-task-merge` 启动，固定 `http://192.168.2.122:7331`，健康检查 200。沿用 `.kaiwu/workbench.db`、访问令牌、登录凭证及其他原 KAIWU 配置；PID 文件已更新。

实际 Chromium 烟测使用重启前的 Cookie，无需重新登录：快照中的原项目、任务、专题和对话保持；Git／merge 能力可用，模型仍未配置。打开／取消高级设置、手动刷新、页面刷新后登录、WebSocket 初始 revision 和无脚本错误均通过。真实服务合并记录仍为空，没有测试提交／合并或发送模型请求；私有临时 Cookie／数据核对文件已删除，未打印令牌或保存真实对话截图。
