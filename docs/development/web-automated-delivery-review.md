# 自动交付后端独立只读审查

审查对象：`4829749 → 1c76c0a` 的后端，以及审查时 `server/` 的未提交修复和新增测试。以 2026-10-17 自动交付规格为权威，对照实施计划与历史裁决。以下行号均指本轮工作目录实际文件。

## 具体优点

- `workbench/store.go:164–213,295–312` 将周期、消息、轮次、投影和 operation 回执放在同一事务；schema 的 active-task 唯一索引确实约束同任务单活动周期，不是仅靠前端防重复。
- 正常自动路径的 FIFO 同时约束 canonical common dir 和远程仓库目标；`delivery_queue.go:109–126` 的前序票据查询不因失败状态而释放隔离。主 CI 失败不会直接放行下一张自动交付卡片。
- `RunChecks` 使用 argv、受限环境、超时、进程组回收和有界输出，并比较检查前后快照；候选指纹包含 HEAD、索引、内容及模式。已有内容／暂存／mode／symlink 的前置变化测试有具体断言，不只是检查命令退出零。
- 推送使用完整 SHA→单分支 refspec，不强推；任务／主推送先持久 intent。`recovery.go:145–178` 用远程精确 ref 区分“已到目标／仍为基线／其他写入者”，无法证明时保留隔离。这条主要 push 回执崩溃路径方向正确。
- Actions 客户端是只读窄接口；逐条过滤 SHA、分支和 push event，按 attempt 获取 jobs；缺失、skipped、neutral 不冒充成功。迟到旧 SHA 的观察还受到 Store 当前候选校验。
- 自动修复使用原任务 conversation，事务性扣共享次数；技术成功未调用旧的 `ConfirmResult`，没有伪造人工功能认可。
- 本轮修复中，handler 按 receipt.DeliveryID 交由 worker 派发、新任务允许先派发再准备 worktree、集成使用 `project_task`、普通 send/resume 的活动周期门禁、Socket policy 查询和 lowerCamel preflight 均能在实际实现及新增测试中对应到。下文也列出其中尚未覆盖的相邻路径。
- 新 webfixture 确实使用真实 Store／agent.Manager／delivery／webapi 和临时 bare refs，并核对实际推送数量；没有通过给产品状态直接写“成功”来制造闭环。但它绕过生产凭据 transport，五场景的本地检查均为 `true`，不能覆盖下列生产协议、本地失败及恢复窗口。

## 问题

### Critical

#### C1．真实 pi 握手的 model 类型被错误改成字符串，连未开启自动交付的对话也会失败

- **位置**：`server/internal/agent/manager.go:178–190`；对照当前安装的 `@earendil-works/pi-coding-agent/dist/modes/rpc/rpc-types.d.ts:148–161`、`rpc-mode.js:345–359`。
- **可复现输入／推演**：真实 `get_state.data` 含 `"model":{"id":"某模型","provider":"某提供商",...}`，类型声明为 `Model<any>`。新增 `Model string` 会使 `json.Unmarshal` 返回类型错误，进入“原会话文件或握手不匹配”，还没到 prompt 就停止。此解码不受 PendingProfile 条件限制，影响所有启动／恢复。
- **为什么现有验证漏掉**：`agent/manager_test.go:32–35` 的 fake 返回字符串 model 和顶层 provider；webfixture fake 又不提供真实 model 对象，双方均未模拟实际协议。
- **最小修复**：按真实嵌套 model.id／model.provider 解码；用该结构校验实际模型，保留缺失／未知的明确语义。新增直接喂真实结构的无模型契约测试，并覆盖普通启动、恢复和下一轮改模型。

#### C2．冻结远程及凭据隔离没有接入生产执行边界：rewrite 能改变目的地，helper 可得到 API token

- **位置**：`server/internal/delivery/manager.go:361–368`；`server/internal/gitops/remote.go:33–115,162–202`；`server/internal/gitops/command.go:16–27`。
- **可复现输入／推演**：delivery 直接从 policy 构造 target，没有调用 `ResolveRemote`；RemoteRunner 执行时也不调用 `verifyRemoteConfig`。仓库配置 `url.https://unapproved.invalid/.insteadOf=https://github.com/` 后，显式冻结 URL 仍会被 Git 改写。与此同时 `productionRun` 使用的 `Env()` 只排除 `GIT_*`，保留 `KAIWU_GITHUB_API_TOKEN`、`KAIWU_GITHUB_GIT_TOKEN` 等全部服务环境；仓库的 `credential.helper` 也没有清空／拒绝。
- **独立实验**：在 `/`、禁用 system/global config、只使用命令行合成配置，执行无网络的 `git ls-remote --get-url`，得到 `https://unapproved.invalid/owner/repo.git`；使用仅输出合成值的内存 credential helper，观察到 `API_TOKEN_IN_HELPER=SYNTHETIC_ONLY`。未读取任何真实凭据或访问网络。Args 的当前安全覆盖项不阻止这两件事。
- **影响**：授权目的地约束失效；本来只应给 REST 的 API secret，以及其他服务 secret，会进入可执行 helper 的环境。凭据包的 allowlist 单测没有验证实际 `cmd.Env`。这不需要把“不提供 OS 沙箱”的已知限制解释成凭据继承许可。
- **最小修复**：独立构造远程命令白名单环境；在实际执行入口强制目标／本地配置验证，禁用非受控 helper、rewrite、remote helper 等入口，且仅装入本次受控认证桥。推送 ref 也应在这个入口绑定 target 范围。注意：显式 URL 的 push 本身不按 remote 名称选择 pushurl；本项不声称 pushurl 会直接取代它，但规定的 pushurl 配置拒绝目前同样没有接到运行路径。

#### C3．没有冻结、比较仓库内 workflow／检查定义，AI 能在同周期削弱门禁后交付

- **位置**：`server/internal/delivery/stages.go:79–85,135–159`；`server/internal/delivery/policy.go:11–31`；`server/internal/gitops/candidate.go:127–147`；`server/internal/workbench/delivery_types.go:57`。
- **可复现输入／推演**：保持 policy argv 和 workflow IDs 不变，把 `.github/workflows/ci.yml` 内失败步骤改成 `run: true`，或将已配置的 `check.sh` 改为直接成功。`gateCode` 只比较 policy JSON，Candidate 的 protected scope 只是私密路径列表；没有周期初始定义内容指纹。`Delivery.ProtectedFingerprint` 也没有生产赋值／校验点。以上文件能正常进入提交及推送，原 workflow ID 的绿色结果仍被接受。
- **影响**：绕过本批最关键的“修复不能降低检查／授权”边界；repair prompt 的禁止文字不是机械门禁。`repair_test.go` 的 policy 变更测试不覆盖仓库文件变化。
- **最小修复**：周期启动冻结 workflow 内容及明确受保护检查定义；每次接受修复、提交和推送前复核，变化转需要审核，不在原周期自动采用新定义。先覆盖实际 workflow 文件和明确检查脚本，不必声称能证明任意测试语义未削弱。

#### C4．干净且已有提交的候选绕过私密历史扫描，可自动上传已经提交后又删除的 secret

- **位置**：`server/internal/gitops/candidate.go:127–147`；`server/internal/delivery/stages.go:155–166`；对照 `server/internal/gitops/status.go:198–226`。
- **可复现输入／推演**：任务分支外部产生两个提交：加入 `.env`／私钥，再将其删除；最终工作区干净。`CaptureCandidate` 仅在 `len(files)>0` 时运行 `committedScope`，因此不检查这些提交历史；stageLocalChecking 又在零脏文件时直接采用 HEAD，随后普通 push 会上传包含 secret 的可达历史。
- **影响**：自动交付比旧手动合并少了一层已有私密历史保护，最终 tree 不含 secret 也无法阻止泄漏。另外 `privateScope(p,x,nil)` 未传入 Config.DataDir，未复用旧路径对自定义服务数据目录的保护。
- **最小修复**：无论工作区是否脏，都检查 base→HEAD 引入历史的私密范围；传入实际服务数据根进行映射保护。新增“clean HEAD 含私密历史、最终已删除”和“非默认 DataDir”测试，拒绝发生在任何远程上传之前。

### Important

#### I1．生产 HTTPS 认证桥未注册且协议格式不匹配；SSH 的两种认可形式都不能走通

- **位置**：`server/internal/githubauth/askpass.go:53–57,102–115`；`server/internal/gitops/remote.go:162–165,176–202`；`server/internal/githubauth/ssh.go:13–18`；`server/internal/app/app.go:35`。
- **可复现输入／推演**：只有服务 token、没有个人 helper 的私有 HTTPS 仓库：桥只设置自定义 `KAIWU_GITHUB_HELPER`，没有注册 Git 识别的 `credential.helper` 或 askpass，因此 Git 不会调用它。即使补注册，helper 只接受 `url=`，而 Git credential get 发来的是拆分字段。独立无网络 `git credential fill` 探针实际得到 `protocol=https`、`host=github.com`、`path=owner/repo.git`。
- **SSH**：`git@github.com:owner/repo.git` 被 RemoteRunner 的 URL/userinfo 检查拒绝；标准化 `ssh://git@github.com/...` 同样被 userinfo 拒绝，且 Resolver.gitSSH 只接受前一种前缀。仅允许 SSH agent、没有 key/token 的配置还会被 app 当成未装配。
- **影响／最小修复**：当前 fake bare transport 完全绕开了这些路径，不能据其 PASS 开始真实写入。统一 canonical SSH 表示和协议用户处理；正式注册受控 helper、按真实 credential 字段解析并绑定仓库路径，验证 fd 经 Git→helper 继承及多次请求生命周期；装配纳入显式 SSH-agent 能力。用无网络假 git／helper 子进程契约测试，不需消耗真实额度。

#### I2．本地检查和集成检查失败无法可靠交回真实原 Agent

- **位置**：`server/internal/delivery/stages.go:127–149`；`server/internal/delivery/integration.go:191–199`；`server/internal/delivery/repair.go:82–99`；`server/internal/workbench/delivery_repair.go:95–98`；`server/internal/agent/manager.go:71`。
- **可复现输入／推演**：新周期首个本地检查退出 1，d.CandidateSHA 仍为空，PrepareDeliveryRepair 的非空 failedSHA 校验直接失败。即使在已有 CandidateSHA 的后续修复／集成检查中失败，delivery 仍持有通过 reserveTask 建立的 reservation，未 dropLease 就调用 DispatchRepair→Send；真实 Manager 因 `m.reserved[c.TaskID]` 拒绝启动。此时 repair 甚至可能已扣额度并将 conversation 留在 starting。
- **影响**：承诺的本地失败自动修复闭环没有实现；五场景本地检查固定 true，CI 失败时租约已释放，因而未触发这个问题。
- **最小修复**：用失败检查实际捕获的 HEAD/tree/指纹建立完整失败身份；确认检查组已回收后安全交还租约，再原子认领并派发原会话，不能在 Send 前让双方同时持写权。真实 agent.Manager fixture 分别加入首轮本地失败和集成检查失败。

#### I3．提交核验比较的是工作目录临时 tree，不是实际 index／最终 commit tree，仍可检查 A 提交 B

- **位置**：`server/internal/gitops/candidate.go:183–228`。
- **可复现输入／推演**：cand.Files 仅含修改文件 f；在实际 add 结束后，外部 `git update-index --cacheinfo` 将原本干净的 g 暂存为 B，工作目录 g 仍为 A。后续只比较 cand.Files 的 blob/mode，漏掉 g；`workingTree()` 使用另一个临时 index 从工作目录重建，仍等于 A；实际 commit 使用真实 index，把 g=B 提交。函数最后只返回 HEAD，未复核该提交 tree。
- **影响**：不是一般“外部进程无法禁止”的泛论，而是当前核验对象不对；返回的 CandidateTree 与实际上传 SHA 的 tree 可以不同。现有变化测试发生在进入 CommitCandidate 之前，不能覆盖这个窗口。
- **最小修复**：比较真实 `write-tree` 与已检查 tree，提交后再核对 HEAD parent/tree，异常只保留现场、不推送。通过 Runner 在 add 后注入上述 index-only 变化即可做确定性反例测试。

#### I4．没有真正同步远程主分支，不同 clone 的第二个正常任务会阻塞，初始本地领先还可能被先上传任务分支

- **位置**：`server/internal/delivery/integration.go:81–85,151–155`；`server/internal/gitops/delivery_merge.go:19–42`；`server/internal/delivery/stages.go:40–85,247`。
- **可复现输入／推演**：clone A 正常交付使 remote main 从 L 到 R；clone B 本地 main 仍为 L。B 到队首只 ls-remote 得到 R，却只将本地 L 合并到候选，从未调用 Remote.Fetch／安全快进本地 main。随后 IntegrateCandidate 因 local L != expected R 必然阻塞。
- **另一方向**：新周期的本地主分支有未归属的领先提交 U 时，先从本地创建任务、检查并 push 任务分支，直到准备集成才第一次查询远程主基线；U 已经随任务分支上传。
- **最小修复**：开启／创建工作区和进入队首时 fetch 精确授权 ref，校验本地与远程关系及本地领先归属；只在安全干净快进条件下同步，随后生成候选并重跑门禁。队列互斥测试不能代替双 clone 连续成功交付测试。

#### I5．主 CI 成功前不再核验远程 main，外部 B 可以被挂上 A 的交付绿灯

- **位置**：`server/internal/delivery/integration.go:291–337,339–361`。
- **可复现输入／推演**：本周期推送 A 后进入 main_ci，外部写入者将 main 推到 B；Actions 对 A 返回 success。stageMainCI 直接写 succeeded 并释放 lane，没有再 Observe main。失败分支也没有先核查外部主分支变化，就发起原会话修复。
- **影响**：当前主分支并非已验证 SHA 却宣告已交付；另一人的主分支变化还可能被当作本任务修复背景。初次 push 的回执检查不能覆盖 CI 等待期间。
- **最小修复**：完成和主线失败修复前分别重新读取目标 ref，验证精确发布 SHA／任务可达性；变化时 needs_check 且保留 lane，不用 A 的结果证明 B。

#### I6．旧手动合并只检查“这张卡片”的周期，能绕过另一张卡片／重复项目入口的失败主线隔离

- **位置**：`server/internal/workbench/git.go:136–143`；`server/internal/agent/integration.go:runMerge`；`server/internal/delivery/stages.go:274`、`integration.go:284`。
- **可复现输入／推演**：自动任务 A 已 push main，但主 CI 失败且保留逻辑 lane；旧任务 B 没有 DeliverySummary。B 的 BeginMerge 检查自身无周期就放行；A 等 CI 时不持 OS 项目锁，B 可正常拿锁并修改同一 main。在同 common dir 的另一个项目登记下也一样。
- **影响／最小修复**：SQL lane 仅约束调用它的自动任务，不能满足同目标所有集成共享隔离的要求。旧 BeginMerge／执行前都应查询实际 common-dir／远程 lane 归属；不是仅阻止同卡手动入口。加入 A 主 CI 红灯期间旧 B 合并的反例。

#### I7．succeeded 与释放 lane 分两次事务，崩溃后可永久堵塞后续任务

- **位置**：`server/internal/delivery/integration.go:332–337`；`server/internal/workbench/delivery_queue.go:115`；`server/internal/workbench/delivery_store.go:145`。
- **可复现输入／推演**：AdvanceDelivery 已提交 succeeded，进程在 ReleaseDeliveryTurn 前退出，或释放返回错误（当前被忽略）。重启 ActiveDeliveries 不包含该终态，Recover 不清理它；队列 ahead 查询仍计入其 owner／ticket，后继永远拿不到轮次。
- **最小修复**：成功持久化与释放同事务；或恢复时明确核验并幂等清理“已成功但仍占槽”的记录，不能忽略释放错误。

#### I8．304 处理会让多个 required workflows 的正常 CI 永远等不到通过

- **位置**：`server/internal/githubactions/runs.go:125–139`；`server/internal/githubactions/client.go:175–177`。
- **可复现输入／推演**：必需 W1、W2。第一次 W1 已成功，W2 仍运行；下次 W1 返回 304，Observe 立即返回 waiting/not_modified，根本不查询 W2。W1 完成后通常不再变化，因此即使 W2 后来成功，每次仍在 W1 提前返回，最后 CI 超时。
- **最小修复**：304 表示该 key 的已有证据未变，不应丢弃证据或跳过其他 workflow；聚合本轮所有项与对应缓存。分页缓存须同时保留 Link／完整性元数据，不能只缓存 body。新增两个 workflow 先后完成＋前者连续 304 的测试。

#### I9．预算不是累计活动时间，60 分钟与触发窗口也未在整个流程执行

- **位置**：`server/internal/delivery/stages.go:84–85,281–287`；`server/internal/delivery/repair.go:70–72`；`server/internal/workbench/delivery_types.go:68–72`。
- **可复现输入／推演**：ActiveMillis 无生产更新；唯一活动时间门禁是在准备 repair 时比较 `now > ActiveSince+limit`。等待人工验收、排队或停机两小时后首次发生失败，会把非活动时间算进去而拒绝修复；反之开发／检查／观察超过 60 分钟但不进入 repair，仍可继续。TriggerDeadline 设置后没有读取；无对应 run 会等到 CISeconds，而非 TriggerSeconds。
- **最小修复**：持久累计仅自动活动阶段的时长，在每个新副作用及运行上限处检查；进入／离开人工等待与调度排队正确停表，重启不重置也不计整段停机。每个 push 分别持久触发截止与总 CI 截止，并实际使用。

#### I10．一次网络／5xx 就永久 blocked，额度追加或凭据修好也无安全继续入口

- **位置**：`server/internal/githubactions/client.go:171,194–224`；`server/internal/delivery/stages.go:353–354`、`integration.go:362–363`；`server/internal/workbench/delivery_control.go:127–157`。
- **可复现输入／推演**：Observe 一次连接失败／500 返回 StateBlocked，delivery 直接进入 blocked，忽略 RetryAt。Control 的 resume 只允许 paused，pause 又拒绝 blocked；extend_budget 仅增加数字、不离开 blocked。达到修复次数上限后显式追加额度同样不能继续。
- **影响／最小修复**：普通瞬断会变成不能在原周期处理的故障，主线 lane 还会长期占着。瞬断／5xx 在原截止内做有界只读重试；为可核实的 blocked 原因保存可恢复阶段并提供明确继续操作，不能把未知副作用也盲目变回 local_checking。

#### I11．Recover 不是调度前屏障，且真实 commit／merge／检查恢复窗口缺少完整 intent 与写权重建

- **位置**：`server/internal/delivery/manager.go:73,164–180`；`server/internal/workbench/store.go:324–329`；`server/internal/app/app.go:115–123`；`server/internal/delivery/stages.go:14–16,159–178`；`server/internal/delivery/integration.go:85,183–191`；`server/internal/delivery/recovery.go:107–112,180–203`。
- **可复现输入／推演**：Subscribe 立即塞入当前 revision，New 立即启动 loop，app 随后才调用 Recover，因此并非“先恢复再调度”。已有 integration_checking 周期启动时直接 RunChecks(lease=nil)，没有先重建项目／任务租约或核验旧 writer。
- **实际 Git 窗口**：CommitCandidate、SyncCandidate、IntegrateCandidate 用的 gitRunnerFunc 始终 `gitops.Run(...,nil,...)`，既不调用所持租约 Attempt/Started，也不继承其 fd。提交／merge 前无 running 步骤落库，回执是在命令完成后才写。若服务死在子 Git 仍运行时，marker 还可能指向已结束的检查组，重启不能证明新 writer 已结束；Recover 的 `case "commit"` 没有对应生产 intent 产生点，其测试是手工插入记录。
- **模型窗口**：修复轮次重启被 Store 变为 interrupted，stageRepairing 只等 finished/confirmed；Recover 不核查 delivery_repairs，故保持 repairing 无限等待，而非明确 needs_check。它没有盲目重发是优点，但不等于已完成不确定窗口处理。
- **最小修复**：New 先保持 dormant，Recover 完成后显式启动；每个可写阶段重新取得并验证租约，所有 Git 子进程使用真实追踪 runner／fd 交接；commit／merge／检查先落意图与 parent/tree，恢复按可证明证据推进，不明转 needs_check。修复 job 的 dispatching／中断也需显式核查状态。现有 push intent 的唯一可证补记应保留，不能用它代替其他步骤。

#### I12．撤销／暂停只在阶段入口生效，正在检查的旧阶段仍能在之后启动提交；暂停还提前释放写权

- **位置**：`server/internal/delivery/manager.go:196–205`；`server/internal/delivery/stages.go:139–159`；`server/internal/delivery/control.go:28–34`。
- **可复现输入／推演**：配置一个可等待的正常检查，进入 local_checking 后撤销授权，或 pause 并发送新修改意图。检查返回成功，旧 stageLocalChecking 不再读 live policy／当前 delivery.Version，直接 CommitCandidate。暂停情况下随后 CAS 会失败，但提交副作用已经发生；Control 在 worker 核实退出之前 dropLease(false)，又允许新 Agent 竞争原工作区。
- **影响／最小修复**：阶段级 gate 不能保证“撤销后不启动下一个副作用”。在每个实际 Git／模型启动点重核授权和周期版本；暂停由 worker 确认当前操作已结束／已核查后再安全交权，不能只改 phase 并关闭租约文件。给检查返回与 commit 之间插入撤销／暂停的确定性测试。

#### I13．真实效果预览未与交付租约交接，验收后可能一直停在 integrating

- **位置**：`server/internal/agent/delivery_lease.go:92–128`；`server/internal/agent/task_preview.go:39–60`；`server/internal/delivery/integration.go:139–146`；对照旧 `agent/integration.go` 的 runMerge 先调用 stopTaskPreview。
- **可复现输入／推演**：awaiting_acceptance 时用户启动真实受控预览，再验收。预览持有 execution lease；AcquireDeliveryLease 只 reserveTask／拿锁，没有 stopTaskPreview，所以拿任务锁失败。stageIntegrating 只原样返回并重试，不能自动可靠结束预览、交还执行权。无浏览器后端也不会自行恢复。
- **最小修复**：在 Agent／Store 锁之外可靠停止本任务预览，再 reserve 并取得租约；失败应有可见原因。加入真实 preview.Manager→验收→主线集成的后端生命周期测试，而不要求前端补发 stop 来保证互斥。

#### I14．“聊天发送开始开发”绕过无 worker 的新增 503 门禁，仍会保存周期并对 nil 调 Wake

- **位置**：`server/internal/webapi/handler.go:372–383,399–409`；`server/internal/workbench/store.go:229–234,298–304`。
- **可复现输入／推演**：Delivery=nil、项目 policy.enabled=true，提交 `send_message`，text=`开始开发`。handler 的新门禁只识别 c.Type==start_task；Store 内才把该消息规范化为 start_task 并返回 DeliveryID；handler 随后执行 `h.cfg.Delivery.Wake()`，发生 nil 接口调用，且消息／周期已经落库。
- **最小修复**：在 handler 所有可用性／平台门禁之前统一识别两种开始入口，或将可用性作为原子 admission 条件；新增和 direct start 相同的“聊天别名＋无 worker＋重复请求”测试。本轮 TestDeliveryStartDispatchOwnedByWorker 目前只测 direct start。

#### I15．派发失败使用旧版本 block，又提前记已派发；容量不足等会变成永远 developing 的幽灵任务

- **位置**：`server/internal/delivery/stages.go:61–71,86`；`server/internal/delivery/manager.go:271–279`；`server/internal/agent/manager.go:92–94`。
- **可复现输入／推演**：已驻留 8 个会话，第 9 个任务 Agent.Send 返回 session capacity reached。stageQueued 已 AdvanceDelivery 得到 sent（新版本），但失败时却 `m.block(d,...)` 使用旧 d，CAS 必败；rememberDispatched 又已经置 true。下次扫描跳过 Send，直接写 developing，实际上没有原会话执行该轮；轮次一直未结束。单次其他 Send 拒绝也一样。
- **最小修复**：先采用持久化后的 sent 版本处理错误；可证明未发出的容量不足应排队重试，已发出／不明必须核查；不要把“尝试过”当成“派发成功”。明确记录 dispatch 结果并在下一次扫描依据其状态决定，补真实 Agent 容量反例。

#### I16．可选提交身份的既有缺省丢失，普通新项目会在首个自动提交时失败

- **位置**：`server/internal/delivery/stages.go:159`；`server/internal/gitops/candidate.go:170–172`；对照旧 `server/internal/gitops/preview.go:22–32`；`server/internal/workbench/commands.go` 的 add_project。
- **可复现输入／推演**：新项目未填写高级 CommitName/CommitEmail（既有正常缺省），自动交付配置和预检都可通过；提交时直接传 p 的空字段，CommitCandidate 必然拒绝。旧流程会读取仓库 local identity，再回退 Kaiwu 缺省；webfixture 主动填入 Fixture 身份掩盖了差异。
- **最小修复**：复用既有安全身份解析／缺省并在周期内冻结，或在启用／预检明确阻塞并要求配置，不要等模型开发和检查完成才失败。

#### I17．验收校验只读数据库 token／version，未重新核对实际候选和最新成果轮次

- **位置**：`server/internal/workbench/delivery_control.go:46–68`；`server/internal/delivery/control.go:12–14`；`server/internal/delivery/integration.go:133–158`；`server/internal/delivery/repair.go:134–139`；`server/internal/delivery/recovery.go:215–223`。
- **可复现输入／推演**：候选 A 到 awaiting_acceptance，外部编辑任务 worktree 为 B（或移动 task HEAD），数据库版本不变。Accept 仍消费 A 的旧 token；integrating 只核查主目录，不重新核查 task owner／HEAD／tree，最终可以发布 A，而用户刚查看的实际效果来自 B。修复后 Delivery.RoundID 也仍为初始开发轮次；恢复已落地 task push 时，仅在 token 为空才签新 token，与正常 push 每次重签不一致。
- **边界说明**：正常修复后的新 push 会生成新 token、CAS 会拒绝旧版本，已有这部分保护，不应抹掉；不足在于数据库之外的候选变化和最新成果绑定没有复核。
- **最小修复**：验收／后续写入前在受控交权下重核当前候选 SHA/tree、基线和实际最新成果轮次；不符则旧验收过期。记录明确的 accepted candidate/base/round 元组，修复及恢复路径遵循同一失效规则，同时保留历史人工认可。

#### I18．修复分类将“没有命中几个关键词”当成源码证据，基础设施未知失败会消耗模型额度

- **位置**：`server/internal/delivery/repair.go:18–33`；`server/internal/githubactions/logs.go:46–60`。
- **可复现输入／推演**：workflow conclusion=failure，日志为 `The runner has lost communication with the server`，或其他未列入黑名单的供应商错误。ClassifyFailure 会返回 repairable=true；没有编译／断言／类型等正向源码依据，也未把 evidence.Truncated／Repairable 作为不足证据门禁。
- **影响／最小修复**：与“原因不确定转人工、不无限烧额度”要求不符；3 次上限只能限制损失，不能证明每次调用有依据。首版对有明确正向源码证据的类别放行，基础设施类别在截止内观察，未知／证据不足明确阻塞；本地无法启动／目录无效等环境错误也不要无条件当源码修复。

### Minor

#### M1．HoldsTurn 投影始终为 false，无法真实展示持有／隔离状态

- **位置**：`server/internal/workbench/delivery_queue.go:125–140`；`server/internal/workbench/delivery_sync.go:21`；`server/internal/workbench/delivery_types.go:75,145`。
- **推演／影响**：AcquireDeliveryTurn 只更新 queue.owner，没有更新 Delivery.HoldsTurn；整个生产代码只有定义与投影，主 CI 红灯仍持 lane 时 API 也报告 false。
- **最小修复**：投影从实际 queue.owner 读取，或与周期更新同事务维护；释放时一致清除。不要依赖 UI 根据 phase 猜隔离事实。

## 七项重点的审查结论

1. **多 clone／重复入口、失败 lane**：正常自动队列的双键隔离有实现；跨 clone 同步缺失、旧手动绕过及成功后遗留 ticket 见 I4／I6／I7。不能称整仓所有集成入口已隔离。
2. **检查输入绑定**：检查期间改源码会使 Passed=false，提前发生的内容／索引／mode／symlink 变化有保护；实际 index→commit tree 的剩余窗口见 I3。检查改源码不会直接成功提交 B，但当前本地失败回修复又被 I2 阻断。
3. **304／attempt／分页／迟到成功**：SHA／branch／event 过滤及 attempt jobs 读取成立，Store 拒绝旧候选证据；多 workflow 304 组合见 I8。未将单项契约测试解读成所有并发 API 变化已验证。
4. **主 push 回执崩溃、启动恢复、commit/model**：精确远程 ref 的 push 补记具备实质依据；其余恢复边界、调度屏障与未知模型结果见 I11／I15。成功持久化后释放槽还有 I7。
5. **效果验收**：正常源码修复新 push 会换 token，人工认可与技术状态分离；实际 worktree／base／最新轮次与恢复路径不足见 I17。
6. **生产凭据、远程范围、外部变化、定义、撤销**：C1–C4、I1／I5／I12 是真实联调前必须处理的边界，不是 UI 待完成问题。
7. **原 Agent／写入权／暂停／预算**：原会话复用方向正确；本地失败交接、预览交接、暂停旧阶段、恢复租约、派发错误和计时分别见 I2／I9–I13／I15。

## 证据、限制与 Declined to judge

### 本次实际执行

- 只读查看 Git 状态／diff（基线 4829749）、实际后端实现、相关已有测试和本轮新增测试／webfixture；审查期间 HEAD 为 `1c76c0a430b57c7c3b9fdcf69ae20d5a0c0e7141`。
- 只读核对本机 pi 安装产物的 RPC get_state 实现／类型，不启动 pi，不发送 prompt。
- 三个不落盘、不联网的 Git 合成探针：credential get 的真实拆分字段、冻结 URL 的 rewrite、helper 可见继承环境。全部使用合成值，退出码均为 0。
- 没有修改仓库文件、index、HEAD，未运行 restore／clean、创建 worktree／Workspace、提交／合并／推送、真实模型／GitHub／7331。唯一人工写入输出为本报告。
- 主控提供的 Go 全量 PASS 和五条真实 Store+bare+fake 外部端场景 PASS，作为背景记录；本审查**没有重新执行这些测试**，不把它们写作本人的新鲜测试结论。除上述三个探针外，问题均为代码路径与明确输入／时间窗口的静态推演，未声称已在完整服务动态复现。

### 未审／Declined to judge

- **Task13 UI 完成性、文案和浏览器布局**：明确由另一个 Agent 负责；不据此扣后端分，也不宣称 UI 已验收。
- **真实 GitHub API 版本是否接受、权限、分支保护和实际 workflow 触发**：禁止真实 GitHub 接触，本轮没有独立联网来源核验；现有合成 REST 结果不能替代它。
- **真实 fd 跨 Git→helper 进程的认证成功、真实 SSH host/key/agent 行为**：未运行真实远程；I1 指出到这一步之前已能由代码证明的阻断，不宣称认证已联调。
- **真实模型修复质量、具体 provider 切换能力／费用**：没有调用真实模型；仅审查本机 RPC schema 和调用生命周期。
- **任意恶意仓库的系统级隔离、同用户进程读取 `/proc`／个人 HOME 的完整防护**：规格明确不提供 OS 沙箱，需另行设计；这不豁免 C2 的显式环境／远程边界要求。
- **Windows/macOS 执行与 PC／Android 原生交付**：本批执行保护限 Linux，未做跨平台运行；未把保留产品目标误当已验证能力。
- **泛化日志秘密识别和任意测试语义是否削弱**：不宣称本轮能证明；已列出明确缺失的定义冻结和已知服务凭据继承，不以“无法自动证明任意语义”为由放弃这些可机械保护点。
- **现有全部旧功能、数据库长期容量／性能与完整全仓安全审计**：只沿自动交付相关调用链核查；本报告不是对未读取路径的总体无缺陷担保。

## Ready for real integration? **no**

当前已有有价值的隔离闭环和 push 回执证据，但真实 pi 握手、生产认证接线及远程／凭据／门禁保护仍有确定阻断，此外本地失败、跨 clone、预览、暂停与恢复存在未覆盖的正确性窗口。建议先为上述明确反例补隔离 RED→GREEN，再单独申请受限测试仓库、真实 Actions 和真实模型的联调授权；不应把现有五条 fake 外部端场景当作开始真实自动上传的放行证据。
