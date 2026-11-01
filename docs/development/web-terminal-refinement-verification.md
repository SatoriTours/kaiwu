# 网页终端精修 A

用户选定V7 A「轻底色标签」后回复“开始”，批准本批实现；开发验收后现已要求“提交代码”，并明确实际部署统一 `.91:7331`。共享main目录基线 `f2df17d`，仅交付本批终端成果，保留其他任务改动。

## 本地实现

- 终端工作区／标签／新建／更多进入48px全站顶栏，无第二行或底部常驻条。正文14px紧凑行距、零外留白；当前标签28px高／12px普通字体／6px圆角、轻底色细边界。手机使用当前终端下拉及菜单。
- xterm按全站浅色／深色／系统变化更新色板，无需重建Shell。现有输入、ACK、尺寸、读屏及WebGL回退路径保留。
- 首次无历史原子ensure，已有终端接回原ID；显式新建使用operationId。独立操作账本防止ensure别名或结束历史淘汰导致重复创建，4096条上限拒绝额外操作；8个运行终端上限不变。
- 每工作区终端组占有一份根租约，各Shell持久记录独立进程组／未知启动窗；逐个结束不放开同组，确认最后成员退出后释放，归属不明保留锁。延续原普通后代FD继承／进程组回收及AI／文件写／合并互斥，不是系统沙箱。
- 浏览器只保存视图、范围／终端ID、手动结束标志及待核查创建ID；无输入、输出或访问令牌。关闭网页不停止Shell，不重放命令；新建送达未知先查询，不自动重发。手动结束需确认，不自动重建。

## 自测与独立门禁

当前：独立首轮发现3项功能问题及行距偏差，原实现者已集中修复；4项新增浏览器回归先红后绿，最终16项受影响浏览器以15＋1分组有成功证据，新增只读查询、默认前端测试／构建、最终Go两包race通过。独立窄复验已通过，F1–F4全部关闭；结合首轮未变范围证据，本批通过本地限定验收，不代表部署或全项目全量验收。

证据统一位于 `~/.local/state/agent-artifacts/kaiwu/2026-10-31-terminal-design/`，唯一短状态／证据索引为 `status.md`，初轮产品源码指纹 `source-review.sha256`，修复后16文件指纹 `source-final.sha256`；原失败报告 `independent-review.md`保留。

- 默认 `npm --prefix apps/web test`（包含新增3项选择／安全存储测试）、`npm --prefix apps/web run build`通过；仍有既有500kB包体警告。
- `go -C server test -race ./internal/agent ./internal/projecttools ./internal/webapi ./internal/app`通过，操作别名／历史淘汰增量后另跑最终projecttools race通过；四包vet通过。
- `cd apps/web && npx playwright test --project=terminal --project=project-tools --project=theme`修复后最终16项有分组通过证据（原11项＋4项独立发现对应回归＋1项过期list／核查回执对齐）。合并运行15通过，最后全站主题登录触发既有一分钟单IP登录限流；不改认证，在新隔离生命周期仅复跑该项通过，不声称同服务连续16全绿。使用隔离Go服务、真实Shell及固定假pi，不调用真实模型。
- 覆盖关页后两个独立Shell的原ID／环境变量／目录恢复、选中持久化、取消／确认结束非当前标签不切错、手动结束后刷新不自建、实时深浅色、360／390／1024／1440px无全页横溢及正文零边距；保留输入-only中文／emoji、历史、Tab补全、Ctrl+C、清屏、ANSI、resize、超长输入整次拒绝、大量输出及文件附件回归。
- 修复后实际深浅色桌面／手机图在 `review-fixes/`，原图在 `implementation/`；截图等待WebGL两帧重绘，主控及独立端已实际读新图，桌面／手机深浅色视觉通过。保留旧失败日志，不覆盖成成功证据。

## 独立首轮发现与集中修复

1. 自然退出误作手动关闭：恢复选中ID时保留其已结束快照及输出，不切到兄弟Shell；只有显式结束才关闭所选标签。
2. 异步首帧抢走键盘标签焦点：若当前焦点在tab，不自动聚焦Shell；ArrowLeft／End在接回完成后仍可连续导航。
3. ensure别名未知结果无法靠list核清：新增纯只读 `terminal/resolve` 查询账本，覆盖别名、未知、跨范围隔离和已淘汰快照；不申请租约或启动进程。核清旧操作后，新的一次新建意图使用新ID，不消耗为旧请求接回。核查等启动完成后返回的真实保留快照可修补较早的空list，不重复启动；已淘汰元数据不伪装成可回放快照。
4. 实际行距19px：xterm系数调整为1.08；本机浏览器实际行盒17px，新增16–17.5px断言，不拿字体字号或继承CSS行高冒充实际度量。

红绿日志 `review-browser-{red,green}.log`；最终 `review-browser-regression.log`、`review-{build,unit,race}.log`。过期库存新增反例见 `receipt-red.log`；最终合并的登录限流原始失败见 `review-browser-combined-rate-limit.log`，该项新生命周期通过见 `review-theme-isolated.log`。最终独立报告 `independent-review-final.md`：3项真实Shell窄探针、Lookup定向race及1项库存／回执对齐探针通过；后者观察1次resolve、0次start重放、后端仍1个Shell。最终16文件指纹清单自身SHA256为 `27be5e79304421b9a9a0acb4fd04b356b667b26a379f87c8ace6c8486221b925`，主控已核对最终diff、清单及原始日志。测试入口／用例指纹另见 `final-tests.sha256`。测试浏览器／17331和17332隔离服务已退出，未触碰共享服务。

## 明确边界

只保证服务器持续运行期间的原Shell接回；服务重启不恢复旧进程。输出仍仅最近256KiB，不保证任意全屏程序像素级重现。不持久化命令输入／输出到浏览器。

开发验收未运行全项目门禁；用户要求提交后，沿用仓库提交钩子的精确SHA干净归档全量Go vet／测试、前端单元／构建、全部既有分组浏览器及Go打包，再更新实际 `.91:7331`，核对版本及真实页面。仓库本次查询无GitHub Actions工作流，不声称CI通过。完整提交SHA／全量结果／推送与实际入口证据保存在同一artifact目录的 `status.md`。

不测试真实模型、真机软键盘、硬件GPU或跨浏览器矩阵。服务更新会终止旧内存Shell，恢复保证仅限同一服务运行期。

## 提交交付增量：局域网 HTTP

首提交 `6283f44` 已推送并部署至 `.91:7331`，精确归档全量Go vet／测试、前端默认单元／构建、90项浏览器及Go打包通过；无Actions工作流。但实际入口追加交互未通过：公网／局域网HTTP不是安全上下文，`crypto.randomUUID`不存在，新增终端在发出启动请求前即报错。原生产失败证据保留 `live/result-first-failure.json` 及 `live/result-http-failure.json`，不能把localhost全绿外推为实际入口通过。

最小修复复用已有 `workbench.ts:createOperationID`（`getRandomValues`生成128位随机操作ID，可用于LAN HTTP），不改认证、创建幂等或租约协议，不引入依赖。正式浏览器新增“terminal creation works on LAN HTTP without secure-context randomUUID”，仅移除浏览器的安全上下文API，真实Go／Shell验证自动打开、再次新建的独立ID及操作ID、显式结束。该项先失败（`lan-http-red.log`），修复后通过（`lan-http-green.log`），构建通过（`lan-http-build.log`）。修复提交必须重新完成精确SHA全量门禁及 `.91` 实际入口，最终结论仍以artifact `status.md` 为准。
