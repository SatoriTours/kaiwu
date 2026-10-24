# 当前开发：产物直达与双合并操作

## 当前阶段（下方保留历史，不作为最新验收结论）

| 范围 | 当前状态与下一步 |
| --- | --- |
| 后端2c/d R2 | a18独立R2.1–8：9既有真链+D1非空error+E2真SQLite重开Recover/外部HEAD对照+RPC/preview spawn证明通过，主控已核日志/hash。R1 A/B/C正常/E1/F/R1.5保留。poison(pid0) malformed seam与marker删除错误残留仅原语记录、非活writer泄漏/未做E2E，非阻塞未冒称修复。Task2 merge/lease冻结；不代表真实GitHub/CI或全仓通过。 |
| 预览UI R3 | 原1f30再次idle/null/pending[]，当前App/ResultPreview/报告无新增；两次旧F5 final不是本次Task3b完成，不第三次续派。原部分Dialog/merge/helper/types保留，由主控a322当前openai-codex/gpt-6.1-sol（按已批准主控小切片、无指定实现模型限制；不是Deepseek额度备用/升级）单写闭合。已实际红→绿：local显式审核/round/原子认可，无confirm_result、checks不变、1写、草稿保持（真Go+临时Git+Chromium）。未知写保留跨close/mode/round且404不重放。原preview-intent6d1未改、旧F3不重开；现新UI消费侧spec/quality已独立通过，详下行§16，视觉/真实远端未验保留。 |
| UI独立验收 | 569§16已实读核日志/hash：指定6例19.4s、敏感UI1例3.2s、可信DOM1例5.4s全部EXIT0；M1关闭，spec与quality分别通过。M2按边界所限UI真实远端未验，非功能失败；独立视觉仍BLOCKED。生产源/dist未改，未冒整体交付完成。 |
| F3根因策略 | 既定缺口已闭合，停止新探针：真实trusted坐标点击、原Open发出约43–47ms后真页就绪，再138–287ms三次重复均0新增Open/tab，过900ms可重开。旧click命令计时判定撤回；W2 ready采样pnow2493ms并非刚完成，不能凭采样后20/289当700内反例。原日志与裁定保留§13/14，不改700ms/不新建测试框架。 |
| 测试进程 | 独立窄验结束，资源已还；16源集合eb8e4902 frozen。正式npm run test:chat-turn自动全19例→5个fresh服务串行片、worker1/每片≤4，不升8驻留/10登录上限，主控19pass/EXIT0。17445空闲，旧fakepi15，无新增孤儿，不碰4173。后续无通知轮询/重复测试，等视觉补授权。 |
| 剩余交付 | 下载R1独立通过；Task3b消费侧spec/quality及M1通过，原68单测/build证据保留，四测试相关差异已独立闭合；主控已核实际增量全文、19全量及8窄验日志、旧分项报告，未第三遍测试。独立视觉仍受阻，已一次补问授权已保存UI配置先核图像能力再只读验六图（额外模型调用），未答不切换、不重复催问。UI真实远端/完整Tab循环/HTTP-WS长期phase及503变体等未验界限保留；整体未完成。未Git/7331/真模型/GitHub/CI/部署。 |

## 提交收尾（用户最新授权）

- 用户重复“提交代码”合并执行一次，现授权本任务 commit／实际 main 推送／既有部署流程；不代表版本发布或豁免未完成验收。核实 GitHub 默认 main、origin 与基线 04d50f4，一条实际 worktree；Actions 工作流总数 0，不适用，不虚称 CI 成功。
- 仅暂存本任务 96 文件；暂停子 Agent 后端、旧截图、二进制和测试遗留均保留，types.ts／workbench/store.go 仅暂存本任务 hunk。共享目录还有待保留成果，不能为归档清理或代交。
- 干净 index 快照前端构建／68 单元通过；首次 Go 全量两项 webapi 合并失败，保留原红。Linux `/proc` 消失窗口实证 ESRCH 被旧判断误拒；原89最小修复及确定性红绿、真实 churn 和包级回归通过；原a18独立窄验需求／质量分别通过，反向突变同时锁定 ESRCH 与权限错误两侧。短 churn 测试未在修前复现，仅作回归守卫，真实高并发 churn 证据未独立重跑。不能宣称该路径就是原两红唯一原因。
- 沿现有 reference-transaction 钩子干净归档门禁，不关闭钩子／降低断言。实际 commit／push／部署结论保存本任务 agent-artifacts 的 delivery 台账；独立视觉仍待原一次授权回复，不切模型，不把代码推送当全部验收通过。

## 阶段历史

- 2026-10-25 M1夹具修复：主控仍唯一实现负责人，跨通知修复计数不清零。两种原根因（rate/ready）真证；新8驻留饱和单列夹具资源问题，改独立服务串行分片而非第三次同策略重跑。Go/生产UI未改。569原功能验收同profile实际Deepseek/off，最新profiles全notes/core/完整3–4节已核，idle/null/pending[]/workspace/cwd已核后仅6例窄验；角色阶段usage未知，已有累计usage不当本轮费用。模型cost0不当免费。

- [ ] 回主线：profiles全notes和最新core已核，89/1f30均idle/null active/pending[]且同Workspace/cwd/main/.git/index后复用原负责人（实际pi/open-design/deepseek-v4.1-flash/off，配置请求high未改，复杂安全/异步状态选择非额度备用/升级）。89 `89f4d69f-e503-403c-918f-a6963a6c80f3` 单独Go实现Task1显式下载，1f30 `1f30f048-7130-4201-b564-3afeb9fc4c6f` 只TS源/单元实现Task2双动作消费端，禁止Go/browser/Vite直到89冻结；下载UI等真实契约同1f30接入。两次send均turn_started，usage本阶段未知。必要源基线单份artifacts/remaining-actions-baseline.tar.gz（111文件269552B，含SHA元数据，HEAD04d50f4）；不建空间/分支、不Git写、不复制凭据/真实会话。F3既定缺口依据§13/14关闭，F5组合失败交原开发诊断，未因单例绿忽略。
- [ ] R2独立验收执行异常（非R2测试失败）：原d7再次重复旧R1结果，`merge-acceptance.md`仍550行且无R2节；核idle/无active/pending/Go，末活动连续准备与2次压缩，缺实际R2验收证据，未以finished算通过。停止原样续派，保留原Agent；新a18在同Workspace/同功能验收profile（pi/open-design/deepseek-v4.1-flash，配置high、实际未核）只读继续，原因必要上下文隔离，非升级/额度备用。策略缩为既有真链测试先执行、仅补D1/E2缺口，修复仍R2不清零。原d7累计usage input1074518/cache22268160/output607874/context103261/128000；R2阶段独立用量及非缓存口径未知，日志price0不当免费/实付。
- [x] 用户确认：网页／HTML直达实际产物，移除“继续修改”，区分“合并到主分支”和“合并并推送”，不强制独立确认完成；2026-10-24明确“好的，开始开发”。本轮不授权本仓库Git写入／部署／真实模型与GitHub联调。
- [x] 主控核对最新全局规则、当前空间／cwd／分支／git-dir/index及6个profiles；[实施计划](docs/superpowers/plans/2026-10-24-task-result-actions.md)已完成并自查。Task1/2共享Go及types契约，采用同执行者串行；Task3消费后端契约后才启动，Task4独立只读验收。不新增Workspace／worktree／分支。
- [ ] 后端Task1/2：`0d4b7140-faa3-4d9b-b773-ac57007db2c9`，profile复杂编码职责，实际 `pi/open-design/deepseek-v4.1-flash`，配置thinking high／实际off；因静态安全／凭据／持久远程副作用与恢复复杂度选择，非额度备用。执行端约94分钟反复读取／规划、2次压缩，结束仅声明将开始，无实现／测试／报告，已核实idle且无activeTurn。不能视为完成，不原样续派，进入有界疑难策略诊断；证据见artifacts/backend-stall.md，非额度问题。
- [x] 用户追加明确需求：移除置顶任务概况／往期记录，一提问一张AI简短状态／总结，运行中loading，点击展开完整详情；对话列表可只看自己的问题并定位上下文。不新增模型调用、不改存储／真实上下文。
- [x] 疑难策略诊断：`965e7895-2877-4ad5-a73d-5cf8b6d82f15`，最新疑难profile实际 `codex/gpt-6.1-sol`、auto-review／high；报告artifacts/backend-diagnosis.md证据支持角色确认／重复设计回圈，任务跨度是可信诱因，无额度或通道故障证据。未改源码／测试。原执行者停止后才升级，非额度替补。
- [x] 后端1a恢复试片有实际红→绿及4文件diff；主控核对manifest `ba55840b`／manifest_test `64a9ecfa`／discover `75cf5325`／discover_test `29c7c398`。v1保持、v2静态解析和固定4候选确定性发现通过开发者聚焦测试／vet；未接manager/gateway，不能称预览可用。报告artifacts/backend-1a-report.md。
- [ ] 后端1b首次续接未产出：原执行端收到Pi RPC prompt 60000ms超时通知，核实仍有autonomous activeTurn，最近活动又出现Compacted，未新增1b文件。按已定停止条件终止原任务执行端，保留1a源码和报告，不视为额度问题。已核实原执行者closed；新执行者 `89f4d69f-e503-403c-918f-a6963a6c80f3`（同复杂编码profile `pi/open-design/deepseek-v4.1-flash` high）已完成1b：4个新文件安全openat原语，主控核对hash `ee18920d/ccb37348/d895d46f/6b1fb946`，开发者定向/race/包全量/vet通过。1c开发包全量/race/vet/build及webapi Preview报告通过，主控核对manager `94413f20`／gateway `fd8404f1`／static `e36e7f75`／tests `6afdca04`；实际diff发现CSP阻挡CSS/图片/字体且JS裸相对source无效、Stop后不能同配置重开、ensureStatic忽略旧进程回收错误、所述超时不能解开网络Write。原开发者第1轮修复已停写，主控核对manager `9336c2f0`／gateway `dead4df00`／static_manager_test `7254c62b`／浏览器fixture `22cc1622`；开发者真实Chromium相对JS/CSS/图片、opaque cookie/storage及/api阻断报告通过，目标包/race/vet/build及webapi Preview通过。已交独立功能验收，当前只冻结静态后端，不把开发自测等同验收。后续下载／命令复开／2a–2d未实现，沿用单份切片报告，计数不重置。
- [ ] UI Task3a开发自测38单元/build/桌面及360两浏览器场景通过，但主控实际diff发现taskState漏conversation使真实轮次成果操作丢失/误绑legacy、展开锚点仍用旧class、report.summary无精简上限、passed缺证据提示被隐去等；已交同一UI执行者第1轮修复并补390px/多progress+final/状态/loading/切卡片草稿与键盘定位/临时目录清理断言。首轮修复后开发者40单元/build/两复杂浏览器场景自测通过；主控核对ChatPanel `6a4b6f26`／AssistantReply `a9c395ff`／chat-messages `0314617a`，已停写冻结交独立验收 `569e082a-1e43-43fb-a98b-c45e0bbb600d`（最新功能验收profile `pi/open-design/deepseek-v4.1-flash` high；报告artifacts/chat-acceptance.md，和后端报告分开避免并行写冲突）。独立验收结论不通过：唯一D1/S2，article.space.prevent无条件吞掉all态内部summary/嵌套details/button的Space，真实浏览器已复现；其余聚合/loading/摘要/依据/正确round/筛选定位/草稿/三视口均有通过证据。原UI第2轮最小修复已停写，主控核对ChatPanel `dd9c7859`／browser test `7daf040b`，其余6hash未变；同验收者569e082a实际D1聚焦探针1 passed/exit0，顶层/嵌套Space、正确round确认、questions Enter/Space无误发均通过；继承其余证据，Task3a交互功能验收通过。主控实际目视390px展开详情与1440px待处理交互两张既有截图，所见状态无明显遮挡/横向溢出，非所有视觉状态完整验收；Task3b尚未验收完成。继续Task3b-预览子片：同UI仅App/ResultPreview/preview.ts、TaskPreview类型块和必要预览事件/测试，实现HTML/进程cap新标签页、首次命令明确授权与内存绑定重开、无网页运行记录/弹窗拦截/迟到隔离；后端Go契约已存在且本轮缺陷修复不改接口。Git双动作及下载UI等后端对应契约后接，Task3a已验收行为不得回退。3b-preview初版hash App e9a314a4／preview c61e4246，主控实际diff发现授权Set未清登录、await后缺epoch/round校验、双击Start新operationId、popup阻挡无可点击链接、首次审批仍旧iframe、无网页空面板且isWebPreviewable未消费；现浏览器仅条件click无实际HTML/命令，不能充当新合同验收。原UI 3b-R1已停写，App `f70b322f`／ResultPreview `7c35d326`／preview `fccebc94`，新增真实静态browser通过，但主控diff仍发现blocked URL设后被closeResult清空、stillCurrent未比当前最新round、局部opID未知重试不稳；官方仅desktop+static，删除360390且缺命令/阻挡/迟到等用例，report错误改写声称全项验证；另查出未申报fakepi/main.go 10行静态测试夹具（`c37c1a69`），归本任务并保留纳入审查，非业务Go；已在同report追加主控纠正与边界。已交原569首次独立验3b-preview，要求一次汇总完整失败/缺证据、真实blocked/迟到探针与必要手机/Space回归，不重做既有3a或把标题当证据；569独立3b-R1不通过：F1 blocked URL清空无链接、F2同卡新轮仍导航旧结果、F3 120ms双击2Open/2tab；F4切卡两种注入证据矛盾需request-id确定性用例；F5命令/未知重发/历史无fixture未完成。静态图片/CSS/JS、非空草稿/Space/360390已有实际通过证据。已交原UI 3b-R2统一intent失效/回退/去重及真实v1 fixture补验；仅额外授权fakepi/main.go测试模式，不改Go业务/preview接口。报告必须追加，第二轮若同根因不收敛第三轮前暂停升级判断。原UI R2仅Read后空assistant（native JSONL末尾07:43:39.246Z，stop/content空/output0/input2076/cache98386；不是新修复/额度错误），四文件hash未变；旧结论重复通知已去重，未启动验收。原075c41cc已kill并核实closed，无activeTurn/pending permissions；同最新UI profile/同模型通道新上下文有界续接 d25606b7-92ff-4df2-b173-e17e659f4aaa，仍本Workspace/cwd和3b-R2计数，限30工具30分钟，不再换上下文循环。R2现停写：App `c55ffa2e`/ResultPreview `eee2ed95`/AssistantReply `113ce094`/browser `75876de7`/fakepi `c765a64b`；开发报告8浏览器/41单元/build/全Go通过，但主控实际断言核对仍有缺口：pre-call全局API钩子并非迟到原回执；F4无切回/登出，F3只40ms且busy清空后时间窗无效；F5无变配置/Stop重开/未知回执/历史/无网页例。报告又丢失原主控纠正，已按实际读取记录恢复并追加R2核对，不采信“全项完成”。569的R2独立复验§11仍不通过：F1真link Enter/opener、F5真实v1首批/变manifest/Stop重开/丢回执不重发、无网页真记录及Space/360390通过；同卡新round失效通过，但ABA切回旧回执会导航，120/250ms在busy清空后每击新Open/tab。稳定opID/历史不可达/跨卡非空draft未验。两轮同根因不收敛已暂停原UI微补策略，主控根因明确为缺单调scope失效和跨完成意图去重，选择复杂状态实现升级R3而非quota fallback，不另派GPT根因盖章。d256核idle无active/pending后kill并核closed。最新6profiles notes已读，新执行者1f30f048-7130-4201-b564-3afeb9fc4c6f（pi/open-design/deepseek-v4.1-flash，请求high，实际runtime未知）仅App/ResultPreview/AssistantReply/preview小intent helper与TS测试；先TS红绿/typecheck，禁止Go/server/Playwright/Vite build/dist直至89冻结，避免Go embed读写冲突。后端R1并行只Go。保留F1/F5/3a已过，不恢复大历史panel；统一intent、单调选择generation、只关闭owned blank、稳定unknown操作ID，快速二击独立busy，保留以后的明确重开。R3 TS首片已停写：intent bbf399ab/App ec02c278/test213a176f/browser8b8903c3，developer typecheck及35+7+6纯单元exit0，尚无浏览器证据。主控读实际源发现unknown Status(false)被误当未执行、Status(true)open-only verdict被忽略后却发新ID Start、任意non0 APIError被当确定失败、settle无原opID绑定/登录清理、start/open完成窗key分离、历史仍status=>Open；F3测试还未确认回包实际已交付。已同R3一次交回原1f30f048纯TS修正上述，额外限12工具15分钟，不换context/计数，不启动Go/browser/Vite/dist。同R3返修现已停写，主控核实际hash：intent 6d1b3d3b/App99f21bff/test19271cc4/browser65d456d1；typecheck与35+9+6纯单元developer exit0。实际源码已保持unknown+原ID、status运行只Open且return、错误unknown、settle绑定ID/auth清starts、跨gesture共享窗、sync watcher和历史守卫；未运行浏览器，不能据此关闭F2/F3。现等待89真实Go冻结事件再续同UI执行者一次build+真实17445 E2E；不轮询、不在未变快照再审/全套重测，不宣称全绿。d256末usage input1896157/cache31986127/output86484，日志price0非实付，非缓存口径未核；effective thinking off未擅调。旧role累计input3285027/cache45978427/output195486，日志cost0非实付/免费，非缓存口径未核；请求high实际runtime off，未擅调配置。
- [ ] Task2a本地审核认可：89f4d69f已实现local+round绑定／认可与queued原子持久化、旧请求hash和不认可语义保持，主控核对workbench/git `6b7919ef`／agent/integration `81c22d40`／新测试 `586020be/2ca1681c`；开发者聚焦/race/vet/build通过。实际diff遗漏新local路径的空rounds+精确legacy token正向兼容（原ConfirmResult已有），原开发者第1轮小修通过：git `fa0e5b39`／merge_ack `ebb4ac4b`／integration `81c22d40`／integration_ack `a4f92fef`；精确legacy token同事务合成认可+queued、错误/无结束证据拒绝与旧hash保持，聚焦两包test/vet通过，未重复全包race。Task2b只读审核/API/socket已产出，开发者包级/race/vet/build通过，主控核对manual_review `bbbd37ae`／workbench `b9245118`／integration `451b8de8`／API `70bb254e`／app `00c20097`。但实际代码只列remote..task，遗漏main额外提交；AlreadyMerged混淆远程包含任务与本地已合并，CanPush未继承本地安全拒绝，已合并本地指纹可能为空。2b第1轮已修：manual_review `3875ab1a`／status `bae2e31a`／workbench Git `8812e96a`／integration `53404022`，main+task完整上传集、localalreadymerged、localSafety+全部历史私密检查、contentProof已实现并自测通过。d7首次独立验Task2a通过、Task2b不通过，报告merge-acceptance.md；完整上传/只读/200上限/私密历史等有实际通过证据，唯F1严重：local已merge再dirty仍PushOnly=true；F2缺凭据blocked丢本地已merged事实；F3 repo credential.helper真实执行且审核未拒绝。原开发者2b-R2已停写：manual_review `04fa6cb4`／remote `9a7f4a2c`／command `9885f361`／r2_test `d41311ef`；PushOnly绑定无Pending、blocked提前保留local facts、repo generic/scoped/include helper拒绝+Args清空，真实credential fill/loopback401 marker未执行。主控核对实际hash与diff，已交d7仅F1-F3独立聚焦复验（含matching scoped URL/fixture正控与原受控凭据供应），d7 R2独立F1/F2/F3均关闭，含unsafe helper正控；但新发现服务凭据桥未接Git，真实credential fill取不到username（merge-acceptance R2.7）。主控核实askpass只url协议且production无helper配置，另Env仍继承原始KAIWU_GITHUB_*；原开发者凭据前置已接线并停写：askpass `5564eeed`／credentials `9bb6cc0a`／command `cfee631d`／remote `c792c228`／真实桥测试 `03a32e99`，真实server binary+Git fill假凭据红绿、Env原始service token/未授权agent隔离报告通过。d7真实binary/fill/quote/环境/恶意helper复验已通过，但真实Git2.55 HTTPS401重复capability[]被map误拒（C1阻塞），url+冲突host仍答token（C2契约偏差）；桥前置仍不通过。已派原backend credential-R1仅协议标量/合法数组、完整混合上下文、真实401整体成功与单pipe二读有界EOF；旧token Env隔离证据保留，测试不放松生产github.com白名单或涉及实际认证/远程。credential-R1已停写：askpass `44f3d2e5`/credentials `259696c5`/test `5db5d95e`/r1test `748798aa`，主控核hash；d7独立真实401整体认证exit0、原合法数组/标量与混合冲突拒绝/token未读、同pipe二次4ms有界EOF与no-op store/erase均通过，CanonicalHost未进生产配置/HTTP面，凭据桥前置解除。R1.5实际148KiB短标量/68KiB数组可被接受，64KiB只单行而非声明总量；无越权/泄密证据，但总量声明须在下一开发片最小收紧并聚焦补证，不推翻C1/C2/FD。Task2c/d已交原89f4d69f复杂编码职责接续（同provider/model，复杂持久副作用原因非quota替补）：完整远程review proof/原子准入、先保存local事实后精确SHA普通push、unknown只观察恢复且不重放、同op幂等与lease/PID归属、R1.5总输入小修；不开放半成品push、不启用delivery worker，限35工具40分钟，临时真实bare push+故障注入，前端冻结接口以外暂不消费。2c/d开发现已停写，主控实际diff/hash核对agent push `9789ff6c`/recovery `93f6a769`、workbench push `3d8807db`/git `04a9a000`、credential总量 `6bd5ba0e`。开发者五包test/四包race/vet/build通过，但全仓test出现pirpc/fixture_truncated失败，单包重跑通过不冒充全绿；首次独立验收已交原d7聚焦实际bare push/冻结准入/lease与未知恢复/legacy/总量上限，继承已过范围不重测。主控发现mainSHAAllowed忽略祖先bool/父数量边界、PushOnly仍调用merge、merged/remote_done未纳active与提前terminal、remote回收safe传播/本地成功后persist失败和review缓存清理的待证风险，要求一次汇总实际证实/证伪。RemoteRunner.Push自身已Observe，不重复误报无核验。d7首次2c/d独立不通过（报告T2CD）：A mainSHAAllowed接受无关root/单父/含未审文件的双父；C merged/remote_done不active且崩溃不恢复；E模拟保存失败后本地事实无法补回；F reviews过期不清。真bare精确SHA/dirty/replay/只Observe等有通过证据。主控追加纠正：当前代码已开放push准入，不是旧“仍拒绝”；R1.5新hash未独立验不可沿旧R1；D只验证无活动writer的残留marker，不证明cleanup失败安全；B无新commit不等于从未调用merge/ORIG_HEAD不变；HTTP/WS及真实persist故障链缺证据。主控单次临时Git语义探针exit0另证：alreadymerged再merge虽main SHA/3个commit不变，却改ORIG_HEAD；产品runPushMerge无条件MergeTask，B只证明不新增commit不足以判纯推送。下一集中修复须真实PushOnly跳过commit/merge并加ORIG_HEAD集成断言，探针已清理未编译Go。UI独立编译型验收现结束，已交原89**2c/d集中修复R1**：A正确父错树/root/单父拒绝且合法非FF放行、B真PushOnly不触碰ORIG_HEAD、C非终态/lease后才terminal、E真persist失败后只读恢复本地审计且绝不重推、F有界review缓存；另补D真活writer/cleanup及HTTP/WS、R1.5总输入证据，保留既有已过。限30工具30分钟，不跑全仓/race；R1现Go冻结，主控核实际hash：push0b5080e0/integrationd2f2bbe1/recoverya8e1c902/workbenchpushd191443e/git0148cd06/storeebdde3d6/mergeInspect41eb6eae/remotedda6170d/procLinuxdc023fbd/totalTeste1a3da4e/webapi9cae63be。developer五包test/vet/build、单点cache race exit0，未全仓/完整race，不等同独立通过。已放行1f30f048接续R3 build+17445真实E2E；其在“All9pass后核F5b”响应流connection reset而error，无active/pending、无17445 server，非quota。实际产品TS hash未变，browser2b416e0c；last-run passed仅辅助，未拿部分thought作完成。主控实际读F5b发现if Stop与>=1/吞click错误/条件页断言，不足证新启动；已原会话同R3一次有界恢复，限12工具15分钟，只补F5 unknown true/false/drop、正常已知成功Stop立即新ID重开与必要清理，不重复未变9例/全仓。另15孤儿preview fakepi，cwd+exe hash一致为chat-turn临时夹具、无Go/Node server；初步Playwright默认硬杀跳过Go/Node清理，app已有Close，不改Go。仅额外准许专属PW gracefulShutdown SIGTERM15s与必要测试时序，记录前后PID保证无新增孤儿，旧15先保留待核归属，不盲删。主控另核Go Status只snapshot，Open只给局部返回URL不存entry；App running分支要求旧URL+approval导致重看running落新Start，已同当前有界恢复steer原UI最小改为running只Open（不需执行同意），新登录Stop后再启动仍重审；补正常Start后隔700ms再看Start delta0，错误“重登running要执行审批”旧测试纠正，不降低执行授权断言。原12工具15分钟不重置，不足诚实停报；UI首次F5已结束（be5ca8bf/2e345460，两例17.2s/exit0），但主控发现onlyExplicit回退已批缓存重开，原UI已按最小import/条件恢复ab9a50a1/browser98684302并仅typecheck0；新浏览器尚未跑。d7现已启动原Task2c/d R1独立编译验收，UI不得Vite/dist/Go/browser，待真实d7完成再仅build+F5子集。主控源检查待证重点交下一d7一次聚合：mainSHAAllowed快速相等分支仍绕祖先/对象核查、双父集合仅子集；finishPush仅fail为空才把unsafe改needs_check，fail非空可能仍failed；unsafe后下一Attempt/Started可能覆盖原PGID marker；recoverPush adapter无onFinish且外层release(true)、租约释放前写success；保存committed失败/dirty task重启审计尚需真正故障链。均标待实测，不把源风险当已复现，也不重报Push内含Observe误解。报告追加backend-1a，代码冻结；不新Agent盖章。远程UI仍未实现，F1/F2不重开，新增供应链缺陷单独计数；不跑真实GitHub或7331。当前源代码已开放审核push但独立未过且未部署，真实GitHub未调用，测试仅临时bare/注入transport。
- [ ] Task4独立功能验收：静态后端1a–1c独立验收者 `d7d2ec3f-408a-451f-827b-3950cd4702d4`（功能验收profile `pi/open-design/deepseek-v4.1-flash` high）已完成真实Chromium／攻击／生命周期／v1／race/vet；12hash一致。但报告证明悬空manifest软链回退发现、编码分隔符仍200和权限错误含绝对路径，主控据必需契约将门禁记为暂不通过（不重复已通过安全测试）。原后端已修3项并停写，主控核对manifest `ec209a2c`／gateway `a3e543a1`／manager `6bdb2553`／manifest_link_test `00accbe2`，开发者聚焦红绿/preview全包/webapi通过。已交原验收者d7仅3项聚焦复验，承接未变CSP/浏览器隔离证据，不重跑全套；d7本轮三项复验通过，随后主控仅对路由识别缺口追加临时overlay探针：`/pre%76iew/<cap>/assets%2fapp.css` 真实HTTP200，退出1，证据prefix-probe.log及报告§10；无越权但原始编码拒绝要求未全关闭。原开发者已补此边界，gateway `3d09280c`／manifest_link_test `b11c4737`，红绿证据成立、已冻结；d7已在真实前缀判定及ServeMux路由/原始socket验证编码前缀+分隔符400、双编码404、正常/空格/中文200，§11关闭§10，静态安全后端切片独立通过，不重做全套；证据及主控判断保存artifacts/acceptance-report.md §8。其余UI/下载/命令复开/Task2未完成，不更新7331。

本任务仅关联 `wks_1fe0f0d8c2bbce08`，协调者 `a32223bf-a426-42a7-80de-b1d43107e1f4`，cwd `/home/jimxl/projects/kaiwu`，main，git-dir/index `.git`／`.git/index`，基线 `04d50f4`。现有暂停subagents后端／types／旧截图／二进制／测试遗留不是本任务，保持原地；初始清单／共享diff／hash和本任务必要证据统一 `~/.local/state/agent-artifacts/kaiwu/2026-10-24-task-result-actions/`。原会话仅写计划／台账／正式状态说明；原后端已closed，新后端89f4d69f串行写preview；UI075c41cc独立写聊天文件，无共享写入冲突。UI普通修复第1轮，后端1b控制符遗漏交1c同开发者补测修复；执行停滞诊断1次、RPC故障新上下文恢复1次。原后端工具原始用量：inputTokens 1780566、cachedInputTokens 21045888、outputTokens 781527，非缓存口径未另核实；totalCostUsd=0是日志价表值，不视为免费或实付账单，其余角色用量未知。

# 当前改版：紧凑灰绿登录页

- [x] 用户查看 `docs/design/login-redesign.html` 及桌面／手机截图后明确批准开发；只改登录入口，保留认证与30天登录，不提交／推送／部署，不调用真实模型。
- [x] UI实现与自测：`c51d8ad2-e7d3-4547-b3a9-3ce3276f43a4`，实际配置 `pi/cliproxy/gemini-3.8-flash-high`、thinking high；App.vue／style.css与专用测试配置、截图完成，npm单元35／build／登录浏览器9／failure浏览器5报告通过。App.vue hash `0a442a59`、style.css `fbce14af`；已停止写入。原会话已核对实际diff与hash，其余混合改动保留；全仓diff-check现有types.ts尾空行为无关基线。
- [x] 独立功能验收与第1轮复验通过：`00e5014d-044b-4266-99c2-fe3c3c1d8f73`（`pi/open-design/deepseek-v4.1-flash`、配置thinking high，运行态off）。实跑 `cd apps/web && npx playwright test -c playwright.login.config.ts` 退出0、10 passed；最小复验探针退出0、1 passed，证明首次真实401无红字、提交后401显示“会话已过期”、按钮恢复、再Enter进入工作台，current-password断言通过。上一轮独立实跑30天cookie／刷新／跨context／退出撤销、网络／500提示、focus-visible 2px、真实快速Enter单POST、无存储泄漏及工作台烟测证据保留；桌面／360／390无溢出和短视口可滚，主控已目视actual桌面／手机与设计一致。测试仅临时回环服务＋假pi，服务已停止。
- [x] 原会话已核对实际diff／最终hash／限定diff-check及独立报告。P1静默回跳与autocomplete恢复均已修复并复验；本地实现完成，未提交／推送／部署，7331未更新。既有socket撤销用例单独grep缺前置session夹具 `browser-session.json`（ENOENT）属未验限制，其他退出撤销证据已通过，不伪称全套通过。完整复验报告 `/tmp/kaiwu-login-acceptance.md`；关键证据已汇总本节，设计与实际截图见 `docs/design/login-redesign*.png`。
- [x] 用户后续明确“提交代码”，授权本批commit／main推送／既有部署及安全归档；核实 GitHub `SatoriTours/kaiwu` 默认main，origin及本地基线同为`700b98a`，Actions工作流数0，不适用。仅提交登录页2实现／2测试配置／5设计资产及3份现有文档；同目录暂停子Agent后端、旧截图、二进制和测试遗留不纳入。通过既有reference-transaction钩子对候选干净归档做部署门禁，不直接构建混合目录上线。最终SHA／部署结果与证据保存于 `~/.local/state/agent-artifacts/kaiwu/2026-10-24-login-redesign/`；共享目录有其他未提交／未跟踪／忽略成果，空间保留，不擅自清理以归档。

本任务仅关联现有空间 `wks_5663ffa6688b6c19`，协调者 `f6df7bdb-8ebc-47f4-bb5a-41b40ce87d27`，cwd `/home/jimxl/projects/kaiwu`，分支 `main`，git-dir/index `.git`／`.git/index`。阶段：实现及独立验收完成，普通修复1轮已关闭；App.vue `cadbc3c6`，style.css `fbce14af`不变，spec `72bcc601`；开发build及专用10项通过，主控核diff/hash/限定diff-check通过。验收通道此前503 `workspace_billing_scope_unavailable`／HTTP2 GOAWAY，非额度耗尽；原会话同模型有界恢复一次，保留已完成9项证据，收敛剩余聚焦检查；验收图像能力受限，主控已实际目视桌面／手机截图补齐。角色token／费用未知。设计稿为本任务成果，暂停子Agent后端／旧截图／测试残留不是本任务范围。不新增或归档空间。

# 当前缺陷：任务失败原因与聊天局部布局

- [x] 核对截图、现有数据流与样式：pi errorMessage 未被保留；任务错误仅在条件化折叠区显示；短用户消息气泡整行拉伸。
- [x] 限定方案：保留具体错误，在所属聊天直接展示失败提示；短消息收紧，长内容／手机不溢出。不触发模型、提交或部署。
- [x] 用户追加授权只读服务器日志取证：2026-10-20T16:49:00Z～16:49:14Z，该任务原 pi JSONL 连续 4 条 HTTP 429／rate_limit_error，MiniMax Token Plan 速率限制（2062）；无工具执行。Web DB 仅保存通用错误，服务仍 active，未重启／重试／改模型。真实会话留服务器，不复制入仓库。
- [x] 用户质疑模型／套餐后追加只读核验：任务与服务器 pi Key 指纹一致、均 Token Plan Key；官方余额只读接口 status_code=0，当前窗口剩余 93%，不能认定额度耗尽。任务模型 `MiniMax-M3.1-Flash`、中国站 Anthropic 兼容端点；服务器 pi 1.3.0 用假 Key／合成消息／全局禁网＋拦截 fetch 的离线请求证实 `max_tokens=524288`、adaptive medium。过大输出预算可能触发 TPM，仅为待验证假设；未改配置、未真实请求。
- [x] 后端执行与自测：`46bd7ddc-e4c0-4504-b6e4-7bbdc175fe92`，仅 `server/internal/agent/`；原会话已核对实际 diff。具体原因安全提取／限长／脱敏并写现有双错误通道；新轮次清旧错误。agent 包、race、构建与限定 vet 报告通过，后续独立验收已通过（见下）。测试hash见执行端完成报告，实际生产未更新。
- [x] UI 备用实现与自测完成：原 Gemini 503 后，经用户明确授权由 `46bd7ddc-e4c0-4504-b6e4-7bbdc175fe92`（pi/opencode-go/deepseek-v4.1-flash）接续8文件；失败卡进入消息流、短气泡fit-content、状态独立，未改MiniMax配置。专用9单元／5浏览器、npm test/build、8交互／4UI一致性报告通过；版本套件2项is-outdated失败有基线复现，不在本批修复。实际diff与SHA256已核对，原会话保留独立验收关注项（发送／中断具体原因是否被隐藏）。
- [x] 后端切片独立功能验收通过：`e7739791-8372-4466-8a57-7e429db68fde`（pi/opencode-go/glm-5.3-flash），13 项聚焦测试、agent 整包、vet 退出 0；原因双通道／脱敏限长／兜底／新轮次清理／已有结果保留覆盖。原会话实际 diff、验收范围、SHA256 与 `git diff --check` 已核对。7 文件 SHA256 前缀：failure.go `dd8f9154`、failure_test `3cf4f52b`、failure_events_test `46e60752`、events `dac8be81`、manager `3d8aaef9`、model_next `3485f59a`、manager_test `7ac94071`。观察项：含 `{` 的不可解析纯文本回退通用原因（安全兜底）；真实上游根因验证与 UI 不在通过范围。
- [x] UI 独立验收通过：原验收 `e7739791-8372-4466-8a57-7e429db68fde` 实跑 `cd apps/web && node tests/failure.test.ts`（9/9）、`npm test`（29＋6）、`npx playwright test --config=playwright.failure.config.ts`（5/5）及临时DOM／视觉探针，退出均0。1440／390／360截图目视与无横向溢出、短气泡、角色状态分离、具体／空原因、任务隔离、待确认结果保留有证据。原会话核对8文件SHA256与diff-check一致。发送／中断当前后端只有固定状态句，未发现具体诊断被隐藏；保留低危文案观察：发送未达时“pi 未提供具体原因”可优化为中性措辞。截图与测试产物位于/tmp，非线上验收。
- [ ] 本地修复及独立切片验收完成，未提交／部署；当前真实网页未更新。既有版本浏览器2项is-outdated失败保留，不宣称全套通过。实际 MiniMax 429根因仍未确认；用户后续明确要求验证，2026-10-21T04:56:25Z用同Key／模型／国内端点、medium、8192预算单次真实最小请求成功：1221ms、回复OK、225 token（212输入／13输出含9推理），无重试／工具／配置写入，非Web任务恢复。未做524288同期对照，不据此认定预算因果。证据见模型选择验证补记。
- [x] 用户批准降低默认预算并验证原任务：服务器定义＋原任务隔离副本均持久化8192（备份留服务器），同一模型／medium、原session／worktree经现有set_next_profile同配置回收后继续。8次真实模型响应／13工具，本轮无429，生成5文件；最终waiting_user／awaiting_user，非完成。工作台未重启／部署，Git未提交。非缓存输入7465、缓存38017、输出4473，本地价表估算$0.0043547891，非实付；详见模型选择验证最新补记。
- [x] 用户后续明确授权环境修复：从官方校验SHA256后用户级安装Go1.27.3，修复Git包装脚本从自身路径定位工具而非HOME；server／pi隔离HOME下git/go均可用。未改系统目录／认证／PATH／工作台程序。备份证据留服务器tool-updates/2026-10-21T11-52-09-937Z。
- [x] 经原待答交互入口继续原pi：4项Go测试／vet／build及真实回环GET/HEAD/404通过，临时程序停止、端口释放；原5文件未改、未提交。任务needs_confirmation／awaiting_review，未代用户确认。此轮6次模型响应无error，非缓存输入11051、缓存读取50021、输出2314，价表估算$0.0037791787非账单。
- [x] 环境与Go示例独立验收通过（e7739791）：官方归档校验／用户安装位置／Git脚本diff与隔离HOME工具可用核对，独立go build/vet/test退出0、4测试通过；真实回环34621的GET/hello200正文world、HEAD空体／未知404及Content-Type通过。测试进程／端口与临时产物已回收，5源码hash及Git无提交不变，工作台54851仍active。主控再次核对wrapper和5源码SHA256、服务PID与34621已释放。
- [ ] 交付边界：原产品任务保留needs_confirmation／awaiting_review待用户确认，不新建Paseo空间，不将其5文件混入开悟源码。MiniMax输出预算8192已生效、本轮真实调用恢复且示例运行验收通过，但未做原大预算同期对照；工作台失败提示／布局源码仍未提交／部署。

- [x] 2026-10-23 用户“提交代码”（重复口令合并一次）授权本批整合／push／部署；核实GitHub `SatoriTours/kaiwu` 默认main，远端及本地基线均e43b8bf。Actions工作流数0，不适用，沿用已安装的提交钩子干净归档部署，不伪称CI通过。
- [x] 19文件限定暂存，无既有暂存内容；排除暂停子Agent实现、旧截图、假进程二进制和测试遗留。源码hash与独立验收一致。暂存树641e5a55导出的测试快照：agent包、专用单元9项、npm单元29＋6、前端构建及专用浏览器5项通过。归档浏览器首次漏传GOFLAGS导致VCS标记失败，按既有部署脚本传入-buildvcs=false后通过，无实现改动，不掩盖失败。
- [ ] 交付执行：本批commit／push、干净归档完整部署门禁及实际7331入口核查；最终SHA和结果另存当前空间外。关联Paseo空间仅当前wks_6b8c83a60fa23afb，子Agent46bd7ddc／e7739791／4c2fea93同checkout且已停止。没有新增关联Workspace。服务器Go示例为原产品测试任务，保留原worktree与待确认状态，不进入开悟仓库。共享checkout尚有其他任务未提交／未跟踪／忽略成果，不满足安全归档，保留空间，不删除、stash或代交。

协调空间：`wks_6b8c83a60fa23afb`，cwd `/home/jimxl/projects/kaiwu`，`main`，git-dir/index `.git`／`.git/index`。现有子 Agent 后端、截图等无关混合改动原地保留。角色消耗未知，修复轮次 0。

# 当前设计：固定开发交付与 Actions 自动修复

- [x] 核对现有 Git／worktree／会话／恢复机制，确认当前仅有本地审核集成。
- [x] 用户将 Push 入口需求扩展为项目配置的统一流程，要求 Actions 检查及自动修复；按 Architectural 路径推进。
- [x] 比较手动串联、仅提示词驱动、Go 固定状态机；用户确认方向并同意整理书面设计。
- [x] 编写并自查[规格](docs/superpowers/specs/2026-10-17-automated-delivery-design.md)，同步需求／架构／README／路线图；不提交、不写业务代码。
- [x] 用户在规格交付后回复“ok”，审核书面规格及其中的默认规则、凭据与授权边界。
- [x] 编写并自查[14项实施计划](docs/superpowers/plans/2026-10-17-automated-delivery.md)，明确接口／失败测试／恢复边界及实际模型切换缺口。
- [x] 用户回复“ok”审核计划并选择当前会话串行实现＋最终独立只读验收。
- [x] Task 1、2：配置／领域校验、schema v2、周期／步骤／快照事务与回执；Go 全量、workbench race／vet 通过，见[执行记录](docs/development/web-automated-delivery-ledger.md)。
- [x] Task 3：官方契约取证阻塞已解除；Actions 只读客户端与合成契约测试已实现。
- [x] Task 4–12：凭据桥、远程 Git、原会话修复／模型下一轮、FIFO 集成、崩溃恢复、业务 Socket 与生产装配；隔离 Go 测试通过。
- [x] 2026-10-18 核对未提交成果：本地提交 `1c76c0a`，81 文件；Go 全量与前端原有 16 项复跑通过，未推送。两项 extra-*.txt 历史测试残留保留但未提交。
- [ ] Task 13：前端按实际 Pi Gemini UI profile 实现，主控已验前端27/27、Chromium42/42（含10项真实服务交付场景）；真实Go＋临时bare＋假pi／Actions六场景race通过。配置保存未知回执等边界仍待收口，不能把页面功能与隔离测试当作整体可上线。
- [ ] Task 14：独立后端审查已完成但不通过；主控修复 2 Critical、3 Important，证伪 1 Important，另 1 项部分修复。仍有 2 Critical／14 Important／1 Minor，见[续接清单](docs/development/web-automated-delivery-review-resolution.md)。Go 全量／race／vet／构建／烟测与六场景夹具 race 已复验；不宣称真实闭环已可用，不启用真实上传，7331 未更新。
- [ ] 真实 GitHub／Actions／模型联调另行说明费用、副作用并获准。

# 当前批次：正式 Web 服务

- [x] 用户确认 Vue 3＋TypeScript＋Vite、SQLite、本机令牌和 pi 独立会话生命周期。
- [x] Go 持久化／幂等回执、认证／同源静态资源／WebSocket、独立 RPC 管理。
- [x] Vue 专题看板、项目／任务聊天、独立草稿／阅读位置、手机视图、断线确认。
- [x] 真实 Go＋隔离假 pi 浏览器 7 项场景通过；真实模型未调用。
- [x] 最终 Go race/vet、独立复验与 [证据归档](docs/development/web-service-verification.md)。
- [ ] 下一条：pi 执行 Agent、worktree 单写入者、Go 编排工具桥、审批／实际验证／结果验收。
- [ ] 用户要求每张卡片有“开始开发”按钮，任务聊天也可说“开始开发”；[双入口与执行生命周期方案](docs/superpowers/specs/2026-10-12-task-start-design.md) 已整理，待审核，尚未编码。

以下保留历史批次记录，不代表当前服务状态：

# 当前状态：Go pi RPC 基础已验收，Web 看板交互原型已实现

## 当前设计修订：Kanban 主界面＋右侧双范围聊天

- [x] 用户最新确认功能专题列可手动添加／删除；每个卡片是任务，右上角显示未开始／执行中／需要确认／执行完成查看结果／执行失败。此前固定状态列取消；右侧项目／任务聊天规则不变。
- [x] 用户确认增加“其他”专题；删除专题时任务全部移到其他，不删任务、不改状态／对话／结果、不停止执行。
- [x] 用户要求开始开发；本批收敛为本地专题看板交互原型，不隐式推进生产工程或真实模型联调。
- [x] 据最新决定统一正文图示、专题管理、其他收纳与五种角标；移除旧状态列／验收后搬列规则。
- [x] 核对当前文件、Git 与同目录写入者；main 无基线，本会话是当前唯一 running 会话，尚未派发编码。
- [x] 核对实际 Paseo profiles：前端用途配置现名“opencode deepseek”，架构／验收配置保留；不根据旧“Gemini”名字猜模型。
- [x] 编写并自查 [限定原型计划](docs/superpowers/plans/2026-10-11-web-kanban-prototype.md)：纯状态测试、真实 DOM 检查、一次独立验收；入口同步。
- [x] 用户明确“继续开发”；按现有原型计划继续实现。默认 Paseo daemon 不可用，当前 Agent 串行编码＋工具提供的只读独立验收；未声称使用不可读取的 profile。
- [x] Task 1：稳定专题／任务／Conversation、独立草稿与迟到记录的 RED→GREEN；15 项状态测试通过。
- [x] Task 2：专题看板、右侧聊天、浏览器行为／布局 RED→GREEN 与新截图；44 项交互检查通过，含审查发现的手机阅读位置及重复标签回归。
- [x] Task 3：本批实际记录、一个只读 Agent 独立验收与修复复验、问题反例修复及本地交付；0 Critical／0 Important／1 焦点 Minor，历史 20 项检查不复用。

## Web 主工作台原设计与历史原型（2026-10-11）

- [x] 用户确认新增 Web 版，作为主要开发入口，优先完整流程；PC/Android 保留首版目标。
- [x] [功能与交互设计提案](docs/superpowers/specs/2026-10-11-web-workbench-design.md)：信息架构、核心流程、状态、审批/模型/恢复边界、技术建议与 W0–W4 分期。
- [x] 历史模拟原型曾演示任务创建、对话、审批、模型待生效、离线只读和结果验收；其 [历史记录](docs/development/web-design-verification.md) 单独保留，HTML 现已换为专题看板原型，不复用旧功能或测试声明。
- [x] README、需求、架构、路线图与客户端入口同步。
- [x] Chromium 原型 20 项交互检查、1440px/360px 截图核对、脚本语法与 Markdown 本地链接检查；[实际记录](docs/development/web-design-verification.md)。不代表生产业务验收。
- [ ] 用户审核具体交互方案与前端选型；认证/持久化/可信执行边界另行设计。
- [ ] 生产 Web 工程、Go 业务 API、真实 pi 闭环与跨浏览器验收；不由设计文档隐式授权实现或发布。

## 已完成批次：Go pi RPC 基础

用户已审核 [RPC 基础规格](docs/superpowers/specs/2026-10-11-go-pi-rpc-design.md) 并回复“继续”，确认具体生命周期、限额及验收范围；自有后端全部 Go。[实施计划](docs/superpowers/plans/2026-10-11-go-pi-rpc.md) 及串行编码＋独立验收方式已批准；Tasks 1–4 已实现并自测通过，Task 5 独立验收已通过，首轮 2 Important/1 Minor 已修正复验。不启动产品真实 pi/模型联调，不提交或推送。

## 已完成批次：Go 服务基础

状态：用户已确认规格、计划及当前 Agent 串行实现＋独立验收方式。模块与 Go 代码已实现并通过初次独立验收；3 项边界修正已获用户确认并通过本机 32 项测试、复测及限定独立核对；不提交、不安装全局工具、不调用真实模型。

## 设计流程

- [x] 核对项目文档、文件和 Git 状态：仓库只有文档，main 尚无提交。
- [x] 明确本轮功能范围和成功标准：先划分仓库模块并建立 Go 服务基础，pi 接入随后推进。
- [x] 比较 3 个推进方案；用户同意推荐方向，并要求先完成 Go。
- [x] 用户随书面规格确认详细配置、HTTP、生命周期与验收设计。
- [x] 当前没有需要浏览器可视化的问题，不启用可视化辅助。
- [x] 写入 docs/superpowers/specs/2026-10-11-go-foundation-design.md，未经授权不提交。
- [x] 自查规格的占位项、一致性、范围与歧义。
- [x] 用户审核书面规格并回复“好的，继续”。
- [x] 编写并自查 docs/superpowers/plans/2026-10-11-go-foundation.md。
- [x] 用户审核实施计划及确认推荐执行方式。
- [x] Task 1：模块入口与回环配置，RED/GREEN 已记录。
- [x] Task 2：HTTP、日志、panic 边界，RED/GREEN 已记录。
- [x] Task 3 编码：启停、退出码及二进制烟测，RED/GREEN 已记录。
- [x] 本机全量测试、race、vet、格式、依赖校验、构建与信号烟测。
- [x] 同步各模块入口、README、架构、初始化范围和路线图。
- [x] Paseo 独立只读验收：无阻塞问题；3 项 Minor 保留，完整结果已保存。
- [x] 最终普通/race 测试、构建/信号烟测复核通过；保留未提交成果，不合并/推送。

## 已确认的小范围收尾

- [x] 核对独立审查结果、源码并自行复现 M2/M3。
- [x] 提出 bounded 修正方案：Server ErrorLog 接入 slog；未知方法/不存在路径返回正确 404；HEAD/GET 显式 Content-Length 一致。
- [x] 用户确认该短设计并回复“继续”，不另写架构规格或长实施计划。
- [x] 每项增加失败回归测试、最小修复，32 项全套测试/race/vet/二进制烟测通过，并更新问题历史与当前状态。
- [x] 限定独立核对：原验收会话 RPC 已关闭，替代只读 Agent 27abc3d6-d302-4b49-85f4-88d89718aecc 验收通过；文档滞后已同步。
- [x] 最终复核和本地交付；仍不提交/推送、不调用真实模型。

## 本批次设计流程：Go pi RPC 接入基础

状态：Architectural 路径；pi 1.0.4 RPC schema 已随规格确认作为有限协议基线，不代表真实兼容验收。以下设计/计划任务不构成编码授权。

- [x] 核对项目文档、已有服务结构和 Git 状态；main 仍无提交。
- [x] 本批次暂无需要浏览器展示的问题，不启用可视化辅助。
- [x] 核对本机 pi 1.0.4 的文档、RPC 类型、客户端、RPC mode、JSONL 分帧和隔离参数；未启动 pi，未验证真实运行。
- [x] 复述目的、约束与成功标准；用户确认模块边界，并进一步明确自有后端全部 Go。
- [x] 比较接入方案：采用 Go 直管 RPC；排除自有 TypeScript SDK 宿主和终端抓取。
- [x] 同步当前需求与架构；旧 Go 基础规格/计划注明扩展建议已被取代，保留历史。
- [x] 写入候选生命周期、超时/取消/背压、故障策略和假进程验收的完整待审规格；未经授权不提交。
- [x] 用户随书面规格审核具体模块、数据流、限额和退出策略。
- [x] 自查规格的占位项、一致性、范围与歧义；明确零值 Options 使用默认值、写入总期限和取消后的载荷释放。全仓库本地 Markdown 链接检查 56 项，损坏 0；未执行代码测试。
- [x] 用户审核并批准书面规格，回复“继续”。
- [x] 编写实施计划并自查覆盖、接口一致性、5 项 Review Focus 与测试归属；计划阶段本地 Markdown 链接检查 65 项、损坏 0，规格/计划状态一致；RPC 包尚未创建，全部代码/测试待执行。
- [x] 用户回复 ok，审核实施计划并确认当前会话串行编码＋完成后独立只读验收。
- Tasks 1–5 已完成；逐项 RED/GREEN 见 [ledger](docs/development/go-pi-rpc-ledger.md)。

## 获批计划执行

- [x] Task 1：协议、显式参数、有界 JSONL 的 RED/GREEN。
- [x] Task 2：事件 FIFO 与 stderr 尾部的 RED/GREEN。
- [x] Task 3：Go 假进程、自管管道和直接子进程回收的 RED/GREEN。
- [x] Task 4：握手、关联、取消、预算和统一终态的 RED/GREEN。
- [x] Task 5：全量验证、实际记录和独立只读验收；84 顶层通过＋1 预期 Skip，首轮问题已修正复验。
- [ ] 真实模型、权限、持久化、主执行闭环和双端继续单独推进，不由本清单隐式授权。

## 已知边界

- 自有后端全部 Go，不新增 TypeScript 扩展或 SDK 宿主；pi 仍是第三方 CLI。
- pi 优先，单执行节点；Go 服务负责生命周期，双端是客户端。
- PC：Nim + Leaf，目标 Windows/macOS/Linux；Android：Kotlin + KMP + Compose。
- 执行任务使用独立 worktree，同工作区单写入者。
- 配置模板、任务快照、任务内模型覆盖分别管理。
- 接受命令、Agent 运行结束、任务验收成功不能混淆。
- 客户端断线不能隐式取消服务器任务。
- 合并、推送及破坏性操作需人工确认；worktree 不是安全沙箱。
- 当前空仓库无 worktree 基线，不为创建隔离空间擅自提交。
